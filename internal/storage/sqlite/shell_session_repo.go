package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
)

type shellSessionRepo struct{ q queryable }

func ensureShellSessionSchema(db *sql.DB) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS shell_sessions (
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	worker_preference TEXT NOT NULL,
	resolved_worker TEXT NOT NULL,
	worker_session_id TEXT NOT NULL,
	worker_session_id_source TEXT NOT NULL,
	attach_capability TEXT NOT NULL,
	host_mode TEXT NOT NULL,
	host_state TEXT NOT NULL,
	started_at TEXT NOT NULL,
	last_updated_at TEXT NOT NULL,
	active INTEGER NOT NULL,
	note TEXT NOT NULL,
	PRIMARY KEY (task_id, session_id)
);
CREATE INDEX IF NOT EXISTS idx_shell_sessions_task_updated
	ON shell_sessions(task_id, last_updated_at DESC);
CREATE TABLE IF NOT EXISTS shell_session_events (
	event_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	host_mode TEXT NOT NULL,
	host_state TEXT NOT NULL,
	worker_session_id TEXT NOT NULL,
	worker_session_id_source TEXT NOT NULL,
	attach_capability TEXT NOT NULL,
	active INTEGER NOT NULL,
	input_live INTEGER NOT NULL,
	exit_code INTEGER,
	pane_width INTEGER NOT NULL,
	pane_height INTEGER NOT NULL,
	note TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shell_session_events_task_created
	ON shell_session_events(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shell_session_events_task_session_created
	ON shell_session_events(task_id, session_id, created_at DESC);
CREATE TABLE IF NOT EXISTS shell_session_transcript_chunks (
	chunk_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	sequence_no INTEGER NOT NULL,
	source TEXT NOT NULL,
	content TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shell_session_transcript_chunks_task_session_seq
	ON shell_session_transcript_chunks(task_id, session_id, sequence_no DESC);
CREATE TABLE IF NOT EXISTS shell_session_transcript_meta (
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	retained_chunks INTEGER NOT NULL,
	dropped_chunks INTEGER NOT NULL,
	last_sequence_no INTEGER NOT NULL,
	last_chunk_at TEXT NOT NULL,
	PRIMARY KEY (task_id, session_id)
);
CREATE TABLE IF NOT EXISTS shell_session_transcript_reviews (
	review_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	source_filter TEXT NOT NULL,
	reviewed_up_to_sequence INTEGER NOT NULL,
	summary TEXT NOT NULL,
	transcript_state TEXT NOT NULL,
	retention_limit INTEGER NOT NULL,
	retained_chunks INTEGER NOT NULL,
	dropped_chunks INTEGER NOT NULL,
	oldest_retained_sequence INTEGER NOT NULL,
	newest_retained_sequence INTEGER NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shell_session_transcript_reviews_task_session_created
	ON shell_session_transcript_reviews(task_id, session_id, created_at DESC, review_id DESC);
CREATE INDEX IF NOT EXISTS idx_shell_session_transcript_reviews_task_session_source_created
	ON shell_session_transcript_reviews(task_id, session_id, source_filter, created_at DESC, review_id DESC);
CREATE TABLE IF NOT EXISTS shell_session_transcript_review_gap_acks (
	ack_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	ack_class TEXT NOT NULL,
	review_state TEXT NOT NULL,
	review_scope TEXT NOT NULL,
	reviewed_up_to_sequence INTEGER NOT NULL,
	oldest_unreviewed_sequence INTEGER NOT NULL,
	newest_retained_sequence INTEGER NOT NULL,
	unreviewed_retained_count INTEGER NOT NULL,
	transcript_state TEXT NOT NULL,
	retention_limit INTEGER NOT NULL,
	retained_chunks INTEGER NOT NULL,
	dropped_chunks INTEGER NOT NULL,
	action_context TEXT NOT NULL,
	summary TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_shell_session_transcript_review_gap_acks_task_session_created
	ON shell_session_transcript_review_gap_acks(task_id, session_id, created_at DESC, ack_id DESC);
CREATE INDEX IF NOT EXISTS idx_shell_session_transcript_review_gap_acks_task_created
	ON shell_session_transcript_review_gap_acks(task_id, created_at DESC, ack_id DESC);
`
	if _, err := db.Exec(ddl); err != nil {
		return fmt.Errorf("apply shell session schema: %w", err)
	}
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "worker_session_id", DDL: "ALTER TABLE shell_sessions ADD COLUMN worker_session_id TEXT NOT NULL DEFAULT ''"},
		{Name: "worker_session_id_source", DDL: "ALTER TABLE shell_sessions ADD COLUMN worker_session_id_source TEXT NOT NULL DEFAULT 'unknown'"},
		{Name: "attach_capability", DDL: "ALTER TABLE shell_sessions ADD COLUMN attach_capability TEXT NOT NULL DEFAULT 'none'"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "shell_sessions", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add shell_sessions column %s: %w", item.Name, err)
		}
	}
	type eventColDef struct {
		Name string
		DDL  string
	}
	eventCols := []eventColDef{
		{Name: "worker_session_id", DDL: "ALTER TABLE shell_session_events ADD COLUMN worker_session_id TEXT NOT NULL DEFAULT ''"},
		{Name: "worker_session_id_source", DDL: "ALTER TABLE shell_session_events ADD COLUMN worker_session_id_source TEXT NOT NULL DEFAULT 'unknown'"},
		{Name: "attach_capability", DDL: "ALTER TABLE shell_session_events ADD COLUMN attach_capability TEXT NOT NULL DEFAULT 'none'"},
		{Name: "active", DDL: "ALTER TABLE shell_session_events ADD COLUMN active INTEGER NOT NULL DEFAULT 0"},
		{Name: "input_live", DDL: "ALTER TABLE shell_session_events ADD COLUMN input_live INTEGER NOT NULL DEFAULT 0"},
		{Name: "exit_code", DDL: "ALTER TABLE shell_session_events ADD COLUMN exit_code INTEGER"},
		{Name: "pane_width", DDL: "ALTER TABLE shell_session_events ADD COLUMN pane_width INTEGER NOT NULL DEFAULT 0"},
		{Name: "pane_height", DDL: "ALTER TABLE shell_session_events ADD COLUMN pane_height INTEGER NOT NULL DEFAULT 0"},
		{Name: "note", DDL: "ALTER TABLE shell_session_events ADD COLUMN note TEXT NOT NULL DEFAULT ''"},
	}
	for _, item := range eventCols {
		ok, err := hasColumn(db, "shell_session_events", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add shell_session_events column %s: %w", item.Name, err)
		}
	}
	return nil
}

func (r *shellSessionRepo) Upsert(record shellsession.Record) error {
	record.WorkerPreference = strings.TrimSpace(record.WorkerPreference)
	record.ResolvedWorker = strings.TrimSpace(record.ResolvedWorker)
	record.WorkerSessionID = strings.TrimSpace(record.WorkerSessionID)
	record.WorkerSessionIDSource = normalizeWorkerSessionIDSource(record.WorkerSessionIDSource, record.WorkerSessionID)
	if record.AttachCapability == "" {
		record.AttachCapability = shellsession.AttachCapabilityNone
	}
	record.HostMode = strings.TrimSpace(record.HostMode)
	record.HostState = strings.TrimSpace(record.HostState)
	record.Note = strings.TrimSpace(record.Note)
	if record.StartedAt.IsZero() {
		record.StartedAt = record.LastUpdatedAt
	}

	_, err := r.q.Exec(`
INSERT INTO shell_sessions(
	task_id, session_id, worker_preference, resolved_worker, worker_session_id, worker_session_id_source, attach_capability, host_mode, host_state,
	started_at, last_updated_at, active, note
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(task_id, session_id) DO UPDATE SET
	worker_preference = CASE
		WHEN excluded.worker_preference <> '' THEN excluded.worker_preference
		ELSE shell_sessions.worker_preference
	END,
	resolved_worker = CASE
		WHEN excluded.resolved_worker <> '' THEN excluded.resolved_worker
		ELSE shell_sessions.resolved_worker
	END,
	worker_session_id = CASE
		WHEN excluded.worker_session_id <> '' THEN excluded.worker_session_id
		ELSE shell_sessions.worker_session_id
	END,
	worker_session_id_source = CASE
		WHEN excluded.worker_session_id <> '' THEN excluded.worker_session_id_source
		ELSE shell_sessions.worker_session_id_source
	END,
	attach_capability = CASE
		WHEN excluded.attach_capability <> '' THEN excluded.attach_capability
		ELSE shell_sessions.attach_capability
	END,
	host_mode = excluded.host_mode,
	host_state = excluded.host_state,
	last_updated_at = excluded.last_updated_at,
	active = excluded.active,
	note = excluded.note
`,
		string(record.TaskID),
		record.SessionID,
		record.WorkerPreference,
		record.ResolvedWorker,
		record.WorkerSessionID,
		string(record.WorkerSessionIDSource),
		string(record.AttachCapability),
		record.HostMode,
		record.HostState,
		record.StartedAt.Format(sqliteTimestampLayout),
		record.LastUpdatedAt.Format(sqliteTimestampLayout),
		boolToInt(record.Active),
		record.Note,
	)
	if err != nil {
		return fmt.Errorf("upsert shell session: %w", err)
	}
	return nil
}

func (r *shellSessionRepo) ListByTask(taskID common.TaskID) ([]shellsession.Record, error) {
	rows, err := r.q.Query(`
SELECT task_id, session_id, worker_preference, resolved_worker, worker_session_id, worker_session_id_source, attach_capability,
	host_mode, host_state, started_at, last_updated_at, active, note
FROM shell_sessions
WHERE task_id = ?
ORDER BY last_updated_at DESC, session_id ASC
`, string(taskID))
	if err != nil {
		return nil, fmt.Errorf("query shell sessions by task: %w", err)
	}
	defer rows.Close()

	var records []shellsession.Record
	for rows.Next() {
		record, err := scanShellSession(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shell sessions: %w", err)
	}
	return records, nil
}

func (r *shellSessionRepo) AppendEvent(event shellsession.Event) error {
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.HostMode = strings.TrimSpace(event.HostMode)
	event.HostState = strings.TrimSpace(event.HostState)
	event.WorkerSessionID = strings.TrimSpace(event.WorkerSessionID)
	event.WorkerSessionIDSource = normalizeWorkerSessionIDSource(event.WorkerSessionIDSource, event.WorkerSessionID)
	if event.AttachCapability == "" {
		event.AttachCapability = shellsession.AttachCapabilityNone
	}
	event.Note = strings.TrimSpace(event.Note)
	if event.EventID == "" {
		event.EventID = common.EventID(fmt.Sprintf("ssev_%d", time.Now().UTC().UnixNano()))
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	_, err := r.q.Exec(`
INSERT INTO shell_session_events(
	event_id, task_id, session_id, kind, host_mode, host_state, worker_session_id, worker_session_id_source, attach_capability,
	active, input_live, exit_code, pane_width, pane_height, note, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(event.EventID),
		string(event.TaskID),
		event.SessionID,
		string(event.Kind),
		event.HostMode,
		event.HostState,
		event.WorkerSessionID,
		string(event.WorkerSessionIDSource),
		string(event.AttachCapability),
		boolToInt(event.Active),
		boolToInt(event.InputLive),
		nilIfInt(event.ExitCode),
		event.PaneWidth,
		event.PaneHeight,
		event.Note,
		event.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert shell session event: %w", err)
	}
	return nil
}

func (r *shellSessionRepo) ListEvents(taskID common.TaskID, sessionID string, limit int) ([]shellsession.Event, error) {
	if limit <= 0 {
		limit = 20
	}
	sessionID = strings.TrimSpace(sessionID)
	var (
		rows *sql.Rows
		err  error
	)
	if sessionID == "" {
		rows, err = r.q.Query(`
SELECT event_id, task_id, session_id, kind, host_mode, host_state, worker_session_id, worker_session_id_source, attach_capability,
	active, input_live, exit_code, pane_width, pane_height, note, created_at
FROM shell_session_events
WHERE task_id = ?
ORDER BY created_at DESC, event_id DESC
LIMIT ?
`, string(taskID), limit)
	} else {
		rows, err = r.q.Query(`
SELECT event_id, task_id, session_id, kind, host_mode, host_state, worker_session_id, worker_session_id_source, attach_capability,
	active, input_live, exit_code, pane_width, pane_height, note, created_at
FROM shell_session_events
WHERE task_id = ? AND session_id = ?
ORDER BY created_at DESC, event_id DESC
LIMIT ?
`, string(taskID), sessionID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query shell session events: %w", err)
	}
	defer rows.Close()

	var events []shellsession.Event
	for rows.Next() {
		event, scanErr := scanShellSessionEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shell session events: %w", err)
	}
	return events, nil
}

func scanShellSession(scanner interface {
	Scan(dest ...any) error
}) (shellsession.Record, error) {
	var (
		taskID                string
		sessionID             string
		workerPreference      string
		resolvedWorker        string
		workerSessionID       string
		workerSessionIDSource string
		attachCapability      string
		hostMode              string
		hostState             string
		startedAt             string
		lastUpdatedAt         string
		active                int
		note                  string
	)
	if err := scanner.Scan(
		&taskID,
		&sessionID,
		&workerPreference,
		&resolvedWorker,
		&workerSessionID,
		&workerSessionIDSource,
		&attachCapability,
		&hostMode,
		&hostState,
		&startedAt,
		&lastUpdatedAt,
		&active,
		&note,
	); err != nil {
		return shellsession.Record{}, err
	}

	started, err := time.Parse(sqliteTimestampLayout, startedAt)
	if err != nil {
		return shellsession.Record{}, fmt.Errorf("parse shell session started_at: %w", err)
	}
	updated, err := time.Parse(sqliteTimestampLayout, lastUpdatedAt)
	if err != nil {
		return shellsession.Record{}, fmt.Errorf("parse shell session last_updated_at: %w", err)
	}

	return shellsession.Record{
		TaskID:                common.TaskID(taskID),
		SessionID:             sessionID,
		WorkerPreference:      workerPreference,
		ResolvedWorker:        resolvedWorker,
		WorkerSessionID:       workerSessionID,
		WorkerSessionIDSource: normalizeWorkerSessionIDSource(shellsession.WorkerSessionIDSource(workerSessionIDSource), workerSessionID),
		AttachCapability:      shellsession.AttachCapability(attachCapability),
		HostMode:              hostMode,
		HostState:             hostState,
		StartedAt:             started,
		LastUpdatedAt:         updated,
		Active:                active == 1,
		Note:                  note,
	}, nil
}

func scanShellSessionEvent(scanner interface {
	Scan(dest ...any) error
}) (shellsession.Event, error) {
	var (
		eventID               string
		taskID                string
		sessionID             string
		kind                  string
		hostMode              string
		hostState             string
		workerSessionID       string
		workerSessionIDSource string
		attachCapability      string
		active                int
		inputLive             int
		exitCode              sql.NullInt64
		paneWidth             int
		paneHeight            int
		note                  string
		createdAt             string
	)
	if err := scanner.Scan(
		&eventID,
		&taskID,
		&sessionID,
		&kind,
		&hostMode,
		&hostState,
		&workerSessionID,
		&workerSessionIDSource,
		&attachCapability,
		&active,
		&inputLive,
		&exitCode,
		&paneWidth,
		&paneHeight,
		&note,
		&createdAt,
	); err != nil {
		return shellsession.Event{}, err
	}
	created, err := time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return shellsession.Event{}, fmt.Errorf("parse shell session event created_at: %w", err)
	}

	var exitCodePtr *int
	if exitCode.Valid {
		code := int(exitCode.Int64)
		exitCodePtr = &code
	}

	return shellsession.Event{
		EventID:               common.EventID(eventID),
		TaskID:                common.TaskID(taskID),
		SessionID:             sessionID,
		Kind:                  shellsession.EventKind(kind),
		HostMode:              hostMode,
		HostState:             hostState,
		WorkerSessionID:       workerSessionID,
		WorkerSessionIDSource: normalizeWorkerSessionIDSource(shellsession.WorkerSessionIDSource(workerSessionIDSource), workerSessionID),
		AttachCapability:      shellsession.AttachCapability(attachCapability),
		Active:                active == 1,
		InputLive:             inputLive == 1,
		ExitCode:              exitCodePtr,
		PaneWidth:             paneWidth,
		PaneHeight:            paneHeight,
		Note:                  note,
		CreatedAt:             created,
	}, nil
}

func normalizeWorkerSessionIDSource(source shellsession.WorkerSessionIDSource, workerSessionID string) shellsession.WorkerSessionIDSource {
	if strings.TrimSpace(workerSessionID) == "" {
		return shellsession.WorkerSessionIDSourceNone
	}
	switch source {
	case shellsession.WorkerSessionIDSourceAuthoritative, shellsession.WorkerSessionIDSourceHeuristic:
		return source
	case shellsession.WorkerSessionIDSourceUnknown:
		return source
	default:
		return shellsession.WorkerSessionIDSourceUnknown
	}
}

func (r *shellSessionRepo) AppendTranscript(taskID common.TaskID, sessionID string, chunks []shellsession.TranscriptChunk, retention int) (shellsession.TranscriptSummary, error) {
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return shellsession.TranscriptSummary{}, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return shellsession.TranscriptSummary{}, fmt.Errorf("session id is required")
	}
	if retention <= 0 {
		retention = shellsession.DefaultTranscriptRetentionChunks
	}
	summary, err := r.TranscriptSummary(taskID, sessionID, retention)
	if err != nil {
		return shellsession.TranscriptSummary{}, err
	}
	if len(chunks) == 0 {
		return summary, nil
	}
	lastSequence := summary.LastSequenceNo
	lastChunkAt := summary.LastChunkAt
	inserted := 0
	for _, chunk := range chunks {
		content := normalizeTranscriptChunkContent(chunk.Content)
		if content == "" {
			continue
		}
		createdAt := chunk.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		lastSequence++
		chunkID := common.EventID(strings.TrimSpace(string(chunk.ChunkID)))
		if chunkID == "" {
			chunkID = common.EventID(fmt.Sprintf("sst_%d_%d", time.Now().UTC().UnixNano(), lastSequence))
		}
		source := normalizeTranscriptSource(chunk.Source)
		_, err := r.q.Exec(`
INSERT INTO shell_session_transcript_chunks(
	chunk_id, task_id, session_id, sequence_no, source, content, created_at
) VALUES(?,?,?,?,?,?,?)
`,
			string(chunkID),
			string(taskID),
			sessionID,
			lastSequence,
			string(source),
			content,
			createdAt.Format(sqliteTimestampLayout),
		)
		if err != nil {
			return shellsession.TranscriptSummary{}, fmt.Errorf("insert shell session transcript chunk: %w", err)
		}
		inserted++
		if createdAt.After(lastChunkAt) {
			lastChunkAt = createdAt
		}
	}
	if inserted == 0 {
		return summary, nil
	}
	pruneResult, err := r.q.Exec(`
DELETE FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ? AND chunk_id IN (
	SELECT chunk_id
	FROM shell_session_transcript_chunks
	WHERE task_id = ? AND session_id = ?
	ORDER BY sequence_no DESC
	LIMIT -1 OFFSET ?
)
`,
		string(taskID),
		sessionID,
		string(taskID),
		sessionID,
		retention,
	)
	if err != nil {
		return shellsession.TranscriptSummary{}, fmt.Errorf("prune shell session transcript chunks: %w", err)
	}
	droppedNow := int64(0)
	if pruneResult != nil {
		if affected, rowsErr := pruneResult.RowsAffected(); rowsErr == nil {
			droppedNow = affected
		}
	}
	retained := 0
	if err := r.q.QueryRow(`
SELECT COUNT(*)
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?
`, string(taskID), sessionID).Scan(&retained); err != nil {
		return shellsession.TranscriptSummary{}, fmt.Errorf("count retained shell transcript chunks: %w", err)
	}
	droppedTotal := summary.DroppedChunks + int(droppedNow)
	lastChunkAtText := ""
	if !lastChunkAt.IsZero() {
		lastChunkAtText = lastChunkAt.Format(sqliteTimestampLayout)
	}
	_, err = r.q.Exec(`
INSERT INTO shell_session_transcript_meta(
	task_id, session_id, retained_chunks, dropped_chunks, last_sequence_no, last_chunk_at
) VALUES(?,?,?,?,?,?)
ON CONFLICT(task_id, session_id) DO UPDATE SET
	retained_chunks = excluded.retained_chunks,
	dropped_chunks = excluded.dropped_chunks,
	last_sequence_no = excluded.last_sequence_no,
	last_chunk_at = excluded.last_chunk_at
`,
		string(taskID),
		sessionID,
		retained,
		droppedTotal,
		lastSequence,
		lastChunkAtText,
	)
	if err != nil {
		return shellsession.TranscriptSummary{}, fmt.Errorf("upsert shell transcript metadata: %w", err)
	}
	summary, err = r.TranscriptSummary(taskID, sessionID, retention)
	if err != nil {
		return shellsession.TranscriptSummary{}, err
	}
	summary.DroppedChunks = droppedTotal
	return summary, nil
}

func (r *shellSessionRepo) ListTranscript(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptChunk, error) {
	chunks, _, err := r.ListTranscriptPage(taskID, sessionID, 0, limit, "")
	return chunks, err
}

func (r *shellSessionRepo) ListTranscriptPage(taskID common.TaskID, sessionID string, beforeSequence int64, limit int, source shellsession.TranscriptSource) ([]shellsession.TranscriptChunk, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return nil, false, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return nil, false, fmt.Errorf("session id is required")
	}
	if limit <= 0 {
		limit = 40
	}
	source, sourceFilter := normalizeTranscriptSourceFilter(source)
	if sourceFilter == invalidTranscriptSource {
		return nil, false, fmt.Errorf("unsupported transcript source filter %q", source)
	}

	query := `
SELECT chunk_id, task_id, session_id, sequence_no, source, content, created_at
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?`
	args := []any{string(taskID), sessionID}
	if beforeSequence > 0 {
		query += ` AND sequence_no < ?`
		args = append(args, beforeSequence)
	}
	if sourceFilter == exactTranscriptSource {
		query += ` AND source = ?`
		args = append(args, string(source))
	}
	query += `
ORDER BY sequence_no DESC
LIMIT ?`
	args = append(args, limit+1)

	rows, err := r.q.Query(query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("query shell transcript chunks: %w", err)
	}
	defer rows.Close()

	out := make([]shellsession.TranscriptChunk, 0, limit+1)
	for rows.Next() {
		chunk, scanErr := scanShellSessionTranscriptChunk(rows)
		if scanErr != nil {
			return nil, false, scanErr
		}
		out = append(out, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate shell transcript chunks: %w", err)
	}

	hasMoreOlder := false
	if len(out) > limit {
		hasMoreOlder = true
		out = out[:limit]
	}
	reverseTranscriptChunks(out)
	return out, hasMoreOlder, nil
}

func (r *shellSessionRepo) TranscriptSummary(taskID common.TaskID, sessionID string, retention int) (shellsession.TranscriptSummary, error) {
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return shellsession.TranscriptSummary{}, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return shellsession.TranscriptSummary{}, fmt.Errorf("session id is required")
	}
	if retention <= 0 {
		retention = shellsession.DefaultTranscriptRetentionChunks
	}
	summary := shellsession.TranscriptSummary{
		TaskID:         taskID,
		SessionID:      sessionID,
		RetentionLimit: retention,
	}
	var lastChunkAtText string
	err := r.q.QueryRow(`
SELECT retained_chunks, dropped_chunks, last_sequence_no, last_chunk_at
FROM shell_session_transcript_meta
WHERE task_id = ? AND session_id = ?
`, string(taskID), sessionID).Scan(&summary.RetainedChunks, &summary.DroppedChunks, &summary.LastSequenceNo, &lastChunkAtText)
	switch {
	case err == nil:
		if strings.TrimSpace(lastChunkAtText) != "" {
			if parsed, parseErr := time.Parse(sqliteTimestampLayout, lastChunkAtText); parseErr == nil {
				summary.LastChunkAt = parsed
			}
		}
		if err := r.populateTranscriptSummaryBoundsAndSources(&summary); err != nil {
			return shellsession.TranscriptSummary{}, err
		}
		return summary, nil
	case err != sql.ErrNoRows:
		return shellsession.TranscriptSummary{}, fmt.Errorf("query shell transcript summary: %w", err)
	}

	if err := r.q.QueryRow(`
SELECT COUNT(*)
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?
`, string(taskID), sessionID).Scan(&summary.RetainedChunks); err != nil {
		return shellsession.TranscriptSummary{}, fmt.Errorf("count shell transcript chunks: %w", err)
	}
	err = r.q.QueryRow(`
SELECT sequence_no, created_at
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?
ORDER BY sequence_no DESC
LIMIT 1
`, string(taskID), sessionID).Scan(&summary.LastSequenceNo, &lastChunkAtText)
	if err != nil {
		if err == sql.ErrNoRows {
			return summary, nil
		}
		return shellsession.TranscriptSummary{}, fmt.Errorf("query latest shell transcript chunk: %w", err)
	}
	if strings.TrimSpace(lastChunkAtText) != "" {
		if parsed, parseErr := time.Parse(sqliteTimestampLayout, lastChunkAtText); parseErr == nil {
			summary.LastChunkAt = parsed
		}
	}
	if err := r.populateTranscriptSummaryBoundsAndSources(&summary); err != nil {
		return shellsession.TranscriptSummary{}, err
	}
	return summary, nil
}

func (r *shellSessionRepo) AppendTranscriptReview(review shellsession.TranscriptReview) (shellsession.TranscriptReview, error) {
	review.TaskID = common.TaskID(strings.TrimSpace(string(review.TaskID)))
	review.SessionID = strings.TrimSpace(review.SessionID)
	review.SourceFilter = shellsession.TranscriptSource(strings.TrimSpace(string(review.SourceFilter)))
	review.Summary = strings.TrimSpace(review.Summary)
	if review.TaskID == "" {
		return shellsession.TranscriptReview{}, fmt.Errorf("task id is required")
	}
	if review.SessionID == "" {
		return shellsession.TranscriptReview{}, fmt.Errorf("session id is required")
	}
	if review.ReviewedUpToSequence <= 0 {
		return shellsession.TranscriptReview{}, fmt.Errorf("reviewed_up_to_sequence must be greater than zero")
	}
	if review.ReviewID == "" {
		review.ReviewID = common.EventID(fmt.Sprintf("srev_%d", time.Now().UTC().UnixNano()))
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = time.Now().UTC()
	}

	_, err := r.q.Exec(`
INSERT INTO shell_session_transcript_reviews(
	review_id, task_id, session_id, source_filter, reviewed_up_to_sequence, summary, transcript_state,
	retention_limit, retained_chunks, dropped_chunks, oldest_retained_sequence, newest_retained_sequence, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(review.ReviewID),
		string(review.TaskID),
		review.SessionID,
		string(review.SourceFilter),
		review.ReviewedUpToSequence,
		review.Summary,
		string(review.TranscriptState),
		review.RetentionLimit,
		review.RetainedChunks,
		review.DroppedChunks,
		review.OldestRetainedSequence,
		review.NewestRetainedSequence,
		review.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return shellsession.TranscriptReview{}, fmt.Errorf("insert shell transcript review: %w", err)
	}
	return review, nil
}

func (r *shellSessionRepo) ListTranscriptReviews(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource, limit int) ([]shellsession.TranscriptReview, error) {
	taskID = common.TaskID(strings.TrimSpace(string(taskID)))
	sessionID = strings.TrimSpace(sessionID)
	source = shellsession.TranscriptSource(strings.TrimSpace(string(source)))
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT review_id, task_id, session_id, source_filter, reviewed_up_to_sequence, summary, transcript_state,
	retention_limit, retained_chunks, dropped_chunks, oldest_retained_sequence, newest_retained_sequence, created_at
FROM shell_session_transcript_reviews
WHERE task_id = ? AND session_id = ? AND source_filter = ?
ORDER BY created_at DESC, review_id DESC
LIMIT ?
`, string(taskID), sessionID, string(source), limit)
	if err != nil {
		return nil, fmt.Errorf("query shell transcript reviews: %w", err)
	}
	defer rows.Close()

	out := make([]shellsession.TranscriptReview, 0, limit)
	for rows.Next() {
		review, scanErr := scanShellSessionTranscriptReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shell transcript reviews: %w", err)
	}
	return out, nil
}

func (r *shellSessionRepo) ListTranscriptReviewsAnyScope(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReview, error) {
	taskID = common.TaskID(strings.TrimSpace(string(taskID)))
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT review_id, task_id, session_id, source_filter, reviewed_up_to_sequence, summary, transcript_state,
	retention_limit, retained_chunks, dropped_chunks, oldest_retained_sequence, newest_retained_sequence, created_at
FROM shell_session_transcript_reviews
WHERE task_id = ? AND session_id = ?
ORDER BY created_at DESC, review_id DESC
LIMIT ?
`, string(taskID), sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("query shell transcript reviews (any scope): %w", err)
	}
	defer rows.Close()

	out := make([]shellsession.TranscriptReview, 0, limit)
	for rows.Next() {
		review, scanErr := scanShellSessionTranscriptReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shell transcript reviews (any scope): %w", err)
	}
	return out, nil
}

func (r *shellSessionRepo) LatestTranscriptReview(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource) (*shellsession.TranscriptReview, error) {
	reviews, err := r.ListTranscriptReviews(taskID, sessionID, source, 1)
	if err != nil {
		return nil, err
	}
	if len(reviews) == 0 {
		return nil, nil
	}
	review := reviews[0]
	return &review, nil
}

func (r *shellSessionRepo) LatestTranscriptReviewAnyScope(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReview, error) {
	reviews, err := r.ListTranscriptReviewsAnyScope(taskID, sessionID, 1)
	if err != nil {
		return nil, err
	}
	if len(reviews) == 0 {
		return nil, nil
	}
	review := reviews[0]
	return &review, nil
}

func (r *shellSessionRepo) AppendTranscriptReviewGapAcknowledgment(record shellsession.TranscriptReviewGapAcknowledgment) (shellsession.TranscriptReviewGapAcknowledgment, error) {
	record.TaskID = common.TaskID(strings.TrimSpace(string(record.TaskID)))
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.Summary = strings.TrimSpace(record.Summary)
	record.ActionContext = strings.TrimSpace(record.ActionContext)
	record.ReviewState = strings.TrimSpace(record.ReviewState)
	record.ReviewScope = shellsession.TranscriptSource(strings.TrimSpace(string(record.ReviewScope)))
	if record.TaskID == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("task id is required")
	}
	if record.SessionID == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("session id is required")
	}
	if record.Class == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("acknowledgment class is required")
	}
	if record.AcknowledgmentID == "" {
		record.AcknowledgmentID = common.EventID(fmt.Sprintf("sack_%d", time.Now().UTC().UnixNano()))
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	_, err := r.q.Exec(`
INSERT INTO shell_session_transcript_review_gap_acks(
	ack_id, task_id, session_id, ack_class, review_state, review_scope,
	reviewed_up_to_sequence, oldest_unreviewed_sequence, newest_retained_sequence, unreviewed_retained_count,
	transcript_state, retention_limit, retained_chunks, dropped_chunks,
	action_context, summary, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(record.AcknowledgmentID),
		string(record.TaskID),
		record.SessionID,
		string(record.Class),
		record.ReviewState,
		string(record.ReviewScope),
		record.ReviewedUpToSequence,
		record.OldestUnreviewedSequence,
		record.NewestRetainedSequence,
		record.UnreviewedRetainedCount,
		string(record.TranscriptState),
		record.RetentionLimit,
		record.RetainedChunks,
		record.DroppedChunks,
		record.ActionContext,
		record.Summary,
		record.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("insert transcript review-gap acknowledgment: %w", err)
	}
	return record, nil
}

func (r *shellSessionRepo) LatestTranscriptReviewGapAcknowledgment(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReviewGapAcknowledgment, error) {
	records, err := r.ListTranscriptReviewGapAcknowledgments(taskID, sessionID, 1)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	record := records[0]
	return &record, nil
}

func (r *shellSessionRepo) ListTranscriptReviewGapAcknowledgments(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReviewGapAcknowledgment, error) {
	taskID = common.TaskID(strings.TrimSpace(string(taskID)))
	sessionID = strings.TrimSpace(sessionID)
	if taskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	query := `
SELECT ack_id, task_id, session_id, ack_class, review_state, review_scope,
	reviewed_up_to_sequence, oldest_unreviewed_sequence, newest_retained_sequence, unreviewed_retained_count,
	transcript_state, retention_limit, retained_chunks, dropped_chunks,
	action_context, summary, created_at
FROM shell_session_transcript_review_gap_acks
WHERE task_id = ?`
	args := []any{string(taskID)}
	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += `
ORDER BY created_at DESC, ack_id DESC
LIMIT ?`
	args = append(args, limit)
	rows, err := r.q.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query transcript review-gap acknowledgments: %w", err)
	}
	defer rows.Close()

	out := make([]shellsession.TranscriptReviewGapAcknowledgment, 0, limit)
	for rows.Next() {
		record, scanErr := scanShellSessionTranscriptReviewGapAcknowledgment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transcript review-gap acknowledgments: %w", err)
	}
	return out, nil
}

func (r *shellSessionRepo) populateTranscriptSummaryBoundsAndSources(summary *shellsession.TranscriptSummary) error {
	if summary == nil || summary.TaskID == "" || strings.TrimSpace(summary.SessionID) == "" || summary.RetainedChunks <= 0 {
		return nil
	}
	sessionID := strings.TrimSpace(summary.SessionID)
	var (
		oldestSequence int64
		oldestAt       string
	)
	err := r.q.QueryRow(`
SELECT sequence_no, created_at
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?
ORDER BY sequence_no ASC
LIMIT 1
`, string(summary.TaskID), sessionID).Scan(&oldestSequence, &oldestAt)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("query oldest shell transcript chunk: %w", err)
	}
	if err == nil {
		summary.OldestSequenceNo = oldestSequence
		if strings.TrimSpace(oldestAt) != "" {
			if parsed, parseErr := time.Parse(sqliteTimestampLayout, oldestAt); parseErr == nil {
				summary.OldestChunkAt = parsed
			}
		}
	}

	var (
		newestSequence int64
		newestAt       string
	)
	err = r.q.QueryRow(`
SELECT sequence_no, created_at
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?
ORDER BY sequence_no DESC
LIMIT 1
`, string(summary.TaskID), sessionID).Scan(&newestSequence, &newestAt)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("query newest shell transcript chunk: %w", err)
	}
	if err == nil {
		summary.NewestSequenceNo = newestSequence
		summary.LastSequenceNo = newestSequence
		if strings.TrimSpace(newestAt) != "" {
			if parsed, parseErr := time.Parse(sqliteTimestampLayout, newestAt); parseErr == nil {
				summary.NewestChunkAt = parsed
				summary.LastChunkAt = parsed
			}
		}
	}

	rows, err := r.q.Query(`
SELECT source, COUNT(*)
FROM shell_session_transcript_chunks
WHERE task_id = ? AND session_id = ?
GROUP BY source
ORDER BY source ASC
`, string(summary.TaskID), sessionID)
	if err != nil {
		return fmt.Errorf("query shell transcript source counts: %w", err)
	}
	defer rows.Close()

	counts := make([]shellsession.TranscriptSourceCount, 0, 3)
	for rows.Next() {
		var (
			source string
			count  int
		)
		if err := rows.Scan(&source, &count); err != nil {
			return fmt.Errorf("scan shell transcript source count: %w", err)
		}
		counts = append(counts, shellsession.TranscriptSourceCount{
			Source: normalizeTranscriptSource(shellsession.TranscriptSource(source)),
			Chunks: count,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate shell transcript source counts: %w", err)
	}
	summary.SourceCounts = counts
	return nil
}

func scanShellSessionTranscriptChunk(scanner interface {
	Scan(dest ...any) error
}) (shellsession.TranscriptChunk, error) {
	var (
		chunkID    string
		taskID     string
		sessionID  string
		sequenceNo int64
		source     string
		content    string
		createdAt  string
	)
	if err := scanner.Scan(&chunkID, &taskID, &sessionID, &sequenceNo, &source, &content, &createdAt); err != nil {
		return shellsession.TranscriptChunk{}, err
	}
	created, err := time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return shellsession.TranscriptChunk{}, fmt.Errorf("parse shell transcript created_at: %w", err)
	}
	return shellsession.TranscriptChunk{
		ChunkID:    common.EventID(chunkID),
		TaskID:     common.TaskID(taskID),
		SessionID:  sessionID,
		SequenceNo: sequenceNo,
		Source:     normalizeTranscriptSource(shellsession.TranscriptSource(source)),
		Content:    content,
		CreatedAt:  created,
	}, nil
}

func scanShellSessionTranscriptReview(scanner interface {
	Scan(dest ...any) error
}) (shellsession.TranscriptReview, error) {
	var (
		reviewID               string
		taskID                 string
		sessionID              string
		sourceFilter           string
		reviewedUpToSequence   int64
		summary                string
		transcriptState        string
		retentionLimit         int
		retainedChunks         int
		droppedChunks          int
		oldestRetainedSequence int64
		newestRetainedSequence int64
		createdAt              string
	)
	if err := scanner.Scan(
		&reviewID,
		&taskID,
		&sessionID,
		&sourceFilter,
		&reviewedUpToSequence,
		&summary,
		&transcriptState,
		&retentionLimit,
		&retainedChunks,
		&droppedChunks,
		&oldestRetainedSequence,
		&newestRetainedSequence,
		&createdAt,
	); err != nil {
		return shellsession.TranscriptReview{}, err
	}
	parsedCreatedAt, err := time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return shellsession.TranscriptReview{}, fmt.Errorf("parse shell transcript review created_at: %w", err)
	}
	return shellsession.TranscriptReview{
		ReviewID:               common.EventID(reviewID),
		TaskID:                 common.TaskID(taskID),
		SessionID:              sessionID,
		SourceFilter:           shellsession.TranscriptSource(sourceFilter),
		ReviewedUpToSequence:   reviewedUpToSequence,
		Summary:                summary,
		TranscriptState:        shellsession.TranscriptState(transcriptState),
		RetentionLimit:         retentionLimit,
		RetainedChunks:         retainedChunks,
		DroppedChunks:          droppedChunks,
		OldestRetainedSequence: oldestRetainedSequence,
		NewestRetainedSequence: newestRetainedSequence,
		CreatedAt:              parsedCreatedAt,
	}, nil
}

