1. Concise diagnosis of what was weak before this phase
- Handoff launch truth was still reconstructed from proof history rather than owned as a durable control-plane record.
- Tuku could distinguish continuity and recovery states, but launch/retry semantics still depended on scanning `HANDOFF_LAUNCH_*` events after the fact.
- The system did not durably model the difference between a requested launch with unknown outcome, a completed launch, and a failed launch.
- Replay safety was therefore narrower than it should be: the logic knew how to block unknown-outcome retries, but only via proof reconstruction.
- Status and inspect could not directly show the latest launch attempt and retry disposition as first-class control-plane truth.

2. Exact implementation plan you executed
- Added a durable handoff launch record to the domain and storage contracts.
- Added SQLite persistence for launch attempts with explicit `REQUESTED` / `COMPLETED` / `FAILED` state.
- Changed `LaunchHandoff` to create a durable requested launch attempt before adapter invocation, then finalize that same attempt to completed or failed after the adapter returns.
- Replaced proof-history replay detection with durable launch-record replay logic.
- Added a launch-control assessment projection to classify not-requested, requested-outcome-unknown, completed, and failed states, plus retry disposition.
- Wired launch-control truth into recovery assessment, status output, and inspect output.
- Added failure-oriented tests for unknown-outcome blocking, durable success reuse, durable failure retry, and status/inspect launch truth.

3. Files changed
- /Users/kagaya/Desktop/Tuku/internal/domain/handoff/types.go
- /Users/kagaya/Desktop/Tuku/internal/storage/contracts.go
- /Users/kagaya/Desktop/Tuku/internal/storage/sqlite/handoff_repo.go
- /Users/kagaya/Desktop/Tuku/internal/storage/sqlite/store.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/launch_control.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/continuity.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_launch.go
- /Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go
- /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_test.go

4. Before vs after behavior summary
- Before: launch requested/completed/failed/unknown lived mostly as proof reconstruction.
- After: every launch attempt is durably recorded as a first-class launch record.
- Before: a repeated launch call reused durable success and blocked unknown outcome by proof scan, but had no durable launch control model.
- After: replay logic reads durable launch state directly and only uses proof as evidence, not as the source of launch truth.
- Before: durable failed launches were terminal for repeated `LaunchHandoff` calls.
- After: durable failed launches are explicit retry-allowed states, and a new launch attempt can be created safely.
- Before: inspect/status could not explain launch retry truth directly.
- After: inspect/status surface launch state, retry disposition, latest attempt identity, and launch-control reason.

5. New durable launch / retry semantics introduced
- Added `handoff.LaunchStatus`:
  - `REQUESTED`
  - `COMPLETED`
  - `FAILED`
- Added durable `handoff.Launch` records persisted in SQLite.
- Added `LaunchControlState`:
  - `NOT_APPLICABLE`
  - `NOT_REQUESTED`
  - `REQUESTED_OUTCOME_UNKNOWN`
  - `COMPLETED`
  - `FAILED`
- Added `LaunchRetryDisposition`:
  - `NOT_APPLICABLE`
  - `ALLOWED`
  - `BLOCKED`
- Added recovery classifications for launch lifecycle truth:
  - `HANDOFF_LAUNCH_PENDING_OUTCOME`
  - `HANDOFF_LAUNCH_COMPLETED`
- Added recovery actions:
  - `WAIT_FOR_LAUNCH_OUTCOME`
  - `MONITOR_LAUNCHED_HANDOFF`
- Launch replay rules are now:
  - no launch record: launch allowed
  - latest launch `REQUESTED`: retry blocked because outcome is unknown
  - latest launch `COMPLETED`: reuse durable completed result, do not relaunch
  - latest launch `FAILED`: retry allowed, create a new durable launch attempt

6. Tests added or updated
- Updated unknown-outcome replay test to seed a durable requested launch record instead of a proof event.
- Added retry-after-durable-failure coverage.
- Added status/inspect launch-control truth coverage.
- Existing durable-success replay coverage now exercises the durable launch record path instead of proof-history reconstruction.

7. Commands run
```bash
gofmt -w internal/domain/handoff/types.go internal/storage/contracts.go internal/storage/sqlite/handoff_repo.go internal/storage/sqlite/store.go internal/orchestrator/launch_control.go internal/orchestrator/continuity.go internal/orchestrator/recovery.go internal/orchestrator/service.go internal/orchestrator/handoff_launch.go internal/ipc/payloads.go internal/runtime/daemon/service.go internal/orchestrator/handoff_test.go
go test ./internal/orchestrator -count=1
go test ./internal/orchestrator ./internal/runtime/daemon ./internal/app -count=1
```

8. Remaining limitations / next risks
- Launch truth is now durable, but it still lives inside the handoff repository rather than a broader generic control-plane attempt model.
- There is still no explicit operator-facing “force relaunch” or “repair launch state” command; retry allowance is now modeled, but the only current retry path is calling `LaunchHandoff` again.
- Shell snapshot transport was not expanded in this phase; inspect/status remain the stronger operational surfaces.
- Launch completion still means launcher invocation completed, not downstream worker execution completion.
- Recovery and launch control are still projected in memory for status/inspect rather than stored as a consolidated durable assessment object.

9. Full code for every changed file

File: /Users/kagaya/Desktop/Tuku/internal/domain/handoff/types.go
```go
package handoff

import (
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/run"
)

type Status string

const (
	StatusCreated  Status = "CREATED"
	StatusAccepted Status = "ACCEPTED"
	StatusBlocked  Status = "BLOCKED"
)

type Mode string

const (
	ModeResume   Mode = "resume"
	ModeReview   Mode = "review"
	ModeTakeover Mode = "takeover"
)

type AcknowledgmentStatus string

const (
	AcknowledgmentCaptured    AcknowledgmentStatus = "CAPTURED"
	AcknowledgmentUnavailable AcknowledgmentStatus = "UNAVAILABLE"
)

type LaunchStatus string

const (
	LaunchStatusRequested LaunchStatus = "REQUESTED"
	LaunchStatusCompleted LaunchStatus = "COMPLETED"
	LaunchStatusFailed    LaunchStatus = "FAILED"
)

// Packet is a durable cross-worker continuation artifact.
type Packet struct {
	Version int `json:"version"`

	HandoffID string        `json:"handoff_id"`
	TaskID    common.TaskID `json:"task_id"`
	Status    Status        `json:"status"`

	SourceWorker run.WorkerKind `json:"source_worker"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	HandoffMode  Mode           `json:"handoff_mode"`
	Reason       string         `json:"reason"`

	CurrentPhase   phase.Phase           `json:"current_phase"`
	CheckpointID   common.CheckpointID   `json:"checkpoint_id"`
	BriefID        common.BriefID        `json:"brief_id"`
	IntentID       common.IntentID       `json:"intent_id"`
	CapsuleVersion common.CapsuleVersion `json:"capsule_version"`
	RepoAnchor     checkpoint.RepoAnchor `json:"repo_anchor"`

	IsResumable      bool   `json:"is_resumable"`
	ResumeDescriptor string `json:"resume_descriptor"`

	LatestRunID     common.RunID `json:"latest_run_id,omitempty"`
	LatestRunStatus run.Status   `json:"latest_run_status,omitempty"`

	Goal             string   `json:"goal"`
	BriefObjective   string   `json:"brief_objective"`
	NormalizedAction string   `json:"normalized_action"`
	Constraints      []string `json:"constraints"`
	DoneCriteria     []string `json:"done_criteria"`
	TouchedFiles     []string `json:"touched_files"`
	Blockers         []string `json:"blockers"`
	NextAction       string   `json:"next_action"`
	Unknowns         []string `json:"unknowns"`
	HandoffNotes     []string `json:"handoff_notes"`

	CreatedAt  time.Time      `json:"created_at"`
	AcceptedAt *time.Time     `json:"accepted_at,omitempty"`
	AcceptedBy run.WorkerKind `json:"accepted_by,omitempty"`
}

// LaunchPayload is the deterministic cross-worker payload materialized from persisted continuity state.
type LaunchPayload struct {
	Version int `json:"version"`

	TaskID       common.TaskID  `json:"task_id"`
	HandoffID    string         `json:"handoff_id"`
	SourceWorker run.WorkerKind `json:"source_worker"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	HandoffMode  Mode           `json:"handoff_mode"`

	CurrentPhase     phase.Phase           `json:"current_phase"`
	CheckpointID     common.CheckpointID   `json:"checkpoint_id"`
	BriefID          common.BriefID        `json:"brief_id"`
	IntentID         common.IntentID       `json:"intent_id"`
	CapsuleVersion   common.CapsuleVersion `json:"capsule_version"`
	RepoAnchor       checkpoint.RepoAnchor `json:"repo_anchor"`
	IsResumable      bool                  `json:"is_resumable"`
	ResumeDescriptor string                `json:"resume_descriptor"`

	LatestRunID      common.RunID `json:"latest_run_id,omitempty"`
	LatestRunStatus  run.Status   `json:"latest_run_status,omitempty"`
	LatestRunSummary string       `json:"latest_run_summary,omitempty"`

	Goal             string   `json:"goal"`
	BriefObjective   string   `json:"brief_objective"`
	NormalizedAction string   `json:"normalized_action"`
	Constraints      []string `json:"constraints"`
	DoneCriteria     []string `json:"done_criteria"`
	TouchedFiles     []string `json:"touched_files"`
	Blockers         []string `json:"blockers"`
	NextAction       string   `json:"next_action"`
	Unknowns         []string `json:"unknowns"`
	HandoffNotes     []string `json:"handoff_notes"`

	GeneratedAt time.Time `json:"generated_at"`
}

// Acknowledgment is a bounded durable artifact proving initial post-launch worker acknowledgement state.
type Acknowledgment struct {
	Version      int                  `json:"version"`
	AckID        string               `json:"ack_id"`
	HandoffID    string               `json:"handoff_id"`
	LaunchID     string               `json:"launch_id"`
	TaskID       common.TaskID        `json:"task_id"`
	TargetWorker run.WorkerKind       `json:"target_worker"`
	Status       AcknowledgmentStatus `json:"status"`
	Summary      string               `json:"summary"`
	Unknowns     []string             `json:"unknowns"`
	CreatedAt    time.Time            `json:"created_at"`
}

// Launch is the durable control-plane record for a handoff launch attempt.
type Launch struct {
	Version int `json:"version"`

	AttemptID    string         `json:"attempt_id"`
	HandoffID    string         `json:"handoff_id"`
	TaskID       common.TaskID  `json:"task_id"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	Status       LaunchStatus   `json:"status"`

	LaunchID          string    `json:"launch_id,omitempty"`
	PayloadHash       string    `json:"payload_hash,omitempty"`
	RequestedAt       time.Time `json:"requested_at"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	EndedAt           time.Time `json:"ended_at,omitempty"`
	Command           string    `json:"command,omitempty"`
	Args              []string  `json:"args,omitempty"`
	ExitCode          *int      `json:"exit_code,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	OutputArtifactRef string    `json:"output_artifact_ref,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Repository interface {
	Create(packet Packet) error
	Get(handoffID string) (Packet, error)
	LatestByTask(taskID common.TaskID) (Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch Launch) error
	GetLaunch(attemptID string) (Launch, error)
	LatestLaunchByHandoff(handoffID string) (Launch, error)
	UpdateLaunch(launch Launch) error
	SaveAcknowledgment(ack Acknowledgment) error
	LatestAcknowledgment(handoffID string) (Acknowledgment, error)
}
```

File: /Users/kagaya/Desktop/Tuku/internal/storage/contracts.go
```go
package storage

import (
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/policy"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/run"
)

type CapsuleStore interface {
	Create(c capsule.WorkCapsule) error
	Get(taskID common.TaskID) (capsule.WorkCapsule, error)
	LatestByRepoRoot(repoRoot string) (capsule.WorkCapsule, error)
	Update(c capsule.WorkCapsule) error
}

type ConversationStore interface {
	Append(message conversation.Message) error
	ListRecent(conversationID common.ConversationID, limit int) ([]conversation.Message, error)
}

type IntentStore interface {
	Save(state intent.State) error
	LatestByTask(taskID common.TaskID) (intent.State, error)
}

type BriefStore interface {
	Save(b brief.ExecutionBrief) error
	Get(briefID common.BriefID) (brief.ExecutionBrief, error)
	LatestByTask(taskID common.TaskID) (brief.ExecutionBrief, error)
}

type ProofStore interface {
	Append(event proof.Event) error
	ListByTask(taskID common.TaskID, limit int) ([]proof.Event, error)
}

type RunStore interface {
	Create(run run.ExecutionRun) error
	Get(runID common.RunID) (run.ExecutionRun, error)
	LatestByTask(taskID common.TaskID) (run.ExecutionRun, error)
	LatestRunningByTask(taskID common.TaskID) (run.ExecutionRun, error)
	Update(run run.ExecutionRun) error
}

type CheckpointStore interface {
	Create(c checkpoint.Checkpoint) error
	Get(checkpointID common.CheckpointID) (checkpoint.Checkpoint, error)
	LatestByTask(taskID common.TaskID) (checkpoint.Checkpoint, error)
}

type HandoffStore interface {
	Create(packet handoff.Packet) error
	Get(handoffID string) (handoff.Packet, error)
	LatestByTask(taskID common.TaskID) (handoff.Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status handoff.Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch handoff.Launch) error
	GetLaunch(attemptID string) (handoff.Launch, error)
	LatestLaunchByHandoff(handoffID string) (handoff.Launch, error)
	UpdateLaunch(launch handoff.Launch) error
	SaveAcknowledgment(ack handoff.Acknowledgment) error
	LatestAcknowledgment(handoffID string) (handoff.Acknowledgment, error)
}

type ContextPackStore interface {
	Save(pack contextdomain.Pack) error
	Get(id common.ContextPackID) (contextdomain.Pack, error)
}

type PolicyDecisionStore interface {
	Save(decision policy.Decision) error
	Get(decisionID common.DecisionID) (policy.Decision, error)
}

type Store interface {
	Capsules() CapsuleStore
	Conversations() ConversationStore
	Intents() IntentStore
	Briefs() BriefStore
	Proofs() ProofStore
	Runs() RunStore
	Checkpoints() CheckpointStore
	Handoffs() HandoffStore
	ContextPacks() ContextPackStore
	PolicyDecisions() PolicyDecisionStore
	WithTx(fn func(Store) error) error
}
```

File: /Users/kagaya/Desktop/Tuku/internal/storage/sqlite/handoff_repo.go
```go
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
```

File: /Users/kagaya/Desktop/Tuku/internal/storage/sqlite/store.go
```go
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/run"
	"tuku/internal/storage"
)

const sqliteTimestampLayout = time.RFC3339Nano

type Store struct {
	db *sql.DB
}

type txStore struct {
	tx *sql.Tx
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		return nil, fmt.Errorf("set pragma journal_mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		return nil, fmt.Errorf("set pragma foreign_keys: %w", err)
	}
	if err := applyMigrations(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Capsules() storage.CapsuleStore {
	return &capsuleRepo{q: s.db}
}

func (s *Store) Conversations() storage.ConversationStore {
	return &conversationRepo{q: s.db}
}

func (s *Store) Intents() storage.IntentStore {
	return &intentRepo{q: s.db}
}

func (s *Store) Briefs() storage.BriefStore {
	return &briefRepo{q: s.db}
}

func (s *Store) Proofs() storage.ProofStore {
	return &proofRepo{q: s.db}
}

func (s *Store) Runs() storage.RunStore {
	return &runRepo{q: s.db}
}

func (s *Store) Checkpoints() storage.CheckpointStore {
	return &checkpointRepo{q: s.db}
}

func (s *Store) ShellSessions() *shellSessionRepo {
	return &shellSessionRepo{q: s.db}
}

func (s *Store) ContextPacks() storage.ContextPackStore {
	panic("TODO: sqlite context-pack repository not implemented in milestone 1")
}

func (s *Store) PolicyDecisions() storage.PolicyDecisionStore {
	panic("TODO: sqlite policy-decision repository not implemented in milestone 1")
}

func (s *Store) WithTx(fn func(storage.Store) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin sqlite tx: %w", err)
	}
	txScoped := &txStore{tx: tx}
	if err := fn(txScoped); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("commit sqlite tx: %w", err)
	}
	return nil
}

func (s *txStore) Capsules() storage.CapsuleStore {
	return &capsuleRepo{q: s.tx}
}

func (s *txStore) Conversations() storage.ConversationStore {
	return &conversationRepo{q: s.tx}
}

func (s *txStore) Intents() storage.IntentStore {
	return &intentRepo{q: s.tx}
}

func (s *txStore) Briefs() storage.BriefStore {
	return &briefRepo{q: s.tx}
}

func (s *txStore) Proofs() storage.ProofStore {
	return &proofRepo{q: s.tx}
}

func (s *txStore) Runs() storage.RunStore {
	return &runRepo{q: s.tx}
}

func (s *txStore) Checkpoints() storage.CheckpointStore {
	return &checkpointRepo{q: s.tx}
}

func (s *txStore) ShellSessions() *shellSessionRepo {
	return &shellSessionRepo{q: s.tx}
}

func (s *txStore) ContextPacks() storage.ContextPackStore {
	panic("TODO: sqlite context-pack repository not implemented in milestone 1")
}

func (s *txStore) PolicyDecisions() storage.PolicyDecisionStore {
	panic("TODO: sqlite policy-decision repository not implemented in milestone 1")
}

func (s *txStore) WithTx(fn func(storage.Store) error) error {
	return fn(s)
}

func applyMigrations(db *sql.DB) error {
	const migration = `
CREATE TABLE IF NOT EXISTS capsules (
	task_id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	version INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	goal TEXT NOT NULL,
	acceptance_criteria_json TEXT NOT NULL,
	constraints_json TEXT NOT NULL,
	repo_root TEXT NOT NULL,
	worktree_path TEXT NOT NULL,
	branch_name TEXT NOT NULL,
	head_sha TEXT NOT NULL,
	working_tree_dirty INTEGER NOT NULL,
	anchor_captured_at TEXT NOT NULL,
	current_phase TEXT NOT NULL,
	status TEXT NOT NULL,
	current_intent_id TEXT NOT NULL,
	current_brief_id TEXT NOT NULL,
	touched_files_json TEXT NOT NULL,
	blockers_json TEXT NOT NULL,
	next_action TEXT NOT NULL,
	parent_task_id TEXT,
	child_task_ids_json TEXT NOT NULL,
	edge_refs_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS conversation_messages (
	message_id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	task_id TEXT NOT NULL,
	role TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_conversation_messages_conversation_created
	ON conversation_messages(conversation_id, created_at DESC);

CREATE TABLE IF NOT EXISTS intent_states (
	intent_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	version INTEGER NOT NULL,
	class TEXT NOT NULL,
	normalized_action TEXT NOT NULL,
	confidence REAL NOT NULL,
	ambiguity_flags_json TEXT NOT NULL,
	requires_clarification INTEGER NOT NULL,
	source_message_ids_json TEXT NOT NULL,
	proposed_phase TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_intent_states_task_created
	ON intent_states(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS execution_briefs (
	brief_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	intent_id TEXT NOT NULL,
	capsule_version INTEGER NOT NULL,
	version INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	objective TEXT NOT NULL,
	normalized_action TEXT NOT NULL,
	scope_in_json TEXT NOT NULL,
	scope_out_json TEXT NOT NULL,
	constraints_json TEXT NOT NULL,
	done_criteria_json TEXT NOT NULL,
	context_pack_id TEXT NOT NULL,
	verbosity TEXT NOT NULL,
	policy_profile_id TEXT NOT NULL,
	brief_hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_execution_briefs_task_created
	ON execution_briefs(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS execution_runs (
	run_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	brief_id TEXT NOT NULL,
	worker_kind TEXT NOT NULL,
	status TEXT NOT NULL,
	started_at TEXT NOT NULL,
	ended_at TEXT,
	interruption_reason TEXT,
	created_from_phase TEXT NOT NULL,
	last_known_summary TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_execution_runs_task_created
	ON execution_runs(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_execution_runs_task_status
	ON execution_runs(task_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS proof_events (
	event_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	run_id TEXT,
	checkpoint_id TEXT,
	sequence_no INTEGER NOT NULL,
	timestamp TEXT NOT NULL,
	type TEXT NOT NULL,
	actor_type TEXT NOT NULL,
	actor_id TEXT NOT NULL,
	payload_json TEXT NOT NULL,
	causal_parent_event_id TEXT,
	capsule_version INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_proof_events_task_seq
	ON proof_events(task_id, sequence_no DESC);

CREATE TABLE IF NOT EXISTS checkpoints (
	checkpoint_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	run_id TEXT,
	version INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	trigger TEXT NOT NULL,
	capsule_version INTEGER NOT NULL,
	phase TEXT NOT NULL,
	anchor_repo_root TEXT NOT NULL,
	anchor_worktree_path TEXT NOT NULL,
	anchor_branch_name TEXT NOT NULL,
	anchor_head_sha TEXT NOT NULL,
	anchor_dirty_hash TEXT NOT NULL,
	anchor_untracked_hash TEXT NOT NULL,
	intent_id TEXT NOT NULL,
	brief_id TEXT NOT NULL,
	context_pack_id TEXT NOT NULL,
	last_event_id TEXT NOT NULL,
	pending_decision_ids_json TEXT NOT NULL,
	resume_descriptor TEXT NOT NULL,
	is_resumable INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_checkpoints_task_created
	ON checkpoints(task_id, created_at DESC);
`
	if _, err := db.Exec(migration); err != nil {
		return fmt.Errorf("apply sqlite baseline migration: %w", err)
	}
	if err := ensureCapsuleColumns(db); err != nil {
		return err
	}
	if err := ensureShellSessionSchema(db); err != nil {
		return err
	}
	return nil
}

func ensureCapsuleColumns(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "working_tree_dirty", DDL: "ALTER TABLE capsules ADD COLUMN working_tree_dirty INTEGER NOT NULL DEFAULT 0"},
		{Name: "anchor_captured_at", DDL: "ALTER TABLE capsules ADD COLUMN anchor_captured_at TEXT NOT NULL DEFAULT ''"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "capsules", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add column %s: %w", item.Name, err)
		}
	}
	return nil
}

func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

type queryable interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type capsuleRepo struct{ q queryable }

type conversationRepo struct{ q queryable }

type intentRepo struct{ q queryable }

type briefRepo struct{ q queryable }

type proofRepo struct{ q queryable }

type runRepo struct{ q queryable }

type checkpointRepo struct{ q queryable }

