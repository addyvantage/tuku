package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/run"
)

type handoffRepo struct{ q queryable }

func ensureHandoffTables(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS handoff_packets (
	handoff_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	status TEXT NOT NULL,
	source_worker TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	checkpoint_id TEXT NOT NULL,
	brief_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	accepted_at TEXT,
	accepted_by TEXT,
	packet_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_packets_task_created
	ON handoff_packets(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS handoff_acknowledgments (
	ack_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	status TEXT NOT NULL,
	summary TEXT NOT NULL,
	unknowns_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_acknowledgments_handoff_created
	ON handoff_acknowledgments(handoff_id, created_at DESC, ack_id DESC);

CREATE TABLE IF NOT EXISTS handoff_follow_throughs (
	record_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	launch_attempt_id TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	kind TEXT NOT NULL,
	summary TEXT NOT NULL,
	notes_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_follow_throughs_handoff_created
	ON handoff_follow_throughs(handoff_id, created_at DESC, record_id DESC);

CREATE TABLE IF NOT EXISTS handoff_resolutions (
	resolution_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	launch_attempt_id TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	kind TEXT NOT NULL,
	summary TEXT NOT NULL,
	notes_json TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_resolutions_handoff_created
	ON handoff_resolutions(handoff_id, created_at DESC, resolution_id DESC);

CREATE TABLE IF NOT EXISTS handoff_launches (
	attempt_id TEXT PRIMARY KEY,
	handoff_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	target_worker TEXT NOT NULL,
	status TEXT NOT NULL,
	launch_id TEXT NOT NULL,
	payload_hash TEXT NOT NULL,
	requested_at TEXT NOT NULL,
	started_at TEXT,
	ended_at TEXT,
	command TEXT NOT NULL,
	args_json TEXT NOT NULL,
	exit_code INTEGER,
	summary TEXT NOT NULL,
	error_message TEXT NOT NULL,
	output_artifact_ref TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_handoff_launches_handoff_requested
	ON handoff_launches(handoff_id, requested_at DESC, attempt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure handoff tables: %w", err)
	}
	return nil
}

func (r *handoffRepo) Create(packet handoff.Packet) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	now := packet.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_packets(
	handoff_id, task_id, status, source_worker, target_worker, checkpoint_id, brief_id,
	created_at, updated_at, accepted_at, accepted_by, packet_json
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`,
		packet.HandoffID,
		string(packet.TaskID),
		string(packet.Status),
		string(packet.SourceWorker),
		string(packet.TargetWorker),
		string(packet.CheckpointID),
		string(packet.BriefID),
		now.Format(sqliteTimestampLayout),
		now.Format(sqliteTimestampLayout),
		nilIfTime(packet.AcceptedAt),
		nilIfString(string(packet.AcceptedBy)),
		string(packetJSON),
	)
	if err != nil {
		return fmt.Errorf("insert handoff packet: %w", err)
	}
	return nil
}

func (r *handoffRepo) Get(handoffID string) (handoff.Packet, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Packet{}, err
	}
	row := r.q.QueryRow(`
SELECT packet_json
FROM handoff_packets
WHERE handoff_id = ?
`, handoffID)
	return scanHandoffPacket(row)
}

func (r *handoffRepo) LatestByTask(taskID common.TaskID) (handoff.Packet, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Packet{}, err
	}
	row := r.q.QueryRow(`
SELECT packet_json
FROM handoff_packets
WHERE task_id = ?
ORDER BY created_at DESC, handoff_id DESC
LIMIT 1
`, string(taskID))
	return scanHandoffPacket(row)
}

func (r *handoffRepo) ListByTask(taskID common.TaskID, limit int) ([]handoff.Packet, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.q.Query(`
SELECT packet_json
FROM handoff_packets
WHERE task_id = ?
ORDER BY created_at DESC, handoff_id DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, fmt.Errorf("list handoff packets by task: %w", err)
	}
	defer rows.Close()

	out := make([]handoff.Packet, 0, limit)
	for rows.Next() {
		var packetJSON string
		if err := rows.Scan(&packetJSON); err != nil {
			return nil, err
		}
		var packet handoff.Packet
		if err := json.Unmarshal([]byte(packetJSON), &packet); err != nil {
			return nil, err
		}
		out = append(out, packet)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *handoffRepo) UpdateStatus(taskID common.TaskID, handoffID string, status handoff.Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	packet, err := r.Get(handoffID)
	if err != nil {
		return err
	}
	if packet.TaskID != taskID {
		return fmt.Errorf("handoff %s task mismatch: got %s expected %s", handoffID, packet.TaskID, taskID)
	}
	packet.Status = status
	if status == handoff.StatusAccepted {
		packet.AcceptedAt = &at
		packet.AcceptedBy = acceptedBy
	}
	if len(notes) > 0 {
		packet.HandoffNotes = append(packet.HandoffNotes, notes...)
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return err
	}

	res, err := r.q.Exec(`
UPDATE handoff_packets SET
	status=?, updated_at=?, accepted_at=?, accepted_by=?, packet_json=?
WHERE handoff_id = ? AND task_id = ?
`,
		string(status),
		at.Format(sqliteTimestampLayout),
		nilIfTime(packet.AcceptedAt),
		nilIfString(string(acceptedBy)),
		string(packetJSON),
		handoffID,
		string(taskID),
	)
	if err != nil {
		return fmt.Errorf("update handoff status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *handoffRepo) SaveAcknowledgment(ack handoff.Acknowledgment) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	unknownsJSON, err := marshalStringSlice(ack.Unknowns)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_acknowledgments(
	ack_id, handoff_id, launch_id, task_id, target_worker, status, summary, unknowns_json, created_at
) VALUES(?,?,?,?,?,?,?,?,?)
`,
		ack.AckID,
		ack.HandoffID,
		ack.LaunchID,
		string(ack.TaskID),
		string(ack.TargetWorker),
		string(ack.Status),
		ack.Summary,
		unknownsJSON,
		ack.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff acknowledgment: %w", err)
	}
	return nil
}

func (r *handoffRepo) SaveResolution(record handoff.Resolution) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	notesJSON, err := marshalStringSlice(record.Notes)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_resolutions(
	resolution_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?)
`,
		record.ResolutionID,
		record.HandoffID,
		record.LaunchAttemptID,
		record.LaunchID,
		string(record.TaskID),
		string(record.TargetWorker),
		string(record.Kind),
		record.Summary,
		notesJSON,
		record.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff resolution: %w", err)
	}
	return nil
}

func (r *handoffRepo) CreateLaunch(launch handoff.Launch) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	argsJSON, err := marshalStringSlice(launch.Args)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_launches(
	attempt_id, handoff_id, task_id, target_worker, status, launch_id, payload_hash,
	requested_at, started_at, ended_at, command, args_json, exit_code, summary,
	error_message, output_artifact_ref, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		launch.AttemptID,
		launch.HandoffID,
		string(launch.TaskID),
		string(launch.TargetWorker),
		string(launch.Status),
		launch.LaunchID,
		launch.PayloadHash,
		launch.RequestedAt.Format(sqliteTimestampLayout),
		nilIfZeroTime(launch.StartedAt),
		nilIfZeroTime(launch.EndedAt),
		launch.Command,
		argsJSON,
		launch.ExitCode,
		launch.Summary,
		launch.ErrorMessage,
		launch.OutputArtifactRef,
		launch.CreatedAt.Format(sqliteTimestampLayout),
		launch.UpdatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff launch: %w", err)
	}
	return nil
}

func (r *handoffRepo) LatestResolution(handoffID string) (handoff.Resolution, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Resolution{}, err
	}
	row := r.q.QueryRow(`
SELECT resolution_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
FROM handoff_resolutions
WHERE handoff_id = ?
ORDER BY created_at DESC, resolution_id DESC
LIMIT 1
`, handoffID)

	var (
		record       handoff.Resolution
		taskID       string
		targetWorker string
		kind         string
		notesJSON    string
		createdAtRaw string
		err          error
	)
	if err := row.Scan(
		&record.ResolutionID,
		&record.HandoffID,
		&record.LaunchAttemptID,
		&record.LaunchID,
		&taskID,
		&targetWorker,
		&kind,
		&record.Summary,
		&notesJSON,
		&createdAtRaw,
	); err != nil {
		return handoff.Resolution{}, err
	}
	record.Version = 1
	record.TaskID = common.TaskID(taskID)
	record.TargetWorker = run.WorkerKind(targetWorker)
	record.Kind = handoff.ResolutionKind(kind)
	if record.Notes, err = unmarshalStringSlice(notesJSON); err != nil {
		return handoff.Resolution{}, err
	}
	if record.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAtRaw); err != nil {
		return handoff.Resolution{}, err
	}
	return record, nil
}

func (r *handoffRepo) LatestResolutionByTask(taskID common.TaskID) (handoff.Resolution, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Resolution{}, err
	}
	row := r.q.QueryRow(`
SELECT resolution_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
FROM handoff_resolutions
WHERE task_id = ?
ORDER BY created_at DESC, resolution_id DESC
LIMIT 1
`, string(taskID))

	var (
		record       handoff.Resolution
		taskIDRaw    string
		targetWorker string
		kind         string
		notesJSON    string
		createdAtRaw string
		err          error
	)
	if err := row.Scan(
		&record.ResolutionID,
		&record.HandoffID,
		&record.LaunchAttemptID,
		&record.LaunchID,
		&taskIDRaw,
		&targetWorker,
		&kind,
		&record.Summary,
		&notesJSON,
		&createdAtRaw,
	); err != nil {
		return handoff.Resolution{}, err
	}
	record.Version = 1
	record.TaskID = common.TaskID(taskIDRaw)
	record.TargetWorker = run.WorkerKind(targetWorker)
	record.Kind = handoff.ResolutionKind(kind)
	if record.Notes, err = unmarshalStringSlice(notesJSON); err != nil {
		return handoff.Resolution{}, err
	}
	if record.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAtRaw); err != nil {
		return handoff.Resolution{}, err
	}
	return record, nil
}

func (r *handoffRepo) GetLaunch(attemptID string) (handoff.Launch, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Launch{}, err
	}
	row := r.q.QueryRow(`
SELECT attempt_id, handoff_id, task_id, target_worker, status, launch_id, payload_hash,
	requested_at, started_at, ended_at, command, args_json, exit_code, summary,
	error_message, output_artifact_ref, created_at, updated_at
FROM handoff_launches
WHERE attempt_id = ?
`, attemptID)
	return scanHandoffLaunch(row)
}