func scanShellSessionTranscriptReviewGapAcknowledgment(scanner interface {
	Scan(dest ...any) error
}) (shellsession.TranscriptReviewGapAcknowledgment, error) {
	var (
		ackID                    string
		taskID                   string
		sessionID                string
		ackClass                 string
		reviewState              string
		reviewScope              string
		reviewedUpToSequence     int64
		oldestUnreviewedSequence int64
		newestRetainedSequence   int64
		unreviewedRetainedCount  int
		transcriptState          string
		retentionLimit           int
		retainedChunks           int
		droppedChunks            int
		actionContext            string
		summary                  string
		createdAt                string
	)
	if err := scanner.Scan(
		&ackID,
		&taskID,
		&sessionID,
		&ackClass,
		&reviewState,
		&reviewScope,
		&reviewedUpToSequence,
		&oldestUnreviewedSequence,
		&newestRetainedSequence,
		&unreviewedRetainedCount,
		&transcriptState,
		&retentionLimit,
		&retainedChunks,
		&droppedChunks,
		&actionContext,
		&summary,
		&createdAt,
	); err != nil {
		return shellsession.TranscriptReviewGapAcknowledgment{}, err
	}
	parsedCreatedAt, err := time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("parse transcript review-gap acknowledgment created_at: %w", err)
	}
	return shellsession.TranscriptReviewGapAcknowledgment{
		AcknowledgmentID:         common.EventID(ackID),
		TaskID:                   common.TaskID(taskID),
		SessionID:                sessionID,
		Class:                    shellsession.TranscriptReviewGapAcknowledgmentClass(ackClass),
		ReviewState:              reviewState,
		ReviewScope:              shellsession.TranscriptSource(reviewScope),
		ReviewedUpToSequence:     reviewedUpToSequence,
		OldestUnreviewedSequence: oldestUnreviewedSequence,
		NewestRetainedSequence:   newestRetainedSequence,
		UnreviewedRetainedCount:  unreviewedRetainedCount,
		TranscriptState:          shellsession.TranscriptState(transcriptState),
		RetentionLimit:           retentionLimit,
		RetainedChunks:           retainedChunks,
		DroppedChunks:            droppedChunks,
		ActionContext:            actionContext,
		Summary:                  summary,
		CreatedAt:                parsedCreatedAt,
	}, nil
}