func (r *capsuleRepo) Create(c capsule.WorkCapsule) error {
	acceptanceJSON, err := marshalStringSlice(c.AcceptanceCriteria)
	if err != nil {
		return err
	}
	constraintsJSON, err := marshalStringSlice(c.Constraints)
	if err != nil {
		return err
	}
	touchedJSON, err := marshalStringSlice(c.TouchedFiles)
	if err != nil {
		return err
	}
	blockersJSON, err := marshalStringSlice(c.Blockers)
	if err != nil {
		return err
	}
	childJSON, err := marshalTaskSlice(c.ChildTaskIDs)
	if err != nil {
		return err
	}
	edgesJSON, err := marshalStringSlice(c.EdgeRefs)
	if err != nil {
		return err
	}

	_, err = r.q.Exec(`
INSERT INTO capsules(
	task_id, conversation_id, version, created_at, updated_at, goal,
	acceptance_criteria_json, constraints_json,
	repo_root, worktree_path, branch_name, head_sha, working_tree_dirty, anchor_captured_at,
	current_phase, status, current_intent_id, current_brief_id,
	touched_files_json, blockers_json, next_action,
	parent_task_id, child_task_ids_json, edge_refs_json
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(c.TaskID), string(c.ConversationID), int64(c.Version),
		c.CreatedAt.Format(sqliteTimestampLayout), c.UpdatedAt.Format(sqliteTimestampLayout), c.Goal,
		acceptanceJSON, constraintsJSON,
		c.RepoRoot, c.WorktreePath, c.BranchName, c.HeadSHA, boolToInt(c.WorkingTreeDirty), c.AnchorCapturedAt.Format(sqliteTimestampLayout),
		string(c.CurrentPhase), c.Status, string(c.CurrentIntentID), string(c.CurrentBriefID),
		touchedJSON, blockersJSON, c.NextAction,
		nilIfTaskID(c.ParentTaskID), childJSON, edgesJSON,
	)
	if err != nil {
		return fmt.Errorf("insert capsule: %w", err)
	}
	return nil
}

func (r *capsuleRepo) Get(taskID common.TaskID) (capsule.WorkCapsule, error) {
	row := r.q.QueryRow(`
SELECT task_id, conversation_id, version, created_at, updated_at, goal,
	acceptance_criteria_json, constraints_json,
	repo_root, worktree_path, branch_name, head_sha, working_tree_dirty, anchor_captured_at,
	current_phase, status, current_intent_id, current_brief_id,
	touched_files_json, blockers_json, next_action,
	parent_task_id, child_task_ids_json, edge_refs_json
FROM capsules WHERE task_id = ?
`, string(taskID))

	var c capsule.WorkCapsule
	var createdAt, updatedAt, anchorCapturedAt string
	var phaseStr string
	var acceptanceJSON, constraintsJSON, touchedJSON, blockersJSON, childJSON, edgesJSON string
	var dirtyInt int
	var parentTask sql.NullString

	err := row.Scan(
		&c.TaskID, &c.ConversationID, &c.Version, &createdAt, &updatedAt, &c.Goal,
		&acceptanceJSON, &constraintsJSON,
		&c.RepoRoot, &c.WorktreePath, &c.BranchName, &c.HeadSHA, &dirtyInt, &anchorCapturedAt,
		&phaseStr, &c.Status, &c.CurrentIntentID, &c.CurrentBriefID,
		&touchedJSON, &blockersJSON, &c.NextAction,
		&parentTask, &childJSON, &edgesJSON,
	)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}

	c.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return capsule.WorkCapsule{}, fmt.Errorf("parse capsule created_at: %w", err)
	}
	c.UpdatedAt, err = time.Parse(sqliteTimestampLayout, updatedAt)
	if err != nil {
		return capsule.WorkCapsule{}, fmt.Errorf("parse capsule updated_at: %w", err)
	}
	if anchorCapturedAt != "" {
		c.AnchorCapturedAt, err = time.Parse(sqliteTimestampLayout, anchorCapturedAt)
		if err != nil {
			return capsule.WorkCapsule{}, fmt.Errorf("parse capsule anchor_captured_at: %w", err)
		}
	}
	c.WorkingTreeDirty = dirtyInt == 1
	c.CurrentPhase = parsePhase(phaseStr)
	c.AcceptanceCriteria, err = unmarshalStringSlice(acceptanceJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.Constraints, err = unmarshalStringSlice(constraintsJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.TouchedFiles, err = unmarshalStringSlice(touchedJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.Blockers, err = unmarshalStringSlice(blockersJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.ChildTaskIDs, err = unmarshalTaskSlice(childJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.EdgeRefs, err = unmarshalStringSlice(edgesJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	if parentTask.Valid {
		p := common.TaskID(parentTask.String)
		c.ParentTaskID = &p
	}
	return c, nil
}

func (r *capsuleRepo) Update(c capsule.WorkCapsule) error {
	acceptanceJSON, err := marshalStringSlice(c.AcceptanceCriteria)
	if err != nil {
		return err
	}
	constraintsJSON, err := marshalStringSlice(c.Constraints)
	if err != nil {
		return err
	}
	touchedJSON, err := marshalStringSlice(c.TouchedFiles)
	if err != nil {
		return err
	}
	blockersJSON, err := marshalStringSlice(c.Blockers)
	if err != nil {
		return err
	}
	childJSON, err := marshalTaskSlice(c.ChildTaskIDs)
	if err != nil {
		return err
	}
	edgesJSON, err := marshalStringSlice(c.EdgeRefs)
	if err != nil {
		return err
	}

	res, err := r.q.Exec(`
UPDATE capsules SET
	conversation_id=?, version=?, created_at=?, updated_at=?, goal=?,
	acceptance_criteria_json=?, constraints_json=?,
	repo_root=?, worktree_path=?, branch_name=?, head_sha=?, working_tree_dirty=?, anchor_captured_at=?,
	current_phase=?, status=?, current_intent_id=?, current_brief_id=?,
	touched_files_json=?, blockers_json=?, next_action=?,
	parent_task_id=?, child_task_ids_json=?, edge_refs_json=?
WHERE task_id=?
`,
		string(c.ConversationID), int64(c.Version), c.CreatedAt.Format(sqliteTimestampLayout), c.UpdatedAt.Format(sqliteTimestampLayout), c.Goal,
		acceptanceJSON, constraintsJSON,
		c.RepoRoot, c.WorktreePath, c.BranchName, c.HeadSHA, boolToInt(c.WorkingTreeDirty), c.AnchorCapturedAt.Format(sqliteTimestampLayout),
		string(c.CurrentPhase), c.Status, string(c.CurrentIntentID), string(c.CurrentBriefID),
		touchedJSON, blockersJSON, c.NextAction,
		nilIfTaskID(c.ParentTaskID), childJSON, edgesJSON,
		string(c.TaskID),
	)
	if err != nil {
		return fmt.Errorf("update capsule: %w", err)
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

func (r *conversationRepo) Append(message conversation.Message) error {
	_, err := r.q.Exec(`
INSERT INTO conversation_messages(message_id, conversation_id, task_id, role, body, created_at)
VALUES(?,?,?,?,?,?)
`,
		string(message.MessageID), string(message.ConversationID), string(message.TaskID), string(message.Role), message.Body,
		message.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert conversation message: %w", err)
	}
	return nil
}

func (r *conversationRepo) ListRecent(conversationID common.ConversationID, limit int) ([]conversation.Message, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT message_id, conversation_id, task_id, role, body, created_at
FROM conversation_messages
WHERE conversation_id = ?
ORDER BY created_at DESC
LIMIT ?
`, string(conversationID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := make([]conversation.Message, 0, limit)
	for rows.Next() {
		var m conversation.Message
		var ts string
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.TaskID, &m.Role, &m.Body, &ts); err != nil {
			return nil, err
		}
		m.CreatedAt, err = time.Parse(sqliteTimestampLayout, ts)
		if err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Return chronological order.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

func (r *intentRepo) Save(state intent.State) error {
	ambJSON, err := marshalStringSlice(state.AmbiguityFlags)
	if err != nil {
		return err
	}
	sourceJSON, err := marshalMessageSlice(state.SourceMessageIDs)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO intent_states(
	intent_id, task_id, version, class, normalized_action, confidence,
	ambiguity_flags_json, requires_clarification, source_message_ids_json,
	proposed_phase, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
`,
		string(state.IntentID), string(state.TaskID), state.Version, string(state.Class), state.NormalizedAction,
		state.Confidence, ambJSON, boolToInt(state.RequiresClarification), sourceJSON,
		string(state.ProposedPhase), state.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert intent state: %w", err)
	}
	return nil
}

func (r *intentRepo) LatestByTask(taskID common.TaskID) (intent.State, error) {
	row := r.q.QueryRow(`
SELECT intent_id, task_id, version, class, normalized_action, confidence,
	ambiguity_flags_json, requires_clarification, source_message_ids_json,
	proposed_phase, created_at
FROM intent_states
WHERE task_id = ?
ORDER BY created_at DESC
LIMIT 1
`, string(taskID))

	var st intent.State
	var ambJSON, sourceJSON, proposedPhase, ts string
	var requiresInt int
	err := row.Scan(
		&st.IntentID, &st.TaskID, &st.Version, &st.Class, &st.NormalizedAction, &st.Confidence,
		&ambJSON, &requiresInt, &sourceJSON,
		&proposedPhase, &ts,
	)
	if err != nil {
		return intent.State{}, err
	}
	st.AmbiguityFlags, err = unmarshalStringSlice(ambJSON)
	if err != nil {
		return intent.State{}, err
	}
	st.RequiresClarification = requiresInt == 1
	st.SourceMessageIDs, err = unmarshalMessageSlice(sourceJSON)
	if err != nil {
		return intent.State{}, err
	}
	st.ProposedPhase = parsePhase(proposedPhase)
	st.CreatedAt, err = time.Parse(sqliteTimestampLayout, ts)
	if err != nil {
		return intent.State{}, err
	}
	return st, nil
}

func (r *briefRepo) Save(b brief.ExecutionBrief) error {
	scopeInJSON, err := marshalStringSlice(b.ScopeIn)
	if err != nil {
		return err
	}
	scopeOutJSON, err := marshalStringSlice(b.ScopeOut)
	if err != nil {
		return err
	}
	constraintsJSON, err := marshalStringSlice(b.Constraints)
	if err != nil {
		return err
	}
	doneJSON, err := marshalStringSlice(b.DoneCriteria)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO execution_briefs(
	brief_id, task_id, intent_id, capsule_version, version, created_at, objective,
	normalized_action, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	context_pack_id, verbosity, policy_profile_id, brief_hash
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(b.BriefID), string(b.TaskID), string(b.IntentID), int64(b.CapsuleVersion), b.Version, b.CreatedAt.Format(sqliteTimestampLayout),
		b.Objective, b.NormalizedAction, scopeInJSON, scopeOutJSON, constraintsJSON, doneJSON,
		string(b.ContextPackID), string(b.Verbosity), b.PolicyProfileID, b.BriefHash,
	)
	if err != nil {
		return fmt.Errorf("insert execution brief: %w", err)
	}
	return nil
}

func (r *briefRepo) Get(briefID common.BriefID) (brief.ExecutionBrief, error) {
	row := r.q.QueryRow(`
SELECT brief_id, task_id, intent_id, capsule_version, version, created_at, objective,
	normalized_action, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	context_pack_id, verbosity, policy_profile_id, brief_hash
FROM execution_briefs WHERE brief_id = ?
`, string(briefID))
	return scanBrief(row)
}

func (r *briefRepo) LatestByTask(taskID common.TaskID) (brief.ExecutionBrief, error) {
	row := r.q.QueryRow(`
SELECT brief_id, task_id, intent_id, capsule_version, version, created_at, objective,
	normalized_action, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	context_pack_id, verbosity, policy_profile_id, brief_hash
FROM execution_briefs WHERE task_id = ?
ORDER BY created_at DESC
LIMIT 1
`, string(taskID))
	return scanBrief(row)
}

func (r *runRepo) Create(execRun run.ExecutionRun) error {
	_, err := r.q.Exec(`
INSERT INTO execution_runs(
	run_id, task_id, brief_id, worker_kind, status, started_at, ended_at,
	interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(execRun.RunID), string(execRun.TaskID), string(execRun.BriefID), string(execRun.WorkerKind),
		string(execRun.Status), execRun.StartedAt.Format(sqliteTimestampLayout), nilIfTime(execRun.EndedAt),
		nilIfString(execRun.InterruptionReason), string(execRun.CreatedFromPhase), nilIfString(execRun.LastKnownSummary),
		execRun.CreatedAt.Format(sqliteTimestampLayout), execRun.UpdatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert execution run: %w", err)
	}
	return nil
}

func (r *runRepo) Get(runID common.RunID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, status, started_at, ended_at,
	interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE run_id = ?
`, string(runID))
	return scanRun(row)
}

func (r *runRepo) LatestByTask(taskID common.TaskID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, status, started_at, ended_at,
	interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE task_id = ?
ORDER BY created_at DESC
LIMIT 1
`, string(taskID))
	return scanRun(row)
}

func (r *runRepo) LatestRunningByTask(taskID common.TaskID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, status, started_at, ended_at,
	interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE task_id = ? AND status = ?
ORDER BY updated_at DESC
LIMIT 1
`, string(taskID), string(run.StatusRunning))
	return scanRun(row)
}

func (r *runRepo) Update(execRun run.ExecutionRun) error {
	res, err := r.q.Exec(`
UPDATE execution_runs SET
	task_id=?, brief_id=?, worker_kind=?, status=?, started_at=?, ended_at=?,
	interruption_reason=?, created_from_phase=?, last_known_summary=?, created_at=?, updated_at=?
WHERE run_id = ?
`,
		string(execRun.TaskID), string(execRun.BriefID), string(execRun.WorkerKind), string(execRun.Status),
		execRun.StartedAt.Format(sqliteTimestampLayout), nilIfTime(execRun.EndedAt),
		nilIfString(execRun.InterruptionReason), string(execRun.CreatedFromPhase), nilIfString(execRun.LastKnownSummary),
		execRun.CreatedAt.Format(sqliteTimestampLayout), execRun.UpdatedAt.Format(sqliteTimestampLayout),
		string(execRun.RunID),
	)
	if err != nil {
		return fmt.Errorf("update execution run: %w", err)
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

func (r *checkpointRepo) Create(cp checkpoint.Checkpoint) error {
	pendingJSON, err := marshalDecisionSlice(cp.PendingDecisionIDs)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO checkpoints(
	checkpoint_id, task_id, run_id, version, created_at, trigger, capsule_version, phase,
	anchor_repo_root, anchor_worktree_path, anchor_branch_name, anchor_head_sha, anchor_dirty_hash, anchor_untracked_hash,
	intent_id, brief_id, context_pack_id, last_event_id, pending_decision_ids_json, resume_descriptor, is_resumable
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(cp.CheckpointID), string(cp.TaskID), nilIfRunIDValue(cp.RunID), cp.Version,
		cp.CreatedAt.Format(sqliteTimestampLayout), string(cp.Trigger), int64(cp.CapsuleVersion), string(cp.Phase),
		cp.Anchor.RepoRoot, cp.Anchor.WorktreePath, cp.Anchor.BranchName, cp.Anchor.HeadSHA, cp.Anchor.DirtyHash, cp.Anchor.UntrackedHash,
		string(cp.IntentID), string(cp.BriefID), string(cp.ContextPackID), string(cp.LastEventID), pendingJSON, cp.ResumeDescriptor,
		boolToInt(cp.IsResumable),
	)
	if err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}
	return nil
}

func (r *checkpointRepo) Get(checkpointID common.CheckpointID) (checkpoint.Checkpoint, error) {
	row := r.q.QueryRow(`
SELECT checkpoint_id, task_id, run_id, version, created_at, trigger, capsule_version, phase,
	anchor_repo_root, anchor_worktree_path, anchor_branch_name, anchor_head_sha, anchor_dirty_hash, anchor_untracked_hash,
	intent_id, brief_id, context_pack_id, last_event_id, pending_decision_ids_json, resume_descriptor, is_resumable
FROM checkpoints
WHERE checkpoint_id = ?
`, string(checkpointID))
	return scanCheckpoint(row)
}

func (r *checkpointRepo) LatestByTask(taskID common.TaskID) (checkpoint.Checkpoint, error) {
	row := r.q.QueryRow(`
SELECT checkpoint_id, task_id, run_id, version, created_at, trigger, capsule_version, phase,
	anchor_repo_root, anchor_worktree_path, anchor_branch_name, anchor_head_sha, anchor_dirty_hash, anchor_untracked_hash,
	intent_id, brief_id, context_pack_id, last_event_id, pending_decision_ids_json, resume_descriptor, is_resumable
FROM checkpoints
WHERE task_id = ?
ORDER BY created_at DESC, checkpoint_id DESC
LIMIT 1
`, string(taskID))
	return scanCheckpoint(row)
}

func (r *proofRepo) Append(event proof.Event) error {
	if event.SequenceNo == 0 {
		row := r.q.QueryRow(`SELECT COALESCE(MAX(sequence_no), 0) + 1 FROM proof_events WHERE task_id = ?`, string(event.TaskID))
		if err := row.Scan(&event.SequenceNo); err != nil {
			return fmt.Errorf("next proof sequence: %w", err)
		}
	}

	_, err := r.q.Exec(`
INSERT INTO proof_events(
	event_id, task_id, run_id, checkpoint_id, sequence_no, timestamp, type,
	actor_type, actor_id, payload_json, causal_parent_event_id, capsule_version
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(event.EventID), string(event.TaskID), nilIfRunID(event.RunID), nilIfCheckpointID(event.CheckpointID), event.SequenceNo,
		event.Timestamp.Format(sqliteTimestampLayout), string(event.Type),
		string(event.ActorType), event.ActorID, event.PayloadJSON,
		nilIfEventID(event.CausalParentEventID), int64(event.CapsuleVersion),
	)
	if err != nil {
		return fmt.Errorf("insert proof event: %w", err)
	}
	return nil
}

func (r *proofRepo) ListByTask(taskID common.TaskID, limit int) ([]proof.Event, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT event_id, task_id, run_id, checkpoint_id, sequence_no, timestamp, type,
	actor_type, actor_id, payload_json, causal_parent_event_id, capsule_version
FROM proof_events
WHERE task_id = ?
ORDER BY sequence_no DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]proof.Event, 0, limit)
	for rows.Next() {
		var e proof.Event
		var runID, checkpointID, parent sql.NullString
		var ts string
		if err := rows.Scan(
			&e.EventID, &e.TaskID, &runID, &checkpointID, &e.SequenceNo, &ts, &e.Type,
			&e.ActorType, &e.ActorID, &e.PayloadJSON, &parent, &e.CapsuleVersion,
		); err != nil {
			return nil, err
		}
		if runID.Valid {
			r := common.RunID(runID.String)
			e.RunID = &r
		}
		if checkpointID.Valid {
			c := common.CheckpointID(checkpointID.String)
			e.CheckpointID = &c
		}
		if parent.Valid {
			p := common.EventID(parent.String)
			e.CausalParentEventID = &p
		}
		e.Timestamp, err = time.Parse(sqliteTimestampLayout, ts)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Return chronological order.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

func parsePhase(value string) phase.Phase {
	return phase.Phase(value)
}

func nilIfTaskID(id *common.TaskID) any {
	if id == nil {
		return nil
	}
	return string(*id)
}

func nilIfRunID(id *common.RunID) any {
	if id == nil {
		return nil
	}
	return string(*id)
}

func nilIfRunIDValue(id common.RunID) any {
	if id == "" {
		return nil
	}
	return string(id)
}

func nilIfCheckpointID(id *common.CheckpointID) any {
	if id == nil {
		return nil
	}
	return string(*id)
}

func nilIfEventID(id *common.EventID) any {
	if id == nil {
		return nil
	}
	return string(*id)
}

func nilIfTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(sqliteTimestampLayout)
}

func nilIfZeroTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format(sqliteTimestampLayout)
}

func nilIfString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func marshalStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalStringSlice(value string) ([]string, error) {
	if value == "" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseNullableTime(value sql.NullString) (time.Time, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}
	return time.Parse(sqliteTimestampLayout, value.String)
}

func marshalTaskSlice(values []common.TaskID) (string, error) {
	if values == nil {
		values = []common.TaskID{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalTaskSlice(value string) ([]common.TaskID, error) {
	if value == "" {
		return []common.TaskID{}, nil
	}
	var out []common.TaskID
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalMessageSlice(values []common.MessageID) (string, error) {
	if values == nil {
		values = []common.MessageID{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalMessageSlice(value string) ([]common.MessageID, error) {
	if value == "" {
		return []common.MessageID{}, nil
	}
	var out []common.MessageID
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalDecisionSlice(values []common.DecisionID) (string, error) {
	if values == nil {
		values = []common.DecisionID{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalDecisionSlice(value string) ([]common.DecisionID, error) {
	if value == "" {
		return []common.DecisionID{}, nil
	}
	var out []common.DecisionID
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanBrief(row *sql.Row) (brief.ExecutionBrief, error) {
	var b brief.ExecutionBrief
	var createdAt string
	var scopeInJSON, scopeOutJSON, constraintsJSON, doneJSON string
	err := row.Scan(
		&b.BriefID, &b.TaskID, &b.IntentID, &b.CapsuleVersion, &b.Version, &createdAt, &b.Objective,
		&b.NormalizedAction, &scopeInJSON, &scopeOutJSON, &constraintsJSON, &doneJSON,
		&b.ContextPackID, &b.Verbosity, &b.PolicyProfileID, &b.BriefHash,
	)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.ScopeIn, err = unmarshalStringSlice(scopeInJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.ScopeOut, err = unmarshalStringSlice(scopeOutJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.Constraints, err = unmarshalStringSlice(constraintsJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.DoneCriteria, err = unmarshalStringSlice(doneJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	return b, nil
}

func scanRun(row *sql.Row) (run.ExecutionRun, error) {
	var r run.ExecutionRun
	var startedAt, createdAt, updatedAt string
	var endedAt sql.NullString
	var interruption sql.NullString
	var summary sql.NullString
	err := row.Scan(
		&r.RunID, &r.TaskID, &r.BriefID, &r.WorkerKind, &r.Status, &startedAt, &endedAt,
		&interruption, &r.CreatedFromPhase, &summary, &createdAt, &updatedAt,
	)
	if err != nil {
		return run.ExecutionRun{}, err
	}
	r.StartedAt, err = time.Parse(sqliteTimestampLayout, startedAt)
	if err != nil {
		return run.ExecutionRun{}, err
	}
	if endedAt.Valid && endedAt.String != "" {
		parsed, err := time.Parse(sqliteTimestampLayout, endedAt.String)
		if err != nil {
			return run.ExecutionRun{}, err
		}
		r.EndedAt = &parsed
	}
	if interruption.Valid {
		r.InterruptionReason = interruption.String
	}
	if summary.Valid {
		r.LastKnownSummary = summary.String
	}
	r.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return run.ExecutionRun{}, err
	}
	r.UpdatedAt, err = time.Parse(sqliteTimestampLayout, updatedAt)
	if err != nil {
		return run.ExecutionRun{}, err
	}
	return r, nil
}

func scanCheckpoint(row *sql.Row) (checkpoint.Checkpoint, error) {
	var cp checkpoint.Checkpoint
	var createdAt string
	var runID sql.NullString
	var triggerStr, phaseStr string
	var pendingJSON string
	var resumableInt int
	err := row.Scan(
		&cp.CheckpointID, &cp.TaskID, &runID, &cp.Version, &createdAt, &triggerStr, &cp.CapsuleVersion, &phaseStr,
		&cp.Anchor.RepoRoot, &cp.Anchor.WorktreePath, &cp.Anchor.BranchName, &cp.Anchor.HeadSHA, &cp.Anchor.DirtyHash, &cp.Anchor.UntrackedHash,
		&cp.IntentID, &cp.BriefID, &cp.ContextPackID, &cp.LastEventID, &pendingJSON, &cp.ResumeDescriptor, &resumableInt,
	)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if runID.Valid {
		cp.RunID = common.RunID(runID.String)
	}
	cp.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	cp.Trigger = checkpoint.Trigger(triggerStr)
	cp.Phase = parsePhase(phaseStr)
	cp.IsResumable = resumableInt == 1
	cp.PendingDecisionIDs, err = unmarshalDecisionSlice(pendingJSON)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	return cp, nil
}
```

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/launch_control.go
```go
package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	rundomain "tuku/internal/domain/run"
)

type LaunchControlState string

const (
	LaunchControlStateNotApplicable           LaunchControlState = "NOT_APPLICABLE"
	LaunchControlStateNotRequested            LaunchControlState = "NOT_REQUESTED"
	LaunchControlStateRequestedOutcomeUnknown LaunchControlState = "REQUESTED_OUTCOME_UNKNOWN"
	LaunchControlStateCompleted               LaunchControlState = "COMPLETED"
	LaunchControlStateFailed                  LaunchControlState = "FAILED"
)

type LaunchRetryDisposition string

const (
	LaunchRetryDispositionNotApplicable LaunchRetryDisposition = "NOT_APPLICABLE"
	LaunchRetryDispositionAllowed       LaunchRetryDisposition = "ALLOWED"
	LaunchRetryDispositionBlocked       LaunchRetryDisposition = "BLOCKED"
)

type LaunchControl struct {
	TaskID           common.TaskID          `json:"task_id"`
	HandoffID        string                 `json:"handoff_id,omitempty"`
	AttemptID        string                 `json:"attempt_id,omitempty"`
	LaunchID         string                 `json:"launch_id,omitempty"`
	State            LaunchControlState     `json:"state"`
	RetryDisposition LaunchRetryDisposition `json:"retry_disposition"`
	Reason           string                 `json:"reason,omitempty"`
	TargetWorker     rundomain.WorkerKind   `json:"target_worker,omitempty"`
	RequestedAt      time.Time              `json:"requested_at,omitempty"`
	CompletedAt      time.Time              `json:"completed_at,omitempty"`
	FailedAt         time.Time              `json:"failed_at,omitempty"`
}

func assessLaunchControl(taskID common.TaskID, packet *handoff.Packet, launch *handoff.Launch) LaunchControl {
	control := LaunchControl{
		TaskID:           taskID,
		State:            LaunchControlStateNotApplicable,
		RetryDisposition: LaunchRetryDispositionNotApplicable,
	}
	if packet == nil {
		control.Reason = "no handoff packet is present"
		return control
	}
	control.HandoffID = packet.HandoffID
	control.TargetWorker = packet.TargetWorker

	if packet.TargetWorker != rundomain.WorkerKindClaude {
		control.Reason = fmt.Sprintf("latest handoff target %s is not launchable by the Claude launcher", packet.TargetWorker)
		return control
	}
	switch packet.Status {
	case handoff.StatusCreated, handoff.StatusAccepted:
	default:
		control.Reason = fmt.Sprintf("handoff %s is not launchable in status %s", packet.HandoffID, packet.Status)
		return control
	}
	if !packet.IsResumable {
		control.Reason = fmt.Sprintf("handoff %s is not launchable because its checkpoint is not resumable", packet.HandoffID)
		return control
	}
	if launch == nil {
		control.State = LaunchControlStateNotRequested
		control.RetryDisposition = LaunchRetryDispositionAllowed
		control.Reason = fmt.Sprintf("accepted handoff %s has no durable launch attempt yet", packet.HandoffID)
		return control
	}

	control.AttemptID = launch.AttemptID
	control.LaunchID = launch.LaunchID
	control.RequestedAt = launch.RequestedAt
	switch launch.Status {
	case handoff.LaunchStatusRequested:
		control.State = LaunchControlStateRequestedOutcomeUnknown
		control.RetryDisposition = LaunchRetryDispositionBlocked
		control.Reason = fmt.Sprintf("launch attempt %s for handoff %s is durably recorded as requested, but no completion or failure outcome is persisted", launch.AttemptID, packet.HandoffID)
	case handoff.LaunchStatusCompleted:
		control.State = LaunchControlStateCompleted
		control.RetryDisposition = LaunchRetryDispositionBlocked
		control.CompletedAt = launch.EndedAt
		if strings.TrimSpace(launch.LaunchID) != "" {
			control.Reason = fmt.Sprintf("handoff %s already has durable completed launch %s", packet.HandoffID, launch.LaunchID)
		} else {
			control.Reason = fmt.Sprintf("handoff %s already has a durable completed launch attempt %s", packet.HandoffID, launch.AttemptID)
		}
	case handoff.LaunchStatusFailed:
		control.State = LaunchControlStateFailed
		control.RetryDisposition = LaunchRetryDispositionAllowed
		control.FailedAt = launch.EndedAt
		if strings.TrimSpace(launch.ErrorMessage) != "" {
			control.Reason = fmt.Sprintf("previous launch attempt %s failed durably: %s", launch.AttemptID, launch.ErrorMessage)
		} else {
			control.Reason = fmt.Sprintf("previous launch attempt %s failed durably and may be retried", launch.AttemptID)
		}
	default:
		control.State = LaunchControlStateRequestedOutcomeUnknown
		control.RetryDisposition = LaunchRetryDispositionBlocked
		control.Reason = fmt.Sprintf("launch attempt %s has unsupported durable status %s", launch.AttemptID, launch.Status)
	}
	return control
}
```

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/continuity.go
```go
package orchestrator

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	rundomain "tuku/internal/domain/run"
)

type continuityViolationCode string

const (
	continuityViolationCapsuleBriefMissing             continuityViolationCode = "CAPSULE_BRIEF_MISSING"
	continuityViolationCapsuleIntentMissing            continuityViolationCode = "CAPSULE_INTENT_MISSING"
	continuityViolationCheckpointTaskMismatch          continuityViolationCode = "CHECKPOINT_TASK_MISMATCH"
	continuityViolationCheckpointRunMissing            continuityViolationCode = "CHECKPOINT_RUN_MISSING"
	continuityViolationCheckpointBriefMissing          continuityViolationCode = "CHECKPOINT_BRIEF_MISSING"
	continuityViolationCheckpointResumablePhase        continuityViolationCode = "CHECKPOINT_RESUMABLE_PHASE_INVALID"
	continuityViolationRunTaskMismatch                 continuityViolationCode = "RUN_TASK_MISMATCH"
	continuityViolationRunBriefMissing                 continuityViolationCode = "RUN_BRIEF_MISSING"
	continuityViolationRunPhaseMismatch                continuityViolationCode = "RUN_PHASE_MISMATCH"
	continuityViolationRunningRunCheckpointMismatch    continuityViolationCode = "RUNNING_RUN_CHECKPOINT_MISMATCH"
	continuityViolationLatestHandoffTaskMismatch       continuityViolationCode = "LATEST_HANDOFF_TASK_MISMATCH"
	continuityViolationLatestHandoffBriefMissing       continuityViolationCode = "LATEST_HANDOFF_BRIEF_MISSING"
	continuityViolationLatestHandoffCheckpointMissing  continuityViolationCode = "LATEST_HANDOFF_CHECKPOINT_MISSING"
	continuityViolationLatestHandoffCheckpointMismatch continuityViolationCode = "LATEST_HANDOFF_CHECKPOINT_MISMATCH"
	continuityViolationLatestHandoffAcceptedInvalid    continuityViolationCode = "LATEST_HANDOFF_ACCEPTED_INVALID"
	continuityViolationLatestLaunchInvalid             continuityViolationCode = "LATEST_LAUNCH_INVALID"
	continuityViolationLatestAckInvalid                continuityViolationCode = "LATEST_ACK_INVALID"
)

type continuityViolation struct {
	Code    continuityViolationCode
	Message string
}

type continuitySnapshot struct {
	Capsule              capsule.WorkCapsule
	LatestRun            *rundomain.ExecutionRun
	LatestCheckpoint     *checkpoint.Checkpoint
	LatestHandoff        *handoff.Packet
	LatestLaunch         *handoff.Launch
	LatestAcknowledgment *handoff.Acknowledgment
}

func (c *Coordinator) loadContinuitySnapshot(taskID common.TaskID) (continuitySnapshot, error) {
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return continuitySnapshot{}, err
	}

	snapshot := continuitySnapshot{Capsule: caps}
	if latestRun, err := c.store.Runs().LatestByTask(taskID); err == nil {
		runCopy := latestRun
		snapshot.LatestRun = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(taskID); err == nil {
		cpCopy := latestCheckpoint
		snapshot.LatestCheckpoint = &cpCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestHandoff, err := c.store.Handoffs().LatestByTask(taskID); err == nil {
		packetCopy := latestHandoff
		snapshot.LatestHandoff = &packetCopy
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(latestHandoff.HandoffID); err == nil {
			launchCopy := latestLaunch
			snapshot.LatestLaunch = &launchCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			snapshot.LatestAcknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	return snapshot, nil
}

func (c *Coordinator) validateContinuitySnapshot(snapshot continuitySnapshot) ([]continuityViolation, error) {
	violations := make([]continuityViolation, 0, 8)
	caps := snapshot.Capsule

	if caps.CurrentBriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCapsuleBriefMissing,
			Message: "capsule has no current brief reference",
		})
	} else if _, err := c.store.Briefs().Get(caps.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCapsuleBriefMissing,
				Message: fmt.Sprintf("capsule references missing brief %s", caps.CurrentBriefID),
			})
		} else {
			return nil, err
		}
	}

	if caps.CurrentIntentID != "" {
		latestIntent, err := c.store.Intents().LatestByTask(caps.TaskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCapsuleIntentMissing,
					Message: fmt.Sprintf("capsule references missing intent %s", caps.CurrentIntentID),
				})
			} else {
				return nil, err
			}
		} else if latestIntent.IntentID != caps.CurrentIntentID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCapsuleIntentMissing,
				Message: fmt.Sprintf("capsule current intent %s does not match latest intent %s", caps.CurrentIntentID, latestIntent.IntentID),
			})
		}
	}

	if snapshot.LatestCheckpoint != nil {
		cpViolations, err := c.validateCheckpointContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, cpViolations...)
	}

	if snapshot.LatestRun != nil {
		runViolations, err := c.validateRunContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, runViolations...)
	} else if caps.CurrentPhase == phase.PhaseExecuting {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunPhaseMismatch,
			Message: "capsule phase is EXECUTING but no run exists",
		})
	}

	if snapshot.LatestHandoff != nil {
		handoffViolations, err := c.validateHandoffContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, handoffViolations...)
	}

	return dedupeContinuityViolations(violations), nil
}

