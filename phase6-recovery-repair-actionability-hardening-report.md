1. Concise diagnosis of what was weak before this phase
- Tuku could classify blocked, review, decision, and repair states, but it had no narrow durable artifact for operator-side recovery progression.
- Review, decision, and repair intent lived mostly as recommended actions and proof interpretation, not as first-class task-scoped control-plane records.
- Recovery posture changed only when other orchestration state changed; explicit operator progression was not durable enough or inspectable enough.
- Status and inspect could explain what was wrong, but not what operator-side recovery action had already been taken.

2. Exact implementation plan you executed
- Added a narrow durable `recoveryaction` domain record for operator recovery progression.
- Added SQLite persistence and storage contracts for immutable recovery action records.
- Added `RecordRecoveryAction` to orchestrator service with typed action validation, transactional persistence, proof emission, and canonical response synthesis.
- Extended continuity validation to ensure latest recovery action references coherent run/checkpoint/handoff/launch state.
- Extended recovery assessment to incorporate latest recovery action so review/decision/repair progression becomes explicit and durable.
- Exposed latest and recent recovery actions through status, inspect, IPC, and daemon routing.
- Added minimal shell-viewmodel mapping for the new `REBRIEF_REQUIRED` / `REGENERATE_BRIEF` state so shell truth does not regress when the new recovery class appears.
- Added focused tests for review acknowledgment, decision progression, regenerate-brief progression, repair intent persistence, replay safety, daemon payload mapping, and shell operator labeling.

3. Files changed
- `/Users/kagaya/Desktop/Tuku/internal/domain/recoveryaction/types.go`
- `/Users/kagaya/Desktop/Tuku/internal/storage/contracts.go`
- `/Users/kagaya/Desktop/Tuku/internal/storage/sqlite/recovery_action_repo.go`
- `/Users/kagaya/Desktop/Tuku/internal/storage/sqlite/store.go`
- `/Users/kagaya/Desktop/Tuku/internal/domain/proof/types.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/orchestrator.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery_action.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/continuity.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go`
- `/Users/kagaya/Desktop/Tuku/internal/ipc/types.go`
- `/Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go`
- `/Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go`
- `/Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel.go`
- `/Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service_test.go`

4. Before vs after behavior summary
- Before: failed-run review, validation review, repair intent, and decision progression were only recommendations.
- After: those operator-side steps can be durably recorded as immutable recovery actions.
- Before: review-required states stayed review-required forever unless other state changed implicitly.
- After: explicit review acknowledgment progresses recovery into `DECISION_REQUIRED`.
- Before: operator choice to continue or regenerate the brief was not a durable control-plane artifact.
- After: the choice is recorded, proven, and reflected in recovery posture.
- Before: phase-changing recovery decisions were not safely replayable after they took effect.
- After: replay of the latest identical recovery action is idempotent, including phase-changing decisions.
- Before: status and inspect did not expose the operator recovery step that had already been taken.
- After: both surfaces expose latest recovery action, and inspect exposes recent recovery action history.

5. New recovery / repair actionability semantics introduced
- Added durable immutable recovery action kinds:
  - `FAILED_RUN_REVIEWED`
  - `VALIDATION_REVIEWED`
  - `DECISION_CONTINUE`
  - `DECISION_REGENERATE_BRIEF`
  - `REPAIR_INTENT_RECORDED`
  - `PENDING_LAUNCH_REVIEWED`
- Added `RecordRecoveryAction` orchestration flow with strict state gating:
  - failed-run review only while `FAILED_RUN_REVIEW_REQUIRED`
  - validation review only while `VALIDATION_REVIEW_REQUIRED`
  - continue / regenerate decisions only while `DECISION_REQUIRED`
  - repair intent only while `REPAIR_REQUIRED` or `BLOCKED_DRIFT`
  - pending-launch review only while `HANDOFF_LAUNCH_PENDING_OUTCOME`
- Recovery progression now recognizes durable operator actions:
  - reviewed failed/validation states advance to `DECISION_REQUIRED`
  - continue decision advances to `READY_NEXT_RUN`
  - regenerate-brief decision advances to `REBRIEF_REQUIRED`
  - repair intent preserves blocked state but records the explicit repair posture
  - pending-launch review preserves blocked retry but records that the state was reviewed
- Added `REBRIEF_REQUIRED` and `REGENERATE_BRIEF` as explicit recovery semantics.
- Important recovery actions now emit `RECOVERY_ACTION_RECORDED` proof events and canonical Tuku narration.

6. Tests added or updated
- Added orchestrator tests for:
  - failed-run review progressing to `DECISION_REQUIRED`
  - continue decision progressing to `READY_NEXT_RUN`
  - regenerate-brief decision progressing to `REBRIEF_REQUIRED`
  - repair intent remaining blocked while becoming durable and inspectable
  - idempotent replay of recovery actions
  - idempotent replay of phase-changing continue decisions
- Added daemon tests for:
  - `task.recovery.record` route
  - status/inspect IPC mapping for latest/recent recovery actions
- Added shell test for:
  - `REBRIEF_REQUIRED` / `REGENERATE_BRIEF` operator truth surfacing

7. Commands run
```bash
gofmt -w internal/domain/recoveryaction/types.go internal/storage/contracts.go internal/storage/sqlite/recovery_action_repo.go internal/storage/sqlite/store.go internal/domain/proof/types.go internal/orchestrator/orchestrator.go internal/orchestrator/recovery_action.go internal/orchestrator/recovery.go internal/orchestrator/continuity.go internal/orchestrator/service.go internal/ipc/types.go internal/ipc/payloads.go internal/runtime/daemon/service.go internal/runtime/daemon/service_test.go internal/tui/shell/viewmodel.go internal/tui/shell/viewmodel_test.go internal/orchestrator/service_test.go

go test ./internal/orchestrator ./internal/runtime/daemon ./internal/tui/shell ./internal/app -count=1
```

8. Remaining limitations / next risks
- This is still not a workflow engine. Recovery actions are durable progression evidence, not a generalized recovery automation framework.
- There is still no CLI command surface for recovery actions yet; the phase added orchestrator + daemon plumbing and proof-backed behavior first.
- Recovery progression is still projected on read, not persisted as a consolidated assessment object.
- `DECISION_REGENERATE_BRIEF` records explicit intent and moves recovery to `REBRIEF_REQUIRED`, but Tuku still does not automate brief regeneration.
- Pending launch review remains intentionally narrow: it records operator review, but does not resolve unknown launch outcome.

9. Full code for every changed file

**internal/domain/recoveryaction/types.go**

```go
package recoveryaction

import (
	"time"

	"tuku/internal/domain/common"
)

type Kind string

const (
	KindFailedRunReviewed       Kind = "FAILED_RUN_REVIEWED"
	KindValidationReviewed      Kind = "VALIDATION_REVIEWED"
	KindDecisionContinue        Kind = "DECISION_CONTINUE"
	KindDecisionRegenerateBrief Kind = "DECISION_REGENERATE_BRIEF"
	KindRepairIntentRecorded    Kind = "REPAIR_INTENT_RECORDED"
	KindPendingLaunchReviewed   Kind = "PENDING_LAUNCH_REVIEWED"
)

type Record struct {
	Version int `json:"version"`

	ActionID string        `json:"action_id"`
	TaskID   common.TaskID `json:"task_id"`
	Kind     Kind          `json:"kind"`

	RunID           common.RunID        `json:"run_id,omitempty"`
	CheckpointID    common.CheckpointID `json:"checkpoint_id,omitempty"`
	HandoffID       string              `json:"handoff_id,omitempty"`
	LaunchAttemptID string              `json:"launch_attempt_id,omitempty"`

	Summary   string    `json:"summary"`
	Notes     []string  `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Repository interface {
	Create(record Record) error
	LatestByTask(taskID common.TaskID) (Record, error)
	ListByTask(taskID common.TaskID, limit int) ([]Record, error)
}

```

**internal/storage/contracts.go**

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
	"tuku/internal/domain/recoveryaction"
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

type RecoveryActionStore interface {
	Create(record recoveryaction.Record) error
	LatestByTask(taskID common.TaskID) (recoveryaction.Record, error)
	ListByTask(taskID common.TaskID, limit int) ([]recoveryaction.Record, error)
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
	RecoveryActions() RecoveryActionStore
	ContextPacks() ContextPackStore
	PolicyDecisions() PolicyDecisionStore
	WithTx(fn func(Store) error) error
}

```

**internal/storage/sqlite/recovery_action_repo.go**

```go
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/recoveryaction"
)

type recoveryActionRepo struct{ q queryable }

func ensureRecoveryActionSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS recovery_actions (
	action_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	run_id TEXT NOT NULL,
	checkpoint_id TEXT NOT NULL,
	handoff_id TEXT NOT NULL,
	launch_attempt_id TEXT NOT NULL,
	summary TEXT NOT NULL,
	notes_json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recovery_actions_task_created
	ON recovery_actions(task_id, created_at DESC, action_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure recovery actions table: %w", err)
	}
	return nil
}

func (r *recoveryActionRepo) Create(record recoveryaction.Record) error {
	if err := ensureRecoveryActionSchema(r.q); err != nil {
		return err
	}
	notesJSON, err := marshalStringSlice(record.Notes)
	if err != nil {
		return err
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO recovery_actions(
	action_id, task_id, kind, run_id, checkpoint_id, handoff_id, launch_attempt_id,
	summary, notes_json, created_at, record_json
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
`,
		record.ActionID,
		string(record.TaskID),
		string(record.Kind),
		string(record.RunID),
		string(record.CheckpointID),
		record.HandoffID,
		record.LaunchAttemptID,
		record.Summary,
		notesJSON,
		record.CreatedAt.Format(sqliteTimestampLayout),
		string(recordJSON),
	)
	if err != nil {
		return fmt.Errorf("insert recovery action: %w", err)
	}
	return nil
}