func normalizeTranscriptSource(source shellsession.TranscriptSource) shellsession.TranscriptSource {
	switch source {
	case shellsession.TranscriptSourceWorkerOutput, shellsession.TranscriptSourceSystemNote, shellsession.TranscriptSourceFallback:
		return source
	default:
		return shellsession.TranscriptSourceWorkerOutput
	}
}

type transcriptSourceFilterState int

const (
	noTranscriptSourceFilter transcriptSourceFilterState = iota
	exactTranscriptSource
	invalidTranscriptSource
)

func normalizeTranscriptSourceFilter(source shellsession.TranscriptSource) (shellsession.TranscriptSource, transcriptSourceFilterState) {
	trimmed := strings.TrimSpace(string(source))
	if trimmed == "" {
		return "", noTranscriptSourceFilter
	}
	switch shellsession.TranscriptSource(trimmed) {
	case shellsession.TranscriptSourceWorkerOutput, shellsession.TranscriptSourceSystemNote, shellsession.TranscriptSourceFallback:
		return shellsession.TranscriptSource(trimmed), exactTranscriptSource
	default:
		return shellsession.TranscriptSource(trimmed), invalidTranscriptSource
	}
}

func normalizeTranscriptChunkContent(content string) string {
	content = strings.TrimRight(strings.ReplaceAll(content, "\x00", ""), "\r\n")
	if strings.TrimSpace(content) == "" {
		return ""
	}
	if len(content) > shellsession.MaxTranscriptChunkChars {
		content = content[:shellsession.MaxTranscriptChunkChars]
	}
	return content
}

func reverseTranscriptChunks(chunks []shellsession.TranscriptChunk) {
	for i, j := 0, len(chunks)-1; i < j; i, j = i+1, j-1 {
		chunks[i], chunks[j] = chunks[j], chunks[i]
	}
}