func (r *handoffRepo) LatestLaunchByHandoff(handoffID string) (handoff.Launch, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Launch{}, err
	}
	row := r.q.QueryRow(`
SELECT attempt_id, handoff_id, task_id, target_worker, status, launch_id, payload_hash,
	requested_at, started_at, ended_at, command, args_json, exit_code, summary,
	error_message, output_artifact_ref, created_at, updated_at
FROM handoff_launches
WHERE handoff_id = ?
ORDER BY requested_at DESC, attempt_id DESC
LIMIT 1
`, handoffID)
	return scanHandoffLaunch(row)
}

func (r *handoffRepo) UpdateLaunch(launch handoff.Launch) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	argsJSON, err := marshalStringSlice(launch.Args)
	if err != nil {
		return err
	}
	res, err := r.q.Exec(`
UPDATE handoff_launches SET
	status=?, launch_id=?, payload_hash=?, requested_at=?, started_at=?, ended_at=?, command=?,
	args_json=?, exit_code=?, summary=?, error_message=?, output_artifact_ref=?, updated_at=?
WHERE attempt_id = ?
`,
		string(launch.Status),
		launch.LaunchID,
		launch.PayloadHash,
		launch.RequestedAt.Format(sqliteTimestampLayout),
		nilIfZeroTime(launch.StartedAt),
		nilIfZeroTime(launch.EndedAt),
		launch.Command,
		argsJSON,
		launch.ExitCode,
		launch.Summary,
		launch.ErrorMessage,
		launch.OutputArtifactRef,
		launch.UpdatedAt.Format(sqliteTimestampLayout),
		launch.AttemptID,
	)
	if err != nil {
		return fmt.Errorf("update handoff launch: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *handoffRepo) LatestAcknowledgment(handoffID string) (handoff.Acknowledgment, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.Acknowledgment{}, err
	}
	row := r.q.QueryRow(`
SELECT ack_id, handoff_id, launch_id, task_id, target_worker, status, summary, unknowns_json, created_at
FROM handoff_acknowledgments
WHERE handoff_id = ?
ORDER BY created_at DESC, ack_id DESC
LIMIT 1
`, handoffID)
	var (
		ackID        string
		persistedHID string
		launchID     string
		taskID       string
		targetWorker string
		status       string
		summary      string
		unknownsJSON string
		createdAtRaw string
	)
	if err := row.Scan(&ackID, &persistedHID, &launchID, &taskID, &targetWorker, &status, &summary, &unknownsJSON, &createdAtRaw); err != nil {
		return handoff.Acknowledgment{}, err
	}
	createdAt, err := time.Parse(sqliteTimestampLayout, createdAtRaw)
	if err != nil {
		return handoff.Acknowledgment{}, err
	}
	unknowns, err := unmarshalStringSlice(unknownsJSON)
	if err != nil {
		return handoff.Acknowledgment{}, err
	}
	return handoff.Acknowledgment{
		Version:      1,
		AckID:        ackID,
		HandoffID:    persistedHID,
		LaunchID:     launchID,
		TaskID:       common.TaskID(taskID),
		TargetWorker: run.WorkerKind(targetWorker),
		Status:       handoff.AcknowledgmentStatus(status),
		Summary:      summary,
		Unknowns:     unknowns,
		CreatedAt:    createdAt,
	}, nil
}