func (r *recoveryActionRepo) LatestByTask(taskID common.TaskID) (recoveryaction.Record, error) {
	if err := ensureRecoveryActionSchema(r.q); err != nil {
		return recoveryaction.Record{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM recovery_actions
WHERE task_id = ?
ORDER BY created_at DESC, action_id DESC
LIMIT 1
`, string(taskID))
	return scanRecoveryAction(row)
}

func (r *recoveryActionRepo) ListByTask(taskID common.TaskID, limit int) ([]recoveryaction.Record, error) {
	if err := ensureRecoveryActionSchema(r.q); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT record_json
FROM recovery_actions
WHERE task_id = ?
ORDER BY created_at DESC, action_id DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, fmt.Errorf("query recovery actions: %w", err)
	}
	defer rows.Close()

	var out []recoveryaction.Record
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record recoveryaction.Record
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery actions: %w", err)
	}
	return out, nil
}

func scanRecoveryAction(row *sql.Row) (recoveryaction.Record, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return recoveryaction.Record{}, err
	}
	var record recoveryaction.Record
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return recoveryaction.Record{}, err
	}
	if record.CreatedAt.IsZero() {
		// Backstop older records if schema is reused in tests.
		record.CreatedAt = time.Now().UTC()
	}
	return record, nil
}

```

**internal/storage/sqlite/store.go**

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

func (s *Store) RecoveryActions() storage.RecoveryActionStore {
	return &recoveryActionRepo{q: s.db}
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

func (s *txStore) RecoveryActions() storage.RecoveryActionStore {
	return &recoveryActionRepo{q: s.tx}
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
	if err := ensureRecoveryActionSchema(db); err != nil {
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

**internal/domain/proof/types.go**

```go
package proof

import (
	"time"

	"tuku/internal/domain/common"
)

type EventType string

const (
	EventUserMessageReceived              EventType = "USER_MESSAGE_RECEIVED"
	EventIntentCompiled                   EventType = "INTENT_COMPILED"
	EventBriefCreated                     EventType = "BRIEF_CREATED"
	EventWorkerRunStarted                 EventType = "WORKER_RUN_STARTED"
	EventWorkerRunCompleted               EventType = "WORKER_RUN_COMPLETED"
	EventWorkerRunFailed                  EventType = "WORKER_RUN_FAILED"
	EventWorkerOutputCaptured             EventType = "WORKER_OUTPUT_CAPTURED"
	EventWorkerCommandExecuted            EventType = "WORKER_COMMAND_EXECUTED"
	EventFileChangeDetected               EventType = "FILE_CHANGE_DETECTED"
	EventValidationResult                 EventType = "VALIDATION_RESULT"
	EventPolicyDecisionRequested          EventType = "POLICY_DECISION_REQUESTED"
	EventPolicyDecisionResolved           EventType = "POLICY_DECISION_RESOLVED"
	EventCheckpointCreated                EventType = "CHECKPOINT_CREATED"
	EventContinueAssessed                 EventType = "CONTINUE_ASSESSED"
	EventHandoffCreated                   EventType = "HANDOFF_CREATED"
	EventHandoffAccepted                  EventType = "HANDOFF_ACCEPTED"
	EventHandoffBlocked                   EventType = "HANDOFF_BLOCKED"
	EventHandoffLaunchRequested           EventType = "HANDOFF_LAUNCH_REQUESTED"
	EventHandoffLaunchCompleted           EventType = "HANDOFF_LAUNCH_COMPLETED"
	EventHandoffLaunchFailed              EventType = "HANDOFF_LAUNCH_FAILED"
	EventHandoffLaunchBlocked             EventType = "HANDOFF_LAUNCH_BLOCKED"
	EventHandoffAcknowledgmentCaptured    EventType = "HANDOFF_ACKNOWLEDGMENT_CAPTURED"
	EventHandoffAcknowledgmentUnavailable EventType = "HANDOFF_ACKNOWLEDGMENT_UNAVAILABLE"
	EventRecoveryActionRecorded           EventType = "RECOVERY_ACTION_RECORDED"
	EventRunInterrupted                   EventType = "RUN_INTERRUPTED"
	EventRunResumed                       EventType = "RUN_RESUMED"
	EventShellHostStarted                 EventType = "SHELL_HOST_STARTED"
	EventShellHostExited                  EventType = "SHELL_HOST_EXITED"
	EventShellFallbackActivated           EventType = "SHELL_FALLBACK_ACTIVATED"
	EventCanonicalResponseEmitted         EventType = "CANONICAL_RESPONSE_EMITTED"
	EventTaskPhaseTransitioned            EventType = "TASK_PHASE_TRANSITIONED"
)

type ActorType string

const (
	ActorUser   ActorType = "USER"
	ActorSystem ActorType = "SYSTEM"
	ActorWorker ActorType = "WORKER"
)

type Event struct {
	EventID             common.EventID        `json:"event_id"`
	TaskID              common.TaskID         `json:"task_id"`
	RunID               *common.RunID         `json:"run_id,omitempty"`
	CheckpointID        *common.CheckpointID  `json:"checkpoint_id,omitempty"`
	SequenceNo          int64                 `json:"sequence_no"`
	Timestamp           time.Time             `json:"timestamp"`
	Type                EventType             `json:"type"`
	ActorType           ActorType             `json:"actor_type"`
	ActorID             string                `json:"actor_id"`
	PayloadJSON         string                `json:"payload_json"`
	CausalParentEventID *common.EventID       `json:"causal_parent_event_id,omitempty"`
	CapsuleVersion      common.CapsuleVersion `json:"capsule_version"`
}

type Ledger interface {
	Append(event Event) error
	ListByTask(taskID common.TaskID, limit int) ([]Event, error)
}

// ProofCard is evidence plus a decision interface artifact.
type ProofCard struct {
	Version           int                 `json:"version"`
	CardID            string              `json:"card_id"`
	TaskID            common.TaskID       `json:"task_id"`
	CheckpointID      common.CheckpointID `json:"checkpoint_id"`
	EventRangeStart   common.EventID      `json:"event_range_start"`
	EventRangeEnd     common.EventID      `json:"event_range_end"`
	WhatChanged       []string            `json:"what_changed"`
	WhatVerified      []string            `json:"what_verified"`
	WhatFailed        []string            `json:"what_failed"`
	Unknowns          []string            `json:"unknowns"`
	ConfidenceNotes   []string            `json:"confidence_notes"`
	RiskNotes         []string            `json:"risk_notes"`
	DecisionRequired  bool                `json:"decision_required"`
	DecisionPrompt    string              `json:"decision_prompt"`
	DecisionOptions   []string            `json:"decision_options"`
	RecommendedOption string              `json:"recommended_option"`
}

```

**internal/orchestrator/orchestrator.go**

```go
package orchestrator

import "context"

type Service interface {
	StartTask(ctx context.Context, goal string, repoRoot string) (StartTaskResult, error)
	ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (ResolveShellTaskResult, error)
	MessageTask(ctx context.Context, taskID string, message string) (MessageTaskResult, error)
	RunTask(ctx context.Context, req RunTaskRequest) (RunTaskResult, error)
	ContinueTask(ctx context.Context, taskID string) (ContinueTaskResult, error)
	CreateCheckpoint(ctx context.Context, taskID string) (CreateCheckpointResult, error)
	CreateHandoff(ctx context.Context, req CreateHandoffRequest) (CreateHandoffResult, error)
	AcceptHandoff(ctx context.Context, req AcceptHandoffRequest) (AcceptHandoffResult, error)
	LaunchHandoff(ctx context.Context, req LaunchHandoffRequest) (LaunchHandoffResult, error)
	RecordRecoveryAction(ctx context.Context, req RecordRecoveryActionRequest) (RecordRecoveryActionResult, error)
	StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error)
	InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error)
	ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error)
	RecordShellLifecycle(ctx context.Context, req RecordShellLifecycleRequest) (RecordShellLifecycleResult, error)
	ReportShellSession(ctx context.Context, req ReportShellSessionRequest) (ReportShellSessionResult, error)
	ListShellSessions(ctx context.Context, taskID string) (ListShellSessionsResult, error)
}

```

**internal/orchestrator/recovery_action.go**

```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
)

type RecordRecoveryActionRequest struct {
	TaskID  string
	Kind    recoveryaction.Kind
	Summary string
	Notes   []string
}

type RecordRecoveryActionResult struct {
	TaskID                common.TaskID
	Action                recoveryaction.Record
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

type recoveryPhaseUpdate struct {
	Phase      phase.Phase
	NextAction string
	Reason     string
}

func (c *Coordinator) RecordRecoveryAction(ctx context.Context, req RecordRecoveryActionRequest) (RecordRecoveryActionResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordRecoveryActionResult{}, fmt.Errorf("task id is required")
	}
	if req.Kind == "" {
		return RecordRecoveryActionResult{}, fmt.Errorf("recovery action kind is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecordRecoveryActionResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if replayableLatestRecoveryAction(req, assessment.LatestRecoveryAction) {
		return RecordRecoveryActionResult{
			TaskID:                taskID,
			Action:                *assessment.LatestRecoveryAction,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     recoveryActionCanonical(*assessment.LatestRecoveryAction, recovery),
		}, nil
	}
	prepared, err := c.prepareRecoveryActionRecord(assessment, recovery, req)
	if err != nil {
		return RecordRecoveryActionResult{}, err
	}
	if reusableRecoveryAction(prepared.Template, assessment.LatestRecoveryAction) {
		return RecordRecoveryActionResult{
			TaskID:                taskID,
			Action:                *assessment.LatestRecoveryAction,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     recoveryActionCanonical(*assessment.LatestRecoveryAction, recovery),
		}, nil
	}

	var result RecordRecoveryActionResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return fmt.Errorf("task state changed during recovery action preparation (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version)
		}

		runID := recoveryActionRunID(prepared.Template)
		if prepared.PhaseUpdate != nil {
			caps.Version++
			caps.UpdatedAt = txc.clock()
			caps.CurrentPhase = prepared.PhaseUpdate.Phase
			caps.NextAction = prepared.PhaseUpdate.NextAction
			if err := txc.store.Capsules().Update(caps); err != nil {
				return err
			}
			if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
				"phase":  caps.CurrentPhase,
				"reason": prepared.PhaseUpdate.Reason,
			}, runID); err != nil {
				return err
			}
		}

		actionRecord := prepared.Template
		actionRecord.ActionID = txc.idGenerator("ract")
		actionRecord.CreatedAt = txc.clock()
		if err := txc.store.RecoveryActions().Create(actionRecord); err != nil {
			return err
		}

		afterAssessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		afterRecovery := txc.recoveryFromContinueAssessment(afterAssessment)
		canonical := recoveryActionCanonical(actionRecord, afterRecovery)
		payload := map[string]any{
			"action_id":                actionRecord.ActionID,
			"kind":                     actionRecord.Kind,
			"summary":                  actionRecord.Summary,
			"notes":                    actionRecord.Notes,
			"run_id":                   actionRecord.RunID,
			"checkpoint_id":            actionRecord.CheckpointID,
			"handoff_id":               actionRecord.HandoffID,
			"launch_attempt_id":        actionRecord.LaunchAttemptID,
			"recovery_class":           afterRecovery.RecoveryClass,
			"recommended_action":       afterRecovery.RecommendedAction,
			"ready_for_next_run":       afterRecovery.ReadyForNextRun,
			"ready_for_handoff_launch": afterRecovery.ReadyForHandoffLaunch,
			"recovery_reason":          afterRecovery.Reason,
		}
		if err := txc.appendProof(caps, proof.EventRecoveryActionRecorded, proof.ActorUser, "user", payload, runID); err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = RecordRecoveryActionResult{
			TaskID:                taskID,
			Action:                actionRecord,
			RecoveryClass:         afterRecovery.RecoveryClass,
			RecommendedAction:     afterRecovery.RecommendedAction,
			ReadyForNextRun:       afterRecovery.ReadyForNextRun,
			ReadyForHandoffLaunch: afterRecovery.ReadyForHandoffLaunch,
			RecoveryReason:        afterRecovery.Reason,
			CanonicalResponse:     canonical,
		}
		return nil
	})
	if err != nil {
		return RecordRecoveryActionResult{}, err
	}
	return result, nil
}

type preparedRecoveryAction struct {
	Template    recoveryaction.Record
	PhaseUpdate *recoveryPhaseUpdate
}

func (c *Coordinator) prepareRecoveryActionRecord(assessment continueAssessment, recovery RecoveryAssessment, req RecordRecoveryActionRequest) (preparedRecoveryAction, error) {
	template := recoveryaction.Record{
		Version: 1,
		TaskID:  assessment.TaskID,
		Kind:    req.Kind,
		Notes:   normalizedRecoveryNotes(req.Notes),
	}
	var phaseUpdate *recoveryPhaseUpdate

	switch req.Kind {
	case recoveryaction.KindFailedRunReviewed:
		if recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
			return preparedRecoveryAction{}, fmt.Errorf("failed-run review can only be recorded while recovery class is %s", RecoveryClassFailedRunReviewRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, fmt.Sprintf("Failed run %s reviewed.", nonEmpty(string(recovery.RunID), "unknown")))
	case recoveryaction.KindValidationReviewed:
		if recovery.RecoveryClass != RecoveryClassValidationReviewRequired {
			return preparedRecoveryAction{}, fmt.Errorf("validation review can only be recorded while recovery class is %s", RecoveryClassValidationReviewRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, fmt.Sprintf("Validation state for run %s reviewed.", nonEmpty(string(recovery.RunID), "unknown")))
	case recoveryaction.KindDecisionContinue:
		if recovery.RecoveryClass != RecoveryClassDecisionRequired {
			return preparedRecoveryAction{}, fmt.Errorf("continue decision can only be recorded while recovery class is %s", RecoveryClassDecisionRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.HandoffID = recovery.HandoffID
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Operator chose to continue with the current brief.")
		phaseUpdate = &recoveryPhaseUpdate{
			Phase:      phase.PhaseBriefReady,
			NextAction: "Execution brief is ready. Start a run with `tuku run --task <id>`.",
			Reason:     "operator recorded decision to continue with current brief",
		}
	case recoveryaction.KindDecisionRegenerateBrief:
		if recovery.RecoveryClass != RecoveryClassDecisionRequired {
			return preparedRecoveryAction{}, fmt.Errorf("regenerate-brief decision can only be recorded while recovery class is %s", RecoveryClassDecisionRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.HandoffID = recovery.HandoffID
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Operator chose to regenerate the execution brief before another run.")
		phaseUpdate = &recoveryPhaseUpdate{
			Phase:      phase.PhaseBlocked,
			NextAction: "Regenerate or replace the execution brief before starting another bounded run.",
			Reason:     "operator recorded decision to regenerate brief before continuing",
		}
	case recoveryaction.KindRepairIntentRecorded:
		if recovery.RecoveryClass != RecoveryClassRepairRequired && recovery.RecoveryClass != RecoveryClassBlockedDrift {
			return preparedRecoveryAction{}, fmt.Errorf("repair intent can only be recorded while recovery class is %s or %s", RecoveryClassRepairRequired, RecoveryClassBlockedDrift)
		}
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Operator recorded repair intent for the current continuity issue.")
	case recoveryaction.KindPendingLaunchReviewed:
		if recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
			return preparedRecoveryAction{}, fmt.Errorf("pending-launch review can only be recorded while recovery class is %s", RecoveryClassHandoffLaunchPendingOutcome)
		}
		template.HandoffID = recovery.HandoffID
		if assessment.LatestLaunch != nil {
			template.LaunchAttemptID = assessment.LatestLaunch.AttemptID
		}
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Pending handoff launch reviewed; waiting for durable outcome.")
	default:
		return preparedRecoveryAction{}, fmt.Errorf("unsupported recovery action kind: %s", req.Kind)
	}

	return preparedRecoveryAction{Template: template, PhaseUpdate: phaseUpdate}, nil
}

func reusableRecoveryAction(candidate recoveryaction.Record, latest *recoveryaction.Record) bool {
	if latest == nil {
		return false
	}
	if latest.TaskID != candidate.TaskID || latest.Kind != candidate.Kind {
		return false
	}
	if latest.RunID != candidate.RunID || latest.CheckpointID != candidate.CheckpointID {
		return false
	}
	if latest.HandoffID != candidate.HandoffID || latest.LaunchAttemptID != candidate.LaunchAttemptID {
		return false
	}
	if strings.TrimSpace(latest.Summary) != strings.TrimSpace(candidate.Summary) {
		return false
	}
	if len(latest.Notes) != len(candidate.Notes) {
		return false
	}
	for i := range latest.Notes {
		if latest.Notes[i] != candidate.Notes[i] {
			return false
		}
	}
	return true
}

func normalizedRecoveryNotes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func replayableLatestRecoveryAction(req RecordRecoveryActionRequest, latest *recoveryaction.Record) bool {
	if latest == nil || latest.Kind != req.Kind {
		return false
	}
	if !stringSlicesEqual(normalizedRecoveryNotes(req.Notes), latest.Notes) {
		return false
	}
	summary := strings.TrimSpace(req.Summary)
	return summary == "" || summary == strings.TrimSpace(latest.Summary)
}

func nonEmptyRecoverySummary(requested string, fallback string) string {
	if strings.TrimSpace(requested) == "" {
		return fallback
	}
	return strings.TrimSpace(requested)
}

func recoveryActionRunID(record recoveryaction.Record) *common.RunID {
	if record.RunID == "" {
		return nil
	}
	id := record.RunID
	return &id
}

func recoveryActionCanonical(record recoveryaction.Record, recovery RecoveryAssessment) string {
	switch record.Kind {
	case recoveryaction.KindFailedRunReviewed:
		return fmt.Sprintf("I recorded review of failed run %s. The next explicit recovery step is to decide whether to continue with the current brief or regenerate it.", nonEmpty(string(record.RunID), "unknown"))
	case recoveryaction.KindValidationReviewed:
		return fmt.Sprintf("I recorded validation review for run %s. The next explicit recovery step is to decide whether to continue with the current brief or regenerate it.", nonEmpty(string(record.RunID), "unknown"))
	case recoveryaction.KindDecisionContinue:
		return "I recorded the operator decision to continue with the current brief. The task is now ready for the next bounded run."
	case recoveryaction.KindDecisionRegenerateBrief:
		return "I recorded the operator decision to regenerate the execution brief before another run. Tuku will not claim next-run readiness until a new or revised brief exists."
	case recoveryaction.KindRepairIntentRecorded:
		return fmt.Sprintf("I recorded repair intent for the current continuity issue. The task remains blocked until the repair is carried out. %s", strings.TrimSpace(recovery.Reason))
	case recoveryaction.KindPendingLaunchReviewed:
		return "I recorded review of the pending handoff launch. Retry remains blocked until Tuku has a durable launch outcome."
	default:
		return fmt.Sprintf("I recorded recovery action %s. Current recovery state is %s.", record.Kind, recovery.RecoveryClass)
	}
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

```

**internal/orchestrator/recovery.go**

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
	"tuku/internal/domain/recoveryaction"
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
	RecoveryClassRebriefRequired                RecoveryClass = "REBRIEF_REQUIRED"
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
	RecoveryActionRegenerateBrief        RecoveryAction = "REGENERATE_BRIEF"
)

type RecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RecoveryAssessment struct {
	TaskID                 common.TaskID          `json:"task_id"`
	ContinuityOutcome      ContinueOutcome        `json:"continuity_outcome"`
	RecoveryClass          RecoveryClass          `json:"recovery_class"`
	RecommendedAction      RecoveryAction         `json:"recommended_action"`
	ReadyForNextRun        bool                   `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                   `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                   `json:"requires_decision,omitempty"`
	RequiresRepair         bool                   `json:"requires_repair,omitempty"`
	RequiresReview         bool                   `json:"requires_review,omitempty"`
	RequiresReconciliation bool                   `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass  `json:"drift_class,omitempty"`
	Reason                 string                 `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID    `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID           `json:"run_id,omitempty"`
	HandoffID              string                 `json:"handoff_id,omitempty"`
	HandoffStatus          handoff.Status         `json:"handoff_status,omitempty"`
	LatestAction           *recoveryaction.Record `json:"latest_action,omitempty"`
	Issues                 []RecoveryIssue        `json:"issues,omitempty"`
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
	if assessment.LatestRecoveryAction != nil {
		actionCopy := *assessment.LatestRecoveryAction
		recovery.LatestAction = &actionCopy
	}

	switch assessment.Outcome {
	case ContinueOutcomeBlockedInconsistent:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = "continuity state is inconsistent and must be repaired before recovery"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeBlockedDrift:
		recovery.RecoveryClass = RecoveryClassBlockedDrift
		recovery.RecommendedAction = RecoveryActionMakeResumeDecision
		recovery.RequiresDecision = true
		if recovery.Reason == "" {
			recovery.Reason = "repository drift blocks automatic recovery"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeNeedsDecision:
		recovery.RecoveryClass = RecoveryClassDecisionRequired
		recovery.RecommendedAction = RecoveryActionMakeResumeDecision
		recovery.RequiresDecision = true
		if recovery.Reason == "" {
			recovery.Reason = "resume requires an explicit operator decision"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeStaleReconciled:
		recovery.RecoveryClass = RecoveryClassStaleRunReconciliationRequired
		recovery.RecommendedAction = RecoveryActionReconcileStaleRun
		recovery.RequiresReconciliation = true
		if recovery.Reason == "" {
			recovery.Reason = "latest run is still durably RUNNING and must be reconciled before recovery"
		}
		return applyRecoveryActionProgression(recovery)
	case ContinueOutcomeSafe:
		// Continue with operational recovery classification below.
	default:
		recovery.RecoveryClass = RecoveryClassRepairRequired
		recovery.RecommendedAction = RecoveryActionRepairContinuity
		recovery.RequiresRepair = true
		if recovery.Reason == "" {
			recovery.Reason = fmt.Sprintf("unsupported continuity outcome: %s", assessment.Outcome)
		}
		return applyRecoveryActionProgression(recovery)
	}

	if packet := assessment.LatestHandoff; packet != nil && packet.Status == handoff.StatusAccepted && packet.TargetWorker == rundomain.WorkerKindClaude && packet.IsResumable {
		control := assessLaunchControl(assessment.TaskID, packet, assessment.LatestLaunch)
		switch control.State {
		case LaunchControlStateNotRequested:
			recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
			recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
			recovery.ReadyForHandoffLaunch = true
			recovery.Reason = fmt.Sprintf("accepted handoff %s is ready to launch for %s", packet.HandoffID, packet.TargetWorker)
			return applyRecoveryActionProgression(recovery)
		case LaunchControlStateFailed:
			recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
			recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
			recovery.ReadyForHandoffLaunch = control.RetryDisposition == LaunchRetryDispositionAllowed
			recovery.Reason = control.Reason
			return applyRecoveryActionProgression(recovery)
		case LaunchControlStateRequestedOutcomeUnknown:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchPendingOutcome
			recovery.RecommendedAction = RecoveryActionWaitForLaunchOutcome
			recovery.Reason = control.Reason
			return applyRecoveryActionProgression(recovery)
		case LaunchControlStateCompleted:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchCompleted
			recovery.RecommendedAction = RecoveryActionMonitorLaunchedHandoff
			recovery.Reason = control.Reason
			return applyRecoveryActionProgression(recovery)
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
				return applyRecoveryActionProgression(recovery)
			}
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = fmt.Sprintf("interrupted run %s has no resumable checkpoint for recovery", runRec.RunID)
			return applyRecoveryActionProgression(recovery)
		case rundomain.StatusFailed:
			recovery.RecoveryClass = RecoveryClassFailedRunReviewRequired
			recovery.RecommendedAction = RecoveryActionInspectFailedRun
			recovery.RequiresReview = true
			recovery.Reason = fmt.Sprintf("latest run %s failed; inspect failure evidence before retrying or regenerating the brief", runRec.RunID)
			return applyRecoveryActionProgression(recovery)
		case rundomain.StatusCompleted:
			switch assessment.Capsule.CurrentPhase {
			case phase.PhaseValidating:
				recovery.RecoveryClass = RecoveryClassValidationReviewRequired
				recovery.RecommendedAction = RecoveryActionReviewValidation
				recovery.RequiresReview = true
				recovery.Reason = fmt.Sprintf("latest run %s completed and task is awaiting validation review", runRec.RunID)
				return applyRecoveryActionProgression(recovery)
			case phase.PhaseCompleted:
				recovery.RecoveryClass = RecoveryClassCompletedNoAction
				recovery.RecommendedAction = RecoveryActionNone
				recovery.Reason = "task is already completed; no recovery action is required"
				return applyRecoveryActionProgression(recovery)
			case phase.PhaseBriefReady:
				recovery.RecoveryClass = RecoveryClassReadyNextRun
				recovery.RecommendedAction = RecoveryActionStartNextRun
				recovery.ReadyForNextRun = true
				recovery.Reason = fmt.Sprintf("task is ready for the next bounded run with brief %s", assessment.Capsule.CurrentBriefID)
				return applyRecoveryActionProgression(recovery)
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

	return applyRecoveryActionProgression(recovery)
}

func applyRecoveryActionProgression(recovery RecoveryAssessment) RecoveryAssessment {
	if recovery.LatestAction == nil {
		return recovery
	}
	action := recovery.LatestAction
	switch action.Kind {
	case recoveryaction.KindFailedRunReviewed:
		if recovery.RecoveryClass == RecoveryClassFailedRunReviewRequired && action.RunID == recovery.RunID {
			recovery.RecoveryClass = RecoveryClassDecisionRequired
			recovery.RecommendedAction = RecoveryActionMakeResumeDecision
			recovery.RequiresReview = false
			recovery.RequiresDecision = true
			recovery.Reason = fmt.Sprintf("failed run %s was reviewed; choose whether to continue with the current brief or regenerate it", recovery.RunID)
		}
	case recoveryaction.KindValidationReviewed:
		if recovery.RecoveryClass == RecoveryClassValidationReviewRequired && (recovery.RunID == "" || action.RunID == recovery.RunID) {
			recovery.RecoveryClass = RecoveryClassDecisionRequired
			recovery.RecommendedAction = RecoveryActionMakeResumeDecision
			recovery.RequiresReview = false
			recovery.RequiresDecision = true
			recovery.Reason = fmt.Sprintf("validation state for run %s was reviewed; choose whether to continue with the current brief or regenerate it", nonEmpty(string(recovery.RunID), "unknown"))
		}
	case recoveryaction.KindRepairIntentRecorded:
		if recovery.RecoveryClass == RecoveryClassRepairRequired || recovery.RecoveryClass == RecoveryClassBlockedDrift {
			recovery.Reason = fmt.Sprintf("repair intent recorded: %s", action.Summary)
		}
	case recoveryaction.KindPendingLaunchReviewed:
		if recovery.RecoveryClass == RecoveryClassHandoffLaunchPendingOutcome {
			recovery.Reason = fmt.Sprintf("pending handoff launch was reviewed: %s", action.Summary)
		}
	case recoveryaction.KindDecisionContinue:
		switch recovery.RecoveryClass {
		case RecoveryClassDecisionRequired, RecoveryClassFailedRunReviewRequired, RecoveryClassValidationReviewRequired, RecoveryClassReadyNextRun:
			recovery.RecoveryClass = RecoveryClassReadyNextRun
			recovery.RecommendedAction = RecoveryActionStartNextRun
			recovery.ReadyForNextRun = true
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator chose to continue with the current brief: %s", action.Summary)
		}
	case recoveryaction.KindDecisionRegenerateBrief:
		recovery.RecoveryClass = RecoveryClassRebriefRequired
		recovery.RecommendedAction = RecoveryActionRegenerateBrief
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.Reason = fmt.Sprintf("operator chose to regenerate the execution brief before another run: %s", action.Summary)
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
	if recovery.LatestAction != nil {
		actionCopy := *recovery.LatestAction
		status.LatestRecoveryAction = &actionCopy
	} else {
		status.LatestRecoveryAction = nil
	}
	if recovery.Reason != "" {
		status.ResumeDescriptor = recovery.Reason
	}
}

```

**internal/orchestrator/continuity.go**

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
	"tuku/internal/domain/recoveryaction"
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
	continuityViolationLatestRecoveryActionInvalid     continuityViolationCode = "LATEST_RECOVERY_ACTION_INVALID"
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
	LatestRecoveryAction *recoveryaction.Record
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

	if latestAction, err := c.store.RecoveryActions().LatestByTask(taskID); err == nil {
		actionCopy := latestAction
		snapshot.LatestRecoveryAction = &actionCopy
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
	if snapshot.LatestRecoveryAction != nil {
		actionViolations, err := c.validateRecoveryActionContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, actionViolations...)
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

func (c *Coordinator) validateRecoveryActionContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	action := snapshot.LatestRecoveryAction
	violations := make([]continuityViolation, 0, 4)
	if action == nil {
		return violations, nil
	}
	if action.TaskID != snapshot.Capsule.TaskID {
		violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action task mismatch: action task=%s capsule task=%s", action.TaskID, snapshot.Capsule.TaskID)})
	}
	if action.RunID != "" {
		runRec, err := c.store.Runs().Get(action.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing run %s", action.RunID)})
			} else {
				return nil, err
			}
		} else if runRec.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action run task mismatch: run task=%s capsule task=%s", runRec.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.CheckpointID != "" {
		cp, err := c.store.Checkpoints().Get(action.CheckpointID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing checkpoint %s", action.CheckpointID)})
			} else {
				return nil, err
			}
		} else if cp.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.HandoffID != "" {
		packet, err := c.store.Handoffs().Get(action.HandoffID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing handoff %s", action.HandoffID)})
			} else {
				return nil, err
			}
		} else if packet.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action handoff task mismatch: handoff task=%s capsule task=%s", packet.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.LaunchAttemptID != "" {
		launch, err := c.store.Handoffs().GetLaunch(action.LaunchAttemptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing launch attempt %s", action.LaunchAttemptID)})
			} else {
				return nil, err
			}
		} else if launch.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action launch task mismatch: launch task=%s capsule task=%s", launch.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	return violations, nil
}

```

**internal/orchestrator/service.go**

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
	"tuku/internal/domain/recoveryaction"
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
	LatestRecoveryAction    *recoveryaction.Record
	LastEventID             common.EventID
	LastEventType           proof.EventType
	LastEventAt             time.Time
}

type InspectTaskResult struct {
	TaskID                common.TaskID
	Intent                *intent.State
	Brief                 *brief.ExecutionBrief
	Run                   *rundomain.ExecutionRun
	Checkpoint            *checkpoint.Checkpoint
	Handoff               *handoff.Packet
	Launch                *handoff.Launch
	Acknowledgment        *handoff.Acknowledgment
	LaunchControl         *LaunchControl
	Recovery              *RecoveryAssessment
	LatestRecoveryAction  *recoveryaction.Record
	RecentRecoveryActions []recoveryaction.Record
	RepoAnchor            anchorgit.Snapshot
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
	TaskID               common.TaskID
	Capsule              capsule.WorkCapsule
	LatestRun            *rundomain.ExecutionRun
	LatestCheckpoint     *checkpoint.Checkpoint
	LatestHandoff        *handoff.Packet
	LatestLaunch         *handoff.Launch
	LatestAck            *handoff.Acknowledgment
	LatestRecoveryAction *recoveryaction.Record
	FreshAnchor          anchorgit.Snapshot
	DriftClass           checkpoint.DriftClass
	Outcome              ContinueOutcome
	Reason               string
	Issues               []continuityViolation
	RequiresMutation     bool
	ReuseCheckpointID    common.CheckpointID
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
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeBlockedInconsistent,
			Reason:               issue,
			Issues:               issues,
			DriftClass:           checkpoint.DriftNone,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if snapshot.LatestRun != nil && snapshot.LatestRun.Status == rundomain.StatusRunning {
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeStaleReconciled,
			Reason:               "latest run is durably RUNNING and requires explicit stale reconciliation",
			Issues:               issues,
			DriftClass:           checkpoint.DriftNone,
			RequiresMutation:     true,
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
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              outcome,
			Reason:               reason,
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	if drift == checkpoint.DriftMajor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeBlockedDrift,
			Reason:               "major repo drift blocks direct resume",
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}
	if drift == checkpoint.DriftMinor {
		reuse := c.canReuseDecisionCheckpoint(caps, snapshot.LatestCheckpoint, anchor)
		return continueAssessment{
			TaskID:               taskID,
			Capsule:              caps,
			LatestRun:            snapshot.LatestRun,
			LatestCheckpoint:     snapshot.LatestCheckpoint,
			LatestHandoff:        snapshot.LatestHandoff,
			LatestLaunch:         snapshot.LatestLaunch,
			LatestAck:            snapshot.LatestAcknowledgment,
			LatestRecoveryAction: snapshot.LatestRecoveryAction,
			FreshAnchor:          anchor,
			Outcome:              ContinueOutcomeNeedsDecision,
			Reason:               "minor repo drift requires explicit decision",
			Issues:               issues,
			DriftClass:           drift,
			RequiresMutation:     !reuse,
			ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuse),
		}, nil
	}

	reuseSafe := c.canReuseSafeCheckpoint(caps, snapshot.LatestRun, snapshot.LatestCheckpoint, anchor)
	return continueAssessment{
		TaskID:               taskID,
		Capsule:              caps,
		LatestRun:            snapshot.LatestRun,
		LatestCheckpoint:     snapshot.LatestCheckpoint,
		LatestHandoff:        snapshot.LatestHandoff,
		LatestLaunch:         snapshot.LatestLaunch,
		LatestAck:            snapshot.LatestAcknowledgment,
		LatestRecoveryAction: snapshot.LatestRecoveryAction,
		FreshAnchor:          anchor,
		Outcome:              ContinueOutcomeSafe,
		Reason:               "safe resume is available from continuity state",
		Issues:               issues,
		DriftClass:           checkpoint.DriftNone,
		RequiresMutation:     !reuseSafe,
		ReuseCheckpointID:    reusableCheckpointID(snapshot.LatestCheckpoint, reuseSafe),
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
		case RecoveryClassRebriefRequired:
			base.CanonicalResponse = "Continuity is intact, but the next run is blocked until the execution brief is regenerated or replaced. No new checkpoint was created."
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
		recovery := c.recoveryFromContinueAssessment(assessment)
		applyRecoveryAssessmentToStatus(&status, recovery, checkpointResumable)
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
	if latestAction, err := c.store.RecoveryActions().LatestByTask(caps.TaskID); err == nil {
		actionCopy := latestAction
		out.LatestRecoveryAction = &actionCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if actions, err := c.store.RecoveryActions().ListByTask(caps.TaskID, 5); err == nil {
		out.RecentRecoveryActions = append([]recoveryaction.Record{}, actions...)
	} else {
		return InspectTaskResult{}, err
	}
	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return InspectTaskResult{}, err
	} else {
		recovery := c.recoveryFromContinueAssessment(assessment)
		out.Recovery = &recovery
		if recovery.LatestAction != nil {
			actionCopy := *recovery.LatestAction
			out.LatestRecoveryAction = &actionCopy
		}
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

**internal/ipc/types.go**

```go
package ipc

import "encoding/json"

type Method string

const (
	MethodStartTask               Method = "task.start"
	MethodResolveShellTaskForRepo Method = "task.shell.resolve"
	MethodSendMessage             Method = "task.message"
	MethodContinueTask            Method = "task.continue"
	MethodRecordRecoveryAction    Method = "task.recovery.record"
	MethodTaskRun                 Method = "task.run"
	MethodTaskStatus              Method = "task.status"
	MethodTaskInspect             Method = "task.inspect"
	MethodTaskShellSnapshot       Method = "task.shell.snapshot"
	MethodTaskShellLifecycle      Method = "task.shell.lifecycle"
	MethodTaskShellSessionReport  Method = "task.shell.session.report"
	MethodTaskShellSessions       Method = "task.shell.sessions"
	MethodCreateCheckpoint        Method = "task.checkpoint"
	MethodCreateHandoff           Method = "task.handoff.create"
	MethodAcceptHandoff           Method = "task.handoff.accept"
	MethodLaunchHandoff           Method = "task.handoff.launch"
	MethodApproveDecision         Method = "task.approve"
	MethodRejectDecision          Method = "task.reject"
)

type Request struct {
	RequestID string          `json:"request_id"`
	Method    Method          `json:"method"`
	Payload   json.RawMessage `json:"payload"`
}

type Response struct {
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     *ErrorPayload   `json:"error,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

```

**internal/ipc/payloads.go**

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

type TaskRecoveryActionRecord struct {
	ActionID        string              `json:"action_id"`
	TaskID          common.TaskID       `json:"task_id"`
	Kind            string              `json:"kind"`
	RunID           common.RunID        `json:"run_id,omitempty"`
	CheckpointID    common.CheckpointID `json:"checkpoint_id,omitempty"`
	HandoffID       string              `json:"handoff_id,omitempty"`
	LaunchAttemptID string              `json:"launch_attempt_id,omitempty"`
	Summary         string              `json:"summary,omitempty"`
	Notes           []string            `json:"notes,omitempty"`
	CreatedAtUnixMs int64               `json:"created_at_unix_ms,omitempty"`
}

type TaskStatusResponse struct {
	TaskID                   common.TaskID             `json:"task_id"`
	ConversationID           common.ConversationID     `json:"conversation_id"`
	Goal                     string                    `json:"goal"`
	Phase                    phase.Phase               `json:"phase"`
	Status                   string                    `json:"status"`
	CurrentIntentID          common.IntentID           `json:"current_intent_id"`
	CurrentIntentClass       string                    `json:"current_intent_class,omitempty"`
	CurrentIntentSummary     string                    `json:"current_intent_summary,omitempty"`
	CurrentBriefID           common.BriefID            `json:"current_brief_id,omitempty"`
	CurrentBriefHash         string                    `json:"current_brief_hash,omitempty"`
	LatestRunID              common.RunID              `json:"latest_run_id,omitempty"`
	LatestRunStatus          run.Status                `json:"latest_run_status,omitempty"`
	LatestRunSummary         string                    `json:"latest_run_summary,omitempty"`
	RepoAnchor               RepoAnchor                `json:"repo_anchor"`
	LatestCheckpointID       common.CheckpointID       `json:"latest_checkpoint_id,omitempty"`
	LatestCheckpointAtUnixMs int64                     `json:"latest_checkpoint_at_unix_ms,omitempty"`
	LatestCheckpointTrigger  string                    `json:"latest_checkpoint_trigger,omitempty"`
	CheckpointResumable      bool                      `json:"checkpoint_resumable,omitempty"`
	ResumeDescriptor         string                    `json:"resume_descriptor,omitempty"`
	LatestLaunchAttemptID    string                    `json:"latest_launch_attempt_id,omitempty"`
	LatestLaunchID           string                    `json:"latest_launch_id,omitempty"`
	LatestLaunchStatus       string                    `json:"latest_launch_status,omitempty"`
	LaunchControlState       string                    `json:"launch_control_state,omitempty"`
	LaunchRetryDisposition   string                    `json:"launch_retry_disposition,omitempty"`
	LaunchControlReason      string                    `json:"launch_control_reason,omitempty"`
	IsResumable              bool                      `json:"is_resumable,omitempty"`
	RecoveryClass            string                    `json:"recovery_class,omitempty"`
	RecommendedAction        string                    `json:"recommended_action,omitempty"`
	ReadyForNextRun          bool                      `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch    bool                      `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason           string                    `json:"recovery_reason,omitempty"`
	LatestRecoveryAction     *TaskRecoveryActionRecord `json:"latest_recovery_action,omitempty"`
	LastEventType            string                    `json:"last_event_type,omitempty"`
	LastEventID              common.EventID            `json:"last_event_id,omitempty"`
	LastEventAtUnixMs        int64                     `json:"last_event_at_unix_ms,omitempty"`
}

type TaskInspectRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskRecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TaskRecoveryAssessment struct {
	TaskID                 common.TaskID             `json:"task_id"`
	ContinuityOutcome      string                    `json:"continuity_outcome"`
	RecoveryClass          string                    `json:"recovery_class"`
	RecommendedAction      string                    `json:"recommended_action"`
	ReadyForNextRun        bool                      `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                      `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                      `json:"requires_decision,omitempty"`
	RequiresRepair         bool                      `json:"requires_repair,omitempty"`
	RequiresReview         bool                      `json:"requires_review,omitempty"`
	RequiresReconciliation bool                      `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass     `json:"drift_class,omitempty"`
	Reason                 string                    `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID       `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID              `json:"run_id,omitempty"`
	HandoffID              string                    `json:"handoff_id,omitempty"`
	HandoffStatus          string                    `json:"handoff_status,omitempty"`
	LatestAction           *TaskRecoveryActionRecord `json:"latest_action,omitempty"`
	Issues                 []TaskRecoveryIssue       `json:"issues,omitempty"`
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
	TaskID                common.TaskID              `json:"task_id"`
	RepoAnchor            RepoAnchor                 `json:"repo_anchor"`
	Intent                *intent.State              `json:"intent,omitempty"`
	Brief                 *brief.ExecutionBrief      `json:"brief,omitempty"`
	Run                   *run.ExecutionRun          `json:"run,omitempty"`
	Checkpoint            *checkpoint.Checkpoint     `json:"checkpoint,omitempty"`
	Handoff               *handoff.Packet            `json:"handoff,omitempty"`
	Launch                *handoff.Launch            `json:"launch,omitempty"`
	Acknowledgment        *handoff.Acknowledgment    `json:"acknowledgment,omitempty"`
	LaunchControl         *TaskLaunchControl         `json:"launch_control,omitempty"`
	Recovery              *TaskRecoveryAssessment    `json:"recovery,omitempty"`
	LatestRecoveryAction  *TaskRecoveryActionRecord  `json:"latest_recovery_action,omitempty"`
	RecentRecoveryActions []TaskRecoveryActionRecord `json:"recent_recovery_actions,omitempty"`
}

type TaskRecordRecoveryActionRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Kind    string        `json:"kind"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskRecordRecoveryActionResponse struct {
	TaskID                common.TaskID            `json:"task_id"`
	Action                TaskRecoveryActionRecord `json:"action"`
	RecoveryClass         string                   `json:"recovery_class"`
	RecommendedAction     string                   `json:"recommended_action"`
	ReadyForNextRun       bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                     `json:"ready_for_handoff_launch"`
	RecoveryReason        string                   `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                   `json:"canonical_response"`
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

type TaskShellLaunch struct {
	AttemptID         string    `json:"attempt_id,omitempty"`
	LaunchID          string    `json:"launch_id,omitempty"`
	Status            string    `json:"status,omitempty"`
	RequestedAt       time.Time `json:"requested_at,omitempty"`
	StartedAt         time.Time `json:"started_at,omitempty"`
	EndedAt           time.Time `json:"ended_at,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	OutputArtifactRef string    `json:"output_artifact_ref,omitempty"`
}

type TaskShellAcknowledgment struct {
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskShellRecoveryIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TaskShellRecovery struct {
	ContinuityOutcome      string                   `json:"continuity_outcome"`
	RecoveryClass          string                   `json:"recovery_class"`
	RecommendedAction      string                   `json:"recommended_action"`
	ReadyForNextRun        bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch  bool                     `json:"ready_for_handoff_launch"`
	RequiresDecision       bool                     `json:"requires_decision,omitempty"`
	RequiresRepair         bool                     `json:"requires_repair,omitempty"`
	RequiresReview         bool                     `json:"requires_review,omitempty"`
	RequiresReconciliation bool                     `json:"requires_reconciliation,omitempty"`
	DriftClass             checkpoint.DriftClass    `json:"drift_class,omitempty"`
	Reason                 string                   `json:"reason,omitempty"`
	CheckpointID           common.CheckpointID      `json:"checkpoint_id,omitempty"`
	RunID                  common.RunID             `json:"run_id,omitempty"`
	HandoffID              string                   `json:"handoff_id,omitempty"`
	HandoffStatus          string                   `json:"handoff_status,omitempty"`
	Issues                 []TaskShellRecoveryIssue `json:"issues,omitempty"`
}

type TaskShellLaunchControl struct {
	State            string         `json:"state"`
	RetryDisposition string         `json:"retry_disposition"`
	Reason           string         `json:"reason,omitempty"`
	HandoffID        string         `json:"handoff_id,omitempty"`
	AttemptID        string         `json:"attempt_id,omitempty"`
	LaunchID         string         `json:"launch_id,omitempty"`
	TargetWorker     run.WorkerKind `json:"target_worker,omitempty"`
	RequestedAt      time.Time      `json:"requested_at,omitempty"`
	CompletedAt      time.Time      `json:"completed_at,omitempty"`
	FailedAt         time.Time      `json:"failed_at,omitempty"`
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
	Launch                  *TaskShellLaunch         `json:"launch,omitempty"`
	LaunchControl           *TaskShellLaunchControl  `json:"launch_control,omitempty"`
	Acknowledgment          *TaskShellAcknowledgment `json:"acknowledgment,omitempty"`
	Recovery                *TaskShellRecovery       `json:"recovery,omitempty"`
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

**internal/runtime/daemon/service.go**

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

	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/shellsession"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
)

func ipcRecoveryActionRecord(in *recoveryaction.Record) *ipc.TaskRecoveryActionRecord {
	if in == nil {
		return nil
	}
	createdAt := int64(0)
	if !in.CreatedAt.IsZero() {
		createdAt = in.CreatedAt.UnixMilli()
	}
	return &ipc.TaskRecoveryActionRecord{
		ActionID:        in.ActionID,
		TaskID:          in.TaskID,
		Kind:            string(in.Kind),
		RunID:           in.RunID,
		CheckpointID:    in.CheckpointID,
		HandoffID:       in.HandoffID,
		LaunchAttemptID: in.LaunchAttemptID,
		Summary:         in.Summary,
		Notes:           append([]string{}, in.Notes...),
		CreatedAtUnixMs: createdAt,
	}
}

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
		LatestAction:           ipcRecoveryActionRecord(in.LatestAction),
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

func ipcShellRecovery(in *orchestrator.ShellRecoverySummary) *ipc.TaskShellRecovery {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellRecovery{
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
		out.Issues = make([]ipc.TaskShellRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ipc.TaskShellRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func ipcShellLaunchControl(in *orchestrator.ShellLaunchControlSummary) *ipc.TaskShellLaunchControl {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLaunchControl{
		State:            string(in.State),
		RetryDisposition: string(in.RetryDisposition),
		Reason:           in.Reason,
		HandoffID:        in.HandoffID,
		AttemptID:        in.AttemptID,
		LaunchID:         in.LaunchID,
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
			LatestRecoveryAction:     ipcRecoveryActionRecord(out.LatestRecoveryAction),
			LastEventType:            string(out.LastEventType),
			LastEventID:              out.LastEventID,
			LastEventAtUnixMs:        lastEventAt,
		})
	case ipc.MethodRecordRecoveryAction:
		var p ipc.TaskRecordRecoveryActionRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordRecoveryAction(ctx, orchestrator.RecordRecoveryActionRequest{
			TaskID:  string(p.TaskID),
			Kind:    recoveryaction.Kind(p.Kind),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("RECOVERY_ACTION_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("RECOVERY_ACTION_FAILED", "missing recovery action payload")
		}
		return respondOK(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                out.TaskID,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
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
		resp := ipc.TaskInspectResponse{
			TaskID: out.TaskID,
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         out.RepoAnchor.RepoRoot,
				Branch:           out.RepoAnchor.Branch,
				HeadSHA:          out.RepoAnchor.HeadSHA,
				WorkingTreeDirty: out.RepoAnchor.WorkingTreeDirty,
				CapturedAt:       out.RepoAnchor.CapturedAt,
			},
			Intent:               out.Intent,
			Brief:                out.Brief,
			Run:                  out.Run,
			Checkpoint:           out.Checkpoint,
			Handoff:              out.Handoff,
			Launch:               out.Launch,
			Acknowledgment:       out.Acknowledgment,
			LaunchControl:        ipcLaunchControl(out.LaunchControl),
			Recovery:             ipcRecoveryAssessment(out.Recovery),
			LatestRecoveryAction: ipcRecoveryActionRecord(out.LatestRecoveryAction),
		}
		if len(out.RecentRecoveryActions) > 0 {
			resp.RecentRecoveryActions = make([]ipc.TaskRecoveryActionRecord, 0, len(out.RecentRecoveryActions))
			for i := range out.RecentRecoveryActions {
				if mapped := ipcRecoveryActionRecord(&out.RecentRecoveryActions[i]); mapped != nil {
					resp.RecentRecoveryActions = append(resp.RecentRecoveryActions, *mapped)
				}
			}
		}
		return respondOK(resp)
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
		if out.Launch != nil {
			resp.Launch = &ipc.TaskShellLaunch{
				AttemptID:         out.Launch.AttemptID,
				LaunchID:          out.Launch.LaunchID,
				Status:            string(out.Launch.Status),
				RequestedAt:       out.Launch.RequestedAt,
				StartedAt:         out.Launch.StartedAt,
				EndedAt:           out.Launch.EndedAt,
				Summary:           out.Launch.Summary,
				ErrorMessage:      out.Launch.ErrorMessage,
				OutputArtifactRef: out.Launch.OutputArtifactRef,
			}
		}
		resp.LaunchControl = ipcShellLaunchControl(out.LaunchControl)
		if out.Acknowledgment != nil {
			resp.Acknowledgment = &ipc.TaskShellAcknowledgment{
				Status:    string(out.Acknowledgment.Status),
				Summary:   out.Acknowledgment.Summary,
				CreatedAt: out.Acknowledgment.CreatedAt,
			}
		}
		resp.Recovery = ipcShellRecovery(out.Recovery)
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

**internal/runtime/daemon/service_test.go**

```go
package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
	"tuku/internal/domain/shellsession"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	"tuku/internal/storage/sqlite"
)

func TestHandleRequestCreateHandoffRoute(t *testing.T) {
	var captured orchestrator.CreateHandoffRequest
	handler := &fakeOrchestratorService{
		createHandoffFn: func(_ context.Context, req orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error) {
			captured = req
			return orchestrator.CreateHandoffResult{
				TaskID:            common.TaskID(req.TaskID),
				HandoffID:         "hnd_test",
				SourceWorker:      run.WorkerKindCodex,
				TargetWorker:      req.TargetWorker,
				Status:            handoff.StatusCreated,
				CheckpointID:      common.CheckpointID("chk_test"),
				BriefID:           common.BriefID("brf_test"),
				CanonicalResponse: "handoff created",
				Packet: &handoff.Packet{
					Version:      1,
					HandoffID:    "hnd_test",
					TaskID:       common.TaskID(req.TaskID),
					Status:       handoff.StatusCreated,
					TargetWorker: req.TargetWorker,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffCreateRequest{
		TaskID:       common.TaskID("tsk_123"),
		TargetWorker: run.WorkerKindClaude,
		Reason:       "manual test",
		Mode:         handoff.ModeResume,
		Notes:        []string{"note"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_1",
		Method:    ipc.MethodCreateHandoff,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" {
		t.Fatalf("expected captured task id tsk_123, got %s", captured.TaskID)
	}
	if captured.TargetWorker != run.WorkerKindClaude {
		t.Fatalf("expected target worker claude, got %s", captured.TargetWorker)
	}
	var out ipc.TaskHandoffCreateResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.HandoffID != "hnd_test" {
		t.Fatalf("expected handoff id hnd_test, got %s", out.HandoffID)
	}
	if out.Status != string(handoff.StatusCreated) {
		t.Fatalf("expected status CREATED, got %s", out.Status)
	}
}

func TestHandleRequestResolveShellTaskForRepoRoute(t *testing.T) {
	var capturedRepoRoot string
	var capturedGoal string
	handler := &fakeOrchestratorService{
		resolveShellTaskForRepoFn: func(_ context.Context, repoRoot string, defaultGoal string) (orchestrator.ResolveShellTaskResult, error) {
			capturedRepoRoot = repoRoot
			capturedGoal = defaultGoal
			return orchestrator.ResolveShellTaskResult{
				TaskID:   common.TaskID("tsk_repo"),
				RepoRoot: repoRoot,
				Created:  true,
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.ResolveShellTaskForRepoRequest{
		RepoRoot:    "/tmp/repo",
		DefaultGoal: "Continue work in this repository",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_repo",
		Method:    ipc.MethodResolveShellTaskForRepo,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if capturedRepoRoot != "/tmp/repo" || capturedGoal != "Continue work in this repository" {
		t.Fatalf("unexpected resolve-shell-task request: repo=%q goal=%q", capturedRepoRoot, capturedGoal)
	}
	var out ipc.ResolveShellTaskForRepoResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal resolve shell task response: %v", err)
	}
	if out.TaskID != "tsk_repo" || !out.Created {
		t.Fatalf("unexpected resolve shell task response: %+v", out)
	}
}

func TestHandleRequestAcceptHandoffRoute(t *testing.T) {
	var captured orchestrator.AcceptHandoffRequest
	handler := &fakeOrchestratorService{
		acceptHandoffFn: func(_ context.Context, req orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error) {
			captured = req
			return orchestrator.AcceptHandoffResult{
				TaskID:            common.TaskID(req.TaskID),
				HandoffID:         req.HandoffID,
				Status:            handoff.StatusAccepted,
				CanonicalResponse: "handoff accepted",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffAcceptRequest{
		TaskID:     common.TaskID("tsk_123"),
		HandoffID:  "hnd_abc",
		AcceptedBy: run.WorkerKindClaude,
		Notes:      []string{"accepted"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_2",
		Method:    ipc.MethodAcceptHandoff,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.HandoffID != "hnd_abc" {
		t.Fatalf("unexpected captured accept request: %+v", captured)
	}
	var out ipc.TaskHandoffAcceptResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Status != string(handoff.StatusAccepted) {
		t.Fatalf("expected status ACCEPTED, got %s", out.Status)
	}
}

func TestHandleRequestShellSnapshotRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		shellSnapshotFn: func(_ context.Context, taskID string) (orchestrator.ShellSnapshotResult, error) {
			if taskID != "tsk_123" {
				t.Fatalf("unexpected task id %s", taskID)
			}
			return orchestrator.ShellSnapshotResult{
				TaskID:        common.TaskID(taskID),
				Goal:          "Shell milestone",
				Phase:         "BRIEF_READY",
				Status:        "ACTIVE",
				IntentClass:   "implement",
				IntentSummary: "implement: wire shell",
				LaunchControl: &orchestrator.ShellLaunchControlSummary{
					State:            orchestrator.LaunchControlStateRequestedOutcomeUnknown,
					RetryDisposition: orchestrator.LaunchRetryDispositionBlocked,
					Reason:           "launch outcome is still unknown",
					HandoffID:        "hnd_1",
					AttemptID:        "hlc_1",
				},
				Recovery: &orchestrator.ShellRecoverySummary{
					ContinuityOutcome: orchestrator.ContinueOutcomeSafe,
					RecoveryClass:     orchestrator.RecoveryClassHandoffLaunchPendingOutcome,
					RecommendedAction: orchestrator.RecoveryActionWaitForLaunchOutcome,
					ReadyForNextRun:   false,
					Issues: []orchestrator.ShellRecoveryIssue{
						{Code: "LATEST_LAUNCH_INVALID", Message: "launch outcome still unknown"},
					},
				},
				RecentProofs: []orchestrator.ShellProofSummary{
					{EventID: "evt_1", Type: proof.EventBriefCreated, Summary: "Execution brief updated"},
				},
				RecentConversation: []orchestrator.ShellConversationSummary{
					{Role: conversation.RoleSystem, Body: "Canonical shell response"},
				},
				LatestCanonicalResponse: "Canonical shell response",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_3",
		Method:    ipc.MethodTaskShellSnapshot,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell snapshot response: %v", err)
	}
	if out.TaskID != "tsk_123" {
		t.Fatalf("expected task id tsk_123, got %s", out.TaskID)
	}
	if out.LatestCanonicalResponse != "Canonical shell response" {
		t.Fatalf("unexpected canonical response %q", out.LatestCanonicalResponse)
	}
	if len(out.RecentProofs) != 1 {
		t.Fatalf("expected one proof item, got %d", len(out.RecentProofs))
	}
	if out.LaunchControl == nil || out.LaunchControl.State != string(orchestrator.LaunchControlStateRequestedOutcomeUnknown) {
		t.Fatalf("expected launch control state mapping, got %+v", out.LaunchControl)
	}
	if out.Recovery == nil || out.Recovery.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchPendingOutcome) {
		t.Fatalf("expected recovery mapping, got %+v", out.Recovery)
	}
	if len(out.Recovery.Issues) != 1 {
		t.Fatalf("expected one recovery issue, got %+v", out.Recovery)
	}
}

func TestHandleRequestRecordRecoveryActionRoute(t *testing.T) {
	var captured orchestrator.RecordRecoveryActionRequest
	handler := &fakeOrchestratorService{
		recordRecoveryActionFn: func(_ context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
			captured = req
			return orchestrator.RecordRecoveryActionResult{
				TaskID: common.TaskID(req.TaskID),
				Action: recoveryaction.Record{
					Version:   1,
					ActionID:  "ract_1",
					TaskID:    common.TaskID(req.TaskID),
					Kind:      req.Kind,
					Summary:   req.Summary,
					Notes:     append([]string{}, req.Notes...),
					CreatedAt: time.Unix(1710000000, 0).UTC(),
				},
				RecoveryClass:         orchestrator.RecoveryClassDecisionRequired,
				RecommendedAction:     orchestrator.RecoveryActionMakeResumeDecision,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "failed run reviewed; choose next step",
				CanonicalResponse:     "recovery action recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionRequest{
		TaskID:  common.TaskID("tsk_123"),
		Kind:    string(recoveryaction.KindFailedRunReviewed),
		Summary: "reviewed failed run",
		Notes:   []string{"operator reviewed logs"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_recovery_1",
		Method:    ipc.MethodRecordRecoveryAction,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("unexpected recovery action request: %+v", captured)
	}
	var out ipc.TaskRecordRecoveryActionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal recovery action response: %v", err)
	}
	if out.Action.ActionID != "ract_1" || out.RecoveryClass != string(orchestrator.RecoveryClassDecisionRequired) {
		t.Fatalf("unexpected recovery action response: %+v", out)
	}
}

func TestHandleRequestStatusAndInspectRouteMapRecoveryActions(t *testing.T) {
	action := &recoveryaction.Record{
		Version:   1,
		ActionID:  "ract_status",
		TaskID:    common.TaskID("tsk_status"),
		Kind:      recoveryaction.KindRepairIntentRecorded,
		Summary:   "repair intent recorded",
		CreatedAt: time.Unix(1710000100, 0).UTC(),
	}
	handler := &fakeOrchestratorService{
		statusFn: func(_ context.Context, _ string) (orchestrator.StatusTaskResult, error) {
			return orchestrator.StatusTaskResult{
				TaskID:                  common.TaskID("tsk_status"),
				Phase:                   phase.PhaseBlocked,
				LatestCheckpointTrigger: checkpoint.TriggerManual,
				RecoveryClass:           orchestrator.RecoveryClassRepairRequired,
				RecommendedAction:       orchestrator.RecoveryActionRepairContinuity,
				LatestRecoveryAction:    action,
			}, nil
		},
		inspectFn: func(_ context.Context, _ string) (orchestrator.InspectTaskResult, error) {
			return orchestrator.InspectTaskResult{
				TaskID:                common.TaskID("tsk_status"),
				LatestRecoveryAction:  action,
				RecentRecoveryActions: []recoveryaction.Record{*action},
				Recovery: &orchestrator.RecoveryAssessment{
					TaskID:            common.TaskID("tsk_status"),
					RecoveryClass:     orchestrator.RecoveryClassRepairRequired,
					RecommendedAction: orchestrator.RecoveryActionRepairContinuity,
					LatestAction:      action,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	statusPayload, _ := json.Marshal(ipc.TaskStatusRequest{TaskID: common.TaskID("tsk_status")})
	statusResp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_status_recovery",
		Method:    ipc.MethodTaskStatus,
		Payload:   statusPayload,
	})
	if !statusResp.OK {
		t.Fatalf("expected OK status response, got %+v", statusResp.Error)
	}
	var statusOut ipc.TaskStatusResponse
	if err := json.Unmarshal(statusResp.Payload, &statusOut); err != nil {
		t.Fatalf("unmarshal status response: %v", err)
	}
	if statusOut.LatestRecoveryAction == nil || statusOut.LatestRecoveryAction.ActionID != action.ActionID {
		t.Fatalf("expected latest recovery action in status response, got %+v", statusOut.LatestRecoveryAction)
	}

	inspectPayload, _ := json.Marshal(ipc.TaskInspectRequest{TaskID: common.TaskID("tsk_status")})
	inspectResp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_inspect_recovery",
		Method:    ipc.MethodTaskInspect,
		Payload:   inspectPayload,
	})
	if !inspectResp.OK {
		t.Fatalf("expected OK inspect response, got %+v", inspectResp.Error)
	}
	var inspectOut ipc.TaskInspectResponse
	if err := json.Unmarshal(inspectResp.Payload, &inspectOut); err != nil {
		t.Fatalf("unmarshal inspect response: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.ActionID != action.ActionID {
		t.Fatalf("expected latest recovery action in inspect response, got %+v", inspectOut.LatestRecoveryAction)
	}
	if len(inspectOut.RecentRecoveryActions) != 1 || inspectOut.RecentRecoveryActions[0].ActionID != action.ActionID {
		t.Fatalf("expected recent recovery action in inspect response, got %+v", inspectOut.RecentRecoveryActions)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.LatestAction == nil || inspectOut.Recovery.LatestAction.ActionID != action.ActionID {
		t.Fatalf("expected recovery latest action mapping, got %+v", inspectOut.Recovery)
	}
}

func TestHandleRequestShellLifecycleRoute(t *testing.T) {
	var captured orchestrator.RecordShellLifecycleRequest
	handler := &fakeOrchestratorService{
		recordShellLifecycleFn: func(_ context.Context, req orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error) {
			captured = req
			return orchestrator.RecordShellLifecycleResult{TaskID: common.TaskID(req.TaskID)}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	exitCode := 9
	payload, _ := json.Marshal(ipc.TaskShellLifecycleRequest{
		TaskID:     common.TaskID("tsk_shell"),
		SessionID:  "shs_123",
		Kind:       "host_exited",
		HostMode:   "codex-pty",
		HostState:  "exited",
		Note:       "codex exited with code 9",
		InputLive:  false,
		ExitCode:   &exitCode,
		PaneWidth:  80,
		PaneHeight: 24,
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_4",
		Method:    ipc.MethodTaskShellLifecycle,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.SessionID != "shs_123" || captured.Kind != orchestrator.ShellLifecycleHostExited {
		t.Fatalf("unexpected shell lifecycle request: %+v", captured)
	}
}

func TestHandleRequestShellSessionReportRoute(t *testing.T) {
	var captured orchestrator.ReportShellSessionRequest
	handler := &fakeOrchestratorService{
		reportShellSessionFn: func(_ context.Context, req orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error) {
			captured = req
			return orchestrator.ReportShellSessionResult{
				TaskID: common.TaskID(req.TaskID),
				Session: orchestrator.ShellSessionView{
					TaskID:           common.TaskID(req.TaskID),
					SessionID:        req.SessionID,
					WorkerPreference: req.WorkerPreference,
					ResolvedWorker:   req.ResolvedWorker,
					WorkerSessionID:  req.WorkerSessionID,
					AttachCapability: req.AttachCapability,
					HostMode:         req.HostMode,
					HostState:        req.HostState,
					StartedAt:        req.StartedAt,
					Active:           req.Active,
					Note:             req.Note,
					SessionClass:     orchestrator.ShellSessionClassAttachable,
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionReportRequest{
		TaskID:           common.TaskID("tsk_shell"),
		SessionID:        "shs_456",
		WorkerPreference: "auto",
		ResolvedWorker:   "claude",
		WorkerSessionID:  "wks_456",
		AttachCapability: "attachable",
		HostMode:         "claude-pty",
		HostState:        "starting",
		Active:           true,
		Note:             "shell session registered",
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_5",
		Method:    ipc.MethodTaskShellSessionReport,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.SessionID != "shs_456" || captured.ResolvedWorker != "claude" {
		t.Fatalf("unexpected shell session report request: %+v", captured)
	}
	var out ipc.TaskShellSessionReportResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell session report response: %v", err)
	}
	if out.Session.SessionClass != "attachable" || out.Session.WorkerSessionID != "wks_456" || out.Session.AttachCapability != "attachable" {
		t.Fatalf("expected active session class, got %+v", out.Session)
	}
}

func TestHandleRequestShellSessionsRoute(t *testing.T) {
	handler := &fakeOrchestratorService{
		listShellSessionsFn: func(_ context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
			return orchestrator.ListShellSessionsResult{
				TaskID: common.TaskID(taskID),
				Sessions: []orchestrator.ShellSessionView{
					{
						TaskID:           common.TaskID(taskID),
						SessionID:        "shs_1",
						WorkerPreference: "auto",
						ResolvedWorker:   "codex",
						WorkerSessionID:  "wks_1",
						AttachCapability: shellsession.AttachCapabilityNone,
						HostMode:         "codex-pty",
						HostState:        "live",
						Active:           true,
						SessionClass:     orchestrator.ShellSessionClassStale,
					},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_6",
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal shell sessions response: %v", err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].ResolvedWorker != "codex" {
		t.Fatalf("unexpected shell sessions payload: %+v", out)
	}
	if out.Sessions[0].SessionClass != "stale" || out.Sessions[0].WorkerSessionID != "wks_1" || out.Sessions[0].AttachCapability != "none" {
		t.Fatalf("expected stale session class, got %+v", out.Sessions[0])
	}
}

func TestHandleRequestShellSessionsRouteReadsDurableRecordsAfterCoordinatorRecreation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tuku-shell-route.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	coord, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:          store,
		IntentCompiler: orchestrator.NewIntentStubCompiler(),
		BriefBuilder:   orchestrator.NewBriefBuilderV1(nil, nil),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  store.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	repoRoot := t.TempDir()
	start, err := coord.StartTask(context.Background(), "Shell route durability", repoRoot)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "prepare shell session route"); err != nil {
		t.Fatalf("message task: %v", err)
	}
	if _, err := coord.ReportShellSession(context.Background(), orchestrator.ReportShellSessionRequest{
		TaskID:           string(start.TaskID),
		SessionID:        "shs_durable",
		WorkerPreference: "auto",
		ResolvedWorker:   "codex",
		WorkerSessionID:  "wks_durable",
		AttachCapability: shellsession.AttachCapabilityAttachable,
		HostMode:         "codex-pty",
		HostState:        "live",
		StartedAt:        time.Unix(1710000000, 0).UTC(),
		Active:           true,
	}); err != nil {
		t.Fatalf("report shell session: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	reopened, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	coord2, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:          reopened,
		IntentCompiler: orchestrator.NewIntentStubCompiler(),
		BriefBuilder:   orchestrator.NewBriefBuilderV1(nil, nil),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  reopened.ShellSessions(),
	})
	if err != nil {
		t.Fatalf("new reopened coordinator: %v", err)
	}

	svc := NewService("/tmp/unused.sock", coord2)
	payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: start.TaskID})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_durable",
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal durable shell sessions response: %v", err)
	}
	if len(out.Sessions) != 1 {
		t.Fatalf("expected one durable shell session in route response, got %d", len(out.Sessions))
	}
	if out.Sessions[0].SessionID != "shs_durable" || out.Sessions[0].WorkerSessionID != "wks_durable" || out.Sessions[0].AttachCapability != "attachable" {
		t.Fatalf("unexpected durable shell session payload: %+v", out.Sessions[0])
	}
}

type fakeOrchestratorService struct {
	resolveShellTaskForRepoFn func(context.Context, string, string) (orchestrator.ResolveShellTaskResult, error)
	createHandoffFn           func(context.Context, orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error)
	acceptHandoffFn           func(context.Context, orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error)
	recordRecoveryActionFn    func(context.Context, orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error)
	statusFn                  func(context.Context, string) (orchestrator.StatusTaskResult, error)
	inspectFn                 func(context.Context, string) (orchestrator.InspectTaskResult, error)
	shellSnapshotFn           func(context.Context, string) (orchestrator.ShellSnapshotResult, error)
	recordShellLifecycleFn    func(context.Context, orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error)
	reportShellSessionFn      func(context.Context, orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error)
	listShellSessionsFn       func(context.Context, string) (orchestrator.ListShellSessionsResult, error)
}

func (f *fakeOrchestratorService) ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (orchestrator.ResolveShellTaskResult, error) {
	if f.resolveShellTaskForRepoFn != nil {
		return f.resolveShellTaskForRepoFn(ctx, repoRoot, defaultGoal)
	}
	return orchestrator.ResolveShellTaskResult{}, nil
}

func (f *fakeOrchestratorService) StartTask(_ context.Context, _ string, _ string) (orchestrator.StartTaskResult, error) {
	return orchestrator.StartTaskResult{}, nil
}

func (f *fakeOrchestratorService) MessageTask(_ context.Context, _, _ string) (orchestrator.MessageTaskResult, error) {
	return orchestrator.MessageTaskResult{}, nil
}

func (f *fakeOrchestratorService) RunTask(_ context.Context, _ orchestrator.RunTaskRequest) (orchestrator.RunTaskResult, error) {
	return orchestrator.RunTaskResult{}, nil
}

func (f *fakeOrchestratorService) ContinueTask(_ context.Context, _ string) (orchestrator.ContinueTaskResult, error) {
	return orchestrator.ContinueTaskResult{}, nil
}

func (f *fakeOrchestratorService) RecordRecoveryAction(ctx context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
	if f.recordRecoveryActionFn != nil {
		return f.recordRecoveryActionFn(ctx, req)
	}
	return orchestrator.RecordRecoveryActionResult{}, nil
}

func (f *fakeOrchestratorService) CreateCheckpoint(_ context.Context, _ string) (orchestrator.CreateCheckpointResult, error) {
	return orchestrator.CreateCheckpointResult{}, nil
}

func (f *fakeOrchestratorService) CreateHandoff(ctx context.Context, req orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error) {
	if f.createHandoffFn != nil {
		return f.createHandoffFn(ctx, req)
	}
	return orchestrator.CreateHandoffResult{}, nil
}

func (f *fakeOrchestratorService) AcceptHandoff(ctx context.Context, req orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error) {
	if f.acceptHandoffFn != nil {
		return f.acceptHandoffFn(ctx, req)
	}
	return orchestrator.AcceptHandoffResult{}, nil
}

func (f *fakeOrchestratorService) LaunchHandoff(_ context.Context, _ orchestrator.LaunchHandoffRequest) (orchestrator.LaunchHandoffResult, error) {
	return orchestrator.LaunchHandoffResult{}, nil
}

func (f *fakeOrchestratorService) StatusTask(ctx context.Context, taskID string) (orchestrator.StatusTaskResult, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, taskID)
	}
	return orchestrator.StatusTaskResult{
		Phase:                   phase.PhaseIntake,
		LatestCheckpointTrigger: checkpoint.TriggerManual,
	}, nil
}

func (f *fakeOrchestratorService) InspectTask(ctx context.Context, taskID string) (orchestrator.InspectTaskResult, error) {
	if f.inspectFn != nil {
		return f.inspectFn(ctx, taskID)
	}
	return orchestrator.InspectTaskResult{}, nil
}

func (f *fakeOrchestratorService) ShellSnapshotTask(ctx context.Context, taskID string) (orchestrator.ShellSnapshotResult, error) {
	if f.shellSnapshotFn != nil {
		return f.shellSnapshotFn(ctx, taskID)
	}
	return orchestrator.ShellSnapshotResult{}, nil
}

func (f *fakeOrchestratorService) RecordShellLifecycle(ctx context.Context, req orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error) {
	if f.recordShellLifecycleFn != nil {
		return f.recordShellLifecycleFn(ctx, req)
	}
	return orchestrator.RecordShellLifecycleResult{}, nil
}

func (f *fakeOrchestratorService) ReportShellSession(ctx context.Context, req orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error) {
	if f.reportShellSessionFn != nil {
		return f.reportShellSessionFn(ctx, req)
	}
	return orchestrator.ReportShellSessionResult{}, nil
}

func (f *fakeOrchestratorService) ListShellSessions(ctx context.Context, taskID string) (orchestrator.ListShellSessionsResult, error) {
	if f.listShellSessionsFn != nil {
		return f.listShellSessionsFn(ctx, taskID)
	}
	return orchestrator.ListShellSessionsResult{}, nil
}

```

**internal/tui/shell/viewmodel.go**

```go
package shell

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func BuildViewModel(snapshot Snapshot, ui UIState, host WorkerHost, width int, height int) ViewModel {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}
	if host == nil {
		host = NewTranscriptHost()
		host.UpdateSnapshot(snapshot)
	}

	header := HeaderView{
		Title:      "Tuku",
		TaskLabel:  displayTaskLabel(snapshot.TaskID),
		Phase:      nonEmpty(snapshot.Phase, "UNKNOWN"),
		Worker:     effectiveWorkerLabel(snapshot, host),
		Repo:       repoLabel(snapshot.Repo),
		Continuity: continuityLabel(snapshot),
	}

	layout := computeShellLayout(width, height, ui)
	bodyHeight := layout.bodyHeight
	workerWidth := layout.workerWidth
	inspectorWidth := layout.inspectorWidth
	if !layout.showInspector && ui.Focus == FocusInspector {
		ui.Focus = FocusWorker
	}
	if !layout.showProof && ui.Focus == FocusActivity {
		ui.Focus = FocusWorker
	}

	workerPane := buildWorkerPane(snapshot, ui, host, bodyHeight-1, workerWidth)

	var inspector *InspectorView
	if layout.showInspector && inspectorWidth > 0 {
		inspector = &InspectorView{
			Title:   "inspector",
			Focused: ui.Focus == FocusInspector,
			Sections: []SectionView{
				{Title: "operator", Lines: inspectorOperator(snapshot)},
				{Title: "worker session", Lines: inspectorWorkerSession(host, ui.Session)},
				{Title: "brief", Lines: inspectorBrief(snapshot)},
				{Title: "intent", Lines: inspectorIntent(snapshot)},
				{Title: "pending message", Lines: inspectorPendingMessage(snapshot, ui)},
				{Title: "checkpoint", Lines: inspectorCheckpoint(snapshot)},
				{Title: "handoff", Lines: inspectorHandoff(snapshot)},
				{Title: "launch", Lines: inspectorLaunch(snapshot)},
				{Title: "run", Lines: inspectorRun(snapshot)},
				{Title: "proof", Lines: inspectorProof(snapshot)},
			},
		}
	}

	var strip *StripView
	if layout.showProof {
		strip = &StripView{
			Title:   "activity",
			Focused: ui.Focus == FocusActivity,
			Lines:   buildActivityLines(snapshot, host, ui.Session),
		}
	}

	vm := ViewModel{
		Header:     header,
		WorkerPane: workerPane,
		Inspector:  inspector,
		ProofStrip: strip,
		Footer:     footerText(snapshot, ui, host),
		Layout:     layout,
	}

	if ui.ShowHelp {
		vm.Overlay = &OverlayView{
			Title: "help",
			Lines: []string{
				"q quit shell",
				"i toggle inspector",
				"p toggle activity strip",
				"r refresh shell state",
				"s toggle compact status card",
				"h toggle help",
				"tab cycle focus",
				"a stage a local draft from surfaced scratch",
				"e edit the staged local draft",
				"m send the current draft through Tuku",
				"x clear the local draft",
				"while editing: type in the worker pane",
				"ctrl-g s save and leave edit mode",
				"ctrl-g c cancel edits and restore the staged draft",
				"ctrl-g next-key when the live worker pane is focused",
				"",
				"Scratch stays local-only. The staged draft stays shell-local until you explicitly send it with m.",
			},
		}
	} else if ui.ShowStatus {
		vm.Overlay = &OverlayView{
			Title: "status",
			Lines: []string{
				fmt.Sprintf("task %s", displayTaskLabel(snapshot.TaskID)),
				fmt.Sprintf("new shell session %s", ui.Session.SessionID),
				fmt.Sprintf("phase %s", nonEmpty(snapshot.Phase, "UNKNOWN")),
				fmt.Sprintf("worker %s", effectiveWorkerLabel(snapshot, host)),
				fmt.Sprintf("host %s", hostStatusLine(snapshot, ui, host)),
				fmt.Sprintf("repo %s", repoLabel(snapshot.Repo)),
				fmt.Sprintf("continuity %s", continuityLabel(snapshot)),
				fmt.Sprintf("recovery %s", operatorStateLabel(snapshot)),
				fmt.Sprintf("next %s", operatorActionLabel(snapshot)),
				fmt.Sprintf("readiness %s", operatorReadinessLine(snapshot)),
				fmt.Sprintf("launch %s", launchControlLine(snapshot)),
				fmt.Sprintf("reason %s", strongestOperatorReason(snapshot)),
				fmt.Sprintf("registry %s", sessionRegistrySummary(ui.Session)),
				fmt.Sprintf("draft %s", pendingMessageSummary(snapshot, ui)),
				fmt.Sprintf("checkpoint %s", checkpointLine(snapshot)),
				fmt.Sprintf("handoff %s", handoffLine(snapshot)),
				sessionPriorLine(ui.Session),
				"",
				latestCanonicalLine(snapshot),
			},
		}
	}

	return vm
}

func buildWorkerPane(snapshot Snapshot, ui UIState, host WorkerHost, height int, width int) PaneView {
	if ui.PendingTaskMessageEditMode {
		return PaneView{
			Title:   "worker pane | pending message editor",
			Lines:   pendingTaskMessageEditorLines(ui, height, width),
			Focused: ui.Focus == FocusWorker,
		}
	}
	hostHeight := height
	lines := []string(nil)
	if summary := workerPaneSummaryLine(snapshot, ui, host); summary != "" && height >= 5 {
		hostHeight = max(1, height-1)
		lines = append(lines, summary)
	}
	lines = append(lines, host.Lines(hostHeight, width)...)
	return PaneView{
		Title:   host.Title(),
		Lines:   lines,
		Focused: ui.Focus == FocusWorker,
	}
}

func shortTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if len(taskID) <= 10 {
		return taskID
	}
	return taskID[:10]
}

func displayTaskLabel(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "no-task"
	}
	return shortTaskID(taskID)
}

func workerLabel(snapshot Snapshot) string {
	return snapshotWorkerLabel(snapshot)
}

func effectiveWorkerLabel(snapshot Snapshot, host WorkerHost) string {
	if isScratchIntakeSnapshot(snapshot) {
		return snapshotWorkerLabel(snapshot)
	}
	if host != nil {
		if label := strings.TrimSpace(host.WorkerLabel()); label != "" {
			return label
		}
		status := host.Status()
		if label := strings.TrimSpace(status.Label); label != "" {
			return label
		}
	}
	return snapshotWorkerLabel(snapshot)
}

func snapshotWorkerLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "scratch intake"
	}
	if snapshot.Run != nil {
		if snapshot.Run.Status == "RUNNING" {
			return fmt.Sprintf("%s active", nonEmpty(snapshot.Run.WorkerKind, "worker"))
		}
		return fmt.Sprintf("%s last", nonEmpty(snapshot.Run.WorkerKind, "worker"))
	}
	if snapshot.Handoff != nil && snapshot.Handoff.TargetWorker != "" {
		return fmt.Sprintf("%s handoff", snapshot.Handoff.TargetWorker)
	}
	return "none"
}

func repoLabel(anchor RepoAnchor) string {
	if strings.TrimSpace(anchor.RepoRoot) == "" {
		return "no-repo"
	}
	name := filepath.Base(anchor.RepoRoot)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = anchor.RepoRoot
	}
	branch := nonEmpty(anchor.Branch, "detached")
	dirty := ""
	if anchor.WorkingTreeDirty {
		dirty = " dirty"
	}
	return fmt.Sprintf("%s@%s%s", name, branch, dirty)
}

func continuityLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Recovery != nil {
		switch snapshot.Recovery.Class {
		case "READY_NEXT_RUN":
			if snapshot.Recovery.ReadyForNextRun {
				return "ready"
			}
		case "INTERRUPTED_RUN_RECOVERABLE":
			return "recoverable"
		case "ACCEPTED_HANDOFF_LAUNCH_READY":
			if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "FAILED" && snapshot.LaunchControl.RetryDisposition == "ALLOWED" {
				return "launch-retry"
			}
			return "handoff-ready"
		case "HANDOFF_LAUNCH_PENDING_OUTCOME":
			return "launch-pending"
		case "HANDOFF_LAUNCH_COMPLETED":
			return "launched"
		case "FAILED_RUN_REVIEW_REQUIRED", "VALIDATION_REVIEW_REQUIRED":
			return "review"
		case "DECISION_REQUIRED", "BLOCKED_DRIFT":
			return "decision"
		case "REBRIEF_REQUIRED":
			return "rebrief"
		case "REPAIR_REQUIRED":
			return "repair"
		case "COMPLETED_NO_ACTION":
			return "complete"
		}
	}
	if snapshot.Checkpoint != nil && snapshot.Checkpoint.IsResumable {
		return "resumable"
	}
	switch snapshot.Phase {
	case "BLOCKED", "FAILED":
		return "blocked"
	case "VALIDATING":
		return "validating"
	default:
		return strings.ToLower(nonEmpty(snapshot.Status, "active"))
	}
}

func inspectorBrief(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"No repo-backed brief exists in scratch intake mode.",
			"Use this session to frame the project, scope milestones, and prepare for repository setup.",
		}
	}
	if snapshot.Brief == nil {
		return []string{"No brief persisted yet."}
	}
	lines := []string{
		truncateWithEllipsis(snapshot.Brief.Objective, 48),
		fmt.Sprintf("action %s", nonEmpty(snapshot.Brief.NormalizedAction, "n/a")),
	}
	if len(snapshot.Brief.Constraints) > 0 {
		lines = append(lines, fmt.Sprintf("constraints %s", strings.Join(snapshot.Brief.Constraints, ", ")))
	}
	if len(snapshot.Brief.DoneCriteria) > 0 {
		lines = append(lines, fmt.Sprintf("done %s", strings.Join(snapshot.Brief.DoneCriteria, ", ")))
	}
	return lines
}

func inspectorIntent(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"Local scratch intake session.",
			"Plan the work here before cloning or initializing a repository.",
		}
	}
	if snapshot.IntentSummary == "" {
		return []string{"No intent summary."}
	}
	return []string{snapshot.IntentSummary}
}

func inspectorCheckpoint(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No checkpoint exists because this session is not repo-backed."}
	}
	if snapshot.Checkpoint == nil {
		return []string{"No checkpoint yet."}
	}
	lines := []string{
		fmt.Sprintf("%s | %s", shortTaskID(snapshot.Checkpoint.ID), strings.ToLower(snapshot.Checkpoint.Trigger)),
	}
	lines = append(lines, fmt.Sprintf("raw resumable %s", yesNo(snapshot.Checkpoint.IsResumable)))
	if snapshot.Checkpoint.ResumeDescriptor != "" {
		lines = append(lines, snapshot.Checkpoint.ResumeDescriptor)
	}
	return lines
}

func inspectorHandoff(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No handoff packet exists in local scratch intake mode."}
	}
	if snapshot.Handoff == nil {
		return []string{"No handoff packet."}
	}
	lines := []string{
		fmt.Sprintf("%s -> %s (%s)", nonEmpty(snapshot.Handoff.SourceWorker, "unknown"), nonEmpty(snapshot.Handoff.TargetWorker, "unknown"), nonEmpty(snapshot.Handoff.Status, "unknown")),
	}
	if snapshot.Handoff.Mode != "" {
		lines = append(lines, fmt.Sprintf("mode %s", snapshot.Handoff.Mode))
	}
	if snapshot.Handoff.Reason != "" {
		lines = append(lines, snapshot.Handoff.Reason)
	}
	if snapshot.Acknowledgment != nil {
		lines = append(lines, fmt.Sprintf("ack %s", strings.ToLower(snapshot.Acknowledgment.Status)))
		lines = append(lines, truncateWithEllipsis(snapshot.Acknowledgment.Summary, 48))
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State != "NOT_APPLICABLE" {
		lines = append(lines, "launch "+launchControlLine(snapshot))
	}
	return lines
}

func inspectorLaunch(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No launch state exists in local scratch intake mode."}
	}
	if snapshot.Launch == nil && (snapshot.LaunchControl == nil || snapshot.LaunchControl.State == "NOT_APPLICABLE") {
		return []string{"No launch state."}
	}
	lines := []string{launchControlLine(snapshot)}
	if snapshot.Launch != nil {
		lines = append(lines, fmt.Sprintf("attempt %s | %s", shortTaskID(snapshot.Launch.AttemptID), strings.ToLower(nonEmpty(snapshot.Launch.Status, "unknown"))))
		if snapshot.Launch.LaunchID != "" {
			lines = append(lines, "launch id "+snapshot.Launch.LaunchID)
		}
		if snapshot.Launch.Summary != "" {
			lines = append(lines, truncateWithEllipsis(snapshot.Launch.Summary, 48))
		}
		if snapshot.Launch.ErrorMessage != "" {
			lines = append(lines, truncateWithEllipsis("error "+snapshot.Launch.ErrorMessage, 48))
		}
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "COMPLETED" {
		lines = append(lines, "launcher invocation completed; downstream work not proven")
	}
	return lines
}

func inspectorRun(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No execution run exists because this session has no task-backed orchestration state."}
	}
	if snapshot.Run == nil {
		return []string{"No run recorded."}
	}
	lines := []string{
		fmt.Sprintf("%s | %s", nonEmpty(snapshot.Run.WorkerKind, "worker"), snapshot.Run.Status),
	}
	if snapshot.Run.LastKnownSummary != "" {
		lines = append(lines, truncateWithEllipsis(snapshot.Run.LastKnownSummary, 48))
	}
	if snapshot.Run.InterruptionReason != "" {
		lines = append(lines, fmt.Sprintf("interrupt %s", snapshot.Run.InterruptionReason))
	}
	return lines
}

func inspectorWorkerSession(host WorkerHost, session SessionState) []string {
	if host == nil {
		return []string{"No worker host."}
	}
	status := host.Status()
	lines := []string{
		fmt.Sprintf("new shell session %s", session.SessionID),
		sessionRegistrySummary(session),
		fmt.Sprintf("preferred %s", nonEmpty(string(session.WorkerPreference), "auto")),
		fmt.Sprintf("resolved %s", nonEmpty(string(session.ResolvedWorker), "unknown")),
		fmt.Sprintf("worker session %s", nonEmpty(session.WorkerSessionID, "none")),
		fmt.Sprintf("attach %s", nonEmpty(string(session.AttachCapability), "none")),
		fmt.Sprintf("mode %s", nonEmpty(string(status.Mode), "unknown")),
		fmt.Sprintf("state %s", nonEmpty(string(status.State), "unknown")),
	}
	if !session.StartedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("started %s", session.StartedAt.Format("15:04:05")))
	}
	if status.InputLive {
		lines = append(lines, "input live")
	} else {
		lines = append(lines, "input disabled")
	}
	if status.Width > 0 && status.Height > 0 {
		lines = append(lines, fmt.Sprintf("pane %dx%d", status.Width, status.Height))
	}
	if status.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("exit code %d", *status.ExitCode))
	}
	if note := strings.TrimSpace(status.Note); note != "" {
		lines = append(lines, truncateWithEllipsis(note, 64))
	}
	if session.PriorPersistedSummary != "" {
		lines = append(lines, truncateWithEllipsis("previous persisted shell outcome "+session.PriorPersistedSummary, 64))
	}
	for _, evt := range recentSessionEvents(session, 2) {
		lines = append(lines, fmt.Sprintf("%s %s", evt.CreatedAt.Format("15:04"), truncateWithEllipsis(evt.Summary, 48)))
	}
	return lines
}

func inspectorProof(snapshot Snapshot) []string {
	if len(snapshot.RecentProofs) == 0 {
		return []string{"No proof events yet."}
	}
	lines := make([]string, 0, min(4, len(snapshot.RecentProofs)))
	limit := min(4, len(snapshot.RecentProofs))
	for _, evt := range snapshot.RecentProofs[:limit] {
		lines = append(lines, fmt.Sprintf("%s %s", evt.Timestamp.Format("15:04"), evt.Summary))
	}
	return lines
}

func inspectorOperator(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"Local-only scratch intake session.",
			"No task-backed recovery or launch-control state exists here.",
		}
	}
	lines := []string{
		"state " + operatorStateLabel(snapshot),
		"next " + operatorActionLabel(snapshot),
		"readiness " + operatorReadinessLine(snapshot),
	}
	if launch := launchControlLine(snapshot); launch != "n/a" {
		lines = append(lines, "launch "+launch)
	}
	if reason := strongestOperatorReason(snapshot); reason != "none" {
		lines = append(lines, "reason "+truncateWithEllipsis(reason, 64))
	}
	return lines
}

func inspectorPendingMessage(snapshot Snapshot, ui UIState) []string {
	if ui.PendingTaskMessageEditMode {
		lines := []string{
			"Editing the staged local draft.",
			pendingMessageSummary(snapshot, ui),
			"Typing changes only the shell-local draft. Nothing here is canonical until you explicitly send it with m.",
		}
		for _, line := range wrapText(truncateWithEllipsis(currentPendingTaskMessage(ui), 160), 48) {
			lines = append(lines, line)
		}
		lines = append(lines, "save with ctrl-g then s", "cancel with ctrl-g then c", "send with ctrl-g then m")
		return lines
	}
	if strings.TrimSpace(ui.PendingTaskMessage) != "" {
		lines := []string{
			"Local draft is staged and ready for review.",
			pendingMessageSummary(snapshot, ui),
			"Editing and clearing stay shell-local. Sending with m is the explicit step that makes this canonical.",
		}
		for _, line := range wrapText(truncateWithEllipsis(ui.PendingTaskMessage, 160), 48) {
			lines = append(lines, line)
		}
		lines = append(lines, "edit with e", "send with m", "clear with x")
		return lines
	}
	if snapshot.HasLocalScratchAdoption() {
		return []string{
			"Local scratch is available for explicit adoption.",
			"Stage a shell-local draft with a.",
			"Nothing becomes canonical until you explicitly send that draft with m.",
		}
	}
	return []string{"No pending task message."}
}

func buildActivityLines(snapshot Snapshot, host WorkerHost, session SessionState) []string {
	lines := []string{latestCanonicalLine(snapshot)}
	if host != nil {
		for _, line := range host.ActivityLines(3) {
			lines = append(lines, line)
		}
	}
	for _, evt := range recentSessionEvents(session, 3) {
		lines = append(lines, fmt.Sprintf("%s  %s", evt.CreatedAt.Format("15:04:05"), evt.Summary))
	}
	if len(snapshot.RecentProofs) > 0 {
		lines = append(lines, "")
		limit := min(3, len(snapshot.RecentProofs))
		for _, evt := range snapshot.RecentProofs[:limit] {
			lines = append(lines, fmt.Sprintf("%s  %s", evt.Timestamp.Format("15:04:05"), evt.Summary))
		}
	}
	return lines
}

func checkpointLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Checkpoint == nil {
		return "none"
	}
	label := shortTaskID(snapshot.Checkpoint.ID)
	if snapshot.Checkpoint.IsResumable {
		return label + " raw-resumable"
	}
	return label + " raw-not-resumable"
}

func handoffLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Handoff == nil {
		return "none"
	}
	return fmt.Sprintf("%s %s->%s", snapshot.Handoff.Status, nonEmpty(snapshot.Handoff.SourceWorker, "unknown"), nonEmpty(snapshot.Handoff.TargetWorker, "unknown"))
}

func latestCanonicalLine(snapshot Snapshot) string {
	if strings.TrimSpace(snapshot.LatestCanonicalResponse) == "" {
		return "No canonical Tuku response persisted yet."
	}
	return truncateWithEllipsis(snapshot.LatestCanonicalResponse, 160)
}

func operatorStateLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Recovery == nil || strings.TrimSpace(snapshot.Recovery.Class) == "" {
		return continuityLabel(snapshot)
	}
	switch snapshot.Recovery.Class {
	case "READY_NEXT_RUN":
		return "ready next run"
	case "INTERRUPTED_RUN_RECOVERABLE":
		return "interrupted recoverable"
	case "ACCEPTED_HANDOFF_LAUNCH_READY":
		if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "FAILED" && snapshot.LaunchControl.RetryDisposition == "ALLOWED" {
			return "launch retry ready"
		}
		return "accepted handoff launch ready"
	case "HANDOFF_LAUNCH_PENDING_OUTCOME":
		return "launch pending"
	case "HANDOFF_LAUNCH_COMPLETED":
		return "launch completed"
	case "FAILED_RUN_REVIEW_REQUIRED":
		return "failed run review required"
	case "VALIDATION_REVIEW_REQUIRED":
		return "validation review required"
	case "STALE_RUN_RECONCILIATION_REQUIRED":
		return "stale run reconciliation required"
	case "DECISION_REQUIRED":
		return "decision required"
	case "BLOCKED_DRIFT":
		return "drift blocked"
	case "REBRIEF_REQUIRED":
		return "rebrief required"
	case "REPAIR_REQUIRED":
		return "repair required"
	case "COMPLETED_NO_ACTION":
		return "completed"
	default:
		return humanizeConstant(snapshot.Recovery.Class)
	}
}

func operatorActionLabel(snapshot Snapshot) string {
	if snapshot.Recovery == nil || strings.TrimSpace(snapshot.Recovery.Action) == "" {
		return "none"
	}
	switch snapshot.Recovery.Action {
	case "START_NEXT_RUN":
		return "start next run"
	case "RESUME_INTERRUPTED_RUN":
		return "resume interrupted run"
	case "LAUNCH_ACCEPTED_HANDOFF":
		return "launch accepted handoff"
	case "WAIT_FOR_LAUNCH_OUTCOME":
		return "wait for launch outcome"
	case "MONITOR_LAUNCHED_HANDOFF":
		return "monitor launched handoff"
	case "INSPECT_FAILED_RUN":
		return "inspect failed run"
	case "REVIEW_VALIDATION_STATE":
		return "review validation state"
	case "RECONCILE_STALE_RUN":
		return "reconcile stale run"
	case "MAKE_RESUME_DECISION":
		return "make resume decision"
	case "REPAIR_CONTINUITY":
		return "repair continuity"
	case "REGENERATE_BRIEF":
		return "regenerate brief"
	case "NONE":
		return "none"
	default:
		return humanizeConstant(snapshot.Recovery.Action)
	}
}

func operatorReadinessLine(snapshot Snapshot) string {
	nextRun := false
	handoffLaunch := false
	if snapshot.Recovery != nil {
		nextRun = snapshot.Recovery.ReadyForNextRun
		handoffLaunch = snapshot.Recovery.ReadyForHandoffLaunch
	}
	return fmt.Sprintf("next-run %s | handoff-launch %s", yesNo(nextRun), yesNo(handoffLaunch))
}

func strongestOperatorReason(snapshot Snapshot) string {
	if snapshot.Recovery != nil {
		if reason := strings.TrimSpace(snapshot.Recovery.Reason); reason != "" {
			return reason
		}
		if len(snapshot.Recovery.Issues) > 0 {
			if msg := strings.TrimSpace(snapshot.Recovery.Issues[0].Message); msg != "" {
				return msg
			}
		}
	}
	if snapshot.LaunchControl != nil {
		if reason := strings.TrimSpace(snapshot.LaunchControl.Reason); reason != "" {
			return reason
		}
	}
	return "none"
}

func launchControlLine(snapshot Snapshot) string {
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State == "" || snapshot.LaunchControl.State == "NOT_APPLICABLE" {
		return "n/a"
	}
	state := ""
	switch snapshot.LaunchControl.State {
	case "NOT_REQUESTED":
		state = "not requested"
	case "REQUESTED_OUTCOME_UNKNOWN":
		state = "pending outcome unknown"
	case "COMPLETED":
		state = "completed (invocation only)"
	case "FAILED":
		state = "failed"
	default:
		state = humanizeConstant(snapshot.LaunchControl.State)
	}
	retry := "retry " + strings.ToLower(nonEmpty(snapshot.LaunchControl.RetryDisposition, "unknown"))
	return state + " | " + retry
}

func operatorPaneCue(snapshot Snapshot) string {
	state := operatorStateLabel(snapshot)
	action := operatorActionLabel(snapshot)
	if state == "" || state == "local-only" {
		return state
	}
	if action == "" || action == "none" {
		return state
	}
	return state + " | next " + action
}

func pendingMessageSummary(snapshot Snapshot, ui UIState) string {
	if ui.PendingTaskMessageEditMode {
		switch ui.PendingTaskMessageSource {
		case "local_scratch_adoption":
			return "editing staged draft from local scratch"
		default:
			return "editing staged local draft"
		}
	}
	if strings.TrimSpace(ui.PendingTaskMessage) != "" {
		switch ui.PendingTaskMessageSource {
		case "local_scratch_adoption":
			return "staged draft from local scratch"
		default:
			return "staged local draft"
		}
	}
	if snapshot.HasLocalScratchAdoption() {
		return "local scratch available"
	}
	return "none"
}

func isScratchIntakeSnapshot(snapshot Snapshot) bool {
	return strings.TrimSpace(snapshot.TaskID) == "" &&
		strings.EqualFold(strings.TrimSpace(snapshot.Phase), "SCRATCH_INTAKE")
}

func footerText(snapshot Snapshot, ui UIState, host WorkerHost) string {
	parts := make([]string, 0, 12)
	if ui.Session.SessionID != "" {
		parts = append(parts, "session "+shortTaskID(ui.Session.SessionID))
	}
	if host != nil {
		status := host.Status()
		if status.InputLive {
			parts = append(parts, "worker live input")
		} else {
			parts = append(parts, "worker read-only")
		}
		if cue := footerHostCue(snapshot, ui, status); cue != "" {
			parts = append(parts, cue)
		}
	}
	if operator := footerOperatorCue(snapshot); operator != "" {
		parts = append(parts, operator)
	}
	parts = append(parts, "q quit", "h help", "i inspector", "p activity", "r refresh", "s status")
	if host != nil && ui.Focus == FocusWorker && host.CanAcceptInput() {
		parts = append(parts, "ctrl-g shell commands")
	}
	if ui.EscapePrefix {
		parts = append(parts, "shell command armed")
	}
	if ui.PendingTaskMessageEditMode {
		parts = append(parts, "editing staged draft")
	} else if pending := strings.TrimSpace(ui.PendingTaskMessage); pending != "" {
		parts = append(parts, "staged local draft")
	} else if snapshot.HasLocalScratchAdoption() {
		parts = append(parts, "local scratch available")
	}
	if !ui.LastRefresh.IsZero() {
		parts = append(parts, "refreshed "+ui.LastRefresh.Format("15:04:05"))
	}
	if ui.LastError != "" {
		parts = append(parts, truncateWithEllipsis(ui.LastError, 80))
	} else if host != nil {
		if note := strings.TrimSpace(host.Status().Note); note != "" {
			parts = append(parts, truncateWithEllipsis(note, 80))
		}
	}
	return strings.Join(parts, " | ")
}

func hostStatusLine(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if host == nil {
		return "none"
	}
	status := host.Status()
	line := fmt.Sprintf("%s / %s", nonEmpty(string(status.Mode), "unknown"), nonEmpty(string(status.State), "unknown"))
	if status.InputLive {
		line += " / input live"
	} else {
		line += " / input off"
	}
	if status.ExitCode != nil {
		line += fmt.Sprintf(" / exit %d", *status.ExitCode)
	}
	if temporal := hostTemporalStatus(snapshot, ui, status); temporal != "" {
		line += " / " + temporal
	}
	if note := strings.TrimSpace(status.Note); note != "" {
		line += " / " + truncateWithEllipsis(note, 48)
	}
	return line
}

func workerPaneSummaryLine(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if host == nil {
		return ""
	}
	status := host.Status()
	label := nonEmpty(strings.TrimSpace(status.Label), strings.TrimSpace(string(status.Mode)))
	now := observedAt(ui)
	cue := workerPanePrimaryCue(snapshot, status, now)
	operatorCue := operatorPaneCue(snapshot)
	if operatorCue == "" {
		if cue == "" {
			return label
		}
		return label + " | " + cue
	}
	if cue == "" {
		return operatorCue + " | " + label
	}
	return operatorCue + " | " + label + " | " + cue
}

func workerPanePrimaryCue(snapshot Snapshot, status HostStatus, now time.Time) string {
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return "awaiting visible output"
		}
		return livePaneCue(status, now)
	case HostStateStarting:
		return "starting up"
	case HostStateExited, HostStateFailed:
		return inactivePaneCue(status)
	case HostStateFallback:
		return "historical transcript below | fallback active"
	case HostStateTranscriptOnly:
		if savedAt := latestTranscriptTimestamp(snapshot); !savedAt.IsZero() {
			return "historical transcript below | saved transcript " + savedAt.Format("15:04:05")
		}
		return "historical transcript below"
	}
	return ""
}

func livePaneCue(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.LastOutputAt)
	switch {
	case since <= 0:
		return "newest output at bottom"
	case since < 60*time.Second:
		return "newest output at bottom"
	case since < 2*time.Minute:
		return "newest output at bottom | quiet"
	default:
		return "newest output at bottom | quiet a while"
	}
}

func inactivePaneCue(status HostStatus) string {
	switch status.State {
	case HostStateFailed:
		return "newest captured output at bottom | worker failed"
	default:
		return "newest captured output at bottom | worker exited"
	}
}

func footerHostCue(snapshot Snapshot, ui UIState, status HostStatus) string {
	now := observedAt(ui)
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return "awaiting output"
		}
		since := elapsedSince(now, status.LastOutputAt)
		switch {
		case since <= 0:
			return "recent output"
		case since < 60*time.Second:
			return "recent output"
		case since < 2*time.Minute:
			return "quiet"
		default:
			return "quiet a while"
		}
	case HostStateStarting:
		return "starting"
	case HostStateExited:
		if elapsedSince(now, status.StateChangedAt) < 30*time.Second {
			return "recent exit"
		}
		return "exited"
	case HostStateFailed:
		if elapsedSince(now, status.StateChangedAt) < 30*time.Second {
			return "recent failure"
		}
		return "failed"
	case HostStateFallback:
		return "fallback active"
	case HostStateTranscriptOnly:
		if !latestTranscriptTimestamp(snapshot).IsZero() {
			return "historical transcript"
		}
		return "read-only transcript"
	}
	return ""
}

func hostTemporalStatus(snapshot Snapshot, ui UIState, status HostStatus) string {
	now := observedAt(ui)
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return describeAwaitingVisibleOutput(status, now)
		}
		return describeLiveOutputAssessment(status, now)
	case HostStateStarting:
		return describeAwaitingVisibleOutput(status, now)
	case HostStateExited, HostStateFailed:
		return describeInactiveState(status, now)
	case HostStateFallback:
		return describeFallbackState(status, now)
	case HostStateTranscriptOnly:
		if savedAt := latestTranscriptTimestamp(snapshot); !savedAt.IsZero() {
			return "latest transcript " + savedAt.Format("15:04:05")
		}
	}
	return ""
}

func latestTranscriptTimestamp(snapshot Snapshot) time.Time {
	var latest time.Time
	for _, item := range snapshot.RecentConversation {
		if item.CreatedAt.After(latest) {
			latest = item.CreatedAt
		}
	}
	return latest
}

func observedAt(ui UIState) time.Time {
	if !ui.ObservedAt.IsZero() {
		return ui.ObservedAt
	}
	if !ui.LastRefresh.IsZero() {
		return ui.LastRefresh
	}
	return time.Now().UTC()
}

func describeAwaitingVisibleOutput(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.StateChangedAt)
	if since <= 0 {
		return "awaiting first visible output"
	}
	return "awaiting first visible output for " + formatElapsed(since)
}

func describeLiveOutputAssessment(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.LastOutputAt)
	if since <= 0 {
		return "quiet with recent visible output"
	}
	if since >= 60*time.Second {
		return "quiet for " + formatElapsed(since) + "; possibly waiting for input or stalled"
	}
	return "quiet for " + formatElapsed(since)
}

func describeInactiveState(status HostStatus, now time.Time) string {
	sinceChange := elapsedSince(now, status.StateChangedAt)
	switch status.State {
	case HostStateFailed:
		if sinceChange > 0 && sinceChange < 30*time.Second {
			return "recently failed " + formatElapsed(sinceChange) + " ago"
		}
		if sinceChange > 0 {
			return "failed " + formatElapsed(sinceChange) + " ago"
		}
		return "worker failed"
	default:
		if sinceChange > 0 && sinceChange < 30*time.Second {
			return "recently exited " + formatElapsed(sinceChange) + " ago"
		}
		if sinceChange > 0 {
			return "exited " + formatElapsed(sinceChange) + " ago"
		}
		return "worker exited"
	}
}

func describeFallbackState(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.StateChangedAt)
	if since <= 0 {
		return "fallback active"
	}
	return "fallback activated " + formatElapsed(since) + " ago"
}

func describeInactiveBody(status HostStatus) string {
	if status.LastOutputAt.IsZero() {
		return "The session ended before any visible output arrived."
	}
	return "No newer worker output arrived after the session ended."
}

func elapsedSince(now time.Time, then time.Time) time.Duration {
	if now.IsZero() || then.IsZero() {
		return 0
	}
	if then.After(now) {
		return 0
	}
	return now.Sub(then)
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	}
	if d < 10*time.Minute {
		seconds := int(d.Round(time.Second) / time.Second)
		minutes := seconds / 60
		remain := seconds % 60
		if remain == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, remain)
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	return fmt.Sprintf("%dm", minutes)
}

func sessionPriorLine(session SessionState) string {
	if strings.TrimSpace(session.PriorPersistedSummary) == "" {
		return "previous shell outcome none"
	}
	return "previous shell outcome " + truncateWithEllipsis(session.PriorPersistedSummary, 48)
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func humanizeConstant(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "_", " ")
}

func footerOperatorCue(snapshot Snapshot) string {
	if snapshot.Recovery == nil || isScratchIntakeSnapshot(snapshot) {
		return ""
	}
	action := operatorActionLabel(snapshot)
	if action == "" || action == "none" {
		return ""
	}
	return "next " + action
}

func truncateWithEllipsis(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func pendingTaskMessageEditorLines(ui UIState, height int, width int) []string {
	if height < 1 {
		return nil
	}
	lines := []string{
		"editing staged local draft",
		"this draft stays shell-local until you explicitly send it",
		"ctrl-g s save edit | ctrl-g c cancel edit | ctrl-g m send | ctrl-g x clear",
		"",
	}
	buffer := currentPendingTaskMessage(ui)
	editorLines := strings.Split(buffer, "\n")
	if len(editorLines) == 0 {
		editorLines = []string{""}
	}
	for idx, line := range editorLines {
		prefix := "draft> "
		if idx > 0 {
			prefix = "       "
		}
		lines = append(lines, wrapText(prefix+line, width)...)
	}
	return fitBottom(lines, height)
}

```

**internal/tui/shell/viewmodel_test.go**

```go
package shell

import (
	"strings"
	"testing"
	"time"

	"tuku/internal/ipc"
)

func TestBuildViewModelReflectsSnapshotState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:    "worker pane | codex live session",
		worker:   "codex live",
		lines:    []string{"codex> hello"},
		activity: []string{"12:00:00  worker host started"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
			Width:     80,
			Height:    20,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_1234567890",
		Goal:   "Implement shell",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Repo: RepoAnchor{
			RepoRoot:         "/Users/kagaya/Desktop/Tuku",
			Branch:           "main",
			HeadSHA:          "abc123",
			WorkingTreeDirty: true,
			CapturedAt:       now,
		},
		IntentSummary: "implement: build shell",
		Brief: &BriefSummary{
			ID:               "brf_1",
			Objective:        "Build worker-native shell",
			NormalizedAction: "build-shell",
			Constraints:      []string{"keep it narrow"},
			DoneCriteria:     []string{"full-screen shell"},
		},
		Run: &RunSummary{
			ID:               "run_1",
			WorkerKind:       "codex",
			Status:           "RUNNING",
			LastKnownSummary: "applying shell patch",
			StartedAt:        now,
		},
		Checkpoint: &CheckpointSummary{
			ID:               "chk_1",
			Trigger:          "CONTINUE",
			CreatedAt:        now,
			ResumeDescriptor: "resume from shell-ready checkpoint",
			IsResumable:      true,
		},
		Recovery: &RecoverySummary{
			Class:           "READY_NEXT_RUN",
			Action:          "START_NEXT_RUN",
			ReadyForNextRun: true,
			Reason:          "task is ready for the next bounded run with brief brf_1",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
			CreatedAt:    now,
		},
		Acknowledgment: &AcknowledgmentSummary{
			Status:    "CAPTURED",
			Summary:   "Claude acknowledged the handoff packet.",
			CreatedAt: now,
		},
		RecentProofs: []ProofItem{
			{ID: "evt_1", Type: "BRIEF_CREATED", Summary: "Execution brief updated", Timestamp: now},
			{ID: "evt_2", Type: "HANDOFF_CREATED", Summary: "Handoff packet created", Timestamp: now},
		},
		RecentConversation: []ConversationItem{
			{Role: "user", Body: "Start implementation.", CreatedAt: now},
			{Role: "system", Body: "I prepared the shell state.", CreatedAt: now},
		},
		LatestCanonicalResponse: "I prepared the shell state.",
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
		Session: SessionState{
			SessionID: "shs_1234567890",
			StartedAt: now,
			Journal: []SessionEvent{
				{Type: SessionEventShellStarted, Summary: "Shell session shs_1234567890 started.", CreatedAt: now},
				{Type: SessionEventHostLive, Summary: "Live worker host is active.", CreatedAt: now},
			},
			PriorPersistedSummary: "Shell live host ended",
		},
		LastRefresh: now,
	}, host, 120, 32)

	if vm.Header.Worker != "codex live" {
		t.Fatalf("expected active worker label, got %q", vm.Header.Worker)
	}
	if vm.Header.Continuity != "ready" {
		t.Fatalf("expected ready continuity, got %q", vm.Header.Continuity)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	if vm.ProofStrip == nil {
		t.Fatal("expected proof strip")
	}
	if vm.Overlay != nil {
		t.Fatal("expected no overlay")
	}
	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if len(vm.ProofStrip.Lines) < 2 {
		t.Fatal("expected activity lines merged into proof strip")
	}
	if vm.Inspector.Sections[0].Title != "operator" {
		t.Fatalf("expected operator section first, got %q", vm.Inspector.Sections[0].Title)
	}
	foundOperatorNext := false
	for _, line := range vm.Inspector.Sections[0].Lines {
		if strings.Contains(line, "next start next run") {
			foundOperatorNext = true
		}
	}
	if !foundOperatorNext {
		t.Fatalf("expected operator section to include next action, got %#v", vm.Inspector.Sections[0].Lines)
	}
	if vm.Inspector.Sections[1].Title != "worker session" {
		t.Fatalf("expected worker session section second, got %q", vm.Inspector.Sections[1].Title)
	}
	foundSessionLine := false
	for _, line := range vm.Inspector.Sections[1].Lines {
		if strings.Contains(line, "new shell session shs_1234567890") {
			foundSessionLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected worker-session inspector to include session id, got %#v", vm.Inspector.Sections[1].Lines)
	}
}

func TestBuildViewModelSurfacesRebriefRequiredState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_rebrief",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "REBRIEF_REQUIRED",
			Action:          "REGENERATE_BRIEF",
			ReadyForNextRun: false,
			Reason:          "operator chose to regenerate the execution brief before another run",
		},
		Checkpoint: &CheckpointSummary{
			ID:          "chk_rebrief",
			IsResumable: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "rebrief" {
		t.Fatalf("expected rebrief continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery rebrief required") {
		t.Fatalf("expected rebrief operator state, got %q", status)
	}
	if !strings.Contains(status, "next regenerate brief") {
		t.Fatalf("expected regenerate-brief operator action, got %q", status)
	}
}

func TestBuildViewModelAddsLiveWorkerPaneRecencySummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | codex live | input to worker",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_live_summary",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(18 * time.Second),
		Session: SessionState{
			SessionID: "shs_live_summary",
		},
	}, host, 120, 20)

	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "codex live | newest output at bottom") {
		t.Fatalf("expected live recency summary, got %#v", vm.WorkerPane.Lines)
	}
	if strings.Contains(vm.WorkerPane.Lines[0], "quiet for 18s") {
		t.Fatalf("expected pane summary to stay concise, got %#v", vm.WorkerPane.Lines)
	}
}

func TestSnapshotFromIPCMapsShellState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	raw := ipc.TaskShellSnapshotResponse{
		TaskID:        "tsk_1",
		Goal:          "Goal",
		Phase:         "BRIEF_READY",
		Status:        "ACTIVE",
		IntentClass:   "implement",
		IntentSummary: "implement: wire shell",
		RepoAnchor: ipc.RepoAnchor{
			RepoRoot:         "/tmp/repo",
			Branch:           "main",
			HeadSHA:          "sha",
			WorkingTreeDirty: true,
			CapturedAt:       now,
		},
		Brief: &ipc.TaskShellBrief{
			BriefID:          "brf_1",
			Objective:        "Objective",
			NormalizedAction: "act",
			Constraints:      []string{"c1"},
			DoneCriteria:     []string{"d1"},
		},
		Run: &ipc.TaskShellRun{
			RunID:            "run_1",
			WorkerKind:       "codex",
			Status:           "COMPLETED",
			LastKnownSummary: "done",
			StartedAt:        now,
		},
		Launch: &ipc.TaskShellLaunch{
			AttemptID:   "hlc_1",
			LaunchID:    "launch_1",
			Status:      "FAILED",
			RequestedAt: now,
			EndedAt:     now.Add(2 * time.Second),
			Summary:     "launcher failed",
		},
		LaunchControl: &ipc.TaskShellLaunchControl{
			State:            "FAILED",
			RetryDisposition: "ALLOWED",
			Reason:           "durable failure may be retried",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			LaunchID:         "launch_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
			FailedAt:         now.Add(2 * time.Second),
		},
		Recovery: &ipc.TaskShellRecovery{
			ContinuityOutcome:     "SAFE_RESUME_AVAILABLE",
			RecoveryClass:         "FAILED_RUN_REVIEW_REQUIRED",
			RecommendedAction:     "INSPECT_FAILED_RUN",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "latest run failed",
			Issues: []ipc.TaskShellRecoveryIssue{
				{Code: "RUN_BRIEF_MISSING", Message: "run references missing brief"},
			},
		},
		RecentProofs: []ipc.TaskShellProof{
			{EventID: "evt_1", Type: "CHECKPOINT_CREATED", Summary: "Checkpoint created", Timestamp: now},
		},
		RecentConversation: []ipc.TaskShellConversation{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
		LatestCanonicalResponse: "Canonical response.",
	}

	snapshot := snapshotFromIPC(raw)
	if snapshot.TaskID != "tsk_1" {
		t.Fatalf("expected task id tsk_1, got %q", snapshot.TaskID)
	}
	if snapshot.Brief == nil || snapshot.Brief.ID != "brf_1" {
		t.Fatal("expected brief mapping")
	}
	if snapshot.Run == nil || snapshot.Run.WorkerKind != "codex" {
		t.Fatal("expected run mapping")
	}
	if snapshot.Launch == nil || snapshot.Launch.AttemptID != "hlc_1" {
		t.Fatalf("expected launch mapping, got %+v", snapshot.Launch)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.RetryDisposition != "ALLOWED" {
		t.Fatalf("expected launch control mapping, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.Class != "FAILED_RUN_REVIEW_REQUIRED" {
		t.Fatalf("expected recovery mapping, got %+v", snapshot.Recovery)
	}
	if len(snapshot.RecentProofs) != 1 || snapshot.RecentProofs[0].Summary != "Checkpoint created" {
		t.Fatal("expected proof mapping")
	}
}

func TestContinuityLabelUsesRecoveryTruthOverRawCheckpointResumability(t *testing.T) {
	snapshot := Snapshot{
		Status: "ACTIVE",
		Checkpoint: &CheckpointSummary{
			ID:          "chk_1",
			IsResumable: true,
		},
		Recovery: &RecoverySummary{
			Class:           "FAILED_RUN_REVIEW_REQUIRED",
			ReadyForNextRun: false,
		},
	}

	if got := continuityLabel(snapshot); got != "review" {
		t.Fatalf("expected recovery-driven continuity label, got %q", got)
	}
}

func TestContinuityLabelDistinguishesReadyRecoverableAndLaunchStates(t *testing.T) {
	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "ready next run",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "READY_NEXT_RUN", ReadyForNextRun: true},
			},
			want: "ready",
		},
		{
			name: "interrupted recoverable",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "INTERRUPTED_RUN_RECOVERABLE", ReadyForNextRun: true},
			},
			want: "recoverable",
		},
		{
			name: "launch retry",
			snapshot: Snapshot{
				Recovery:      &RecoverySummary{Class: "ACCEPTED_HANDOFF_LAUNCH_READY", ReadyForHandoffLaunch: true},
				LaunchControl: &LaunchControlSummary{State: "FAILED", RetryDisposition: "ALLOWED"},
			},
			want: "launch-retry",
		},
		{
			name: "completed",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "COMPLETED_NO_ACTION"},
			},
			want: "complete",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := continuityLabel(tc.snapshot); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildViewModelStatusOverlayReflectsHostState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		status: HostStatus{
			Mode:           HostModeTranscript,
			State:          HostStateFallback,
			Label:          "transcript fallback",
			InputLive:      false,
			Note:           "live worker exited; switched to transcript fallback",
			StateChangedAt: now,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_overlay",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "FAILED_RUN_REVIEW_REQUIRED",
			Action:          "INSPECT_FAILED_RUN",
			ReadyForNextRun: false,
			Reason:          "latest run run_1 failed; inspect failure evidence before retrying or regenerating the brief",
		},
		LatestCanonicalResponse: "Tuku is ready to continue from transcript mode.",
	}, UIState{
		ShowStatus: true,
		ObservedAt: now.Add(6 * time.Second),
		Session: SessionState{
			SessionID:             "shs_overlay",
			PriorPersistedSummary: "Shell transcript fallback activated",
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundHostLine := false
	foundVerboseFallbackTiming := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "host transcript / fallback / input off") {
			foundHostLine = true
		}
		if strings.Contains(line, "fallback activated 6s ago") {
			foundVerboseFallbackTiming = true
		}
	}
	if !foundHostLine {
		t.Fatalf("expected host status line in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundVerboseFallbackTiming {
		t.Fatalf("expected overlay to retain verbose fallback timing, got %#v", vm.Overlay.Lines)
	}
	foundSessionLine := false
	foundPriorLine := false
	foundRecoveryLine := false
	foundNextLine := false
	foundReasonLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "new shell session shs_overlay") {
			foundSessionLine = true
		}
		if strings.Contains(line, "previous shell outcome Shell transcript fallback activated") {
			foundPriorLine = true
		}
		if strings.Contains(line, "recovery failed run review required") {
			foundRecoveryLine = true
		}
		if strings.Contains(line, "next inspect failed run") {
			foundNextLine = true
		}
		if strings.Contains(line, "reason latest run run_1 failed") {
			foundReasonLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected session id in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundPriorLine {
		t.Fatalf("expected previous shell outcome in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundRecoveryLine || !foundNextLine || !foundReasonLine {
		t.Fatalf("expected operator truth lines in overlay, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "read-only") {
		t.Fatalf("expected footer to clarify read-only fallback, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "fallback active") {
		t.Fatalf("expected footer to include short fallback cue, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "next inspect failed run") {
		t.Fatalf("expected footer to include operator next-action cue, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "fallback activated 6s ago") {
		t.Fatalf("expected footer to avoid duplicating verbose fallback timing, got %q", vm.Footer)
	}
}

func TestBuildViewModelAddsFallbackWorkerPaneSummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.status.StateChangedAt = now
	host.UpdateSnapshot(Snapshot{
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	})

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_fallback_summary",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "HANDOFF_LAUNCH_PENDING_OUTCOME",
			Action:          "WAIT_FOR_LAUNCH_OUTCOME",
			ReadyForNextRun: false,
		},
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(6 * time.Second),
		Session: SessionState{
			SessionID: "shs_fallback_summary",
		},
	}, host, 120, 20)

	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "launch pending | next wait for launch outcome | transcript fallback | historical transcript below | fallback active") {
		t.Fatalf("expected fallback summary line, got %#v", vm.WorkerPane.Lines)
	}
}

func TestBuildViewModelShowsLongQuietLiveInferenceCarefully(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | codex live | input to worker",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_live_quiet",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(2 * time.Minute),
		Session: SessionState{
			SessionID: "shs_live_quiet",
		},
	}, host, 120, 20)

	if !strings.Contains(vm.WorkerPane.Lines[0], "quiet a while") {
		t.Fatalf("expected concise long-quiet pane cue, got %#v", vm.WorkerPane.Lines)
	}
	if !strings.Contains(vm.Footer, "quiet a while") {
		t.Fatalf("expected footer to carry a short quiet-state cue, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "possibly waiting for input or stalled") {
		t.Fatalf("expected footer to avoid duplicating verbose quiet inference, got %q", vm.Footer)
	}
}

func TestBuildViewModelReflectsClaudeHostState(t *testing.T) {
	host := &stubHost{
		title:    "worker pane | claude live session",
		worker:   "claude live",
		lines:    []string{"claude> hello"},
		canInput: true,
		status: HostStatus{
			Mode:  HostModeClaudePTY,
			State: HostStateLive,
			Label: "claude live",
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_claude",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_claude",
		},
	}, host, 120, 32)

	if vm.Header.Worker != "claude live" {
		t.Fatalf("expected claude worker label, got %q", vm.Header.Worker)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundHostLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "host claude-pty / live / input live") {
			foundHostLine = true
		}
	}
	if !foundHostLine {
		t.Fatalf("expected claude host line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelSurfacesInterruptedRecoverableState(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex transcript",
		worker: "codex last",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "codex transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_interrupt",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "INTERRUPTED_RUN_RECOVERABLE",
			Action:                "RESUME_INTERRUPTED_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			Reason:                "latest run run_1 was interrupted and can be resumed safely",
		},
	}, UIState{
		ShowInspector: true,
		Session:       SessionState{SessionID: "shs_interrupt"},
	}, host, 120, 32)

	if vm.Header.Continuity != "recoverable" {
		t.Fatalf("expected recoverable continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "interrupted recoverable | next resume interrupted run") {
		t.Fatalf("expected interrupted operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	joined := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(joined, "readiness next-run yes | handoff-launch no") || !strings.Contains(joined, "reason latest run run_1 was interrupted") {
		t.Fatalf("expected interrupted recovery truth in operator section, got %q", joined)
	}
}

func TestBuildViewModelSurfacesAcceptedHandoffLaunchReadyState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | transcript",
		worker: "claude handoff",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch_ready",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "ACCEPTED_HANDOFF_LAUNCH_READY",
			Action:                "LAUNCH_ACCEPTED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: true,
			Reason:                "accepted handoff hnd_1 is ready to launch for claude",
		},
		LaunchControl: &LaunchControlSummary{
			State:            "NOT_REQUESTED",
			RetryDisposition: "ALLOWED",
			Reason:           "accepted handoff hnd_1 is ready to launch for claude",
			HandoffID:        "hnd_1",
			TargetWorker:     "claude",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
			CreatedAt:    now,
		},
	}, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_launch_ready"},
	}, host, 120, 32)

	if vm.Header.Continuity != "handoff-ready" {
		t.Fatalf("expected handoff-ready continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "accepted handoff launch ready | next launch accepted handoff") {
		t.Fatalf("expected worker pane operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundReadiness := false
	foundLaunch := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "readiness next-run no | handoff-launch yes") {
			foundReadiness = true
		}
		if strings.Contains(line, "launch not requested | retry allowed") {
			foundLaunch = true
		}
	}
	if !foundReadiness || !foundLaunch {
		t.Fatalf("expected launch-ready operator lines, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelSurfacesRepairRequiredReason(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_repair",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Checkpoint: &CheckpointSummary{
			ID:          "chk_repair",
			IsResumable: true,
		},
		Recovery: &RecoverySummary{
			Class:                 "REPAIR_REQUIRED",
			Action:                "REPAIR_CONTINUITY",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "capsule references missing brief brf_missing",
			Issues:                []RecoveryIssue{{Code: "MISSING_BRIEF", Message: "capsule references missing brief brf_missing"}},
		},
	}, UIState{
		ShowInspector: true,
		Session:       SessionState{SessionID: "shs_repair"},
	}, host, 120, 32)

	if vm.Header.Continuity != "repair" {
		t.Fatalf("expected repair continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "repair required | next repair continuity") {
		t.Fatalf("expected repair operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	if vm.Inspector.Sections[0].Title != "operator" {
		t.Fatalf("expected operator section first, got %q", vm.Inspector.Sections[0].Title)
	}
	joined := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(joined, "state repair required") || !strings.Contains(joined, "reason capsule references missing brief brf_missing") {
		t.Fatalf("expected repair reason in operator section, got %q", joined)
	}
	checkpointJoined := ""
	for _, section := range vm.Inspector.Sections {
		if section.Title == "checkpoint" {
			checkpointJoined = strings.Join(section.Lines, "\n")
			break
		}
	}
	if !strings.Contains(checkpointJoined, "raw resumable yes") {
		t.Fatalf("expected checkpoint section to preserve raw resumable truth, got %q", checkpointJoined)
	}
}

func TestBuildViewModelSurfacesPendingLaunchBlockedRetry(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.status.StateChangedAt = now
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch_pending",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "HANDOFF_LAUNCH_PENDING_OUTCOME",
			Action:                "WAIT_FOR_LAUNCH_OUTCOME",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "handoff launch hlc_1 is still pending durable outcome",
		},
		Launch: &LaunchSummary{
			AttemptID:   "hlc_1",
			Status:      "REQUESTED",
			RequestedAt: now,
			Summary:     "launch requested",
		},
		LaunchControl: &LaunchControlSummary{
			State:            "REQUESTED_OUTCOME_UNKNOWN",
			RetryDisposition: "BLOCKED",
			Reason:           "handoff launch hlc_1 is still pending durable outcome",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
		},
	}, UIState{
		ShowStatus: true,
		ObservedAt: now.Add(6 * time.Second),
		Session:    SessionState{SessionID: "shs_launch_pending"},
	}, host, 120, 32)

	if vm.Header.Continuity != "launch-pending" {
		t.Fatalf("expected launch-pending continuity, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRecovery := false
	foundNext := false
	foundLaunch := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "recovery launch pending") {
			foundRecovery = true
		}
		if strings.Contains(line, "next wait for launch outcome") {
			foundNext = true
		}
		if strings.Contains(line, "launch pending outcome unknown | retry blocked") {
			foundLaunch = true
		}
	}
	if !foundRecovery || !foundNext || !foundLaunch {
		t.Fatalf("expected pending-launch operator truth in overlay, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelInspectorLaunchSectionUsesLaunchControlTruth(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | transcript",
		worker: "claude handoff",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "HANDOFF_LAUNCH_COMPLETED",
			Action:                "MONITOR_LAUNCHED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "handoff hnd_1 already has durable completed launch launch_1",
		},
		Launch: &LaunchSummary{
			AttemptID:   "hlc_1",
			LaunchID:    "launch_1",
			Status:      "COMPLETED",
			RequestedAt: now,
			EndedAt:     now.Add(2 * time.Second),
		},
		LaunchControl: &LaunchControlSummary{
			State:            "COMPLETED",
			RetryDisposition: "BLOCKED",
			Reason:           "handoff hnd_1 already has durable completed launch launch_1",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			LaunchID:         "launch_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
			CompletedAt:      now.Add(2 * time.Second),
		},
	}, UIState{
		ShowInspector: true,
	}, host, 120, 32)

	if vm.Header.Continuity != "launched" {
		t.Fatalf("expected launched continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundLaunchSection := false
	foundInvocationOnly := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "launch" {
			continue
		}
		foundLaunchSection = true
		for _, line := range section.Lines {
			if strings.Contains(line, "completed (invocation only) | retry blocked") {
				foundInvocationOnly = true
			}
		}
	}
	if !foundLaunchSection || !foundInvocationOnly {
		t.Fatalf("expected launch inspector section with invocation-only truth, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelShowsAnotherKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_known",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_other", SessionClass: KnownShellSessionClassAttachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "another attachable shell session is known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsStaleKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_stale",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_stale", SessionClass: KnownShellSessionClassStale, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "stale shell session is known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected stale registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsActiveUnattachableKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_unattachable",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_other", SessionClass: KnownShellSessionClassActiveUnattachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateFallback, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "another active but non-attachable shell session is known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected active-unattachable registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsNoRepoLabels(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		Phase:                   "SCRATCH_INTAKE",
		Status:                  "LOCAL_ONLY",
		IntentClass:             "scratch",
		IntentSummary:           "Use this local scratch session to plan work before cloning or initializing a repository.",
		LatestCanonicalResponse: "No git repository was detected. Tuku opened a local scratch and intake session instead of repo-backed continuity.",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_no_repo",
		},
	}, host, 120, 32)

	if vm.Header.TaskLabel != "no-task" {
		t.Fatalf("expected no-task header label, got %q", vm.Header.TaskLabel)
	}
	if vm.Header.Repo != "no-repo" {
		t.Fatalf("expected no-repo header label, got %q", vm.Header.Repo)
	}
	if vm.Header.Continuity != "local-only" {
		t.Fatalf("expected local-only continuity, got %q", vm.Header.Continuity)
	}
	if vm.Header.Worker != "scratch intake" {
		t.Fatalf("expected scratch intake worker label, got %q", vm.Header.Worker)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundTaskLine := false
	foundRepoLine := false
	foundContinuityLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "task no-task") {
			foundTaskLine = true
		}
		if strings.Contains(line, "repo no-repo") {
			foundRepoLine = true
		}
		if strings.Contains(line, "continuity local-only") {
			foundContinuityLine = true
		}
	}
	if !foundTaskLine || !foundRepoLine || !foundContinuityLine {
		t.Fatalf("expected no-repo overlay lines, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsPendingScratchAdoptionDraft(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_pending",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector:            true,
		ShowStatus:               true,
		PendingTaskMessage:       "Explicitly adopt these local scratch intake notes into this repo-backed Tuku task:\n\n- Plan project structure",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session: SessionState{
			SessionID: "shs_pending",
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Local draft is staged and ready for review.") {
			t.Fatalf("expected staged draft guidance, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft staged draft from local scratch") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected pending message overlay line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsPendingDraftEditMode(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex live session",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_editing",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector:                true,
		ShowStatus:                   true,
		Focus:                        FocusWorker,
		PendingTaskMessage:           "Saved draft",
		PendingTaskMessageSource:     "local_scratch_adoption",
		PendingTaskMessageEditMode:   true,
		PendingTaskMessageEditBuffer: "Edited draft",
		Session: SessionState{
			SessionID: "shs_editing",
		},
	}, host, 120, 32)

	if vm.WorkerPane.Title != "worker pane | pending message editor" {
		t.Fatalf("expected worker pane edit title, got %q", vm.WorkerPane.Title)
	}
	joinedPane := strings.Join(vm.WorkerPane.Lines, "\n")
	if !strings.Contains(joinedPane, "Edited draft") {
		t.Fatalf("expected editor lines to show edited draft, got %q", joinedPane)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Editing the staged local draft.") {
			t.Fatalf("expected edit-mode copy, got %q", joined)
		}
		if !strings.Contains(joined, "Nothing here is canonical until you explicitly send it with m.") {
			t.Fatalf("expected explicit local-only boundary, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft editing staged draft from local scratch") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected edit-mode overlay line, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "editing staged draft") {
		t.Fatalf("expected footer edit-mode hint, got %q", vm.Footer)
	}
}

func TestBuildViewModelShowsLocalScratchAvailableState(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_local_scratch",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session: SessionState{
			SessionID: "shs_local_scratch",
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Local scratch is available for explicit adoption.") {
			t.Fatalf("expected local scratch available copy, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft local scratch available") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected local scratch available overlay line, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "local scratch available") {
		t.Fatalf("expected footer local scratch hint, got %q", vm.Footer)
	}
}

func TestBuildViewModelCollapsesSecondaryChromeInNarrowTerminal(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex live session",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_narrow",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
		Session: SessionState{
			SessionID: "shs_narrow",
		},
	}, host, 100, 16)

	if vm.Inspector != nil {
		t.Fatalf("expected inspector to auto-collapse in narrow terminal, got %#v", vm.Inspector)
	}
	if vm.ProofStrip != nil {
		t.Fatalf("expected activity strip to auto-collapse in narrow terminal, got %#v", vm.ProofStrip)
	}
	if vm.Layout.showInspector || vm.Layout.showProof {
		t.Fatalf("expected collapsed layout flags, got %+v", vm.Layout)
	}
}

```

**internal/orchestrator/service_test.go**

```go
package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func TestStartTaskCreatesCapsuleWithAnchorAndProof(t *testing.T) {
	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "abc123", WorkingTreeDirty: true, CapturedAt: time.Unix(1700000000, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	res, err := coord.StartTask(context.Background(), "Build milestone four", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	caps, err := store.Capsules().Get(res.TaskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.BranchName != "main" || caps.HeadSHA != "abc123" || !caps.WorkingTreeDirty {
		t.Fatalf("expected anchor persisted in capsule: %+v", caps)
	}
}

func TestMessageCreatesIntentAndBriefAndProof(t *testing.T) {
	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "head-1", WorkingTreeDirty: false, CapturedAt: time.Unix(1700001000, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "Implement parser", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	msgRes, err := coord.MessageTask(context.Background(), string(start.TaskID), "continue and prepare implementation")
	if err != nil {
		t.Fatalf("message task: %v", err)
	}
	if msgRes.BriefID == "" || msgRes.BriefHash == "" {
		t.Fatal("expected brief id and hash")
	}

	events, err := store.Proofs().ListByTask(start.TaskID, 30)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventBriefCreated) {
		t.Fatal("expected brief created event")
	}
}

func TestStartTaskRollsBackOnProofAppendFailure(t *testing.T) {
	base := newTestStore(t)
	injected := &faultInjectedStore{base: base, failProofAppend: true}
	coord, err := NewCoordinator(Dependencies{
		Store:          injected,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
		IDGenerator: func(prefix string) string {
			return prefix + "_fixed"
		},
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	if _, err := coord.StartTask(context.Background(), "tx rollback start", "/tmp/repo"); err == nil {
		t.Fatal("expected start task failure")
	}

	if _, err := base.Capsules().Get(common.TaskID("tsk_fixed")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no persisted capsule after rollback, got err=%v", err)
	}
	events, err := base.Proofs().ListByTask(common.TaskID("tsk_fixed"), 20)
	if err != nil {
		t.Fatalf("list proofs after rollback: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no proof events for rolled-back start, got %d", len(events))
	}
}

func TestMessageTaskRollsBackOnSynthesisFailure(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	start := setupTaskWithBrief(t, coord)

	capsBefore, err := store.Capsules().Get(start)
	if err != nil {
		t.Fatalf("get capsule before: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversations before: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(start, 200)
	if err != nil {
		t.Fatalf("list proofs before: %v", err)
	}

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}

	if _, err := failCoord.MessageTask(context.Background(), string(start), "this write should rollback"); err == nil {
		t.Fatal("expected message task failure")
	}

	capsAfter, err := store.Capsules().Get(start)
	if err != nil {
		t.Fatalf("get capsule after: %v", err)
	}
	if capsAfter.CurrentIntentID != capsBefore.CurrentIntentID {
		t.Fatalf("capsule intent pointer changed despite rollback: before=%s after=%s", capsBefore.CurrentIntentID, capsAfter.CurrentIntentID)
	}
	if capsAfter.CurrentBriefID != capsBefore.CurrentBriefID {
		t.Fatalf("capsule brief pointer changed despite rollback: before=%s after=%s", capsBefore.CurrentBriefID, capsAfter.CurrentBriefID)
	}

	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversations after: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("conversation count changed despite rollback: before=%d after=%d", len(convBefore), len(convAfter))
	}
	eventsAfter, err := store.Proofs().ListByTask(start, 200)
	if err != nil {
		t.Fatalf("list proofs after: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("proof event count changed despite rollback: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
}

func TestRunRealSuccessCompletesAndRecordsEvidence(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if res.RunID == "" {
		t.Fatal("expected run id")
	}
	if res.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed status, got %s", res.RunStatus)
	}
	if res.Phase != phase.PhaseValidating {
		t.Fatalf("expected %s phase, got %s", phase.PhaseValidating, res.Phase)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "completed") {
		t.Fatalf("expected canonical completion response, got %q", res.CanonicalResponse)
	}

	runRec, err := store.Runs().Get(res.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if runRec.Status != rundomain.StatusCompleted {
		t.Fatalf("expected run status completed, got %s", runRec.Status)
	}

	events, err := store.Proofs().ListByTask(taskID, 80)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventWorkerRunStarted) {
		t.Fatal("expected worker run started")
	}
	if !hasEvent(events, proof.EventWorkerOutputCaptured) {
		t.Fatal("expected worker output captured")
	}
	if !hasEvent(events, proof.EventFileChangeDetected) {
		t.Fatal("expected file change detected event")
	}
	if !hasEvent(events, proof.EventWorkerRunCompleted) {
		t.Fatal("expected worker run completed")
	}
	for _, e := range events {
		switch e.Type {
		case proof.EventWorkerRunStarted, proof.EventWorkerOutputCaptured, proof.EventFileChangeDetected, proof.EventWorkerRunCompleted, proof.EventWorkerRunFailed, proof.EventRunInterrupted:
			if e.RunID == nil {
				t.Fatalf("expected run_id for run-related event %s", e.Type)
			}
		}
	}
}

func TestRunRealFailureMarksBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real failure path: %v", err)
	}
	if res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed status, got %s", res.RunStatus)
	}
	if res.Phase != phase.PhaseBlocked {
		t.Fatalf("expected %s phase, got %s", phase.PhaseBlocked, res.Phase)
	}

	events, err := store.Proofs().ListByTask(taskID, 80)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventWorkerRunFailed) {
		t.Fatal("expected worker run failed")
	}
}

func TestRunRealAdapterErrorMarksBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterError(errors.New("codex missing")))
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real adapter error should map to canonical failure, got: %v", err)
	}
	if res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed status, got %s", res.RunStatus)
	}
}

func TestRunRealPassesBoundedExecutionEnvelopeToAdapter(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if !adapter.called {
		t.Fatal("expected adapter execute to be called")
	}
	if adapter.lastReq.TaskID != taskID {
		t.Fatalf("expected adapter task id %s, got %s", taskID, adapter.lastReq.TaskID)
	}
	if adapter.lastReq.RunID != res.RunID {
		t.Fatalf("expected adapter run id %s, got %s", res.RunID, adapter.lastReq.RunID)
	}
	if adapter.lastReq.Brief.BriefID == "" {
		t.Fatal("expected adapter brief id to be populated")
	}
	if adapter.lastReq.Brief.NormalizedAction == "" {
		t.Fatal("expected adapter normalized action to be populated")
	}
	if adapter.lastReq.RepoAnchor.RepoRoot == "" {
		t.Fatal("expected adapter repo root to be populated")
	}
	if adapter.lastReq.ContextSummary == "" {
		t.Fatal("expected adapter context summary to be populated")
	}
	if adapter.lastReq.PolicyProfileID == "" {
		t.Fatal("expected adapter policy profile to be populated")
	}
}

func TestRunDurablyRunningBeforeWorkerExecute(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	var observedRunStatus rundomain.Status
	var observedCapsulePhase phase.Phase
	adapter.onExecute = func(req adapter_contract.ExecutionRequest) {
		runRec, err := store.Runs().Get(req.RunID)
		if err != nil {
			t.Fatalf("expected run to exist before execute: %v", err)
		}
		observedRunStatus = runRec.Status

		caps, err := store.Capsules().Get(req.TaskID)
		if err != nil {
			t.Fatalf("expected capsule to exist before execute: %v", err)
		}
		observedCapsulePhase = caps.CurrentPhase
	}
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if observedRunStatus != rundomain.StatusRunning {
		t.Fatalf("expected RUNNING before execute, got %s", observedRunStatus)
	}
	if observedCapsulePhase != phase.PhaseExecuting {
		t.Fatalf("expected EXECUTING before execute, got %s", observedCapsulePhase)
	}
	if res.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed final status, got %s", res.RunStatus)
	}
}

func TestCanonicalResponseNotRawWorkerText(t *testing.T) {
	store := newTestStore(t)
	adapter := &fakeWorkerAdapter{kind: adapter_contract.WorkerCodex, result: adapter_contract.ExecutionResult{
		ExitCode:  0,
		Stdout:    "RAW_WORKER_OUTPUT_TOKEN_12345",
		Stderr:    "",
		Summary:   "completed summary",
		StartedAt: time.Now().UTC(),
		EndedAt:   time.Now().UTC(),
	}}
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if res.CanonicalResponse == adapter.result.Stdout {
		t.Fatal("canonical response must not equal raw worker stdout")
	}
	if strings.Contains(res.CanonicalResponse, "RAW_WORKER_OUTPUT_TOKEN_12345") {
		t.Fatal("canonical response leaked raw worker token")
	}
}

func TestRunNoBriefBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "No brief case", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(start.TaskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" {
		t.Fatalf("expected empty run id when blocked, got %s", res.RunID)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "cannot start") {
		t.Fatalf("unexpected canonical response: %s", res.CanonicalResponse)
	}
}

func TestRunNoopModeManualLifecycleStillWorks(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop start: %v", err)
	}
	if startRes.RunStatus != rundomain.StatusRunning {
		t.Fatalf("expected running noop run, got %s", startRes.RunStatus)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after noop start: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseExecuting {
		t.Fatalf("running invariant broken: expected phase %s, got %s", phase.PhaseExecuting, caps.CurrentPhase)
	}
	completeRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "complete", RunID: startRes.RunID})
	if err != nil {
		t.Fatalf("noop complete: %v", err)
	}
	if completeRes.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed noop run, got %s", completeRes.RunStatus)
	}
}

func TestRunInterruptSetsPausedInvariant(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop start: %v", err)
	}
	interruptRes, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startRes.RunID,
		InterruptionReason: "test interruption",
	})
	if err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if interruptRes.RunStatus != rundomain.StatusInterrupted {
		t.Fatalf("expected interrupted status, got %s", interruptRes.RunStatus)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after interrupt: %v", err)
	}
	if caps.CurrentPhase != phase.PhasePaused {
		t.Fatalf("interrupt invariant broken: expected phase %s, got %s", phase.PhasePaused, caps.CurrentPhase)
	}
}

func TestStatusAndInspectExposeLatestRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.LatestRunID != runRes.RunID {
		t.Fatalf("status missing latest run id: %+v", status)
	}

	ins, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if ins.Run == nil || ins.Run.RunID != runRes.RunID {
		t.Fatalf("inspect missing latest run: %+v", ins)
	}
}

func TestBriefBuilderDeterministicHash(t *testing.T) {
	builder := NewBriefBuilderV1(func(_ string) string { return "brf_fixed" }, func() time.Time {
		return time.Unix(1700003000, 0).UTC()
	})

	input := brief.BuildInput{
		TaskID:           "tsk_1",
		IntentID:         "int_1",
		CapsuleVersion:   2,
		Goal:             "Implement feature X",
		NormalizedAction: "continue from current state",
		Constraints:      []string{"do not execute workers"},
		ScopeHints:       []string{"internal/orchestrator"},
		ScopeOutHints:    []string{"web"},
		DoneCriteria:     []string{"brief is generated"},
		Verbosity:        brief.VerbosityStandard,
		PolicyProfileID:  "default-safe-v1",
	}

	b1, err := builder.Build(input)
	if err != nil {
		t.Fatalf("build brief 1: %v", err)
	}
	b2, err := builder.Build(input)
	if err != nil {
		t.Fatalf("build brief 2: %v", err)
	}
	if b1.BriefHash != b2.BriefHash {
		t.Fatalf("expected deterministic hash, got %s vs %s", b1.BriefHash, b2.BriefHash)
	}
}

func TestRunTaskKeepsDurableRunningStateWhenFinalizationFails(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 200)
	if err != nil {
		t.Fatalf("list conversations before: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proofs before: %v", err)
	}

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}

	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected run task failure")
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("expected persisted running run after stage-1 commit, got err=%v", err)
	}
	if runRec.Status != rundomain.StatusRunning {
		t.Fatalf("expected run to remain RUNNING when finalization fails, got %s", runRec.Status)
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after: %v", err)
	}
	if capsAfter.CurrentPhase != phase.PhaseExecuting {
		t.Fatalf("expected capsule to remain EXECUTING when finalization fails, got %s", capsAfter.CurrentPhase)
	}
	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 200)
	if err != nil {
		t.Fatalf("list conversations after: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("conversation count changed despite rollback: before=%d after=%d", len(convBefore), len(convAfter))
	}

	eventsAfter, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proofs after: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore)+2 {
		t.Fatalf("expected only stage-1 run start events to persist: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
	if !hasEvent(eventsAfter, proof.EventWorkerRunStarted) {
		t.Fatal("expected durable worker run started event from stage-1 commit")
	}
	if hasEvent(eventsAfter, proof.EventWorkerOutputCaptured) {
		t.Fatal("worker output captured should rollback when finalization transaction fails")
	}
	if hasEvent(eventsAfter, proof.EventWorkerRunCompleted) || hasEvent(eventsAfter, proof.EventWorkerRunFailed) {
		t.Fatal("terminal run events should not persist when finalization transaction fails")
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after failed finalization: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerBeforeExecution {
		t.Fatalf("expected before-execution checkpoint from prepare stage, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.RunID != runRec.RunID {
		t.Fatalf("expected checkpoint run id %s, got %s", runRec.RunID, latestCheckpoint.RunID)
	}
}

func TestRunRealSuccessCreatesAfterExecutionCheckpoint(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerAfterExecution {
		t.Fatalf("expected after-execution checkpoint, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.RunID != runRes.RunID {
		t.Fatalf("expected checkpoint run id %s, got %s", runRes.RunID, latestCheckpoint.RunID)
	}
	if !latestCheckpoint.IsResumable {
		t.Fatal("expected checkpoint to be resumable")
	}
}

func TestCreateCheckpointManual(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	if out.Trigger != checkpoint.TriggerManual {
		t.Fatalf("expected manual trigger, got %s", out.Trigger)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected checkpoint id")
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.CheckpointID != out.CheckpointID {
		t.Fatalf("expected latest checkpoint %s, got %s", out.CheckpointID, latestCheckpoint.CheckpointID)
	}
	if !hasEventMust(t, store, taskID, proof.EventCheckpointCreated) {
		t.Fatal("expected checkpoint created proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after checkpoint: %v", err)
	}
	if status.LatestCheckpointID != out.CheckpointID {
		t.Fatalf("status missing latest checkpoint id: expected %s got %s", out.CheckpointID, status.LatestCheckpointID)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect after checkpoint: %v", err)
	}
	if inspectOut.Checkpoint == nil || inspectOut.Checkpoint.CheckpointID != out.CheckpointID {
		t.Fatalf("inspect missing checkpoint: %+v", inspectOut.Checkpoint)
	}
}

func TestContinueReconcilesStaleRunningRun(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave stale running state")
	}
	beforeCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint before continue reconciliation: %v", err)
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeStaleReconciled {
		t.Fatalf("expected stale reconciliation outcome, got %s", out.Outcome)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected reconciliation checkpoint id")
	}

	runRec, err := store.Runs().Get(out.RunID)
	if err != nil {
		t.Fatalf("get reconciled run: %v", err)
	}
	if runRec.Status != rundomain.StatusInterrupted {
		t.Fatalf("expected run interrupted after reconciliation, got %s", runRec.Status)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhasePaused {
		t.Fatalf("expected paused phase after stale reconciliation, got %s", caps.CurrentPhase)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after reconciliation: %v", err)
	}
	if latestCheckpoint.CheckpointID == beforeCheckpoint.CheckpointID {
		t.Fatalf("expected new checkpoint for reconciliation, got same id %s", latestCheckpoint.CheckpointID)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerInterruption {
		t.Fatalf("expected interruption checkpoint after stale reconciliation, got %s", latestCheckpoint.Trigger)
	}
}

func TestContinueBlockedOnMajorDrift(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	driftAnchor := &staticAnchorProvider{
		snapshot: anchorgit.Snapshot{
			RepoRoot:         "/tmp/repo",
			Branch:           "feature/drift",
			HeadSHA:          "head-x",
			WorkingTreeDirty: false,
			CapturedAt:       time.Unix(1700005000, 0).UTC(),
		},
	}
	driftCoord := newTestCoordinator(t, store, driftAnchor, newFakeAdapterSuccess())
	out, err := driftCoord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with drift: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedDrift {
		t.Fatalf("expected blocked drift outcome, got %s", out.Outcome)
	}
	if out.DriftClass != checkpoint.DriftMajor {
		t.Fatalf("expected major drift class, got %s", out.DriftClass)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseAwaitingDecision {
		t.Fatalf("expected awaiting decision phase, got %s", caps.CurrentPhase)
	}
}

func TestContinueSafeFromCheckpoint(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("events before safe continue: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue safe: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe outcome, got %s", out.Outcome)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected continuation checkpoint")
	}
	if out.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected safe continue to reuse checkpoint %s, got %s", seed.CheckpointID, out.CheckpointID)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "safe resume") {
		t.Fatalf("expected canonical safe resume response, got %q", out.CanonicalResponse)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after safe continue: %v", err)
	}
	if latestCheckpoint.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected no new checkpoint to be created on safe continue")
	}
	eventsAfter, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("events after safe continue: %v", err)
	}
	if len(eventsAfter) <= len(eventsBefore) {
		t.Fatalf("expected durable proof records for no-op safe continue")
	}
	if !hasEvent(eventsAfter, proof.EventContinueAssessed) {
		t.Fatalf("expected continue-assessed proof event for no-op safe continue")
	}
}

func TestContinueInterruptedRunReportsRecoveryReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startRes.RunID,
		InterruptionReason: "phase 2 interrupted recovery test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume interrupted action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected interrupted recovery to be ready for next run")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "interrupted") {
		t.Fatalf("expected interrupted recovery canonical response, got %q", out.CanonicalResponse)
	}
}

func TestFailedRunRecoveryRequiresReviewNotNextRunReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	runOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if runOut.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed run status, got %s", runOut.RunStatus)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.IsResumable {
		t.Fatal("failed run checkpoint must not claim resumable recovery")
	}

	continueOut, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if continueOut.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run review recovery class, got %s", continueOut.RecoveryClass)
	}
	if continueOut.RecommendedAction != RecoveryActionInspectFailedRun {
		t.Fatalf("expected inspect failed run action, got %s", continueOut.RecommendedAction)
	}
	if continueOut.ReadyForNextRun {
		t.Fatal("failed run recovery must not be ready for next run")
	}
	if !strings.Contains(strings.ToLower(continueOut.CanonicalResponse), "not ready") {
		t.Fatalf("expected failed recovery canonical response to avoid ready claim, got %q", continueOut.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CheckpointResumable {
		t.Fatal("status should report failed checkpoint as non-resumable")
	}
	if status.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed recovery class in status, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not claim ready-for-next-run after failed execution")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected inspect failed recovery class, got %s", inspectOut.Recovery.RecoveryClass)
	}
	if inspectOut.Recovery.ReadyForNextRun {
		t.Fatal("inspect recovery must not claim ready-for-next-run after failed execution")
	}
}

func TestRecordRecoveryActionFailedRunReviewedPromotesDecisionRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
		Notes:  []string{"reviewed failure evidence"},
	})
	if err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if out.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected decision-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionMakeResumeDecision {
		t.Fatalf("expected make-resume-decision action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("failed-run review should not make the task ready yet")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("expected latest recovery action in status, got %+v", status.LatestRecoveryAction)
	}
	if status.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected status decision-required class, got %s", status.RecoveryClass)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("expected latest inspect recovery action, got %+v", inspectOut.LatestRecoveryAction)
	}
	if len(inspectOut.RecentRecoveryActions) != 1 {
		t.Fatalf("expected one persisted recovery action, got %d", len(inspectOut.RecentRecoveryActions))
	}
	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("expected recovery-action-recorded proof event")
	}
}

func TestRecordRecoveryActionDecisionContinueMakesTaskReadyNextRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("record decision continue: %v", err)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected ready-next-run recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionStartNextRun {
		t.Fatalf("expected start-next-run action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected ready-for-next-run after continue decision")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}
}

func TestRecordRecoveryActionDecisionRegenerateBriefRequiresRebrief(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	})
	if err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}
	if out.RecoveryClass != RecoveryClassRebriefRequired {
		t.Fatalf("expected rebrief-required class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionRegenerateBrief {
		t.Fatalf("expected regenerate-brief action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("regenerate-brief decision must not claim next-run readiness")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBlocked {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBlocked, caps.CurrentPhase)
	}
}

func TestRecordRecoveryActionIdempotentReplayReusesLatestAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindFailedRunReviewed,
		Summary: "reviewed failed run",
		Notes:   []string{"same-note"},
	})
	if err != nil {
		t.Fatalf("first record recovery action: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindFailedRunReviewed,
		Summary: "reviewed failed run",
		Notes:   []string{"same-note"},
	})
	if err != nil {
		t.Fatalf("second record recovery action: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected idempotent recovery action replay, got %s and %s", first.Action.ActionID, second.Action.ActionID)
	}
	actions, err := store.RecoveryActions().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list recovery actions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one persisted recovery action, got %d", len(actions))
	}
}

func TestRecordRecoveryActionDecisionContinueReplayReusesLatestAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("first decision continue: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("second decision continue: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected decision-continue replay to reuse latest action, got %s and %s", first.Action.ActionID, second.Action.ActionID)
	}
	if second.RecoveryClass != RecoveryClassReadyNextRun || !second.ReadyForNextRun {
		t.Fatalf("expected ready-next-run after decision continue replay, got %+v", second)
	}
	actions, err := store.RecoveryActions().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list recovery actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected exactly two persisted recovery actions (review + decision), got %d", len(actions))
	}
}

func TestRecordRecoveryActionRepairIntentPersistsWhileStillBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_broken_repair_intent",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff for repair intent test",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_repair_intent"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken repair handoff",
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create broken handoff: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindRepairIntentRecorded,
		Summary: "repair broken checkpoint reference",
	})
	if err != nil {
		t.Fatalf("record repair intent: %v", err)
	}
	if out.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required class, got %s", out.RecoveryClass)
	}
	if out.ReadyForNextRun {
		t.Fatal("repair intent must not claim next-run readiness")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindRepairIntentRecorded {
		t.Fatalf("expected repair intent action in inspect output, got %+v", inspectOut.LatestRecoveryAction)
	}
	if inspectOut.Recovery == nil || !strings.Contains(strings.ToLower(inspectOut.Recovery.Reason), "repair intent recorded") {
		t.Fatalf("expected recovery reason to reflect repair intent, got %+v", inspectOut.Recovery)
	}
}

func TestInspectTaskSurfacesRecoveryIssuesForBrokenHandoffState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_broken_inspect_recovery",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff for inspect recovery test",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_for_inspect_recovery"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken inspect handoff",
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
		t.Fatalf("create broken handoff packet: %v", err)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Handoff == nil || inspectOut.Handoff.HandoffID != packet.HandoffID {
		t.Fatalf("expected inspect handoff %s, got %+v", packet.HandoffID, inspectOut.Handoff)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery class, got %s", inspectOut.Recovery.RecoveryClass)
	}
	if len(inspectOut.Recovery.Issues) == 0 {
		t.Fatal("expected inspect recovery issues for broken handoff state")
	}
	foundCheckpointIssue := false
	for _, issue := range inspectOut.Recovery.Issues {
		if strings.Contains(strings.ToLower(issue.Message), "missing checkpoint") {
			foundCheckpointIssue = true
			break
		}
	}
	if !foundCheckpointIssue {
		t.Fatalf("expected missing-checkpoint issue, got %+v", inspectOut.Recovery.Issues)
	}
}

func TestContinueBlockedWhenBriefMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	start, err := coord.StartTask(context.Background(), "No brief continue", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(start.TaskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "inconsistent") {
		t.Fatalf("expected canonical inconsistent response, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenCheckpointBriefMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_missing_brief"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_checkpoint"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with bad checkpoint brief: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "missing brief") {
		t.Fatalf("expected canonical missing-brief message, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenCheckpointRunMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_missing_run"),
		TaskID:             taskID,
		RunID:              common.RunID("run_missing_for_checkpoint"),
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for missing run test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with bad checkpoint run: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "missing run") {
		t.Fatalf("expected canonical missing-run message, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenRunningCheckpointLinkageBroken(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave RUNNING state")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_running_linkage"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(10 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "broken checkpoint linkage for test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "inconsistent") {
		t.Fatalf("expected inconsistent canonical response, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenLatestHandoffCheckpointMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}

	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_missing_checkpoint_for_continue",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff state",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_for_handoff"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken handoff packet for continue validation",
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
		t.Fatalf("create broken handoff packet: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "handoff") {
		t.Fatalf("expected handoff-related inconsistency, got %q", out.CanonicalResponse)
	}
}

func TestContinueSafeAssessmentDoesNotRequireWriteTransaction(t *testing.T) {
	base := newTestStore(t)
	baseCoord := newTestCoordinator(t, base, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, baseCoord)
	seed, err := baseCoord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	counting := &txCountingStore{base: base}
	coord, err := NewCoordinator(Dependencies{
		Store:          counting,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe outcome, got %s", out.Outcome)
	}
	if out.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected checkpoint reuse %s, got %s", seed.CheckpointID, out.CheckpointID)
	}
	if counting.withTxCount < 1 {
		t.Fatalf("expected lightweight durable write path for no-op safe continue")
	}
}

func TestContinueSafeReuseDoesNotCreateCheckpointChurn(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	first, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("first continue: %v", err)
	}
	second, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("second continue: %v", err)
	}
	if first.CheckpointID != seed.CheckpointID || second.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected checkpoint reuse across continues, got first=%s second=%s seed=%s", first.CheckpointID, second.CheckpointID, seed.CheckpointID)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected no checkpoint churn, latest=%s seed=%s", latestCheckpoint.CheckpointID, seed.CheckpointID)
	}
}

func TestSafeContinueCreatesCheckpointWithContinueTrigger(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe continue, got %s", out.Outcome)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerContinue {
		t.Fatalf("expected continue trigger, got %s", latestCheckpoint.Trigger)
	}
}

func setupTaskWithBrief(t *testing.T, coord *Coordinator) common.TaskID {
	t.Helper()
	start, err := coord.StartTask(context.Background(), "Run lifecycle test", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "start implementation process"); err != nil {
		t.Fatalf("message task: %v", err)
	}
	return start.TaskID
}

func hasEvent(events []proof.Event, typ proof.EventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func countEvents(events []proof.Event, typ proof.EventType) int {
	count := 0
	for _, e := range events {
		if e.Type == typ {
			count++
		}
	}
	return count
}

func hasEventMust(t *testing.T, store storage.Store, taskID common.TaskID, typ proof.EventType) bool {
	t.Helper()
	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	return hasEvent(events, typ)
}

func latestEventID(store storage.Store, taskID common.TaskID) (common.EventID, error) {
	events, err := store.Proofs().ListByTask(taskID, 1)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	return events[len(events)-1].EventID, nil
}

func newTestCoordinator(t *testing.T, store *sqlite.Store, anchorProvider anchorgit.Provider, adapter adapter_contract.WorkerAdapter) *Coordinator {
	t.Helper()
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  adapter,
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorProvider,
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	return coord
}

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tuku-test.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type staticAnchorProvider struct {
	snapshot anchorgit.Snapshot
}

func (p *staticAnchorProvider) Capture(_ context.Context, repoRoot string) anchorgit.Snapshot {
	out := p.snapshot
	if out.RepoRoot == "" {
		out.RepoRoot = repoRoot
	}
	if out.CapturedAt.IsZero() {
		out.CapturedAt = time.Now().UTC()
	}
	return out
}

func defaultAnchor() anchorgit.Provider {
	return &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "head-x", WorkingTreeDirty: false, CapturedAt: time.Unix(1700004000, 0).UTC()}}
}

type fakeWorkerAdapter struct {
	kind      adapter_contract.WorkerKind
	result    adapter_contract.ExecutionResult
	err       error
	called    bool
	lastReq   adapter_contract.ExecutionRequest
	onExecute func(req adapter_contract.ExecutionRequest)
}

func newFakeAdapterSuccess() *fakeWorkerAdapter {
	now := time.Now().UTC()
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode:          0,
			StartedAt:         now,
			EndedAt:           now.Add(200 * time.Millisecond),
			Stdout:            "implemented bounded step",
			Stderr:            "",
			ChangedFiles:      []string{"internal/orchestrator/service.go"},
			ValidationSignals: []string{"worker mentioned test activity"},
			Summary:           "bounded codex step complete",
		},
	}
}

func newFakeAdapterExitFailure() *fakeWorkerAdapter {
	now := time.Now().UTC()
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode:  1,
			StartedAt: now,
			EndedAt:   now.Add(100 * time.Millisecond),
			Stdout:    "attempted change",
			Stderr:    "test failed",
			Summary:   "run failed",
		},
	}
}

func newFakeAdapterError(err error) *fakeWorkerAdapter {
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode: -1,
			Summary:  "adapter error",
		},
		err: err,
	}
}

func (f *fakeWorkerAdapter) Name() adapter_contract.WorkerKind {
	return f.kind
}

func (f *fakeWorkerAdapter) Execute(_ context.Context, req adapter_contract.ExecutionRequest, _ adapter_contract.WorkerEventSink) (adapter_contract.ExecutionResult, error) {
	f.called = true
	f.lastReq = req
	if f.onExecute != nil {
		f.onExecute(req)
	}
	out := f.result
	if out.WorkerRunID == "" {
		out.WorkerRunID = common.WorkerRunID("wrk_" + string(req.RunID))
	}
	if out.Command == "" {
		out.Command = "codex"
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now().UTC()
	}
	if out.EndedAt.IsZero() {
		out.EndedAt = out.StartedAt.Add(100 * time.Millisecond)
	}
	return out, f.err
}

var _ adapter_contract.WorkerAdapter = (*fakeWorkerAdapter)(nil)

type failingSynthesizer struct {
	err error
}

func (s *failingSynthesizer) Synthesize(_ context.Context, _ capsule.WorkCapsule, _ []proof.Event) (string, error) {
	return "", s.err
}

type faultInjectedStore struct {
	base            storage.Store
	failProofAppend bool
}

func (s *faultInjectedStore) Capsules() storage.CapsuleStore {
	return s.base.Capsules()
}

func (s *faultInjectedStore) Conversations() storage.ConversationStore {
	return s.base.Conversations()
}

func (s *faultInjectedStore) Intents() storage.IntentStore {
	return s.base.Intents()
}

func (s *faultInjectedStore) Briefs() storage.BriefStore {
	return s.base.Briefs()
}

func (s *faultInjectedStore) Proofs() storage.ProofStore {
	if !s.failProofAppend {
		return s.base.Proofs()
	}
	return &faultProofStore{base: s.base.Proofs()}
}

func (s *faultInjectedStore) Runs() storage.RunStore {
	return s.base.Runs()
}

func (s *faultInjectedStore) Checkpoints() storage.CheckpointStore {
	return s.base.Checkpoints()
}

func (s *faultInjectedStore) RecoveryActions() storage.RecoveryActionStore {
	return s.base.RecoveryActions()
}

func (s *faultInjectedStore) ContextPacks() storage.ContextPackStore {
	return s.base.ContextPacks()
}

func (s *faultInjectedStore) PolicyDecisions() storage.PolicyDecisionStore {
	return s.base.PolicyDecisions()
}

func (s *faultInjectedStore) WithTx(fn func(storage.Store) error) error {
	return s.base.WithTx(func(txStore storage.Store) error {
		wrapped := &faultInjectedStore{
			base:            txStore,
			failProofAppend: s.failProofAppend,
		}
		return fn(wrapped)
	})
}

type txCountingStore struct {
	base        storage.Store
	withTxCount int
}

func (s *txCountingStore) Capsules() storage.CapsuleStore {
	return s.base.Capsules()
}

func (s *txCountingStore) Conversations() storage.ConversationStore {
	return s.base.Conversations()
}

func (s *txCountingStore) Intents() storage.IntentStore {
	return s.base.Intents()
}

func (s *txCountingStore) Briefs() storage.BriefStore {
	return s.base.Briefs()
}

func (s *txCountingStore) Proofs() storage.ProofStore {
	return s.base.Proofs()
}

func (s *txCountingStore) Runs() storage.RunStore {
	return s.base.Runs()
}

func (s *txCountingStore) Checkpoints() storage.CheckpointStore {
	return s.base.Checkpoints()
}

func (s *txCountingStore) RecoveryActions() storage.RecoveryActionStore {
	return s.base.RecoveryActions()
}

func (s *txCountingStore) ContextPacks() storage.ContextPackStore {
	return s.base.ContextPacks()
}

func (s *txCountingStore) PolicyDecisions() storage.PolicyDecisionStore {
	return s.base.PolicyDecisions()
}

func (s *txCountingStore) WithTx(fn func(storage.Store) error) error {
	s.withTxCount++
	return s.base.WithTx(fn)
}

type faultProofStore struct {
	base storage.ProofStore
}

func (s *faultProofStore) Append(event proof.Event) error {
	return errors.New("forced proof append failure")
}

func (s *faultProofStore) ListByTask(taskID common.TaskID, limit int) ([]proof.Event, error) {
	return s.base.ListByTask(taskID, limit)
}

```