func (c *Coordinator) validateCheckpointContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	cp := snapshot.LatestCheckpoint
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 4)

	if cp.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCheckpointTaskMismatch,
			Message: fmt.Sprintf("latest checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, caps.TaskID),
		})
	}
	if cp.BriefID != "" {
		if _, err := c.store.Briefs().Get(cp.BriefID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCheckpointBriefMissing,
					Message: fmt.Sprintf("latest checkpoint references missing brief %s", cp.BriefID),
				})
			} else {
				return nil, err
			}
		}
	}
	if cp.RunID != "" {
		runRec, err := c.store.Runs().Get(cp.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCheckpointRunMissing,
					Message: fmt.Sprintf("latest checkpoint references missing run %s", cp.RunID),
				})
			} else {
				return nil, err
			}
		} else if runRec.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCheckpointTaskMismatch,
				Message: fmt.Sprintf("latest checkpoint run task mismatch: run task=%s capsule task=%s", runRec.TaskID, caps.TaskID),
			})
		}
	}
	if cp.IsResumable && (cp.Phase == phase.PhaseAwaitingDecision || cp.Phase == phase.PhaseBlocked) {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCheckpointResumablePhase,
			Message: fmt.Sprintf("latest checkpoint %s is marked resumable in incompatible phase %s", cp.CheckpointID, cp.Phase),
		})
	}

	return violations, nil
}

func (c *Coordinator) validateRunContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	runRec := snapshot.LatestRun
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 4)

	if runRec.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunTaskMismatch,
			Message: fmt.Sprintf("latest run task mismatch: run task=%s capsule task=%s", runRec.TaskID, caps.TaskID),
		})
	}
	if runRec.BriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunBriefMissing,
			Message: "latest run has empty brief reference",
		})
	} else if _, err := c.store.Briefs().Get(runRec.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunBriefMissing,
				Message: fmt.Sprintf("latest run references missing brief %s", runRec.BriefID),
			})
		} else {
			return nil, err
		}
	}

	if runRec.Status == rundomain.StatusRunning {
		if caps.CurrentPhase != phase.PhaseExecuting {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunPhaseMismatch,
				Message: fmt.Sprintf("latest run %s is RUNNING but capsule phase is %s", runRec.RunID, caps.CurrentPhase),
			})
		}
		if caps.CurrentBriefID != "" && caps.CurrentBriefID != runRec.BriefID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunPhaseMismatch,
				Message: fmt.Sprintf("RUNNING run brief %s does not match capsule brief %s", runRec.BriefID, caps.CurrentBriefID),
			})
		}
		if snapshot.LatestCheckpoint != nil {
			if snapshot.LatestCheckpoint.RunID == "" {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationRunningRunCheckpointMismatch,
					Message: fmt.Sprintf("RUNNING run %s has checkpoint linkage without run_id", runRec.RunID),
				})
			} else if snapshot.LatestCheckpoint.RunID != runRec.RunID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationRunningRunCheckpointMismatch,
					Message: fmt.Sprintf("RUNNING run %s does not match checkpoint run linkage %s", runRec.RunID, snapshot.LatestCheckpoint.RunID),
				})
			}
		}
	} else if caps.CurrentPhase == phase.PhaseExecuting {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunPhaseMismatch,
			Message: fmt.Sprintf("capsule phase EXECUTING is inconsistent with latest run terminal status %s", runRec.Status),
		})
	}

	return violations, nil
}

func (c *Coordinator) validateHandoffContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	packet := snapshot.LatestHandoff
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 8)

	if packet.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffTaskMismatch,
			Message: fmt.Sprintf("latest handoff task mismatch: handoff task=%s capsule task=%s", packet.TaskID, caps.TaskID),
		})
	}
	if packet.BriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffBriefMissing,
			Message: fmt.Sprintf("latest handoff %s has empty brief reference", packet.HandoffID),
		})
	} else if _, err := c.store.Briefs().Get(packet.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffBriefMissing,
				Message: fmt.Sprintf("latest handoff references missing brief %s", packet.BriefID),
			})
		} else {
			return nil, err
		}
	}
	if packet.CheckpointID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffCheckpointMissing,
			Message: fmt.Sprintf("latest handoff %s has empty checkpoint reference", packet.HandoffID),
		})
	} else {
		cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMissing,
					Message: fmt.Sprintf("latest handoff references missing checkpoint %s", packet.CheckpointID),
				})
			} else {
				return nil, err
			}
		} else {
			if cp.TaskID != caps.TaskID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffTaskMismatch,
					Message: fmt.Sprintf("latest handoff checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, caps.TaskID),
				})
			}
			if packet.IsResumable && !cp.IsResumable {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff %s claims resumable continuity but checkpoint %s is not resumable", packet.HandoffID, packet.CheckpointID),
				})
			}
			if packet.BriefID != "" && cp.BriefID != "" && packet.BriefID != cp.BriefID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff brief %s does not match checkpoint brief %s", packet.BriefID, cp.BriefID),
				})
			}
			if packet.IntentID != "" && cp.IntentID != "" && packet.IntentID != cp.IntentID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff intent %s does not match checkpoint intent %s", packet.IntentID, cp.IntentID),
				})
			}
		}
	}

	switch packet.Status {
	case handoff.StatusAccepted:
		if packet.AcceptedBy == "" || packet.AcceptedAt == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is ACCEPTED without accepted_by and accepted_at", packet.HandoffID),
			})
		}
	case handoff.StatusCreated:
		if packet.AcceptedBy != "" || packet.AcceptedAt != nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is CREATED but carries acceptance fields", packet.HandoffID),
			})
		}
	case handoff.StatusBlocked:
		if packet.AcceptedBy != "" || packet.AcceptedAt != nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but carries acceptance fields", packet.HandoffID),
			})
		}
	}

	if snapshot.LatestLaunch != nil {
		launch := snapshot.LatestLaunch
		if launch.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch task mismatch: launch task=%s capsule task=%s", launch.TaskID, caps.TaskID),
			})
		}
		if launch.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch handoff mismatch: launch handoff=%s latest handoff=%s", launch.HandoffID, packet.HandoffID),
			})
		}
		if launch.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch target %s does not match latest handoff target %s", launch.TargetWorker, packet.TargetWorker),
			})
		}
		if packet.Status == handoff.StatusBlocked {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but has a persisted launch attempt", packet.HandoffID),
			})
		}
		switch launch.Status {
		case handoff.LaunchStatusRequested:
		case handoff.LaunchStatusCompleted:
			if launch.EndedAt.IsZero() {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestLaunchInvalid,
					Message: fmt.Sprintf("latest launch %s is COMPLETED without ended_at", launch.AttemptID),
				})
			}
		case handoff.LaunchStatusFailed:
			if launch.EndedAt.IsZero() {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestLaunchInvalid,
					Message: fmt.Sprintf("latest launch %s is FAILED without ended_at", launch.AttemptID),
				})
			}
		default:
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch %s has unsupported status %s", launch.AttemptID, launch.Status),
			})
		}
	}

	if snapshot.LatestAcknowledgment != nil {
		ack := snapshot.LatestAcknowledgment
		if ack.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment task mismatch: ack task=%s capsule task=%s", ack.TaskID, caps.TaskID),
			})
		}
		if ack.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment handoff mismatch: ack handoff=%s latest handoff=%s", ack.HandoffID, packet.HandoffID),
			})
		}
		if ack.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment target %s does not match latest handoff target %s", ack.TargetWorker, packet.TargetWorker),
			})
		}
		if packet.Status == handoff.StatusBlocked {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but has a persisted launch acknowledgment", packet.HandoffID),
			})
		}
		if strings.TrimSpace(ack.LaunchID) == "" {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment for handoff %s has empty launch id", packet.HandoffID),
			})
		}
		if snapshot.LatestLaunch != nil {
			launch := snapshot.LatestLaunch
			switch launch.Status {
			case handoff.LaunchStatusCompleted:
				if strings.TrimSpace(launch.LaunchID) != "" && ack.LaunchID != launch.LaunchID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestAckInvalid,
						Message: fmt.Sprintf("latest acknowledgment launch %s does not match latest completed launch %s", ack.LaunchID, launch.LaunchID),
					})
				}
			case handoff.LaunchStatusFailed:
				if strings.TrimSpace(ack.LaunchID) != "" && (ack.LaunchID == launch.LaunchID || launch.LaunchID == "") {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestAckInvalid,
						Message: fmt.Sprintf("latest failed launch %s should not have acknowledgment state for the same attempt", launch.AttemptID),
					})
				}
			}
		}
	}

	return violations, nil
}

func dedupeContinuityViolations(values []continuityViolation) []continuityViolation {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]continuityViolation, 0, len(values))
	for _, value := range values {
		key := string(value.Code) + "|" + value.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstContinuityViolationMessage(values []continuityViolation) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Message
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func repoAnchorsEqual(a checkpoint.RepoAnchor, b checkpoint.RepoAnchor) bool {
	return a.RepoRoot == b.RepoRoot &&
		a.WorktreePath == b.WorktreePath &&
		a.BranchName == b.BranchName &&
		a.HeadSHA == b.HeadSHA &&
		a.DirtyHash == b.DirtyHash &&
		a.UntrackedHash == b.UntrackedHash
}

func buildReplayBlockedLaunchResponse(packet handoff.Packet) LaunchHandoffResult {
	canonical := fmt.Sprintf(
		"Launch for handoff %s was previously requested, but Tuku does not have a durable completion or failure record for that request. The outcome is unknown, so automatic retry is blocked to avoid duplicate worker launch.",
		packet.HandoffID,
	)
	return LaunchHandoffResult{
		TaskID:            packet.TaskID,
		HandoffID:         packet.HandoffID,
		TargetWorker:      packet.TargetWorker,
		LaunchStatus:      HandoffLaunchStatusBlocked,
		CanonicalResponse: canonical,
	}
}
```

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go
```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	rundomain "tuku/internal/domain/run"
)

type RecoveryClass string

const (
	RecoveryClassReadyNextRun                   RecoveryClass = "READY_NEXT_RUN"
	RecoveryClassInterruptedRunRecoverable      RecoveryClass = "INTERRUPTED_RUN_RECOVERABLE"
	RecoveryClassAcceptedHandoffLaunchReady     RecoveryClass = "ACCEPTED_HANDOFF_LAUNCH_READY"
	RecoveryClassHandoffLaunchPendingOutcome    RecoveryClass = "HANDOFF_LAUNCH_PENDING_OUTCOME"
	RecoveryClassHandoffLaunchCompleted         RecoveryClass = "HANDOFF_LAUNCH_COMPLETED"
	RecoveryClassFailedRunReviewRequired        RecoveryClass = "FAILED_RUN_REVIEW_REQUIRED"
	RecoveryClassValidationReviewRequired       RecoveryClass = "VALIDATION_REVIEW_REQUIRED"
	RecoveryClassStaleRunReconciliationRequired RecoveryClass = "STALE_RUN_RECONCILIATION_REQUIRED"
	RecoveryClassDecisionRequired               RecoveryClass = "DECISION_REQUIRED"
	RecoveryClassBlockedDrift                   RecoveryClass = "BLOCKED_DRIFT"
	RecoveryClassRepairRequired                 RecoveryClass = "REPAIR_REQUIRED"
	RecoveryClassCompletedNoAction              RecoveryClass = "COMPLETED_NO_ACTION"
)

type RecoveryAction string

const (
	RecoveryActionNone                   RecoveryAction = "NONE"
	RecoveryActionStartNextRun           RecoveryAction = "START_NEXT_RUN"
	RecoveryActionResumeInterrupted      RecoveryAction = "RESUME_INTERRUPTED_RUN"
	RecoveryActionLaunchAcceptedHandoff  RecoveryAction = "LAUNCH_ACCEPTED_HANDOFF"
	RecoveryActionWaitForLaunchOutcome   RecoveryAction = "WAIT_FOR_LAUNCH_OUTCOME"
	RecoveryActionMonitorLaunchedHandoff RecoveryAction = "MONITOR_LAUNCHED_HANDOFF"
	RecoveryActionInspectFailedRun       RecoveryAction = "INSPECT_FAILED_RUN"
	RecoveryActionReviewValidation       RecoveryAction = "REVIEW_VALIDATION_STATE"
	RecoveryActionReconcileStaleRun      RecoveryAction = "RECONCILE_STALE_RUN"
	RecoveryActionMakeResumeDecision     RecoveryAction = "MAKE_RESUME_DECISION"
	RecoveryActionRepairContinuity       RecoveryAction = "REPAIR_CONTINUITY"
)

type RecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RecoveryAssessment struct {
	TaskID                 common.TaskID         `json:"task_id"`
	ContinuityOutcome      ContinueOutcome       `json:"continuity_outcome"`
	RecoveryClass          RecoveryClass         `json:"recovery_class"`
	RecommendedAction      RecoveryAction        `json:"recommended_action"`
	ReadyForNextRun        bool                  `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                  `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                  `json:"requires_decision,omitempty"`
	RequiresRepair         bool                  `json:"requires_repair,omitempty"`
	RequiresReview         bool                  `json:"requires_review,omitempty"`
	RequiresReconciliation bool                  `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass `json:"drift_class,omitempty"`
	Reason                 string                `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID   `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID          `json:"run_id,omitempty"`
	HandoffID              string                `json:"handoff_id,omitempty"`
	HandoffStatus          handoff.Status        `json:"handoff_status,omitempty"`
	Issues                 []RecoveryIssue       `json:"issues,omitempty"`
}

func (c *Coordinator) AssessRecovery(ctx context.Context, taskID string) (RecoveryAssessment, error) {
	assessment, err := c.assessContinue(ctx, common.TaskID(strings.TrimSpace(taskID)))
	if err != nil {
		return RecoveryAssessment{}, err
	}
	return c.recoveryFromContinueAssessment(assessment), nil
}

func (c *Coordinator) recoveryFromContinueAssessment(assessment continueAssessment) RecoveryAssessment {
	recovery := RecoveryAssessment{
		TaskID:            assessment.TaskID,
		ContinuityOutcome: assessment.Outcome,
		DriftClass:        assessment.DriftClass,
		Reason:            assessment.Reason,
		CheckpointID:      assessment.ReuseCheckpointID,
		Issues:            recoveryIssuesFromContinuity(assessment.Issues),
	}
	if assessment.LatestCheckpoint != nil {
		recovery.CheckpointID = assessment.LatestCheckpoint.CheckpointID
	}
	if assessment.LatestRun != nil {
		recovery.RunID = assessment.LatestRun.RunID
	}
	if assessment.LatestHandoff != nil {
		recovery.HandoffID = assessment.LatestHandoff.HandoffID
		recovery.HandoffStatus = assessment.LatestHandoff.Status
	}

	switch assessment.Outcome {
	case ContinueOutcomeBlockedInconsistent:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = "continuity state is inconsistent and must be repaired before recovery"
		}
		return recovery
	case ContinueOutcomeBlockedDrift:
		recovery.RecoveryClass = RecoveryClassBlockedDrift
		recovery.RecommendedAction = RecoveryActionMakeResumeDecision
		recovery.RequiresDecision = true
		if recovery.Reason == "" {
			recovery.Reason = "repository drift blocks automatic recovery"
		}
		return recovery
	case ContinueOutcomeNeedsDecision:
		recovery.RecoveryClass = RecoveryClassDecisionRequired
		recovery.RecommendedAction = RecoveryActionMakeResumeDecision
		recovery.RequiresDecision = true
		if recovery.Reason == "" {
			recovery.Reason = "resume requires an explicit operator decision"
		}
		return recovery
	case ContinueOutcomeStaleReconciled:
		recovery.RecoveryClass = RecoveryClassStaleRunReconciliationRequired
		recovery.RecommendedAction = RecoveryActionReconcileStaleRun
		recovery.RequiresReconciliation = true
		if recovery.Reason == "" {
			recovery.Reason = "latest run is still durably RUNNING and must be reconciled before recovery"
		}
		return recovery
	case ContinueOutcomeSafe:
		// Continue with operational recovery classification below.
	default:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = fmt.Sprintf("unsupported continuity outcome: %s", assessment.Outcome)
		}
		return recovery
	}

	if packet := assessment.LatestHandoff; packet != nil && packet.Status == handoff.StatusAccepted && packet.TargetWorker == rundomain.WorkerKindClaude && packet.IsResumable {
		control := assessLaunchControl(assessment.TaskID, packet, assessment.LatestLaunch)
		switch control.State {
		case LaunchControlStateNotRequested:
			recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
			recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
			recovery.ReadyForHandoffLaunch = true
			recovery.Reason = fmt.Sprintf("accepted handoff %s is ready to launch for %s", packet.HandoffID, packet.TargetWorker)
			return recovery
		case LaunchControlStateFailed:
			recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
			recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
			recovery.ReadyForHandoffLaunch = control.RetryDisposition == LaunchRetryDispositionAllowed
			recovery.Reason = control.Reason
			return recovery
		case LaunchControlStateRequestedOutcomeUnknown:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchPendingOutcome
			recovery.RecommendedAction = RecoveryActionWaitForLaunchOutcome
			recovery.Reason = control.Reason
			return recovery
		case LaunchControlStateCompleted:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchCompleted
			recovery.RecommendedAction = RecoveryActionMonitorLaunchedHandoff
			recovery.Reason = control.Reason
			return recovery
		}
	}

	if runRec := assessment.LatestRun; runRec != nil {
		switch runRec.Status {
		case rundomain.StatusInterrupted:
			if assessment.LatestCheckpoint != nil && assessment.LatestCheckpoint.IsResumable {
				recovery.RecoveryClass = RecoveryClassInterruptedRunRecoverable
				recovery.RecommendedAction = RecoveryActionResumeInterrupted
				recovery.ReadyForNextRun = true
				recovery.Reason = fmt.Sprintf("interrupted run %s is recoverable from checkpoint %s", runRec.RunID, assessment.LatestCheckpoint.CheckpointID)
				return recovery
			}
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = fmt.Sprintf("interrupted run %s has no resumable checkpoint for recovery", runRec.RunID)
			return recovery
		case rundomain.StatusFailed:
			recovery.RecoveryClass = RecoveryClassFailedRunReviewRequired
			recovery.RecommendedAction = RecoveryActionInspectFailedRun
			recovery.RequiresReview = true
			recovery.Reason = fmt.Sprintf("latest run %s failed; inspect failure evidence before retrying or regenerating the brief", runRec.RunID)
			return recovery
		case rundomain.StatusCompleted:
			switch assessment.Capsule.CurrentPhase {
			case phase.PhaseValidating:
				recovery.RecoveryClass = RecoveryClassValidationReviewRequired
				recovery.RecommendedAction = RecoveryActionReviewValidation
				recovery.RequiresReview = true
				recovery.Reason = fmt.Sprintf("latest run %s completed and task is awaiting validation review", runRec.RunID)
				return recovery
			case phase.PhaseCompleted:
				recovery.RecoveryClass = RecoveryClassCompletedNoAction
				recovery.RecommendedAction = RecoveryActionNone
				recovery.Reason = "task is already completed; no recovery action is required"
				return recovery
			case phase.PhaseBriefReady:
				recovery.RecoveryClass = RecoveryClassReadyNextRun
				recovery.RecommendedAction = RecoveryActionStartNextRun
				recovery.ReadyForNextRun = true
				recovery.Reason = fmt.Sprintf("task is ready for the next bounded run with brief %s", assessment.Capsule.CurrentBriefID)
				return recovery
			}
		}
	}

	switch assessment.Capsule.CurrentPhase {
	case phase.PhaseBriefReady:
		recovery.RecoveryClass = RecoveryClassReadyNextRun
		recovery.RecommendedAction = RecoveryActionStartNextRun
		recovery.ReadyForNextRun = true
		recovery.Reason = fmt.Sprintf("task is ready for the next bounded run with brief %s", assessment.Capsule.CurrentBriefID)
	case phase.PhasePaused:
		recovery.RecoveryClass = RecoveryClassInterruptedRunRecoverable
		recovery.RecommendedAction = RecoveryActionResumeInterrupted
		recovery.ReadyForNextRun = assessment.LatestCheckpoint != nil && assessment.LatestCheckpoint.IsResumable
		if recovery.ReadyForNextRun {
			recovery.Reason = fmt.Sprintf("paused task is recoverable from checkpoint %s", assessment.LatestCheckpoint.CheckpointID)
		} else {
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = "paused task has no resumable checkpoint for recovery"
		}
	case phase.PhaseValidating:
		recovery.RecoveryClass = RecoveryClassValidationReviewRequired
		recovery.RecommendedAction = RecoveryActionReviewValidation
		recovery.RequiresReview = true
		recovery.Reason = "task is awaiting validation review before another run"
	case phase.PhaseCompleted:
		recovery.RecoveryClass = RecoveryClassCompletedNoAction
		recovery.RecommendedAction = RecoveryActionNone
		recovery.Reason = "task is already completed; no recovery action is required"
	default:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = fmt.Sprintf("task phase %s does not support deterministic recovery", assessment.Capsule.CurrentPhase)
		}
	}

	return recovery
}

func recoveryIssuesFromContinuity(values []continuityViolation) []RecoveryIssue {
	if len(values) == 0 {
		return nil
	}
	issues := make([]RecoveryIssue, 0, len(values))
	for _, value := range values {
		issues = append(issues, RecoveryIssue{Code: string(value.Code), Message: value.Message})
	}
	return issues
}

func applyRecoveryAssessmentToContinueResult(result *ContinueTaskResult, recovery RecoveryAssessment) {
	if result == nil {
		return
	}
	result.RecoveryClass = recovery.RecoveryClass
	result.RecommendedAction = recovery.RecommendedAction
	result.ReadyForNextRun = recovery.ReadyForNextRun
	result.ReadyForHandoffLaunch = recovery.ReadyForHandoffLaunch
	result.RecoveryReason = recovery.Reason
}

func applyRecoveryAssessmentToStatus(status *StatusTaskResult, recovery RecoveryAssessment, checkpointResumable bool) {
	if status == nil {
		return
	}
	status.CheckpointResumable = checkpointResumable
	status.IsResumable = recovery.ReadyForNextRun
	status.RecoveryClass = recovery.RecoveryClass
	status.RecommendedAction = recovery.RecommendedAction
	status.ReadyForNextRun = recovery.ReadyForNextRun
	status.ReadyForHandoffLaunch = recovery.ReadyForHandoffLaunch
	status.RecoveryReason = recovery.Reason
	if recovery.Reason != "" {
		status.ResumeDescriptor = recovery.Reason
	}
}
```

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go
```go
package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
)

type StartTaskResult struct {
	TaskID            common.TaskID
	ConversationID    common.ConversationID
	Phase             phase.Phase
	CanonicalResponse string
	RepoAnchor        anchorgit.Snapshot
}

type MessageTaskResult struct {
	TaskID            common.TaskID
	Phase             phase.Phase
	IntentClass       intent.Class
	BriefID           common.BriefID
	BriefHash         string
	CanonicalResponse string
	RepoAnchor        anchorgit.Snapshot
}

type RunTaskRequest struct {
	TaskID             string
	Action             string // start|complete|interrupt
	Mode               string // real|noop
	RunID              common.RunID
	SimulateInterrupt  bool
	InterruptionReason string
}

type RunTaskResult struct {
	TaskID            common.TaskID
	RunID             common.RunID
	RunStatus         rundomain.Status
	Phase             phase.Phase
	CanonicalResponse string
}

type ContinueOutcome string

const (
	ContinueOutcomeSafe                ContinueOutcome = "SAFE_RESUME_AVAILABLE"
	ContinueOutcomeStaleReconciled     ContinueOutcome = "STALE_RUN_RECONCILED"
	ContinueOutcomeNeedsDecision       ContinueOutcome = "RESUME_DECISION_REQUIRED"
	ContinueOutcomeBlockedDrift        ContinueOutcome = "RESUME_BLOCKED_DRIFT"
	ContinueOutcomeBlockedInconsistent ContinueOutcome = "RESUME_BLOCKED_INCONSISTENT_STATE"
)

type ContinueTaskResult struct {
	TaskID                common.TaskID
	Outcome               ContinueOutcome
	DriftClass            checkpoint.DriftClass
	Phase                 phase.Phase
	RunID                 common.RunID
	CheckpointID          common.CheckpointID
	ResumeDescriptor      string
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

type CreateCheckpointResult struct {
	TaskID            common.TaskID
	CheckpointID      common.CheckpointID
	Trigger           checkpoint.Trigger
	IsResumable       bool
	CanonicalResponse string
}

type StatusTaskResult struct {
	TaskID                  common.TaskID
	ConversationID          common.ConversationID
	Goal                    string
	Phase                   phase.Phase
	Status                  string
	CurrentIntentID         common.IntentID
	CurrentIntentClass      intent.Class
	CurrentIntentSummary    string
	CurrentBriefID          common.BriefID
	CurrentBriefHash        string
	LatestRunID             common.RunID
	LatestRunStatus         rundomain.Status
	LatestRunSummary        string
	RepoAnchor              anchorgit.Snapshot
	LatestCheckpointID      common.CheckpointID
	LatestCheckpointAt      time.Time
	LatestCheckpointTrigger checkpoint.Trigger
	CheckpointResumable     bool
	ResumeDescriptor        string
	LatestLaunchAttemptID   string
	LatestLaunchID          string
	LatestLaunchStatus      handoff.LaunchStatus
	LaunchControlState      LaunchControlState
	LaunchRetryDisposition  LaunchRetryDisposition
	LaunchControlReason     string
	IsResumable             bool
	RecoveryClass           RecoveryClass
	RecommendedAction       RecoveryAction
	ReadyForNextRun         bool
	ReadyForHandoffLaunch   bool
	RecoveryReason          string
	LastEventID             common.EventID
	LastEventType           proof.EventType
	LastEventAt             time.Time
}

type InspectTaskResult struct {
	TaskID         common.TaskID
	Intent         *intent.State
	Brief          *brief.ExecutionBrief
	Run            *rundomain.ExecutionRun
	Checkpoint     *checkpoint.Checkpoint
	Handoff        *handoff.Packet
	Launch         *handoff.Launch
	Acknowledgment *handoff.Acknowledgment
	LaunchControl  *LaunchControl
	Recovery       *RecoveryAssessment
	RepoAnchor     anchorgit.Snapshot
}

type Dependencies struct {
	Store                  storage.Store
	IntentCompiler         intent.Compiler
	BriefBuilder           brief.Builder
	WorkerAdapter          adapter_contract.WorkerAdapter
	HandoffLauncher        adapter_contract.HandoffLauncher
	Synthesizer            canonical.Synthesizer
	AnchorProvider         anchorgit.Provider
	ShellSessions          ShellSessionRegistry
	ShellSessionStaleAfter time.Duration
	Clock                  func() time.Time
	IDGenerator            func(prefix string) string
}

type Coordinator struct {
	store                  storage.Store
	intentCompiler         intent.Compiler
	briefBuilder           brief.Builder
	workerAdapter          adapter_contract.WorkerAdapter
	handoffLauncher        adapter_contract.HandoffLauncher
	synthesizer            canonical.Synthesizer
	anchorProvider         anchorgit.Provider
	shellSessions          ShellSessionRegistry
	shellSessionStaleAfter time.Duration
	clock                  func() time.Time
	idGenerator            func(prefix string) string
}

func NewCoordinator(deps Dependencies) (*Coordinator, error) {
	if deps.Store == nil {
		return nil, errors.New("store is required")
	}
	if deps.IntentCompiler == nil {
		return nil, errors.New("intent compiler is required")
	}
	if deps.BriefBuilder == nil {
		return nil, errors.New("brief builder is required")
	}
	if deps.Synthesizer == nil {
		return nil, errors.New("canonical synthesizer is required")
	}
	if deps.ShellSessions == nil {
		return nil, errors.New("shell session registry is required")
	}
	if deps.AnchorProvider == nil {
		deps.AnchorProvider = anchorgit.NewGitProvider()
	}
	if deps.ShellSessionStaleAfter <= 0 {
		deps.ShellSessionStaleAfter = DefaultShellSessionStaleAfter
	}
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.IDGenerator == nil {
		deps.IDGenerator = newID
	}
	return &Coordinator{
		store:                  deps.Store,
		intentCompiler:         deps.IntentCompiler,
		briefBuilder:           deps.BriefBuilder,
		workerAdapter:          deps.WorkerAdapter,
		handoffLauncher:        deps.HandoffLauncher,
		synthesizer:            deps.Synthesizer,
		anchorProvider:         deps.AnchorProvider,
		shellSessions:          deps.ShellSessions,
		shellSessionStaleAfter: deps.ShellSessionStaleAfter,
		clock:                  deps.Clock,
		idGenerator:            deps.IDGenerator,
	}, nil
}

func (c *Coordinator) StartTask(ctx context.Context, goal string, repoRoot string) (StartTaskResult, error) {
	var result StartTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		now := txc.clock()
		taskID := common.TaskID(txc.idGenerator("tsk"))
		conversationID := common.ConversationID(txc.idGenerator("conv"))
		repo := strings.TrimSpace(repoRoot)
		if repo == "" {
			repo = "."
		}
		repo = filepath.Clean(repo)
		anchor := txc.anchorProvider.Capture(ctx, repo)

		caps := capsule.WorkCapsule{
			TaskID:             taskID,
			ConversationID:     conversationID,
			Version:            1,
			CreatedAt:          now,
			UpdatedAt:          now,
			Goal:               strings.TrimSpace(goal),
			AcceptanceCriteria: []string{},
			Constraints:        []string{},
			RepoRoot:           anchor.RepoRoot,
			WorktreePath:       anchor.RepoRoot,
			BranchName:         anchor.Branch,
			HeadSHA:            anchor.HeadSHA,
			WorkingTreeDirty:   anchor.WorkingTreeDirty,
			AnchorCapturedAt:   anchor.CapturedAt,
			CurrentPhase:       phase.PhaseIntake,
			Status:             "ACTIVE",
			CurrentIntentID:    "",
			CurrentBriefID:     "",
			TouchedFiles:       []string{},
			Blockers:           []string{},
			NextAction:         "Await user message for intent interpretation",
			ParentTaskID:       nil,
			ChildTaskIDs:       []common.TaskID{},
			EdgeRefs:           []string{},
		}
		if err := txc.store.Capsules().Create(caps); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": string(phase.PhaseIntake), "reason": "task created"}, nil); err != nil {
			return err
		}

		canonicalText := "Tuku task initialized. Repo anchor captured. I am tracking canonical task state and evidence. Send your first implementation instruction to generate an execution brief."
		if err := txc.emitCanonicalConversation(caps, canonicalText, map[string]any{"summary": "task initialized"}, nil); err != nil {
			return err
		}

		result = StartTaskResult{
			TaskID:            taskID,
			ConversationID:    conversationID,
			Phase:             caps.CurrentPhase,
			CanonicalResponse: canonicalText,
			RepoAnchor:        anchor,
		}
		return nil
	})
	if err != nil {
		return StartTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) MessageTask(ctx context.Context, taskID string, message string) (MessageTaskResult, error) {
	var result MessageTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(taskID))
		if err != nil {
			return err
		}
		now := txc.clock()

		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt

		userMsg := conversation.Message{
			MessageID:      common.MessageID(txc.idGenerator("msg")),
			ConversationID: caps.ConversationID,
			TaskID:         caps.TaskID,
			Role:           conversation.RoleUser,
			Body:           message,
			CreatedAt:      now,
		}
		if err := txc.store.Conversations().Append(userMsg); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventUserMessageReceived, proof.ActorUser, "user", map[string]any{"message_id": userMsg.MessageID}, nil); err != nil {
			return err
		}

		recent, err := txc.store.Conversations().ListRecent(caps.ConversationID, 12)
		if err != nil {
			return err
		}
		recentBodies := make([]string, 0, len(recent))
		for _, m := range recent {
			recentBodies = append(recentBodies, m.Body)
		}

		intentState, err := txc.intentCompiler.Compile(intent.CompileInput{
			TaskID:            caps.TaskID,
			LatestMessage:     message,
			RecentMessages:    recentBodies,
			CurrentPhase:      caps.CurrentPhase,
			CurrentBlockers:   caps.Blockers,
			CurrentGoal:       caps.Goal,
			RepoAnchorSummary: fmt.Sprintf("repo=%s branch=%s head=%s dirty=%t", caps.RepoRoot, caps.BranchName, caps.HeadSHA, caps.WorkingTreeDirty),
		})
		if err != nil {
			return err
		}
		intentState.SourceMessageIDs = []common.MessageID{userMsg.MessageID}
		if err := txc.store.Intents().Save(intentState); err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = now
		caps.CurrentIntentID = intentState.IntentID
		caps.CurrentPhase = intentState.ProposedPhase
		if err := txc.appendProof(caps, proof.EventIntentCompiled, proof.ActorSystem, "tuku-intent-stub", map[string]any{
			"intent_id": intentState.IntentID, "class": intentState.Class,
			"normalized_action": intentState.NormalizedAction, "confidence": intentState.Confidence,
		}, nil); err != nil {
			return err
		}

		briefArtifact, err := txc.briefBuilder.Build(brief.BuildInput{
			TaskID:           caps.TaskID,
			IntentID:         intentState.IntentID,
			CapsuleVersion:   caps.Version,
			Goal:             caps.Goal,
			NormalizedAction: intentState.NormalizedAction,
			Constraints:      caps.Constraints,
			ScopeHints:       caps.TouchedFiles,
			ScopeOutHints:    []string{},
			DoneCriteria:     []string{"Execution plan is prepared and ready for worker dispatch"},
			ContextPackID:    "",
			Verbosity:        brief.VerbosityStandard,
			PolicyProfileID:  "default-safe-v1",
		})
		if err != nil {
			return err
		}
		if err := txc.store.Briefs().Save(briefArtifact); err != nil {
			return err
		}

		caps.CurrentBriefID = briefArtifact.BriefID
		caps.CurrentPhase = phase.PhaseBriefReady
		caps.NextAction = "Execution brief is ready. Start a run with `tuku run --task <id>`."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		if err := txc.appendProof(caps, proof.EventBriefCreated, proof.ActorSystem, "tuku-brief-builder", map[string]any{"brief_id": briefArtifact.BriefID, "brief_hash": briefArtifact.BriefHash, "intent_id": intentState.IntentID}, nil); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "intent and brief prepared"}, nil); err != nil {
			return err
		}

		recentEvents, err := txc.store.Proofs().ListByTask(caps.TaskID, 10)
		if err != nil {
			return err
		}
		canonicalText, err := txc.synthesizer.Synthesize(ctx, caps, recentEvents)
		if err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, canonicalText, map[string]any{"intent_id": intentState.IntentID, "brief_id": briefArtifact.BriefID}, nil); err != nil {
			return err
		}

		result = MessageTaskResult{
			TaskID:            caps.TaskID,
			Phase:             caps.CurrentPhase,
			IntentClass:       intentState.Class,
			BriefID:           briefArtifact.BriefID,
			BriefHash:         briefArtifact.BriefHash,
			CanonicalResponse: canonicalText,
			RepoAnchor:        anchor,
		}
		return nil
	})
	if err != nil {
		return MessageTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) RunTask(ctx context.Context, req RunTaskRequest) (RunTaskResult, error) {
	action := strings.TrimSpace(strings.ToLower(req.Action))
	if action == "" {
		action = "start"
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	if mode == "" {
		mode = "real"
	}

	switch action {
	case "start":
		if mode == "real" {
			return c.startRunRealStaged(ctx, req)
		}
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.startRunNoop(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	case "complete":
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.completeRun(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	case "interrupt":
		var result RunTaskResult
		err := c.withTx(func(txc *Coordinator) error {
			caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
			if err != nil {
				return err
			}
			result, err = txc.interruptRun(ctx, caps, req)
			return err
		})
		if err != nil {
			return RunTaskResult{}, err
		}
		return result, nil
	default:
		return RunTaskResult{}, fmt.Errorf("unsupported run action: %s", req.Action)
	}
}

func (c *Coordinator) CreateCheckpoint(ctx context.Context, taskID string) (CreateCheckpointResult, error) {
	var result CreateCheckpointResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(taskID))
		if err != nil {
			return err
		}
		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt
		caps.Version++
		caps.UpdatedAt = txc.clock()
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		resumable := caps.CurrentBriefID != "" && caps.CurrentPhase != phase.PhaseBlocked && caps.CurrentPhase != phase.PhaseAwaitingDecision
		descriptor := "Manual checkpoint captured for deterministic continue."
		if !resumable {
			descriptor = "Manual checkpoint captured for recovery inspection; direct resume is not currently ready."
		}
		cp, err := txc.createCheckpoint(caps, "", checkpoint.TriggerManual, resumable, descriptor)
		if err != nil {
			return err
		}
		canonical := fmt.Sprintf(
			"Manual checkpoint %s captured. Task is resumable from branch %s (head %s).",
			cp.CheckpointID,
			caps.BranchName,
			caps.HeadSHA,
		)
		if !resumable {
			canonical = fmt.Sprintf(
				"Manual checkpoint %s captured on branch %s (head %s), but direct resume is not currently ready.",
				cp.CheckpointID,
				caps.BranchName,
				caps.HeadSHA,
			)
		}
		if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{
			"checkpoint_id": cp.CheckpointID,
			"trigger":       cp.Trigger,
			"is_resumable":  cp.IsResumable,
		}, nil); err != nil {
			return err
		}

		result = CreateCheckpointResult{
			TaskID:            caps.TaskID,
			CheckpointID:      cp.CheckpointID,
			Trigger:           cp.Trigger,
			IsResumable:       cp.IsResumable,
			CanonicalResponse: canonical,
		}
		return nil
	})
	if err != nil {
		return CreateCheckpointResult{}, err
	}
	return result, nil
}

func (c *Coordinator) ContinueTask(ctx context.Context, taskID string) (ContinueTaskResult, error) {
	assessment, err := c.assessContinue(ctx, common.TaskID(taskID))
	if err != nil {
		return ContinueTaskResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if assessment.Outcome == ContinueOutcomeSafe && !recovery.ReadyForNextRun {
		assessment.RequiresMutation = false
	}
	if !assessment.RequiresMutation {
		return c.recordNoMutationContinueOutcome(ctx, assessment, recovery)
	}

	var result ContinueTaskResult
	err = c.withTx(func(txc *Coordinator) error {
		return txc.finalizeContinue(ctx, assessment, recovery, &result)
	})
	if err != nil {
		return ContinueTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) recordNoMutationContinueOutcome(_ context.Context, assessment continueAssessment, recovery RecoveryAssessment) (ContinueTaskResult, error) {
	result := c.noMutationContinueResult(assessment, recovery)
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(assessment.TaskID)
		if err != nil {
			return err
		}
		runID := runIDPointer(result.RunID)
		payload := map[string]any{
			"outcome":           result.Outcome,
			"drift_class":       result.DriftClass,
			"checkpoint_id":     result.CheckpointID,
			"resume_descriptor": result.ResumeDescriptor,
			"no_state_mutation": true,
			"checkpoint_reused": result.CheckpointID != "",
			"assessment_reason": assessment.Reason,
		}
		payload["recovery_class"] = result.RecoveryClass
		payload["recommended_action"] = result.RecommendedAction
		payload["ready_for_next_run"] = result.ReadyForNextRun
		payload["ready_for_handoff_launch"] = result.ReadyForHandoffLaunch
		payload["recovery_reason"] = result.RecoveryReason
		if err := txc.appendProof(caps, proof.EventContinueAssessed, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, result.CanonicalResponse, payload, runID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ContinueTaskResult{}, err
	}
	return result, nil
}

type continueAssessment struct {
	TaskID            common.TaskID
	Capsule           capsule.WorkCapsule
	LatestRun         *rundomain.ExecutionRun
	LatestCheckpoint  *checkpoint.Checkpoint
	LatestHandoff     *handoff.Packet
	LatestLaunch      *handoff.Launch
	LatestAck         *handoff.Acknowledgment
	FreshAnchor       anchorgit.Snapshot
	DriftClass        checkpoint.DriftClass
	Outcome           ContinueOutcome
	Reason            string
	Issues            []continuityViolation
	RequiresMutation  bool
	ReuseCheckpointID common.CheckpointID
}

func (c *Coordinator) assessContinue(ctx context.Context, taskID common.TaskID) (continueAssessment, error) {
	snapshot, err := c.loadContinuitySnapshot(taskID)
	if err != nil {
		return continueAssessment{}, err
	}
	caps := snapshot.Capsule
	anchor := c.anchorProvider.Capture(ctx, caps.RepoRoot)
	issues, err := c.validateContinuitySnapshot(snapshot)
	if err != nil {
		return continueAssessment{}, err
	}
	issue := firstContinuityViolationMessage(issues)
	if issue != "" {
		reuse := c.canReuseInconsistencyCheckpoint(caps, snapshot.LatestCheckpoint, anchor, issue)
		return continueAssessment{
			TaskID:            taskID,
			Capsule:           caps,
			LatestRun:         snapshot.LatestRun,
			LatestCheckpoint:  snapshot.LatestCheckpoint,
			LatestHandoff:     snapshot.LatestHandoff,
			LatestLaunch:      snapshot.LatestLaunch,
			LatestAck:         snapshot.LatestAcknowledgment,
			FreshAnchor:       anchor,
			Outcome:           ContinueOutcomeBlockedInconsistent,
			Reason:            issue,
			Issues:            issues,
			DriftClass:        checkpoint.DriftNone,
			RequiresMutation:  !reuse,
			ReuseCheckpointID: reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if snapshot.LatestRun != nil && snapshot.LatestRun.Status == rundomain.StatusRunning {
		return continueAssessment{
			TaskID:           taskID,
			Capsule:          caps,
			LatestRun:        snapshot.LatestRun,
			LatestCheckpoint: snapshot.LatestCheckpoint,
			LatestHandoff:    snapshot.LatestHandoff,
			LatestLaunch:     snapshot.LatestLaunch,
			LatestAck:        snapshot.LatestAcknowledgment,
			FreshAnchor:      anchor,
			Outcome:          ContinueOutcomeStaleReconciled,
			Reason:           "latest run is durably RUNNING and requires explicit stale reconciliation",
			Issues:           issues,
			DriftClass:       checkpoint.DriftNone,
			RequiresMutation: true,
		}, nil
	}

	baseline := anchorFromCapsule(caps)
	if snapshot.LatestCheckpoint != nil {
		baseline = snapshot.LatestCheckpoint.Anchor
	}
	drift := classifyAnchorDrift(baseline, anchor)

	if caps.CurrentPhase == phase.PhaseAwaitingDecision {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		outcome := ContinueOutcomeNeedsDecision
		reason := "task is already in decision-gated resume state"
		if drift == checkpoint.DriftMajor {
			outcome = ContinueOutcomeBlockedDrift
			reason = "task remains decision-gated with major repo drift"
		}
		return continueAssessment{
			TaskID:            taskID,
			Capsule:           caps,
			LatestRun:         snapshot.LatestRun,
			LatestCheckpoint:  snapshot.LatestCheckpoint,
			LatestHandoff:     snapshot.LatestHandoff,
			LatestLaunch:      snapshot.LatestLaunch,
			LatestAck:         snapshot.LatestAcknowledgment,
			FreshAnchor:       anchor,
			Outcome:           outcome,
			Reason:            reason,
			Issues:            issues,
			DriftClass:        drift,
			RequiresMutation:  !reuse,
			ReuseCheckpointID: reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if drift == checkpoint.DriftMajor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:            taskID,
			Capsule:           caps,
			LatestRun:         snapshot.LatestRun,
			LatestCheckpoint:  snapshot.LatestCheckpoint,
			LatestHandoff:     snapshot.LatestHandoff,
			LatestLaunch:      snapshot.LatestLaunch,
			LatestAck:         snapshot.LatestAcknowledgment,
			FreshAnchor:       anchor,
			Outcome:           ContinueOutcomeBlockedDrift,
			Reason:            "major repo drift blocks direct resume",
			Issues:            issues,
			DriftClass:        drift,
			RequiresMutation:  !reuse,
			ReuseCheckpointID: reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}
	if drift == checkpoint.DriftMinor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:            taskID,
			Capsule:           caps,
			LatestRun:         snapshot.LatestRun,
			LatestCheckpoint:  snapshot.LatestCheckpoint,
			LatestHandoff:     snapshot.LatestHandoff,
			LatestAck:         snapshot.LatestAcknowledgment,
			FreshAnchor:       anchor,
			Outcome:           ContinueOutcomeNeedsDecision,
			Reason:            "minor repo drift requires explicit decision",
			Issues:            issues,
			DriftClass:        drift,
			RequiresMutation:  !reuse,
			ReuseCheckpointID: reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	reuseSafe := c.canReuseSafeCheckpoint(caps, snapshot.LatestRun, snapshot.LatestCheckpoint, anchor)
	return continueAssessment{
		TaskID:            taskID,
		Capsule:           caps,
		LatestRun:         snapshot.LatestRun,
		LatestCheckpoint:  snapshot.LatestCheckpoint,
		LatestHandoff:     snapshot.LatestHandoff,
		LatestLaunch:      snapshot.LatestLaunch,
		LatestAck:         snapshot.LatestAcknowledgment,
		FreshAnchor:       anchor,
		Outcome:           ContinueOutcomeSafe,
		Reason:            "safe resume is available from continuity state",
		Issues:            issues,
		DriftClass:        checkpoint.DriftNone,
		RequiresMutation:  !reuseSafe,
		ReuseCheckpointID: reusableCheckpointID(snapshot.LatestCheckpoint, reuseSafe),
	}, nil
}

func reusableCheckpointID(cp *checkpoint.Checkpoint, ok bool) common.CheckpointID {
	if !ok || cp == nil {
		return ""
	}
	return cp.CheckpointID
}

func (c *Coordinator) finalizeContinue(ctx context.Context, assessment continueAssessment, recovery RecoveryAssessment, out *ContinueTaskResult) error {
	caps, err := c.store.Capsules().Get(assessment.TaskID)
	if err != nil {
		return err
	}
	if caps.Version != assessment.Capsule.Version {
		return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("task state changed during continue assessment (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version), out)
	}
	caps.BranchName = assessment.FreshAnchor.Branch
	caps.HeadSHA = assessment.FreshAnchor.HeadSHA
	caps.WorkingTreeDirty = assessment.FreshAnchor.WorkingTreeDirty
	caps.AnchorCapturedAt = assessment.FreshAnchor.CapturedAt

	switch assessment.Outcome {
	case ContinueOutcomeStaleReconciled:
		if assessment.LatestRun == nil {
			return c.blockedContinueByInconsistency(ctx, caps, "stale reconciliation requested without latest run", out)
		}
		runRec, err := c.store.Runs().Get(assessment.LatestRun.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return c.blockedContinueByInconsistency(ctx, caps, "latest run referenced by assessment is missing", out)
			}
			return err
		}
		if runRec.Status != rundomain.StatusRunning {
			return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("latest run %s is not RUNNING (status=%s)", runRec.RunID, runRec.Status), out)
		}
		return c.reconcileStaleRun(ctx, caps, runRec, out)

	case ContinueOutcomeBlockedDrift:
		return c.blockedContinueByDrift(ctx, caps, assessment.DriftClass, out)

	case ContinueOutcomeNeedsDecision:
		return c.awaitDecisionOnContinue(ctx, caps, assessment.DriftClass, out)

	case ContinueOutcomeBlockedInconsistent:
		return c.blockedContinueByInconsistency(ctx, caps, assessment.Reason, out)

	case ContinueOutcomeSafe:
		var hasCheckpoint bool
		var cp checkpoint.Checkpoint
		if assessment.LatestCheckpoint != nil {
			hasCheckpoint = true
			cp = *assessment.LatestCheckpoint
		}
		var hasRun bool
		var runRec rundomain.ExecutionRun
		if assessment.LatestRun != nil {
			hasRun = true
			runRec = *assessment.LatestRun
		}
		return c.safeContinue(ctx, caps, hasCheckpoint, cp, hasRun, runRec, recovery, out)

	default:
		return c.blockedContinueByInconsistency(ctx, caps, fmt.Sprintf("unsupported continue outcome: %s", assessment.Outcome), out)
	}
}

func (c *Coordinator) noMutationContinueResult(assessment continueAssessment, recovery RecoveryAssessment) ContinueTaskResult {
	caps := assessment.Capsule
	checkpointID := assessment.ReuseCheckpointID
	resumeDescriptor := ""
	if assessment.LatestCheckpoint != nil {
		resumeDescriptor = assessment.LatestCheckpoint.ResumeDescriptor
	}
	runID := common.RunID("")
	if assessment.LatestRun != nil {
		runID = assessment.LatestRun.RunID
	}
	base := ContinueTaskResult{
		TaskID:           caps.TaskID,
		Outcome:          assessment.Outcome,
		DriftClass:       assessment.DriftClass,
		Phase:            caps.CurrentPhase,
		RunID:            runID,
		CheckpointID:     checkpointID,
		ResumeDescriptor: resumeDescriptor,
	}
	applyRecoveryAssessmentToContinueResult(&base, recovery)
	switch assessment.Outcome {
	case ContinueOutcomeSafe:
		switch recovery.RecoveryClass {
		case RecoveryClassInterruptedRunRecoverable:
			base.CanonicalResponse = fmt.Sprintf(
				"Interrupted execution is already recoverable from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because recovery state is unchanged.",
				checkpointID,
				caps.CurrentBriefID,
				assessment.FreshAnchor.Branch,
				assessment.FreshAnchor.HeadSHA,
			)
		case RecoveryClassAcceptedHandoffLaunchReady:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact and accepted handoff %s is ready to launch. No new checkpoint was created because the handoff-based recovery state is unchanged.",
				recovery.HandoffID,
			)
		case RecoveryClassHandoffLaunchPendingOutcome:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but handoff launch is not retryable yet: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassHandoffLaunchCompleted:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, and the latest handoff launch step is already complete: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassFailedRunReviewRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but the next run is not ready because latest run %s failed. Review failure evidence before retrying or regenerating the brief. No new checkpoint was created.",
				runID,
			)
		case RecoveryClassValidationReviewRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity is intact, but the task is still in validation review after run %s. Review validation state before starting another bounded run. No new checkpoint was created.",
				runID,
			)
		case RecoveryClassCompletedNoAction:
			base.CanonicalResponse = "Continuity is intact, and the task is already completed. No recovery action was taken."
		case RecoveryClassRepairRequired:
			base.CanonicalResponse = fmt.Sprintf(
				"Continuity facts are present, but deterministic recovery is not ready: %s. No new checkpoint was created.",
				recovery.Reason,
			)
		case RecoveryClassReadyNextRun:
			base.CanonicalResponse = fmt.Sprintf(
				"Safe resume is already available from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because continuity state is unchanged.",
				checkpointID,
				caps.CurrentBriefID,
				assessment.FreshAnchor.Branch,
				assessment.FreshAnchor.HeadSHA,
			)
		default:
			base.CanonicalResponse = "Continuity is intact. No new checkpoint was created because recovery state is unchanged."
		}
		return base
	case ContinueOutcomeNeedsDecision:
		base.CanonicalResponse = fmt.Sprintf(
			"Resume still requires a decision. I reused checkpoint %s and did not create a new one because the decision-gated continuity state is unchanged.",
			checkpointID,
		)
		return base
	case ContinueOutcomeBlockedDrift:
		base.CanonicalResponse = fmt.Sprintf(
			"Direct resume is still blocked by major repo drift. I reused checkpoint %s and did not create a new continuity record because state is unchanged.",
			checkpointID,
		)
		return base
	case ContinueOutcomeBlockedInconsistent:
		base.CanonicalResponse = fmt.Sprintf(
			"Resume remains blocked due to inconsistent continuity state. I reused checkpoint %s and did not create a new one because the blocked state is unchanged.",
			checkpointID,
		)
		return base
	default:
		base.CanonicalResponse = "Continue assessment completed with no state mutation."
		return base
	}
}

func (c *Coordinator) validateContinueConsistency(snapshot continuitySnapshot) (string, error) {
	violations, err := c.validateContinuitySnapshot(snapshot)
	if err != nil {
		return "", err
	}
	return firstContinuityViolationMessage(violations), nil
}

func (c *Coordinator) canReuseSafeCheckpoint(caps capsule.WorkCapsule, latestRun *rundomain.ExecutionRun, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot) bool {
	if latestCheckpoint == nil || !latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.BriefID != caps.CurrentBriefID {
		return false
	}
	if latestCheckpoint.IntentID != caps.CurrentIntentID {
		return false
	}
	if latestCheckpoint.Phase != caps.CurrentPhase {
		return false
	}
	if latestCheckpoint.CapsuleVersion != caps.Version {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	if latestRun != nil && latestCheckpoint.RunID != "" && latestCheckpoint.RunID != latestRun.RunID {
		return false
	}
	return true
}

func (c *Coordinator) canReuseDecisionCheckpoint(caps capsule.WorkCapsule, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot) bool {
	if latestCheckpoint == nil {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.Phase != phase.PhaseAwaitingDecision || caps.CurrentPhase != phase.PhaseAwaitingDecision {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	return true
}

func (c *Coordinator) canReuseInconsistencyCheckpoint(caps capsule.WorkCapsule, latestCheckpoint *checkpoint.Checkpoint, currentAnchor anchorgit.Snapshot, reason string) bool {
	if latestCheckpoint == nil {
		return false
	}
	if latestCheckpoint.TaskID != caps.TaskID {
		return false
	}
	if latestCheckpoint.IsResumable {
		return false
	}
	if latestCheckpoint.Phase != phase.PhaseBlocked || caps.CurrentPhase != phase.PhaseBlocked {
		return false
	}
	if latestCheckpoint.Anchor.RepoRoot != currentAnchor.RepoRoot {
		return false
	}
	if latestCheckpoint.Anchor.BranchName != currentAnchor.Branch {
		return false
	}
	if latestCheckpoint.Anchor.HeadSHA != currentAnchor.HeadSHA {
		return false
	}
	if latestCheckpoint.Anchor.DirtyHash != boolString(currentAnchor.WorkingTreeDirty) {
		return false
	}
	return strings.Contains(strings.ToLower(latestCheckpoint.ResumeDescriptor), strings.ToLower(reason))
}

type preparedRealRun struct {
	TaskID  common.TaskID
	RunID   common.RunID
	Brief   brief.ExecutionBrief
	Capsule capsule.WorkCapsule
}

func (c *Coordinator) startRunRealStaged(ctx context.Context, req RunTaskRequest) (RunTaskResult, error) {
	prepared, immediateResult, err := c.prepareRealRun(ctx, req)
	if err != nil {
		return RunTaskResult{}, err
	}
	if immediateResult != nil {
		return *immediateResult, nil
	}

	execReq := c.buildExecutionRequest(prepared)
	execResult, execErr := c.workerAdapter.Execute(ctx, execReq, nil)

	finalResult, finalizeErr := c.finalizeRealRun(ctx, prepared, execResult, execErr)
	if finalizeErr != nil {
		return RunTaskResult{}, fmt.Errorf("finalize run %s after worker execution: %w", prepared.RunID, finalizeErr)
	}
	return finalResult, nil
}

func (c *Coordinator) prepareRealRun(ctx context.Context, req RunTaskRequest) (*preparedRealRun, *RunTaskResult, error) {
	var prepared preparedRealRun
	var immediate *RunTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(common.TaskID(req.TaskID))
		if err != nil {
			return err
		}
		if txc.workerAdapter == nil {
			canonical := "Execution adapter is not configured. Tuku cannot run Codex in real mode yet."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_worker_adapter"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}
		if caps.CurrentBriefID == "" {
			canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}

		b, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}
		anchor := txc.anchorProvider.Capture(ctx, caps.RepoRoot)
		caps.BranchName = anchor.Branch
		caps.HeadSHA = anchor.HeadSHA
		caps.WorkingTreeDirty = anchor.WorkingTreeDirty
		caps.AnchorCapturedAt = anchor.CapturedAt

		now := txc.clock()
		runID := req.RunID
		if runID == "" {
			runID = common.RunID(txc.idGenerator("run"))
		}
		runRec := rundomain.ExecutionRun{
			RunID:              runID,
			TaskID:             caps.TaskID,
			BriefID:            b.BriefID,
			WorkerKind:         rundomain.WorkerKindCodex,
			Status:             rundomain.StatusRunning,
			StartedAt:          now,
			CreatedFromPhase:   caps.CurrentPhase,
			LastKnownSummary:   "Codex execution started",
			CreatedAt:          now,
			UpdatedAt:          now,
			InterruptionReason: "",
		}
		if err := txc.store.Runs().Create(runRec); err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = txc.clock()
		caps.CurrentPhase = phase.PhaseExecuting
		caps.NextAction = "Real execution run is in progress."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventWorkerRunStarted, proof.ActorSystem, "tuku-runner", map[string]any{
			"run_id":      runID,
			"brief_id":    b.BriefID,
			"worker_kind": runRec.WorkerKind,
			"mode":        "real",
		}, &runID); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "real codex run started"}, &runID); err != nil {
			return err
		}
		if _, err := txc.createCheckpointWithOptions(caps, runID, checkpoint.TriggerBeforeExecution, true, "Run started and durably marked RUNNING before worker execution.", false); err != nil {
			return err
		}

		prepared = preparedRealRun{
			TaskID:  caps.TaskID,
			RunID:   runID,
			Brief:   b,
			Capsule: caps,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if immediate != nil {
		return nil, immediate, nil
	}
	return &prepared, nil, nil
}

func (c *Coordinator) buildExecutionRequest(prepared *preparedRealRun) adapter_contract.ExecutionRequest {
	agentsChecksum, agentsInstructions := agentsMetadata(prepared.Capsule.RepoRoot)
	return adapter_contract.ExecutionRequest{
		RunID:  prepared.RunID,
		TaskID: prepared.TaskID,
		Worker: adapter_contract.WorkerCodex,
		Brief:  prepared.Brief,
		ContextPack: contextdomain.Pack{
			ContextPackID:      "",
			TaskID:             prepared.TaskID,
			Mode:               contextdomain.ModeCompact,
			TokenBudget:        0,
			RepoAnchorHash:     prepared.Capsule.HeadSHA,
			FreshnessState:     "current",
			IncludedFiles:      prepared.Capsule.TouchedFiles,
			IncludedSnippets:   []contextdomain.Snippet{},
			SelectionRationale: []string{"placeholder context pack for bounded milestone 4 execution"},
			PackHash:           "",
			CreatedAt:          c.clock(),
		},
		RepoAnchor: checkpoint.RepoAnchor{
			RepoRoot:      prepared.Capsule.RepoRoot,
			WorktreePath:  prepared.Capsule.WorktreePath,
			BranchName:    prepared.Capsule.BranchName,
			HeadSHA:       prepared.Capsule.HeadSHA,
			DirtyHash:     boolString(prepared.Capsule.WorkingTreeDirty),
			UntrackedHash: "",
		},
		PolicyProfileID:    prepared.Brief.PolicyProfileID,
		AgentsChecksum:     agentsChecksum,
		AgentsInstructions: agentsInstructions,
		ContextSummary:     fmt.Sprintf("phase=%s touched_files=%d", prepared.Capsule.CurrentPhase, len(prepared.Capsule.TouchedFiles)),
	}
}

func (c *Coordinator) finalizeRealRun(ctx context.Context, prepared *preparedRealRun, execResult adapter_contract.ExecutionResult, execErr error) (RunTaskResult, error) {
	var result RunTaskResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(prepared.TaskID)
		if err != nil {
			return err
		}
		runRec, err := txc.store.Runs().Get(prepared.RunID)
		if err != nil {
			return err
		}
		if runRec.Status != rundomain.StatusRunning {
			return fmt.Errorf("run %s is not RUNNING during finalization (status=%s)", runRec.RunID, runRec.Status)
		}

		if err := txc.appendProof(caps, proof.EventWorkerOutputCaptured, proof.ActorSystem, "tuku-runner", map[string]any{
			"run_id":                  prepared.RunID,
			"exit_code":               execResult.ExitCode,
			"started_at_unix_ms":      execResult.StartedAt.UnixMilli(),
			"ended_at_unix_ms":        execResult.EndedAt.UnixMilli(),
			"stdout_excerpt":          truncate(execResult.Stdout, 2000),
			"stderr_excerpt":          truncate(execResult.Stderr, 2000),
			"changed_files":           execResult.ChangedFiles,
			"changed_files_semantics": execResult.ChangedFilesSemantics,
			"validation_signals":      execResult.ValidationSignals,
			"summary":                 execResult.Summary,
			"error_message":           execResult.ErrorMessage,
		}, &prepared.RunID); err != nil {
			return err
		}
		if len(execResult.ChangedFiles) > 0 {
			if err := txc.appendProof(caps, proof.EventFileChangeDetected, proof.ActorSystem, "tuku-runner", map[string]any{
				"run_id":                  prepared.RunID,
				"changed_files":           execResult.ChangedFiles,
				"changed_files_semantics": execResult.ChangedFilesSemantics,
				"count":                   len(execResult.ChangedFiles),
			}, &prepared.RunID); err != nil {
				return err
			}
		}

		if execErr != nil {
			result, err = txc.markRunFailed(ctx, caps, runRec, execResult, execErr)
			return err
		}
		if execResult.ExitCode != 0 {
			result, err = txc.markRunFailed(ctx, caps, runRec, execResult, fmt.Errorf("codex exit code %d", execResult.ExitCode))
			return err
		}
		result, err = txc.markRunCompleted(ctx, caps, runRec, execResult)
		return err
	})
	if err != nil {
		return RunTaskResult{}, err
	}
	return result, nil
}

func (c *Coordinator) startRunNoop(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	if caps.CurrentBriefID == "" {
		canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
		if err := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	b, err := c.store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		return RunTaskResult{}, err
	}

	now := c.clock()
	runID := req.RunID
	if runID == "" {
		runID = common.RunID(c.idGenerator("run"))
	}
	r := rundomain.ExecutionRun{
		RunID:              runID,
		TaskID:             caps.TaskID,
		BriefID:            b.BriefID,
		WorkerKind:         rundomain.WorkerKindNoop,
		Status:             rundomain.StatusCreated,
		StartedAt:          now,
		CreatedFromPhase:   caps.CurrentPhase,
		LastKnownSummary:   "No-op run created and awaiting placeholder execution",
		CreatedAt:          now,
		UpdatedAt:          now,
		InterruptionReason: "",
	}
	if err := c.store.Runs().Create(r); err != nil {
		return RunTaskResult{}, err
	}

	r.Status = rundomain.StatusRunning
	r.LastKnownSummary = "No-op execution placeholder started"
	r.UpdatedAt = c.clock()
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = c.clock()
	caps.CurrentPhase = phase.PhaseExecuting
	caps.NextAction = "No-op run is active. Complete with `tuku run --task <id> --action complete` or interrupt with `--action interrupt`."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunStarted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": runID, "brief_id": b.BriefID, "worker_kind": r.WorkerKind, "mode": "noop"}, &runID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "execution run started"}, &runID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, runID, checkpoint.TriggerBeforeExecution, true, "No-op run entered RUNNING state."); err != nil {
		return RunTaskResult{}, err
	}

	if req.SimulateInterrupt {
		interruptReq := RunTaskRequest{TaskID: string(caps.TaskID), Action: "interrupt", RunID: runID, InterruptionReason: "simulated interruption"}
		return c.interruptRun(ctx, caps, interruptReq)
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": runID, "status": r.Status}, &runID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: runID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) completeRun(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	r, err := c.resolveRunForAction(caps.TaskID, req.RunID)
	if err != nil {
		canonical := "Execution cannot complete because there is no active run for this task."
		if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_running_run"}, nil); emitErr != nil {
			return RunTaskResult{}, emitErr
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	now := c.clock()
	r.Status = rundomain.StatusCompleted
	r.LastKnownSummary = "Execution placeholder completed"
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Execution placeholder completed. Validation logic is deferred to the next milestone."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunCompleted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "status": r.Status}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run completed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, true, "Run completed and task moved to validation."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) interruptRun(ctx context.Context, caps capsule.WorkCapsule, req RunTaskRequest) (RunTaskResult, error) {
	r, err := c.resolveRunForAction(caps.TaskID, req.RunID)
	if err != nil {
		canonical := "Execution cannot be interrupted because there is no active run for this task."
		if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_running_run"}, nil); emitErr != nil {
			return RunTaskResult{}, emitErr
		}
		return RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}, nil
	}

	now := c.clock()
	reason := strings.TrimSpace(req.InterruptionReason)
	if reason == "" {
		reason = "manual interruption"
	}
	r.Status = rundomain.StatusInterrupted
	r.InterruptionReason = reason
	r.LastKnownSummary = "Execution placeholder interrupted"
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhasePaused
	caps.NextAction = "Run interrupted. Use `tuku continue --task <id>` to reconcile and resume safely."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "reason": reason}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run interrupted"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerInterruption, true, "Run interrupted and task is resumable from paused state."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 12)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "reason": reason}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}

	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) resolveRunForAction(taskID common.TaskID, preferredRunID common.RunID) (rundomain.ExecutionRun, error) {
	var runRecord rundomain.ExecutionRun
	var err error
	if preferredRunID != "" {
		runRecord, err = c.store.Runs().Get(preferredRunID)
	} else {
		runRecord, err = c.store.Runs().LatestRunningByTask(taskID)
	}
	if err != nil {
		return rundomain.ExecutionRun{}, err
	}
	if runRecord.Status != rundomain.StatusRunning {
		return rundomain.ExecutionRun{}, fmt.Errorf("run %s is not RUNNING (status=%s)", runRecord.RunID, runRecord.Status)
	}
	return runRecord, nil
}

func (c *Coordinator) markRunCompleted(ctx context.Context, caps capsule.WorkCapsule, r rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult) (RunTaskResult, error) {
	now := c.clock()
	r.Status = rundomain.StatusCompleted
	r.LastKnownSummary = execResult.Summary
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Codex run completed. Review evidence and decide validation/follow-up."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunCompleted, proof.ActorSystem, "tuku-runner", map[string]any{
		"run_id":                  r.RunID,
		"status":                  r.Status,
		"exit_code":               execResult.ExitCode,
		"changed_files":           execResult.ChangedFiles,
		"changed_files_semantics": execResult.ChangedFilesSemantics,
		"summary":                 execResult.Summary,
		"validation_hints":        execResult.ValidationSignals,
	}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run completed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, true, "Run completed with captured evidence; ready for validation follow-up."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "exit_code": execResult.ExitCode}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) markRunFailed(ctx context.Context, caps capsule.WorkCapsule, r rundomain.ExecutionRun, execResult adapter_contract.ExecutionResult, runErr error) (RunTaskResult, error) {
	now := c.clock()
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		r.Status = rundomain.StatusInterrupted
		r.InterruptionReason = runErr.Error()
		r.LastKnownSummary = "Codex run interrupted"
		r.EndedAt = &now
		r.UpdatedAt = now
		if err := c.store.Runs().Update(r); err != nil {
			return RunTaskResult{}, err
		}
		caps.Version++
		caps.UpdatedAt = now
		caps.CurrentPhase = phase.PhasePaused
		caps.NextAction = "Codex run was interrupted. Check execution evidence and retry."
		if err := c.store.Capsules().Update(caps); err != nil {
			return RunTaskResult{}, err
		}
		if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-runner", map[string]any{"run_id": r.RunID, "reason": runErr.Error()}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run interrupted"}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerInterruption, true, "Run interrupted during execution; resumable from paused phase."); err != nil {
			return RunTaskResult{}, err
		}
		recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
		if err != nil {
			return RunTaskResult{}, err
		}
		canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
		if err != nil {
			return RunTaskResult{}, err
		}
		if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "reason": runErr.Error()}, &r.RunID); err != nil {
			return RunTaskResult{}, err
		}
		return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
	}

	r.Status = rundomain.StatusFailed
	r.LastKnownSummary = fmt.Sprintf("Codex run failed: %s", execResult.Summary)
	r.EndedAt = &now
	r.UpdatedAt = now
	if err := c.store.Runs().Update(r); err != nil {
		return RunTaskResult{}, err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseBlocked
	caps.NextAction = "Codex run failed. Inspect proof evidence and adjust brief or constraints."
	if err := c.store.Capsules().Update(caps); err != nil {
		return RunTaskResult{}, err
	}

	if err := c.appendProof(caps, proof.EventWorkerRunFailed, proof.ActorSystem, "tuku-runner", map[string]any{
		"run_id":                  r.RunID,
		"error":                   runErr.Error(),
		"exit_code":               execResult.ExitCode,
		"summary":                 execResult.Summary,
		"stderr_excerpt":          truncate(execResult.Stderr, 2000),
		"changed_files":           execResult.ChangedFiles,
		"changed_files_semantics": execResult.ChangedFilesSemantics,
	}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{"phase": caps.CurrentPhase, "reason": "run failed"}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	if _, err := c.createCheckpoint(caps, r.RunID, checkpoint.TriggerAfterExecution, false, "Run failed with evidence captured; inspect failure evidence before retrying or regenerating the brief."); err != nil {
		return RunTaskResult{}, err
	}

	recentEvents, err := c.store.Proofs().ListByTask(caps.TaskID, 20)
	if err != nil {
		return RunTaskResult{}, err
	}
	canonicalText, err := c.synthesizer.Synthesize(ctx, caps, recentEvents)
	if err != nil {
		return RunTaskResult{}, err
	}
	if err := c.emitCanonicalConversation(caps, canonicalText, map[string]any{"run_id": r.RunID, "status": r.Status, "error": runErr.Error()}, &r.RunID); err != nil {
		return RunTaskResult{}, err
	}
	return RunTaskResult{TaskID: caps.TaskID, RunID: r.RunID, RunStatus: r.Status, Phase: caps.CurrentPhase, CanonicalResponse: canonicalText}, nil
}

func (c *Coordinator) StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error) {
	caps, err := c.store.Capsules().Get(common.TaskID(taskID))
	if err != nil {
		return StatusTaskResult{}, err
	}

	status := StatusTaskResult{
		TaskID:          caps.TaskID,
		ConversationID:  caps.ConversationID,
		Goal:            caps.Goal,
		Phase:           caps.CurrentPhase,
		Status:          caps.Status,
		CurrentIntentID: caps.CurrentIntentID,
		CurrentBriefID:  caps.CurrentBriefID,
		RepoAnchor: anchorgit.Snapshot{
			RepoRoot:         caps.RepoRoot,
			Branch:           caps.BranchName,
			HeadSHA:          caps.HeadSHA,
			WorkingTreeDirty: caps.WorkingTreeDirty,
			CapturedAt:       caps.AnchorCapturedAt,
		},
	}

	intentState, err := c.store.Intents().LatestByTask(caps.TaskID)
	if err == nil {
		status.CurrentIntentClass = intentState.Class
		status.CurrentIntentSummary = intentState.NormalizedAction
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	if caps.CurrentBriefID != "" {
		b, err := c.store.Briefs().Get(caps.CurrentBriefID)
		if err == nil {
			status.CurrentBriefHash = b.BriefHash
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		}
	}

	if latestRun, err := c.store.Runs().LatestByTask(caps.TaskID); err == nil {
		status.LatestRunID = latestRun.RunID
		status.LatestRunStatus = latestRun.Status
		status.LatestRunSummary = latestRun.LastKnownSummary
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	checkpointResumable := false
	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(caps.TaskID); err == nil {
		status.LatestCheckpointID = latestCheckpoint.CheckpointID
		status.LatestCheckpointAt = latestCheckpoint.CreatedAt
		status.LatestCheckpointTrigger = latestCheckpoint.Trigger
		status.ResumeDescriptor = latestCheckpoint.ResumeDescriptor
		checkpointResumable = latestCheckpoint.IsResumable
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	var latestPacket *handoff.Packet
	if packet, err := c.store.Handoffs().LatestByTask(caps.TaskID); err == nil {
		packetCopy := packet
		latestPacket = &packetCopy
		if launch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err == nil {
			status.LatestLaunchAttemptID = launch.AttemptID
			status.LatestLaunchID = launch.LaunchID
			status.LatestLaunchStatus = launch.Status
			control := assessLaunchControl(caps.TaskID, latestPacket, &launch)
			status.LaunchControlState = control.State
			status.LaunchRetryDisposition = control.RetryDisposition
			status.LaunchControlReason = control.Reason
		} else if !errors.Is(err, sql.ErrNoRows) {
			return StatusTaskResult{}, err
		} else {
			control := assessLaunchControl(caps.TaskID, latestPacket, nil)
			status.LaunchControlState = control.State
			status.LaunchRetryDisposition = control.RetryDisposition
			status.LaunchControlReason = control.Reason
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}

	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else {
		applyRecoveryAssessmentToStatus(&status, c.recoveryFromContinueAssessment(assessment), checkpointResumable)
	}

	events, err := c.store.Proofs().ListByTask(caps.TaskID, 1)
	if err == nil && len(events) > 0 {
		last := events[len(events)-1]
		status.LastEventID = last.EventID
		status.LastEventType = last.Type
		status.LastEventAt = last.Timestamp
	} else if err != nil {
		return StatusTaskResult{}, err
	}

	return status, nil
}

func (c *Coordinator) InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error) {
	caps, err := c.store.Capsules().Get(common.TaskID(taskID))
	if err != nil {
		return InspectTaskResult{}, err
	}
	out := InspectTaskResult{
		TaskID: caps.TaskID,
		RepoAnchor: anchorgit.Snapshot{
			RepoRoot:         caps.RepoRoot,
			Branch:           caps.BranchName,
			HeadSHA:          caps.HeadSHA,
			WorkingTreeDirty: caps.WorkingTreeDirty,
			CapturedAt:       caps.AnchorCapturedAt,
		},
	}

	if in, err := c.store.Intents().LatestByTask(caps.TaskID); err == nil {
		inCopy := in
		out.Intent = &inCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if caps.CurrentBriefID != "" {
		b, err := c.store.Briefs().Get(caps.CurrentBriefID)
		if err == nil {
			briefCopy := b
			out.Brief = &briefCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
	}

	if latestRun, err := c.store.Runs().LatestByTask(caps.TaskID); err == nil {
		runCopy := latestRun
		out.Run = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}

	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(caps.TaskID); err == nil {
		cpCopy := latestCheckpoint
		out.Checkpoint = &cpCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestHandoff, err := c.store.Handoffs().LatestByTask(caps.TaskID); err == nil {
		packetCopy := latestHandoff
		out.Handoff = &packetCopy
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(latestHandoff.HandoffID); err == nil {
			launchCopy := latestLaunch
			out.Launch = &launchCopy
			control := assessLaunchControl(caps.TaskID, out.Handoff, out.Launch)
			out.LaunchControl = &control
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		} else {
			control := assessLaunchControl(caps.TaskID, out.Handoff, nil)
			out.LaunchControl = &control
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			out.Acknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return InspectTaskResult{}, err
	} else {
		recovery := c.recoveryFromContinueAssessment(assessment)
		out.Recovery = &recovery
	}

	return out, nil
}

func (c *Coordinator) reconcileStaleRun(ctx context.Context, caps capsule.WorkCapsule, latestRun rundomain.ExecutionRun, out *ContinueTaskResult) error {
	now := c.clock()
	latestRun.Status = rundomain.StatusInterrupted
	latestRun.InterruptionReason = "stale RUNNING reconciled during continue: no active execution handle"
	latestRun.LastKnownSummary = "Reconciled stale RUNNING run as INTERRUPTED"
	latestRun.EndedAt = &now
	latestRun.UpdatedAt = now
	if err := c.store.Runs().Update(latestRun); err != nil {
		return err
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhasePaused
	caps.NextAction = "A stale RUNNING run was reconciled to INTERRUPTED. Review evidence and restart execution when ready."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventRunInterrupted, proof.ActorSystem, "tuku-daemon", map[string]any{
		"run_id":              latestRun.RunID,
		"reason":              latestRun.InterruptionReason,
		"reconciliation":      true,
		"previous_run_status": rundomain.StatusRunning,
	}, &latestRun.RunID); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "stale running run reconciled on continue",
	}, &latestRun.RunID); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, latestRun.RunID, checkpoint.TriggerInterruption, true, "Stale RUNNING run reconciled to INTERRUPTED; resumable from paused state.")
	if err != nil {
		return err
	}

	canonical := fmt.Sprintf(
		"I found run %s still marked RUNNING but no active execution handle was present. I reconciled it as INTERRUPTED and created resumable checkpoint %s. Continue by starting a new bounded run from brief %s.",
		latestRun.RunID,
		cp.CheckpointID,
		caps.CurrentBriefID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeStaleReconciled,
		"run_id":        latestRun.RunID,
		"checkpoint_id": cp.CheckpointID,
	}, &latestRun.RunID); err != nil {
		return err
	}

	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeStaleReconciled,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		RunID:             latestRun.RunID,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeSafe,
		RecoveryClass:     RecoveryClassInterruptedRunRecoverable,
		RecommendedAction: RecoveryActionResumeInterrupted,
		ReadyForNextRun:   true,
		Reason:            fmt.Sprintf("stale run %s was reconciled and is now recoverable from checkpoint %s", latestRun.RunID, cp.CheckpointID),
		CheckpointID:      cp.CheckpointID,
		RunID:             latestRun.RunID,
	})
	return nil
}

func (c *Coordinator) blockedContinueByDrift(_ context.Context, caps capsule.WorkCapsule, drift checkpoint.DriftClass, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseAwaitingDecision
	caps.NextAction = "Direct resume is blocked by major repo drift. Re-anchor or create a new brief before executing."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "major anchor drift blocked resume",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, "Major repo drift detected. Direct resume blocked pending user decision.")
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Direct resume is not safe. I detected major repo drift versus the last continuity anchor, so I blocked automatic resume and recorded checkpoint %s for decision review.",
		cp.CheckpointID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeBlockedDrift,
		"drift_class":   drift,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeBlockedDrift,
		DriftClass:        drift,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeBlockedDrift,
		RecoveryClass:     RecoveryClassBlockedDrift,
		RecommendedAction: RecoveryActionMakeResumeDecision,
		DriftClass:        drift,
		RequiresDecision:  true,
		Reason:            "repository drift blocks automatic recovery",
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) blockedContinueByInconsistency(_ context.Context, caps capsule.WorkCapsule, reason string, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseBlocked
	caps.NextAction = "Continuity state is inconsistent. Re-anchor state or regenerate intent/brief before continuing."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "continue blocked by inconsistent continuity state",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, fmt.Sprintf("Continue blocked by inconsistent continuity state: %s", reason))
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Resume is blocked because continuity state is inconsistent: %s. I recorded checkpoint %s for explicit recovery decisions.",
		reason,
		cp.CheckpointID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeBlockedInconsistent,
		"reason":        reason,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeBlockedInconsistent,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeBlockedInconsistent,
		RecoveryClass:     RecoveryClassRepairRequired,
		RecommendedAction: RecoveryActionRepairContinuity,
		RequiresRepair:    true,
		Reason:            reason,
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) awaitDecisionOnContinue(_ context.Context, caps capsule.WorkCapsule, drift checkpoint.DriftClass, out *ContinueTaskResult) error {
	now := c.clock()
	caps.Version++
	caps.UpdatedAt = now
	caps.CurrentPhase = phase.PhaseAwaitingDecision
	caps.NextAction = "Minor repo drift detected. Confirm whether to continue with the existing brief or regenerate intent/brief."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
		"phase":  caps.CurrentPhase,
		"reason": "minor anchor drift requires decision",
	}, nil); err != nil {
		return err
	}
	cp, err := c.createCheckpoint(caps, "", checkpoint.TriggerAwaitingDecision, false, "Minor repo drift detected. Awaiting explicit decision before resume.")
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"I found minor repo drift since the last checkpoint. I paused at decision state and created checkpoint %s. Confirm whether to continue with brief %s or regenerate the brief.",
		cp.CheckpointID,
		caps.CurrentBriefID,
	)
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":       ContinueOutcomeNeedsDecision,
		"drift_class":   drift,
		"checkpoint_id": cp.CheckpointID,
	}, nil); err != nil {
		return err
	}
	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeNeedsDecision,
		DriftClass:        drift,
		Phase:             caps.CurrentPhase,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, RecoveryAssessment{
		TaskID:            caps.TaskID,
		ContinuityOutcome: ContinueOutcomeNeedsDecision,
		RecoveryClass:     RecoveryClassDecisionRequired,
		RecommendedAction: RecoveryActionMakeResumeDecision,
		DriftClass:        drift,
		RequiresDecision:  true,
		Reason:            "resume requires an explicit operator decision",
		CheckpointID:      cp.CheckpointID,
	})
	return nil
}

func (c *Coordinator) safeContinue(_ context.Context, caps capsule.WorkCapsule, hasCheckpoint bool, latestCheckpoint checkpoint.Checkpoint, hasRun bool, latestRun rundomain.ExecutionRun, recovery RecoveryAssessment, out *ContinueTaskResult) error {
	now := c.clock()
	if caps.CurrentBriefID == "" {
		canonical := "Resume is blocked because no execution brief exists for this task. Send a task message to compile intent and generate a brief first."
		if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
			"outcome": ContinueOutcomeBlockedInconsistent,
			"reason":  "missing_brief",
		}, nil); err != nil {
			return err
		}
		*out = ContinueTaskResult{
			TaskID:            caps.TaskID,
			Outcome:           ContinueOutcomeBlockedInconsistent,
			DriftClass:        checkpoint.DriftNone,
			Phase:             caps.CurrentPhase,
			CanonicalResponse: canonical,
		}
		return nil
	}
	if _, err := c.store.Briefs().Get(caps.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			canonical := "Resume is blocked because capsule state references a missing execution brief. Recompile intent to restore continuity."
			if emitErr := c.emitCanonicalConversation(caps, canonical, map[string]any{
				"outcome": ContinueOutcomeBlockedInconsistent,
				"reason":  "brief_pointer_missing",
			}, nil); emitErr != nil {
				return emitErr
			}
			*out = ContinueTaskResult{
				TaskID:            caps.TaskID,
				Outcome:           ContinueOutcomeBlockedInconsistent,
				DriftClass:        checkpoint.DriftNone,
				Phase:             caps.CurrentPhase,
				CanonicalResponse: canonical,
			}
			return nil
		}
		return err
	}

	runID := common.RunID("")
	if hasRun {
		runID = latestRun.RunID
	}

	caps.Version++
	caps.UpdatedAt = now
	caps.NextAction = "Resume is safe. Start the next bounded run when ready."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}

	descriptor := "Safe resume available from current capsule and checkpoint state."
	trigger := checkpoint.TriggerContinue
	if hasCheckpoint {
		descriptor = fmt.Sprintf("Safe resume confirmed from checkpoint %s.", latestCheckpoint.CheckpointID)
	}
	cp, err := c.createCheckpoint(caps, runID, trigger, true, descriptor)
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Safe resume is available. Use checkpoint %s with brief %s on branch %s (head %s) to continue with a bounded run.",
		cp.CheckpointID,
		caps.CurrentBriefID,
		caps.BranchName,
		caps.HeadSHA,
	)
	if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
		canonical = fmt.Sprintf(
			"Interrupted execution is recoverable. Use checkpoint %s with brief %s on branch %s (head %s) to start the next bounded run.",
			cp.CheckpointID,
			caps.CurrentBriefID,
			caps.BranchName,
			caps.HeadSHA,
		)
	}
	if err := c.emitCanonicalConversation(caps, canonical, map[string]any{
		"outcome":            ContinueOutcomeSafe,
		"checkpoint_id":      cp.CheckpointID,
		"brief_id":           caps.CurrentBriefID,
		"recovery_class":     recovery.RecoveryClass,
		"recommended_action": recovery.RecommendedAction,
	}, runIDPointer(runID)); err != nil {
		return err
	}

	*out = ContinueTaskResult{
		TaskID:            caps.TaskID,
		Outcome:           ContinueOutcomeSafe,
		DriftClass:        checkpoint.DriftNone,
		Phase:             caps.CurrentPhase,
		RunID:             runID,
		CheckpointID:      cp.CheckpointID,
		ResumeDescriptor:  cp.ResumeDescriptor,
		CanonicalResponse: canonical,
	}
	applyRecoveryAssessmentToContinueResult(out, recovery)
	return nil
}

func (c *Coordinator) createCheckpoint(caps capsule.WorkCapsule, runID common.RunID, trigger checkpoint.Trigger, resumable bool, descriptor string) (checkpoint.Checkpoint, error) {
	return c.createCheckpointWithOptions(caps, runID, trigger, resumable, descriptor, true)
}

func (c *Coordinator) createCheckpointWithOptions(caps capsule.WorkCapsule, runID common.RunID, trigger checkpoint.Trigger, resumable bool, descriptor string, emitProof bool) (checkpoint.Checkpoint, error) {
	lastEventID, err := c.latestProofEventID(caps.TaskID)
	if err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if strings.TrimSpace(descriptor) == "" {
		descriptor = "Checkpoint captured for continuity."
	}
	cp := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID(c.idGenerator("chk")),
		TaskID:             caps.TaskID,
		RunID:              runID,
		CreatedAt:          c.clock(),
		Trigger:            trigger,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEventID,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   descriptor,
		IsResumable:        resumable,
	}
	if err := c.store.Checkpoints().Create(cp); err != nil {
		return checkpoint.Checkpoint{}, err
	}
	if emitProof {
		if err := c.appendCheckpointCreatedProof(caps, cp, runIDPointer(runID)); err != nil {
			return checkpoint.Checkpoint{}, err
		}
	}
	return cp, nil
}

func (c *Coordinator) appendCheckpointCreatedProof(caps capsule.WorkCapsule, cp checkpoint.Checkpoint, runID *common.RunID) error {
	checkpointID := cp.CheckpointID
	event := proof.Event{
		EventID:        common.EventID(c.idGenerator("evt")),
		TaskID:         caps.TaskID,
		RunID:          runID,
		CheckpointID:   &checkpointID,
		Timestamp:      c.clock(),
		Type:           proof.EventCheckpointCreated,
		ActorType:      proof.ActorSystem,
		ActorID:        "tuku-daemon",
		PayloadJSON:    mustJSON(map[string]any{"checkpoint_id": cp.CheckpointID, "trigger": cp.Trigger, "resumable": cp.IsResumable, "descriptor": cp.ResumeDescriptor}),
		CapsuleVersion: caps.Version,
	}
	return c.store.Proofs().Append(event)
}

func (c *Coordinator) latestProofEventID(taskID common.TaskID) (common.EventID, error) {
	events, err := c.store.Proofs().ListByTask(taskID, 1)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	return events[len(events)-1].EventID, nil
}

func anchorFromCapsule(caps capsule.WorkCapsule) checkpoint.RepoAnchor {
	return checkpoint.RepoAnchor{
		RepoRoot:      caps.RepoRoot,
		WorktreePath:  caps.WorktreePath,
		BranchName:    caps.BranchName,
		HeadSHA:       caps.HeadSHA,
		DirtyHash:     boolString(caps.WorkingTreeDirty),
		UntrackedHash: "",
	}
}

func classifyAnchorDrift(baseline checkpoint.RepoAnchor, current anchorgit.Snapshot) checkpoint.DriftClass {
	if strings.TrimSpace(current.RepoRoot) == "" {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.RepoRoot) != "" && filepath.Clean(baseline.RepoRoot) != filepath.Clean(current.RepoRoot) {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.WorktreePath) != "" && filepath.Clean(baseline.WorktreePath) != filepath.Clean(current.RepoRoot) {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.BranchName) != "" && strings.TrimSpace(current.Branch) != "" && baseline.BranchName != current.Branch {
		return checkpoint.DriftMajor
	}
	if strings.TrimSpace(baseline.HeadSHA) != "" && strings.TrimSpace(current.HeadSHA) != "" && baseline.HeadSHA != current.HeadSHA {
		return checkpoint.DriftMinor
	}
	if strings.TrimSpace(baseline.DirtyHash) != "" && baseline.DirtyHash != boolString(current.WorkingTreeDirty) {
		return checkpoint.DriftMinor
	}
	return checkpoint.DriftNone
}

func runIDPointer(runID common.RunID) *common.RunID {
	if runID == "" {
		return nil
	}
	id := runID
	return &id
}

func (c *Coordinator) withTx(fn func(txc *Coordinator) error) error {
	return c.store.WithTx(func(txStore storage.Store) error {
		txc := *c
		txc.store = txStore
		return fn(&txc)
	})
}

func (c *Coordinator) appendProof(caps capsule.WorkCapsule, eventType proof.EventType, actorType proof.ActorType, actorID string, payload map[string]any, runID *common.RunID) error {
	e := proof.Event{
		EventID:        common.EventID(c.idGenerator("evt")),
		TaskID:         caps.TaskID,
		RunID:          runID,
		Timestamp:      c.clock(),
		Type:           eventType,
		ActorType:      actorType,
		ActorID:        actorID,
		PayloadJSON:    mustJSON(payload),
		CapsuleVersion: caps.Version,
	}
	return c.store.Proofs().Append(e)
}

func (c *Coordinator) emitCanonicalConversation(caps capsule.WorkCapsule, canonicalText string, payload map[string]any, runID *common.RunID) error {
	systemMsg := conversation.Message{
		MessageID:      common.MessageID(c.idGenerator("msg")),
		ConversationID: caps.ConversationID,
		TaskID:         caps.TaskID,
		Role:           conversation.RoleSystem,
		Body:           canonicalText,
		CreatedAt:      c.clock(),
	}
	if err := c.store.Conversations().Append(systemMsg); err != nil {
		return err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["message_id"] = systemMsg.MessageID
	return c.appendProof(caps, proof.EventCanonicalResponseEmitted, proof.ActorSystem, "tuku-daemon", payload, runID)
}

func agentsMetadata(repoRoot string) (checksum string, instructions string) {
	path := filepath.Join(repoRoot, "AGENTS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	sum := sha256.Sum256(data)
	checksum = hexString(sum[:])
	lines := strings.Split(string(data), "\n")
	selected := make([]string, 0, 6)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		selected = append(selected, line)
		if len(selected) >= 6 {
			break
		}
	}
	return checksum, strings.Join(selected, " | ")
}

func boolString(v bool) string {
	if v {
		return "dirty"
	}
	return "clean"
}

func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "...(truncated)"
}

func hexString(bytes []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(bytes)*2)
	for i, b := range bytes {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
```

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_launch.go
```go
package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
)

type HandoffLaunchStatus string

const (
	// HandoffLaunchStatusBlocked means Tuku intentionally refused launch before adapter invocation.
	HandoffLaunchStatusBlocked HandoffLaunchStatus = "BLOCKED"
	// HandoffLaunchStatusCompleted means launch invocation completed, not downstream coding completion.
	HandoffLaunchStatusCompleted HandoffLaunchStatus = "COMPLETED"
	// HandoffLaunchStatusFailed means adapter invocation was attempted but failed.
	HandoffLaunchStatusFailed HandoffLaunchStatus = "FAILED"
)

type LaunchHandoffRequest struct {
	TaskID       string
	HandoffID    string
	TargetWorker rundomain.WorkerKind
}

type LaunchHandoffResult struct {
	TaskID            common.TaskID
	HandoffID         string
	TargetWorker      rundomain.WorkerKind
	LaunchStatus      HandoffLaunchStatus
	LaunchID          string
	CanonicalResponse string
	Payload           *handoff.LaunchPayload
}

type preparedHandoffLaunch struct {
	TaskID  common.TaskID
	Packet  handoff.Packet
	Payload handoff.LaunchPayload
	Launch  handoff.Launch
}

func (c *Coordinator) LaunchHandoff(ctx context.Context, req LaunchHandoffRequest) (LaunchHandoffResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return LaunchHandoffResult{}, fmt.Errorf("task id is required")
	}
	if c.handoffLauncher == nil {
		return c.recordHandoffLaunchBlockedWithoutAdapter(taskID, req)
	}

	prepared, blocked, err := c.prepareHandoffLaunch(ctx, taskID, req)
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	if blocked != nil {
		return *blocked, nil
	}

	launchReq := adapter_contract.HandoffLaunchRequest{
		TaskID:       prepared.TaskID,
		HandoffID:    prepared.Packet.HandoffID,
		SourceWorker: adapterWorkerKind(prepared.Packet.SourceWorker),
		TargetWorker: adapterWorkerKind(prepared.Packet.TargetWorker),
		Payload:      prepared.Payload,
	}

	var launchOut adapter_contract.HandoffLaunchResult
	var launchErr error
	launchOut, launchErr = c.handoffLauncher.LaunchHandoff(ctx, launchReq)

	result, err := c.finalizeHandoffLaunch(prepared, launchOut, launchErr)
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) prepareHandoffLaunch(ctx context.Context, taskID common.TaskID, req LaunchHandoffRequest) (*preparedHandoffLaunch, *LaunchHandoffResult, error) {
	var prepared preparedHandoffLaunch
	var blocked *LaunchHandoffResult

	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}

		packet, err := txc.resolveLaunchPacket(taskID, strings.TrimSpace(req.HandoffID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, "", "handoff packet not found for this task")
				if blockErr != nil {
					return blockErr
				}
				blocked = &out
				return nil
			}
			return err
		}
		if packet.TaskID != taskID {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("handoff task mismatch: packet task=%s request task=%s", packet.TaskID, taskID))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}
		if req.TargetWorker != "" && req.TargetWorker != packet.TargetWorker {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("requested target worker %s does not match packet target %s", req.TargetWorker, packet.TargetWorker))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}
		if packet.TargetWorker != rundomain.WorkerKindClaude {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("unsupported handoff launch target: %s", packet.TargetWorker))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}
		switch packet.Status {
		case handoff.StatusCreated, handoff.StatusAccepted:
		default:
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("handoff %s is not launchable in status %s", packet.HandoffID, packet.Status))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}

		if prior, ok, err := txc.tryReusePriorLaunchOutcome(taskID, packet); err != nil {
			return err
		} else if ok {
			blocked = &prior
			return nil
		}

		assessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		if reason, err := txc.validateLaunchSafety(packet, assessment); err != nil {
			return err
		} else if reason != "" {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, reason)
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}

		briefRec, err := txc.store.Briefs().Get(packet.BriefID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("handoff brief %s is missing", packet.BriefID))
				if blockErr != nil {
					return blockErr
				}
				blocked = &out
				return nil
			}
			return err
		}

		runSummary, runStatus, runID := txc.resolveLaunchRunSummary(packet, taskID)
		payload := txc.materializeLaunchPayload(packet, briefRec, runSummary, runStatus, runID)
		payloadHash := hashLaunchPayload(payload)
		launch := txc.buildRequestedLaunch(taskID, packet, payloadHash)
		if err := txc.store.Handoffs().CreateLaunch(launch); err != nil {
			return err
		}

		launchRunID := runIDPointer(payload.LatestRunID)
		proofPayload := map[string]any{
			"handoff_id":          packet.HandoffID,
			"target_worker":       packet.TargetWorker,
			"source_worker":       packet.SourceWorker,
			"checkpoint_id":       packet.CheckpointID,
			"brief_id":            packet.BriefID,
			"launch_attempt_id":   launch.AttemptID,
			"launch_payload_hash": payloadHash,
		}
		if err := txc.appendProof(caps, proof.EventHandoffLaunchRequested, proof.ActorSystem, "tuku-daemon", proofPayload, launchRunID); err != nil {
			return err
		}
		canonical := fmt.Sprintf(
			"I prepared Claude handoff launch for packet %s. The launch payload is anchored to checkpoint %s and brief %s.",
			packet.HandoffID,
			packet.CheckpointID,
			packet.BriefID,
		)
		if err := txc.emitCanonicalConversation(caps, canonical, proofPayload, launchRunID); err != nil {
			return err
		}

		prepared = preparedHandoffLaunch{
			TaskID:  taskID,
			Packet:  packet,
			Payload: payload,
			Launch:  launch,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if blocked != nil {
		return nil, blocked, nil
	}
	return &prepared, nil, nil
}

func (c *Coordinator) finalizeHandoffLaunch(prepared *preparedHandoffLaunch, launchOut adapter_contract.HandoffLaunchResult, launchErr error) (LaunchHandoffResult, error) {
	var result LaunchHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(prepared.TaskID)
		if err != nil {
			return err
		}
		launchRec, err := txc.store.Handoffs().GetLaunch(prepared.Launch.AttemptID)
		if err != nil {
			return err
		}
		if launchRec.Status != handoff.LaunchStatusRequested {
			return fmt.Errorf("launch attempt %s is not REQUESTED during finalization (status=%s)", launchRec.AttemptID, launchRec.Status)
		}
		runID := runIDPointer(prepared.Payload.LatestRunID)
		target := prepared.Packet.TargetWorker
		if target == "" {
			target = prepared.Payload.TargetWorker
		}

		if launchErr != nil {
			launchRec.Status = handoff.LaunchStatusFailed
			launchRec.LaunchID = strings.TrimSpace(launchOut.LaunchID)
			launchRec.StartedAt = launchOut.StartedAt
			launchRec.EndedAt = launchOut.EndedAt
			launchRec.Command = launchOut.Command
			launchRec.Args = append([]string{}, launchOut.Args...)
			launchRec.Summary = launchOut.Summary
			launchRec.ErrorMessage = launchErr.Error()
			launchRec.OutputArtifactRef = launchOut.OutputArtifactRef
			launchRec.UpdatedAt = txc.clock()
			if launchOut.ExitCode != 0 {
				exitCode := launchOut.ExitCode
				launchRec.ExitCode = &exitCode
			}
			if err := txc.store.Handoffs().UpdateLaunch(launchRec); err != nil {
				return err
			}
			payload := map[string]any{
				"handoff_id":           prepared.Packet.HandoffID,
				"target_worker":        target,
				"launch_attempt_id":    launchRec.AttemptID,
				"launch_id":            launchOut.LaunchID,
				"error":                launchErr.Error(),
				"started_at":           launchOut.StartedAt,
				"ended_at":             launchOut.EndedAt,
				"command":              launchOut.Command,
				"args":                 launchOut.Args,
				"exit_code":            launchOut.ExitCode,
				"stdout_excerpt":       truncate(launchOut.Stdout, 1200),
				"stderr_excerpt":       truncate(launchOut.Stderr, 1200),
				"summary":              launchOut.Summary,
				"launch_scope":         "launcher invocation only",
				"completion_semantics": "invocation attempted; downstream coding completion not observed by Tuku",
			}
			if err := txc.appendProof(caps, proof.EventHandoffLaunchFailed, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
				return err
			}
			canonical := fmt.Sprintf(
				"Claude handoff launch failed for packet %s: %s. No execution worker state was mutated; review the launch evidence and retry.",
				prepared.Packet.HandoffID,
				launchErr.Error(),
			)
			if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
				return err
			}
			result = LaunchHandoffResult{
				TaskID:            prepared.TaskID,
				HandoffID:         prepared.Packet.HandoffID,
				TargetWorker:      target,
				LaunchStatus:      HandoffLaunchStatusFailed,
				LaunchID:          launchRec.LaunchID,
				CanonicalResponse: canonical,
				Payload:           &prepared.Payload,
			}
			return nil
		}

		launchRec.Status = handoff.LaunchStatusCompleted
		launchRec.LaunchID = strings.TrimSpace(launchOut.LaunchID)
		launchRec.StartedAt = launchOut.StartedAt
		launchRec.EndedAt = launchOut.EndedAt
		launchRec.Command = launchOut.Command
		launchRec.Args = append([]string{}, launchOut.Args...)
		launchRec.Summary = launchOut.Summary
		launchRec.ErrorMessage = ""
		launchRec.OutputArtifactRef = launchOut.OutputArtifactRef
		launchRec.UpdatedAt = txc.clock()
		if launchOut.ExitCode != 0 {
			exitCode := launchOut.ExitCode
			launchRec.ExitCode = &exitCode
		} else {
			launchRec.ExitCode = nil
		}
		if err := txc.store.Handoffs().UpdateLaunch(launchRec); err != nil {
			return err
		}

		payload := map[string]any{
			"handoff_id":           prepared.Packet.HandoffID,
			"target_worker":        target,
			"launch_attempt_id":    launchRec.AttemptID,
			"launch_id":            launchRec.LaunchID,
			"started_at":           launchOut.StartedAt,
			"ended_at":             launchOut.EndedAt,
			"command":              launchOut.Command,
			"args":                 launchOut.Args,
			"exit_code":            launchOut.ExitCode,
			"summary":              launchOut.Summary,
			"output_artifact_ref":  launchOut.OutputArtifactRef,
			"launch_scope":         "launcher invocation only",
			"completion_semantics": "launch request submitted; downstream coding completion not observed by Tuku",
		}

		ack := txc.buildLaunchAcknowledgment(prepared.TaskID, prepared.Packet.HandoffID, target, launchOut)
		if err := txc.store.Handoffs().SaveAcknowledgment(ack); err != nil {
			return err
		}
		ackPayload := map[string]any{
			"ack_id":        ack.AckID,
			"handoff_id":    ack.HandoffID,
			"launch_id":     ack.LaunchID,
			"target_worker": ack.TargetWorker,
			"status":        ack.Status,
			"summary":       ack.Summary,
			"unknowns":      append([]string{}, ack.Unknowns...),
			"timestamp":     ack.CreatedAt,
		}
		ackEvent := proof.EventHandoffAcknowledgmentCaptured
		if ack.Status == handoff.AcknowledgmentUnavailable {
			ackEvent = proof.EventHandoffAcknowledgmentUnavailable
		}
		if err := txc.appendProof(caps, ackEvent, proof.ActorSystem, "tuku-daemon", ackPayload, runID); err != nil {
			return err
		}
		payload["ack_id"] = ack.AckID
		payload["ack_status"] = ack.Status
		payload["ack_summary"] = ack.Summary
		payload["ack_unknowns"] = append([]string{}, ack.Unknowns...)

		if err := txc.appendProof(caps, proof.EventHandoffLaunchCompleted, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}
		canonical := txc.buildLaunchCanonicalSuccess(prepared.Packet.HandoffID, launchOut.LaunchID, ack)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}
		result = LaunchHandoffResult{
			TaskID:            prepared.TaskID,
			HandoffID:         prepared.Packet.HandoffID,
			TargetWorker:      target,
			LaunchStatus:      HandoffLaunchStatusCompleted,
			LaunchID:          launchRec.LaunchID,
			CanonicalResponse: canonical,
			Payload:           &prepared.Payload,
		}
		return nil
	})
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) resolveLaunchPacket(taskID common.TaskID, handoffID string) (handoff.Packet, error) {
	if handoffID == "" {
		return c.store.Handoffs().LatestByTask(taskID)
	}
	return c.store.Handoffs().Get(handoffID)
}

func (c *Coordinator) tryReusePriorLaunchOutcome(taskID common.TaskID, packet handoff.Packet) (LaunchHandoffResult, bool, error) {
	launch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LaunchHandoffResult{}, false, nil
		}
		return LaunchHandoffResult{}, false, err
	}

	control := assessLaunchControl(taskID, &packet, &launch)
	switch control.State {
	case LaunchControlStateNotRequested, LaunchControlStateFailed:
		return LaunchHandoffResult{}, false, nil
	case LaunchControlStateCompleted:
		ack, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return LaunchHandoffResult{
					TaskID:            taskID,
					HandoffID:         packet.HandoffID,
					TargetWorker:      packet.TargetWorker,
					LaunchStatus:      HandoffLaunchStatusBlocked,
					CanonicalResponse: fmt.Sprintf("Launch for handoff %s is inconsistent: Tuku has a durable completion record for launch attempt %s but no persisted acknowledgment. Automatic retry is blocked until continuity is repaired.", packet.HandoffID, launch.AttemptID),
				}, true, nil
			}
			return LaunchHandoffResult{}, false, err
		}
		return LaunchHandoffResult{
			TaskID:            taskID,
			HandoffID:         packet.HandoffID,
			TargetWorker:      packet.TargetWorker,
			LaunchStatus:      HandoffLaunchStatusCompleted,
			LaunchID:          launch.LaunchID,
			CanonicalResponse: c.buildLaunchCanonicalSuccess(packet.HandoffID, launch.LaunchID, ack),
		}, true, nil
	case LaunchControlStateRequestedOutcomeUnknown:
		result := buildReplayBlockedLaunchResponse(packet)
		return result, true, nil
	}
	return LaunchHandoffResult{}, false, nil
}

func (c *Coordinator) validateLaunchSafety(packet handoff.Packet, assessment continueAssessment) (string, error) {
	switch assessment.Outcome {
	case ContinueOutcomeSafe:
	case ContinueOutcomeStaleReconciled:
		return "handoff launch blocked because a stale RUNNING execution state requires reconciliation first", nil
	case ContinueOutcomeBlockedInconsistent:
		return fmt.Sprintf("handoff launch blocked by inconsistent continuity state: %s", assessment.Reason), nil
	case ContinueOutcomeBlockedDrift:
		return "handoff launch blocked by major repository drift", nil
	case ContinueOutcomeNeedsDecision:
		return "handoff launch blocked while task is in decision-gated continuity state", nil
	default:
		return fmt.Sprintf("handoff launch blocked by unsupported continuity outcome: %s", assessment.Outcome), nil
	}

	if packet.BriefID == "" {
		return "handoff launch blocked because packet brief reference is empty", nil
	}
	if packet.CheckpointID == "" {
		return "handoff launch blocked because packet checkpoint reference is empty", nil
	}
	if !packet.IsResumable {
		return "handoff launch blocked because packet checkpoint is not resumable", nil
	}
	cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Sprintf("handoff launch blocked because checkpoint %s is missing", packet.CheckpointID), nil
		}
		return "", err
	}
	if cp.TaskID != assessment.TaskID {
		return fmt.Sprintf("handoff launch blocked because checkpoint %s belongs to task %s", packet.CheckpointID, cp.TaskID), nil
	}
	if !cp.IsResumable {
		return fmt.Sprintf("handoff launch blocked because checkpoint %s is not resumable", packet.CheckpointID), nil
	}
	if packet.BriefID != "" && cp.BriefID != "" && packet.BriefID != cp.BriefID {
		return fmt.Sprintf("handoff launch blocked because packet brief %s does not match checkpoint brief %s", packet.BriefID, cp.BriefID), nil
	}
	if assessment.LatestRun != nil && assessment.LatestRun.Status == rundomain.StatusRunning {
		return "handoff launch blocked because an execution run is still marked RUNNING", nil
	}
	if _, err := c.store.Briefs().Get(packet.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Sprintf("handoff launch blocked because brief %s is missing", packet.BriefID), nil
		}
		return "", err
	}
	return "", nil
}

func (c *Coordinator) resolveLaunchRunSummary(packet handoff.Packet, taskID common.TaskID) (string, rundomain.Status, common.RunID) {
	if packet.LatestRunID != "" {
		if runRec, err := c.store.Runs().Get(packet.LatestRunID); err == nil {
			return runRec.LastKnownSummary, runRec.Status, runRec.RunID
		}
	}
	if latest, err := c.store.Runs().LatestByTask(taskID); err == nil {
		return latest.LastKnownSummary, latest.Status, latest.RunID
	}
	return "", packet.LatestRunStatus, packet.LatestRunID
}

func (c *Coordinator) materializeLaunchPayload(packet handoff.Packet, b brief.ExecutionBrief, runSummary string, runStatus rundomain.Status, runID common.RunID) handoff.LaunchPayload {
	return handoff.LaunchPayload{
		Version:          1,
		TaskID:           packet.TaskID,
		HandoffID:        packet.HandoffID,
		SourceWorker:     packet.SourceWorker,
		TargetWorker:     packet.TargetWorker,
		HandoffMode:      packet.HandoffMode,
		CurrentPhase:     packet.CurrentPhase,
		CheckpointID:     packet.CheckpointID,
		BriefID:          b.BriefID,
		IntentID:         packet.IntentID,
		CapsuleVersion:   packet.CapsuleVersion,
		RepoAnchor:       packet.RepoAnchor,
		IsResumable:      packet.IsResumable,
		ResumeDescriptor: packet.ResumeDescriptor,
		LatestRunID:      runID,
		LatestRunStatus:  runStatus,
		LatestRunSummary: strings.TrimSpace(runSummary),
		Goal:             packet.Goal,
		BriefObjective:   b.Objective,
		NormalizedAction: b.NormalizedAction,
		Constraints:      append([]string{}, b.Constraints...),
		DoneCriteria:     append([]string{}, b.DoneCriteria...),
		TouchedFiles:     append([]string{}, packet.TouchedFiles...),
		Blockers:         append([]string{}, packet.Blockers...),
		NextAction:       packet.NextAction,
		Unknowns:         append([]string{}, packet.Unknowns...),
		HandoffNotes:     append([]string{}, packet.HandoffNotes...),
		GeneratedAt:      c.clock(),
	}
}

func hashLaunchPayload(payload handoff.LaunchPayload) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hexString(sum[:8])
}

func (c *Coordinator) recordHandoffLaunchBlocked(caps capsule.WorkCapsule, req LaunchHandoffRequest, packetTarget rundomain.WorkerKind, reason string) (LaunchHandoffResult, error) {
	target := resolveBlockedLaunchTarget(req.TargetWorker, packetTarget)
	payload := map[string]any{
		"handoff_id":    strings.TrimSpace(req.HandoffID),
		"target_worker": target,
		"reason":        strings.TrimSpace(reason),
	}
	if err := c.appendProof(caps, proof.EventHandoffLaunchBlocked, proof.ActorSystem, "tuku-daemon", payload, nil); err != nil {
		return LaunchHandoffResult{}, err
	}
	canonical := fmt.Sprintf("Handoff launch is blocked: %s", strings.TrimSpace(reason))
	if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
		return LaunchHandoffResult{}, err
	}
	return LaunchHandoffResult{
		TaskID:            caps.TaskID,
		HandoffID:         strings.TrimSpace(req.HandoffID),
		TargetWorker:      target,
		LaunchStatus:      HandoffLaunchStatusBlocked,
		CanonicalResponse: canonical,
	}, nil
}

func (c *Coordinator) recordHandoffLaunchBlockedWithoutAdapter(taskID common.TaskID, req LaunchHandoffRequest) (LaunchHandoffResult, error) {
	var out LaunchHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		blocked, err := txc.recordHandoffLaunchBlocked(caps, req, "", "Claude handoff launcher is not configured")
		if err != nil {
			return err
		}
		out = blocked
		return nil
	})
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	return out, nil
}

func adapterWorkerKind(kind rundomain.WorkerKind) adapter_contract.WorkerKind {
	switch kind {
	case rundomain.WorkerKindClaude:
		return adapter_contract.WorkerClaude
	case rundomain.WorkerKindCodex:
		return adapter_contract.WorkerCodex
	default:
		return adapter_contract.WorkerUnknown
	}
}

func resolveBlockedLaunchTarget(reqTarget rundomain.WorkerKind, packetTarget rundomain.WorkerKind) rundomain.WorkerKind {
	if reqTarget != "" {
		return reqTarget
	}
	if packetTarget != "" {
		return packetTarget
	}
	return rundomain.WorkerKindClaude
}

func (c *Coordinator) buildLaunchAcknowledgment(taskID common.TaskID, handoffID string, target rundomain.WorkerKind, launchOut adapter_contract.HandoffLaunchResult) handoff.Acknowledgment {
	status := handoff.AcknowledgmentCaptured
	unknowns := []string{}
	summary := summarizeAcknowledgment(launchOut)
	if summary == "" {
		status = handoff.AcknowledgmentUnavailable
		summary = "No usable initial acknowledgment text was returned by the target worker."
		unknowns = append(unknowns, "Initial target-worker acknowledgment text was empty or unusable.")
	}
	unknowns = append(unknowns, "Acknowledgment alone does not prove downstream coding execution completed.")

	return handoff.Acknowledgment{
		Version:      1,
		AckID:        c.idGenerator("hak"),
		HandoffID:    handoffID,
		LaunchID:     strings.TrimSpace(launchOut.LaunchID),
		TaskID:       taskID,
		TargetWorker: target,
		Status:       status,
		Summary:      summary,
		Unknowns:     unknowns,
		CreatedAt:    c.clock(),
	}
}

func (c *Coordinator) buildRequestedLaunch(taskID common.TaskID, packet handoff.Packet, payloadHash string) handoff.Launch {
	now := c.clock()
	return handoff.Launch{
		Version:      1,
		AttemptID:    c.idGenerator("hlc"),
		HandoffID:    packet.HandoffID,
		TaskID:       taskID,
		TargetWorker: packet.TargetWorker,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  payloadHash,
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func summarizeAcknowledgment(launchOut adapter_contract.HandoffLaunchResult) string {
	if summary := strings.TrimSpace(launchOut.Summary); summary != "" {
		return truncate(summary, 280)
	}
	stdout := strings.TrimSpace(launchOut.Stdout)
	if stdout == "" {
		return ""
	}
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return truncate(line, 280)
	}
	return ""
}

func (c *Coordinator) buildLaunchCanonicalSuccess(handoffID, launchID string, ack handoff.Acknowledgment) string {
	if ack.Status == handoff.AcknowledgmentCaptured {
		return fmt.Sprintf(
			"Claude handoff launch invocation succeeded for packet %s (launch %s). I captured an initial acknowledgment: %s. Downstream coding execution is not yet proven complete.",
			handoffID,
			strings.TrimSpace(launchID),
			ack.Summary,
		)
	}
	return fmt.Sprintf(
		"Claude handoff launch invocation succeeded for packet %s (launch %s), but no usable initial acknowledgment was captured. Downstream coding execution is not yet proven complete.",
		handoffID,
		strings.TrimSpace(launchID),
	)
}
```

File: /Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go
```go
package ipc

import (
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/run"
)

type RepoAnchor struct {
	RepoRoot         string    `json:"repo_root"`
	Branch           string    `json:"branch"`
	HeadSHA          string    `json:"head_sha"`
	WorkingTreeDirty bool      `json:"working_tree_dirty"`
	CapturedAt       time.Time `json:"captured_at"`
}

type StartTaskRequest struct {
	Goal     string `json:"goal"`
	RepoRoot string `json:"repo_root"`
}

type StartTaskResponse struct {
	TaskID            common.TaskID         `json:"task_id"`
	ConversationID    common.ConversationID `json:"conversation_id"`
	Phase             phase.Phase           `json:"phase"`
	RepoAnchor        RepoAnchor            `json:"repo_anchor"`
	CanonicalResponse string                `json:"canonical_response"`
}

type ResolveShellTaskForRepoRequest struct {
	RepoRoot    string `json:"repo_root"`
	DefaultGoal string `json:"default_goal,omitempty"`
}

type ResolveShellTaskForRepoResponse struct {
	TaskID   common.TaskID `json:"task_id"`
	RepoRoot string        `json:"repo_root"`
	Created  bool          `json:"created"`
}

type TaskMessageRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Message string        `json:"message"`
}

type TaskMessageResponse struct {
	TaskID            common.TaskID  `json:"task_id"`
	Phase             phase.Phase    `json:"phase"`
	IntentClass       string         `json:"intent_class"`
	BriefID           common.BriefID `json:"brief_id"`
	BriefHash         string         `json:"brief_hash"`
	RepoAnchor        RepoAnchor     `json:"repo_anchor"`
	CanonicalResponse string         `json:"canonical_response"`
}

type TaskStatusRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskStatusResponse struct {
	TaskID                   common.TaskID         `json:"task_id"`
	ConversationID           common.ConversationID `json:"conversation_id"`
	Goal                     string                `json:"goal"`
	Phase                    phase.Phase           `json:"phase"`
	Status                   string                `json:"status"`
	CurrentIntentID          common.IntentID       `json:"current_intent_id"`
	CurrentIntentClass       string                `json:"current_intent_class,omitempty"`
	CurrentIntentSummary     string                `json:"current_intent_summary,omitempty"`
	CurrentBriefID           common.BriefID        `json:"current_brief_id,omitempty"`
	CurrentBriefHash         string                `json:"current_brief_hash,omitempty"`
	LatestRunID              common.RunID          `json:"latest_run_id,omitempty"`
	LatestRunStatus          run.Status            `json:"latest_run_status,omitempty"`
	LatestRunSummary         string                `json:"latest_run_summary,omitempty"`
	RepoAnchor               RepoAnchor            `json:"repo_anchor"`
	LatestCheckpointID       common.CheckpointID   `json:"latest_checkpoint_id,omitempty"`
	LatestCheckpointAtUnixMs int64                 `json:"latest_checkpoint_at_unix_ms,omitempty"`
	LatestCheckpointTrigger  string                `json:"latest_checkpoint_trigger,omitempty"`
	CheckpointResumable      bool                  `json:"checkpoint_resumable,omitempty"`
	ResumeDescriptor         string                `json:"resume_descriptor,omitempty"`
	LatestLaunchAttemptID    string                `json:"latest_launch_attempt_id,omitempty"`
	LatestLaunchID           string                `json:"latest_launch_id,omitempty"`
	LatestLaunchStatus       string                `json:"latest_launch_status,omitempty"`
	LaunchControlState       string                `json:"launch_control_state,omitempty"`
	LaunchRetryDisposition   string                `json:"launch_retry_disposition,omitempty"`
	LaunchControlReason      string                `json:"launch_control_reason,omitempty"`
	IsResumable              bool                  `json:"is_resumable,omitempty"`
	RecoveryClass            string                `json:"recovery_class,omitempty"`
	RecommendedAction        string                `json:"recommended_action,omitempty"`
	ReadyForNextRun          bool                  `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch    bool                  `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason           string                `json:"recovery_reason,omitempty"`
	LastEventType            string                `json:"last_event_type,omitempty"`
	LastEventID              common.EventID        `json:"last_event_id,omitempty"`
	LastEventAtUnixMs        int64                 `json:"last_event_at_unix_ms,omitempty"`
}

type TaskInspectRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskRecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TaskRecoveryAssessment struct {
	TaskID                 common.TaskID         `json:"task_id"`
	ContinuityOutcome      string                `json:"continuity_outcome"`
	RecoveryClass          string                `json:"recovery_class"`
	RecommendedAction      string                `json:"recommended_action"`
	ReadyForNextRun        bool                  `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                  `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                  `json:"requires_decision,omitempty"`
	RequiresRepair         bool                  `json:"requires_repair,omitempty"`
	RequiresReview         bool                  `json:"requires_review,omitempty"`
	RequiresReconciliation bool                  `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass `json:"drift_class,omitempty"`
	Reason                 string                `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID   `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID          `json:"run_id,omitempty"`
	HandoffID              string                `json:"handoff_id,omitempty"`
	HandoffStatus          string                `json:"handoff_status,omitempty"`
	Issues                 []TaskRecoveryIssue   `json:"issues,omitempty"`
}