func (r *handoffRepo) SaveFollowThrough(record handoff.FollowThrough) error {
	if err := ensureHandoffTables(r.q); err != nil {
		return err
	}
	notesJSON, err := marshalStringSlice(record.Notes)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO handoff_follow_throughs(
	record_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?)
`,
		record.RecordID,
		record.HandoffID,
		record.LaunchAttemptID,
		record.LaunchID,
		string(record.TaskID),
		string(record.TargetWorker),
		string(record.Kind),
		record.Summary,
		notesJSON,
		record.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert handoff follow-through: %w", err)
	}
	return nil
}

func (r *handoffRepo) LatestFollowThrough(handoffID string) (handoff.FollowThrough, error) {
	if err := ensureHandoffTables(r.q); err != nil {
		return handoff.FollowThrough{}, err
	}
	row := r.q.QueryRow(`
SELECT record_id, handoff_id, launch_attempt_id, launch_id, task_id, target_worker, kind, summary, notes_json, created_at
FROM handoff_follow_throughs
WHERE handoff_id = ?
ORDER BY created_at DESC, record_id DESC
LIMIT 1
`, handoffID)
	var (
		recordID        string
		persistedHID    string
		launchAttemptID string
		launchID        string
		taskID          string
		targetWorker    string
		kind            string
		summary         string
		notesJSON       string
		createdAtRaw    string
	)
	if err := row.Scan(&recordID, &persistedHID, &launchAttemptID, &launchID, &taskID, &targetWorker, &kind, &summary, &notesJSON, &createdAtRaw); err != nil {
		return handoff.FollowThrough{}, err
	}
	createdAt, err := time.Parse(sqliteTimestampLayout, createdAtRaw)
	if err != nil {
		return handoff.FollowThrough{}, err
	}
	notes, err := unmarshalStringSlice(notesJSON)
	if err != nil {
		return handoff.FollowThrough{}, err
	}
	return handoff.FollowThrough{
		Version:         1,
		RecordID:        recordID,
		HandoffID:       persistedHID,
		LaunchAttemptID: launchAttemptID,
		LaunchID:        launchID,
		TaskID:          common.TaskID(taskID),
		TargetWorker:    run.WorkerKind(targetWorker),
		Kind:            handoff.FollowThroughKind(kind),
		Summary:         summary,
		Notes:           notes,
		CreatedAt:       createdAt,
	}, nil
}

func scanHandoffPacket(row *sql.Row) (handoff.Packet, error) {
	var packetJSON string
	if err := row.Scan(&packetJSON); err != nil {
		return handoff.Packet{}, err
	}
	var packet handoff.Packet
	if err := json.Unmarshal([]byte(packetJSON), &packet); err != nil {
		return handoff.Packet{}, err
	}
	return packet, nil
}

func scanHandoffLaunch(row *sql.Row) (handoff.Launch, error) {
	var (
		attemptID         string
		handoffID         string
		taskID            string
		targetWorker      string
		status            string
		launchID          string
		payloadHash       string
		requestedAtRaw    string
		startedAtRaw      sql.NullString
		endedAtRaw        sql.NullString
		command           string
		argsJSON          string
		exitCode          sql.NullInt64
		summary           string
		errorMessage      string
		outputArtifactRef string
		createdAtRaw      string
		updatedAtRaw      string
	)
	if err := row.Scan(
		&attemptID,
		&handoffID,
		&taskID,
		&targetWorker,
		&status,
		&launchID,
		&payloadHash,
		&requestedAtRaw,
		&startedAtRaw,
		&endedAtRaw,
		&command,
		&argsJSON,
		&exitCode,
		&summary,
		&errorMessage,
		&outputArtifactRef,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return handoff.Launch{}, err
	}
	requestedAt, err := time.Parse(sqliteTimestampLayout, requestedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	createdAt, err := time.Parse(sqliteTimestampLayout, createdAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	updatedAt, err := time.Parse(sqliteTimestampLayout, updatedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	startedAt, err := parseNullableTime(startedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	endedAt, err := parseNullableTime(endedAtRaw)
	if err != nil {
		return handoff.Launch{}, err
	}
	args, err := unmarshalStringSlice(argsJSON)
	if err != nil {
		return handoff.Launch{}, err
	}
	var exitCodePtr *int
	if exitCode.Valid {
		value := int(exitCode.Int64)
		exitCodePtr = &value
	}
	return handoff.Launch{
		Version:           1,
		AttemptID:         attemptID,
		HandoffID:         handoffID,
		TaskID:            common.TaskID(taskID),
		TargetWorker:      run.WorkerKind(targetWorker),
		Status:            handoff.LaunchStatus(status),
		LaunchID:          launchID,
		PayloadHash:       payloadHash,
		RequestedAt:       requestedAt,
		StartedAt:         startedAt,
		EndedAt:           endedAt,
		Command:           command,
		Args:              args,
		ExitCode:          exitCodePtr,
		Summary:           summary,
		ErrorMessage:      errorMessage,
		OutputArtifactRef: outputArtifactRef,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}, nil
}