type TaskLaunchControl struct {
	TaskID           common.TaskID  `json:"task_id"`
	HandoffID        string         `json:"handoff_id,omitempty"`
	AttemptID        string         `json:"attempt_id,omitempty"`
	LaunchID         string         `json:"launch_id,omitempty"`
	State            string         `json:"state"`
	RetryDisposition string         `json:"retry_disposition"`
	Reason           string         `json:"reason,omitempty"`
	TargetWorker     run.WorkerKind `json:"target_worker,omitempty"`
	RequestedAt      time.Time      `json:"requested_at,omitempty"`
	CompletedAt      time.Time      `json:"completed_at,omitempty"`
	FailedAt         time.Time      `json:"failed_at,omitempty"`
}

type TaskInspectResponse struct {
	TaskID         common.TaskID           `json:"task_id"`
	RepoAnchor     RepoAnchor              `json:"repo_anchor"`
	Intent         *intent.State           `json:"intent,omitempty"`
	Brief          *brief.ExecutionBrief   `json:"brief,omitempty"`
	Run            *run.ExecutionRun       `json:"run,omitempty"`
	Checkpoint     *checkpoint.Checkpoint  `json:"checkpoint,omitempty"`
	Handoff        *handoff.Packet         `json:"handoff,omitempty"`
	Launch         *handoff.Launch         `json:"launch,omitempty"`
	Acknowledgment *handoff.Acknowledgment `json:"acknowledgment,omitempty"`
	LaunchControl  *TaskLaunchControl      `json:"launch_control,omitempty"`
	Recovery       *TaskRecoveryAssessment `json:"recovery,omitempty"`
}

type TaskShellSnapshotRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellBrief struct {
	BriefID          common.BriefID `json:"brief_id"`
	Objective        string         `json:"objective"`
	NormalizedAction string         `json:"normalized_action"`
	Constraints      []string       `json:"constraints,omitempty"`
	DoneCriteria     []string       `json:"done_criteria,omitempty"`
}

type TaskShellRun struct {
	RunID              common.RunID   `json:"run_id"`
	WorkerKind         run.WorkerKind `json:"worker_kind"`
	Status             run.Status     `json:"status"`
	LastKnownSummary   string         `json:"last_known_summary,omitempty"`
	StartedAt          time.Time      `json:"started_at"`
	EndedAt            *time.Time     `json:"ended_at,omitempty"`
	InterruptionReason string         `json:"interruption_reason,omitempty"`
}

type TaskShellCheckpoint struct {
	CheckpointID     common.CheckpointID `json:"checkpoint_id"`
	Trigger          checkpoint.Trigger  `json:"trigger"`
	CreatedAt        time.Time           `json:"created_at"`
	ResumeDescriptor string              `json:"resume_descriptor,omitempty"`
	IsResumable      bool                `json:"is_resumable"`
}

type TaskShellHandoff struct {
	HandoffID    string         `json:"handoff_id"`
	Status       string         `json:"status"`
	SourceWorker run.WorkerKind `json:"source_worker"`
	TargetWorker run.WorkerKind `json:"target_worker"`
	Mode         string         `json:"mode"`
	Reason       string         `json:"reason,omitempty"`
	AcceptedBy   run.WorkerKind `json:"accepted_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type TaskShellAcknowledgment struct {
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskShellProof struct {
	EventID   common.EventID `json:"event_id"`
	Type      string         `json:"type"`
	Summary   string         `json:"summary"`
	Timestamp time.Time      `json:"timestamp"`
}

type TaskShellConversation struct {
	Role      string    `json:"role"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskShellSnapshotResponse struct {
	TaskID                  common.TaskID            `json:"task_id"`
	Goal                    string                   `json:"goal"`
	Phase                   string                   `json:"phase"`
	Status                  string                   `json:"status"`
	RepoAnchor              RepoAnchor               `json:"repo_anchor"`
	IntentClass             string                   `json:"intent_class,omitempty"`
	IntentSummary           string                   `json:"intent_summary,omitempty"`
	Brief                   *TaskShellBrief          `json:"brief,omitempty"`
	Run                     *TaskShellRun            `json:"run,omitempty"`
	Checkpoint              *TaskShellCheckpoint     `json:"checkpoint,omitempty"`
	Handoff                 *TaskShellHandoff        `json:"handoff,omitempty"`
	Acknowledgment          *TaskShellAcknowledgment `json:"acknowledgment,omitempty"`
	RecentProofs            []TaskShellProof         `json:"recent_proofs,omitempty"`
	RecentConversation      []TaskShellConversation  `json:"recent_conversation,omitempty"`
	LatestCanonicalResponse string                   `json:"latest_canonical_response,omitempty"`
}

type TaskShellLifecycleRequest struct {
	TaskID     common.TaskID `json:"task_id"`
	SessionID  string        `json:"session_id"`
	Kind       string        `json:"kind"`
	HostMode   string        `json:"host_mode"`
	HostState  string        `json:"host_state"`
	Note       string        `json:"note,omitempty"`
	InputLive  bool          `json:"input_live"`
	ExitCode   *int          `json:"exit_code,omitempty"`
	PaneWidth  int           `json:"pane_width,omitempty"`
	PaneHeight int           `json:"pane_height,omitempty"`
}

type TaskShellLifecycleResponse struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellSessionRecord struct {
	SessionID        string        `json:"session_id"`
	TaskID           common.TaskID `json:"task_id"`
	WorkerPreference string        `json:"worker_preference,omitempty"`
	ResolvedWorker   string        `json:"resolved_worker,omitempty"`
	WorkerSessionID  string        `json:"worker_session_id,omitempty"`
	AttachCapability string        `json:"attach_capability,omitempty"`
	HostMode         string        `json:"host_mode,omitempty"`
	HostState        string        `json:"host_state,omitempty"`
	SessionClass     string        `json:"session_class,omitempty"`
	StartedAt        time.Time     `json:"started_at"`
	LastUpdatedAt    time.Time     `json:"last_updated_at"`
	Active           bool          `json:"active"`
	Note             string        `json:"note,omitempty"`
}

type TaskShellSessionReportRequest struct {
	TaskID           common.TaskID `json:"task_id"`
	SessionID        string        `json:"session_id"`
	WorkerPreference string        `json:"worker_preference,omitempty"`
	ResolvedWorker   string        `json:"resolved_worker,omitempty"`
	WorkerSessionID  string        `json:"worker_session_id,omitempty"`
	AttachCapability string        `json:"attach_capability,omitempty"`
	HostMode         string        `json:"host_mode,omitempty"`
	HostState        string        `json:"host_state,omitempty"`
	StartedAt        time.Time     `json:"started_at"`
	Active           bool          `json:"active"`
	Note             string        `json:"note,omitempty"`
}

type TaskShellSessionReportResponse struct {
	TaskID  common.TaskID          `json:"task_id"`
	Session TaskShellSessionRecord `json:"session"`
}

type TaskShellSessionsRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellSessionsResponse struct {
	TaskID   common.TaskID            `json:"task_id"`
	Sessions []TaskShellSessionRecord `json:"sessions,omitempty"`
}

type TaskRunRequest struct {
	TaskID             common.TaskID `json:"task_id"`
	Action             string        `json:"action,omitempty"` // start|complete|interrupt
	Mode               string        `json:"mode,omitempty"`   // real|noop
	RunID              common.RunID  `json:"run_id,omitempty"`
	SimulateInterrupt  bool          `json:"simulate_interrupt,omitempty"`
	InterruptionReason string        `json:"interruption_reason,omitempty"`
}

type TaskRunResponse struct {
	TaskID            common.TaskID `json:"task_id"`
	RunID             common.RunID  `json:"run_id"`
	RunStatus         run.Status    `json:"run_status"`
	Phase             phase.Phase   `json:"phase"`
	CanonicalResponse string        `json:"canonical_response"`
}

type TaskContinueRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskContinueResponse struct {
	TaskID                common.TaskID         `json:"task_id"`
	Outcome               string                `json:"outcome"`
	DriftClass            checkpoint.DriftClass `json:"drift_class"`
	Phase                 phase.Phase           `json:"phase"`
	RunID                 common.RunID          `json:"run_id,omitempty"`
	CheckpointID          common.CheckpointID   `json:"checkpoint_id,omitempty"`
	ResumeDescriptor      string                `json:"resume_descriptor,omitempty"`
	RecoveryClass         string                `json:"recovery_class,omitempty"`
	RecommendedAction     string                `json:"recommended_action,omitempty"`
	ReadyForNextRun       bool                  `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch bool                  `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason        string                `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                `json:"canonical_response"`
}

type TaskCheckpointRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskCheckpointResponse struct {
	TaskID            common.TaskID       `json:"task_id"`
	CheckpointID      common.CheckpointID `json:"checkpoint_id"`
	Trigger           checkpoint.Trigger  `json:"trigger"`
	IsResumable       bool                `json:"is_resumable"`
	CanonicalResponse string              `json:"canonical_response"`
}

type TaskHandoffCreateRequest struct {
	TaskID       common.TaskID  `json:"task_id"`
	TargetWorker run.WorkerKind `json:"target_worker,omitempty"`
	Reason       string         `json:"reason,omitempty"`
	Mode         handoff.Mode   `json:"mode,omitempty"`
	Notes        []string       `json:"notes,omitempty"`
}

type TaskHandoffCreateResponse struct {
	TaskID            common.TaskID       `json:"task_id"`
	HandoffID         string              `json:"handoff_id"`
	SourceWorker      run.WorkerKind      `json:"source_worker"`
	TargetWorker      run.WorkerKind      `json:"target_worker"`
	Status            string              `json:"status"`
	CheckpointID      common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID           common.BriefID      `json:"brief_id,omitempty"`
	CanonicalResponse string              `json:"canonical_response"`
	Packet            *handoff.Packet     `json:"packet,omitempty"`
}

type TaskHandoffAcceptRequest struct {
	TaskID     common.TaskID  `json:"task_id"`
	HandoffID  string         `json:"handoff_id"`
	AcceptedBy run.WorkerKind `json:"accepted_by,omitempty"`
	Notes      []string       `json:"notes,omitempty"`
}

type TaskHandoffAcceptResponse struct {
	TaskID            common.TaskID `json:"task_id"`
	HandoffID         string        `json:"handoff_id"`
	Status            string        `json:"status"`
	CanonicalResponse string        `json:"canonical_response"`
}

type TaskHandoffLaunchRequest struct {
	TaskID       common.TaskID  `json:"task_id"`
	HandoffID    string         `json:"handoff_id,omitempty"`
	TargetWorker run.WorkerKind `json:"target_worker,omitempty"`
}

type TaskHandoffLaunchResponse struct {
	TaskID            common.TaskID          `json:"task_id"`
	HandoffID         string                 `json:"handoff_id"`
	TargetWorker      run.WorkerKind         `json:"target_worker"`
	LaunchStatus      string                 `json:"launch_status"`
	LaunchID          string                 `json:"launch_id,omitempty"`
	CanonicalResponse string                 `json:"canonical_response"`
	Payload           *handoff.LaunchPayload `json:"payload,omitempty"`
}
```

File: /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go
```go
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"tuku/internal/domain/shellsession"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
)

func ipcRecoveryAssessment(in *orchestrator.RecoveryAssessment) *ipc.TaskRecoveryAssessment {
	if in == nil {
		return nil
	}
	out := &ipc.TaskRecoveryAssessment{
		TaskID:                 in.TaskID,
		ContinuityOutcome:      string(in.ContinuityOutcome),
		RecoveryClass:          string(in.RecoveryClass),
		RecommendedAction:      string(in.RecommendedAction),
		ReadyForNextRun:        in.ReadyForNextRun,
		ReadyForHandoffLaunch:  in.ReadyForHandoffLaunch,
		RequiresDecision:       in.RequiresDecision,
		RequiresRepair:         in.RequiresRepair,
		RequiresReview:         in.RequiresReview,
		RequiresReconciliation: in.RequiresReconciliation,
		DriftClass:             in.DriftClass,
		Reason:                 in.Reason,
		CheckpointID:           in.CheckpointID,
		RunID:                  in.RunID,
		HandoffID:              in.HandoffID,
		HandoffStatus:          string(in.HandoffStatus),
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ipc.TaskRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ipc.TaskRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func ipcLaunchControl(in *orchestrator.LaunchControl) *ipc.TaskLaunchControl {
	if in == nil {
		return nil
	}
	return &ipc.TaskLaunchControl{
		TaskID:           in.TaskID,
		HandoffID:        in.HandoffID,
		AttemptID:        in.AttemptID,
		LaunchID:         in.LaunchID,
		State:            string(in.State),
		RetryDisposition: string(in.RetryDisposition),
		Reason:           in.Reason,
		TargetWorker:     in.TargetWorker,
		RequestedAt:      in.RequestedAt,
		CompletedAt:      in.CompletedAt,
		FailedAt:         in.FailedAt,
	}
}

type Service struct {
	SocketPath string
	Handler    orchestrator.Service
}

func NewService(socketPath string, handler orchestrator.Service) *Service {
	return &Service{SocketPath: socketPath, Handler: handler}
}

func (s *Service) Run(ctx context.Context) error {
	if s.Handler == nil {
		return errors.New("daemon handler is required")
	}
	if s.SocketPath == "" {
		return errors.New("daemon socket path is required")
	}

	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(s.SocketPath)

	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(s.SocketPath)
	}()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Service) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	decoder := json.NewDecoder(bufio.NewReader(conn))
	encoder := json.NewEncoder(conn)

	var req ipc.Request
	if err := decoder.Decode(&req); err != nil {
		_ = encoder.Encode(ipc.Response{RequestID: req.RequestID, OK: false, Error: &ipc.ErrorPayload{Code: "BAD_REQUEST", Message: err.Error()}})
		return
	}

	resp := s.handleRequest(ctx, req)
	_ = encoder.Encode(resp)
}

func (s *Service) handleRequest(ctx context.Context, req ipc.Request) ipc.Response {
	respondErr := func(code, msg string) ipc.Response {
		return ipc.Response{RequestID: req.RequestID, OK: false, Error: &ipc.ErrorPayload{Code: code, Message: msg}}
	}
	respondOK := func(payload any) ipc.Response {
		b, err := json.Marshal(payload)
		if err != nil {
			return respondErr("ENCODE_ERROR", err.Error())
		}
		return ipc.Response{RequestID: req.RequestID, OK: true, Payload: b}
	}

	switch req.Method {
	case ipc.MethodResolveShellTaskForRepo:
		var p ipc.ResolveShellTaskForRepoRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ResolveShellTaskForRepo(ctx, p.RepoRoot, p.DefaultGoal)
		if err != nil {
			return respondErr("SHELL_TASK_RESOLVE_FAILED", err.Error())
		}
		return respondOK(ipc.ResolveShellTaskForRepoResponse{
			TaskID:   out.TaskID,
			RepoRoot: out.RepoRoot,
			Created:  out.Created,
		})
	case ipc.MethodStartTask:
		var p ipc.StartTaskRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.StartTask(ctx, p.Goal, p.RepoRoot)
		if err != nil {
			return respondErr("START_FAILED", err.Error())
		}
		return respondOK(ipc.StartTaskResponse{
			TaskID:         out.TaskID,
			ConversationID: out.ConversationID,
			Phase:          out.Phase,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			CanonicalResponse: out.CanonicalResponse,
		})

	case ipc.MethodSendMessage:
		var p ipc.TaskMessageRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.MessageTask(ctx, string(p.TaskID), p.Message)
		if err != nil {
			return respondErr("MESSAGE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskMessageResponse{
			TaskID:      out.TaskID,
			Phase:       out.Phase,
			IntentClass: string(out.IntentClass),
			BriefID:     out.BriefID,
			BriefHash:   out.BriefHash,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			CanonicalResponse: out.CanonicalResponse,
		})

	case ipc.MethodTaskStatus:
		var p ipc.TaskStatusRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.StatusTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("STATUS_FAILED", err.Error())
		}
		latestCheckpointAt := int64(0)
		if !out.LatestCheckpointAt.IsZero() {
			latestCheckpointAt = out.LatestCheckpointAt.UnixMilli()
		}
		lastEventAt := int64(0)
		if !out.LastEventAt.IsZero() {
			lastEventAt = out.LastEventAt.UnixMilli()
		}
		return respondOK(ipc.TaskStatusResponse{
			TaskID:               out.TaskID,
			ConversationID:       out.ConversationID,
			Goal:                 out.Goal,
			Phase:                out.Phase,
			Status:               out.Status,
			CurrentIntentID:      out.CurrentIntentID,
			CurrentIntentClass:   string(out.CurrentIntentClass),
			CurrentIntentSummary: out.CurrentIntentSummary,
			CurrentBriefID:       out.CurrentBriefID,
			CurrentBriefHash:     out.CurrentBriefHash,
			LatestRunID:          out.LatestRunID,
			LatestRunStatus:      out.LatestRunStatus,
			LatestRunSummary:     out.LatestRunSummary,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCheckpointID:       out.LatestCheckpointID,
			LatestCheckpointAtUnixMs: latestCheckpointAt,
			LatestCheckpointTrigger:  string(out.LatestCheckpointTrigger),
			CheckpointResumable:      out.CheckpointResumable,
			ResumeDescriptor:         out.ResumeDescriptor,
			LatestLaunchAttemptID:    out.LatestLaunchAttemptID,
			LatestLaunchID:           out.LatestLaunchID,
			LatestLaunchStatus:       string(out.LatestLaunchStatus),
			LaunchControlState:       string(out.LaunchControlState),
			LaunchRetryDisposition:   string(out.LaunchRetryDisposition),
			LaunchControlReason:      out.LaunchControlReason,
			IsResumable:              out.IsResumable,
			RecoveryClass:            string(out.RecoveryClass),
			RecommendedAction:        string(out.RecommendedAction),
			ReadyForNextRun:          out.ReadyForNextRun,
			ReadyForHandoffLaunch:    out.ReadyForHandoffLaunch,
			RecoveryReason:           out.RecoveryReason,
			LastEventType:            string(out.LastEventType),
			LastEventID:              out.LastEventID,
			LastEventAtUnixMs:        lastEventAt,
		})
	case ipc.MethodTaskRun:
		var p ipc.TaskRunRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RunTask(ctx, orchestrator.RunTaskRequest{
			TaskID:             string(p.TaskID),
			Action:             p.Action,
			Mode:               p.Mode,
			RunID:              p.RunID,
			SimulateInterrupt:  p.SimulateInterrupt,
			InterruptionReason: p.InterruptionReason,
		})
		if err != nil {
			return respondErr("RUN_FAILED", err.Error())
		}
		return respondOK(ipc.TaskRunResponse{
			TaskID:            out.TaskID,
			RunID:             out.RunID,
			RunStatus:         out.RunStatus,
			Phase:             out.Phase,
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodTaskInspect:
		var p ipc.TaskInspectRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.InspectTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("INSPECT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskInspectResponse{
			TaskID: out.TaskID,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			Intent:         out.Intent,
			Brief:          out.Brief,
			Run:            out.Run,
			Checkpoint:     out.Checkpoint,
			Handoff:        out.Handoff,
			Launch:         out.Launch,
			Acknowledgment: out.Acknowledgment,
			LaunchControl:  ipcLaunchControl(out.LaunchControl),
			Recovery:       ipcRecoveryAssessment(out.Recovery),
		})
	case ipc.MethodTaskShellSnapshot:
		var p ipc.TaskShellSnapshotRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ShellSnapshotTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("SHELL_SNAPSHOT_FAILED", err.Error())
		}
		resp := ipc.TaskShellSnapshotResponse{
			TaskID:        out.TaskID,
			Goal:          out.Goal,
			Phase:         out.Phase,
			Status:        out.Status,
			IntentClass:   out.IntentClass,
			IntentSummary: out.IntentSummary,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			LatestCanonicalResponse: out.LatestCanonicalResponse,
		}
		if out.Brief != nil {
			resp.Brief = &ipc.TaskShellBrief{
				BriefID:          out.Brief.BriefID,
				Objective:        out.Brief.Objective,
				NormalizedAction: out.Brief.NormalizedAction,
				Constraints:      append([]string{}, out.Brief.Constraints...),
				DoneCriteria:     append([]string{}, out.Brief.DoneCriteria...),
			}
		}
		if out.Run != nil {
			resp.Run = &ipc.TaskShellRun{
				RunID:              out.Run.RunID,
				WorkerKind:         out.Run.WorkerKind,
				Status:             out.Run.Status,
				LastKnownSummary:   out.Run.LastKnownSummary,
				StartedAt:          out.Run.StartedAt,
				EndedAt:            out.Run.EndedAt,
				InterruptionReason: out.Run.InterruptionReason,
			}
		}
		if out.Checkpoint != nil {
			resp.Checkpoint = &ipc.TaskShellCheckpoint{
				CheckpointID:     out.Checkpoint.CheckpointID,
				Trigger:          out.Checkpoint.Trigger,
				CreatedAt:        out.Checkpoint.CreatedAt,
				ResumeDescriptor: out.Checkpoint.ResumeDescriptor,
				IsResumable:      out.Checkpoint.IsResumable,
			}
		}
		if out.Handoff != nil {
			resp.Handoff = &ipc.TaskShellHandoff{
				HandoffID:    out.Handoff.HandoffID,
				Status:       string(out.Handoff.Status),
				SourceWorker: out.Handoff.SourceWorker,
				TargetWorker: out.Handoff.TargetWorker,
				Mode:         string(out.Handoff.Mode),
				Reason:       out.Handoff.Reason,
				AcceptedBy:   out.Handoff.AcceptedBy,
				CreatedAt:    out.Handoff.CreatedAt,
			}
		}
		if out.Acknowledgment != nil {
			resp.Acknowledgment = &ipc.TaskShellAcknowledgment{
				Status:    string(out.Acknowledgment.Status),
				Summary:   out.Acknowledgment.Summary,
				CreatedAt: out.Acknowledgment.CreatedAt,
			}
		}
		if len(out.RecentProofs) > 0 {
			resp.RecentProofs = make([]ipc.TaskShellProof, 0, len(out.RecentProofs))
			for _, evt := range out.RecentProofs {
				resp.RecentProofs = append(resp.RecentProofs, ipc.TaskShellProof{
					EventID:   evt.EventID,
					Type:      string(evt.Type),
					Summary:   evt.Summary,
					Timestamp: evt.Timestamp,
				})
			}
		}
		if len(out.RecentConversation) > 0 {
			resp.RecentConversation = make([]ipc.TaskShellConversation, 0, len(out.RecentConversation))
			for _, msg := range out.RecentConversation {
				resp.RecentConversation = append(resp.RecentConversation, ipc.TaskShellConversation{
					Role:      string(msg.Role),
					Body:      msg.Body,
					CreatedAt: msg.CreatedAt,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodTaskShellLifecycle:
		var p ipc.TaskShellLifecycleRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordShellLifecycle(ctx, orchestrator.RecordShellLifecycleRequest{
			TaskID:     string(p.TaskID),
			SessionID:  p.SessionID,
			Kind:       orchestrator.ShellLifecycleKind(p.Kind),
			HostMode:   p.HostMode,
			HostState:  p.HostState,
			Note:       p.Note,
			InputLive:  p.InputLive,
			ExitCode:   p.ExitCode,
			PaneWidth:  p.PaneWidth,
			PaneHeight: p.PaneHeight,
		})
		if err != nil {
			return respondErr("SHELL_LIFECYCLE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellLifecycleResponse{TaskID: out.TaskID})
	case ipc.MethodTaskShellSessionReport:
		var p ipc.TaskShellSessionReportRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ReportShellSession(ctx, orchestrator.ReportShellSessionRequest{
			TaskID:           string(p.TaskID),
			SessionID:        p.SessionID,
			WorkerPreference: p.WorkerPreference,
			ResolvedWorker:   p.ResolvedWorker,
			WorkerSessionID:  p.WorkerSessionID,
			AttachCapability: shellsession.AttachCapability(p.AttachCapability),
			HostMode:         p.HostMode,
			HostState:        p.HostState,
			StartedAt:        p.StartedAt,
			Active:           p.Active,
			Note:             p.Note,
		})
		if err != nil {
			return respondErr("SHELL_SESSION_REPORT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskShellSessionReportResponse{
			TaskID: out.TaskID,
			Session: ipc.TaskShellSessionRecord{
				SessionID:        out.Session.SessionID,
				TaskID:           out.Session.TaskID,
				WorkerPreference: out.Session.WorkerPreference,
				ResolvedWorker:   out.Session.ResolvedWorker,
				WorkerSessionID:  out.Session.WorkerSessionID,
				AttachCapability: string(out.Session.AttachCapability),
				HostMode:         out.Session.HostMode,
				HostState:        out.Session.HostState,
				SessionClass:     string(out.Session.SessionClass),
				StartedAt:        out.Session.StartedAt,
				LastUpdatedAt:    out.Session.LastUpdatedAt,
				Active:           out.Session.Active,
				Note:             out.Session.Note,
			},
		})
	case ipc.MethodTaskShellSessions:
		var p ipc.TaskShellSessionsRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ListShellSessions(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("SHELL_SESSIONS_FAILED", err.Error())
		}
		resp := ipc.TaskShellSessionsResponse{TaskID: out.TaskID}
		if len(out.Sessions) > 0 {
			resp.Sessions = make([]ipc.TaskShellSessionRecord, 0, len(out.Sessions))
			for _, session := range out.Sessions {
				resp.Sessions = append(resp.Sessions, ipc.TaskShellSessionRecord{
					SessionID:        session.SessionID,
					TaskID:           session.TaskID,
					WorkerPreference: session.WorkerPreference,
					ResolvedWorker:   session.ResolvedWorker,
					WorkerSessionID:  session.WorkerSessionID,
					AttachCapability: string(session.AttachCapability),
					HostMode:         session.HostMode,
					HostState:        session.HostState,
					SessionClass:     string(session.SessionClass),
					StartedAt:        session.StartedAt,
					LastUpdatedAt:    session.LastUpdatedAt,
					Active:           session.Active,
					Note:             session.Note,
				})
			}
		}
		return respondOK(resp)
	case ipc.MethodContinueTask:
		var p ipc.TaskContinueRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ContinueTask(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("CONTINUE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskContinueResponse{
			TaskID:                out.TaskID,
			Outcome:               string(out.Outcome),
			DriftClass:            out.DriftClass,
			Phase:                 out.Phase,
			RunID:                 out.RunID,
			CheckpointID:          out.CheckpointID,
			ResumeDescriptor:      out.ResumeDescriptor,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodCreateCheckpoint:
		var p ipc.TaskCheckpointRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.CreateCheckpoint(ctx, string(p.TaskID))
		if err != nil {
			return respondErr("CHECKPOINT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskCheckpointResponse{
			TaskID:            out.TaskID,
			CheckpointID:      out.CheckpointID,
			Trigger:           out.Trigger,
			IsResumable:       out.IsResumable,
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodCreateHandoff:
		var p ipc.TaskHandoffCreateRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.CreateHandoff(ctx, orchestrator.CreateHandoffRequest{
			TaskID:       string(p.TaskID),
			TargetWorker: p.TargetWorker,
			Reason:       p.Reason,
			Mode:         p.Mode,
			Notes:        append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_CREATE_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffCreateResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			SourceWorker:      out.SourceWorker,
			TargetWorker:      out.TargetWorker,
			Status:            string(out.Status),
			CheckpointID:      out.CheckpointID,
			BriefID:           out.BriefID,
			CanonicalResponse: out.CanonicalResponse,
			Packet:            out.Packet,
		})
	case ipc.MethodAcceptHandoff:
		var p ipc.TaskHandoffAcceptRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.AcceptHandoff(ctx, orchestrator.AcceptHandoffRequest{
			TaskID:     string(p.TaskID),
			HandoffID:  p.HandoffID,
			AcceptedBy: p.AcceptedBy,
			Notes:      append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_ACCEPT_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffAcceptResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			Status:            string(out.Status),
			CanonicalResponse: out.CanonicalResponse,
		})
	case ipc.MethodLaunchHandoff:
		var p ipc.TaskHandoffLaunchRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.LaunchHandoff(ctx, orchestrator.LaunchHandoffRequest{
			TaskID:       string(p.TaskID),
			HandoffID:    p.HandoffID,
			TargetWorker: p.TargetWorker,
		})
		if err != nil {
			return respondErr("HANDOFF_LAUNCH_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffLaunchResponse{
			TaskID:            out.TaskID,
			HandoffID:         out.HandoffID,
			TargetWorker:      out.TargetWorker,
			LaunchStatus:      string(out.LaunchStatus),
			LaunchID:          out.LaunchID,
			CanonicalResponse: out.CanonicalResponse,
			Payload:           out.Payload,
		})
	default:
		return respondErr("UNSUPPORTED_METHOD", fmt.Sprintf("unsupported method: %s", req.Method))
	}
}
```

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_test.go
```go
package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func TestCreateHandoffFromSafeResumableState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before handoff: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "codex quota exhausted",
		Mode:         handoff.ModeResume,
		Notes:        []string{"prefer minimal diff follow-up"},
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected claude target worker, got %s", out.TargetWorker)
	}
	if out.Packet == nil {
		t.Fatal("expected handoff packet")
	}
	if out.Packet.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected reused checkpoint %s, got %s", seed.CheckpointID, out.Packet.CheckpointID)
	}
	if out.Packet.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected packet target worker claude, got %s", out.Packet.TargetWorker)
	}
	if out.Packet.RepoAnchor.HeadSHA == "" {
		t.Fatal("expected repo anchor in handoff packet")
	}
	if out.Packet.BriefID == "" || out.Packet.IntentID == "" {
		t.Fatalf("expected brief and intent references in packet, got brief=%s intent=%s", out.Packet.BriefID, out.Packet.IntentID)
	}

	persisted, err := store.Handoffs().Get(out.HandoffID)
	if err != nil {
		t.Fatalf("load persisted handoff: %v", err)
	}
	if persisted.HandoffID != out.HandoffID {
		t.Fatalf("expected persisted handoff %s, got %s", out.HandoffID, persisted.HandoffID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffCreated) {
		t.Fatal("expected HANDOFF_CREATED proof event")
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after handoff: %v", err)
	}
	if capsAfter.Version != capsBefore.Version {
		t.Fatalf("handoff should not mutate capsule version in reuse case: before=%d after=%d", capsBefore.Version, capsAfter.Version)
	}
}

func TestCreateHandoffBlockedOnInconsistentContinuity(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_handoff_missing_brief"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(9 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_for_handoff"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for handoff consistency test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff despite broken continuity",
	})
	if err != nil {
		t.Fatalf("create blocked handoff: %v", err)
	}
	if out.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked status, got %s", out.Status)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffBlocked) {
		t.Fatal("expected HANDOFF_BLOCKED proof event")
	}
}

func TestCreateHandoffCreatesCheckpointWhenReuseNotPossible(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff without existing checkpoint",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerHandoff {
		t.Fatalf("expected handoff trigger on handoff-created checkpoint, got %s", latestCheckpoint.Trigger)
	}
}

func TestCreateHandoffCreatesNewCheckpointWhenLatestCheckpointNotReusable(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	nonReusable := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_non_reusable_for_handoff"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(-1 * time.Second),
		Trigger:            checkpoint.TriggerAwaitingDecision,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "non-resumable checkpoint for reuse guard test",
		IsResumable:        false,
	}
	if err := store.Checkpoints().Create(nonReusable); err != nil {
		t.Fatalf("create non-reusable checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff requiring fresh resumable checkpoint",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.Packet == nil {
		t.Fatal("expected packet")
	}
	if out.Packet.CheckpointID == nonReusable.CheckpointID {
		t.Fatalf("expected fresh handoff checkpoint, got reused non-resumable checkpoint %s", nonReusable.CheckpointID)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerHandoff {
		t.Fatalf("expected handoff trigger for newly created checkpoint, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.CheckpointID != out.Packet.CheckpointID {
		t.Fatalf("expected packet checkpoint %s to match latest %s", out.Packet.CheckpointID, latestCheckpoint.CheckpointID)
	}
}

func TestCreateHandoffReusesMatchingLatestPacket(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	req := CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "reuse existing handoff packet",
		Mode:         handoff.ModeResume,
		Notes:        []string{"preserve prior packet"},
	}
	first, err := coord.CreateHandoff(context.Background(), req)
	if err != nil {
		t.Fatalf("create first handoff: %v", err)
	}
	second, err := coord.CreateHandoff(context.Background(), req)
	if err != nil {
		t.Fatalf("create second handoff: %v", err)
	}
	if first.HandoffID != second.HandoffID {
		t.Fatalf("expected handoff reuse, got first=%s second=%s", first.HandoffID, second.HandoffID)
	}
	if first.CheckpointID != second.CheckpointID {
		t.Fatalf("expected checkpoint reuse, got first=%s second=%s", first.CheckpointID, second.CheckpointID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffCreated) != 1 {
		t.Fatalf("expected exactly one HANDOFF_CREATED event, got %d", countEvents(events, proof.EventHandoffCreated))
	}
}

func TestAcceptHandoffRecordsCompletion(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff to claude",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	acceptOut, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accepted for follow-up implementation"},
	})
	if err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if acceptOut.Status != handoff.StatusAccepted {
		t.Fatalf("expected accepted status, got %s", acceptOut.Status)
	}
	persisted, err := store.Handoffs().Get(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load persisted handoff: %v", err)
	}
	if persisted.Status != handoff.StatusAccepted {
		t.Fatalf("expected persisted accepted status, got %s", persisted.Status)
	}
	if persisted.AcceptedBy != rundomain.WorkerKindClaude {
		t.Fatalf("expected persisted accepted_by %s, got %s", rundomain.WorkerKindClaude, persisted.AcceptedBy)
	}
	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffAccepted) {
		t.Fatal("expected HANDOFF_ACCEPTED proof event")
	}
}

func TestAcceptHandoffIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "idempotent accept test",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	first, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accept once"},
	})
	if err != nil {
		t.Fatalf("accept handoff first: %v", err)
	}
	second, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
		Notes:      []string{"accept once"},
	})
	if err != nil {
		t.Fatalf("accept handoff second: %v", err)
	}
	if first.Status != handoff.StatusAccepted || second.Status != handoff.StatusAccepted {
		t.Fatalf("expected accepted status on idempotent path, got first=%s second=%s", first.Status, second.Status)
	}

	persisted, err := store.Handoffs().Get(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if len(persisted.HandoffNotes) != 1 {
		t.Fatalf("expected exactly one persisted note after idempotent accept, got %+v", persisted.HandoffNotes)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffAccepted) != 1 {
		t.Fatalf("expected exactly one HANDOFF_ACCEPTED event, got %d", countEvents(events, proof.EventHandoffAccepted))
	}
}

func TestAcceptedHandoffRecoveryIsLaunchReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff recovery readiness",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	continueOut, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if continueOut.RecoveryClass != RecoveryClassAcceptedHandoffLaunchReady {
		t.Fatalf("expected accepted handoff recovery class, got %s", continueOut.RecoveryClass)
	}
	if continueOut.RecommendedAction != RecoveryActionLaunchAcceptedHandoff {
		t.Fatalf("expected launch accepted handoff action, got %s", continueOut.RecommendedAction)
	}
	if continueOut.ReadyForNextRun {
		t.Fatal("accepted handoff recovery should not claim local next-run readiness")
	}
	if !continueOut.ReadyForHandoffLaunch {
		t.Fatal("accepted handoff recovery should be ready for handoff launch")
	}
}

func TestCreateHandoffBuildsPacketFromPersistedContinuityState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "complete", RunID: startRes.RunID}); err != nil {
		t.Fatalf("complete noop run: %v", err)
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run: %v", err)
	}
	runRec.LastKnownSummary = "persisted-summary-for-handoff-trust"
	runRec.UpdatedAt = time.Now().UTC()
	if err := store.Runs().Update(runRec); err != nil {
		t.Fatalf("update latest run summary: %v", err)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.Version++
	caps.UpdatedAt = time.Now().UTC()
	caps.WorkingTreeDirty = true
	caps.TouchedFiles = append(caps.TouchedFiles, "persisted/worker_state.go")
	caps.Blockers = []string{"persisted blocker for trust test"}
	caps.NextAction = "persisted next action for handoff"
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	briefRec, err := store.Briefs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest brief: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event: %v", err)
	}
	persistedCheckpoint := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_persisted_trust_anchor"),
		TaskID:             taskID,
		RunID:              runRec.RunID,
		CreatedAt:          time.Now().UTC().Add(15 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            briefRec.BriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "persisted resume descriptor for trust test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(persistedCheckpoint); err != nil {
		t.Fatalf("create persisted checkpoint: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "trust test handoff",
		Mode:         handoff.ModeTakeover,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusCreated {
		t.Fatalf("expected created status, got %s", out.Status)
	}
	if out.Packet == nil {
		t.Fatal("expected packet")
	}
	if out.Packet.CheckpointID != persistedCheckpoint.CheckpointID {
		t.Fatalf("expected packet checkpoint from persisted state %s, got %s", persistedCheckpoint.CheckpointID, out.Packet.CheckpointID)
	}
	if out.Packet.ResumeDescriptor != persistedCheckpoint.ResumeDescriptor {
		t.Fatalf("expected persisted resume descriptor %q, got %q", persistedCheckpoint.ResumeDescriptor, out.Packet.ResumeDescriptor)
	}
	if out.Packet.LatestRunID != runRec.RunID {
		t.Fatalf("expected packet latest run %s, got %s", runRec.RunID, out.Packet.LatestRunID)
	}
	if out.Packet.BriefID != briefRec.BriefID {
		t.Fatalf("expected packet brief %s, got %s", briefRec.BriefID, out.Packet.BriefID)
	}
	if out.Packet.HandoffMode != handoff.ModeTakeover {
		t.Fatalf("expected typed handoff mode %s, got %s", handoff.ModeTakeover, out.Packet.HandoffMode)
	}
	if !containsString(out.Packet.TouchedFiles, "persisted/worker_state.go") {
		t.Fatalf("expected touched files to reflect persisted capsule update: %+v", out.Packet.TouchedFiles)
	}
	if !containsString(out.Packet.Blockers, "persisted blocker for trust test") {
		t.Fatalf("expected blockers to reflect persisted capsule update: %+v", out.Packet.Blockers)
	}
}

func TestLaunchHandoffClaudeSuccessFlow(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff launch test",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed launch status, got %s", launchOut.LaunchStatus)
	}
	if launchOut.Payload == nil {
		t.Fatal("expected launch payload")
	}
	if launchOut.Payload.HandoffID != createOut.HandoffID {
		t.Fatalf("expected payload handoff id %s, got %s", createOut.HandoffID, launchOut.Payload.HandoffID)
	}
	if launchOut.Payload.BriefID != createOut.Packet.BriefID {
		t.Fatalf("expected payload brief id %s, got %s", createOut.Packet.BriefID, launchOut.Payload.BriefID)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "acknowledgment") {
		t.Fatalf("expected canonical response to mention acknowledgment, got %q", launchOut.CanonicalResponse)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "not yet proven complete") {
		t.Fatalf("expected canonical response to avoid downstream completion claims, got %q", launchOut.CanonicalResponse)
	}
	if !launcher.called {
		t.Fatal("expected handoff launcher to be called")
	}
	if launcher.lastReq.Payload.CheckpointID != createOut.Packet.CheckpointID {
		t.Fatalf("expected launcher payload checkpoint %s, got %s", createOut.Packet.CheckpointID, launcher.lastReq.Payload.CheckpointID)
	}
	ack, err := store.Handoffs().LatestAcknowledgment(createOut.HandoffID)
	if err != nil {
		t.Fatalf("expected persisted launch acknowledgment: %v", err)
	}
	if ack.Status != handoff.AcknowledgmentCaptured {
		t.Fatalf("expected captured acknowledgment status, got %s", ack.Status)
	}
	if ack.Summary == "" {
		t.Fatal("expected non-empty acknowledgment summary")
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("expected HANDOFF_LAUNCH_REQUESTED proof event")
	}
	if !hasEvent(events, proof.EventHandoffLaunchCompleted) {
		t.Fatal("expected HANDOFF_LAUNCH_COMPLETED proof event")
	}
	if !hasEvent(events, proof.EventHandoffAcknowledgmentCaptured) {
		t.Fatal("expected HANDOFF_ACKNOWLEDGMENT_CAPTURED proof event")
	}
	if !hasEvent(events, proof.EventCanonicalResponseEmitted) {
		t.Fatal("expected canonical response proof event")
	}
}

func TestCreateHandoffBlockedAfterFailedRunRecovery(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	out, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "should block after failed run",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if out.Status != handoff.StatusBlocked {
		t.Fatalf("expected blocked handoff after failed run recovery state, got %s", out.Status)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}
}

func TestLaunchHandoffSuccessWithUnusableOutputPersistsUnavailableAcknowledgment(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherUnusableOutput()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff unusable-ack test",
		Mode:         handoff.ModeResume,
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	launchOut, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if launchOut.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed launch status, got %s", launchOut.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "no usable initial acknowledgment") {
		t.Fatalf("expected canonical fallback wording, got %q", launchOut.CanonicalResponse)
	}
	if !strings.Contains(strings.ToLower(launchOut.CanonicalResponse), "not yet proven complete") {
		t.Fatalf("expected explicit uncertainty in canonical response, got %q", launchOut.CanonicalResponse)
	}

	ack, err := store.Handoffs().LatestAcknowledgment(createOut.HandoffID)
	if err != nil {
		t.Fatalf("load latest acknowledgment: %v", err)
	}
	if ack.Status != handoff.AcknowledgmentUnavailable {
		t.Fatalf("expected unavailable acknowledgment status, got %s", ack.Status)
	}
	if len(ack.Unknowns) == 0 {
		t.Fatal("expected unknowns for unavailable acknowledgment")
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffAcknowledgmentUnavailable) {
		t.Fatal("expected HANDOFF_ACKNOWLEDGMENT_UNAVAILABLE proof event")
	}
	if hasEvent(events, proof.EventHandoffAcknowledgmentCaptured) {
		t.Fatal("did not expect HANDOFF_ACKNOWLEDGMENT_CAPTURED for unusable output")
	}
}

func TestLaunchHandoffBlockedCases(t *testing.T) {
	t.Run("missing handoff", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:    string(taskID),
			HandoffID: "hnd_missing",
		})
		if err != nil {
			t.Fatalf("launch handoff missing: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked launch status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on blocked path")
		}
		events, err := store.Proofs().ListByTask(taskID, 500)
		if err != nil {
			t.Fatalf("list proofs: %v", err)
		}
		if !hasEvent(events, proof.EventHandoffLaunchBlocked) {
			t.Fatal("expected HANDOFF_LAUNCH_BLOCKED proof event")
		}
	})

	t.Run("wrong status", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
			TaskID:       string(taskID),
			TargetWorker: rundomain.WorkerKindClaude,
			Reason:       "status block test",
		})
		if err != nil {
			t.Fatalf("create handoff: %v", err)
		}
		if err := store.Handoffs().UpdateStatus(taskID, createOut.HandoffID, handoff.StatusBlocked, rundomain.WorkerKindUnknown, []string{"blocked for test"}, time.Now().UTC()); err != nil {
			t.Fatalf("force blocked status: %v", err)
		}

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:    string(taskID),
			HandoffID: createOut.HandoffID,
		})
		if err != nil {
			t.Fatalf("launch handoff wrong status: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on wrong-status blocked path")
		}
	})

	t.Run("wrong target", func(t *testing.T) {
		store := newTestStore(t)
		launcher := newFakeHandoffLauncherSuccess()
		coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
		taskID := setupTaskWithBrief(t, coord)

		createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
			TaskID:       string(taskID),
			TargetWorker: rundomain.WorkerKindClaude,
			Reason:       "target mismatch test",
		})
		if err != nil {
			t.Fatalf("create handoff: %v", err)
		}

		out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
			TaskID:       string(taskID),
			HandoffID:    createOut.HandoffID,
			TargetWorker: rundomain.WorkerKindCodex,
		})
		if err != nil {
			t.Fatalf("launch handoff wrong target: %v", err)
		}
		if out.LaunchStatus != HandoffLaunchStatusBlocked {
			t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
		}
		if launcher.called {
			t.Fatal("launcher should not be called on wrong-target blocked path")
		}
	})
}

func TestLaunchHandoffFailureRecordsProofAndCanonical(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherError(errors.New("claude launch unavailable"))
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "failure path test",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff failure path should return canonical result, got err: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusFailed {
		t.Fatalf("expected failed launch status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "failed") {
		t.Fatalf("expected failed canonical response, got %q", out.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("expected HANDOFF_LAUNCH_REQUESTED proof event")
	}
	if !hasEvent(events, proof.EventHandoffLaunchFailed) {
		t.Fatal("expected HANDOFF_LAUNCH_FAILED proof event")
	}
	if !hasEvent(events, proof.EventCanonicalResponseEmitted) {
		t.Fatal("expected canonical response proof event")
	}
}

func TestLaunchHandoffReusesDurableSuccessWithoutRelaunch(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "replay durable success",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff first: %v", err)
	}
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff second: %v", err)
	}
	if launcher.callCount != 1 {
		t.Fatalf("expected launcher to run once, got %d", launcher.callCount)
	}
	if first.LaunchID == "" || second.LaunchID != first.LaunchID {
		t.Fatalf("expected durable launch id reuse, got first=%s second=%s", first.LaunchID, second.LaunchID)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if countEvents(events, proof.EventHandoffLaunchCompleted) != 1 {
		t.Fatalf("expected exactly one HANDOFF_LAUNCH_COMPLETED event, got %d", countEvents(events, proof.EventHandoffLaunchCompleted))
	}
	if countEvents(events, proof.EventHandoffAcknowledgmentCaptured) != 1 {
		t.Fatalf("expected exactly one HANDOFF_ACKNOWLEDGMENT_CAPTURED event, got %d", countEvents(events, proof.EventHandoffAcknowledgmentCaptured))
	}
}

func TestLaunchHandoffBlockedWhenLauncherMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "missing launcher guard",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff with missing launcher should return canonical blocked result, got err: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked launch status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "blocked") {
		t.Fatalf("expected blocked canonical response, got %q", out.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("list proofs: %v", err)
	}
	if !hasEvent(events, proof.EventHandoffLaunchBlocked) {
		t.Fatal("expected HANDOFF_LAUNCH_BLOCKED proof event")
	}
	if hasEvent(events, proof.EventHandoffLaunchRequested) {
		t.Fatal("should not emit HANDOFF_LAUNCH_REQUESTED when launcher is missing")
	}
}

func TestLaunchHandoffBlocksRetryWhenPriorRequestOutcomeUnknown(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "unknown launch replay guard",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_requested_unknown",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: createOut.TargetWorker,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "hash_requested_unknown",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create requested launch record: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff retry guard: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked replay status, got %s", out.LaunchStatus)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "unknown") {
		t.Fatalf("expected unknown-outcome canonical response, got %q", out.CanonicalResponse)
	}
	if launcher.callCount != 0 {
		t.Fatalf("expected launcher not to run, got %d calls", launcher.callCount)
	}
}

func TestLaunchHandoffAllowsRetryAfterDurableFailure(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherError(errors.New("claude launch unavailable"))
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "retry after durable failure",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	first, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("first failed launch: %v", err)
	}
	if first.LaunchStatus != HandoffLaunchStatusFailed {
		t.Fatalf("expected failed first launch, got %s", first.LaunchStatus)
	}

	launcher.err = nil
	launcher.result = newFakeHandoffLauncherSuccess().result
	second, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	})
	if err != nil {
		t.Fatalf("second retry launch: %v", err)
	}
	if second.LaunchStatus != HandoffLaunchStatusCompleted {
		t.Fatalf("expected completed second launch, got %s", second.LaunchStatus)
	}
	if launcher.callCount != 2 {
		t.Fatalf("expected launcher to run twice across retry, got %d", launcher.callCount)
	}

	latestLaunch, err := store.Handoffs().LatestLaunchByHandoff(createOut.HandoffID)
	if err != nil {
		t.Fatalf("latest launch by handoff: %v", err)
	}
	if latestLaunch.Status != handoff.LaunchStatusCompleted {
		t.Fatalf("expected latest durable launch to be completed, got %s", latestLaunch.Status)
	}
	if latestLaunch.AttemptID == "" {
		t.Fatal("expected latest durable launch attempt id")
	}
}

func TestStatusAndInspectSurfaceDurableLaunchControl(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch control inspectability",
	})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{
		TaskID:     string(taskID),
		HandoffID:  createOut.HandoffID,
		AcceptedBy: rundomain.WorkerKindClaude,
	}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	now := time.Now().UTC()
	if err := store.Handoffs().CreateLaunch(handoff.Launch{
		Version:      1,
		AttemptID:    "hlc_control_pending",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "launch_control_pending",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create launch control record: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LaunchControlState != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected requested-unknown launch control state, got %s", status.LaunchControlState)
	}
	if status.LaunchRetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked retry disposition, got %s", status.LaunchRetryDisposition)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected pending launch recovery class, got %s", status.RecoveryClass)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Launch == nil {
		t.Fatal("expected persisted launch in inspect output")
	}
	if inspectOut.LaunchControl == nil {
		t.Fatal("expected launch control in inspect output")
	}
	if inspectOut.LaunchControl.State != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected inspect launch control state requested-unknown, got %s", inspectOut.LaunchControl.State)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected inspect recovery class %s, got %+v", RecoveryClassHandoffLaunchPendingOutcome, inspectOut.Recovery)
	}
}

func TestLaunchHandoffBlockedUsesPacketTargetWhenRequestTargetEmpty(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	cpOut, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("get latest brief: %v", err)
	}
	cp, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("get latest checkpoint: %v", err)
	}
	if cp.CheckpointID != cpOut.CheckpointID {
		t.Fatalf("expected checkpoint %s, got %s", cpOut.CheckpointID, cp.CheckpointID)
	}

	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_codex_target_for_block_test",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindUnknown,
		TargetWorker:     rundomain.WorkerKindCodex,
		HandoffMode:      handoff.ModeResume,
		Reason:           "seed unsupported target launch packet",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     cp.CheckpointID,
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       cp.Anchor,
		IsResumable:      true,
		ResumeDescriptor: cp.ResumeDescriptor,
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create handoff packet: %v", err)
	}

	out, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: packet.HandoffID,
	})
	if err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if out.LaunchStatus != HandoffLaunchStatusBlocked {
		t.Fatalf("expected blocked status, got %s", out.LaunchStatus)
	}
	if out.TargetWorker != rundomain.WorkerKindCodex {
		t.Fatalf("expected blocked target worker to match packet target %s, got %s", rundomain.WorkerKindCodex, out.TargetWorker)
	}
	if launcher.called {
		t.Fatal("launcher should not be called for unsupported target")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func newTestCoordinatorWithLauncher(t *testing.T, store *sqlite.Store, anchorProvider anchorgit.Provider, adapter adapter_contract.WorkerAdapter, launcher adapter_contract.HandoffLauncher) *Coordinator {
	t.Helper()
	coord, err := NewCoordinator(Dependencies{
		Store:           store,
		IntentCompiler:  NewIntentStubCompiler(),
		BriefBuilder:    NewBriefBuilderV1(nil, nil),
		WorkerAdapter:   adapter,
		HandoffLauncher: launcher,
		Synthesizer:     canonical.NewSimpleSynthesizer(),
		AnchorProvider:  anchorProvider,
		ShellSessions:   NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	return coord
}

type fakeHandoffLauncher struct {
	kind      adapter_contract.WorkerKind
	result    adapter_contract.HandoffLaunchResult
	err       error
	called    bool
	callCount int
	lastReq   adapter_contract.HandoffLaunchRequest
}

func newFakeHandoffLauncherSuccess() *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_test",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(150 * time.Millisecond),
			Command:      "claude",
			Args:         []string{"--print"},
			ExitCode:     0,
			Stdout:       "claude handoff launch accepted",
			Summary:      "handoff launch accepted",
		},
	}
}

func newFakeHandoffLauncherError(err error) *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_err",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(50 * time.Millisecond),
			Command:      "claude",
			Args:         []string{},
			ExitCode:     1,
			Stderr:       "launcher failed",
			Summary:      "handoff launch failed",
		},
		err: err,
	}
}

func newFakeHandoffLauncherUnusableOutput() *fakeHandoffLauncher {
	now := time.Now().UTC()
	return &fakeHandoffLauncher{
		kind: adapter_contract.WorkerClaude,
		result: adapter_contract.HandoffLaunchResult{
			LaunchID:     "hlc_unusable",
			TargetWorker: adapter_contract.WorkerClaude,
			StartedAt:    now,
			EndedAt:      now.Add(80 * time.Millisecond),
			Command:      "claude",
			Args:         []string{"--print"},
			ExitCode:     0,
			Stdout:       "   \n  ",
			Stderr:       "",
			Summary:      "",
		},
	}
}

func (f *fakeHandoffLauncher) Name() adapter_contract.WorkerKind {
	return f.kind
}

func (f *fakeHandoffLauncher) LaunchHandoff(_ context.Context, req adapter_contract.HandoffLaunchRequest) (adapter_contract.HandoffLaunchResult, error) {
	f.called = true
	f.callCount++
	f.lastReq = req
	out := f.result
	if out.TargetWorker == "" {
		out.TargetWorker = req.TargetWorker
	}
	if out.LaunchID == "" {
		out.LaunchID = "hlc_generated"
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now().UTC()
	}
	if out.EndedAt.IsZero() {
		out.EndedAt = out.StartedAt.Add(100 * time.Millisecond)
	}
	return out, f.err
}

var _ adapter_contract.HandoffLauncher = (*fakeHandoffLauncher)(nil)

func (s *faultInjectedStore) Handoffs() storage.HandoffStore {
	return s.base.Handoffs()
}

func (s *txCountingStore) Handoffs() storage.HandoffStore {
	return s.base.Handoffs()
}

var _ storage.Store = (*faultInjectedStore)(nil)
var _ storage.Store = (*txCountingStore)(nil)
```
