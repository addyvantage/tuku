## internal/domain/operatorstep/types.go
```go
package operatorstep

import (
	"time"

	"tuku/internal/domain/common"
)

type ResultClass string

const (
	ResultSucceeded  ResultClass = "SUCCEEDED"
	ResultRejected   ResultClass = "REJECTED"
	ResultFailed     ResultClass = "FAILED"
	ResultNoopReused ResultClass = "NOOP_REUSED"
)

type Receipt struct {
	Version int `json:"version"`

	ReceiptID          string              `json:"receipt_id"`
	TaskID             common.TaskID       `json:"task_id"`
	ActionHandle       string              `json:"action_handle"`
	ExecutionDomain    string              `json:"execution_domain,omitempty"`
	CommandSurfaceKind string              `json:"command_surface_kind,omitempty"`
	ExecutionAttempted bool                `json:"execution_attempted"`
	ResultClass        ResultClass         `json:"result_class"`
	Summary            string              `json:"summary,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	RunID              common.RunID        `json:"run_id,omitempty"`
	CheckpointID       common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID            common.BriefID      `json:"brief_id,omitempty"`
	HandoffID          string              `json:"handoff_id,omitempty"`
	LaunchAttemptID    string              `json:"launch_attempt_id,omitempty"`
	LaunchID           string              `json:"launch_id,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	CompletedAt        *time.Time          `json:"completed_at,omitempty"`
}
```

## internal/storage/contracts.go
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
	"tuku/internal/domain/operatorstep"
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
	ListByTask(taskID common.TaskID, limit int) ([]handoff.Packet, error)
	UpdateStatus(taskID common.TaskID, handoffID string, status handoff.Status, acceptedBy run.WorkerKind, notes []string, at time.Time) error
	CreateLaunch(launch handoff.Launch) error
	GetLaunch(attemptID string) (handoff.Launch, error)
	LatestLaunchByHandoff(handoffID string) (handoff.Launch, error)
	UpdateLaunch(launch handoff.Launch) error
	SaveAcknowledgment(ack handoff.Acknowledgment) error
	LatestAcknowledgment(handoffID string) (handoff.Acknowledgment, error)
	SaveFollowThrough(record handoff.FollowThrough) error
	LatestFollowThrough(handoffID string) (handoff.FollowThrough, error)
	SaveResolution(record handoff.Resolution) error
	LatestResolution(handoffID string) (handoff.Resolution, error)
	LatestResolutionByTask(taskID common.TaskID) (handoff.Resolution, error)
}

type RecoveryActionStore interface {
	Create(record recoveryaction.Record) error
	LatestByTask(taskID common.TaskID) (recoveryaction.Record, error)
	ListByTask(taskID common.TaskID, limit int) ([]recoveryaction.Record, error)
}

type OperatorStepReceiptStore interface {
	Create(record operatorstep.Receipt) error
	LatestByTask(taskID common.TaskID) (operatorstep.Receipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]operatorstep.Receipt, error)
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
	OperatorStepReceipts() OperatorStepReceiptStore
	ContextPacks() ContextPackStore
	PolicyDecisions() PolicyDecisionStore
	WithTx(fn func(Store) error) error
}
```

## internal/storage/sqlite/store.go
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

func (s *Store) OperatorStepReceipts() storage.OperatorStepReceiptStore {
	return &operatorStepReceiptRepo{q: s.db}
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

func (s *txStore) OperatorStepReceipts() storage.OperatorStepReceiptStore {
	return &operatorStepReceiptRepo{q: s.tx}
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
	if err := ensureOperatorStepReceiptSchema(db); err != nil {
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

## internal/storage/sqlite/operator_step_receipt_repo.go
```go
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"tuku/internal/domain/common"
	"tuku/internal/domain/operatorstep"
)

type operatorStepReceiptRepo struct{ q queryable }

func ensureOperatorStepReceiptSchema(q queryable) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS operator_step_receipts (
	receipt_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	action_handle TEXT NOT NULL,
	result_class TEXT NOT NULL,
	created_at TEXT NOT NULL,
	record_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_operator_step_receipts_task_created
	ON operator_step_receipts(task_id, created_at DESC, receipt_id DESC);
`
	if _, err := q.Exec(ddl); err != nil {
		return fmt.Errorf("ensure operator step receipts table: %w", err)
	}
	return nil
}

func (r *operatorStepReceiptRepo) Create(record operatorstep.Receipt) error {
	if err := ensureOperatorStepReceiptSchema(r.q); err != nil {
		return err
	}
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO operator_step_receipts(
	receipt_id, task_id, action_handle, result_class, created_at, record_json
) VALUES(?,?,?,?,?,?)
`,
		record.ReceiptID,
		string(record.TaskID),
		record.ActionHandle,
		string(record.ResultClass),
		record.CreatedAt.Format(sqliteTimestampLayout),
		string(recordJSON),
	)
	if err != nil {
		return fmt.Errorf("insert operator step receipt: %w", err)
	}
	return nil
}

func (r *operatorStepReceiptRepo) LatestByTask(taskID common.TaskID) (operatorstep.Receipt, error) {
	if err := ensureOperatorStepReceiptSchema(r.q); err != nil {
		return operatorstep.Receipt{}, err
	}
	row := r.q.QueryRow(`
SELECT record_json
FROM operator_step_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT 1
`, string(taskID))
	return scanOperatorStepReceipt(row)
}

func (r *operatorStepReceiptRepo) ListByTask(taskID common.TaskID, limit int) ([]operatorstep.Receipt, error) {
	if err := ensureOperatorStepReceiptSchema(r.q); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.q.Query(`
SELECT record_json
FROM operator_step_receipts
WHERE task_id = ?
ORDER BY created_at DESC, receipt_id DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, fmt.Errorf("query operator step receipts: %w", err)
	}
	defer rows.Close()

	out := make([]operatorstep.Receipt, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var record operatorstep.Receipt
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operator step receipts: %w", err)
	}
	return out, nil
}

func scanOperatorStepReceipt(row *sql.Row) (operatorstep.Receipt, error) {
	var raw string
	if err := row.Scan(&raw); err != nil {
		return operatorstep.Receipt{}, err
	}
	var record operatorstep.Receipt
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return operatorstep.Receipt{}, err
	}
	return record, nil
}
```

## internal/domain/proof/types.go
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
	EventHandoffFollowThroughRecorded     EventType = "HANDOFF_FOLLOW_THROUGH_RECORDED"
	EventHandoffResolutionRecorded        EventType = "HANDOFF_RESOLUTION_RECORDED"
	EventOperatorStepExecutionRecorded    EventType = "OPERATOR_STEP_EXECUTION_RECORDED"
	EventRecoveryActionRecorded           EventType = "RECOVERY_ACTION_RECORDED"
	EventInterruptedRunReviewed           EventType = "INTERRUPTED_RUN_REVIEWED"
	EventInterruptedRunResumeExecuted     EventType = "INTERRUPTED_RUN_RESUME_EXECUTED"
	EventRecoveryContinueExecuted         EventType = "RECOVERY_CONTINUE_EXECUTED"
	EventBriefRegenerated                 EventType = "BRIEF_REGENERATED"
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

## internal/orchestrator/orchestrator.go
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
	RecordHandoffFollowThrough(ctx context.Context, req RecordHandoffFollowThroughRequest) (RecordHandoffFollowThroughResult, error)
	RecordHandoffResolution(ctx context.Context, req RecordHandoffResolutionRequest) (RecordHandoffResolutionResult, error)
	RecordRecoveryAction(ctx context.Context, req RecordRecoveryActionRequest) (RecordRecoveryActionResult, error)
	ExecuteRebrief(ctx context.Context, req ExecuteRebriefRequest) (ExecuteRebriefResult, error)
	ExecuteInterruptedResume(ctx context.Context, req ExecuteInterruptedResumeRequest) (ExecuteInterruptedResumeResult, error)
	ExecuteContinueRecovery(ctx context.Context, req ExecuteContinueRecoveryRequest) (ExecuteContinueRecoveryResult, error)
	ExecutePrimaryOperatorStep(ctx context.Context, req ExecutePrimaryOperatorStepRequest) (ExecutePrimaryOperatorStepResult, error)
	StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error)
	InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error)
	ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error)
	RecordShellLifecycle(ctx context.Context, req RecordShellLifecycleRequest) (RecordShellLifecycleResult, error)
	ReportShellSession(ctx context.Context, req ReportShellSessionRequest) (ReportShellSessionResult, error)
	ListShellSessions(ctx context.Context, taskID string) (ListShellSessionsResult, error)
}
```

## internal/orchestrator/operator_step_execution.go
```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/proof"
)

type ExecutePrimaryOperatorStepRequest struct {
	TaskID string
}

type ExecutePrimaryOperatorStepResult struct {
	TaskID                     common.TaskID
	Receipt                    operatorstep.Receipt
	ActiveBranch               ActiveBranchProvenance
	OperatorDecision           OperatorDecisionSummary
	OperatorExecutionPlan      OperatorExecutionPlan
	RecoveryClass              RecoveryClass
	RecommendedAction          RecoveryAction
	ReadyForNextRun            bool
	ReadyForHandoffLaunch      bool
	RecoveryReason             string
	CanonicalResponse          string
	RecentOperatorStepReceipts []operatorstep.Receipt
}

type operatorStepExecutionDispatch struct {
	attempted         bool
	resultClass       operatorstep.ResultClass
	summary           string
	reason            string
	canonicalResponse string
	runID             common.RunID
	checkpointID      common.CheckpointID
	briefID           common.BriefID
	handoffID         string
	launchAttemptID   string
	launchID          string
}

func (c *Coordinator) ExecutePrimaryOperatorStep(ctx context.Context, req ExecutePrimaryOperatorStepRequest) (ExecutePrimaryOperatorStepResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ExecutePrimaryOperatorStepResult{}, fmt.Errorf("task id is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	_, _, _, _, plan, continuity, _, _ := c.operatorTruthForAssessment(assessment)
	if plan.PrimaryStep == nil {
		receipt, recErr := c.recordOperatorStepReceipt(ctx, taskID, plan, continuity, operatorStepExecutionDispatch{
			attempted:   false,
			resultClass: operatorstep.ResultRejected,
			summary:     "primary operator step is unavailable",
			reason:      "no primary operator step is currently available",
		}, nil)
		if recErr != nil {
			return ExecutePrimaryOperatorStepResult{}, recErr
		}
		fresh, err := c.buildPrimaryOperatorStepExecutionResult(ctx, taskID, receipt, "")
		if err != nil {
			return ExecutePrimaryOperatorStepResult{}, err
		}
		return fresh, nil
	}
	step := *plan.PrimaryStep
	if step.CommandSurface != OperatorCommandSurfaceDedicated {
		reason := fmt.Sprintf("primary operator step %s is guidance-only and cannot be executed directly", step.Action)
		receipt, recErr := c.recordOperatorStepReceipt(ctx, taskID, plan, continuity, operatorStepExecutionDispatch{
			attempted:   false,
			resultClass: operatorstep.ResultRejected,
			summary:     fmt.Sprintf("rejected %s", stepExecutionLabel(step.Action)),
			reason:      reason,
		}, &step)
		if recErr != nil {
			return ExecutePrimaryOperatorStepResult{}, recErr
		}
		fresh, err := c.buildPrimaryOperatorStepExecutionResult(ctx, taskID, receipt, "")
		if err != nil {
			return ExecutePrimaryOperatorStepResult{}, err
		}
		return fresh, nil
	}

	dispatch := c.dispatchPrimaryOperatorStep(ctx, taskID, step, continuity)
	receipt, err := c.recordOperatorStepReceipt(ctx, taskID, plan, continuity, dispatch, &step)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	return c.buildPrimaryOperatorStepExecutionResult(ctx, taskID, receipt, dispatch.canonicalResponse)
}

func (c *Coordinator) dispatchPrimaryOperatorStep(ctx context.Context, taskID common.TaskID, step OperatorExecutionStep, continuity HandoffContinuity) operatorStepExecutionDispatch {
	switch step.Action {
	case OperatorActionStartLocalRun:
		out, err := c.RunTask(ctx, RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("started local run %s", out.RunID),
			canonicalResponse: out.CanonicalResponse,
			runID:             out.RunID,
		}
	case OperatorActionResumeInterruptedLineage:
		out, err := c.ExecuteInterruptedResume(ctx, ExecuteInterruptedResumeRequest{TaskID: string(taskID)})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("resumed interrupted lineage for brief %s", out.BriefID),
			canonicalResponse: out.CanonicalResponse,
			runID:             out.Action.RunID,
			checkpointID:      out.Action.CheckpointID,
			briefID:           out.BriefID,
		}
	case OperatorActionFinalizeContinueRecovery:
		out, err := c.ExecuteContinueRecovery(ctx, ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("finalized continue recovery for brief %s", out.BriefID),
			canonicalResponse: out.CanonicalResponse,
			runID:             out.Action.RunID,
			checkpointID:      out.Action.CheckpointID,
			briefID:           out.BriefID,
		}
	case OperatorActionExecuteRebrief:
		out, err := c.ExecuteRebrief(ctx, ExecuteRebriefRequest{TaskID: string(taskID)})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("regenerated brief %s", out.BriefID),
			canonicalResponse: out.CanonicalResponse,
			briefID:           out.BriefID,
		}
	case OperatorActionLaunchAcceptedHandoff:
		out, err := c.LaunchHandoff(ctx, LaunchHandoffRequest{TaskID: string(taskID), HandoffID: continuity.HandoffID})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		dispatch := launchDispatchFromResult(continuity, out)
		dispatch.attempted = true
		return dispatch
	case OperatorActionResolveActiveHandoff:
		out, err := c.RecordHandoffResolution(ctx, RecordHandoffResolutionRequest{
			TaskID:    string(taskID),
			HandoffID: continuity.HandoffID,
			Kind:      handoff.ResolutionSupersededByLocal,
			Summary:   "operator-next returned canonical local control",
		})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("resolved active handoff %s as %s", out.Record.HandoffID, out.Record.Kind),
			canonicalResponse: out.CanonicalResponse,
			handoffID:         out.Record.HandoffID,
			launchAttemptID:   out.Record.LaunchAttemptID,
			launchID:          out.Record.LaunchID,
		}
	default:
		return operatorStepExecutionDispatch{
			attempted:   false,
			resultClass: operatorstep.ResultRejected,
			summary:     fmt.Sprintf("rejected %s", stepExecutionLabel(step.Action)),
			reason:      fmt.Sprintf("primary operator step %s does not have a dedicated unified backend execution path", step.Action),
		}
	}
}

func launchDispatchFromResult(continuity HandoffContinuity, out LaunchHandoffResult) operatorStepExecutionDispatch {
	resultClass := operatorstep.ResultSucceeded
	summary := fmt.Sprintf("launched accepted handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
	reason := ""
	switch out.LaunchStatus {
	case HandoffLaunchStatusBlocked:
		resultClass = operatorstep.ResultRejected
		summary = fmt.Sprintf("rejected launch of accepted handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
		reason = strings.TrimSpace(out.CanonicalResponse)
	case HandoffLaunchStatusFailed:
		resultClass = operatorstep.ResultFailed
		summary = fmt.Sprintf("failed launch of accepted handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
		reason = strings.TrimSpace(out.CanonicalResponse)
	case HandoffLaunchStatusCompleted:
		if continuity.LaunchID != "" && continuity.LaunchID == out.LaunchID {
			resultClass = operatorstep.ResultNoopReused
			summary = fmt.Sprintf("reused durable launch result for handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
		}
	}
	return operatorStepExecutionDispatch{
		resultClass:       resultClass,
		summary:           summary,
		reason:            reason,
		canonicalResponse: out.CanonicalResponse,
		handoffID:         nonEmpty(out.HandoffID, continuity.HandoffID),
		launchID:          out.LaunchID,
	}
}

func classifyOperatorStepError(step OperatorExecutionStep, err error) operatorStepExecutionDispatch {
	reason := strings.TrimSpace(err.Error())
	class := operatorstep.ResultFailed
	lower := strings.ToLower(reason)
	for _, token := range []string{"already", "blocked", "cannot", "requires", "not ", "missing", "unsupported", "mismatch", "guidance-only", "no active", "no primary", "only be executed", "rejected"} {
		if strings.Contains(lower, token) {
			class = operatorstep.ResultRejected
			break
		}
	}
	summary := fmt.Sprintf("failed %s", stepExecutionLabel(step.Action))
	if class == operatorstep.ResultRejected {
		summary = fmt.Sprintf("rejected %s", stepExecutionLabel(step.Action))
	}
	return operatorStepExecutionDispatch{
		attempted:   true,
		resultClass: class,
		summary:     summary,
		reason:      reason,
	}
}

func stepExecutionLabel(action OperatorAction) string {
	return strings.ToLower(strings.TrimSpace(string(action)))
}

func (c *Coordinator) recordOperatorStepReceipt(_ context.Context, taskID common.TaskID, plan OperatorExecutionPlan, continuity HandoffContinuity, dispatch operatorStepExecutionDispatch, step *OperatorExecutionStep) (operatorstep.Receipt, error) {
	now := c.clock()
	receipt := operatorstep.Receipt{
		Version:            1,
		ReceiptID:          c.idGenerator("orec"),
		TaskID:             taskID,
		ExecutionAttempted: dispatch.attempted,
		ResultClass:        dispatch.resultClass,
		Summary:            strings.TrimSpace(dispatch.summary),
		Reason:             strings.TrimSpace(dispatch.reason),
		RunID:              dispatch.runID,
		CheckpointID:       dispatch.checkpointID,
		BriefID:            dispatch.briefID,
		HandoffID:          dispatch.handoffID,
		LaunchAttemptID:    dispatch.launchAttemptID,
		LaunchID:           dispatch.launchID,
		CreatedAt:          now,
	}
	if step != nil {
		receipt.ActionHandle = string(step.Action)
		receipt.ExecutionDomain = string(step.Domain)
		receipt.CommandSurfaceKind = string(step.CommandSurface)
	} else if plan.PrimaryStep != nil {
		receipt.ActionHandle = string(plan.PrimaryStep.Action)
		receipt.ExecutionDomain = string(plan.PrimaryStep.Domain)
		receipt.CommandSurfaceKind = string(plan.PrimaryStep.CommandSurface)
	}
	completedAt := now
	receipt.CompletedAt = &completedAt
	if receipt.HandoffID == "" {
		receipt.HandoffID = continuity.HandoffID
	}
	if receipt.LaunchAttemptID == "" {
		receipt.LaunchAttemptID = continuity.LaunchAttemptID
	}
	if receipt.LaunchID == "" {
		receipt.LaunchID = continuity.LaunchID
	}

	err := c.withTx(func(txc *Coordinator) error {
		if err := txc.store.OperatorStepReceipts().Create(receipt); err != nil {
			return err
		}
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"receipt_id":           receipt.ReceiptID,
			"action_handle":        receipt.ActionHandle,
			"execution_domain":     receipt.ExecutionDomain,
			"command_surface_kind": receipt.CommandSurfaceKind,
			"execution_attempted":  receipt.ExecutionAttempted,
			"result_class":         receipt.ResultClass,
			"summary":              receipt.Summary,
			"reason":               receipt.Reason,
			"handoff_id":           receipt.HandoffID,
			"launch_attempt_id":    receipt.LaunchAttemptID,
			"launch_id":            receipt.LaunchID,
			"brief_id":             receipt.BriefID,
			"checkpoint_id":        receipt.CheckpointID,
			"run_id":               receipt.RunID,
		}
		return txc.appendProof(caps, proof.EventOperatorStepExecutionRecorded, proof.ActorUser, "user", payload, runIDPointer(receipt.RunID))
	})
	if err != nil {
		return operatorstep.Receipt{}, err
	}
	return receipt, nil
}

func (c *Coordinator) buildPrimaryOperatorStepExecutionResult(ctx context.Context, taskID common.TaskID, receipt operatorstep.Receipt, canonicalResponse string) (ExecutePrimaryOperatorStepResult, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	recovery, branch, _, decision, plan, _, _, _ := c.operatorTruthForAssessment(assessment)
	recent, err := c.store.OperatorStepReceipts().ListByTask(taskID, 5)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	return ExecutePrimaryOperatorStepResult{
		TaskID:                     taskID,
		Receipt:                    receipt,
		ActiveBranch:               branch,
		OperatorDecision:           decision,
		OperatorExecutionPlan:      plan,
		RecoveryClass:              recovery.RecoveryClass,
		RecommendedAction:          recovery.RecommendedAction,
		ReadyForNextRun:            recovery.ReadyForNextRun,
		ReadyForHandoffLaunch:      recovery.ReadyForHandoffLaunch,
		RecoveryReason:             recovery.Reason,
		CanonicalResponse:          canonicalResponse,
		RecentOperatorStepReceipts: append([]operatorstep.Receipt{}, recent...),
	}, nil
}

func (c *Coordinator) operatorTruthForAssessment(assessment continueAssessment) (RecoveryAssessment, ActiveBranchProvenance, OperatorActionAuthoritySet, OperatorDecisionSummary, OperatorExecutionPlan, HandoffContinuity, LocalRunFinalization, LocalResumeAuthority) {
	recovery := c.recoveryFromContinueAssessment(assessment)
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	runFinalization := deriveLocalRunFinalization(assessment, recovery)
	localResume := deriveLocalResumeAuthority(assessment, recovery)
	actions := deriveOperatorActionAuthoritySet(assessment, recovery, branch, runFinalization, localResume)
	decision := deriveOperatorDecisionSummary(assessment, recovery, branch, runFinalization, localResume, actions)
	plan := deriveOperatorExecutionPlan(assessment, branch, actions, decision)
	continuity := assessHandoffContinuity(assessment.TaskID, assessment.LatestHandoff, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough, assessment.LatestResolution)
	return recovery, branch, actions, decision, plan, continuity, runFinalization, localResume
}
```

## internal/orchestrator/service.go
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
	"tuku/internal/domain/operatorstep"
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
	TaskID                           common.TaskID
	ConversationID                   common.ConversationID
	Goal                             string
	Phase                            phase.Phase
	Status                           string
	CurrentIntentID                  common.IntentID
	CurrentIntentClass               intent.Class
	CurrentIntentSummary             string
	CurrentBriefID                   common.BriefID
	CurrentBriefHash                 string
	LatestRunID                      common.RunID
	LatestRunStatus                  rundomain.Status
	LatestRunSummary                 string
	RepoAnchor                       anchorgit.Snapshot
	LatestCheckpointID               common.CheckpointID
	LatestCheckpointAt               time.Time
	LatestCheckpointTrigger          checkpoint.Trigger
	CheckpointResumable              bool
	ResumeDescriptor                 string
	LatestLaunchAttemptID            string
	LatestLaunchID                   string
	LatestLaunchStatus               handoff.LaunchStatus
	LatestAcknowledgmentID           string
	LatestAcknowledgmentStatus       handoff.AcknowledgmentStatus
	LatestAcknowledgmentSummary      string
	LatestFollowThroughID            string
	LatestFollowThroughKind          handoff.FollowThroughKind
	LatestFollowThroughSummary       string
	LatestResolutionID               string
	LatestResolutionKind             handoff.ResolutionKind
	LatestResolutionSummary          string
	LatestResolutionAt               time.Time
	LaunchControlState               LaunchControlState
	LaunchRetryDisposition           LaunchRetryDisposition
	LaunchControlReason              string
	HandoffContinuityState           HandoffContinuityState
	HandoffContinuityReason          string
	HandoffContinuationProven        bool
	ActiveBranchClass                ActiveBranchClass
	ActiveBranchRef                  string
	ActiveBranchAnchorKind           ActiveBranchAnchorKind
	ActiveBranchAnchorRef            string
	ActiveBranchReason               string
	LocalRunFinalizationState        LocalRunFinalizationState
	LocalRunFinalizationRunID        common.RunID
	LocalRunFinalizationStatus       rundomain.Status
	LocalRunFinalizationCheckpointID common.CheckpointID
	LocalRunFinalizationReason       string
	LocalResumeAuthorityState        LocalResumeAuthorityState
	LocalResumeMode                  LocalResumeMode
	LocalResumeCheckpointID          common.CheckpointID
	LocalResumeRunID                 common.RunID
	LocalResumeReason                string
	RequiredNextOperatorAction       OperatorAction
	ActionAuthority                  []OperatorActionAuthority
	OperatorDecision                 *OperatorDecisionSummary
	OperatorExecutionPlan            *OperatorExecutionPlan
	LatestOperatorStepReceipt        *operatorstep.Receipt
	RecentOperatorStepReceipts       []operatorstep.Receipt
	IsResumable                      bool
	RecoveryClass                    RecoveryClass
	RecommendedAction                RecoveryAction
	ReadyForNextRun                  bool
	ReadyForHandoffLaunch            bool
	RecoveryReason                   string
	LatestRecoveryAction             *recoveryaction.Record
	LastEventID                      common.EventID
	LastEventType                    proof.EventType
	LastEventAt                      time.Time
}

type InspectTaskResult struct {
	TaskID                     common.TaskID
	Intent                     *intent.State
	Brief                      *brief.ExecutionBrief
	Run                        *rundomain.ExecutionRun
	Checkpoint                 *checkpoint.Checkpoint
	Handoff                    *handoff.Packet
	Launch                     *handoff.Launch
	Acknowledgment             *handoff.Acknowledgment
	FollowThrough              *handoff.FollowThrough
	Resolution                 *handoff.Resolution
	ActiveBranch               *ActiveBranchProvenance
	LocalRunFinalization       *LocalRunFinalization
	LocalResumeAuthority       *LocalResumeAuthority
	ActionAuthority            *OperatorActionAuthoritySet
	OperatorDecision           *OperatorDecisionSummary
	OperatorExecutionPlan      *OperatorExecutionPlan
	LatestOperatorStepReceipt  *operatorstep.Receipt
	RecentOperatorStepReceipts []operatorstep.Receipt
	LaunchControl              *LaunchControl
	HandoffContinuity          *HandoffContinuity
	Recovery                   *RecoveryAssessment
	LatestRecoveryAction       *recoveryaction.Record
	RecentRecoveryActions      []recoveryaction.Record
	RepoAnchor                 anchorgit.Snapshot
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
	if blocked, err := c.localMutationBlockedByClaudeHandoff(ctx, common.TaskID(taskID), "compile a new local execution brief"); err != nil {
		return MessageTaskResult{}, err
	} else if blocked != "" {
		return MessageTaskResult{}, fmt.Errorf(blocked)
	}
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
	if blocked, err := c.localMutationBlockedByClaudeHandoff(ctx, common.TaskID(taskID), "capture a new local checkpoint"); err != nil {
		return CreateCheckpointResult{}, err
	} else if blocked != "" {
		return CreateCheckpointResult{}, fmt.Errorf(blocked)
	}
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
	LatestFollowThrough  *handoff.FollowThrough
	LatestResolution     *handoff.Resolution
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
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
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
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
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
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
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
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
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
			LatestHandoff:        snapshot.ActiveHandoff,
			LatestLaunch:         snapshot.ActiveLaunch,
			LatestAck:            snapshot.ActiveAcknowledgment,
			LatestFollowThrough:  snapshot.ActiveFollowThrough,
			LatestResolution:     snapshot.LatestResolution,
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
		LatestHandoff:        snapshot.ActiveHandoff,
		LatestLaunch:         snapshot.ActiveLaunch,
		LatestAck:            snapshot.ActiveAcknowledgment,
		LatestFollowThrough:  snapshot.ActiveFollowThrough,
		LatestResolution:     snapshot.LatestResolution,
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
				"Interrupted execution is already recoverable from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because the interrupted recovery state is unchanged; resume the interrupted execution path from that checkpoint.",
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
		case RecoveryClassContinueExecutionRequired:
			base.CanonicalResponse = "Continuity is intact, but the current brief is not yet cleared for execution. Explicit continue finalization must happen before the next bounded run. No new checkpoint was created."
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
				"Fresh next bounded run is already ready from checkpoint %s using brief %s on branch %s (head %s). No new checkpoint was created because the local recovery boundary is unchanged.",
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

func (c *Coordinator) assessRunStartRecovery(ctx context.Context, taskID common.TaskID) (RecoveryAssessment, bool, string, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecoveryAssessment{}, false, "", err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	runFinalization := deriveLocalRunFinalization(assessment, recovery)
	localResume := deriveLocalResumeAuthority(assessment, recovery)
	actions := deriveOperatorActionAuthoritySet(assessment, recovery, branch, runFinalization, localResume)
	allowed, canonical := runStartEligibility(recovery, actions)
	return recovery, allowed, canonical, nil
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
		if caps.CurrentBriefID == "" {
			canonical := "Execution cannot start yet because no execution brief is available. Send a task message first so Tuku can compile intent and create a brief."
			if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{"reason": "missing_brief"}, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
		}
		recovery, allowed, canonical, err := txc.assessRunStartRecovery(ctx, caps.TaskID)
		if err != nil {
			return err
		}
		if !allowed {
			payload := map[string]any{
				"reason":                   "recovery_gate_blocked",
				"recovery_class":           recovery.RecoveryClass,
				"recommended_action":       recovery.RecommendedAction,
				"ready_for_next_run":       recovery.ReadyForNextRun,
				"ready_for_handoff_launch": recovery.ReadyForHandoffLaunch,
				"recovery_reason":          recovery.Reason,
			}
			if err := txc.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
				return err
			}
			out := RunTaskResult{TaskID: caps.TaskID, Phase: caps.CurrentPhase, RunStatus: rundomain.StatusFailed, CanonicalResponse: canonical}
			immediate = &out
			return nil
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
	recovery, allowed, canonical, err := c.assessRunStartRecovery(ctx, caps.TaskID)
	if err != nil {
		return RunTaskResult{}, err
	}
	if !allowed {
		payload := map[string]any{
			"reason":                   "recovery_gate_blocked",
			"recovery_class":           recovery.RecoveryClass,
			"recommended_action":       recovery.RecommendedAction,
			"ready_for_next_run":       recovery.ReadyForNextRun,
			"ready_for_handoff_launch": recovery.ReadyForHandoffLaunch,
			"recovery_reason":          recovery.Reason,
		}
		if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
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
	var latestLaunch *handoff.Launch
	var latestAck *handoff.Acknowledgment
	var latestFollowThrough *handoff.FollowThrough
	if record, err := c.store.Handoffs().LatestResolutionByTask(caps.TaskID); err == nil {
		status.LatestResolutionID = record.ResolutionID
		status.LatestResolutionKind = record.Kind
		status.LatestResolutionSummary = record.Summary
		status.LatestResolutionAt = record.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}
	if packet, launch, ack, followThrough, err := c.loadActiveClaudeHandoffBranch(caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else {
		latestPacket = packet
		latestLaunch = launch
		latestAck = ack
		latestFollowThrough = followThrough
		if latestPacket != nil {
			if latestLaunch != nil {
				status.LatestLaunchAttemptID = latestLaunch.AttemptID
				status.LatestLaunchID = latestLaunch.LaunchID
				status.LatestLaunchStatus = latestLaunch.Status
			}
			control := assessLaunchControl(caps.TaskID, latestPacket, latestLaunch)
			status.LaunchControlState = control.State
			status.LaunchRetryDisposition = control.RetryDisposition
			status.LaunchControlReason = control.Reason
			if latestAck != nil {
				status.LatestAcknowledgmentID = latestAck.AckID
				status.LatestAcknowledgmentStatus = latestAck.Status
				status.LatestAcknowledgmentSummary = latestAck.Summary
			}
			if latestFollowThrough != nil {
				status.LatestFollowThroughID = latestFollowThrough.RecordID
				status.LatestFollowThroughKind = latestFollowThrough.Kind
				status.LatestFollowThroughSummary = latestFollowThrough.Summary
			}
		}
		handoffContinuity := assessHandoffContinuity(caps.TaskID, latestPacket, latestLaunch, latestAck, latestFollowThrough, nil)
		status.HandoffContinuityState = handoffContinuity.State
		status.HandoffContinuityReason = handoffContinuity.Reason
		status.HandoffContinuationProven = handoffContinuity.DownstreamContinuationProven
	}

	if assessment, err := c.assessContinue(ctx, caps.TaskID); err != nil {
		return StatusTaskResult{}, err
	} else {
		recovery, branch, actions, decision, plan, _, runFinalization, localResume := c.operatorTruthForAssessment(assessment)
		applyRecoveryAssessmentToStatus(&status, recovery, checkpointResumable)
		status.ActiveBranchClass = branch.Class
		status.ActiveBranchRef = branch.BranchRef
		status.ActiveBranchAnchorKind = branch.ActionabilityAnchor
		status.ActiveBranchAnchorRef = branch.ActionabilityAnchorRef
		status.ActiveBranchReason = branch.Reason
		status.LocalRunFinalizationState = runFinalization.State
		status.LocalRunFinalizationRunID = runFinalization.RunID
		status.LocalRunFinalizationStatus = runFinalization.RunStatus
		status.LocalRunFinalizationCheckpointID = runFinalization.CheckpointID
		status.LocalRunFinalizationReason = runFinalization.Reason
		status.LocalResumeAuthorityState = localResume.State
		status.LocalResumeMode = localResume.Mode
		status.LocalResumeCheckpointID = localResume.CheckpointID
		status.LocalResumeRunID = localResume.RunID
		status.LocalResumeReason = localResume.Reason
		status.RequiredNextOperatorAction = actions.RequiredNextAction
		status.ActionAuthority = append([]OperatorActionAuthority{}, actions.Actions...)
		status.OperatorDecision = &decision
		status.OperatorExecutionPlan = &plan
	}
	if latestReceipt, err := c.store.OperatorStepReceipts().LatestByTask(caps.TaskID); err == nil {
		receiptCopy := latestReceipt
		status.LatestOperatorStepReceipt = &receiptCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return StatusTaskResult{}, err
	}
	if recentReceipts, err := c.store.OperatorStepReceipts().ListByTask(caps.TaskID, 3); err == nil {
		status.RecentOperatorStepReceipts = append([]operatorstep.Receipt{}, recentReceipts...)
	} else {
		return StatusTaskResult{}, err
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
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			out.Acknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(latestHandoff.HandoffID); err == nil {
			recordCopy := latestFollowThrough
			out.FollowThrough = &recordCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return InspectTaskResult{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestResolution, err := c.store.Handoffs().LatestResolutionByTask(caps.TaskID); err == nil {
		recordCopy := latestResolution
		out.Resolution = &recordCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if latestHandoff, latestLaunch, latestAck, latestFollowThrough, err := c.loadActiveClaudeHandoffBranch(caps.TaskID); err != nil {
		return InspectTaskResult{}, err
	} else {
		if latestHandoff != nil {
			control := assessLaunchControl(caps.TaskID, out.Handoff, out.Launch)
			if out.Handoff == nil || out.Handoff.HandoffID != latestHandoff.HandoffID {
				control = assessLaunchControl(caps.TaskID, latestHandoff, latestLaunch)
			}
			out.LaunchControl = &control
		}
		continuity := assessHandoffContinuity(caps.TaskID, latestHandoff, latestLaunch, latestAck, latestFollowThrough, nil)
		out.HandoffContinuity = &continuity
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
		recovery, branch, actions, decision, plan, _, runFinalization, localResume := c.operatorTruthForAssessment(assessment)
		out.Recovery = &recovery
		out.ActiveBranch = &branch
		out.LocalRunFinalization = &runFinalization
		out.LocalResumeAuthority = &localResume
		out.ActionAuthority = &actions
		out.OperatorDecision = &decision
		out.OperatorExecutionPlan = &plan
		if recovery.LatestAction != nil {
			actionCopy := *recovery.LatestAction
			out.LatestRecoveryAction = &actionCopy
		}
	}
	if latestReceipt, err := c.store.OperatorStepReceipts().LatestByTask(caps.TaskID); err == nil {
		receiptCopy := latestReceipt
		out.LatestOperatorStepReceipt = &receiptCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return InspectTaskResult{}, err
	}
	if recentReceipts, err := c.store.OperatorStepReceipts().ListByTask(caps.TaskID, 5); err == nil {
		out.RecentOperatorStepReceipts = append([]operatorstep.Receipt{}, recentReceipts...)
	} else {
		return InspectTaskResult{}, err
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
		"I found run %s still marked RUNNING but no active execution handle was present. I reconciled it as INTERRUPTED and created resumable checkpoint %s. Resume the interrupted execution path from brief %s.",
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
		ReadyForNextRun:   false,
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
	caps.NextAction = "Fresh next bounded run is ready. Start the next bounded run when local execution should proceed."
	if err := c.store.Capsules().Update(caps); err != nil {
		return err
	}

	descriptor := localResumeDescriptorForReadyNextRun("")
	trigger := checkpoint.TriggerContinue
	if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
		descriptor = localResumeDescriptorForInterrupted("")
	}
	if hasCheckpoint {
		if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
			descriptor = localResumeDescriptorForInterrupted(latestCheckpoint.CheckpointID)
		} else {
			descriptor = localResumeDescriptorForReadyNextRun(latestCheckpoint.CheckpointID)
		}
	}
	cp, err := c.createCheckpoint(caps, runID, trigger, true, descriptor)
	if err != nil {
		return err
	}
	canonical := fmt.Sprintf(
		"Fresh next bounded run is ready. Checkpoint %s captures the current local recovery boundary for brief %s on branch %s (head %s).",
		cp.CheckpointID,
		caps.CurrentBriefID,
		caps.BranchName,
		caps.HeadSHA,
	)
	if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable {
		caps.NextAction = "Interrupted recovery is available. Resume the interrupted execution path from the recoverable checkpoint."
		if err := c.store.Capsules().Update(caps); err != nil {
			return err
		}
		canonical = fmt.Sprintf(
			"Interrupted execution is recoverable. Use checkpoint %s with brief %s on branch %s (head %s) to resume the interrupted execution path.",
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

## internal/orchestrator/shell.go
```go
package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
)

type ShellSnapshotResult struct {
	TaskID                     common.TaskID
	Goal                       string
	Phase                      string
	Status                     string
	RepoAnchor                 anchorgit.Snapshot
	IntentClass                string
	IntentSummary              string
	Brief                      *ShellBriefSummary
	Run                        *ShellRunSummary
	Checkpoint                 *ShellCheckpointSummary
	Handoff                    *ShellHandoffSummary
	Launch                     *ShellLaunchSummary
	LaunchControl              *ShellLaunchControlSummary
	Acknowledgment             *ShellAcknowledgmentSummary
	FollowThrough              *ShellFollowThroughSummary
	Resolution                 *ShellResolutionSummary
	ActiveBranch               *ShellActiveBranchSummary
	LocalRunFinalization       *ShellLocalRunFinalizationSummary
	LocalResume                *ShellLocalResumeAuthoritySummary
	ActionAuthority            *ShellOperatorActionAuthoritySet
	OperatorDecision           *ShellOperatorDecisionSummary
	OperatorExecutionPlan      *ShellOperatorExecutionPlan
	LatestOperatorStepReceipt  *operatorstep.Receipt
	RecentOperatorStepReceipts []operatorstep.Receipt
	HandoffContinuity          *ShellHandoffContinuitySummary
	Recovery                   *ShellRecoverySummary
	RecentProofs               []ShellProofSummary
	RecentConversation         []ShellConversationSummary
	LatestCanonicalResponse    string
}

type ShellBriefSummary struct {
	BriefID          common.BriefID
	Objective        string
	NormalizedAction string
	Constraints      []string
	DoneCriteria     []string
}

type ShellRunSummary struct {
	RunID              common.RunID
	WorkerKind         rundomain.WorkerKind
	Status             rundomain.Status
	LastKnownSummary   string
	StartedAt          time.Time
	EndedAt            *time.Time
	InterruptionReason string
}

type ShellCheckpointSummary struct {
	CheckpointID     common.CheckpointID
	Trigger          checkpoint.Trigger
	CreatedAt        time.Time
	ResumeDescriptor string
	IsResumable      bool
}

type ShellHandoffSummary struct {
	HandoffID    string
	Status       handoff.Status
	SourceWorker rundomain.WorkerKind
	TargetWorker rundomain.WorkerKind
	Mode         handoff.Mode
	Reason       string
	AcceptedBy   rundomain.WorkerKind
	CreatedAt    time.Time
}

type ShellLaunchSummary struct {
	AttemptID         string
	LaunchID          string
	Status            handoff.LaunchStatus
	RequestedAt       time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Summary           string
	ErrorMessage      string
	OutputArtifactRef string
}

type ShellLaunchControlSummary struct {
	State            LaunchControlState
	RetryDisposition LaunchRetryDisposition
	Reason           string
	HandoffID        string
	AttemptID        string
	LaunchID         string
	TargetWorker     rundomain.WorkerKind
	RequestedAt      time.Time
	CompletedAt      time.Time
	FailedAt         time.Time
}

type ShellAcknowledgmentSummary struct {
	Status    handoff.AcknowledgmentStatus
	Summary   string
	CreatedAt time.Time
}

type ShellFollowThroughSummary struct {
	RecordID        string
	Kind            handoff.FollowThroughKind
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ShellResolutionSummary struct {
	ResolutionID    string
	Kind            handoff.ResolutionKind
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ShellActiveBranchSummary struct {
	Class                  ActiveBranchClass
	BranchRef              string
	ActionabilityAnchor    ActiveBranchAnchorKind
	ActionabilityAnchorRef string
	Reason                 string
}

type ShellLocalRunFinalizationSummary struct {
	State        LocalRunFinalizationState
	RunID        common.RunID
	RunStatus    rundomain.Status
	CheckpointID common.CheckpointID
	Reason       string
}

type ShellLocalResumeAuthoritySummary struct {
	State               LocalResumeAuthorityState
	Mode                LocalResumeMode
	CheckpointID        common.CheckpointID
	RunID               common.RunID
	BlockingBranchClass ActiveBranchClass
	BlockingBranchRef   string
	Reason              string
}

type ShellOperatorActionAuthority struct {
	Action              OperatorAction
	State               OperatorActionAuthorityState
	Reason              string
	BlockingBranchClass ActiveBranchClass
	BlockingBranchRef   string
	AnchorKind          ActiveBranchAnchorKind
	AnchorRef           string
}

type ShellOperatorActionAuthoritySet struct {
	RequiredNextAction OperatorAction
	Actions            []ShellOperatorActionAuthority
}

type ShellOperatorDecisionBlockedAction struct {
	Action OperatorAction
	Reason string
}

type ShellOperatorDecisionSummary struct {
	ActiveOwnerClass   ActiveBranchClass
	ActiveOwnerRef     string
	Headline           string
	RequiredNextAction OperatorAction
	PrimaryReason      string
	Guidance           string
	IntegrityNote      string
	BlockedActions     []ShellOperatorDecisionBlockedAction
}

type ShellOperatorExecutionStep struct {
	Action         OperatorAction
	Status         OperatorActionAuthorityState
	Domain         OperatorExecutionDomain
	CommandSurface OperatorCommandSurfaceType
	CommandHint    string
	Reason         string
}

type ShellOperatorExecutionPlan struct {
	PrimaryStep             *ShellOperatorExecutionStep
	MandatoryBeforeProgress bool
	SecondarySteps          []ShellOperatorExecutionStep
	BlockedSteps            []ShellOperatorExecutionStep
}

type ShellHandoffContinuitySummary struct {
	State                        HandoffContinuityState
	Reason                       string
	LaunchAttemptID              string
	LaunchID                     string
	AcknowledgmentID             string
	AcknowledgmentStatus         handoff.AcknowledgmentStatus
	AcknowledgmentSummary        string
	FollowThroughID              string
	FollowThroughKind            handoff.FollowThroughKind
	FollowThroughSummary         string
	ResolutionID                 string
	ResolutionKind               handoff.ResolutionKind
	ResolutionSummary            string
	DownstreamContinuationProven bool
}

type ShellRecoveryIssue struct {
	Code    string
	Message string
}

type ShellRecoverySummary struct {
	ContinuityOutcome      ContinueOutcome
	RecoveryClass          RecoveryClass
	RecommendedAction      RecoveryAction
	ReadyForNextRun        bool
	ReadyForHandoffLaunch  bool
	RequiresDecision       bool
	RequiresRepair         bool
	RequiresReview         bool
	RequiresReconciliation bool
	DriftClass             checkpoint.DriftClass
	Reason                 string
	CheckpointID           common.CheckpointID
	RunID                  common.RunID
	HandoffID              string
	HandoffStatus          handoff.Status
	Issues                 []ShellRecoveryIssue
}

type ShellProofSummary struct {
	EventID   common.EventID
	Type      proof.EventType
	Summary   string
	Timestamp time.Time
}

type ShellConversationSummary struct {
	Role      conversation.Role
	Body      string
	CreatedAt time.Time
}

func (c *Coordinator) ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error) {
	id := common.TaskID(strings.TrimSpace(taskID))
	if id == "" {
		return ShellSnapshotResult{}, fmt.Errorf("task id is required")
	}

	caps, err := c.store.Capsules().Get(id)
	if err != nil {
		return ShellSnapshotResult{}, err
	}

	result := ShellSnapshotResult{
		TaskID:     caps.TaskID,
		Goal:       caps.Goal,
		Phase:      string(caps.CurrentPhase),
		Status:     caps.Status,
		RepoAnchor: capsuleAnchorSnapshot(caps),
	}

	if st, ok, err := c.shellIntent(id, caps.CurrentIntentID); err != nil {
		return ShellSnapshotResult{}, err
	} else if ok {
		result.IntentClass = string(st.Class)
		result.IntentSummary = shellIntentSummary(st)
	}

	if b, ok, err := c.shellBrief(id, caps.CurrentBriefID); err != nil {
		return ShellSnapshotResult{}, err
	} else if ok {
		result.Brief = &ShellBriefSummary{
			BriefID:          b.BriefID,
			Objective:        b.Objective,
			NormalizedAction: b.NormalizedAction,
			Constraints:      append([]string{}, b.Constraints...),
			DoneCriteria:     append([]string{}, b.DoneCriteria...),
		}
	}

	if runRec, err := c.store.Runs().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Run = &ShellRunSummary{
			RunID:              runRec.RunID,
			WorkerKind:         runRec.WorkerKind,
			Status:             runRec.Status,
			LastKnownSummary:   runRec.LastKnownSummary,
			StartedAt:          runRec.StartedAt,
			EndedAt:            runRec.EndedAt,
			InterruptionReason: runRec.InterruptionReason,
		}
	}

	if cp, err := c.store.Checkpoints().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Checkpoint = &ShellCheckpointSummary{
			CheckpointID:     cp.CheckpointID,
			Trigger:          cp.Trigger,
			CreatedAt:        cp.CreatedAt,
			ResumeDescriptor: cp.ResumeDescriptor,
			IsResumable:      cp.IsResumable,
		}
	}

	if packet, err := c.store.Handoffs().LatestByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Handoff = &ShellHandoffSummary{
			HandoffID:    packet.HandoffID,
			Status:       packet.Status,
			SourceWorker: packet.SourceWorker,
			TargetWorker: packet.TargetWorker,
			Mode:         packet.HandoffMode,
			Reason:       packet.Reason,
			AcceptedBy:   packet.AcceptedBy,
			CreatedAt:    packet.CreatedAt,
		}
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.Launch = &ShellLaunchSummary{
				AttemptID:         latestLaunch.AttemptID,
				LaunchID:          latestLaunch.LaunchID,
				Status:            latestLaunch.Status,
				RequestedAt:       latestLaunch.RequestedAt,
				StartedAt:         latestLaunch.StartedAt,
				EndedAt:           latestLaunch.EndedAt,
				Summary:           latestLaunch.Summary,
				ErrorMessage:      latestLaunch.ErrorMessage,
				OutputArtifactRef: latestLaunch.OutputArtifactRef,
			}
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.Acknowledgment = &ShellAcknowledgmentSummary{
				Status:    latestAck.Status,
				Summary:   latestAck.Summary,
				CreatedAt: latestAck.CreatedAt,
			}
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.FollowThrough = &ShellFollowThroughSummary{
				RecordID:        latestFollowThrough.RecordID,
				Kind:            latestFollowThrough.Kind,
				Summary:         latestFollowThrough.Summary,
				LaunchAttemptID: latestFollowThrough.LaunchAttemptID,
				LaunchID:        latestFollowThrough.LaunchID,
				CreatedAt:       latestFollowThrough.CreatedAt,
			}
		}
	}

	if latestResolution, err := c.store.Handoffs().LatestResolutionByTask(id); err != nil {
		if err != sql.ErrNoRows {
			return ShellSnapshotResult{}, err
		}
	} else {
		result.Resolution = &ShellResolutionSummary{
			ResolutionID:    latestResolution.ResolutionID,
			Kind:            latestResolution.Kind,
			Summary:         latestResolution.Summary,
			LaunchAttemptID: latestResolution.LaunchAttemptID,
			LaunchID:        latestResolution.LaunchID,
			CreatedAt:       latestResolution.CreatedAt,
		}
	}
	if packet, latestLaunch, latestAck, latestFollowThrough, err := c.loadActiveClaudeHandoffBranch(id); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		continuity := assessHandoffContinuity(id, packet, latestLaunch, latestAck, latestFollowThrough, nil)
		result.HandoffContinuity = &ShellHandoffContinuitySummary{
			State:                        continuity.State,
			Reason:                       continuity.Reason,
			LaunchAttemptID:              continuity.LaunchAttemptID,
			LaunchID:                     continuity.LaunchID,
			AcknowledgmentID:             continuity.AcknowledgmentID,
			AcknowledgmentStatus:         continuity.AcknowledgmentStatus,
			AcknowledgmentSummary:        continuity.AcknowledgmentSummary,
			FollowThroughID:              continuity.FollowThroughID,
			FollowThroughKind:            continuity.FollowThroughKind,
			FollowThroughSummary:         continuity.FollowThroughSummary,
			ResolutionID:                 continuity.ResolutionID,
			ResolutionKind:               continuity.ResolutionKind,
			ResolutionSummary:            continuity.ResolutionSummary,
			DownstreamContinuationProven: continuity.DownstreamContinuationProven,
		}
	}

	if events, err := c.store.Proofs().ListByTask(id, 8); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentProofs = make([]ShellProofSummary, 0, len(events))
		for _, evt := range events {
			result.RecentProofs = append(result.RecentProofs, ShellProofSummary{
				EventID:   evt.EventID,
				Type:      evt.Type,
				Summary:   summarizeProofEvent(evt),
				Timestamp: evt.Timestamp,
			})
		}
	}
	if latestReceipt, err := c.store.OperatorStepReceipts().LatestByTask(id); err == nil {
		receiptCopy := latestReceipt
		result.LatestOperatorStepReceipt = &receiptCopy
	} else if err != sql.ErrNoRows {
		return ShellSnapshotResult{}, err
	}
	if recentReceipts, err := c.store.OperatorStepReceipts().ListByTask(id, 5); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentOperatorStepReceipts = append([]operatorstep.Receipt{}, recentReceipts...)
	}

	if messages, err := c.store.Conversations().ListRecent(caps.ConversationID, 18); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		result.RecentConversation = make([]ShellConversationSummary, 0, len(messages))
		for _, msg := range messages {
			result.RecentConversation = append(result.RecentConversation, ShellConversationSummary{
				Role:      msg.Role,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			})
			if msg.Role == conversation.RoleSystem {
				result.LatestCanonicalResponse = msg.Body
			}
		}
	}

	if assessment, err := c.assessContinue(ctx, id); err != nil {
		return ShellSnapshotResult{}, err
	} else {
		recovery, branch, authorities, decision, plan, _, runFinalization, localResume := c.operatorTruthForAssessment(assessment)
		result.Recovery = shellRecoverySummary(recovery)
		result.ActiveBranch = &ShellActiveBranchSummary{
			Class:                  branch.Class,
			BranchRef:              branch.BranchRef,
			ActionabilityAnchor:    branch.ActionabilityAnchor,
			ActionabilityAnchorRef: branch.ActionabilityAnchorRef,
			Reason:                 branch.Reason,
		}
		result.LocalRunFinalization = &ShellLocalRunFinalizationSummary{
			State:        runFinalization.State,
			RunID:        runFinalization.RunID,
			RunStatus:    runFinalization.RunStatus,
			CheckpointID: runFinalization.CheckpointID,
			Reason:       runFinalization.Reason,
		}
		result.LocalResume = &ShellLocalResumeAuthoritySummary{
			State:               localResume.State,
			Mode:                localResume.Mode,
			CheckpointID:        localResume.CheckpointID,
			RunID:               localResume.RunID,
			BlockingBranchClass: localResume.BlockingBranchClass,
			BlockingBranchRef:   localResume.BlockingBranchRef,
			Reason:              localResume.Reason,
		}
		result.ActionAuthority = shellOperatorActionAuthoritySet(authorities)
		result.OperatorDecision = shellOperatorDecisionSummary(decision)
		result.OperatorExecutionPlan = shellOperatorExecutionPlan(plan)
		control := assessLaunchControl(id, assessment.LatestHandoff, assessment.LatestLaunch)
		result.LaunchControl = &ShellLaunchControlSummary{
			State:            control.State,
			RetryDisposition: control.RetryDisposition,
			Reason:           control.Reason,
			HandoffID:        control.HandoffID,
			AttemptID:        control.AttemptID,
			LaunchID:         control.LaunchID,
			TargetWorker:     control.TargetWorker,
			RequestedAt:      control.RequestedAt,
			CompletedAt:      control.CompletedAt,
			FailedAt:         control.FailedAt,
		}
	}

	return result, nil
}

func shellOperatorActionAuthoritySet(in OperatorActionAuthoritySet) *ShellOperatorActionAuthoritySet {
	out := &ShellOperatorActionAuthoritySet{
		RequiredNextAction: in.RequiredNextAction,
	}
	if len(in.Actions) > 0 {
		out.Actions = make([]ShellOperatorActionAuthority, 0, len(in.Actions))
		for _, action := range in.Actions {
			out.Actions = append(out.Actions, ShellOperatorActionAuthority{
				Action:              action.Action,
				State:               action.State,
				Reason:              action.Reason,
				BlockingBranchClass: action.BlockingBranchClass,
				BlockingBranchRef:   action.BlockingBranchRef,
				AnchorKind:          action.AnchorKind,
				AnchorRef:           action.AnchorRef,
			})
		}
	}
	return out
}

func shellOperatorDecisionSummary(in OperatorDecisionSummary) *ShellOperatorDecisionSummary {
	out := &ShellOperatorDecisionSummary{
		ActiveOwnerClass:   in.ActiveOwnerClass,
		ActiveOwnerRef:     in.ActiveOwnerRef,
		Headline:           in.Headline,
		RequiredNextAction: in.RequiredNextAction,
		PrimaryReason:      in.PrimaryReason,
		Guidance:           in.Guidance,
		IntegrityNote:      in.IntegrityNote,
	}
	if len(in.BlockedActions) > 0 {
		out.BlockedActions = make([]ShellOperatorDecisionBlockedAction, 0, len(in.BlockedActions))
		for _, blocked := range in.BlockedActions {
			out.BlockedActions = append(out.BlockedActions, ShellOperatorDecisionBlockedAction{
				Action: blocked.Action,
				Reason: blocked.Reason,
			})
		}
	}
	return out
}

func shellOperatorExecutionPlan(in OperatorExecutionPlan) *ShellOperatorExecutionPlan {
	out := &ShellOperatorExecutionPlan{
		MandatoryBeforeProgress: in.MandatoryBeforeProgress,
	}
	if in.PrimaryStep != nil {
		out.PrimaryStep = &ShellOperatorExecutionStep{
			Action:         in.PrimaryStep.Action,
			Status:         in.PrimaryStep.Status,
			Domain:         in.PrimaryStep.Domain,
			CommandSurface: in.PrimaryStep.CommandSurface,
			CommandHint:    in.PrimaryStep.CommandHint,
			Reason:         in.PrimaryStep.Reason,
		}
	}
	if len(in.SecondarySteps) > 0 {
		out.SecondarySteps = make([]ShellOperatorExecutionStep, 0, len(in.SecondarySteps))
		for _, step := range in.SecondarySteps {
			out.SecondarySteps = append(out.SecondarySteps, ShellOperatorExecutionStep{
				Action:         step.Action,
				Status:         step.Status,
				Domain:         step.Domain,
				CommandSurface: step.CommandSurface,
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	if len(in.BlockedSteps) > 0 {
		out.BlockedSteps = make([]ShellOperatorExecutionStep, 0, len(in.BlockedSteps))
		for _, step := range in.BlockedSteps {
			out.BlockedSteps = append(out.BlockedSteps, ShellOperatorExecutionStep{
				Action:         step.Action,
				Status:         step.Status,
				Domain:         step.Domain,
				CommandSurface: step.CommandSurface,
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	return out
}

func shellRecoverySummary(in RecoveryAssessment) *ShellRecoverySummary {
	out := &ShellRecoverySummary{
		ContinuityOutcome:      in.ContinuityOutcome,
		RecoveryClass:          in.RecoveryClass,
		RecommendedAction:      in.RecommendedAction,
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
		HandoffStatus:          in.HandoffStatus,
	}
	if len(in.Issues) > 0 {
		out.Issues = make([]ShellRecoveryIssue, 0, len(in.Issues))
		for _, issue := range in.Issues {
			out.Issues = append(out.Issues, ShellRecoveryIssue{
				Code:    issue.Code,
				Message: issue.Message,
			})
		}
	}
	return out
}

func (c *Coordinator) shellIntent(taskID common.TaskID, currentID common.IntentID) (intent.State, bool, error) {
	if currentID != "" {
		st, err := c.store.Intents().LatestByTask(taskID)
		if err == nil && st.IntentID == currentID {
			return st, true, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return intent.State{}, false, err
		}
	}
	st, err := c.store.Intents().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return intent.State{}, false, nil
		}
		return intent.State{}, false, err
	}
	return st, true, nil
}

func (c *Coordinator) shellBrief(taskID common.TaskID, currentID common.BriefID) (brief.ExecutionBrief, bool, error) {
	if currentID != "" {
		b, err := c.store.Briefs().Get(currentID)
		if err == nil {
			return b, true, nil
		}
		if err != sql.ErrNoRows {
			return brief.ExecutionBrief{}, false, err
		}
	}
	b, err := c.store.Briefs().LatestByTask(taskID)
	if err != nil {
		if err == sql.ErrNoRows {
			return brief.ExecutionBrief{}, false, nil
		}
		return brief.ExecutionBrief{}, false, err
	}
	return b, true, nil
}

func capsuleAnchorSnapshot(caps capsule.WorkCapsule) anchorgit.Snapshot {
	return anchorgit.Snapshot{
		RepoRoot:         caps.RepoRoot,
		Branch:           caps.BranchName,
		HeadSHA:          caps.HeadSHA,
		WorkingTreeDirty: caps.WorkingTreeDirty,
		CapturedAt:       caps.AnchorCapturedAt,
	}
}

func shellIntentSummary(st intent.State) string {
	if strings.TrimSpace(st.NormalizedAction) == "" {
		return string(st.Class)
	}
	return fmt.Sprintf("%s: %s", st.Class, st.NormalizedAction)
}

func summarizeProofEvent(evt proof.Event) string {
	switch evt.Type {
	case proof.EventUserMessageReceived:
		return "User message recorded"
	case proof.EventIntentCompiled:
		return "Intent compiled"
	case proof.EventBriefCreated:
		return "Execution brief updated"
	case proof.EventBriefRegenerated:
		return "Execution brief regenerated"
	case proof.EventWorkerRunStarted:
		return "Worker run started"
	case proof.EventWorkerRunCompleted:
		return "Worker run completed"
	case proof.EventWorkerRunFailed:
		return "Worker run failed"
	case proof.EventRunInterrupted:
		return "Run interrupted"
	case proof.EventCheckpointCreated:
		return "Checkpoint created"
	case proof.EventContinueAssessed:
		return "Continuity assessed"
	case proof.EventHandoffCreated:
		return "Handoff packet created"
	case proof.EventHandoffAccepted:
		return "Handoff accepted"
	case proof.EventHandoffLaunchRequested:
		return "Handoff launch prepared"
	case proof.EventHandoffLaunchCompleted:
		return "Handoff launch invoked"
	case proof.EventHandoffLaunchFailed:
		return "Handoff launch failed"
	case proof.EventHandoffLaunchBlocked:
		return "Handoff launch blocked"
	case proof.EventHandoffAcknowledgmentCaptured:
		return "Worker acknowledgment captured"
	case proof.EventHandoffAcknowledgmentUnavailable:
		return "Worker acknowledgment unavailable"
	case proof.EventHandoffFollowThroughRecorded:
		return "Downstream follow-through recorded"
	case proof.EventHandoffResolutionRecorded:
		return "Handoff resolution recorded"
	case proof.EventOperatorStepExecutionRecorded:
		return "Operator step receipt recorded"
	case proof.EventRecoveryActionRecorded:
		return "Recovery action recorded"
	case proof.EventInterruptedRunReviewed:
		return "Interrupted run reviewed"
	case proof.EventInterruptedRunResumeExecuted:
		return "Interrupted lineage continuation selected"
	case proof.EventRecoveryContinueExecuted:
		return "Continue recovery executed"
	case proof.EventShellHostStarted:
		return "Shell live host started"
	case proof.EventShellHostExited:
		return "Shell live host ended"
	case proof.EventShellFallbackActivated:
		return "Shell transcript fallback activated"
	case proof.EventCanonicalResponseEmitted:
		return "Canonical response emitted"
	default:
		return strings.ReplaceAll(strings.ToLower(string(evt.Type)), "_", " ")
	}
}
```

## internal/ipc/types.go
```go
package ipc

import "encoding/json"

type Method string

const (
	MethodStartTask                  Method = "task.start"
	MethodResolveShellTaskForRepo    Method = "task.shell.resolve"
	MethodSendMessage                Method = "task.message"
	MethodContinueTask               Method = "task.continue"
	MethodRecordRecoveryAction       Method = "task.recovery.record"
	MethodReviewInterruptedRun       Method = "task.recovery.review_interrupted"
	MethodExecuteRebrief             Method = "task.recovery.rebrief"
	MethodExecuteInterruptedResume   Method = "task.recovery.resume_interrupted"
	MethodExecuteContinueRecovery    Method = "task.recovery.continue"
	MethodExecutePrimaryOperatorStep Method = "task.operator.next"
	MethodTaskRun                    Method = "task.run"
	MethodTaskStatus                 Method = "task.status"
	MethodTaskInspect                Method = "task.inspect"
	MethodTaskShellSnapshot          Method = "task.shell.snapshot"
	MethodTaskShellLifecycle         Method = "task.shell.lifecycle"
	MethodTaskShellSessionReport     Method = "task.shell.session.report"
	MethodTaskShellSessions          Method = "task.shell.sessions"
	MethodCreateCheckpoint           Method = "task.checkpoint"
	MethodCreateHandoff              Method = "task.handoff.create"
	MethodAcceptHandoff              Method = "task.handoff.accept"
	MethodLaunchHandoff              Method = "task.handoff.launch"
	MethodRecordHandoffFollowThrough Method = "task.handoff.followthrough.record"
	MethodRecordHandoffResolution    Method = "task.handoff.resolve"
	MethodApproveDecision            Method = "task.approve"
	MethodRejectDecision             Method = "task.reject"
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

## internal/ipc/payloads.go
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
	TaskID                           common.TaskID                 `json:"task_id"`
	ConversationID                   common.ConversationID         `json:"conversation_id"`
	Goal                             string                        `json:"goal"`
	Phase                            phase.Phase                   `json:"phase"`
	Status                           string                        `json:"status"`
	CurrentIntentID                  common.IntentID               `json:"current_intent_id"`
	CurrentIntentClass               string                        `json:"current_intent_class,omitempty"`
	CurrentIntentSummary             string                        `json:"current_intent_summary,omitempty"`
	CurrentBriefID                   common.BriefID                `json:"current_brief_id,omitempty"`
	CurrentBriefHash                 string                        `json:"current_brief_hash,omitempty"`
	LatestRunID                      common.RunID                  `json:"latest_run_id,omitempty"`
	LatestRunStatus                  run.Status                    `json:"latest_run_status,omitempty"`
	LatestRunSummary                 string                        `json:"latest_run_summary,omitempty"`
	RepoAnchor                       RepoAnchor                    `json:"repo_anchor"`
	LatestCheckpointID               common.CheckpointID           `json:"latest_checkpoint_id,omitempty"`
	LatestCheckpointAtUnixMs         int64                         `json:"latest_checkpoint_at_unix_ms,omitempty"`
	LatestCheckpointTrigger          string                        `json:"latest_checkpoint_trigger,omitempty"`
	CheckpointResumable              bool                          `json:"checkpoint_resumable,omitempty"`
	ResumeDescriptor                 string                        `json:"resume_descriptor,omitempty"`
	LatestLaunchAttemptID            string                        `json:"latest_launch_attempt_id,omitempty"`
	LatestLaunchID                   string                        `json:"latest_launch_id,omitempty"`
	LatestLaunchStatus               string                        `json:"latest_launch_status,omitempty"`
	LatestAcknowledgmentID           string                        `json:"latest_acknowledgment_id,omitempty"`
	LatestAcknowledgmentStatus       string                        `json:"latest_acknowledgment_status,omitempty"`
	LatestAcknowledgmentSummary      string                        `json:"latest_acknowledgment_summary,omitempty"`
	LatestFollowThroughID            string                        `json:"latest_follow_through_id,omitempty"`
	LatestFollowThroughKind          string                        `json:"latest_follow_through_kind,omitempty"`
	LatestFollowThroughSummary       string                        `json:"latest_follow_through_summary,omitempty"`
	LatestResolutionID               string                        `json:"latest_resolution_id,omitempty"`
	LatestResolutionKind             string                        `json:"latest_resolution_kind,omitempty"`
	LatestResolutionSummary          string                        `json:"latest_resolution_summary,omitempty"`
	LatestResolutionAtUnixMs         int64                         `json:"latest_resolution_at_unix_ms,omitempty"`
	LaunchControlState               string                        `json:"launch_control_state,omitempty"`
	LaunchRetryDisposition           string                        `json:"launch_retry_disposition,omitempty"`
	LaunchControlReason              string                        `json:"launch_control_reason,omitempty"`
	HandoffContinuityState           string                        `json:"handoff_continuity_state,omitempty"`
	HandoffContinuityReason          string                        `json:"handoff_continuity_reason,omitempty"`
	HandoffContinuationProven        bool                          `json:"handoff_continuation_proven"`
	ActiveBranchClass                string                        `json:"active_branch_class,omitempty"`
	ActiveBranchRef                  string                        `json:"active_branch_ref,omitempty"`
	ActiveBranchAnchorKind           string                        `json:"active_branch_anchor_kind,omitempty"`
	ActiveBranchAnchorRef            string                        `json:"active_branch_anchor_ref,omitempty"`
	ActiveBranchReason               string                        `json:"active_branch_reason,omitempty"`
	LocalRunFinalizationState        string                        `json:"local_run_finalization_state,omitempty"`
	LocalRunFinalizationRunID        common.RunID                  `json:"local_run_finalization_run_id,omitempty"`
	LocalRunFinalizationStatus       run.Status                    `json:"local_run_finalization_status,omitempty"`
	LocalRunFinalizationCheckpointID common.CheckpointID           `json:"local_run_finalization_checkpoint_id,omitempty"`
	LocalRunFinalizationReason       string                        `json:"local_run_finalization_reason,omitempty"`
	LocalResumeAuthorityState        string                        `json:"local_resume_authority_state,omitempty"`
	LocalResumeMode                  string                        `json:"local_resume_mode,omitempty"`
	LocalResumeCheckpointID          common.CheckpointID           `json:"local_resume_checkpoint_id,omitempty"`
	LocalResumeRunID                 common.RunID                  `json:"local_resume_run_id,omitempty"`
	LocalResumeReason                string                        `json:"local_resume_reason,omitempty"`
	RequiredNextOperatorAction       string                        `json:"required_next_operator_action,omitempty"`
	ActionAuthority                  []TaskOperatorActionAuthority `json:"action_authority,omitempty"`
	OperatorDecision                 *TaskOperatorDecisionSummary  `json:"operator_decision,omitempty"`
	OperatorExecutionPlan            *TaskOperatorExecutionPlan    `json:"operator_execution_plan,omitempty"`
	LatestOperatorStepReceipt        *TaskOperatorStepReceipt      `json:"latest_operator_step_receipt,omitempty"`
	RecentOperatorStepReceipts       []TaskOperatorStepReceipt     `json:"recent_operator_step_receipts,omitempty"`
	IsResumable                      bool                          `json:"is_resumable,omitempty"`
	RecoveryClass                    string                        `json:"recovery_class,omitempty"`
	RecommendedAction                string                        `json:"recommended_action,omitempty"`
	ReadyForNextRun                  bool                          `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch            bool                          `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason                   string                        `json:"recovery_reason,omitempty"`
	LatestRecoveryAction             *TaskRecoveryActionRecord     `json:"latest_recovery_action,omitempty"`
	LastEventType                    string                        `json:"last_event_type,omitempty"`
	LastEventID                      common.EventID                `json:"last_event_id,omitempty"`
	LastEventAtUnixMs                int64                         `json:"last_event_at_unix_ms,omitempty"`
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

type TaskHandoffContinuity struct {
	TaskID                       common.TaskID  `json:"task_id"`
	HandoffID                    string         `json:"handoff_id,omitempty"`
	TargetWorker                 run.WorkerKind `json:"target_worker,omitempty"`
	State                        string         `json:"state"`
	LaunchAttemptID              string         `json:"launch_attempt_id,omitempty"`
	LaunchID                     string         `json:"launch_id,omitempty"`
	LaunchStatus                 string         `json:"launch_status,omitempty"`
	AcknowledgmentID             string         `json:"acknowledgment_id,omitempty"`
	AcknowledgmentStatus         string         `json:"acknowledgment_status,omitempty"`
	AcknowledgmentSummary        string         `json:"acknowledgment_summary,omitempty"`
	FollowThroughID              string         `json:"follow_through_id,omitempty"`
	FollowThroughKind            string         `json:"follow_through_kind,omitempty"`
	FollowThroughSummary         string         `json:"follow_through_summary,omitempty"`
	ResolutionID                 string         `json:"resolution_id,omitempty"`
	ResolutionKind               string         `json:"resolution_kind,omitempty"`
	ResolutionSummary            string         `json:"resolution_summary,omitempty"`
	DownstreamContinuationProven bool           `json:"downstream_continuation_proven"`
	Reason                       string         `json:"reason,omitempty"`
}

type TaskActiveBranch struct {
	TaskID                 common.TaskID `json:"task_id"`
	Class                  string        `json:"class"`
	BranchRef              string        `json:"branch_ref,omitempty"`
	ActionabilityAnchor    string        `json:"actionability_anchor_kind,omitempty"`
	ActionabilityAnchorRef string        `json:"actionability_anchor_ref,omitempty"`
	Reason                 string        `json:"reason,omitempty"`
}

type TaskLocalResumeAuthority struct {
	TaskID              common.TaskID       `json:"task_id"`
	State               string              `json:"state"`
	Mode                string              `json:"mode"`
	CheckpointID        common.CheckpointID `json:"checkpoint_id,omitempty"`
	RunID               common.RunID        `json:"run_id,omitempty"`
	BlockingBranchClass string              `json:"blocking_branch_class,omitempty"`
	BlockingBranchRef   string              `json:"blocking_branch_ref,omitempty"`
	Reason              string              `json:"reason,omitempty"`
}

type TaskLocalRunFinalization struct {
	TaskID       common.TaskID       `json:"task_id"`
	State        string              `json:"state"`
	RunID        common.RunID        `json:"run_id,omitempty"`
	RunStatus    run.Status          `json:"run_status,omitempty"`
	CheckpointID common.CheckpointID `json:"checkpoint_id,omitempty"`
	Reason       string              `json:"reason,omitempty"`
}

type TaskOperatorActionAuthority struct {
	Action              string `json:"action"`
	State               string `json:"state"`
	Reason              string `json:"reason,omitempty"`
	BlockingBranchClass string `json:"blocking_branch_class,omitempty"`
	BlockingBranchRef   string `json:"blocking_branch_ref,omitempty"`
	AnchorKind          string `json:"anchor_kind,omitempty"`
	AnchorRef           string `json:"anchor_ref,omitempty"`
}

type TaskOperatorActionAuthoritySet struct {
	RequiredNextAction string                        `json:"required_next_action,omitempty"`
	Actions            []TaskOperatorActionAuthority `json:"actions,omitempty"`
}

type TaskOperatorDecisionBlockedAction struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type TaskOperatorDecisionSummary struct {
	ActiveOwnerClass   string                              `json:"active_owner_class,omitempty"`
	ActiveOwnerRef     string                              `json:"active_owner_ref,omitempty"`
	Headline           string                              `json:"headline,omitempty"`
	RequiredNextAction string                              `json:"required_next_action,omitempty"`
	PrimaryReason      string                              `json:"primary_reason,omitempty"`
	Guidance           string                              `json:"guidance,omitempty"`
	IntegrityNote      string                              `json:"integrity_note,omitempty"`
	BlockedActions     []TaskOperatorDecisionBlockedAction `json:"blocked_actions,omitempty"`
}

type TaskOperatorExecutionStep struct {
	Action         string `json:"action"`
	Status         string `json:"status"`
	Domain         string `json:"domain,omitempty"`
	CommandSurface string `json:"command_surface,omitempty"`
	CommandHint    string `json:"command_hint,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type TaskOperatorExecutionPlan struct {
	PrimaryStep             *TaskOperatorExecutionStep  `json:"primary_step,omitempty"`
	MandatoryBeforeProgress bool                        `json:"mandatory_before_progress"`
	SecondarySteps          []TaskOperatorExecutionStep `json:"secondary_steps,omitempty"`
	BlockedSteps            []TaskOperatorExecutionStep `json:"blocked_steps,omitempty"`
}

type TaskOperatorStepReceipt struct {
	ReceiptID          string              `json:"receipt_id"`
	TaskID             common.TaskID       `json:"task_id"`
	ActionHandle       string              `json:"action_handle"`
	ExecutionDomain    string              `json:"execution_domain,omitempty"`
	CommandSurfaceKind string              `json:"command_surface_kind,omitempty"`
	ExecutionAttempted bool                `json:"execution_attempted"`
	ResultClass        string              `json:"result_class"`
	Summary            string              `json:"summary,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	RunID              common.RunID        `json:"run_id,omitempty"`
	CheckpointID       common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID            common.BriefID      `json:"brief_id,omitempty"`
	HandoffID          string              `json:"handoff_id,omitempty"`
	LaunchAttemptID    string              `json:"launch_attempt_id,omitempty"`
	LaunchID           string              `json:"launch_id,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	CompletedAt        time.Time           `json:"completed_at,omitempty"`
}

type TaskInspectResponse struct {
	TaskID                     common.TaskID                   `json:"task_id"`
	RepoAnchor                 RepoAnchor                      `json:"repo_anchor"`
	Intent                     *intent.State                   `json:"intent,omitempty"`
	Brief                      *brief.ExecutionBrief           `json:"brief,omitempty"`
	Run                        *run.ExecutionRun               `json:"run,omitempty"`
	Checkpoint                 *checkpoint.Checkpoint          `json:"checkpoint,omitempty"`
	Handoff                    *handoff.Packet                 `json:"handoff,omitempty"`
	Launch                     *handoff.Launch                 `json:"launch,omitempty"`
	Acknowledgment             *handoff.Acknowledgment         `json:"acknowledgment,omitempty"`
	FollowThrough              *handoff.FollowThrough          `json:"follow_through,omitempty"`
	Resolution                 *handoff.Resolution             `json:"resolution,omitempty"`
	ActiveBranch               *TaskActiveBranch               `json:"active_branch,omitempty"`
	LocalRunFinalization       *TaskLocalRunFinalization       `json:"local_run_finalization,omitempty"`
	LocalResumeAuthority       *TaskLocalResumeAuthority       `json:"local_resume_authority,omitempty"`
	ActionAuthority            *TaskOperatorActionAuthoritySet `json:"action_authority,omitempty"`
	OperatorDecision           *TaskOperatorDecisionSummary    `json:"operator_decision,omitempty"`
	OperatorExecutionPlan      *TaskOperatorExecutionPlan      `json:"operator_execution_plan,omitempty"`
	LatestOperatorStepReceipt  *TaskOperatorStepReceipt        `json:"latest_operator_step_receipt,omitempty"`
	RecentOperatorStepReceipts []TaskOperatorStepReceipt       `json:"recent_operator_step_receipts,omitempty"`
	LaunchControl              *TaskLaunchControl              `json:"launch_control,omitempty"`
	HandoffContinuity          *TaskHandoffContinuity          `json:"handoff_continuity,omitempty"`
	Recovery                   *TaskRecoveryAssessment         `json:"recovery,omitempty"`
	LatestRecoveryAction       *TaskRecoveryActionRecord       `json:"latest_recovery_action,omitempty"`
	RecentRecoveryActions      []TaskRecoveryActionRecord      `json:"recent_recovery_actions,omitempty"`
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

type TaskReviewInterruptedRunRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskRebriefRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskInterruptedResumeRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskInterruptedResumeResponse struct {
	TaskID                common.TaskID            `json:"task_id"`
	BriefID               common.BriefID           `json:"brief_id"`
	BriefHash             string                   `json:"brief_hash"`
	Action                TaskRecoveryActionRecord `json:"action"`
	RecoveryClass         string                   `json:"recovery_class"`
	RecommendedAction     string                   `json:"recommended_action"`
	ReadyForNextRun       bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                     `json:"ready_for_handoff_launch"`
	RecoveryReason        string                   `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                   `json:"canonical_response"`
}

type TaskRebriefResponse struct {
	TaskID                common.TaskID  `json:"task_id"`
	PreviousBriefID       common.BriefID `json:"previous_brief_id"`
	BriefID               common.BriefID `json:"brief_id"`
	BriefHash             string         `json:"brief_hash"`
	RecoveryClass         string         `json:"recovery_class"`
	RecommendedAction     string         `json:"recommended_action"`
	ReadyForNextRun       bool           `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool           `json:"ready_for_handoff_launch"`
	RecoveryReason        string         `json:"recovery_reason,omitempty"`
	CanonicalResponse     string         `json:"canonical_response"`
}

type TaskContinueRecoveryRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskContinueRecoveryResponse struct {
	TaskID                common.TaskID            `json:"task_id"`
	BriefID               common.BriefID           `json:"brief_id"`
	BriefHash             string                   `json:"brief_hash"`
	Action                TaskRecoveryActionRecord `json:"action"`
	RecoveryClass         string                   `json:"recovery_class"`
	RecommendedAction     string                   `json:"recommended_action"`
	ReadyForNextRun       bool                     `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                     `json:"ready_for_handoff_launch"`
	RecoveryReason        string                   `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                   `json:"canonical_response"`
}

type TaskExecutePrimaryOperatorStepRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskExecutePrimaryOperatorStepResponse struct {
	TaskID                     common.TaskID                `json:"task_id"`
	Receipt                    TaskOperatorStepReceipt      `json:"receipt"`
	ActiveBranch               *TaskActiveBranch            `json:"active_branch,omitempty"`
	OperatorDecision           *TaskOperatorDecisionSummary `json:"operator_decision,omitempty"`
	OperatorExecutionPlan      *TaskOperatorExecutionPlan   `json:"operator_execution_plan,omitempty"`
	RecoveryClass              string                       `json:"recovery_class,omitempty"`
	RecommendedAction          string                       `json:"recommended_action,omitempty"`
	ReadyForNextRun            bool                         `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch      bool                         `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason             string                       `json:"recovery_reason,omitempty"`
	CanonicalResponse          string                       `json:"canonical_response,omitempty"`
	RecentOperatorStepReceipts []TaskOperatorStepReceipt    `json:"recent_operator_step_receipts,omitempty"`
}

type TaskHandoffFollowThroughRecordRequest struct {
	TaskID  common.TaskID `json:"task_id"`
	Kind    string        `json:"kind"`
	Summary string        `json:"summary,omitempty"`
	Notes   []string      `json:"notes,omitempty"`
}

type TaskHandoffFollowThroughRecordResponse struct {
	TaskID                common.TaskID          `json:"task_id"`
	Record                *handoff.FollowThrough `json:"record"`
	HandoffContinuity     *TaskHandoffContinuity `json:"handoff_continuity,omitempty"`
	RecoveryClass         string                 `json:"recovery_class,omitempty"`
	RecommendedAction     string                 `json:"recommended_action,omitempty"`
	ReadyForNextRun       bool                   `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                   `json:"ready_for_handoff_launch"`
	RecoveryReason        string                 `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                 `json:"canonical_response"`
}

type TaskHandoffResolutionRecordRequest struct {
	TaskID    common.TaskID `json:"task_id"`
	HandoffID string        `json:"handoff_id,omitempty"`
	Kind      string        `json:"kind"`
	Summary   string        `json:"summary,omitempty"`
	Notes     []string      `json:"notes,omitempty"`
}

type TaskHandoffResolutionRecordResponse struct {
	TaskID                common.TaskID          `json:"task_id"`
	Record                *handoff.Resolution    `json:"record"`
	HandoffContinuity     *TaskHandoffContinuity `json:"handoff_continuity,omitempty"`
	RecoveryClass         string                 `json:"recovery_class,omitempty"`
	RecommendedAction     string                 `json:"recommended_action,omitempty"`
	ReadyForNextRun       bool                   `json:"ready_for_next_run"`
	ReadyForHandoffLaunch bool                   `json:"ready_for_handoff_launch"`
	RecoveryReason        string                 `json:"recovery_reason,omitempty"`
	CanonicalResponse     string                 `json:"canonical_response"`
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

type TaskShellFollowThrough struct {
	RecordID        string    `json:"record_id,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	LaunchAttemptID string    `json:"launch_attempt_id,omitempty"`
	LaunchID        string    `json:"launch_id,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

type TaskShellResolution struct {
	ResolutionID    string    `json:"resolution_id,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	LaunchAttemptID string    `json:"launch_attempt_id,omitempty"`
	LaunchID        string    `json:"launch_id,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

type TaskShellActiveBranch struct {
	Class                  string `json:"class"`
	BranchRef              string `json:"branch_ref,omitempty"`
	ActionabilityAnchor    string `json:"actionability_anchor_kind,omitempty"`
	ActionabilityAnchorRef string `json:"actionability_anchor_ref,omitempty"`
	Reason                 string `json:"reason,omitempty"`
}

type TaskShellLocalResumeAuthority struct {
	State               string              `json:"state"`
	Mode                string              `json:"mode"`
	CheckpointID        common.CheckpointID `json:"checkpoint_id,omitempty"`
	RunID               common.RunID        `json:"run_id,omitempty"`
	BlockingBranchClass string              `json:"blocking_branch_class,omitempty"`
	BlockingBranchRef   string              `json:"blocking_branch_ref,omitempty"`
	Reason              string              `json:"reason,omitempty"`
}

type TaskShellLocalRunFinalization struct {
	State        string              `json:"state"`
	RunID        common.RunID        `json:"run_id,omitempty"`
	RunStatus    run.Status          `json:"run_status,omitempty"`
	CheckpointID common.CheckpointID `json:"checkpoint_id,omitempty"`
	Reason       string              `json:"reason,omitempty"`
}

type TaskShellOperatorActionAuthority struct {
	Action              string `json:"action"`
	State               string `json:"state"`
	Reason              string `json:"reason,omitempty"`
	BlockingBranchClass string `json:"blocking_branch_class,omitempty"`
	BlockingBranchRef   string `json:"blocking_branch_ref,omitempty"`
	AnchorKind          string `json:"anchor_kind,omitempty"`
	AnchorRef           string `json:"anchor_ref,omitempty"`
}

type TaskShellOperatorActionAuthoritySet struct {
	RequiredNextAction string                             `json:"required_next_action,omitempty"`
	Actions            []TaskShellOperatorActionAuthority `json:"actions,omitempty"`
}

type TaskShellOperatorDecisionBlockedAction struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type TaskShellOperatorDecisionSummary struct {
	ActiveOwnerClass   string                                   `json:"active_owner_class,omitempty"`
	ActiveOwnerRef     string                                   `json:"active_owner_ref,omitempty"`
	Headline           string                                   `json:"headline,omitempty"`
	RequiredNextAction string                                   `json:"required_next_action,omitempty"`
	PrimaryReason      string                                   `json:"primary_reason,omitempty"`
	Guidance           string                                   `json:"guidance,omitempty"`
	IntegrityNote      string                                   `json:"integrity_note,omitempty"`
	BlockedActions     []TaskShellOperatorDecisionBlockedAction `json:"blocked_actions,omitempty"`
}

type TaskShellOperatorExecutionStep struct {
	Action         string `json:"action"`
	Status         string `json:"status"`
	Domain         string `json:"domain,omitempty"`
	CommandSurface string `json:"command_surface,omitempty"`
	CommandHint    string `json:"command_hint,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type TaskShellOperatorExecutionPlan struct {
	PrimaryStep             *TaskShellOperatorExecutionStep  `json:"primary_step,omitempty"`
	MandatoryBeforeProgress bool                             `json:"mandatory_before_progress"`
	SecondarySteps          []TaskShellOperatorExecutionStep `json:"secondary_steps,omitempty"`
	BlockedSteps            []TaskShellOperatorExecutionStep `json:"blocked_steps,omitempty"`
}

type TaskShellOperatorStepReceipt struct {
	ReceiptID    string    `json:"receipt_id"`
	ActionHandle string    `json:"action_handle"`
	ResultClass  string    `json:"result_class"`
	Summary      string    `json:"summary,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type TaskShellHandoffContinuity struct {
	State                        string `json:"state"`
	Reason                       string `json:"reason,omitempty"`
	LaunchAttemptID              string `json:"launch_attempt_id,omitempty"`
	LaunchID                     string `json:"launch_id,omitempty"`
	AcknowledgmentID             string `json:"acknowledgment_id,omitempty"`
	AcknowledgmentStatus         string `json:"acknowledgment_status,omitempty"`
	AcknowledgmentSummary        string `json:"acknowledgment_summary,omitempty"`
	FollowThroughID              string `json:"follow_through_id,omitempty"`
	FollowThroughKind            string `json:"follow_through_kind,omitempty"`
	FollowThroughSummary         string `json:"follow_through_summary,omitempty"`
	ResolutionID                 string `json:"resolution_id,omitempty"`
	ResolutionKind               string `json:"resolution_kind,omitempty"`
	ResolutionSummary            string `json:"resolution_summary,omitempty"`
	DownstreamContinuationProven bool   `json:"downstream_continuation_proven"`
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
	TaskID                     common.TaskID                        `json:"task_id"`
	Goal                       string                               `json:"goal"`
	Phase                      string                               `json:"phase"`
	Status                     string                               `json:"status"`
	RepoAnchor                 RepoAnchor                           `json:"repo_anchor"`
	IntentClass                string                               `json:"intent_class,omitempty"`
	IntentSummary              string                               `json:"intent_summary,omitempty"`
	Brief                      *TaskShellBrief                      `json:"brief,omitempty"`
	Run                        *TaskShellRun                        `json:"run,omitempty"`
	Checkpoint                 *TaskShellCheckpoint                 `json:"checkpoint,omitempty"`
	Handoff                    *TaskShellHandoff                    `json:"handoff,omitempty"`
	Launch                     *TaskShellLaunch                     `json:"launch,omitempty"`
	LaunchControl              *TaskShellLaunchControl              `json:"launch_control,omitempty"`
	Acknowledgment             *TaskShellAcknowledgment             `json:"acknowledgment,omitempty"`
	FollowThrough              *TaskShellFollowThrough              `json:"follow_through,omitempty"`
	Resolution                 *TaskShellResolution                 `json:"resolution,omitempty"`
	ActiveBranch               *TaskShellActiveBranch               `json:"active_branch,omitempty"`
	LocalRunFinalization       *TaskShellLocalRunFinalization       `json:"local_run_finalization,omitempty"`
	LocalResume                *TaskShellLocalResumeAuthority       `json:"local_resume,omitempty"`
	ActionAuthority            *TaskShellOperatorActionAuthoritySet `json:"action_authority,omitempty"`
	OperatorDecision           *TaskShellOperatorDecisionSummary    `json:"operator_decision,omitempty"`
	OperatorExecutionPlan      *TaskShellOperatorExecutionPlan      `json:"operator_execution_plan,omitempty"`
	LatestOperatorStepReceipt  *TaskShellOperatorStepReceipt        `json:"latest_operator_step_receipt,omitempty"`
	RecentOperatorStepReceipts []TaskShellOperatorStepReceipt       `json:"recent_operator_step_receipts,omitempty"`
	HandoffContinuity          *TaskShellHandoffContinuity          `json:"handoff_continuity,omitempty"`
	Recovery                   *TaskShellRecovery                   `json:"recovery,omitempty"`
	RecentProofs               []TaskShellProof                     `json:"recent_proofs,omitempty"`
	RecentConversation         []TaskShellConversation              `json:"recent_conversation,omitempty"`
	LatestCanonicalResponse    string                               `json:"latest_canonical_response,omitempty"`
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

## internal/runtime/daemon/service.go
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

	"tuku/internal/domain/handoff"
	"tuku/internal/domain/operatorstep"
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

func ipcHandoffContinuity(in *orchestrator.HandoffContinuity) *ipc.TaskHandoffContinuity {
	if in == nil {
		return nil
	}
	return &ipc.TaskHandoffContinuity{
		TaskID:                       in.TaskID,
		HandoffID:                    in.HandoffID,
		TargetWorker:                 in.TargetWorker,
		State:                        string(in.State),
		LaunchAttemptID:              in.LaunchAttemptID,
		LaunchID:                     in.LaunchID,
		LaunchStatus:                 string(in.LaunchStatus),
		AcknowledgmentID:             in.AcknowledgmentID,
		AcknowledgmentStatus:         string(in.AcknowledgmentStatus),
		AcknowledgmentSummary:        in.AcknowledgmentSummary,
		FollowThroughID:              in.FollowThroughID,
		FollowThroughKind:            string(in.FollowThroughKind),
		FollowThroughSummary:         in.FollowThroughSummary,
		ResolutionID:                 in.ResolutionID,
		ResolutionKind:               string(in.ResolutionKind),
		ResolutionSummary:            in.ResolutionSummary,
		DownstreamContinuationProven: in.DownstreamContinuationProven,
		Reason:                       in.Reason,
	}
}

func ipcActiveBranch(in *orchestrator.ActiveBranchProvenance) *ipc.TaskActiveBranch {
	if in == nil {
		return nil
	}
	return &ipc.TaskActiveBranch{
		TaskID:                 in.TaskID,
		Class:                  string(in.Class),
		BranchRef:              in.BranchRef,
		ActionabilityAnchor:    string(in.ActionabilityAnchor),
		ActionabilityAnchorRef: in.ActionabilityAnchorRef,
		Reason:                 in.Reason,
	}
}

func ipcLocalResumeAuthority(in *orchestrator.LocalResumeAuthority) *ipc.TaskLocalResumeAuthority {
	if in == nil {
		return nil
	}
	return &ipc.TaskLocalResumeAuthority{
		TaskID:              in.TaskID,
		State:               string(in.State),
		Mode:                string(in.Mode),
		CheckpointID:        in.CheckpointID,
		RunID:               in.RunID,
		BlockingBranchClass: string(in.BlockingBranchClass),
		BlockingBranchRef:   in.BlockingBranchRef,
		Reason:              in.Reason,
	}
}

func ipcLocalRunFinalization(in *orchestrator.LocalRunFinalization) *ipc.TaskLocalRunFinalization {
	if in == nil {
		return nil
	}
	return &ipc.TaskLocalRunFinalization{
		TaskID:       in.TaskID,
		State:        string(in.State),
		RunID:        in.RunID,
		RunStatus:    in.RunStatus,
		CheckpointID: in.CheckpointID,
		Reason:       in.Reason,
	}
}

func ipcOperatorActionAuthoritySet(in *orchestrator.OperatorActionAuthoritySet) *ipc.TaskOperatorActionAuthoritySet {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorActionAuthoritySet{RequiredNextAction: string(in.RequiredNextAction)}
	if len(in.Actions) > 0 {
		out.Actions = make([]ipc.TaskOperatorActionAuthority, 0, len(in.Actions))
		for _, action := range in.Actions {
			out.Actions = append(out.Actions, ipc.TaskOperatorActionAuthority{
				Action:              string(action.Action),
				State:               string(action.State),
				Reason:              action.Reason,
				BlockingBranchClass: string(action.BlockingBranchClass),
				BlockingBranchRef:   action.BlockingBranchRef,
				AnchorKind:          string(action.AnchorKind),
				AnchorRef:           action.AnchorRef,
			})
		}
	}
	return out
}

func ipcOperatorActionAuthorities(in []orchestrator.OperatorActionAuthority) []ipc.TaskOperatorActionAuthority {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskOperatorActionAuthority, 0, len(in))
	for _, action := range in {
		out = append(out, ipc.TaskOperatorActionAuthority{
			Action:              string(action.Action),
			State:               string(action.State),
			Reason:              action.Reason,
			BlockingBranchClass: string(action.BlockingBranchClass),
			BlockingBranchRef:   action.BlockingBranchRef,
			AnchorKind:          string(action.AnchorKind),
			AnchorRef:           action.AnchorRef,
		})
	}
	return out
}

func ipcOperatorDecisionSummary(in *orchestrator.OperatorDecisionSummary) *ipc.TaskOperatorDecisionSummary {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorDecisionSummary{
		ActiveOwnerClass:   string(in.ActiveOwnerClass),
		ActiveOwnerRef:     in.ActiveOwnerRef,
		Headline:           in.Headline,
		RequiredNextAction: string(in.RequiredNextAction),
		PrimaryReason:      in.PrimaryReason,
		Guidance:           in.Guidance,
		IntegrityNote:      in.IntegrityNote,
	}
	if len(in.BlockedActions) > 0 {
		out.BlockedActions = make([]ipc.TaskOperatorDecisionBlockedAction, 0, len(in.BlockedActions))
		for _, blocked := range in.BlockedActions {
			out.BlockedActions = append(out.BlockedActions, ipc.TaskOperatorDecisionBlockedAction{
				Action: string(blocked.Action),
				Reason: blocked.Reason,
			})
		}
	}
	return out
}

func ipcOperatorStepReceipt(in *operatorstep.Receipt) *ipc.TaskOperatorStepReceipt {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorStepReceipt{
		ReceiptID:          in.ReceiptID,
		TaskID:             in.TaskID,
		ActionHandle:       in.ActionHandle,
		ExecutionDomain:    in.ExecutionDomain,
		CommandSurfaceKind: in.CommandSurfaceKind,
		ExecutionAttempted: in.ExecutionAttempted,
		ResultClass:        string(in.ResultClass),
		Summary:            in.Summary,
		Reason:             in.Reason,
		RunID:              in.RunID,
		CheckpointID:       in.CheckpointID,
		BriefID:            in.BriefID,
		HandoffID:          in.HandoffID,
		LaunchAttemptID:    in.LaunchAttemptID,
		LaunchID:           in.LaunchID,
		CreatedAt:          in.CreatedAt,
	}
	if in.CompletedAt != nil {
		out.CompletedAt = *in.CompletedAt
	}
	return out
}

func ipcOperatorStepReceipts(in []operatorstep.Receipt) []ipc.TaskOperatorStepReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskOperatorStepReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcOperatorStepReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcOperatorExecutionPlan(in *orchestrator.OperatorExecutionPlan) *ipc.TaskOperatorExecutionPlan {
	if in == nil {
		return nil
	}
	out := &ipc.TaskOperatorExecutionPlan{
		MandatoryBeforeProgress: in.MandatoryBeforeProgress,
	}
	if in.PrimaryStep != nil {
		out.PrimaryStep = &ipc.TaskOperatorExecutionStep{
			Action:         string(in.PrimaryStep.Action),
			Status:         string(in.PrimaryStep.Status),
			Domain:         string(in.PrimaryStep.Domain),
			CommandSurface: string(in.PrimaryStep.CommandSurface),
			CommandHint:    in.PrimaryStep.CommandHint,
			Reason:         in.PrimaryStep.Reason,
		}
	}
	if len(in.SecondarySteps) > 0 {
		out.SecondarySteps = make([]ipc.TaskOperatorExecutionStep, 0, len(in.SecondarySteps))
		for _, step := range in.SecondarySteps {
			out.SecondarySteps = append(out.SecondarySteps, ipc.TaskOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	if len(in.BlockedSteps) > 0 {
		out.BlockedSteps = make([]ipc.TaskOperatorExecutionStep, 0, len(in.BlockedSteps))
		for _, step := range in.BlockedSteps {
			out.BlockedSteps = append(out.BlockedSteps, ipc.TaskOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	return out
}

func ipcShellHandoffContinuity(in *orchestrator.ShellHandoffContinuitySummary) *ipc.TaskShellHandoffContinuity {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellHandoffContinuity{
		State:                        string(in.State),
		Reason:                       in.Reason,
		LaunchAttemptID:              in.LaunchAttemptID,
		LaunchID:                     in.LaunchID,
		AcknowledgmentID:             in.AcknowledgmentID,
		AcknowledgmentStatus:         string(in.AcknowledgmentStatus),
		AcknowledgmentSummary:        in.AcknowledgmentSummary,
		FollowThroughID:              in.FollowThroughID,
		FollowThroughKind:            string(in.FollowThroughKind),
		FollowThroughSummary:         in.FollowThroughSummary,
		ResolutionID:                 in.ResolutionID,
		ResolutionKind:               string(in.ResolutionKind),
		ResolutionSummary:            in.ResolutionSummary,
		DownstreamContinuationProven: in.DownstreamContinuationProven,
	}
}

func ipcShellActiveBranch(in *orchestrator.ShellActiveBranchSummary) *ipc.TaskShellActiveBranch {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellActiveBranch{
		Class:                  string(in.Class),
		BranchRef:              in.BranchRef,
		ActionabilityAnchor:    string(in.ActionabilityAnchor),
		ActionabilityAnchorRef: in.ActionabilityAnchorRef,
		Reason:                 in.Reason,
	}
}

func ipcShellLocalResumeAuthority(in *orchestrator.ShellLocalResumeAuthoritySummary) *ipc.TaskShellLocalResumeAuthority {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLocalResumeAuthority{
		State:               string(in.State),
		Mode:                string(in.Mode),
		CheckpointID:        in.CheckpointID,
		RunID:               in.RunID,
		BlockingBranchClass: string(in.BlockingBranchClass),
		BlockingBranchRef:   in.BlockingBranchRef,
		Reason:              in.Reason,
	}
}

func ipcShellLocalRunFinalization(in *orchestrator.ShellLocalRunFinalizationSummary) *ipc.TaskShellLocalRunFinalization {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellLocalRunFinalization{
		State:        string(in.State),
		RunID:        in.RunID,
		RunStatus:    in.RunStatus,
		CheckpointID: in.CheckpointID,
		Reason:       in.Reason,
	}
}

func ipcShellOperatorActionAuthoritySet(in *orchestrator.ShellOperatorActionAuthoritySet) *ipc.TaskShellOperatorActionAuthoritySet {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellOperatorActionAuthoritySet{RequiredNextAction: string(in.RequiredNextAction)}
	if len(in.Actions) > 0 {
		out.Actions = make([]ipc.TaskShellOperatorActionAuthority, 0, len(in.Actions))
		for _, action := range in.Actions {
			out.Actions = append(out.Actions, ipc.TaskShellOperatorActionAuthority{
				Action:              string(action.Action),
				State:               string(action.State),
				Reason:              action.Reason,
				BlockingBranchClass: string(action.BlockingBranchClass),
				BlockingBranchRef:   action.BlockingBranchRef,
				AnchorKind:          string(action.AnchorKind),
				AnchorRef:           action.AnchorRef,
			})
		}
	}
	return out
}

func ipcShellOperatorDecisionSummary(in *orchestrator.ShellOperatorDecisionSummary) *ipc.TaskShellOperatorDecisionSummary {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellOperatorDecisionSummary{
		ActiveOwnerClass:   string(in.ActiveOwnerClass),
		ActiveOwnerRef:     in.ActiveOwnerRef,
		Headline:           in.Headline,
		RequiredNextAction: string(in.RequiredNextAction),
		PrimaryReason:      in.PrimaryReason,
		Guidance:           in.Guidance,
		IntegrityNote:      in.IntegrityNote,
	}
	if len(in.BlockedActions) > 0 {
		out.BlockedActions = make([]ipc.TaskShellOperatorDecisionBlockedAction, 0, len(in.BlockedActions))
		for _, blocked := range in.BlockedActions {
			out.BlockedActions = append(out.BlockedActions, ipc.TaskShellOperatorDecisionBlockedAction{
				Action: string(blocked.Action),
				Reason: blocked.Reason,
			})
		}
	}
	return out
}

func ipcShellOperatorStepReceipt(in *operatorstep.Receipt) *ipc.TaskShellOperatorStepReceipt {
	if in == nil {
		return nil
	}
	return &ipc.TaskShellOperatorStepReceipt{
		ReceiptID:    in.ReceiptID,
		ActionHandle: in.ActionHandle,
		ResultClass:  string(in.ResultClass),
		Summary:      in.Summary,
		Reason:       in.Reason,
		CreatedAt:    in.CreatedAt,
	}
}

func ipcShellOperatorStepReceipts(in []operatorstep.Receipt) []ipc.TaskShellOperatorStepReceipt {
	if len(in) == 0 {
		return nil
	}
	out := make([]ipc.TaskShellOperatorStepReceipt, 0, len(in))
	for i := range in {
		if mapped := ipcShellOperatorStepReceipt(&in[i]); mapped != nil {
			out = append(out, *mapped)
		}
	}
	return out
}

func ipcShellOperatorExecutionPlan(in *orchestrator.ShellOperatorExecutionPlan) *ipc.TaskShellOperatorExecutionPlan {
	if in == nil {
		return nil
	}
	out := &ipc.TaskShellOperatorExecutionPlan{
		MandatoryBeforeProgress: in.MandatoryBeforeProgress,
	}
	if in.PrimaryStep != nil {
		out.PrimaryStep = &ipc.TaskShellOperatorExecutionStep{
			Action:         string(in.PrimaryStep.Action),
			Status:         string(in.PrimaryStep.Status),
			Domain:         string(in.PrimaryStep.Domain),
			CommandSurface: string(in.PrimaryStep.CommandSurface),
			CommandHint:    in.PrimaryStep.CommandHint,
			Reason:         in.PrimaryStep.Reason,
		}
	}
	if len(in.SecondarySteps) > 0 {
		out.SecondarySteps = make([]ipc.TaskShellOperatorExecutionStep, 0, len(in.SecondarySteps))
		for _, step := range in.SecondarySteps {
			out.SecondarySteps = append(out.SecondarySteps, ipc.TaskShellOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	if len(in.BlockedSteps) > 0 {
		out.BlockedSteps = make([]ipc.TaskShellOperatorExecutionStep, 0, len(in.BlockedSteps))
		for _, step := range in.BlockedSteps {
			out.BlockedSteps = append(out.BlockedSteps, ipc.TaskShellOperatorExecutionStep{
				Action:         string(step.Action),
				Status:         string(step.Status),
				Domain:         string(step.Domain),
				CommandSurface: string(step.CommandSurface),
				CommandHint:    step.CommandHint,
				Reason:         step.Reason,
			})
		}
	}
	return out
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
		latestResolutionAt := int64(0)
		if !out.LatestResolutionAt.IsZero() {
			latestResolutionAt = out.LatestResolutionAt.UnixMilli()
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
			LatestCheckpointID:               out.LatestCheckpointID,
			LatestCheckpointAtUnixMs:         latestCheckpointAt,
			LatestCheckpointTrigger:          string(out.LatestCheckpointTrigger),
			CheckpointResumable:              out.CheckpointResumable,
			ResumeDescriptor:                 out.ResumeDescriptor,
			LatestLaunchAttemptID:            out.LatestLaunchAttemptID,
			LatestLaunchID:                   out.LatestLaunchID,
			LatestLaunchStatus:               string(out.LatestLaunchStatus),
			LatestAcknowledgmentID:           out.LatestAcknowledgmentID,
			LatestAcknowledgmentStatus:       string(out.LatestAcknowledgmentStatus),
			LatestAcknowledgmentSummary:      out.LatestAcknowledgmentSummary,
			LatestFollowThroughID:            out.LatestFollowThroughID,
			LatestFollowThroughKind:          string(out.LatestFollowThroughKind),
			LatestFollowThroughSummary:       out.LatestFollowThroughSummary,
			LatestResolutionID:               out.LatestResolutionID,
			LatestResolutionKind:             string(out.LatestResolutionKind),
			LatestResolutionSummary:          out.LatestResolutionSummary,
			LatestResolutionAtUnixMs:         latestResolutionAt,
			LaunchControlState:               string(out.LaunchControlState),
			LaunchRetryDisposition:           string(out.LaunchRetryDisposition),
			LaunchControlReason:              out.LaunchControlReason,
			HandoffContinuityState:           string(out.HandoffContinuityState),
			HandoffContinuityReason:          out.HandoffContinuityReason,
			HandoffContinuationProven:        out.HandoffContinuationProven,
			ActiveBranchClass:                string(out.ActiveBranchClass),
			ActiveBranchRef:                  out.ActiveBranchRef,
			ActiveBranchAnchorKind:           string(out.ActiveBranchAnchorKind),
			ActiveBranchAnchorRef:            out.ActiveBranchAnchorRef,
			ActiveBranchReason:               out.ActiveBranchReason,
			LocalRunFinalizationState:        string(out.LocalRunFinalizationState),
			LocalRunFinalizationRunID:        out.LocalRunFinalizationRunID,
			LocalRunFinalizationStatus:       out.LocalRunFinalizationStatus,
			LocalRunFinalizationCheckpointID: out.LocalRunFinalizationCheckpointID,
			LocalRunFinalizationReason:       out.LocalRunFinalizationReason,
			LocalResumeAuthorityState:        string(out.LocalResumeAuthorityState),
			LocalResumeMode:                  string(out.LocalResumeMode),
			LocalResumeCheckpointID:          out.LocalResumeCheckpointID,
			LocalResumeRunID:                 out.LocalResumeRunID,
			LocalResumeReason:                out.LocalResumeReason,
			RequiredNextOperatorAction:       string(out.RequiredNextOperatorAction),
			ActionAuthority:                  ipcOperatorActionAuthorities(out.ActionAuthority),
			OperatorDecision:                 ipcOperatorDecisionSummary(out.OperatorDecision),
			OperatorExecutionPlan:            ipcOperatorExecutionPlan(out.OperatorExecutionPlan),
			LatestOperatorStepReceipt:        ipcOperatorStepReceipt(out.LatestOperatorStepReceipt),
			RecentOperatorStepReceipts:       ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			IsResumable:                      out.IsResumable,
			RecoveryClass:                    string(out.RecoveryClass),
			RecommendedAction:                string(out.RecommendedAction),
			ReadyForNextRun:                  out.ReadyForNextRun,
			ReadyForHandoffLaunch:            out.ReadyForHandoffLaunch,
			RecoveryReason:                   out.RecoveryReason,
			LatestRecoveryAction:             ipcRecoveryActionRecord(out.LatestRecoveryAction),
			LastEventType:                    string(out.LastEventType),
			LastEventID:                      out.LastEventID,
			LastEventAtUnixMs:                lastEventAt,
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
	case ipc.MethodReviewInterruptedRun:
		var p ipc.TaskReviewInterruptedRunRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordRecoveryAction(ctx, orchestrator.RecordRecoveryActionRequest{
			TaskID:  string(p.TaskID),
			Kind:    recoveryaction.KindInterruptedRunReviewed,
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("INTERRUPTED_REVIEW_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("INTERRUPTED_REVIEW_FAILED", "missing interrupted review action payload")
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
	case ipc.MethodExecuteRebrief:
		var p ipc.TaskRebriefRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteRebrief(ctx, orchestrator.ExecuteRebriefRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("REBRIEF_FAILED", err.Error())
		}
		return respondOK(ipc.TaskRebriefResponse{
			TaskID:                out.TaskID,
			PreviousBriefID:       out.PreviousBriefID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteInterruptedResume:
		var p ipc.TaskInterruptedResumeRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteInterruptedResume(ctx, orchestrator.ExecuteInterruptedResumeRequest{
			TaskID:  string(p.TaskID),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("INTERRUPTED_RESUME_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("INTERRUPTED_RESUME_FAILED", "missing interrupted resume action payload")
		}
		return respondOK(ipc.TaskInterruptedResumeResponse{
			TaskID:                out.TaskID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecuteContinueRecovery:
		var p ipc.TaskContinueRecoveryRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecuteContinueRecovery(ctx, orchestrator.ExecuteContinueRecoveryRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("CONTINUE_RECOVERY_FAILED", err.Error())
		}
		action := ipcRecoveryActionRecord(&out.Action)
		if action == nil {
			return respondErr("CONTINUE_RECOVERY_FAILED", "missing continue recovery action payload")
		}
		return respondOK(ipc.TaskContinueRecoveryResponse{
			TaskID:                out.TaskID,
			BriefID:               out.BriefID,
			BriefHash:             out.BriefHash,
			Action:                *action,
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodExecutePrimaryOperatorStep:
		var p ipc.TaskExecutePrimaryOperatorStepRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.ExecutePrimaryOperatorStep(ctx, orchestrator.ExecutePrimaryOperatorStepRequest{TaskID: string(p.TaskID)})
		if err != nil {
			return respondErr("OPERATOR_NEXT_FAILED", err.Error())
		}
		receipt := ipcOperatorStepReceipt(&out.Receipt)
		if receipt == nil {
			return respondErr("OPERATOR_NEXT_FAILED", "missing operator step receipt")
		}
		return respondOK(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID:                     out.TaskID,
			Receipt:                    *receipt,
			ActiveBranch:               ipcActiveBranch(&out.ActiveBranch),
			OperatorDecision:           ipcOperatorDecisionSummary(&out.OperatorDecision),
			OperatorExecutionPlan:      ipcOperatorExecutionPlan(&out.OperatorExecutionPlan),
			RecoveryClass:              string(out.RecoveryClass),
			RecommendedAction:          string(out.RecommendedAction),
			ReadyForNextRun:            out.ReadyForNextRun,
			ReadyForHandoffLaunch:      out.ReadyForHandoffLaunch,
			RecoveryReason:             out.RecoveryReason,
			CanonicalResponse:          out.CanonicalResponse,
			RecentOperatorStepReceipts: ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
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
			Intent:                     out.Intent,
			Brief:                      out.Brief,
			Run:                        out.Run,
			Checkpoint:                 out.Checkpoint,
			Handoff:                    out.Handoff,
			Launch:                     out.Launch,
			Acknowledgment:             out.Acknowledgment,
			FollowThrough:              out.FollowThrough,
			Resolution:                 out.Resolution,
			ActiveBranch:               ipcActiveBranch(out.ActiveBranch),
			LocalRunFinalization:       ipcLocalRunFinalization(out.LocalRunFinalization),
			LocalResumeAuthority:       ipcLocalResumeAuthority(out.LocalResumeAuthority),
			ActionAuthority:            ipcOperatorActionAuthoritySet(out.ActionAuthority),
			OperatorDecision:           ipcOperatorDecisionSummary(out.OperatorDecision),
			OperatorExecutionPlan:      ipcOperatorExecutionPlan(out.OperatorExecutionPlan),
			LatestOperatorStepReceipt:  ipcOperatorStepReceipt(out.LatestOperatorStepReceipt),
			RecentOperatorStepReceipts: ipcOperatorStepReceipts(out.RecentOperatorStepReceipts),
			LaunchControl:              ipcLaunchControl(out.LaunchControl),
			HandoffContinuity:          ipcHandoffContinuity(out.HandoffContinuity),
			Recovery:                   ipcRecoveryAssessment(out.Recovery),
			LatestRecoveryAction:       ipcRecoveryActionRecord(out.LatestRecoveryAction),
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
		if out.FollowThrough != nil {
			resp.FollowThrough = &ipc.TaskShellFollowThrough{
				RecordID:        out.FollowThrough.RecordID,
				Kind:            string(out.FollowThrough.Kind),
				Summary:         out.FollowThrough.Summary,
				LaunchAttemptID: out.FollowThrough.LaunchAttemptID,
				LaunchID:        out.FollowThrough.LaunchID,
				CreatedAt:       out.FollowThrough.CreatedAt,
			}
		}
		if out.Resolution != nil {
			resp.Resolution = &ipc.TaskShellResolution{
				ResolutionID:    out.Resolution.ResolutionID,
				Kind:            string(out.Resolution.Kind),
				Summary:         out.Resolution.Summary,
				LaunchAttemptID: out.Resolution.LaunchAttemptID,
				LaunchID:        out.Resolution.LaunchID,
				CreatedAt:       out.Resolution.CreatedAt,
			}
		}
		resp.ActiveBranch = ipcShellActiveBranch(out.ActiveBranch)
		resp.LocalRunFinalization = ipcShellLocalRunFinalization(out.LocalRunFinalization)
		resp.LocalResume = ipcShellLocalResumeAuthority(out.LocalResume)
		resp.ActionAuthority = ipcShellOperatorActionAuthoritySet(out.ActionAuthority)
		resp.OperatorDecision = ipcShellOperatorDecisionSummary(out.OperatorDecision)
		resp.OperatorExecutionPlan = ipcShellOperatorExecutionPlan(out.OperatorExecutionPlan)
		resp.LatestOperatorStepReceipt = ipcShellOperatorStepReceipt(out.LatestOperatorStepReceipt)
		resp.RecentOperatorStepReceipts = ipcShellOperatorStepReceipts(out.RecentOperatorStepReceipts)
		resp.HandoffContinuity = ipcShellHandoffContinuity(out.HandoffContinuity)
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
	case ipc.MethodRecordHandoffFollowThrough:
		var p ipc.TaskHandoffFollowThroughRecordRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordHandoffFollowThrough(ctx, orchestrator.RecordHandoffFollowThroughRequest{
			TaskID:  string(p.TaskID),
			Kind:    handoff.FollowThroughKind(p.Kind),
			Summary: p.Summary,
			Notes:   append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_FOLLOW_THROUGH_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffFollowThroughRecordResponse{
			TaskID:                out.TaskID,
			Record:                &out.Record,
			HandoffContinuity:     ipcHandoffContinuity(&out.HandoffContinuity),
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	case ipc.MethodRecordHandoffResolution:
		var p ipc.TaskHandoffResolutionRecordRequest
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return respondErr("BAD_PAYLOAD", err.Error())
		}
		out, err := s.Handler.RecordHandoffResolution(ctx, orchestrator.RecordHandoffResolutionRequest{
			TaskID:    string(p.TaskID),
			HandoffID: p.HandoffID,
			Kind:      handoff.ResolutionKind(p.Kind),
			Summary:   p.Summary,
			Notes:     append([]string{}, p.Notes...),
		})
		if err != nil {
			return respondErr("HANDOFF_RESOLUTION_FAILED", err.Error())
		}
		return respondOK(ipc.TaskHandoffResolutionRecordResponse{
			TaskID:                out.TaskID,
			Record:                &out.Record,
			HandoffContinuity:     ipcHandoffContinuity(&out.HandoffContinuity),
			RecoveryClass:         string(out.RecoveryClass),
			RecommendedAction:     string(out.RecommendedAction),
			ReadyForNextRun:       out.ReadyForNextRun,
			ReadyForHandoffLaunch: out.ReadyForHandoffLaunch,
			RecoveryReason:        out.RecoveryReason,
			CanonicalResponse:     out.CanonicalResponse,
		})
	default:
		return respondErr("UNSUPPORTED_METHOD", fmt.Sprintf("unsupported method: %s", req.Method))
	}
}
```

## internal/app/bootstrap.go
```go
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"tuku/internal/adapters/claude"
	"tuku/internal/adapters/codex"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	daemonruntime "tuku/internal/runtime/daemon"
	"tuku/internal/storage/sqlite"
	tukushell "tuku/internal/tui/shell"
)

// CLIApplication is the top-level command host for the user-facing Tuku CLI.
type CLIApplication struct {
	openShellFn         func(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error
	openFallbackShellFn func(ctx context.Context, cwd string, preference tukushell.WorkerPreference) error
}

type repoShellTaskResolution struct {
	TaskID   common.TaskID
	RepoRoot string
	Created  bool
}

// DaemonApplication is the top-level process host for the local Tuku daemon.
type DaemonApplication struct{}

func NewCLIApplication() *CLIApplication {
	return &CLIApplication{}
}

func NewDaemonApplication() *DaemonApplication {
	return &DaemonApplication{}
}

var (
	getWorkingDir          = os.Getwd
	resolveRepoRootFromDir = anchorgit.ResolveRepoRoot
	ipcCall                = ipc.CallUnix
	startLocalDaemon       = launchLocalDaemonProcess
	resolveScratchPath     = defaultScratchSessionPath
	daemonReadyTimeout     = 5 * time.Second
	daemonRetryInterval    = 150 * time.Millisecond
)

func (a *CLIApplication) Run(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		_, _ = fmt.Fprintln(os.Stdout, cliUsage())
		return nil
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return a.runPrimaryEntry(ctx, socketPath, nil)
	}

	switch args[0] {
	case "chat":
		return a.runPrimaryEntry(ctx, socketPath, args[1:])
	case "start":
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		goal := fs.String("goal", "", "task goal")
		repo := fs.String("repo", ".", "repo root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload, _ := json.Marshal(ipc.StartTaskRequest{Goal: *goal, RepoRoot: *repo})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodStartTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.StartTaskResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "message":
		fs := flag.NewFlagSet("message", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		message := fs.String("text", "", "user message")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *message == "" {
			return errors.New("--text is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task, "message": *message})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodSendMessage, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskMessageResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskStatus, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskStatusResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "shell":
		fs := flag.NewFlagSet("shell", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		preference, err := parseShellWorkerPreference(*worker)
		if err != nil {
			return err
		}
		return a.openShell(ctx, socketPath, *task, preference)

	case "shell-sessions":
		fs := flag.NewFlagSet("shell-sessions", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskShellSessions, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskShellSessionsResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		action := fs.String("action", "start", "run action: start|complete|interrupt")
		mode := fs.String("mode", "real", "run mode: real|noop")
		runID := fs.String("run-id", "", "run id for complete/interrupt actions")
		simInterrupt := fs.Bool("simulate-interrupt", false, "start then immediately interrupt")
		reason := fs.String("reason", "", "interruption reason")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id":             *task,
			"action":              *action,
			"mode":                *mode,
			"run_id":              *runID,
			"simulate_interrupt":  *simInterrupt,
			"interruption_reason": *reason,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskRun, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskRunResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "next":
		fs := flag.NewFlagSet("next", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{TaskID: common.TaskID(*task)})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecutePrimaryOperatorStep, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskExecutePrimaryOperatorStepResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "continue":
		fs := flag.NewFlagSet("continue", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskContinueRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodContinueTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskContinueResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "checkpoint":
		fs := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskCheckpointRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateCheckpoint, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskCheckpointResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "recovery":
		if len(args) < 2 {
			return errors.New("usage: tuku recovery <record|review-interrupted|resume-interrupted|rebrief|continue> ...")
		}
		switch args[1] {
		case "record":
			fs := flag.NewFlagSet("recovery record", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			action := fs.String("action", "", "recovery action kind")
			summary := fs.String("summary", "", "optional recovery action summary")
			note := fs.String("note", "", "optional recovery action note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			if *action == "" {
				return errors.New("--action is required")
			}
			kind, err := parseRecoveryActionKind(*action)
			if err != nil {
				return err
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionRequest{
				TaskID:  common.TaskID(*task),
				Kind:    string(kind),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordRecoveryAction, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRecordRecoveryActionResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "review-interrupted":
			fs := flag.NewFlagSet("recovery review-interrupted", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			summary := fs.String("summary", "", "optional interrupted recovery summary")
			note := fs.String("note", "", "optional interrupted recovery note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskReviewInterruptedRunRequest{
				TaskID:  common.TaskID(*task),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodReviewInterruptedRun, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRecordRecoveryActionResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "continue":
			fs := flag.NewFlagSet("recovery continue", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			payload, _ := json.Marshal(ipc.TaskContinueRecoveryRequest{TaskID: common.TaskID(*task)})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteContinueRecovery, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskContinueRecoveryResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "rebrief":
			fs := flag.NewFlagSet("recovery rebrief", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			payload, _ := json.Marshal(ipc.TaskRebriefRequest{TaskID: common.TaskID(*task)})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteRebrief, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRebriefResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		case "resume-interrupted":
			fs := flag.NewFlagSet("recovery resume-interrupted", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			summary := fs.String("summary", "", "optional interrupted resume summary")
			note := fs.String("note", "", "optional interrupted resume note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskInterruptedResumeRequest{
				TaskID:  common.TaskID(*task),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteInterruptedResume, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskInterruptedResumeResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		default:
			return fmt.Errorf("unknown recovery command: %s", args[1])
		}

	case "handoff-followthrough":
		fs := flag.NewFlagSet("handoff-followthrough", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		kindValue := fs.String("kind", "", "follow-through kind")
		summary := fs.String("summary", "", "optional follow-through summary")
		note := fs.String("note", "", "optional follow-through note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *kindValue == "" {
			return errors.New("--kind is required")
		}
		kind, err := parseHandoffFollowThroughKind(*kindValue)
		if err != nil {
			return err
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordRequest{
			TaskID:  common.TaskID(*task),
			Kind:    string(kind),
			Summary: strings.TrimSpace(*summary),
			Notes:   notes,
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordHandoffFollowThrough, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffFollowThroughRecordResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-create":
		fs := flag.NewFlagSet("handoff-create", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		target := fs.String("target", string(rundomain.WorkerKindClaude), "target worker (claude)")
		mode := fs.String("mode", string(handoff.ModeResume), "handoff mode: resume|review|takeover")
		reason := fs.String("reason", "", "handoff reason")
		note := fs.String("note", "", "optional handoff note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffCreateRequest{
			TaskID:       common.TaskID(*task),
			TargetWorker: rundomain.WorkerKind(*target),
			Reason:       *reason,
			Mode:         handoff.Mode(*mode),
			Notes:        notes,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffCreateResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-accept":
		fs := flag.NewFlagSet("handoff-accept", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id")
		acceptedBy := fs.String("by", string(rundomain.WorkerKindClaude), "accepted-by worker")
		note := fs.String("note", "", "optional acceptance note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *handoffID == "" {
			return errors.New("--handoff is required")
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffAcceptRequest{
			TaskID:     common.TaskID(*task),
			HandoffID:  *handoffID,
			AcceptedBy: rundomain.WorkerKind(*acceptedBy),
			Notes:      notes,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodAcceptHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffAcceptResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-launch":
		fs := flag.NewFlagSet("handoff-launch", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id (optional; defaults to latest for task)")
		target := fs.String("target", "", "target worker override (optional; must match packet target if set)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskHandoffLaunchRequest{
			TaskID:       common.TaskID(*task),
			HandoffID:    *handoffID,
			TargetWorker: rundomain.WorkerKind(*target),
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodLaunchHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffLaunchResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-resolve":
		fs := flag.NewFlagSet("handoff-resolve", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "optional handoff id")
		kindValue := fs.String("kind", "", "resolution kind")
		summary := fs.String("summary", "", "optional resolution summary")
		note := fs.String("note", "", "optional resolution note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *kindValue == "" {
			return errors.New("--kind is required")
		}
		kind, err := parseHandoffResolutionKind(*kindValue)
		if err != nil {
			return err
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordRequest{
			TaskID:    common.TaskID(*task),
			HandoffID: strings.TrimSpace(*handoffID),
			Kind:      string(kind),
			Summary:   strings.TrimSpace(*summary),
			Notes:     notes,
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordHandoffResolution, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffResolutionRecordResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "inspect":
		fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskInspect, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskInspectResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cliUsage() string {
	return "usage: tuku [chat] | tuku <start|message|shell|shell-sessions|run|continue|checkpoint|recovery|handoff-create|handoff-accept|handoff-launch|handoff-followthrough|handoff-resolve|status|inspect|help> [flags]"
}

func parseRecoveryActionKind(value string) (recoveryaction.Kind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "failed-run-reviewed":
		return recoveryaction.KindFailedRunReviewed, nil
	case "interrupted-run-reviewed":
		return recoveryaction.KindInterruptedRunReviewed, nil
	case "validation-reviewed":
		return recoveryaction.KindValidationReviewed, nil
	case "decision-continue":
		return recoveryaction.KindDecisionContinue, nil
	case "decision-regenerate-brief":
		return recoveryaction.KindDecisionRegenerateBrief, nil
	case "repair-intent-recorded":
		return recoveryaction.KindRepairIntentRecorded, nil
	case "pending-launch-reviewed":
		return recoveryaction.KindPendingLaunchReviewed, nil
	default:
		return "", fmt.Errorf("unsupported recovery action %q", value)
	}
}

func parseHandoffFollowThroughKind(value string) (handoff.FollowThroughKind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "proof-of-life-observed":
		return handoff.FollowThroughProofOfLifeObserved, nil
	case "continuation-confirmed":
		return handoff.FollowThroughContinuationConfirmed, nil
	case "continuation-unknown":
		return handoff.FollowThroughContinuationUnknown, nil
	case "stalled-review-required":
		return handoff.FollowThroughStalledReviewRequired, nil
	default:
		return "", fmt.Errorf("unsupported handoff follow-through kind %q", value)
	}
}

func parseHandoffResolutionKind(value string) (handoff.ResolutionKind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "abandoned":
		return handoff.ResolutionAbandoned, nil
	case "superseded-by-local":
		return handoff.ResolutionSupersededByLocal, nil
	case "closed-unproven":
		return handoff.ResolutionClosedUnproven, nil
	case "reviewed-stale":
		return handoff.ResolutionReviewedStale, nil
	default:
		return "", fmt.Errorf("unsupported handoff resolution kind %q", value)
	}
}

func (a *DaemonApplication) Run(ctx context.Context) error {
	dbPath, err := defaultDBPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	coord, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:           store,
		IntentCompiler:  orchestrator.NewIntentStubCompiler(),
		BriefBuilder:    orchestrator.NewBriefBuilderV1(nil, nil),
		WorkerAdapter:   codex.NewAdapter(),
		HandoffLauncher: claude.NewLauncher(),
		Synthesizer:     canonical.NewSimpleSynthesizer(),
		AnchorProvider:  anchorgit.NewGitProvider(),
		ShellSessions:   store.ShellSessions(),
	})
	if err != nil {
		return err
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		return err
	}
	service := daemonruntime.NewService(socketPath, coord)
	return service.Run(ctx)
}

func defaultDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Tuku"), nil
}

func defaultDBPath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tuku.db"), nil
}

func defaultSocketPath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "run", "tukud.sock"), nil
}

func requestID() string {
	return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
}

func (a *CLIApplication) runPrimaryEntry(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
	if err := fs.Parse(args); err != nil {
		return err
	}
	preference, err := parseShellWorkerPreference(*worker)
	if err != nil {
		return err
	}
	cwd, repoRoot, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return err
	}
	if !repoDetected {
		openFallback := a.openPrimaryFallbackShell
		if a.openFallbackShellFn != nil {
			openFallback = a.openFallbackShellFn
		}
		return openFallback(ctx, cwd, preference)
	}
	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(ctx, socketPath, repoRoot, orchestrator.DefaultRepoContinueGoal)
	if err != nil {
		return err
	}
	if a.openShellFn != nil {
		return a.openShellFn(ctx, socketPath, string(resolution.TaskID), preference)
	}
	source, err := newPrimaryRepoSnapshotSource(socketPath, repoRoot, resolution.Created)
	if err != nil {
		return err
	}
	return a.openShellWithSource(ctx, string(resolution.TaskID), preference, source)
}

func (a *CLIApplication) openPrimaryFallbackShell(ctx context.Context, cwd string, _ tukushell.WorkerPreference) error {
	scratchPath, err := resolveScratchPath(cwd)
	if err != nil {
		return err
	}
	return newPrimaryScratchIntake(cwd, scratchPath).Run(ctx)
}

func (a *CLIApplication) openShell(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error {
	return a.openShellWithSource(ctx, taskID, preference, tukushell.NewIPCSnapshotSource(socketPath))
}

func (a *CLIApplication) openShellWithSource(ctx context.Context, taskID string, preference tukushell.WorkerPreference, source tukushell.SnapshotSource) error {
	shellApp := tukushell.NewApp(taskID, source)
	shellApp.WorkerPreference = preference
	if socketPath := snapshotSourceSocketPath(source); socketPath != "" {
		shellApp.MessageSender = tukushell.NewIPCTaskMessageSender(socketPath)
		shellApp.ActionExecutor = tukushell.NewIPCPrimaryActionExecutor(socketPath)
		shellApp.LifecycleSink = tukushell.NewIPCLifecycleSink(socketPath)
		shellApp.RegistrySink = tukushell.NewIPCSessionRegistryClient(socketPath)
		shellApp.RegistrySource = tukushell.NewIPCSessionRegistryClient(socketPath)
	}
	return shellApp.Run(ctx)
}

func resolvePrimaryEntryContext(ctx context.Context) (string, string, bool, error) {
	cwd, err := getWorkingDir()
	if err != nil {
		return "", "", false, err
	}
	root, err := resolveRepoRootFromDir(ctx, cwd)
	if err != nil {
		return cwd, "", false, nil
	}
	return cwd, root, true, nil
}

func resolveCurrentRepoRoot(ctx context.Context) (string, error) {
	_, root, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return "", err
	}
	if !repoDetected {
		return "", fmt.Errorf("tuku needs a git repository for the primary entry path; current directory is not inside one")
	}
	return root, nil
}

func resolveShellTaskForRepo(ctx context.Context, socketPath string, repoRoot string, defaultGoal string) (repoShellTaskResolution, error) {
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoRequest{
		RepoRoot:    repoRoot,
		DefaultGoal: defaultGoal,
	})
	if err != nil {
		return repoShellTaskResolution{}, err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodResolveShellTaskForRepo,
		Payload:   payload,
	})
	if err != nil {
		return repoShellTaskResolution{}, err
	}
	var out ipc.ResolveShellTaskForRepoResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return repoShellTaskResolution{}, err
	}
	return repoShellTaskResolution{
		TaskID:   out.TaskID,
		RepoRoot: out.RepoRoot,
		Created:  out.Created,
	}, nil
}

func resolveShellTaskForRepoWithDaemonBootstrap(ctx context.Context, socketPath string, repoRoot string, defaultGoal string) (repoShellTaskResolution, error) {
	resolution, err := resolveShellTaskForRepo(ctx, socketPath, repoRoot, defaultGoal)
	if err == nil {
		return resolution, nil
	}
	if !isDaemonUnavailableError(err) {
		return repoShellTaskResolution{}, err
	}

	waitCh, err := startLocalDaemon()
	if err != nil {
		return repoShellTaskResolution{}, fmt.Errorf("could not start the local Tuku daemon automatically: %w", err)
	}

	deadline := time.Now().Add(daemonReadyTimeout)
	for {
		resolution, err = resolveShellTaskForRepo(ctx, socketPath, repoRoot, defaultGoal)
		if err == nil {
			return resolution, nil
		}
		if !isDaemonUnavailableError(err) {
			return repoShellTaskResolution{}, err
		}
		select {
		case waitErr, ok := <-waitCh:
			if ok && waitErr != nil {
				return repoShellTaskResolution{}, fmt.Errorf("local Tuku daemon failed to start: %w", waitErr)
			}
			return repoShellTaskResolution{}, errors.New("local Tuku daemon exited before becoming ready")
		default:
		}
		if time.Now().After(deadline) {
			return repoShellTaskResolution{}, fmt.Errorf("local Tuku daemon did not become ready within %s", daemonReadyTimeout)
		}
		if err := sleepWithContext(ctx, daemonRetryInterval); err != nil {
			return repoShellTaskResolution{}, err
		}
	}
}

func isDaemonUnavailableError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENOTCONN)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func launchLocalDaemonProcess() (<-chan error, error) {
	spec, err := resolveDaemonLaunchSpec()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	if spec.WorkingDir != "" {
		cmd.Dir = spec.WorkingDir
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch %s: %w", spec.Label, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				err = fmt.Errorf("%w: %s", err, msg)
			}
		}
		waitCh <- err
		close(waitCh)
	}()
	return waitCh, nil
}

type daemonLaunchSpec struct {
	Command    string
	Args       []string
	WorkingDir string
	Label      string
}

func resolveDaemonLaunchSpec() (daemonLaunchSpec, error) {
	if path, err := exec.LookPath("tukud"); err == nil {
		return daemonLaunchSpec{
			Command: path,
			Label:   path,
		}, nil
	}
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "tukud")
		if fileExists(sibling) {
			return daemonLaunchSpec{
				Command: sibling,
				Label:   sibling,
			}, nil
		}
	}
	if root, ok := sourceTreeRoot(); ok {
		goBin, err := exec.LookPath("go")
		if err == nil {
			return daemonLaunchSpec{
				Command:    goBin,
				Args:       []string{"run", "./cmd/tukud"},
				WorkingDir: root,
				Label:      "go run ./cmd/tukud",
			}, nil
		}
	}
	return daemonLaunchSpec{}, errors.New("could not locate `tukud`; build or install it, or continue starting `tukud` manually")
}

func sourceTreeRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if !fileExists(filepath.Join(root, "go.mod")) {
		return "", false
	}
	if !fileExists(filepath.Join(root, "cmd", "tukud", "main.go")) {
		return "", false
	}
	return root, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func defaultScratchSessionPath(cwd string) (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	normalized := filepath.Clean(strings.TrimSpace(cwd))
	sum := sha256.Sum256([]byte(normalized))
	return filepath.Join(root, "scratch", fmt.Sprintf("%x.json", sum[:])), nil
}

type primaryRepoScratchBridge struct {
	RepoRoot string
	Notes    []tukushell.ConversationItem
}

type primaryRepoScratchBridgeSource struct {
	base   tukushell.SnapshotSource
	bridge *primaryRepoScratchBridge
}

func snapshotSourceSocketPath(source tukushell.SnapshotSource) string {
	switch src := source.(type) {
	case *tukushell.IPCSnapshotSource:
		return src.SocketPath
	case *primaryRepoScratchBridgeSource:
		return snapshotSourceSocketPath(src.base)
	default:
		return ""
	}
}

func newPrimaryRepoSnapshotSource(socketPath string, repoRoot string, created bool) (tukushell.SnapshotSource, error) {
	base := tukushell.NewIPCSnapshotSource(socketPath)
	if !created {
		return base, nil
	}
	bridge, err := loadPrimaryRepoScratchBridge(repoRoot)
	if err != nil {
		return nil, err
	}
	if bridge == nil {
		return base, nil
	}
	return &primaryRepoScratchBridgeSource{
		base:   base,
		bridge: bridge,
	}, nil
}

func loadPrimaryRepoScratchBridge(repoRoot string) (*primaryRepoScratchBridge, error) {
	scratchPath, err := resolveScratchPath(repoRoot)
	if err != nil {
		return nil, err
	}
	notes, err := tukushell.LoadLocalScratchNotes(scratchPath)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, nil
	}
	return &primaryRepoScratchBridge{
		RepoRoot: filepath.Clean(strings.TrimSpace(repoRoot)),
		Notes:    notes,
	}, nil
}

func (s *primaryRepoScratchBridgeSource) Load(taskID string) (tukushell.Snapshot, error) {
	snapshot, err := s.base.Load(taskID)
	if err != nil {
		return tukushell.Snapshot{}, err
	}
	return applyPrimaryRepoScratchBridge(snapshot, s.bridge), nil
}

func applyPrimaryRepoScratchBridge(snapshot tukushell.Snapshot, bridge *primaryRepoScratchBridge) tukushell.Snapshot {
	if bridge == nil || len(bridge.Notes) == 0 {
		return snapshot
	}
	surfacedNotes := surfacedScratchBridgeNotes(bridge.Notes, 3)
	out := snapshot
	out.LocalScratch = &tukushell.LocalScratchContext{
		RepoRoot: bridge.RepoRoot,
		Notes:    surfacedNotes,
	}
	out.RecentConversation = append([]tukushell.ConversationItem{}, snapshot.RecentConversation...)
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Local scratch notes were found for this repo root when this task was first created. They have not been imported into canonical task state.",
	})
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Use the shell adopt command to stage them into a pending task message. Sending that pending message is the explicit adoption step into real Tuku continuity.",
	})
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Shell commands: stage local scratch with `a`, send the pending task message with `m`, clear it with `x`. When worker input is live, press Ctrl-G before the command key.",
	})
	for _, note := range surfacedNotes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
			Role:      "system",
			Body:      "local scratch note: " + body,
			CreatedAt: note.CreatedAt,
		})
	}
	return out
}

func surfacedScratchBridgeNotes(notes []tukushell.ConversationItem, limit int) []tukushell.ConversationItem {
	if limit <= 0 || len(notes) <= limit {
		return append([]tukushell.ConversationItem{}, notes...)
	}
	start := len(notes) - limit
	return append([]tukushell.ConversationItem{}, notes[start:]...)
}

func parseShellWorkerPreference(raw string) (tukushell.WorkerPreference, error) {
	preference, err := tukushell.ParseWorkerPreference(raw)
	if err != nil {
		return "", fmt.Errorf("invalid --worker: %w", err)
	}
	return preference, nil
}

func writeJSON(out *os.File, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func primaryEntryScratchSnapshot(cwd string) tukushell.Snapshot {
	message := "No git repository was detected. Tuku opened a local scratch and intake session instead of repo-backed continuity."
	return tukushell.Snapshot{
		Goal:                    "Local scratch and intake session",
		Phase:                   "SCRATCH_INTAKE",
		Status:                  "LOCAL_ONLY",
		IntentClass:             "scratch",
		IntentSummary:           fmt.Sprintf("Use this local scratch session to plan work, sketch a new project, or prepare to clone or initialize a repository. Current directory: %s", cwd),
		LatestCanonicalResponse: message,
		RecentConversation: []tukushell.ConversationItem{
			{
				Role: "system",
				Body: message,
			},
			{
				Role: "system",
				Body: "This session is local-only. Tuku is not starting the daemon, not creating a task, and not claiming repo-backed continuity here.",
			},
			{
				Role: "system",
				Body: "Good uses for this mode: outline a new project, define milestones, list requirements, or prepare the next step before a repository exists.",
			},
			{
				Role: "system",
				Body: "Type one line and press Enter to save a local scratch note on this machine. Use /help, /list, or /quit as needed. This is scratch history only, not a Tuku task.",
			},
			{
				Role: "system",
				Body: fmt.Sprintf("Current directory: %s", cwd),
			},
		},
	}
}
```

## internal/tui/shell/types.go
```go
package shell

import (
	"context"
	"time"
)

type Snapshot struct {
	TaskID                     string
	Goal                       string
	Phase                      string
	Status                     string
	Repo                       RepoAnchor
	LocalScratch               *LocalScratchContext
	IntentClass                string
	IntentSummary              string
	Brief                      *BriefSummary
	Run                        *RunSummary
	Checkpoint                 *CheckpointSummary
	Handoff                    *HandoffSummary
	Launch                     *LaunchSummary
	LaunchControl              *LaunchControlSummary
	Acknowledgment             *AcknowledgmentSummary
	FollowThrough              *FollowThroughSummary
	Resolution                 *ResolutionSummary
	ActiveBranch               *ActiveBranchSummary
	LocalRunFinalization       *LocalRunFinalizationSummary
	LocalResume                *LocalResumeAuthoritySummary
	ActionAuthority            *OperatorActionAuthoritySet
	OperatorDecision           *OperatorDecisionSummary
	OperatorExecutionPlan      *OperatorExecutionPlan
	LatestOperatorStepReceipt  *OperatorStepReceiptSummary
	RecentOperatorStepReceipts []OperatorStepReceiptSummary
	HandoffContinuity          *HandoffContinuitySummary
	Recovery                   *RecoverySummary
	RecentProofs               []ProofItem
	RecentConversation         []ConversationItem
	LatestCanonicalResponse    string
}

type RepoAnchor struct {
	RepoRoot         string
	Branch           string
	HeadSHA          string
	WorkingTreeDirty bool
	CapturedAt       time.Time
}

type LocalScratchContext struct {
	RepoRoot string
	Notes    []ConversationItem
}

type BriefSummary struct {
	ID               string
	Objective        string
	NormalizedAction string
	Constraints      []string
	DoneCriteria     []string
}

type RunSummary struct {
	ID                 string
	WorkerKind         string
	Status             string
	LastKnownSummary   string
	StartedAt          time.Time
	EndedAt            *time.Time
	InterruptionReason string
}

type CheckpointSummary struct {
	ID               string
	Trigger          string
	CreatedAt        time.Time
	ResumeDescriptor string
	IsResumable      bool
}

type HandoffSummary struct {
	ID           string
	Status       string
	SourceWorker string
	TargetWorker string
	Mode         string
	Reason       string
	AcceptedBy   string
	CreatedAt    time.Time
}

type LaunchSummary struct {
	AttemptID         string
	LaunchID          string
	Status            string
	RequestedAt       time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	Summary           string
	ErrorMessage      string
	OutputArtifactRef string
}

type LaunchControlSummary struct {
	State            string
	RetryDisposition string
	Reason           string
	HandoffID        string
	AttemptID        string
	LaunchID         string
	TargetWorker     string
	RequestedAt      time.Time
	CompletedAt      time.Time
	FailedAt         time.Time
}

type AcknowledgmentSummary struct {
	Status    string
	Summary   string
	CreatedAt time.Time
}

type FollowThroughSummary struct {
	RecordID        string
	Kind            string
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ResolutionSummary struct {
	ResolutionID    string
	Kind            string
	Summary         string
	LaunchAttemptID string
	LaunchID        string
	CreatedAt       time.Time
}

type ActiveBranchSummary struct {
	Class                  string
	BranchRef              string
	ActionabilityAnchor    string
	ActionabilityAnchorRef string
	Reason                 string
}

type LocalRunFinalizationSummary struct {
	State        string
	RunID        string
	RunStatus    string
	CheckpointID string
	Reason       string
}

type LocalResumeAuthoritySummary struct {
	State               string
	Mode                string
	CheckpointID        string
	RunID               string
	BlockingBranchClass string
	BlockingBranchRef   string
	Reason              string
}

type OperatorActionAuthority struct {
	Action              string
	State               string
	Reason              string
	BlockingBranchClass string
	BlockingBranchRef   string
	AnchorKind          string
	AnchorRef           string
}

type OperatorActionAuthoritySet struct {
	RequiredNextAction string
	Actions            []OperatorActionAuthority
}

type OperatorDecisionBlockedAction struct {
	Action string
	Reason string
}

type OperatorDecisionSummary struct {
	ActiveOwnerClass   string
	ActiveOwnerRef     string
	Headline           string
	RequiredNextAction string
	PrimaryReason      string
	Guidance           string
	IntegrityNote      string
	BlockedActions     []OperatorDecisionBlockedAction
}

type OperatorExecutionStep struct {
	Action         string
	Status         string
	Domain         string
	CommandSurface string
	CommandHint    string
	Reason         string
}

type OperatorExecutionPlan struct {
	PrimaryStep             *OperatorExecutionStep
	MandatoryBeforeProgress bool
	SecondarySteps          []OperatorExecutionStep
	BlockedSteps            []OperatorExecutionStep
}

type OperatorStepReceiptSummary struct {
	ReceiptID          string
	TaskID             string
	ActionHandle       string
	ExecutionDomain    string
	CommandSurfaceKind string
	ExecutionAttempted bool
	ResultClass        string
	Summary            string
	Reason             string
	RunID              string
	CheckpointID       string
	BriefID            string
	HandoffID          string
	LaunchAttemptID    string
	LaunchID           string
	CreatedAt          time.Time
	CompletedAt        time.Time
}

type HandoffContinuitySummary struct {
	State                        string
	Reason                       string
	LaunchAttemptID              string
	LaunchID                     string
	AcknowledgmentID             string
	AcknowledgmentStatus         string
	AcknowledgmentSummary        string
	FollowThroughID              string
	FollowThroughKind            string
	FollowThroughSummary         string
	ResolutionID                 string
	ResolutionKind               string
	ResolutionSummary            string
	DownstreamContinuationProven bool
}

type RecoveryIssue struct {
	Code    string
	Message string
}

type RecoverySummary struct {
	ContinuityOutcome      string
	Class                  string
	Action                 string
	ReadyForNextRun        bool
	ReadyForHandoffLaunch  bool
	RequiresDecision       bool
	RequiresRepair         bool
	RequiresReview         bool
	RequiresReconciliation bool
	DriftClass             string
	Reason                 string
	CheckpointID           string
	RunID                  string
	HandoffID              string
	HandoffStatus          string
	Issues                 []RecoveryIssue
}

type ProofItem struct {
	ID        string
	Type      string
	Summary   string
	Timestamp time.Time
}

type ConversationItem struct {
	Role      string
	Body      string
	CreatedAt time.Time
}

type HostMode string

const (
	HostModeCodexPTY   HostMode = "codex-pty"
	HostModeClaudePTY  HostMode = "claude-pty"
	HostModeTranscript HostMode = "transcript"
)

type HostState string

const (
	HostStateStarting       HostState = "starting"
	HostStateLive           HostState = "live"
	HostStateExited         HostState = "exited"
	HostStateFailed         HostState = "failed"
	HostStateFallback       HostState = "fallback"
	HostStateTranscriptOnly HostState = "transcript-only"
)

type HostStatus struct {
	Mode           HostMode
	State          HostState
	Label          string
	Note           string
	InputLive      bool
	ExitCode       *int
	Width          int
	Height         int
	LastOutputAt   time.Time
	StateChangedAt time.Time
}

type SessionEventType string

const (
	SessionEventShellStarted                  SessionEventType = "shell_started"
	SessionEventHostStartupAttempted          SessionEventType = "host_startup_attempted"
	SessionEventHostLive                      SessionEventType = "host_live"
	SessionEventResizeApplied                 SessionEventType = "resize_applied"
	SessionEventHostExited                    SessionEventType = "host_exited"
	SessionEventHostFailed                    SessionEventType = "host_failed"
	SessionEventFallbackActivated             SessionEventType = "fallback_activated"
	SessionEventManualRefresh                 SessionEventType = "manual_refresh"
	SessionEventPendingMessageStaged          SessionEventType = "pending_message_staged"
	SessionEventPendingMessageEditStarted     SessionEventType = "pending_message_edit_started"
	SessionEventPendingMessageEditSaved       SessionEventType = "pending_message_edit_saved"
	SessionEventPendingMessageEditCanceled    SessionEventType = "pending_message_edit_canceled"
	SessionEventPendingMessageSent            SessionEventType = "pending_message_sent"
	SessionEventPendingMessageCleared         SessionEventType = "pending_message_cleared"
	SessionEventPrimaryOperatorActionStarted  SessionEventType = "primary_operator_action_started"
	SessionEventPrimaryOperatorActionExecuted SessionEventType = "primary_operator_action_executed"
	SessionEventPrimaryOperatorActionFailed   SessionEventType = "primary_operator_action_failed"
	SessionEventPriorPersistedProof           SessionEventType = "prior_persisted_proof"
)

type SessionEvent struct {
	Type      SessionEventType
	Summary   string
	CreatedAt time.Time
}

type SessionState struct {
	SessionID             string
	StartedAt             time.Time
	WorkerPreference      WorkerPreference
	ResolvedWorker        WorkerPreference
	WorkerSessionID       string
	AttachCapability      WorkerAttachCapability
	Journal               []SessionEvent
	KnownSessions         []KnownShellSession
	PriorPersistedSummary string
}

type WorkerAttachCapability string

const (
	WorkerAttachCapabilityNone       WorkerAttachCapability = "none"
	WorkerAttachCapabilityAttachable WorkerAttachCapability = "attachable"
)

type KnownShellSessionClass string

const (
	KnownShellSessionClassAttachable         KnownShellSessionClass = "attachable"
	KnownShellSessionClassActiveUnattachable KnownShellSessionClass = "active_unattachable"
	KnownShellSessionClassStale              KnownShellSessionClass = "stale"
	KnownShellSessionClassEnded              KnownShellSessionClass = "ended"
)

type KnownShellSession struct {
	SessionID        string
	TaskID           string
	WorkerPreference WorkerPreference
	ResolvedWorker   WorkerPreference
	WorkerSessionID  string
	AttachCapability WorkerAttachCapability
	HostMode         HostMode
	HostState        HostState
	SessionClass     KnownShellSessionClass
	StartedAt        time.Time
	LastUpdatedAt    time.Time
	Active           bool
	Note             string
}

type FocusPane int

const (
	FocusWorker FocusPane = iota
	FocusInspector
	FocusActivity
)

type UIState struct {
	ShowInspector                  bool
	ShowProof                      bool
	ShowHelp                       bool
	ShowStatus                     bool
	Focus                          FocusPane
	EscapePrefix                   bool
	PendingTaskMessage             string
	PendingTaskMessageSource       string
	PendingTaskMessageEditMode     bool
	PendingTaskMessageEditBuffer   string
	PendingTaskMessageEditOriginal string
	Session                        SessionState
	LastRefresh                    time.Time
	ObservedAt                     time.Time
	LastError                      string
	PrimaryActionInFlight          *PrimaryActionInFlightSummary
	LastPrimaryActionResult        *PrimaryActionResultSummary
}

type PrimaryActionInFlightSummary struct {
	Action    string
	StartedAt time.Time
}

type PrimaryActionResultSummary struct {
	Action      string
	Outcome     string
	Summary     string
	Deltas      []string
	NextStep    string
	ErrorText   string
	ReceiptID   string
	ResultClass string
	CreatedAt   time.Time
}

type ViewModel struct {
	Header     HeaderView
	WorkerPane PaneView
	Inspector  *InspectorView
	ProofStrip *StripView
	Footer     string
	Overlay    *OverlayView
	Layout     shellLayout
}

type HeaderView struct {
	Title      string
	TaskLabel  string
	Phase      string
	Worker     string
	Repo       string
	Continuity string
}

type PaneView struct {
	Title   string
	Lines   []string
	Focused bool
}

type InspectorView struct {
	Title    string
	Sections []SectionView
	Focused  bool
}

type SectionView struct {
	Title string
	Lines []string
}

type StripView struct {
	Title   string
	Lines   []string
	Focused bool
}

type OverlayView struct {
	Title string
	Lines []string
}

type SnapshotSource interface {
	Load(taskID string) (Snapshot, error)
}

type WorkerHost interface {
	Start(ctx context.Context, snapshot Snapshot) error
	Stop() error
	UpdateSnapshot(snapshot Snapshot)
	Resize(width int, height int) bool
	CanAcceptInput() bool
	WriteInput(data []byte) bool
	Status() HostStatus
	Title() string
	WorkerLabel() string
	Lines(height int, width int) []string
	ActivityLines(limit int) []string
}

func (s Snapshot) RunWorkerKind() string {
	if s.Run == nil {
		return ""
	}
	return s.Run.WorkerKind
}

func (s Snapshot) HandoffTargetWorker() string {
	if s.Handoff == nil {
		return ""
	}
	return s.Handoff.TargetWorker
}

func (s Snapshot) HasLocalScratchAdoption() bool {
	return s.LocalScratch != nil && len(s.LocalScratch.Notes) > 0
}
```

## internal/tui/shell/adapter.go
```go
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

type IPCSnapshotSource struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCSnapshotSource(socketPath string) *IPCSnapshotSource {
	return &IPCSnapshotSource{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (s *IPCSnapshotSource) Load(taskID string) (Snapshot, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: ipcTaskID(taskID)})
	if err != nil {
		return Snapshot{}, err
	}
	resp, err := ipc.CallUnix(ctx, s.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellSnapshot,
		Payload:   payload,
	})
	if err != nil {
		return Snapshot{}, err
	}
	var raw ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &raw); err != nil {
		return Snapshot{}, err
	}
	return snapshotFromIPC(raw), nil
}

func snapshotFromIPC(raw ipc.TaskShellSnapshotResponse) Snapshot {
	out := Snapshot{
		TaskID:        string(raw.TaskID),
		Goal:          raw.Goal,
		Phase:         raw.Phase,
		Status:        raw.Status,
		IntentClass:   raw.IntentClass,
		IntentSummary: raw.IntentSummary,
		Repo: RepoAnchor{
			RepoRoot:         raw.RepoAnchor.RepoRoot,
			Branch:           raw.RepoAnchor.Branch,
			HeadSHA:          raw.RepoAnchor.HeadSHA,
			WorkingTreeDirty: raw.RepoAnchor.WorkingTreeDirty,
			CapturedAt:       raw.RepoAnchor.CapturedAt,
		},
		LatestCanonicalResponse: raw.LatestCanonicalResponse,
	}
	if raw.Brief != nil {
		out.Brief = &BriefSummary{
			ID:               string(raw.Brief.BriefID),
			Objective:        raw.Brief.Objective,
			NormalizedAction: raw.Brief.NormalizedAction,
			Constraints:      append([]string{}, raw.Brief.Constraints...),
			DoneCriteria:     append([]string{}, raw.Brief.DoneCriteria...),
		}
	}
	if raw.Run != nil {
		out.Run = &RunSummary{
			ID:                 string(raw.Run.RunID),
			WorkerKind:         string(raw.Run.WorkerKind),
			Status:             string(raw.Run.Status),
			LastKnownSummary:   raw.Run.LastKnownSummary,
			StartedAt:          raw.Run.StartedAt,
			EndedAt:            raw.Run.EndedAt,
			InterruptionReason: raw.Run.InterruptionReason,
		}
	}
	if raw.Checkpoint != nil {
		out.Checkpoint = &CheckpointSummary{
			ID:               string(raw.Checkpoint.CheckpointID),
			Trigger:          string(raw.Checkpoint.Trigger),
			CreatedAt:        raw.Checkpoint.CreatedAt,
			ResumeDescriptor: raw.Checkpoint.ResumeDescriptor,
			IsResumable:      raw.Checkpoint.IsResumable,
		}
	}
	if raw.Handoff != nil {
		out.Handoff = &HandoffSummary{
			ID:           raw.Handoff.HandoffID,
			Status:       raw.Handoff.Status,
			SourceWorker: string(raw.Handoff.SourceWorker),
			TargetWorker: string(raw.Handoff.TargetWorker),
			Mode:         raw.Handoff.Mode,
			Reason:       raw.Handoff.Reason,
			AcceptedBy:   string(raw.Handoff.AcceptedBy),
			CreatedAt:    raw.Handoff.CreatedAt,
		}
	}
	if raw.Launch != nil {
		out.Launch = &LaunchSummary{
			AttemptID:         raw.Launch.AttemptID,
			LaunchID:          raw.Launch.LaunchID,
			Status:            raw.Launch.Status,
			RequestedAt:       raw.Launch.RequestedAt,
			StartedAt:         raw.Launch.StartedAt,
			EndedAt:           raw.Launch.EndedAt,
			Summary:           raw.Launch.Summary,
			ErrorMessage:      raw.Launch.ErrorMessage,
			OutputArtifactRef: raw.Launch.OutputArtifactRef,
		}
	}
	if raw.LaunchControl != nil {
		out.LaunchControl = &LaunchControlSummary{
			State:            raw.LaunchControl.State,
			RetryDisposition: raw.LaunchControl.RetryDisposition,
			Reason:           raw.LaunchControl.Reason,
			HandoffID:        raw.LaunchControl.HandoffID,
			AttemptID:        raw.LaunchControl.AttemptID,
			LaunchID:         raw.LaunchControl.LaunchID,
			TargetWorker:     string(raw.LaunchControl.TargetWorker),
			RequestedAt:      raw.LaunchControl.RequestedAt,
			CompletedAt:      raw.LaunchControl.CompletedAt,
			FailedAt:         raw.LaunchControl.FailedAt,
		}
	}
	if raw.Acknowledgment != nil {
		out.Acknowledgment = &AcknowledgmentSummary{
			Status:    raw.Acknowledgment.Status,
			Summary:   raw.Acknowledgment.Summary,
			CreatedAt: raw.Acknowledgment.CreatedAt,
		}
	}
	if raw.FollowThrough != nil {
		out.FollowThrough = &FollowThroughSummary{
			RecordID:        raw.FollowThrough.RecordID,
			Kind:            raw.FollowThrough.Kind,
			Summary:         raw.FollowThrough.Summary,
			LaunchAttemptID: raw.FollowThrough.LaunchAttemptID,
			LaunchID:        raw.FollowThrough.LaunchID,
			CreatedAt:       raw.FollowThrough.CreatedAt,
		}
	}
	if raw.Resolution != nil {
		out.Resolution = &ResolutionSummary{
			ResolutionID:    raw.Resolution.ResolutionID,
			Kind:            raw.Resolution.Kind,
			Summary:         raw.Resolution.Summary,
			LaunchAttemptID: raw.Resolution.LaunchAttemptID,
			LaunchID:        raw.Resolution.LaunchID,
			CreatedAt:       raw.Resolution.CreatedAt,
		}
	}
	if raw.ActiveBranch != nil {
		out.ActiveBranch = &ActiveBranchSummary{
			Class:                  raw.ActiveBranch.Class,
			BranchRef:              raw.ActiveBranch.BranchRef,
			ActionabilityAnchor:    raw.ActiveBranch.ActionabilityAnchor,
			ActionabilityAnchorRef: raw.ActiveBranch.ActionabilityAnchorRef,
			Reason:                 raw.ActiveBranch.Reason,
		}
	}
	if raw.LocalRunFinalization != nil {
		out.LocalRunFinalization = &LocalRunFinalizationSummary{
			State:        raw.LocalRunFinalization.State,
			RunID:        string(raw.LocalRunFinalization.RunID),
			RunStatus:    string(raw.LocalRunFinalization.RunStatus),
			CheckpointID: string(raw.LocalRunFinalization.CheckpointID),
			Reason:       raw.LocalRunFinalization.Reason,
		}
	}
	if raw.LocalResume != nil {
		out.LocalResume = &LocalResumeAuthoritySummary{
			State:               raw.LocalResume.State,
			Mode:                raw.LocalResume.Mode,
			CheckpointID:        string(raw.LocalResume.CheckpointID),
			RunID:               string(raw.LocalResume.RunID),
			BlockingBranchClass: raw.LocalResume.BlockingBranchClass,
			BlockingBranchRef:   raw.LocalResume.BlockingBranchRef,
			Reason:              raw.LocalResume.Reason,
		}
	}
	if raw.ActionAuthority != nil {
		out.ActionAuthority = &OperatorActionAuthoritySet{
			RequiredNextAction: raw.ActionAuthority.RequiredNextAction,
		}
		if len(raw.ActionAuthority.Actions) > 0 {
			out.ActionAuthority.Actions = make([]OperatorActionAuthority, 0, len(raw.ActionAuthority.Actions))
			for _, action := range raw.ActionAuthority.Actions {
				out.ActionAuthority.Actions = append(out.ActionAuthority.Actions, OperatorActionAuthority{
					Action:              action.Action,
					State:               action.State,
					Reason:              action.Reason,
					BlockingBranchClass: action.BlockingBranchClass,
					BlockingBranchRef:   action.BlockingBranchRef,
					AnchorKind:          action.AnchorKind,
					AnchorRef:           action.AnchorRef,
				})
			}
		}
	}
	if raw.OperatorDecision != nil {
		out.OperatorDecision = &OperatorDecisionSummary{
			ActiveOwnerClass:   raw.OperatorDecision.ActiveOwnerClass,
			ActiveOwnerRef:     raw.OperatorDecision.ActiveOwnerRef,
			Headline:           raw.OperatorDecision.Headline,
			RequiredNextAction: raw.OperatorDecision.RequiredNextAction,
			PrimaryReason:      raw.OperatorDecision.PrimaryReason,
			Guidance:           raw.OperatorDecision.Guidance,
			IntegrityNote:      raw.OperatorDecision.IntegrityNote,
		}
		if len(raw.OperatorDecision.BlockedActions) > 0 {
			out.OperatorDecision.BlockedActions = make([]OperatorDecisionBlockedAction, 0, len(raw.OperatorDecision.BlockedActions))
			for _, blocked := range raw.OperatorDecision.BlockedActions {
				out.OperatorDecision.BlockedActions = append(out.OperatorDecision.BlockedActions, OperatorDecisionBlockedAction{
					Action: blocked.Action,
					Reason: blocked.Reason,
				})
			}
		}
	}
	if raw.OperatorExecutionPlan != nil {
		out.OperatorExecutionPlan = &OperatorExecutionPlan{
			MandatoryBeforeProgress: raw.OperatorExecutionPlan.MandatoryBeforeProgress,
		}
		if raw.OperatorExecutionPlan.PrimaryStep != nil {
			out.OperatorExecutionPlan.PrimaryStep = &OperatorExecutionStep{
				Action:         raw.OperatorExecutionPlan.PrimaryStep.Action,
				Status:         raw.OperatorExecutionPlan.PrimaryStep.Status,
				Domain:         raw.OperatorExecutionPlan.PrimaryStep.Domain,
				CommandSurface: raw.OperatorExecutionPlan.PrimaryStep.CommandSurface,
				CommandHint:    raw.OperatorExecutionPlan.PrimaryStep.CommandHint,
				Reason:         raw.OperatorExecutionPlan.PrimaryStep.Reason,
			}
		}
		if len(raw.OperatorExecutionPlan.SecondarySteps) > 0 {
			out.OperatorExecutionPlan.SecondarySteps = make([]OperatorExecutionStep, 0, len(raw.OperatorExecutionPlan.SecondarySteps))
			for _, step := range raw.OperatorExecutionPlan.SecondarySteps {
				out.OperatorExecutionPlan.SecondarySteps = append(out.OperatorExecutionPlan.SecondarySteps, OperatorExecutionStep{
					Action:         step.Action,
					Status:         step.Status,
					Domain:         step.Domain,
					CommandSurface: step.CommandSurface,
					CommandHint:    step.CommandHint,
					Reason:         step.Reason,
				})
			}
		}
		if len(raw.OperatorExecutionPlan.BlockedSteps) > 0 {
			out.OperatorExecutionPlan.BlockedSteps = make([]OperatorExecutionStep, 0, len(raw.OperatorExecutionPlan.BlockedSteps))
			for _, step := range raw.OperatorExecutionPlan.BlockedSteps {
				out.OperatorExecutionPlan.BlockedSteps = append(out.OperatorExecutionPlan.BlockedSteps, OperatorExecutionStep{
					Action:         step.Action,
					Status:         step.Status,
					Domain:         step.Domain,
					CommandSurface: step.CommandSurface,
					CommandHint:    step.CommandHint,
					Reason:         step.Reason,
				})
			}
		}
	}
	if raw.LatestOperatorStepReceipt != nil {
		out.LatestOperatorStepReceipt = &OperatorStepReceiptSummary{
			ReceiptID:    raw.LatestOperatorStepReceipt.ReceiptID,
			ActionHandle: raw.LatestOperatorStepReceipt.ActionHandle,
			ResultClass:  raw.LatestOperatorStepReceipt.ResultClass,
			Summary:      raw.LatestOperatorStepReceipt.Summary,
			Reason:       raw.LatestOperatorStepReceipt.Reason,
			CreatedAt:    raw.LatestOperatorStepReceipt.CreatedAt,
		}
	}
	if len(raw.RecentOperatorStepReceipts) > 0 {
		out.RecentOperatorStepReceipts = make([]OperatorStepReceiptSummary, 0, len(raw.RecentOperatorStepReceipts))
		for _, item := range raw.RecentOperatorStepReceipts {
			out.RecentOperatorStepReceipts = append(out.RecentOperatorStepReceipts, OperatorStepReceiptSummary{
				ReceiptID:    item.ReceiptID,
				ActionHandle: item.ActionHandle,
				ResultClass:  item.ResultClass,
				Summary:      item.Summary,
				Reason:       item.Reason,
				CreatedAt:    item.CreatedAt,
			})
		}
	}
	if raw.HandoffContinuity != nil {
		out.HandoffContinuity = &HandoffContinuitySummary{
			State:                        raw.HandoffContinuity.State,
			Reason:                       raw.HandoffContinuity.Reason,
			LaunchAttemptID:              raw.HandoffContinuity.LaunchAttemptID,
			LaunchID:                     raw.HandoffContinuity.LaunchID,
			AcknowledgmentID:             raw.HandoffContinuity.AcknowledgmentID,
			AcknowledgmentStatus:         raw.HandoffContinuity.AcknowledgmentStatus,
			AcknowledgmentSummary:        raw.HandoffContinuity.AcknowledgmentSummary,
			FollowThroughID:              raw.HandoffContinuity.FollowThroughID,
			FollowThroughKind:            raw.HandoffContinuity.FollowThroughKind,
			FollowThroughSummary:         raw.HandoffContinuity.FollowThroughSummary,
			ResolutionID:                 raw.HandoffContinuity.ResolutionID,
			ResolutionKind:               raw.HandoffContinuity.ResolutionKind,
			ResolutionSummary:            raw.HandoffContinuity.ResolutionSummary,
			DownstreamContinuationProven: raw.HandoffContinuity.DownstreamContinuationProven,
		}
	}
	if raw.Recovery != nil {
		out.Recovery = &RecoverySummary{
			ContinuityOutcome:      raw.Recovery.ContinuityOutcome,
			Class:                  raw.Recovery.RecoveryClass,
			Action:                 raw.Recovery.RecommendedAction,
			ReadyForNextRun:        raw.Recovery.ReadyForNextRun,
			ReadyForHandoffLaunch:  raw.Recovery.ReadyForHandoffLaunch,
			RequiresDecision:       raw.Recovery.RequiresDecision,
			RequiresRepair:         raw.Recovery.RequiresRepair,
			RequiresReview:         raw.Recovery.RequiresReview,
			RequiresReconciliation: raw.Recovery.RequiresReconciliation,
			DriftClass:             string(raw.Recovery.DriftClass),
			Reason:                 raw.Recovery.Reason,
			CheckpointID:           string(raw.Recovery.CheckpointID),
			RunID:                  string(raw.Recovery.RunID),
			HandoffID:              raw.Recovery.HandoffID,
			HandoffStatus:          raw.Recovery.HandoffStatus,
		}
		if len(raw.Recovery.Issues) > 0 {
			out.Recovery.Issues = make([]RecoveryIssue, 0, len(raw.Recovery.Issues))
			for _, issue := range raw.Recovery.Issues {
				out.Recovery.Issues = append(out.Recovery.Issues, RecoveryIssue{
					Code:    issue.Code,
					Message: issue.Message,
				})
			}
		}
	}
	if len(raw.RecentProofs) > 0 {
		out.RecentProofs = make([]ProofItem, 0, len(raw.RecentProofs))
		for _, evt := range raw.RecentProofs {
			out.RecentProofs = append(out.RecentProofs, ProofItem{
				ID:        string(evt.EventID),
				Type:      evt.Type,
				Summary:   evt.Summary,
				Timestamp: evt.Timestamp,
			})
		}
	}
	if len(raw.RecentConversation) > 0 {
		out.RecentConversation = make([]ConversationItem, 0, len(raw.RecentConversation))
		for _, msg := range raw.RecentConversation {
			out.RecentConversation = append(out.RecentConversation, ConversationItem{
				Role:      msg.Role,
				Body:      msg.Body,
				CreatedAt: msg.CreatedAt,
			})
		}
	}
	return out
}

func ipcTaskID(taskID string) common.TaskID {
	return common.TaskID(taskID)
}
```

## internal/tui/shell/primary_action_executor.go
```go
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

var primaryActionIPCCall = ipc.CallUnix

type PrimaryActionExecutionOutcome struct {
	Receipt OperatorStepReceiptSummary
}

type PrimaryActionExecutor interface {
	Execute(taskID string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error)
}

type IPCPrimaryActionExecutor struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCPrimaryActionExecutor(socketPath string) *IPCPrimaryActionExecutor {
	return &IPCPrimaryActionExecutor{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (e *IPCPrimaryActionExecutor) Execute(taskID string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error) {
	step, err := executablePrimaryStep(snapshot)
	if err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return executePrimaryStepIPC(ctx, e.SocketPath, taskID, *step)
}

func executablePrimaryStep(snapshot Snapshot) (*OperatorExecutionStep, error) {
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return nil, fmt.Errorf("no primary operator step is currently available")
	}
	step := snapshot.OperatorExecutionPlan.PrimaryStep
	if step.CommandSurface != "DEDICATED" {
		return nil, fmt.Errorf("primary operator step %s is guidance-only and cannot be executed directly from the shell", primaryStepActionLabel(step.Action))
	}
	return step, nil
}

func executePrimaryStepIPC(ctx context.Context, socketPath string, taskID string, step OperatorExecutionStep) (PrimaryActionExecutionOutcome, error) {
	raw, err := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{TaskID: common.TaskID(taskID)})
	if err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	resp, err := primaryActionIPCCall(ctx, socketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_primary_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodExecutePrimaryOperatorStep,
		Payload:   raw,
	})
	if err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	var out ipc.TaskExecutePrimaryOperatorStepResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	if out.Receipt.ActionHandle == "" {
		return PrimaryActionExecutionOutcome{}, fmt.Errorf("primary operator step execution returned no durable receipt")
	}
	if out.Receipt.ActionHandle != step.Action {
		return PrimaryActionExecutionOutcome{}, fmt.Errorf("primary operator step changed during execution from %s to %s", primaryStepActionLabel(step.Action), primaryStepActionLabel(out.Receipt.ActionHandle))
	}
	return PrimaryActionExecutionOutcome{
		Receipt: operatorStepReceiptFromIPC(out.Receipt),
	}, nil
}

func operatorStepReceiptFromIPC(raw ipc.TaskOperatorStepReceipt) OperatorStepReceiptSummary {
	return OperatorStepReceiptSummary{
		ReceiptID:          raw.ReceiptID,
		TaskID:             string(raw.TaskID),
		ActionHandle:       raw.ActionHandle,
		ExecutionDomain:    raw.ExecutionDomain,
		CommandSurfaceKind: raw.CommandSurfaceKind,
		ExecutionAttempted: raw.ExecutionAttempted,
		ResultClass:        raw.ResultClass,
		Summary:            raw.Summary,
		Reason:             raw.Reason,
		RunID:              string(raw.RunID),
		CheckpointID:       string(raw.CheckpointID),
		BriefID:            string(raw.BriefID),
		HandoffID:          raw.HandoffID,
		LaunchAttemptID:    raw.LaunchAttemptID,
		LaunchID:           raw.LaunchID,
		CreatedAt:          raw.CreatedAt,
		CompletedAt:        raw.CompletedAt,
	}
}

func primaryStepActionLabel(action string) string {
	if action == "" {
		return "action"
	}
	return action
}
```

## internal/tui/shell/app.go
```go
package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type App struct {
	TaskID           string
	Source           SnapshotSource
	MessageSender    TaskMessageSender
	ActionExecutor   PrimaryActionExecutor
	LifecycleSink    LifecycleSink
	RegistrySink     SessionRegistrySink
	RegistrySource   SessionRegistrySource
	WorkerPreference WorkerPreference
	Host             WorkerHost
	FallbackHost     WorkerHost
	Input            io.Reader
	Output           io.Writer
	RefreshInterval  time.Duration
}

type primaryActionExecutionResult struct {
	outcome    PrimaryActionExecutionOutcome
	step       OperatorExecutionStep
	before     Snapshot
	err        error
	finishedAt time.Time
}

func NewApp(taskID string, source SnapshotSource) *App {
	return &App{
		TaskID:           taskID,
		Source:           source,
		WorkerPreference: WorkerPreferenceAuto,
		FallbackHost:     NewTranscriptHost(),
		Input:            os.Stdin,
		Output:           os.Stdout,
		RefreshInterval:  5 * time.Second,
	}
}

func (a *App) Run(ctx context.Context) error {
	if a.Source == nil {
		return fmt.Errorf("shell snapshot source is required")
	}
	if a.Input == nil {
		a.Input = os.Stdin
	}
	if a.Output == nil {
		a.Output = os.Stdout
	}
	snapshot, err := a.Source.Load(a.TaskID)
	if err != nil {
		return err
	}

	ui := initialUIState(time.Now().UTC(), a.WorkerPreference)
	addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventShellStarted, fmt.Sprintf("Shell session %s started.", shortTaskID(ui.Session.SessionID)))
	if prior := capturePriorPersistedShellOutcome(snapshot); prior != "" {
		ui.Session.PriorPersistedSummary = prior
		addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventPriorPersistedProof, "Previous persisted shell outcome: "+prior)
	}

	stdinFile, ok := a.Input.(*os.File)
	if !ok {
		return fmt.Errorf("shell input must be an *os.File")
	}
	stdoutFile, ok := a.Output.(*os.File)
	if !ok {
		return fmt.Errorf("shell output must be an *os.File")
	}

	restore, err := enterTerminalMode(stdinFile, stdoutFile)
	if err != nil {
		return err
	}
	defer restore()

	preferredHost := a.Host
	resolvedWorker := resolveWorkerPreference(a.WorkerPreference, snapshot)
	if isScratchIntakeSnapshot(snapshot) {
		resolvedWorker = WorkerPreferenceAuto
	}
	if preferredHost != nil {
		if hostPreference := workerPreferenceFromHost(preferredHost); hostPreference != WorkerPreferenceAuto {
			resolvedWorker = hostPreference
		}
	}
	if preferredHost == nil {
		preferredHost, resolvedWorker, err = selectPreferredHost(a.WorkerPreference, snapshot)
		if err != nil {
			return err
		}
	}
	ui.Session.ResolvedWorker = resolvedWorker
	reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, preferredHost.Status(), true, &ui)
	if err := loadKnownShellSessions(a.RegistrySource, a.TaskID, &ui.Session); err != nil {
		ui.LastError = "shell session registry read failed: " + err.Error()
	}

	host, hostErr := startPreferredHost(ctx, preferredHost, a.FallbackHost, snapshot)
	startupLabel := workerPreferenceLabel(resolvedWorker)
	if preferredHost != nil {
		if label := strings.TrimSpace(preferredHost.WorkerLabel()); label != "" {
			startupLabel = label
		}
	}
	addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventHostStartupAttempted, fmt.Sprintf("Attempted %s host startup.", startupLabel))
	if hostErr != "" {
		ui.LastError = hostErr
	}
	defer reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, host.Status(), false, &ui)
	defer func() {
		_ = host.Stop()
	}()

	lastWidth, lastHeight := terminalSize()
	applyHostResize(host, lastWidth, lastHeight, ui)
	initialStatus := host.Status()
	reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, initialStatus, true, &ui)
	captureHostLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, &ui, HostStatus{}, initialStatus)

	keyCh := make(chan byte, 16)
	go readKeys(stdinFile, keyCh)
	primaryActionDoneCh := make(chan primaryActionExecutionResult, 1)

	frameTicker := time.NewTicker(100 * time.Millisecond)
	defer frameTicker.Stop()

	tickerInterval := a.RefreshInterval
	if tickerInterval <= 0 {
		tickerInterval = 5 * time.Second
	}
	snapshotTicker := time.NewTicker(tickerInterval)
	defer snapshotTicker.Stop()
	registryTicker := time.NewTicker(shellSessionHeartbeatInterval)
	defer registryTicker.Stop()

	ui.ObservedAt = time.Now().UTC()
	if err := renderShell(stdoutFile, snapshot, ui, host); err != nil {
		return err
	}

	lastHostStatus := host.Status()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case key, ok := <-keyCh:
			if !ok {
				return nil
			}
			action := routeKey(&ui, host, key)
			switch action {
			case actionQuit:
				return nil
			case actionRefresh:
				if loadErr := reloadShellSnapshot(a.Source, a.TaskID, host, a.RegistrySource, &snapshot, &ui, true); loadErr != nil {
					ui.LastError = loadErr.Error()
				} else {
					ui.LastError = ""
				}
			case actionStageScratchAdoption:
				if err := stagePendingTaskMessageFromLocalScratch(&ui, snapshot); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionEnterPendingTaskMessageEdit:
				if err := enterPendingTaskMessageEditMode(&ui); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionSavePendingTaskMessageEdit:
				if err := savePendingTaskMessageEditMode(&ui); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionCancelPendingTaskMessageEdit:
				if err := cancelPendingTaskMessageEditMode(&ui); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			case actionSendPendingTaskMessage:
				if err := sendPendingTaskMessage(a.MessageSender, a.TaskID, &ui); err != nil {
					ui.LastError = err.Error()
					break
				}
				next, loadErr := a.Source.Load(a.TaskID)
				if loadErr != nil {
					ui.LastRefresh = time.Now().UTC()
					ui.LastError = "task message sent, but shell refresh failed: " + loadErr.Error()
					break
				}
				snapshot = next
				host.UpdateSnapshot(snapshot)
				ui.LastRefresh = time.Now().UTC()
				ui.LastError = ""
				if err := loadKnownShellSessions(a.RegistrySource, a.TaskID, &ui.Session); err != nil {
					ui.LastError = "shell session registry read failed: " + err.Error()
				}
			case actionClearPendingTaskMessage:
				clearPendingTaskMessage(&ui)
			case actionExecutePrimaryOperatorStep:
				if err := startPrimaryOperatorStepExecution(a.ActionExecutor, a.TaskID, snapshot, &ui, primaryActionDoneCh); err != nil {
					ui.LastError = err.Error()
				} else {
					ui.LastError = ""
				}
			}
		case result := <-primaryActionDoneCh:
			if err := completePrimaryOperatorStepExecution(a.Source, a.TaskID, host, a.RegistrySource, &snapshot, &ui, result); err != nil {
				ui.LastError = err.Error()
			} else {
				ui.LastError = ""
			}
		case <-snapshotTicker.C:
			if loadErr := reloadShellSnapshot(a.Source, a.TaskID, host, a.RegistrySource, &snapshot, &ui, false); loadErr != nil {
				ui.LastError = loadErr.Error()
				continue
			}
			ui.LastError = ""
		case <-registryTicker.C:
			reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, host.Status(), true, &ui)
		case <-frameTicker.C:
		}

		width, height := terminalSize()
		if width != lastWidth || height != lastHeight {
			if applyHostResize(host, width, height, ui) {
				addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventResizeApplied, fmt.Sprintf("Resized live worker pane to %dx%d.", host.Status().Width, host.Status().Height))
			}
			lastWidth = width
			lastHeight = height
		}

		currentStatus := host.Status()
		captureHostLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, &ui, lastHostStatus, currentStatus)
		if hostStatusChanged(lastHostStatus, currentStatus) {
			reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, currentStatus, true, &ui)
		}
		if nextHost, note, changed := transitionExitedHost(ctx, host, a.FallbackHost, snapshot); changed {
			host = nextHost
			applyHostResize(host, lastWidth, lastHeight, ui)
			if note != "" {
				ui.LastError = note
			}
			captureHostLifecycle(ctx, a.LifecycleSink, a.TaskID, ui.Session.SessionID, &ui, currentStatus, host.Status())
			reportShellSession(a.RegistrySink, a.TaskID, &ui.Session, host.Status(), true, &ui)
		}
		lastHostStatus = host.Status()

		ui.ObservedAt = time.Now().UTC()
		if err := renderShell(stdoutFile, snapshot, ui, host); err != nil {
			return err
		}
	}
}

func initialUIState(now time.Time, preference WorkerPreference) UIState {
	ui := UIState{
		ShowInspector: false,
		ShowProof:     false,
		Focus:         FocusWorker,
		LastRefresh:   now,
		ObservedAt:    now,
		Session:       newSessionState(now),
	}
	ui.Session.WorkerPreference = preference
	return ui
}

type keyAction int

const (
	actionNone keyAction = iota
	actionQuit
	actionRefresh
	actionStageScratchAdoption
	actionEnterPendingTaskMessageEdit
	actionSavePendingTaskMessageEdit
	actionCancelPendingTaskMessageEdit
	actionSendPendingTaskMessage
	actionClearPendingTaskMessage
	actionExecutePrimaryOperatorStep
)

func handleShellKey(ui *UIState, key byte) keyAction {
	switch key {
	case 'q', 'Q', 3:
		return actionQuit
	case 'i', 'I':
		ui.ShowInspector = !ui.ShowInspector
		if !ui.ShowInspector && ui.Focus == FocusInspector {
			ui.Focus = FocusWorker
		}
	case 'p', 'P':
		ui.ShowProof = !ui.ShowProof
		if !ui.ShowProof && ui.Focus == FocusActivity {
			ui.Focus = FocusWorker
		}
	case 'r', 'R':
		return actionRefresh
	case 'a', 'A':
		return actionStageScratchAdoption
	case 'e', 'E':
		return actionEnterPendingTaskMessageEdit
	case 'm', 'M':
		return actionSendPendingTaskMessage
	case 'n', 'N':
		return actionExecutePrimaryOperatorStep
	case 'x', 'X':
		return actionClearPendingTaskMessage
	case 'h', 'H':
		ui.ShowHelp = !ui.ShowHelp
		if ui.ShowHelp {
			ui.ShowStatus = false
		}
	case 's', 'S':
		ui.ShowStatus = !ui.ShowStatus
		if ui.ShowStatus {
			ui.ShowHelp = false
		}
	case '\t':
		ui.Focus = nextFocus(*ui)
	}
	return actionNone
}

func routeKey(ui *UIState, host WorkerHost, key byte) keyAction {
	if ui.PendingTaskMessageEditMode && ui.Focus == FocusWorker {
		return routePendingTaskMessageEditKey(ui, key)
	}
	if host != nil && ui.Focus == FocusWorker {
		if ui.EscapePrefix {
			ui.EscapePrefix = false
			return handleShellKey(ui, key)
		}
		if key == 0x07 {
			ui.EscapePrefix = true
			return actionNone
		}
		if host.CanAcceptInput() && host.WriteInput([]byte{key}) {
			ui.LastError = ""
			return actionNone
		}
		action := handleShellKey(ui, key)
		if action != actionNone {
			return action
		}
		if isPrintableKey(key) {
			ui.LastError = unavailableInputMessage(host.Status())
			return actionNone
		}
	}
	return handleShellKey(ui, key)
}

func routePendingTaskMessageEditKey(ui *UIState, key byte) keyAction {
	if ui.EscapePrefix {
		ui.EscapePrefix = false
		switch key {
		case 's', 'S':
			return actionSavePendingTaskMessageEdit
		case 'c', 'C':
			return actionCancelPendingTaskMessageEdit
		default:
			return handleShellKey(ui, key)
		}
	}
	if key == 0x07 {
		ui.EscapePrefix = true
		return actionNone
	}
	if applyPendingTaskMessageEditInput(ui, key) {
		ui.LastError = ""
	}
	return actionNone
}

func nextFocus(ui UIState) FocusPane {
	order := []FocusPane{FocusWorker}
	if ui.ShowInspector {
		order = append(order, FocusInspector)
	}
	if ui.ShowProof {
		order = append(order, FocusActivity)
	}
	for idx, pane := range order {
		if pane == ui.Focus {
			return order[(idx+1)%len(order)]
		}
	}
	return FocusWorker
}

func renderShell(out io.Writer, snapshot Snapshot, ui UIState, host WorkerHost) error {
	width, height := terminalSize()
	vm := BuildViewModel(snapshot, ui, host, width, height)
	_, err := io.WriteString(out, Render(vm, width, height))
	return err
}

func reloadShellSnapshot(source SnapshotSource, taskID string, host WorkerHost, registrySource SessionRegistrySource, snapshot *Snapshot, ui *UIState, manual bool) error {
	next, err := source.Load(taskID)
	if err != nil {
		return err
	}
	if snapshot != nil {
		*snapshot = next
	}
	if host != nil {
		host.UpdateSnapshot(next)
	}
	if ui != nil {
		ui.LastRefresh = time.Now().UTC()
		if manual {
			addSessionEvent(&ui.Session, ui.LastRefresh, SessionEventManualRefresh, "Manual shell refresh completed.")
		}
		if err := loadKnownShellSessions(registrySource, taskID, &ui.Session); err != nil {
			return fmt.Errorf("shell session registry read failed: %w", err)
		}
	}
	return nil
}

func startPrimaryOperatorStepExecution(executor PrimaryActionExecutor, taskID string, snapshot Snapshot, ui *UIState, done chan<- primaryActionExecutionResult) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("primary operator step cannot run because this shell is not attached to a task")
	}
	if executor == nil {
		return fmt.Errorf("primary operator step cannot run because no shell action executor is configured")
	}
	if done == nil {
		return fmt.Errorf("primary operator step cannot run because no completion channel is configured")
	}
	if ui != nil && ui.PrimaryActionInFlight != nil {
		return fmt.Errorf("primary operator step %s is already in progress", operatorActionDisplayName(ui.PrimaryActionInFlight.Action))
	}
	step, err := executablePrimaryStep(snapshot)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if ui != nil {
		ui.PrimaryActionInFlight = &PrimaryActionInFlightSummary{
			Action:    step.Action,
			StartedAt: now,
		}
		addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionStarted, fmt.Sprintf("Executing primary operator step %s through Tuku control-plane IPC.", strings.ToLower(step.Action)))
	}
	before := snapshot
	go func() {
		outcome, err := executor.Execute(taskID, snapshot)
		done <- primaryActionExecutionResult{
			outcome:    outcome,
			step:       *step,
			before:     before,
			err:        err,
			finishedAt: time.Now().UTC(),
		}
	}()
	return nil
}

func completePrimaryOperatorStepExecution(source SnapshotSource, taskID string, host WorkerHost, registrySource SessionRegistrySource, snapshot *Snapshot, ui *UIState, result primaryActionExecutionResult) error {
	if snapshot == nil {
		return fmt.Errorf("primary operator step cannot finish because shell snapshot state is unavailable")
	}
	if ui != nil {
		ui.PrimaryActionInFlight = nil
	}
	now := time.Now().UTC()
	if result.err != nil {
		if ui != nil {
			ui.LastPrimaryActionResult = failedPrimaryActionResult(result.step, result.err, result.finishedAt)
			addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionFailed, ui.LastPrimaryActionResult.Summary+". "+truncateWithEllipsis(ui.LastPrimaryActionResult.ErrorText, 96))
		}
		return fmt.Errorf("primary operator step %s failed: %w", strings.ToLower(result.step.Action), result.err)
	}
	if err := reloadShellSnapshot(source, taskID, host, registrySource, snapshot, ui, true); err != nil {
		if ui != nil {
			ui.LastPrimaryActionResult = failedPrimaryActionResult(result.step, fmt.Errorf("primary operator step ran, but shell refresh failed: %w", err), result.finishedAt)
			addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionFailed, ui.LastPrimaryActionResult.Summary+". "+truncateWithEllipsis(ui.LastPrimaryActionResult.ErrorText, 96))
		}
		return fmt.Errorf("primary operator step ran, but shell refresh failed: %w", err)
	}
	if ui != nil {
		if receiptIsFailure(result.outcome.Receipt) {
			ui.LastPrimaryActionResult = failedPrimaryActionResultWithReceipt(result.step, result.outcome.Receipt, result.finishedAt)
		} else {
			ui.LastPrimaryActionResult = successfulPrimaryActionResult(result.step, result.before, *snapshot, result.outcome.Receipt, result.finishedAt)
		}
		summary := ui.LastPrimaryActionResult.Summary
		if next := strings.TrimSpace(ui.LastPrimaryActionResult.NextStep); next != "" && next != "none" {
			summary += ". next " + next
		}
		addSessionEvent(&ui.Session, now, SessionEventPrimaryOperatorActionExecuted, summary)
	}
	if receiptIsFailure(result.outcome.Receipt) {
		return fmt.Errorf("primary operator step %s %s: %s", strings.ToLower(result.step.Action), strings.ToLower(nonEmpty(result.outcome.Receipt.ResultClass, "failed")), nonEmpty(result.outcome.Receipt.Reason, result.outcome.Receipt.Summary))
	}
	return nil
}

func executePrimaryOperatorStep(executor PrimaryActionExecutor, source SnapshotSource, taskID string, host WorkerHost, registrySource SessionRegistrySource, snapshot *Snapshot, ui *UIState) error {
	if snapshot == nil {
		return fmt.Errorf("primary operator step cannot run because shell snapshot state is unavailable")
	}
	done := make(chan primaryActionExecutionResult, 1)
	if err := startPrimaryOperatorStepExecution(executor, taskID, *snapshot, ui, done); err != nil {
		return err
	}
	result := <-done
	return completePrimaryOperatorStepExecution(source, taskID, host, registrySource, snapshot, ui, result)
}

func applyHostResize(host WorkerHost, width int, height int, ui UIState) bool {
	if host == nil {
		return false
	}
	layout := computeShellLayout(width, height, ui)
	paneWidth, paneHeight := layout.workerContentSize()
	return host.Resize(paneWidth, paneHeight)
}

func unavailableInputMessage(status HostStatus) string {
	switch status.State {
	case HostStateFallback:
		return "worker session is in transcript fallback mode; live input is unavailable"
	case HostStateTranscriptOnly:
		return "worker session is transcript-only; live input is unavailable"
	case HostStateExited:
		if status.ExitCode != nil {
			return fmt.Sprintf("worker session exited with code %d; live input is unavailable", *status.ExitCode)
		}
		return "worker session exited; live input is unavailable"
	case HostStateFailed:
		return "worker session failed; live input is unavailable"
	case HostStateStarting:
		return "worker session is still starting; try again in a moment"
	default:
		return "worker input is unavailable"
	}
}

func isPrintableKey(key byte) bool {
	return key >= 32 && key < 127
}

func stagePendingTaskMessageFromLocalScratch(ui *UIState, snapshot Snapshot) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	draft, err := buildLocalScratchAdoptionDraft(snapshot)
	if err != nil {
		return err
	}
	ui.PendingTaskMessage = draft
	ui.PendingTaskMessageSource = "local_scratch_adoption"
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageStaged, "Staged pending task message from local scratch intake notes.")
	return nil
}

func sendPendingTaskMessage(sender TaskMessageSender, taskID string, ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("pending task message cannot be sent because this shell is not attached to a task")
	}
	if sender == nil {
		return fmt.Errorf("pending task message cannot be sent because no task-message sender is configured")
	}
	message := currentPendingTaskMessage(*ui)
	if isEffectivelyEmptyPendingTaskMessage(message) {
		return fmt.Errorf("no pending task message is staged")
	}
	if err := sender.Send(taskID, message); err != nil {
		return err
	}
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageSent, "Sent pending task message to Tuku canonical continuity.")
	ui.PendingTaskMessage = ""
	ui.PendingTaskMessageSource = ""
	resetPendingTaskMessageEditMode(ui)
	return nil
}

func clearPendingTaskMessage(ui *UIState) {
	if ui == nil {
		return
	}
	if isEffectivelyEmptyPendingTaskMessage(ui.PendingTaskMessage) && !ui.PendingTaskMessageEditMode {
		return
	}
	ui.PendingTaskMessage = ""
	ui.PendingTaskMessageSource = ""
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageCleared, "Cleared pending task message.")
}

func enterPendingTaskMessageEditMode(ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if ui.PendingTaskMessageEditMode {
		return fmt.Errorf("pending task message edit mode is already active")
	}
	if isEffectivelyEmptyPendingTaskMessage(ui.PendingTaskMessage) {
		return fmt.Errorf("no pending task message is staged")
	}
	ui.PendingTaskMessageEditMode = true
	ui.PendingTaskMessageEditOriginal = ui.PendingTaskMessage
	ui.PendingTaskMessageEditBuffer = ui.PendingTaskMessage
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageEditStarted, "Pending task message edit mode is active. Draft changes remain shell-local until explicit send.")
	return nil
}

func savePendingTaskMessageEditMode(ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if !ui.PendingTaskMessageEditMode {
		return fmt.Errorf("pending task message edit mode is not active")
	}
	ui.PendingTaskMessage = ui.PendingTaskMessageEditBuffer
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageEditSaved, "Saved pending task message edits. Draft remains shell-local until explicit send.")
	return nil
}

func cancelPendingTaskMessageEditMode(ui *UIState) error {
	if ui == nil {
		return fmt.Errorf("shell ui state is unavailable")
	}
	if !ui.PendingTaskMessageEditMode {
		return fmt.Errorf("pending task message edit mode is not active")
	}
	ui.PendingTaskMessage = ui.PendingTaskMessageEditOriginal
	resetPendingTaskMessageEditMode(ui)
	addSessionEvent(&ui.Session, time.Now().UTC(), SessionEventPendingMessageEditCanceled, "Canceled pending task message edits and restored the saved draft.")
	return nil
}

func resetPendingTaskMessageEditMode(ui *UIState) {
	if ui == nil {
		return
	}
	ui.PendingTaskMessageEditMode = false
	ui.PendingTaskMessageEditBuffer = ""
	ui.PendingTaskMessageEditOriginal = ""
}

func currentPendingTaskMessage(ui UIState) string {
	if ui.PendingTaskMessageEditMode {
		return ui.PendingTaskMessageEditBuffer
	}
	return ui.PendingTaskMessage
}

func applyPendingTaskMessageEditInput(ui *UIState, key byte) bool {
	if ui == nil || !ui.PendingTaskMessageEditMode {
		return false
	}
	switch key {
	case '\r', '\n':
		ui.PendingTaskMessageEditBuffer += "\n"
		return true
	case 0x7f, 0x08:
		if ui.PendingTaskMessageEditBuffer == "" {
			return true
		}
		_, size := utf8.DecodeLastRuneInString(ui.PendingTaskMessageEditBuffer)
		if size <= 0 {
			return true
		}
		ui.PendingTaskMessageEditBuffer = ui.PendingTaskMessageEditBuffer[:len(ui.PendingTaskMessageEditBuffer)-size]
		return true
	default:
		if key >= 32 {
			ui.PendingTaskMessageEditBuffer += string(key)
			return true
		}
	}
	return false
}

func isEffectivelyEmptyPendingTaskMessage(message string) bool {
	return strings.TrimSpace(message) == ""
}

func buildLocalScratchAdoptionDraft(snapshot Snapshot) (string, error) {
	if !snapshot.HasLocalScratchAdoption() {
		return "", fmt.Errorf("no surfaced local scratch notes are available to stage")
	}
	lines := []string{
		"Explicitly adopt these local scratch intake notes into this repo-backed Tuku task:",
		"",
	}
	for _, note := range snapshot.LocalScratch.Notes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		lines = append(lines, "- "+body)
	}
	lines = append(lines,
		"",
		"These notes came from local scratch history for this repo root. I am explicitly adopting them into canonical task continuity now.",
	)
	return strings.Join(lines, "\n"), nil
}

func hostStatusChanged(previous HostStatus, current HostStatus) bool {
	if previous.Mode != current.Mode {
		return true
	}
	if previous.State != current.State {
		return true
	}
	if previous.InputLive != current.InputLive {
		return true
	}
	if previous.Note != current.Note {
		return true
	}
	if previous.Width != current.Width || previous.Height != current.Height {
		return true
	}
	switch {
	case previous.ExitCode == nil && current.ExitCode == nil:
		return false
	case previous.ExitCode == nil || current.ExitCode == nil:
		return true
	default:
		return *previous.ExitCode != *current.ExitCode
	}
}

func captureHostLifecycle(ctx context.Context, sink LifecycleSink, taskID string, sessionID string, ui *UIState, previous HostStatus, current HostStatus) {
	now := time.Now().UTC()
	switch current.State {
	case HostStateLive:
		if previous.State != HostStateLive {
			addSessionEvent(&ui.Session, now, SessionEventHostLive, "Live worker host is active.")
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleHostStarted, current, ui)
		}
	case HostStateExited:
		if previous.State != HostStateExited {
			summary := "Live worker host ended."
			if current.ExitCode != nil {
				summary = fmt.Sprintf("Live worker host ended with exit code %d.", *current.ExitCode)
			}
			addSessionEvent(&ui.Session, now, SessionEventHostExited, summary)
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleHostExited, current, ui)
		}
	case HostStateFailed:
		if previous.State != HostStateFailed {
			summary := "Live worker host failed."
			if current.Note != "" {
				summary = "Live worker host failed: " + current.Note
			}
			addSessionEvent(&ui.Session, now, SessionEventHostFailed, summary)
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleHostExited, current, ui)
		}
	case HostStateFallback:
		if previous.State != HostStateFallback {
			summary := "Transcript fallback is active."
			if current.Note != "" {
				summary = current.Note
			}
			addSessionEvent(&ui.Session, now, SessionEventFallbackActivated, summary)
			recordLifecycle(ctx, sink, taskID, sessionID, PersistedLifecycleFallback, current, ui)
		}
	}
}

func recordLifecycle(ctx context.Context, sink LifecycleSink, taskID string, sessionID string, kind PersistedLifecycleKind, status HostStatus, ui *UIState) {
	if sink == nil {
		return
	}
	if err := sink.Record(taskID, sessionID, kind, status); err != nil {
		ui.LastError = "shell lifecycle proof bridge failed: " + err.Error()
	}
}

func readKeys(in io.Reader, out chan<- byte) {
	buf := make([]byte, 1)
	for {
		n, err := in.Read(buf)
		if err != nil || n == 0 {
			close(out)
			return
		}
		out <- buf[0]
	}
}

func enterTerminalMode(stdin *os.File, stdout *os.File) (func(), error) {
	fd := int(stdin.Fd())
	termios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, fmt.Errorf("read terminal attrs: %w", err)
	}
	raw := *termios
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return nil, fmt.Errorf("set raw terminal attrs: %w", err)
	}
	if _, err := io.WriteString(stdout, "\x1b[?1049h\x1b[?25l"); err != nil {
		return nil, err
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, termios)
		_, _ = io.WriteString(stdout, "\x1b[?25h\x1b[?1049l")
	}, nil
}

func terminalSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 120, 32
	}
	return int(ws.Col), int(ws.Row)
}
```

## internal/tui/shell/primary_action_result.go
```go
package shell

import (
	"fmt"
	"strings"
	"time"
)

func successfulPrimaryActionResult(step OperatorExecutionStep, before Snapshot, after Snapshot, receipt OperatorStepReceiptSummary, now time.Time) *PrimaryActionResultSummary {
	result := &PrimaryActionResultSummary{
		Action:      strings.TrimSpace(step.Action),
		Outcome:     "SUCCESS",
		Summary:     nonEmpty(strings.TrimSpace(receipt.Summary), fmt.Sprintf("executed %s", operatorActionDisplayName(step.Action))),
		Deltas:      meaningfulPrimaryActionDeltas(before, after),
		NextStep:    compactNextStep(after),
		ReceiptID:   strings.TrimSpace(receipt.ReceiptID),
		ResultClass: strings.TrimSpace(receipt.ResultClass),
		CreatedAt:   now.UTC(),
	}
	if len(result.Deltas) == 0 {
		result.Deltas = []string{"no operator-visible control-plane delta"}
	}
	return result
}

func failedPrimaryActionResult(step OperatorExecutionStep, err error, now time.Time) *PrimaryActionResultSummary {
	text := ""
	if err != nil {
		text = strings.TrimSpace(err.Error())
	}
	return &PrimaryActionResultSummary{
		Action:      strings.TrimSpace(step.Action),
		Outcome:     "FAILED",
		Summary:     fmt.Sprintf("failed %s", operatorActionDisplayName(step.Action)),
		ErrorText:   text,
		ResultClass: "FAILED",
		CreatedAt:   now.UTC(),
	}
}

func failedPrimaryActionResultWithReceipt(step OperatorExecutionStep, receipt OperatorStepReceiptSummary, now time.Time) *PrimaryActionResultSummary {
	return &PrimaryActionResultSummary{
		Action:      strings.TrimSpace(step.Action),
		Outcome:     "FAILED",
		Summary:     nonEmpty(strings.TrimSpace(receipt.Summary), fmt.Sprintf("failed %s", operatorActionDisplayName(step.Action))),
		ErrorText:   nonEmpty(strings.TrimSpace(receipt.Reason), strings.TrimSpace(receipt.Summary)),
		ReceiptID:   strings.TrimSpace(receipt.ReceiptID),
		ResultClass: strings.TrimSpace(receipt.ResultClass),
		CreatedAt:   now.UTC(),
	}
}

func receiptIsFailure(receipt OperatorStepReceiptSummary) bool {
	switch strings.TrimSpace(receipt.ResultClass) {
	case "FAILED", "REJECTED":
		return true
	default:
		return false
	}
}

func meaningfulPrimaryActionDeltas(before Snapshot, after Snapshot) []string {
	type candidate struct {
		label string
		from  string
		to    string
	}
	candidates := []candidate{
		{label: "branch", from: compactActiveBranchOwner(before), to: compactActiveBranchOwner(after)},
		{label: "decision", from: operatorDecisionHeadline(before), to: operatorDecisionHeadline(after)},
		{label: "next", from: compactNextStep(before), to: compactNextStep(after)},
		{label: "launch", from: launchControlLine(before), to: launchControlLine(after)},
		{label: "handoff", from: handoffContinuityLine(before), to: handoffContinuityLine(after)},
		{label: "local message", from: compactActionAuthorityState(before, "LOCAL_MESSAGE_MUTATION"), to: compactActionAuthorityState(after, "LOCAL_MESSAGE_MUTATION")},
		{label: "local resume", from: localResumeLine(before), to: localResumeLine(after)},
		{label: "local run", from: localRunFinalizationLine(before), to: localRunFinalizationLine(after)},
	}

	out := make([]string, 0, 4)
	for _, item := range candidates {
		from := compactResultValue(item.from)
		to := compactResultValue(item.to)
		if from == to {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s -> %s", item.label, from, to))
		if len(out) == 4 {
			break
		}
	}
	return out
}

func compactResultValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "n/a"
	}
	return value
}

func compactNextStep(snapshot Snapshot) string {
	if plan := operatorExecutionPlanLine(snapshot); plan != "n/a" && strings.TrimSpace(plan) != "" {
		return plan
	}
	if action := operatorActionLabel(snapshot); action != "none" && action != "n/a" && strings.TrimSpace(action) != "" {
		return action
	}
	return "none"
}

func compactActionAuthorityState(snapshot Snapshot, action string) string {
	authority := authorityFor(snapshot, action)
	if authority == nil {
		return "n/a"
	}
	switch authority.State {
	case "ALLOWED":
		return "allowed"
	case "BLOCKED":
		return "blocked"
	case "REQUIRED_NEXT":
		return "required"
	case "NOT_APPLICABLE":
		return "not applicable"
	default:
		return humanizeConstant(authority.State)
	}
}

func compactActiveBranchOwner(snapshot Snapshot) string {
	if snapshot.ActiveBranch == nil || strings.TrimSpace(snapshot.ActiveBranch.Class) == "" {
		return "n/a"
	}
	switch snapshot.ActiveBranch.Class {
	case "LOCAL":
		return "local"
	case "HANDOFF_CLAUDE":
		if strings.TrimSpace(snapshot.ActiveBranch.BranchRef) != "" {
			return "Claude " + shortTaskID(snapshot.ActiveBranch.BranchRef)
		}
		return "Claude"
	default:
		return humanizeConstant(snapshot.ActiveBranch.Class)
	}
}
```

## internal/tui/shell/viewmodel.go
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
				{Title: "operator", Lines: inspectorOperator(snapshot, ui)},
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
			Lines:   buildActivityLines(snapshot, host, ui),
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
				"n execute the current primary operator step when Tuku has a direct command path",
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
		lines := []string{
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
			fmt.Sprintf("branch %s", activeBranchLine(snapshot)),
			fmt.Sprintf("local run %s", localRunFinalizationLine(snapshot)),
			fmt.Sprintf("local resume %s", localResumeLine(snapshot)),
			fmt.Sprintf("authority %s", operatorAuthorityLine(snapshot)),
			fmt.Sprintf("decision %s", operatorDecisionHeadline(snapshot)),
			fmt.Sprintf("plan %s", operatorExecutionPlanLine(snapshot)),
			fmt.Sprintf("command %s", operatorExecutionCommand(snapshot)),
			fmt.Sprintf("progress %s", primaryActionInFlightLine(ui)),
			fmt.Sprintf("guidance %s", operatorDecisionGuidance(snapshot)),
			fmt.Sprintf("caution %s", operatorDecisionIntegrity(snapshot)),
		}
		if result := operatorActionResultHeadline(ui); result != "n/a" {
			lines = append(lines, fmt.Sprintf("result %s", result))
			for _, delta := range operatorActionResultDeltas(ui, 3) {
				lines = append(lines, fmt.Sprintf("delta %s", delta))
			}
			if next := operatorActionResultNextStep(ui); next != "n/a" {
				lines = append(lines, fmt.Sprintf("new next %s", next))
			}
		}
		lines = append(lines,
			fmt.Sprintf("reason %s", strongestOperatorReason(snapshot)),
			fmt.Sprintf("registry %s", sessionRegistrySummary(ui.Session)),
			fmt.Sprintf("draft %s", pendingMessageSummary(snapshot, ui)),
			fmt.Sprintf("checkpoint %s", checkpointLine(snapshot)),
			fmt.Sprintf("handoff %s", handoffLine(snapshot)),
			sessionPriorLine(ui.Session),
			"",
			latestCanonicalLine(snapshot),
		)
		vm.Overlay = &OverlayView{
			Title: "status",
			Lines: lines,
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
		case "CONTINUE_EXECUTION_REQUIRED":
			return "continue-pending"
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
		case "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED":
			return "review"
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
		if snapshot.Resolution == nil {
			return []string{"No handoff packet."}
		}
		lines := []string{"No active handoff packet."}
		lines = append(lines, fmt.Sprintf("resolution %s", strings.ToLower(strings.ReplaceAll(snapshot.Resolution.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.Resolution.Summary, 48))
		if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
			lines = append(lines, "continuity "+continuity)
		}
		return lines
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
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, "continuity "+continuity)
	}
	if snapshot.Acknowledgment != nil {
		lines = append(lines, fmt.Sprintf("ack %s", strings.ToLower(snapshot.Acknowledgment.Status)))
		lines = append(lines, truncateWithEllipsis(snapshot.Acknowledgment.Summary, 48))
	}
	if snapshot.FollowThrough != nil {
		lines = append(lines, fmt.Sprintf("follow-through %s", strings.ToLower(strings.ReplaceAll(snapshot.FollowThrough.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.FollowThrough.Summary, 48))
	}
	if snapshot.Resolution != nil {
		lines = append(lines, fmt.Sprintf("resolution %s", strings.ToLower(strings.ReplaceAll(snapshot.Resolution.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.Resolution.Summary, 48))
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State != "NOT_APPLICABLE" {
		lines = append(lines, "launch "+launchControlLine(snapshot))
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, "continuity "+continuity)
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
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, continuity)
	}
	if snapshot.HandoffContinuity != nil && snapshot.HandoffContinuity.State == "LAUNCH_COMPLETED_ACK_UNAVAILABLE" {
		lines = append(lines, "no usable acknowledgment captured; downstream work not proven")
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

func inspectorOperator(snapshot Snapshot, ui UIState) []string {
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
	if branch := activeBranchLine(snapshot); branch != "n/a" {
		lines = append(lines, "branch "+branch)
	}
	if localRun := localRunFinalizationLine(snapshot); localRun != "n/a" {
		lines = append(lines, "local run "+localRun)
	}
	if localResume := localResumeLine(snapshot); localResume != "n/a" {
		lines = append(lines, "local resume "+localResume)
	}
	if authority := operatorAuthorityLine(snapshot); authority != "n/a" {
		lines = append(lines, "authority "+authority)
	}
	if decision := operatorDecisionHeadline(snapshot); decision != "n/a" {
		lines = append(lines, "decision "+decision)
	}
	if plan := operatorExecutionPlanLine(snapshot); plan != "n/a" {
		lines = append(lines, "plan "+plan)
	}
	if command := operatorExecutionCommand(snapshot); command != "n/a" {
		lines = append(lines, "command "+truncateWithEllipsis(command, 64))
	}
	if progress := primaryActionInFlightLine(ui); progress != "n/a" {
		lines = append(lines, "progress "+truncateWithEllipsis(progress, 64))
	}
	if guidance := operatorDecisionGuidance(snapshot); guidance != "n/a" {
		lines = append(lines, "guidance "+truncateWithEllipsis(guidance, 64))
	}
	if caution := operatorDecisionIntegrity(snapshot); caution != "n/a" {
		lines = append(lines, "caution "+truncateWithEllipsis(caution, 64))
	}
	if result := operatorActionResultHeadline(ui); result != "n/a" {
		lines = append(lines, "result "+truncateWithEllipsis(result, 64))
	}
	if receipt := latestOperatorReceiptLine(snapshot); receipt != "n/a" {
		lines = append(lines, "receipt "+truncateWithEllipsis(receipt, 64))
	}
	for _, delta := range operatorActionResultDeltas(ui, 3) {
		lines = append(lines, "delta "+truncateWithEllipsis(delta, 64))
	}
	if next := operatorActionResultNextStep(ui); next != "n/a" {
		lines = append(lines, "new next "+truncateWithEllipsis(next, 64))
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

func buildActivityLines(snapshot Snapshot, host WorkerHost, ui UIState) []string {
	lines := []string{latestCanonicalLine(snapshot)}
	if progress := primaryActionInFlightLine(ui); progress != "n/a" {
		lines = append(lines, "progress "+truncateWithEllipsis(progress, 96))
	}
	if result := operatorActionResultHeadline(ui); result != "n/a" {
		lines = append(lines, "result  "+truncateWithEllipsis(result, 96))
		for _, delta := range operatorActionResultDeltas(ui, 2) {
			lines = append(lines, "delta   "+truncateWithEllipsis(delta, 96))
		}
		if next := operatorActionResultNextStep(ui); next != "n/a" {
			lines = append(lines, "next    "+truncateWithEllipsis(next, 96))
		}
	}
	for _, receipt := range recentOperatorReceiptLines(snapshot, 2) {
		lines = append(lines, receipt)
	}
	if host != nil {
		for _, line := range host.ActivityLines(3) {
			lines = append(lines, line)
		}
	}
	for _, evt := range recentSessionEvents(ui.Session, 3) {
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
		if snapshot.Resolution != nil {
			return "resolved history only"
		}
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
	case "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED":
		return "handoff follow-through review required"
	case "FAILED_RUN_REVIEW_REQUIRED":
		return "failed run review required"
	case "VALIDATION_REVIEW_REQUIRED":
		return "validation review required"
	case "STALE_RUN_RECONCILIATION_REQUIRED":
		return "stale run reconciliation required"
	case "DECISION_REQUIRED":
		return "decision required"
	case "CONTINUE_EXECUTION_REQUIRED":
		return "continue confirmation required"
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
	if action := requiredNextOperatorAction(snapshot); action != "" && action != "NONE" {
		switch action {
		case "START_LOCAL_RUN":
			return "start next run"
		case "RECONCILE_STALE_RUN":
			return "reconcile stale run"
		case "INSPECT_FAILED_RUN":
			return "inspect failed run"
		case "REVIEW_VALIDATION_STATE":
			return "review validation state"
		case "MAKE_RESUME_DECISION":
			return "make resume decision"
		case "RESUME_INTERRUPTED_LINEAGE":
			return "resume interrupted run"
		case "FINALIZE_CONTINUE_RECOVERY":
			return "finalize continue"
		case "EXECUTE_REBRIEF":
			return "regenerate brief"
		case "LAUNCH_ACCEPTED_HANDOFF":
			return "launch accepted handoff"
		case "REVIEW_HANDOFF_FOLLOW_THROUGH":
			return "review handoff follow-through"
		case "RESOLVE_ACTIVE_HANDOFF":
			return "resolve active handoff"
		case "REPAIR_CONTINUITY":
			return "repair continuity"
		default:
			return humanizeConstant(action)
		}
	}
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
	case "REVIEW_HANDOFF_FOLLOW_THROUGH":
		return "review handoff follow-through"
	case "INSPECT_FAILED_RUN":
		return "inspect failed run"
	case "REVIEW_VALIDATION_STATE":
		return "review validation state"
	case "RECONCILE_STALE_RUN":
		return "reconcile stale run"
	case "MAKE_RESUME_DECISION":
		return "make resume decision"
	case "EXECUTE_CONTINUE_RECOVERY":
		return "finalize continue"
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
	if snapshot.OperatorDecision != nil {
		if reason := strings.TrimSpace(snapshot.OperatorDecision.PrimaryReason); reason != "" {
			return reason
		}
	}
	if snapshot.ActionAuthority != nil {
		if action := authorityFor(snapshot, snapshot.ActionAuthority.RequiredNextAction); action != nil {
			if reason := strings.TrimSpace(action.Reason); reason != "" {
				return reason
			}
		}
		for _, candidate := range []string{"LOCAL_MESSAGE_MUTATION", "CREATE_CHECKPOINT", "START_LOCAL_RUN"} {
			if action := authorityFor(snapshot, candidate); action != nil && action.State == "BLOCKED" {
				if reason := strings.TrimSpace(action.Reason); reason != "" {
					return reason
				}
			}
		}
	}
	if snapshot.ActiveBranch != nil {
		if reason := strings.TrimSpace(snapshot.ActiveBranch.Reason); reason != "" {
			return reason
		}
	}
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

func operatorDecisionHeadline(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.OperatorDecision == nil || strings.TrimSpace(snapshot.OperatorDecision.Headline) == "" {
		return "n/a"
	}
	return snapshot.OperatorDecision.Headline
}

func operatorDecisionGuidance(snapshot Snapshot) string {
	if snapshot.OperatorDecision == nil || strings.TrimSpace(snapshot.OperatorDecision.Guidance) == "" {
		return "n/a"
	}
	return snapshot.OperatorDecision.Guidance
}

func operatorDecisionIntegrity(snapshot Snapshot) string {
	if snapshot.OperatorDecision == nil || strings.TrimSpace(snapshot.OperatorDecision.IntegrityNote) == "" {
		return "n/a"
	}
	return snapshot.OperatorDecision.IntegrityNote
}

func operatorExecutionPlanLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return "n/a"
	}
	step := snapshot.OperatorExecutionPlan.PrimaryStep
	label := operatorActionDisplayName(step.Action)
	if label == "" {
		return "n/a"
	}
	prefix := operatorExecutionStatusLabel(step.Status)
	if prefix == "" {
		return label
	}
	return prefix + " " + label
}

func operatorExecutionCommand(snapshot Snapshot) string {
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return "n/a"
	}
	if command := strings.TrimSpace(snapshot.OperatorExecutionPlan.PrimaryStep.CommandHint); command != "" {
		return command
	}
	if action := strings.TrimSpace(snapshot.OperatorExecutionPlan.PrimaryStep.Action); action != "" {
		return "handle " + action
	}
	return "n/a"
}

func operatorActionResultHeadline(ui UIState) string {
	if ui.LastPrimaryActionResult == nil || strings.TrimSpace(ui.LastPrimaryActionResult.Summary) == "" {
		return "n/a"
	}
	result := strings.ToLower(strings.TrimSpace(ui.LastPrimaryActionResult.Outcome))
	if result == "" {
		result = "unknown"
	}
	line := result + " | " + ui.LastPrimaryActionResult.Summary
	if receipt := strings.TrimSpace(ui.LastPrimaryActionResult.ReceiptID); receipt != "" {
		line += " | " + shortTaskID(receipt)
	}
	return line
}

func operatorActionResultDeltas(ui UIState, limit int) []string {
	if ui.LastPrimaryActionResult == nil || len(ui.LastPrimaryActionResult.Deltas) == 0 {
		return nil
	}
	if limit <= 0 || limit >= len(ui.LastPrimaryActionResult.Deltas) {
		return append([]string{}, ui.LastPrimaryActionResult.Deltas...)
	}
	return append([]string{}, ui.LastPrimaryActionResult.Deltas[:limit]...)
}

func operatorActionResultDeltaLine(ui UIState) string {
	deltas := operatorActionResultDeltas(ui, 1)
	if len(deltas) == 0 {
		return "n/a"
	}
	return deltas[0]
}

func operatorActionResultNextStep(ui UIState) string {
	if ui.LastPrimaryActionResult == nil || strings.TrimSpace(ui.LastPrimaryActionResult.NextStep) == "" || strings.TrimSpace(ui.LastPrimaryActionResult.NextStep) == "none" {
		return "n/a"
	}
	return ui.LastPrimaryActionResult.NextStep
}

func latestOperatorReceiptLine(snapshot Snapshot) string {
	if snapshot.LatestOperatorStepReceipt == nil || strings.TrimSpace(snapshot.LatestOperatorStepReceipt.Summary) == "" {
		return "n/a"
	}
	line := strings.ToLower(strings.TrimSpace(snapshot.LatestOperatorStepReceipt.ResultClass))
	if line == "" {
		line = "recorded"
	}
	line += " | " + strings.TrimSpace(snapshot.LatestOperatorStepReceipt.Summary)
	if snapshot.LatestOperatorStepReceipt.ReceiptID != "" {
		line += " | " + shortTaskID(snapshot.LatestOperatorStepReceipt.ReceiptID)
	}
	return line
}

func recentOperatorReceiptLines(snapshot Snapshot, limit int) []string {
	if len(snapshot.RecentOperatorStepReceipts) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(snapshot.RecentOperatorStepReceipts) {
		limit = len(snapshot.RecentOperatorStepReceipts)
	}
	out := make([]string, 0, limit)
	for _, item := range snapshot.RecentOperatorStepReceipts[:limit] {
		summary := nonEmpty(strings.TrimSpace(item.Summary), operatorActionDisplayName(item.ActionHandle))
		out = append(out, fmt.Sprintf("%s  operator %s %s", item.CreatedAt.Format("15:04:05"), strings.ToLower(nonEmpty(item.ResultClass, "recorded")), truncateWithEllipsis(summary, 72)))
	}
	return out
}

func primaryActionInFlightLine(ui UIState) string {
	if ui.PrimaryActionInFlight == nil || strings.TrimSpace(ui.PrimaryActionInFlight.Action) == "" {
		return "n/a"
	}
	return "executing " + operatorActionDisplayName(ui.PrimaryActionInFlight.Action) + "..."
}

func primaryOperatorStepDirectlyExecutable(snapshot Snapshot) bool {
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return false
	}
	return strings.TrimSpace(snapshot.OperatorExecutionPlan.PrimaryStep.CommandSurface) == "DEDICATED"
}

func requiredNextOperatorAction(snapshot Snapshot) string {
	if snapshot.ActionAuthority == nil {
		return ""
	}
	return strings.TrimSpace(snapshot.ActionAuthority.RequiredNextAction)
}

func authorityFor(snapshot Snapshot, action string) *OperatorActionAuthority {
	if snapshot.ActionAuthority == nil {
		return nil
	}
	action = strings.TrimSpace(action)
	for i := range snapshot.ActionAuthority.Actions {
		if snapshot.ActionAuthority.Actions[i].Action == action {
			return &snapshot.ActionAuthority.Actions[i]
		}
	}
	return nil
}

func operatorActionDisplayName(action string) string {
	switch strings.TrimSpace(action) {
	case "LOCAL_MESSAGE_MUTATION":
		return "send local message"
	case "CREATE_CHECKPOINT":
		return "create checkpoint"
	case "START_LOCAL_RUN":
		return "start local run"
	case "RECONCILE_STALE_RUN":
		return "reconcile stale run"
	case "INSPECT_FAILED_RUN":
		return "inspect failed run"
	case "REVIEW_VALIDATION_STATE":
		return "review validation"
	case "MAKE_RESUME_DECISION":
		return "make resume decision"
	case "RESUME_INTERRUPTED_LINEAGE":
		return "resume interrupted lineage"
	case "FINALIZE_CONTINUE_RECOVERY":
		return "finalize continue recovery"
	case "EXECUTE_REBRIEF":
		return "regenerate brief"
	case "LAUNCH_ACCEPTED_HANDOFF":
		return "launch accepted handoff"
	case "REVIEW_HANDOFF_FOLLOW_THROUGH":
		return "review handoff follow-through"
	case "RESOLVE_ACTIVE_HANDOFF":
		return "resolve active handoff"
	case "REPAIR_CONTINUITY":
		return "repair continuity"
	default:
		return humanizeConstant(action)
	}
}

func operatorExecutionStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "REQUIRED_NEXT":
		return "required"
	case "ALLOWED":
		return "allowed"
	case "BLOCKED":
		return "blocked"
	case "NOT_APPLICABLE":
		return "not applicable"
	default:
		return humanizeConstant(status)
	}
}

func operatorAuthorityLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if action := requiredNextOperatorAction(snapshot); action != "" && action != "NONE" {
		return "required " + operatorActionLabel(snapshot)
	}
	if blocked := authorityFor(snapshot, "LOCAL_MESSAGE_MUTATION"); blocked != nil && blocked.State == "BLOCKED" {
		if blocked.BlockingBranchClass == "HANDOFF_CLAUDE" && blocked.BlockingBranchRef != "" {
			return fmt.Sprintf("local mutation blocked by Claude handoff %s", shortTaskID(blocked.BlockingBranchRef))
		}
		return "local mutation blocked"
	}
	if blocked := authorityFor(snapshot, "START_LOCAL_RUN"); blocked != nil && blocked.State == "BLOCKED" {
		return "fresh run blocked"
	}
	return "n/a"
}

func activeBranchLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.ActiveBranch == nil || strings.TrimSpace(snapshot.ActiveBranch.Class) == "" {
		return "n/a"
	}
	switch snapshot.ActiveBranch.Class {
	case "LOCAL":
		switch snapshot.ActiveBranch.ActionabilityAnchor {
		case "BRIEF":
			return fmt.Sprintf("local via brief %s", shortTaskID(snapshot.ActiveBranch.ActionabilityAnchorRef))
		case "CHECKPOINT":
			return fmt.Sprintf("local via checkpoint %s", shortTaskID(snapshot.ActiveBranch.ActionabilityAnchorRef))
		default:
			return "local"
		}
	case "HANDOFF_CLAUDE":
		return fmt.Sprintf("Claude handoff %s", shortTaskID(snapshot.ActiveBranch.BranchRef))
	default:
		return humanizeConstant(snapshot.ActiveBranch.Class)
	}
}

func localResumeLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.LocalResume == nil || strings.TrimSpace(snapshot.LocalResume.State) == "" {
		return "n/a"
	}
	switch snapshot.LocalResume.State {
	case "ALLOWED":
		switch snapshot.LocalResume.Mode {
		case "RESUME_INTERRUPTED_LINEAGE":
			if snapshot.LocalResume.CheckpointID != "" {
				return fmt.Sprintf("allowed via checkpoint %s", shortTaskID(snapshot.LocalResume.CheckpointID))
			}
			return "allowed for interrupted lineage"
		default:
			return "allowed"
		}
	case "BLOCKED":
		if snapshot.LocalResume.BlockingBranchClass == "HANDOFF_CLAUDE" && snapshot.LocalResume.BlockingBranchRef != "" {
			return fmt.Sprintf("blocked by Claude handoff %s", shortTaskID(snapshot.LocalResume.BlockingBranchRef))
		}
		return "blocked"
	default:
		switch snapshot.LocalResume.Mode {
		case "FINALIZE_CONTINUE_RECOVERY":
			return "not applicable | finalize continue first"
		case "START_FRESH_NEXT_RUN":
			return "not applicable | start fresh next run"
		case "RESUME_INTERRUPTED_LINEAGE":
			return "not applicable"
		default:
			return "not applicable"
		}
	}
}

func localRunFinalizationLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.LocalRunFinalization == nil || strings.TrimSpace(snapshot.LocalRunFinalization.State) == "" {
		return "n/a"
	}
	switch snapshot.LocalRunFinalization.State {
	case "NO_RELEVANT_RUN":
		return "none"
	case "FINALIZED":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("finalized %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "finalized"
	case "INTERRUPTED_RECOVERABLE":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("interrupted recoverable %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "interrupted recoverable"
	case "INTERRUPTED_NEEDS_REPAIR":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("interrupted needs repair %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "interrupted needs repair"
	case "FAILED_REVIEW_REQUIRED":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("failed review required %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "failed review required"
	case "STALE_RECONCILIATION_REQUIRED":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("stale reconciliation required %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "stale reconciliation required"
	default:
		return humanizeConstant(snapshot.LocalRunFinalization.State)
	}
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

func handoffContinuityLine(snapshot Snapshot) string {
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State == "" || snapshot.HandoffContinuity.State == "NOT_APPLICABLE" {
		return "n/a"
	}
	switch snapshot.HandoffContinuity.State {
	case "ACCEPTED_NOT_LAUNCHED":
		return "accepted, not launched"
	case "LAUNCH_PENDING_OUTCOME":
		return "launch pending, downstream outcome unknown"
	case "LAUNCH_FAILED_RETRYABLE":
		return "launch failed, retry allowed"
	case "LAUNCH_COMPLETED_ACK_CAPTURED":
		return "launch completed, acknowledgment captured, downstream unproven"
	case "LAUNCH_COMPLETED_ACK_UNAVAILABLE":
		return "launch completed, acknowledgment unavailable, downstream unproven"
	case "LAUNCH_COMPLETED_ACK_MISSING":
		return "launch completed, acknowledgment missing, continuity inconsistent"
	case "FOLLOW_THROUGH_PROOF_OF_LIFE":
		return "proof of life observed, completion unproven"
	case "FOLLOW_THROUGH_CONFIRMED":
		return "continuation confirmed, completion unproven"
	case "FOLLOW_THROUGH_UNKNOWN":
		return "follow-through still unknown"
	case "FOLLOW_THROUGH_STALLED":
		return "follow-through stalled, review required"
	case "RESOLVED":
		return "explicitly resolved, no downstream completion claim"
	default:
		return humanizeConstant(snapshot.HandoffContinuity.State)
	}
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
	if progress := primaryActionInFlightLine(ui); progress != "n/a" {
		parts = append(parts, progress)
	}
	parts = append(parts, "q quit", "h help", "i inspector", "p activity", "r refresh", "s status")
	if cue := footerExecutePrimaryCue(snapshot, ui, host); cue != "" {
		parts = append(parts, cue)
	}
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

func footerExecutePrimaryCue(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if ui.PrimaryActionInFlight != nil {
		return ""
	}
	if !primaryOperatorStepDirectlyExecutable(snapshot) {
		return ""
	}
	if host != nil && ui.Focus == FocusWorker && host.CanAcceptInput() {
		return "ctrl-g n execute next step"
	}
	return "n execute next step"
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

## internal/orchestrator/service_test.go
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
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func testActionAuthority(t *testing.T, actions []OperatorActionAuthority, action OperatorAction) OperatorActionAuthority {
	t.Helper()
	for _, candidate := range actions {
		if candidate.Action == action {
			return candidate
		}
	}
	t.Fatalf("missing action authority for %s", action)
	return OperatorActionAuthority{}
}

func requireOperatorDecision(t *testing.T, decision *OperatorDecisionSummary) *OperatorDecisionSummary {
	t.Helper()
	if decision == nil {
		t.Fatal("expected operator decision summary")
	}
	return decision
}

func requireExecutionPlan(t *testing.T, plan *OperatorExecutionPlan) *OperatorExecutionPlan {
	t.Helper()
	if plan == nil {
		t.Fatal("expected operator execution plan")
	}
	if plan.PrimaryStep == nil {
		t.Fatal("expected operator execution primary step")
	}
	return plan
}

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

func TestMessageTaskBlockedWhileAcceptedClaudeHandoffIsActiveBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed resumable checkpoint: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "active handoff mutation gate",
		Mode:         handoff.ModeResume,
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
	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before blocked message: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversation before blocked message: %v", err)
	}

	_, err = coord.MessageTask(context.Background(), string(taskID), "change the execution brief locally")
	if err == nil || !strings.Contains(err.Error(), "active continuity branch") {
		t.Fatalf("expected active-handoff mutation gate error, got %v", err)
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after blocked message: %v", err)
	}
	if capsAfter.CurrentBriefID != capsBefore.CurrentBriefID {
		t.Fatalf("blocked message must not replace current brief: before=%s after=%s", capsBefore.CurrentBriefID, capsAfter.CurrentBriefID)
	}
	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversation after blocked message: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("blocked message must not append conversation entries: before=%d after=%d", len(convBefore), len(convAfter))
	}
}

func TestStatusTaskDefaultTaskReportsLocalActiveBranchOwner(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local active branch class, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != string(taskID) {
		t.Fatalf("expected local branch ref %s, got %s", taskID, status.ActiveBranchRef)
	}
	if status.ActiveBranchAnchorKind != ActiveBranchAnchorKindBrief {
		t.Fatalf("expected local branch to be anchored by current brief, got %s", status.ActiveBranchAnchorKind)
	}
	if status.ActiveBranchAnchorRef == "" {
		t.Fatal("expected local branch anchor ref")
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected no interrupted-lineage resume authority by default, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeStartFreshNextRun {
		t.Fatalf("expected default local mode to be fresh next run, got %s", status.LocalResumeMode)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Local fresh run ready" || decision.RequiredNextAction != OperatorActionStartLocalRun {
		t.Fatalf("unexpected default operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "start the next bounded local run") {
		t.Fatalf("expected fresh-run guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionStartLocalRun || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected default operator execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku run --task "+string(taskID)+" --action start" {
		t.Fatalf("expected fresh-run command hint, got %+v", plan.PrimaryStep)
	}
}

func TestStatusTaskAcceptedClaudeHandoffReportsClaudeActiveBranchOwner(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff should own continuity",
		Mode:         handoff.ModeResume,
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

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected Claude handoff active branch class, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != createOut.HandoffID {
		t.Fatalf("expected active branch ref %s, got %s", createOut.HandoffID, status.ActiveBranchRef)
	}
	if status.ActiveBranchAnchorKind != ActiveBranchAnchorKindHandoff {
		t.Fatalf("expected handoff anchor kind, got %s", status.ActiveBranchAnchorKind)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected accepted handoff to block local resume authority, got %s", status.LocalResumeAuthorityState)
	}
	if status.RequiredNextOperatorAction != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("expected accepted handoff launch to be required next, got %s", status.RequiredNextOperatorAction)
	}
	messageAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(messageAuthority.Reason, "active continuity branch") {
		t.Fatalf("expected local message mutation block under Claude ownership, got %+v", messageAuthority)
	}
	checkpointAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionCreateCheckpoint)
	if checkpointAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(checkpointAuthority.Reason, "active continuity branch") {
		t.Fatalf("expected checkpoint creation block under Claude ownership, got %+v", checkpointAuthority)
	}
	launchAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLaunchAcceptedHandoff)
	if launchAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected launch-accepted-handoff to be required next, got %+v", launchAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Accepted Claude handoff launch ready" || decision.RequiredNextAction != OperatorActionLaunchAcceptedHandoff {
		t.Fatalf("unexpected accepted-handoff operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "launch the accepted claude handoff") {
		t.Fatalf("expected launch-handoff guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionLaunchAcceptedHandoff || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected accepted-handoff execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku handoff-launch --task "+string(taskID)+" --handoff "+createOut.HandoffID {
		t.Fatalf("expected truthful accepted-handoff launch command, got %+v", plan.PrimaryStep)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	if inspectPlan.PrimaryStep.CommandHint != plan.PrimaryStep.CommandHint {
		t.Fatalf("expected inspect execution plan to use same canonical launch hint, status=%q inspect=%q", plan.PrimaryStep.CommandHint, inspectPlan.PrimaryStep.CommandHint)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected shell operator execution plan")
	}
	if snapshot.OperatorExecutionPlan.PrimaryStep.CommandHint != plan.PrimaryStep.CommandHint {
		t.Fatalf("expected shell execution plan to use same canonical launch hint, status=%q shell=%q", plan.PrimaryStep.CommandHint, snapshot.OperatorExecutionPlan.PrimaryStep.CommandHint)
	}
}

func TestStatusTaskLaunchedClaudeHandoffReportsClaudeActiveBranchOwner(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launched handoff should own continuity",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected launched Claude handoff to own continuity, got %s", status.ActiveBranchClass)
	}
	if status.ActiveBranchRef != createOut.HandoffID {
		t.Fatalf("expected launched active branch ref %s, got %s", createOut.HandoffID, status.ActiveBranchRef)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected launched Claude handoff to block local resume authority, got %s", status.LocalResumeAuthorityState)
	}
}

func TestFinalizedLocalRunDoesNotReportStaleReconciliation(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID: string(taskID),
		Action: "complete",
		RunID:  startOut.RunID,
	}); err != nil {
		t.Fatalf("complete noop run: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationFinalized {
		t.Fatalf("expected finalized local run state, got %s", status.LocalRunFinalizationState)
	}
	if status.RecoveryClass == RecoveryClassStaleRunReconciliationRequired {
		t.Fatalf("finalized run must not report stale reconciliation: %+v", status)
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

func TestRunStartBlockedWhenRecoveryClassDecisionRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "decision") {
		t.Fatalf("expected decision-required canonical response, got %q", res.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected decision-required status recovery class, got %s", status.RecoveryClass)
	}
}

func TestRunStartBlockedWhenRecoveryClassFailedRunReviewRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "failed run") {
		t.Fatalf("expected failed-run review canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartBlockedWhenRecoveryClassRebriefRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	}); err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "brief") {
		t.Fatalf("expected rebrief canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartBlockedWhenRecoveryClassBlockedDrift(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	driftCoord := newTestCoordinator(t, store, &staticAnchorProvider{
		snapshot: anchorgit.Snapshot{
			RepoRoot:         "/tmp/repo",
			Branch:           "feature/drift",
			HeadSHA:          "head-drift",
			WorkingTreeDirty: false,
			CapturedAt:       time.Unix(1700006000, 0).UTC(),
		},
	}, newFakeAdapterSuccess())

	res, err := driftCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked drift run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "drift") {
		t.Fatalf("expected drift canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartBlockedWhenAcceptedClaudeHandoffIsActiveRecoveryPath(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "handoff branch active for launch",
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

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked handoff-path run start result, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "handoff") {
		t.Fatalf("expected handoff canonical response, got %q", res.CanonicalResponse)
	}
}

func TestRunStartStrictContinuePathRequiresExecutedContinueRecovery(t *testing.T) {
	store := newTestStore(t)
	failCoord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, failCoord)

	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
	if _, err := failCoord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if _, err := failCoord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	}); err != nil {
		t.Fatalf("record continue decision: %v", err)
	}

	statusBefore, err := failCoord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status before continue execution: %v", err)
	}
	if statusBefore.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required before execute, got %s", statusBefore.RecoveryClass)
	}
	if statusBefore.ReadyForNextRun {
		t.Fatal("status must not claim ready-for-next-run before continue execution")
	}

	blocked, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start before continue execution should return canonical blocked response, got error: %v", err)
	}
	if blocked.RunID != "" || blocked.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected blocked run start before continue execution, got %+v", blocked)
	}
	if !strings.Contains(strings.ToLower(blocked.CanonicalResponse), "continue") {
		t.Fatalf("expected continue-finalization canonical response, got %q", blocked.CanonicalResponse)
	}

	successCoord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	if _, err := successCoord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute continue recovery: %v", err)
	}

	statusAfter, err := successCoord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after continue execution: %v", err)
	}
	if statusAfter.RecoveryClass != RecoveryClassReadyNextRun || !statusAfter.ReadyForNextRun {
		t.Fatalf("expected ready-next-run after continue execution, got %+v", statusAfter)
	}
	if statusAfter.LatestRecoveryAction == nil || statusAfter.LatestRecoveryAction.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected latest continue-executed action after continue execution, got %+v", statusAfter.LatestRecoveryAction)
	}

	inspectAfter, err := successCoord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect after continue execution: %v", err)
	}
	if inspectAfter.Recovery == nil || inspectAfter.Recovery.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected inspect ready-next-run after continue execution, got %+v", inspectAfter.Recovery)
	}

	allowed, err := successCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start after continue execution: %v", err)
	}
	if allowed.RunID == "" {
		t.Fatalf("expected run id after continue execution, got %+v", allowed)
	}
}

func TestRunStartNoopAlsoUsesRecoveryGate(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("seed failed run: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop run start should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" || res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected noop run start to be blocked by recovery gate, got %+v", res)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "failed run") {
		t.Fatalf("expected noop recovery-gate canonical response, got %q", res.CanonicalResponse)
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

func TestCreateCheckpointBlockedWhileLaunchedClaudeHandoffIsActiveBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "checkpoint mutation gate",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	checkpointBefore, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint before blocked manual checkpoint: %v", err)
	}

	_, err = coord.CreateCheckpoint(context.Background(), string(taskID))
	if err == nil || !strings.Contains(err.Error(), "local checkpoint") {
		t.Fatalf("expected launched-handoff checkpoint gate error, got %v", err)
	}
	checkpointAfter, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after blocked manual checkpoint: %v", err)
	}
	if checkpointAfter.CheckpointID != checkpointBefore.CheckpointID {
		t.Fatalf("blocked checkpoint must not create a newer checkpoint: before=%s after=%s", checkpointBefore.CheckpointID, checkpointAfter.CheckpointID)
	}
}

func TestMessageTaskBlockedWhileLaunchedClaudeHandoffRemainsActiveBranch(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launched handoff message gate",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	_, err = coord.MessageTask(context.Background(), string(taskID), "rewrite the local brief after launching Claude")
	if err == nil || !strings.Contains(err.Error(), "launched Claude handoff") {
		t.Fatalf("expected launched-handoff message gate error, got %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after blocked message: %v", err)
	}
	if status.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("blocked message must leave launched-handoff recovery intact, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("blocked message must not make launched handoff fresh-run ready")
	}
}

func TestMessageTaskAllowedAfterExplicitClaudeHandoffResolution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve and return to local control",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "operator returned local control",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	out, err := coord.MessageTask(context.Background(), string(taskID), "continue local implementation after resolving Claude branch")
	if err != nil {
		t.Fatalf("message task after resolution: %v", err)
	}
	if out.BriefID == "" {
		t.Fatalf("expected message task to persist a new local brief after resolution, got %+v", out)
	}
}

func TestCreateCheckpointAllowedAfterExplicitClaudeHandoffResolution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve launched Claude branch",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionClosedUnproven,
		Summary: "close launched Claude branch without completion proof",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	out, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint after resolution: %v", err)
	}
	if out.CheckpointID == "" {
		t.Fatalf("expected checkpoint after resolution, got %+v", out)
	}
}

func TestRunStartUsesLocalTruthAfterExplicitClaudeHandoffResolution(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve handoff before local run",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control before starting a fresh run",
	}); err != nil {
		t.Fatalf("record handoff resolution: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.HandoffContinuityState != HandoffContinuityStateNotApplicable {
		t.Fatalf("expected no active handoff continuity after resolution, got %s", status.HandoffContinuityState)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %s", status.ActiveBranchClass)
	}
	if !status.ReadyForNextRun || status.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected local next-run truth after resolution, got %+v", status)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable || status.LocalResumeMode != LocalResumeModeStartFreshNextRun {
		t.Fatalf("expected fresh-next-run local resume summary after resolution, got %+v", status)
	}

	runOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run after resolution: %v", err)
	}
	if runOut.RunStatus != rundomain.StatusRunning {
		t.Fatalf("expected noop run to start after resolution, got %+v", runOut)
	}
}

func TestGatingFollowsExplicitActiveBranchOwnerTruth(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "active branch gating truth",
		Mode:         handoff.ModeResume,
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

	statusBefore, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status before: %v", err)
	}
	if statusBefore.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected Claude handoff owner before resolution, got %s", statusBefore.ActiveBranchClass)
	}
	blocked, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("run start while handoff owns continuity: %v", err)
	}
	if blocked.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected run start to be blocked while Claude branch owns continuity, got %+v", blocked)
	}

	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control after review",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	statusAfter, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after: %v", err)
	}
	if statusAfter.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local owner after resolution, got %s", statusAfter.ActiveBranchClass)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"}); err != nil {
		t.Fatalf("expected run start to use local truth after resolution, got %v", err)
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
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class after stale reconciliation, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume interrupted action after stale reconciliation, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("stale-run reconciliation must not claim fresh next-run readiness")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "resume the interrupted execution path") {
		t.Fatalf("expected stale-run reconciliation canonical response to describe interrupted resume, got %q", out.CanonicalResponse)
	}
}

func TestStatusTaskExposesStaleRunFinalizationDistinctFromLocalResumeAuthority(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("leave stale run")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave stale running state")
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale run finalization state, got %s", status.LocalRunFinalizationState)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("stale run should not claim interrupted resume authority, got %s", status.LocalResumeAuthorityState)
	}
	if status.RecoveryClass != RecoveryClassStaleRunReconciliationRequired {
		t.Fatalf("expected stale-run reconciliation recovery, got %s", status.RecoveryClass)
	}
	if status.RequiredNextOperatorAction != OperatorActionReconcileStaleRun {
		t.Fatalf("expected reconcile-stale-run to be required next, got %s", status.RequiredNextOperatorAction)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(strings.ToLower(startAuthority.Reason), "stale run") {
		t.Fatalf("expected stale run to block fresh start explicitly, got %+v", startAuthority)
	}
	reconcileAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionReconcileStaleRun)
	if reconcileAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected stale reconciliation authority to be required next, got %+v", reconcileAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Stale local run reconciliation required" || decision.RequiredNextAction != OperatorActionReconcileStaleRun {
		t.Fatalf("unexpected stale-reconciliation operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "reconcile stale run state") {
		t.Fatalf("expected stale-reconciliation guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionReconcileStaleRun || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected stale-reconciliation execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku continue --task "+string(taskID) {
		t.Fatalf("expected stale-reconciliation command hint, got %+v", plan.PrimaryStep)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LocalRunFinalization == nil || inspectOut.LocalRunFinalization.State != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected inspect stale run finalization summary, got %+v", inspectOut.LocalRunFinalization)
	}
	if inspectOut.LocalResumeAuthority == nil || inspectOut.LocalResumeAuthority.State != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected inspect local resume authority to remain not-applicable for stale run, got %+v", inspectOut.LocalResumeAuthority)
	}
	if inspectOut.ActionAuthority == nil || inspectOut.ActionAuthority.RequiredNextAction != OperatorActionReconcileStaleRun {
		t.Fatalf("expected inspect action authority to expose stale reconciliation distinctly, got %+v", inspectOut.ActionAuthority)
	}
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	if inspectPlan.PrimaryStep.Action != OperatorActionReconcileStaleRun {
		t.Fatalf("expected inspect execution plan to expose stale reconciliation distinctly, got %+v", inspectPlan)
	}
}

func TestStatusTaskFailedRunFinalizationRemainsDistinctFromStaleReconciliation(t *testing.T) {
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

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationFailedReviewRequired {
		t.Fatalf("expected failed-review local run finalization, got %s", status.LocalRunFinalizationState)
	}
	if status.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run review recovery, got %s", status.RecoveryClass)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("failed run should not claim interrupted resume authority, got %s", status.LocalResumeAuthorityState)
	}
}

func TestAcceptedClaudeOwnershipStillBlocksLocalActionabilityWhenStaleRunExists(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "active Claude branch should outrank stale local run actionability",
		Mode:         handoff.ModeResume,
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

	runID := common.RunID("run_stale_branch")
	now := time.Now().UTC()
	if err := store.Runs().Create(rundomain.ExecutionRun{
		RunID:            runID,
		TaskID:           taskID,
		BriefID:          mustCurrentBriefID(t, store, taskID),
		WorkerKind:       rundomain.WorkerKindCodex,
		Status:           rundomain.StatusRunning,
		LastKnownSummary: "synthetic stale run for branch ownership test",
		StartedAt:        now,
		CreatedFromPhase: phase.PhaseExecuting,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create stale run: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.CurrentPhase = phase.PhaseExecuting
	caps.Version++
	caps.UpdatedAt = now
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected accepted Claude handoff to remain active owner, got %s", status.ActiveBranchClass)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale local run finalization truth, got %s", status.LocalRunFinalizationState)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected active Claude ownership to block local actionability, got %s", status.LocalResumeAuthorityState)
	}
}

func TestResolutionLetsLocalOwnershipExposeStaleRunTruthAgain(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed resumable checkpoint: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolution should return stale local truth",
		Mode:         handoff.ModeResume,
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

	runID := common.RunID("run_stale_after_resolution")
	now := time.Now().UTC()
	if err := store.Runs().Create(rundomain.ExecutionRun{
		RunID:            runID,
		TaskID:           taskID,
		BriefID:          mustCurrentBriefID(t, store, taskID),
		WorkerKind:       rundomain.WorkerKindCodex,
		Status:           rundomain.StatusRunning,
		LastKnownSummary: "synthetic stale run before resolution",
		StartedAt:        now,
		CreatedFromPhase: phase.PhaseExecuting,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("create stale run: %v", err)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.CurrentPhase = phase.PhaseExecuting
	caps.Version++
	caps.UpdatedAt = now
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control despite stale run",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %s", status.ActiveBranchClass)
	}
	if status.LocalRunFinalizationState != LocalRunFinalizationStaleReconciliationNeeded {
		t.Fatalf("expected stale run truth to reappear after resolution, got %s", status.LocalRunFinalizationState)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected stale reconciliation to remain distinct from resume after resolution, got %s", status.LocalResumeAuthorityState)
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
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "fresh next bounded run is already ready") {
		t.Fatalf("expected canonical fresh-next-run response, got %q", out.CanonicalResponse)
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
	if out.ReadyForNextRun {
		t.Fatal("interrupted recovery must not claim fresh next-run readiness")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "resume the interrupted execution path") {
		t.Fatalf("expected interrupted recovery canonical response to describe resume, got %q", out.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class in status, got %s", status.RecoveryClass)
	}
	if status.RequiredNextOperatorAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected interrupted resume to be required next, got %s", status.RequiredNextOperatorAction)
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not claim fresh next-run readiness for interrupted recovery")
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityAllowed {
		t.Fatalf("expected interrupted local resume authority to be allowed, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("expected interrupted local resume mode, got %s", status.LocalResumeMode)
	}
	if status.LocalResumeCheckpointID == "" {
		t.Fatal("expected interrupted local resume checkpoint id in status")
	}
	resumeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResumeInterruptedLineage)
	if resumeAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected interrupted resume authority to be required next, got %+v", resumeAuthority)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected fresh local run start to remain blocked during interrupted recovery, got %+v", startAuthority)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class in inspect, got %+v", inspectOut.Recovery)
	}
	if inspectOut.Recovery.ReadyForNextRun {
		t.Fatal("inspect must not claim fresh next-run readiness for interrupted recovery")
	}
	if inspectOut.LocalResumeAuthority == nil {
		t.Fatal("expected inspect local resume authority")
	}
	if inspectOut.LocalResumeAuthority.State != LocalResumeAuthorityAllowed || inspectOut.LocalResumeAuthority.Mode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("unexpected inspect local resume authority: %+v", inspectOut.LocalResumeAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Interrupted local lineage recoverable" || decision.RequiredNextAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("unexpected interrupted operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "resume the interrupted local lineage") {
		t.Fatalf("expected interrupted-lineage guidance, got %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.IntegrityNote), "checkpoint resumability") {
		t.Fatalf("expected checkpoint caution note for interrupted recovery, got %+v", decision)
	}
	if inspectDecision := requireOperatorDecision(t, inspectOut.OperatorDecision); inspectDecision.RequiredNextAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected inspect operator decision to match interrupted resume truth, got %+v", inspectDecision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected interrupted execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku recovery resume-interrupted --task "+string(taskID) {
		t.Fatalf("expected interrupted-resume command hint, got %+v", plan.PrimaryStep)
	}
	inspectPlan := requireExecutionPlan(t, inspectOut.OperatorExecutionPlan)
	if inspectPlan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected inspect execution plan to preserve interrupted resume truth, got %+v", inspectPlan)
	}
}

func TestStatusTaskContinueExecutionRequiredDistinguishesLocalResumeAuthority(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "continue recovery distinction test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{
		TaskID:  string(taskID),
		Summary: "operator resumed interrupted lineage",
	}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required recovery, got %s", status.RecoveryClass)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityNotApplicable {
		t.Fatalf("expected interrupted resume authority to be not applicable after interrupted-resume execution, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeFinalizeContinueRecovery {
		t.Fatalf("expected finalize-continue local mode, got %s", status.LocalResumeMode)
	}
	if status.RequiredNextOperatorAction != OperatorActionFinalizeContinueRecovery {
		t.Fatalf("expected finalize-continue to be required next, got %s", status.RequiredNextOperatorAction)
	}
	finalizeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionFinalizeContinueRecovery)
	if finalizeAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected finalize-continue authority to be required next, got %+v", finalizeAuthority)
	}
	resumeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResumeInterruptedLineage)
	if resumeAuthority.State != OperatorActionAuthorityNotApplicable {
		t.Fatalf("expected interrupted resume to remain distinct after continue confirmation, got %+v", resumeAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Continue finalization required" || decision.RequiredNextAction != OperatorActionFinalizeContinueRecovery {
		t.Fatalf("unexpected continue-confirmation operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "finalize continue recovery") {
		t.Fatalf("expected finalize-continue guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionFinalizeContinueRecovery || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected continue-finalization execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku recovery continue --task "+string(taskID) {
		t.Fatalf("expected continue-recovery command hint, got %+v", plan.PrimaryStep)
	}
}

func TestBlockingClaudeBranchDoesNotLetRawCheckpointOverrideLocalResumeAuthority(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "raw checkpoint authority test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "accepted handoff should block local resume authority",
		Mode:         handoff.ModeResume,
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

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if !status.CheckpointResumable {
		t.Fatal("expected raw checkpoint resumability to remain visible")
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityBlocked {
		t.Fatalf("expected blocked local resume authority under active Claude handoff, got %s", status.LocalResumeAuthorityState)
	}
	if status.LocalResumeMode != LocalResumeModeNone {
		t.Fatalf("expected no authorized local resume mode while handoff owns continuity, got %s", status.LocalResumeMode)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LocalResumeAuthority == nil {
		t.Fatal("expected inspect local resume authority")
	}
	if inspectOut.LocalResumeAuthority.State != LocalResumeAuthorityBlocked {
		t.Fatalf("expected inspect local resume authority to be blocked, got %+v", inspectOut.LocalResumeAuthority)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityBlocked || !strings.Contains(strings.ToLower(startAuthority.Reason), "claude handoff") {
		t.Fatalf("expected active Claude ownership to block fresh run authority, got %+v", startAuthority)
	}
}

func TestResolutionCanRestoreInterruptedLocalResumeAuthority(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "resolution resume authority test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "resolve back to interrupted local lineage",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.RecordHandoffResolution(context.Background(), RecordHandoffResolutionRequest{
		TaskID:  string(taskID),
		Kind:    handoff.ResolutionSupersededByLocal,
		Summary: "return local control to interrupted lineage",
	}); err != nil {
		t.Fatalf("resolve handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.ActiveBranchClass != ActiveBranchClassLocal {
		t.Fatalf("expected local branch owner after resolution, got %s", status.ActiveBranchClass)
	}
	if status.LocalResumeAuthorityState != LocalResumeAuthorityAllowed || status.LocalResumeMode != LocalResumeModeResumeInterruptedLineage {
		t.Fatalf("expected interrupted local resume authority to return after resolution, got %+v", status)
	}
	if status.RequiredNextOperatorAction != OperatorActionResumeInterruptedLineage {
		t.Fatalf("expected interrupted resume to become required next after resolution, got %s", status.RequiredNextOperatorAction)
	}
	messageAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected local message mutation to return after resolution, got %+v", messageAuthority)
	}
	checkpointAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionCreateCheckpoint)
	if checkpointAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected checkpoint creation to return after resolution, got %+v", checkpointAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.ActiveOwnerClass != ActiveBranchClassLocal {
		t.Fatalf("expected local owner in post-resolution decision summary, got %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.IntegrityNote), "historical claude branch resolution") {
		t.Fatalf("expected historical-resolution caution note without completion claim, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResumeInterruptedLineage || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext {
		t.Fatalf("unexpected post-resolution execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku recovery resume-interrupted --task "+string(taskID) {
		t.Fatalf("expected post-resolution interrupted-resume command hint, got %+v", plan.PrimaryStep)
	}
}

func TestReadyNextRunActionAuthorityAllowsFreshRunWithoutResume(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RequiredNextOperatorAction != OperatorActionStartLocalRun {
		t.Fatalf("expected start-local-run to be required next, got %s", status.RequiredNextOperatorAction)
	}
	startAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionStartLocalRun)
	if startAuthority.State != OperatorActionAuthorityRequiredNext {
		t.Fatalf("expected fresh local run to be required next, got %+v", startAuthority)
	}
	resumeAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResumeInterruptedLineage)
	if resumeAuthority.State != OperatorActionAuthorityNotApplicable {
		t.Fatalf("expected interrupted resume to remain distinct from fresh-next-run readiness, got %+v", resumeAuthority)
	}
}

func TestLaunchedClaudeHandoffActionAuthorityAllowsResolutionWhileBlockingUnsafeMutation(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launched handoff authority test",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	resolveAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionResolveActiveHandoff)
	if resolveAuthority.State != OperatorActionAuthorityAllowed {
		t.Fatalf("expected launched handoff to allow explicit resolution, got %+v", resolveAuthority)
	}
	messageAuthority := testActionAuthority(t, status.ActionAuthority, OperatorActionLocalMessageMutation)
	if messageAuthority.State != OperatorActionAuthorityBlocked {
		t.Fatalf("expected launched handoff to block local mutation, got %+v", messageAuthority)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Claude handoff branch active" || decision.ActiveOwnerClass != ActiveBranchClassHandoffClaude {
		t.Fatalf("unexpected launched handoff operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "monitor or explicitly resolve") {
		t.Fatalf("expected launched handoff guidance, got %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.IntegrityNote), "downstream claude completion remains unproven") {
		t.Fatalf("expected launched-handoff caution note, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionResolveActiveHandoff || plan.PrimaryStep.Status != OperatorActionAuthorityAllowed || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected launched-handoff execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku handoff-resolve --task "+string(taskID)+" --handoff "+createOut.HandoffID+" --kind <abandoned|superseded-by-local|closed-unproven|reviewed-stale>" {
		t.Fatalf("expected truthful launched-handoff resolution command hint, got %+v", plan.PrimaryStep)
	}
}

func TestStalledFollowThroughDecisionSummaryRequiresReview(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "stalled follow-through decision summary",
		Mode:         handoff.ModeResume,
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
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{
		TaskID:    string(taskID),
		HandoffID: createOut.HandoffID,
	}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{
		TaskID:  string(taskID),
		Kind:    handoff.FollowThroughStalledReviewRequired,
		Summary: "Claude follow-through appears stalled",
	}); err != nil {
		t.Fatalf("record handoff follow-through: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	decision := requireOperatorDecision(t, status.OperatorDecision)
	if decision.Headline != "Claude follow-through review required" || decision.RequiredNextAction != OperatorActionReviewHandoffFollowUp {
		t.Fatalf("unexpected stalled-follow-through operator decision summary: %+v", decision)
	}
	if !strings.Contains(strings.ToLower(decision.Guidance), "review the stalled claude follow-through") {
		t.Fatalf("expected stalled-follow-through guidance, got %+v", decision)
	}
	plan := requireExecutionPlan(t, status.OperatorExecutionPlan)
	if plan.PrimaryStep.Action != OperatorActionReviewHandoffFollowUp || plan.PrimaryStep.Status != OperatorActionAuthorityRequiredNext || !plan.MandatoryBeforeProgress {
		t.Fatalf("unexpected stalled-follow-through execution plan: %+v", plan)
	}
	if plan.PrimaryStep.CommandHint != "tuku inspect --task "+string(taskID) {
		t.Fatalf("expected truthful stalled-follow-through review command hint, got %+v", plan.PrimaryStep)
	}
}

func TestRecordRecoveryActionInterruptedRunReviewedKeepsInterruptedPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
		Notes:   []string{"preserve interrupted lineage"},
	})
	if err != nil {
		t.Fatalf("record interrupted-run review: %v", err)
	}
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class after review, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume-interrupted action after review, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted review must not make the task fresh-start ready")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "interrupted") || strings.Contains(strings.ToLower(out.CanonicalResponse), "start a run") {
		t.Fatalf("expected interrupted-lineage canonical response, got %q", out.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassInterruptedRunRecoverable || status.ReadyForNextRun {
		t.Fatalf("unexpected status recovery after interrupted review: %+v", status)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("expected latest recovery action to be interrupted review, got %+v", status.LatestRecoveryAction)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable || inspectOut.Recovery.ReadyForNextRun {
		t.Fatalf("unexpected inspect recovery after interrupted review: %+v", inspectOut.Recovery)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("expected inspect latest recovery action to be interrupted review, got %+v", inspectOut.LatestRecoveryAction)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable || snapshot.Recovery.ReadyForNextRun {
		t.Fatalf("unexpected shell recovery after interrupted review: %+v", snapshot.Recovery)
	}
	if !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "reviewed") {
		t.Fatalf("expected reviewed interrupted-lineage reason in shell snapshot, got %q", snapshot.Recovery.Reason)
	}

	runStart, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if runStart.RunID != "" || runStart.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected interrupted-review run start to remain blocked, got %+v", runStart)
	}
	if !strings.Contains(strings.ToLower(runStart.CanonicalResponse), "interrupted") {
		t.Fatalf("expected interrupted recovery blocked response, got %q", runStart.CanonicalResponse)
	}

	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventInterruptedRunReviewed) {
		t.Fatal("expected interrupted-run-reviewed proof event")
	}
	if hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("interrupted review should emit only the specific interrupted-review proof event")
	}
}

func TestRecordRecoveryActionInterruptedRunReviewedRejectsInvalidPostureAndReplaysIdempotently(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindInterruptedRunReviewed,
	}); err == nil || !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) {
		t.Fatalf("expected interrupted-review invalid-posture rejection, got %v", err)
	}

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	})
	if err != nil {
		t.Fatalf("first interrupted review: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	})
	if err != nil {
		t.Fatalf("second interrupted review: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected interrupted-review replay to reuse latest action, got %s then %s", first.Action.ActionID, second.Action.ActionID)
	}
}

func TestRecordRecoveryActionInterruptedRunReviewedReplayRejectedAfterPostureChanges(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	}); err != nil {
		t.Fatalf("record interrupted review: %v", err)
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run: %v", err)
	}
	runRec.Status = rundomain.StatusCompleted
	now := time.Now().UTC()
	runRec.EndedAt = &now
	runRec.LastKnownSummary = "execution completed after out-of-band reconciliation"
	if err := store.Runs().Update(runRec); err != nil {
		t.Fatalf("update run: %v", err)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	caps.CurrentPhase = phase.PhaseValidating
	caps.NextAction = "Validation review is required before another run."
	caps.Version++
	caps.UpdatedAt = now
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update capsule: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.RecoveryClass != RecoveryClassValidationReviewRequired {
		t.Fatalf("expected validation-review posture after mutation, got %s", status.RecoveryClass)
	}

	_, err = coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindInterruptedRunReviewed,
		Summary: "interrupted lineage reviewed",
	})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) {
		t.Fatalf("expected interrupted-review replay to reject outside interrupted posture, got %v", err)
	}
}

func TestExecuteInterruptedResumeTransitionsToContinueExecutionRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{
		TaskID:  string(taskID),
		Summary: "operator resumed interrupted lineage",
		Notes:   []string{"maintain interrupted lineage semantics"},
	})
	if err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}
	if out.Action.Kind != recoveryaction.KindInterruptedResumeExecuted {
		t.Fatalf("expected interrupted-resume action kind, got %s", out.Action.Kind)
	}
	if out.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionExecuteContinueRecovery {
		t.Fatalf("expected execute-continue-recovery action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted resume must not claim fresh next-run readiness")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "not claiming fresh-run readiness") {
		t.Fatalf("expected honest interrupted resume canonical response, got %q", out.CanonicalResponse)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}
	if !strings.Contains(strings.ToLower(caps.NextAction), "execute continue recovery") {
		t.Fatalf("expected next action to require continue recovery, got %q", caps.NextAction)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.Phase != phase.PhaseBriefReady {
		t.Fatalf("expected status phase %s after interrupted resume, got %s", phase.PhaseBriefReady, status.Phase)
	}
	if status.RecoveryClass != RecoveryClassContinueExecutionRequired || status.ReadyForNextRun {
		t.Fatalf("unexpected status recovery after interrupted resume: %+v", status)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedResumeExecuted {
		t.Fatalf("expected latest recovery action to be interrupted resume, got %+v", status.LatestRecoveryAction)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassContinueExecutionRequired || inspectOut.Recovery.ReadyForNextRun {
		t.Fatalf("unexpected inspect recovery after interrupted resume: %+v", inspectOut.Recovery)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindInterruptedResumeExecuted {
		t.Fatalf("expected inspect latest recovery action to be interrupted resume, got %+v", inspectOut.LatestRecoveryAction)
	}

	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventInterruptedRunResumeExecuted) {
		t.Fatal("expected interrupted-run-resume-executed proof event")
	}
	if hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("interrupted resume should emit only the dedicated interrupted resume proof event")
	}
}

func TestExecuteInterruptedResumeBlocksFreshRunUntilContinueRecovery(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	runStart, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start should return canonical blocked response, got error: %v", err)
	}
	if runStart.RunID != "" || runStart.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected fresh run start to remain blocked, got %+v", runStart)
	}
	if !strings.Contains(strings.ToLower(runStart.CanonicalResponse), "continue finalization") {
		t.Fatalf("expected continue-finalization block reason, got %q", runStart.CanonicalResponse)
	}
}

func TestExecuteInterruptedResumeRejectsInvalidPostureAndReplayAfterSuccess(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err == nil || !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) {
		t.Fatalf("expected interrupted resume invalid-posture rejection, got %v", err)
	}

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}
	_, err = coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)})
	if err == nil {
		t.Fatal("expected interrupted resume replay rejection after success")
	}
	if !strings.Contains(err.Error(), string(RecoveryClassInterruptedRunRecoverable)) && !strings.Contains(strings.ToLower(err.Error()), "already been executed") {
		t.Fatalf("expected interrupted resume replay to reject on posture change or prior execution, got %v", err)
	}
}

func TestExecuteContinueRecoveryAcceptsInterruptedResumeTrigger(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startOut.RunID,
		InterruptionReason: "operator interrupted",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}

	out, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute continue recovery after interrupted resume: %v", err)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun || !out.ReadyForNextRun {
		t.Fatalf("expected ready-next-run after interrupted resume + continue recovery, got %+v", out)
	}
	if out.Action.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected continue-executed action after interrupted resume trigger, got %+v", out.Action)
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

func TestRecordRecoveryActionDecisionContinueRequiresExplicitContinueExecution(t *testing.T) {
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
	if out.RecoveryClass != RecoveryClassContinueExecutionRequired {
		t.Fatalf("expected continue-execution-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionExecuteContinueRecovery {
		t.Fatalf("expected execute-continue-recovery action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("continue decision must not claim ready-for-next-run before continue execution")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}
	if !strings.Contains(strings.ToLower(caps.NextAction), "execute continue recovery") {
		t.Fatalf("expected capsule next action to require continue execution, got %q", caps.NextAction)
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
	if second.RecoveryClass != RecoveryClassContinueExecutionRequired || second.ReadyForNextRun {
		t.Fatalf("expected continue-execution-required after decision continue replay, got %+v", second)
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

func TestRecordRecoveryActionRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassDecisionRequired)) {
		t.Fatalf("expected decision-required posture rejection, got %v", err)
	}
}

func TestExecuteRebriefRegeneratesBriefAndReadiesTask(t *testing.T) {
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
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	}); err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}

	beforeCaps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before rebrief: %v", err)
	}
	beforeBriefID := beforeCaps.CurrentBriefID

	out, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute rebrief: %v", err)
	}
	if out.PreviousBriefID != beforeBriefID {
		t.Fatalf("expected previous brief %s, got %s", beforeBriefID, out.PreviousBriefID)
	}
	if out.BriefID == "" || out.BriefID == beforeBriefID {
		t.Fatalf("expected new brief id, got %s", out.BriefID)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected ready-next-run recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionStartNextRun {
		t.Fatalf("expected start-next-run action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected ready-for-next-run after rebrief")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after rebrief: %v", err)
	}
	if caps.CurrentBriefID != out.BriefID {
		t.Fatalf("expected capsule current brief %s, got %s", out.BriefID, caps.CurrentBriefID)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}

	briefRec, err := store.Briefs().Get(out.BriefID)
	if err != nil {
		t.Fatalf("get regenerated brief: %v", err)
	}
	if briefRec.BriefHash != out.BriefHash {
		t.Fatalf("expected brief hash %s, got %s", out.BriefHash, briefRec.BriefHash)
	}

	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventBriefRegenerated) {
		t.Fatal("expected brief-regenerated proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CurrentBriefID != out.BriefID {
		t.Fatalf("expected status current brief %s, got %s", out.BriefID, status.CurrentBriefID)
	}
	if status.RecoveryClass != RecoveryClassReadyNextRun || !status.ReadyForNextRun {
		t.Fatalf("expected ready-next-run status after rebrief, got %+v", status)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Brief == nil || inspectOut.Brief.BriefID != out.BriefID {
		t.Fatalf("expected inspect current brief %s, got %+v", out.BriefID, inspectOut.Brief)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected inspect ready-next-run recovery, got %+v", inspectOut.Recovery)
	}
}

func TestExecuteRebriefRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassRebriefRequired)) {
		t.Fatalf("expected rebrief-required rejection, got %v", err)
	}
}

func TestExecuteRebriefReplayRejectedAfterSuccessfulExecution(t *testing.T) {
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
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	}); err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}
	if _, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("first execute rebrief: %v", err)
	}

	_, err := coord.ExecuteRebrief(context.Background(), ExecuteRebriefRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassRebriefRequired)) {
		t.Fatalf("expected replay rebrief rejection after success, got %v", err)
	}
}

func TestExecuteContinueRecoveryFinalizesReadyState(t *testing.T) {
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
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	}); err != nil {
		t.Fatalf("record continue decision: %v", err)
	}

	beforeCaps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before continue execution: %v", err)
	}
	beforeBrief, err := store.Briefs().Get(beforeCaps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get current brief before continue execution: %v", err)
	}

	out, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute continue recovery: %v", err)
	}
	if out.BriefID != beforeBrief.BriefID {
		t.Fatalf("expected continue recovery to keep brief %s, got %s", beforeBrief.BriefID, out.BriefID)
	}
	if out.BriefHash != beforeBrief.BriefHash {
		t.Fatalf("expected continue recovery to keep brief hash %s, got %s", beforeBrief.BriefHash, out.BriefHash)
	}
	if out.Action.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected continue-executed action, got %s", out.Action.Kind)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected ready-next-run recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionStartNextRun {
		t.Fatalf("expected start-next-run action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected ready-for-next-run after continue recovery execution")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "confirmed continuation") {
		t.Fatalf("expected canonical response to describe continue confirmation, got %q", out.CanonicalResponse)
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after continue execution: %v", err)
	}
	if caps.CurrentBriefID != beforeBrief.BriefID {
		t.Fatalf("expected capsule current brief %s, got %s", beforeBrief.BriefID, caps.CurrentBriefID)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}

	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventRecoveryContinueExecuted) {
		t.Fatal("expected recovery-continue-executed proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CurrentBriefID != beforeBrief.BriefID {
		t.Fatalf("expected status current brief %s, got %s", beforeBrief.BriefID, status.CurrentBriefID)
	}
	if status.RecoveryClass != RecoveryClassReadyNextRun || !status.ReadyForNextRun {
		t.Fatalf("expected ready-next-run status after continue execution, got %+v", status)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected latest continue-executed action in status, got %+v", status.LatestRecoveryAction)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Brief == nil || inspectOut.Brief.BriefID != beforeBrief.BriefID {
		t.Fatalf("expected inspect current brief %s, got %+v", beforeBrief.BriefID, inspectOut.Brief)
	}
	if inspectOut.Recovery == nil || inspectOut.Recovery.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected inspect ready-next-run recovery, got %+v", inspectOut.Recovery)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindContinueExecuted {
		t.Fatalf("expected inspect latest continue-executed action, got %+v", inspectOut.LatestRecoveryAction)
	}
}

func TestExecuteContinueRecoveryRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassContinueExecutionRequired)) || !strings.Contains(err.Error(), string(recoveryaction.KindDecisionContinue)) || !strings.Contains(err.Error(), string(recoveryaction.KindInterruptedResumeExecuted)) {
		t.Fatalf("expected continue-execution-required trigger rejection, got %v", err)
	}
}

func TestExecuteContinueRecoveryReplayRejectedAfterSuccessfulExecution(t *testing.T) {
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
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	}); err != nil {
		t.Fatalf("record continue decision: %v", err)
	}
	if _, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("first execute continue recovery: %v", err)
	}

	_, err := coord.ExecuteContinueRecovery(context.Background(), ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "already been executed") {
		t.Fatalf("expected replay continue recovery rejection after success, got %v", err)
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

func mustCurrentBriefID(t *testing.T, store storage.Store, taskID common.TaskID) common.BriefID {
	t.Helper()
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule for brief id: %v", err)
	}
	if caps.CurrentBriefID == "" {
		t.Fatal("expected current brief id")
	}
	return caps.CurrentBriefID
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

func (s *faultInjectedStore) OperatorStepReceipts() storage.OperatorStepReceiptStore {
	return s.base.OperatorStepReceipts()
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

func (s *txCountingStore) OperatorStepReceipts() storage.OperatorStepReceiptStore {
	return s.base.OperatorStepReceipts()
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

func TestExecutePrimaryOperatorStepStartRunRecordsDurableReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionStartLocalRun) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.Receipt.RunID == "" {
		t.Fatalf("expected run target on receipt, got %+v", out.Receipt)
	}
	if len(out.RecentOperatorStepReceipts) == 0 || out.RecentOperatorStepReceipts[0].ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected latest receipt in recent history, got %+v", out.RecentOperatorStepReceipts)
	}
}

func TestExecutePrimaryOperatorStepInterruptedResumeAdvancesToContinueRecovery(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "interrupt", RunID: startOut.RunID, InterruptionReason: "operator next test"}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionResumeInterruptedLineage) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.OperatorExecutionPlan.PrimaryStep == nil || out.OperatorExecutionPlan.PrimaryStep.Action != OperatorActionFinalizeContinueRecovery {
		t.Fatalf("expected continue recovery next, got %+v", out.OperatorExecutionPlan)
	}
}

func TestExecutePrimaryOperatorStepContinueRecoveryAdvancesToFreshRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "interrupt", RunID: startOut.RunID, InterruptionReason: "operator next test"}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if _, err := coord.ExecuteInterruptedResume(context.Background(), ExecuteInterruptedResumeRequest{TaskID: string(taskID)}); err != nil {
		t.Fatalf("resume interrupted lineage: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionFinalizeContinueRecovery) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.OperatorExecutionPlan.PrimaryStep == nil || out.OperatorExecutionPlan.PrimaryStep.Action != OperatorActionStartLocalRun {
		t.Fatalf("expected start-local-run next, got %+v", out.OperatorExecutionPlan)
	}
}

func TestExecutePrimaryOperatorStepAcceptedHandoffLaunchRecordsReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{TaskID: string(taskID), TargetWorker: rundomain.WorkerKindClaude, Reason: "operator next launch", Mode: handoff.ModeResume})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID, AcceptedBy: rundomain.WorkerKindClaude}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionLaunchAcceptedHandoff) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.ActiveBranch.Class != ActiveBranchClassHandoffClaude {
		t.Fatalf("expected launched Claude branch to remain active, got %+v", out.ActiveBranch)
	}
	if strings.Contains(strings.ToLower(out.CanonicalResponse), "completed coding") {
		t.Fatalf("launch response overclaimed downstream completion: %q", out.CanonicalResponse)
	}
}

func TestExecutePrimaryOperatorStepResolveActiveHandoffReturnsToLocalOwnership(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{TaskID: string(taskID), TargetWorker: rundomain.WorkerKindClaude, Reason: "operator next resolve", Mode: handoff.ModeResume})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID, AcceptedBy: rundomain.WorkerKindClaude}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionResolveActiveHandoff) || out.Receipt.ResultClass != operatorstep.ResultSucceeded {
		t.Fatalf("unexpected receipt: %+v", out.Receipt)
	}
	if out.ActiveBranch.Class != ActiveBranchClassLocal {
		t.Fatalf("expected local branch ownership after resolution, got %+v", out.ActiveBranch)
	}
}

func TestExecutePrimaryOperatorStepInspectFallbackRecordsRejectedReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), newFakeHandoffLauncherSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{TaskID: string(taskID), TargetWorker: rundomain.WorkerKindClaude, Reason: "operator next inspect fallback", Mode: handoff.ModeResume})
	if err != nil {
		t.Fatalf("create handoff: %v", err)
	}
	if _, err := coord.AcceptHandoff(context.Background(), AcceptHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID, AcceptedBy: rundomain.WorkerKindClaude}); err != nil {
		t.Fatalf("accept handoff: %v", err)
	}
	if _, err := coord.LaunchHandoff(context.Background(), LaunchHandoffRequest{TaskID: string(taskID), HandoffID: createOut.HandoffID}); err != nil {
		t.Fatalf("launch handoff: %v", err)
	}
	if _, err := coord.RecordHandoffFollowThrough(context.Background(), RecordHandoffFollowThroughRequest{TaskID: string(taskID), Kind: handoff.FollowThroughStalledReviewRequired}); err != nil {
		t.Fatalf("record stalled follow-through: %v", err)
	}

	out, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if out.Receipt.ActionHandle != string(OperatorActionReviewHandoffFollowUp) || out.Receipt.ResultClass != operatorstep.ResultRejected || out.Receipt.ExecutionAttempted {
		t.Fatalf("expected rejected non-executable receipt, got %+v", out.Receipt)
	}
}

func TestOperatorStepReceiptHistorySurfacesInInspectAndShellSnapshot(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	startOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "interrupt", RunID: startOut.RunID, InterruptionReason: "history transport"}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	first, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute interrupted resume: %v", err)
	}
	second, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute continue recovery: %v", err)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestOperatorStepReceipt == nil || inspectOut.LatestOperatorStepReceipt.ReceiptID != second.Receipt.ReceiptID {
		t.Fatalf("expected latest inspect receipt %s, got %+v", second.Receipt.ReceiptID, inspectOut.LatestOperatorStepReceipt)
	}
	if len(inspectOut.RecentOperatorStepReceipts) < 2 || inspectOut.RecentOperatorStepReceipts[1].ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("expected inspect recent receipt history, got %+v", inspectOut.RecentOperatorStepReceipts)
	}

	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if shellOut.LatestOperatorStepReceipt == nil || shellOut.LatestOperatorStepReceipt.ReceiptID != second.Receipt.ReceiptID {
		t.Fatalf("expected latest shell receipt %s, got %+v", second.Receipt.ReceiptID, shellOut.LatestOperatorStepReceipt)
	}
	if len(shellOut.RecentOperatorStepReceipts) < 2 || shellOut.RecentOperatorStepReceipts[1].ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("expected shell receipt history, got %+v", shellOut.RecentOperatorStepReceipts)
	}
}

func TestLaunchDispatchFromResultMarksReusedLaunchAsNoop(t *testing.T) {
	continuity := HandoffContinuity{HandoffID: "hnd_123", LaunchID: "launch_123"}
	dispatch := launchDispatchFromResult(continuity, LaunchHandoffResult{HandoffID: "hnd_123", LaunchID: "launch_123", LaunchStatus: HandoffLaunchStatusCompleted, CanonicalResponse: "reused launch"})
	if dispatch.resultClass != operatorstep.ResultNoopReused {
		t.Fatalf("expected noop reused launch classification, got %+v", dispatch)
	}
}

func TestStatusTaskSurfacesLatestOperatorStepReceipt(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	result, err := coord.ExecutePrimaryOperatorStep(context.Background(), ExecutePrimaryOperatorStepRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestOperatorStepReceipt == nil || status.LatestOperatorStepReceipt.ReceiptID != result.Receipt.ReceiptID {
		t.Fatalf("expected latest status receipt %s, got %+v", result.Receipt.ReceiptID, status.LatestOperatorStepReceipt)
	}
}
```

## internal/runtime/daemon/service_test.go
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
	"tuku/internal/domain/operatorstep"
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
				ActiveBranch: &orchestrator.ShellActiveBranchSummary{
					Class:                  orchestrator.ActiveBranchClassHandoffClaude,
					BranchRef:              "hnd_1",
					ActionabilityAnchor:    orchestrator.ActiveBranchAnchorKindHandoff,
					ActionabilityAnchorRef: "hnd_1",
					Reason:                 "accepted Claude handoff branch currently owns continuity",
				},
				LocalRunFinalization: &orchestrator.ShellLocalRunFinalizationSummary{
					State:     orchestrator.LocalRunFinalizationStaleReconciliationNeeded,
					RunID:     common.RunID("run_1"),
					RunStatus: run.StatusRunning,
					Reason:    "latest run is still durably RUNNING and requires explicit stale reconciliation",
				},
				LocalResume: &orchestrator.ShellLocalResumeAuthoritySummary{
					State:               orchestrator.LocalResumeAuthorityBlocked,
					Mode:                orchestrator.LocalResumeModeNone,
					BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude,
					BlockingBranchRef:   "hnd_1",
					Reason:              "local interrupted-lineage resume is blocked while Claude handoff branch hnd_1 owns continuity",
				},
				ActionAuthority: &orchestrator.ShellOperatorActionAuthoritySet{
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					Actions: []orchestrator.ShellOperatorActionAuthority{
						{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_1", Reason: "Cannot send a local task message while launched Claude handoff hnd_1 remains the active continuity branch."},
						{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launched handoff follow-through appears stalled and requires review"},
					},
				},
				OperatorExecutionPlan: &orchestrator.ShellOperatorExecutionPlan{
					PrimaryStep: &orchestrator.ShellOperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_123",
						Reason:      "launched handoff follow-through appears stalled and requires review",
					},
					MandatoryBeforeProgress: true,
					BlockedSteps: []orchestrator.ShellOperatorExecutionStep{
						{Action: orchestrator.OperatorActionLocalMessageMutation, Status: orchestrator.OperatorActionAuthorityBlocked, Domain: orchestrator.OperatorExecutionDomainLocal, CommandHint: "tuku message --task tsk_123 --text \"<message>\"", Reason: "Cannot send a local task message while launched Claude handoff hnd_1 remains the active continuity branch."},
					},
				},
				LaunchControl: &orchestrator.ShellLaunchControlSummary{
					State:            orchestrator.LaunchControlStateCompleted,
					RetryDisposition: orchestrator.LaunchRetryDispositionBlocked,
					Reason:           "launcher invocation completed; downstream continuation remains unproven",
					HandoffID:        "hnd_1",
					AttemptID:        "hlc_1",
					LaunchID:         "launch_1",
				},
				HandoffContinuity: &orchestrator.ShellHandoffContinuitySummary{
					State:                        orchestrator.HandoffContinuityStateFollowThroughProofOfLife,
					Reason:                       "Claude handoff launch has downstream proof-of-life evidence, but downstream completion remains unproven",
					LaunchAttemptID:              "hlc_1",
					FollowThroughID:              "hft_1",
					FollowThroughKind:            handoff.FollowThroughProofOfLifeObserved,
					FollowThroughSummary:         "later Claude proof of life observed",
					DownstreamContinuationProven: true,
				},
				FollowThrough: &orchestrator.ShellFollowThroughSummary{
					RecordID:        "hft_1",
					Kind:            handoff.FollowThroughProofOfLifeObserved,
					Summary:         "later Claude proof of life observed",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					CreatedAt:       time.Unix(1710000300, 0).UTC(),
				},
				Resolution: &orchestrator.ShellResolutionSummary{
					ResolutionID:    "hrs_1",
					Kind:            handoff.ResolutionSupersededByLocal,
					Summary:         "operator returned local control",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					CreatedAt:       time.Unix(1710000400, 0).UTC(),
				},
				Recovery: &orchestrator.ShellRecoverySummary{
					ContinuityOutcome: orchestrator.ContinueOutcomeSafe,
					RecoveryClass:     orchestrator.RecoveryClassHandoffLaunchCompleted,
					RecommendedAction: orchestrator.RecoveryActionMonitorLaunchedHandoff,
					ReadyForNextRun:   false,
					Issues: []orchestrator.ShellRecoveryIssue{
						{Code: "HANDOFF_MONITORING", Message: "downstream completion remains unproven"},
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
	if out.LaunchControl == nil || out.LaunchControl.State != string(orchestrator.LaunchControlStateCompleted) {
		t.Fatalf("expected launch control state mapping, got %+v", out.LaunchControl)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughProofOfLife) {
		t.Fatalf("expected handoff continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.HandoffContinuity.FollowThroughKind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("expected follow-through continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.ActiveBranch == nil || out.ActiveBranch.Class != string(orchestrator.ActiveBranchClassHandoffClaude) || out.ActiveBranch.BranchRef != "hnd_1" {
		t.Fatalf("expected shell active branch mapping, got %+v", out.ActiveBranch)
	}
	if out.LocalRunFinalization == nil || out.LocalRunFinalization.State != string(orchestrator.LocalRunFinalizationStaleReconciliationNeeded) {
		t.Fatalf("expected shell local run finalization mapping, got %+v", out.LocalRunFinalization)
	}
	if out.LocalResume == nil || out.LocalResume.State != string(orchestrator.LocalResumeAuthorityBlocked) || out.LocalResume.BlockingBranchRef != "hnd_1" {
		t.Fatalf("expected shell local resume mapping, got %+v", out.LocalResume)
	}
	if out.ActionAuthority == nil || out.ActionAuthority.RequiredNextAction != string(orchestrator.OperatorActionReviewHandoffFollowUp) {
		t.Fatalf("expected shell action authority mapping, got %+v", out.ActionAuthority)
	}
	if out.OperatorExecutionPlan == nil || out.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected shell execution plan mapping, got %+v", out.OperatorExecutionPlan)
	}
	if out.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || out.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_123" {
		t.Fatalf("expected shell execution plan primary step mapping, got %+v", out.OperatorExecutionPlan.PrimaryStep)
	}
	if out.FollowThrough == nil || out.FollowThrough.Kind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("expected shell follow-through payload mapping, got %+v", out.FollowThrough)
	}
	if out.Resolution == nil || out.Resolution.Kind != string(handoff.ResolutionSupersededByLocal) {
		t.Fatalf("expected shell resolution payload mapping, got %+v", out.Resolution)
	}
	if out.Recovery == nil || out.Recovery.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchCompleted) {
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

func TestHandleRequestReviewInterruptedRunRoute(t *testing.T) {
	var captured orchestrator.RecordRecoveryActionRequest
	handler := &fakeOrchestratorService{
		recordRecoveryActionFn: func(_ context.Context, req orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error) {
			captured = req
			return orchestrator.RecordRecoveryActionResult{
				TaskID: common.TaskID(req.TaskID),
				Action: recoveryaction.Record{
					Version:      1,
					ActionID:     "ract_interrupt_1",
					TaskID:       common.TaskID(req.TaskID),
					Kind:         req.Kind,
					RunID:        common.RunID("run_123"),
					CheckpointID: common.CheckpointID("chk_123"),
					Summary:      req.Summary,
					Notes:        append([]string{}, req.Notes...),
					CreatedAt:    time.Unix(1710000001, 0).UTC(),
				},
				RecoveryClass:         orchestrator.RecoveryClassInterruptedRunRecoverable,
				RecommendedAction:     orchestrator.RecoveryActionResumeInterrupted,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "interrupted execution lineage was reviewed and remains recoverable",
				CanonicalResponse:     "interrupted run reviewed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskReviewInterruptedRunRequest{
		TaskID:  common.TaskID("tsk_123"),
		Summary: "interrupted lineage reviewed",
		Notes:   []string{"preserve interrupted lineage"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_review_interrupt",
		Method:    ipc.MethodReviewInterruptedRun,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" || captured.Kind != recoveryaction.KindInterruptedRunReviewed {
		t.Fatalf("unexpected captured interrupted review request: %+v", captured)
	}
	if len(captured.Notes) != 1 || captured.Notes[0] != "preserve interrupted lineage" {
		t.Fatalf("unexpected captured interrupted review notes: %+v", captured.Notes)
	}
	var out ipc.TaskRecordRecoveryActionResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Action.Kind != string(recoveryaction.KindInterruptedRunReviewed) {
		t.Fatalf("expected interrupted review action kind, got %s", out.Action.Kind)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted review response must not claim next-run readiness")
	}
}

func TestHandleRequestExecuteRebriefRoute(t *testing.T) {
	var captured orchestrator.ExecuteRebriefRequest
	handler := &fakeOrchestratorService{
		executeRebriefFn: func(_ context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
			captured = req
			return orchestrator.ExecuteRebriefResult{
				TaskID:                common.TaskID(req.TaskID),
				PreviousBriefID:       common.BriefID("brf_old"),
				BriefID:               common.BriefID("brf_new"),
				BriefHash:             "hash_new",
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "execution brief was regenerated after operator decision",
				CanonicalResponse:     "rebrief executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskRebriefRequest{TaskID: common.TaskID("tsk_456")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_rebrief_1",
		Method:    ipc.MethodExecuteRebrief,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_456" {
		t.Fatalf("unexpected rebrief request: %+v", captured)
	}
	var out ipc.TaskRebriefResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal rebrief response: %v", err)
	}
	if out.BriefID != "brf_new" || !out.ReadyForNextRun {
		t.Fatalf("unexpected rebrief response: %+v", out)
	}
}

func TestHandleRequestExecuteInterruptedResumeRoute(t *testing.T) {
	var captured orchestrator.ExecuteInterruptedResumeRequest
	handler := &fakeOrchestratorService{
		executeInterruptedResumeFn: func(_ context.Context, req orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error) {
			captured = req
			return orchestrator.ExecuteInterruptedResumeResult{
				TaskID:                common.TaskID(req.TaskID),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                recoveryaction.Record{ActionID: "ract_resume_interrupt_1", TaskID: common.TaskID(req.TaskID), Kind: recoveryaction.KindInterruptedResumeExecuted, Summary: "operator resumed interrupted lineage"},
				RecoveryClass:         orchestrator.RecoveryClassContinueExecutionRequired,
				RecommendedAction:     orchestrator.RecoveryActionExecuteContinueRecovery,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run",
				CanonicalResponse:     "interrupted resume executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskInterruptedResumeRequest{
		TaskID:  common.TaskID("tsk_interrupt_resume"),
		Summary: "operator resumed interrupted lineage",
		Notes:   []string{"maintain interrupted lineage semantics"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_interrupt_resume_1",
		Method:    ipc.MethodExecuteInterruptedResume,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_interrupt_resume" || captured.Summary != "operator resumed interrupted lineage" {
		t.Fatalf("unexpected interrupted resume request: %+v", captured)
	}
	if len(captured.Notes) != 1 || captured.Notes[0] != "maintain interrupted lineage semantics" {
		t.Fatalf("unexpected interrupted resume notes: %+v", captured.Notes)
	}
	var out ipc.TaskInterruptedResumeResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal interrupted resume response: %v", err)
	}
	if out.BriefID != "brf_current" || out.Action.Kind != string(recoveryaction.KindInterruptedResumeExecuted) {
		t.Fatalf("unexpected interrupted resume response: %+v", out)
	}
	if out.ReadyForNextRun {
		t.Fatal("interrupted resume response must not claim fresh next-run readiness")
	}
}

func TestHandleRequestExecuteContinueRecoveryRoute(t *testing.T) {
	var captured orchestrator.ExecuteContinueRecoveryRequest
	handler := &fakeOrchestratorService{
		executeContinueRecoveryFn: func(_ context.Context, req orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error) {
			captured = req
			return orchestrator.ExecuteContinueRecoveryResult{
				TaskID:                common.TaskID(req.TaskID),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                recoveryaction.Record{ActionID: "ract_continue_1", TaskID: common.TaskID(req.TaskID), Kind: recoveryaction.KindContinueExecuted, Summary: "operator confirmed current brief"},
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
				CanonicalResponse:     "continue recovery executed",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskContinueRecoveryRequest{TaskID: common.TaskID("tsk_789")})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_continue_recovery_1",
		Method:    ipc.MethodExecuteContinueRecovery,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_789" {
		t.Fatalf("unexpected continue recovery request: %+v", captured)
	}
	var out ipc.TaskContinueRecoveryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal continue recovery response: %v", err)
	}
	if out.BriefID != "brf_current" || out.Action.Kind != string(recoveryaction.KindContinueExecuted) || !out.ReadyForNextRun {
		t.Fatalf("unexpected continue recovery response: %+v", out)
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
				TaskID:                      common.TaskID("tsk_status"),
				Phase:                       phase.PhaseBlocked,
				LatestCheckpointTrigger:     checkpoint.TriggerManual,
				HandoffContinuityState:      orchestrator.HandoffContinuityStateFollowThroughStalled,
				HandoffContinuityReason:     "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
				HandoffContinuationProven:   false,
				LatestAcknowledgmentID:      "hak_1",
				LatestAcknowledgmentStatus:  handoff.AcknowledgmentCaptured,
				LatestAcknowledgmentSummary: "Claude acknowledged the handoff packet.",
				LatestFollowThroughID:       "hft_status",
				LatestFollowThroughKind:     handoff.FollowThroughStalledReviewRequired,
				LatestFollowThroughSummary:  "Claude follow-through looks stalled",
				LatestResolutionID:          "hrs_status",
				LatestResolutionKind:        handoff.ResolutionReviewedStale,
				LatestResolutionSummary:     "reviewed stale after operator follow-up",
				LatestResolutionAt:          time.Unix(1710000800, 0).UTC(),
				ActiveBranchClass:           orchestrator.ActiveBranchClassHandoffClaude,
				ActiveBranchRef:             "hnd_status",
				ActiveBranchAnchorKind:      orchestrator.ActiveBranchAnchorKindHandoff,
				ActiveBranchAnchorRef:       "hnd_status",
				ActiveBranchReason:          "accepted Claude handoff branch currently owns continuity",
				LocalRunFinalizationState:   orchestrator.LocalRunFinalizationStaleReconciliationNeeded,
				LocalRunFinalizationRunID:   common.RunID("run_123"),
				LocalRunFinalizationStatus:  run.StatusRunning,
				LocalRunFinalizationReason:  "latest run is still durably RUNNING and requires explicit stale reconciliation",
				LocalResumeAuthorityState:   orchestrator.LocalResumeAuthorityBlocked,
				LocalResumeMode:             orchestrator.LocalResumeModeNone,
				LocalResumeReason:           "local interrupted-lineage resume is blocked while Claude handoff branch hnd_status owns continuity",
				RequiredNextOperatorAction:  orchestrator.OperatorActionReviewHandoffFollowUp,
				ActionAuthority: []orchestrator.OperatorActionAuthority{
					{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_status", Reason: "Cannot send a local task message while launched Claude handoff hnd_status remains the active continuity branch."},
					{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launch completed and acknowledgment captured, but downstream follow-through appears stalled"},
				},
				OperatorExecutionPlan: &orchestrator.OperatorExecutionPlan{
					PrimaryStep: &orchestrator.OperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_status",
						Reason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
					},
					MandatoryBeforeProgress: true,
				},
				RecoveryClass:        orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
				RecommendedAction:    orchestrator.RecoveryActionReviewHandoffFollowThrough,
				LatestRecoveryAction: action,
			}, nil
		},
		inspectFn: func(_ context.Context, _ string) (orchestrator.InspectTaskResult, error) {
			return orchestrator.InspectTaskResult{
				TaskID:                common.TaskID("tsk_status"),
				LatestRecoveryAction:  action,
				RecentRecoveryActions: []recoveryaction.Record{*action},
				Recovery: &orchestrator.RecoveryAssessment{
					TaskID:            common.TaskID("tsk_status"),
					RecoveryClass:     orchestrator.RecoveryClassHandoffFollowThroughReviewRequired,
					RecommendedAction: orchestrator.RecoveryActionReviewHandoffFollowThrough,
					LatestAction:      action,
				},
				HandoffContinuity: &orchestrator.HandoffContinuity{
					TaskID:                       common.TaskID("tsk_status"),
					HandoffID:                    "hnd_status",
					State:                        orchestrator.HandoffContinuityStateFollowThroughStalled,
					AcknowledgmentID:             "hak_1",
					AcknowledgmentStatus:         handoff.AcknowledgmentCaptured,
					AcknowledgmentSummary:        "Claude acknowledged the handoff packet.",
					FollowThroughID:              "hft_status",
					FollowThroughKind:            handoff.FollowThroughStalledReviewRequired,
					FollowThroughSummary:         "Claude follow-through looks stalled",
					ResolutionID:                 "hrs_status",
					ResolutionKind:               handoff.ResolutionReviewedStale,
					ResolutionSummary:            "reviewed stale after operator follow-up",
					DownstreamContinuationProven: false,
					Reason:                       "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
				},
				FollowThrough: &handoff.FollowThrough{
					Version:         1,
					RecordID:        "hft_status",
					HandoffID:       "hnd_status",
					LaunchAttemptID: "hlc_status",
					LaunchID:        "launch_status",
					TaskID:          common.TaskID("tsk_status"),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            handoff.FollowThroughStalledReviewRequired,
					Summary:         "Claude follow-through looks stalled",
					CreatedAt:       time.Unix(1710000300, 0).UTC(),
				},
				Resolution: &handoff.Resolution{
					Version:         1,
					ResolutionID:    "hrs_status",
					HandoffID:       "hnd_status",
					LaunchAttemptID: "hlc_status",
					LaunchID:        "launch_status",
					TaskID:          common.TaskID("tsk_status"),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            handoff.ResolutionReviewedStale,
					Summary:         "reviewed stale after operator follow-up",
					CreatedAt:       time.Unix(1710000800, 0).UTC(),
				},
				ActiveBranch: &orchestrator.ActiveBranchProvenance{
					TaskID:                 common.TaskID("tsk_status"),
					Class:                  orchestrator.ActiveBranchClassHandoffClaude,
					BranchRef:              "hnd_status",
					ActionabilityAnchor:    orchestrator.ActiveBranchAnchorKindHandoff,
					ActionabilityAnchorRef: "hnd_status",
					Reason:                 "accepted Claude handoff branch currently owns continuity",
				},
				LocalRunFinalization: &orchestrator.LocalRunFinalization{
					TaskID:    common.TaskID("tsk_status"),
					State:     orchestrator.LocalRunFinalizationStaleReconciliationNeeded,
					RunID:     common.RunID("run_123"),
					RunStatus: run.StatusRunning,
					Reason:    "latest run is still durably RUNNING and requires explicit stale reconciliation",
				},
				LocalResumeAuthority: &orchestrator.LocalResumeAuthority{
					TaskID:              common.TaskID("tsk_status"),
					State:               orchestrator.LocalResumeAuthorityBlocked,
					Mode:                orchestrator.LocalResumeModeNone,
					BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude,
					BlockingBranchRef:   "hnd_status",
					Reason:              "local interrupted-lineage resume is blocked while Claude handoff branch hnd_status owns continuity",
				},
				ActionAuthority: &orchestrator.OperatorActionAuthoritySet{
					RequiredNextAction: orchestrator.OperatorActionReviewHandoffFollowUp,
					Actions: []orchestrator.OperatorActionAuthority{
						{Action: orchestrator.OperatorActionLocalMessageMutation, State: orchestrator.OperatorActionAuthorityBlocked, BlockingBranchClass: orchestrator.ActiveBranchClassHandoffClaude, BlockingBranchRef: "hnd_status", Reason: "Cannot send a local task message while launched Claude handoff hnd_status remains the active continuity branch."},
						{Action: orchestrator.OperatorActionReviewHandoffFollowUp, State: orchestrator.OperatorActionAuthorityRequiredNext, Reason: "launch completed and acknowledgment captured, but downstream follow-through appears stalled"},
					},
				},
				OperatorExecutionPlan: &orchestrator.OperatorExecutionPlan{
					PrimaryStep: &orchestrator.OperatorExecutionStep{
						Action:      orchestrator.OperatorActionReviewHandoffFollowUp,
						Status:      orchestrator.OperatorActionAuthorityRequiredNext,
						Domain:      orchestrator.OperatorExecutionDomainReview,
						CommandHint: "tuku inspect --task tsk_status",
						Reason:      "launch completed and acknowledgment captured, but downstream follow-through appears stalled",
					},
					MandatoryBeforeProgress: true,
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
	if statusOut.HandoffContinuityState != string(orchestrator.HandoffContinuityStateFollowThroughStalled) {
		t.Fatalf("expected handoff continuity state in status response, got %+v", statusOut)
	}
	if statusOut.LatestAcknowledgmentStatus != string(handoff.AcknowledgmentCaptured) {
		t.Fatalf("expected acknowledgment status in status response, got %+v", statusOut)
	}
	if statusOut.LatestFollowThroughKind != string(handoff.FollowThroughStalledReviewRequired) {
		t.Fatalf("expected follow-through status mapping, got %+v", statusOut)
	}
	if statusOut.LatestResolutionKind != string(handoff.ResolutionReviewedStale) {
		t.Fatalf("expected resolution status mapping, got %+v", statusOut)
	}
	if statusOut.ActiveBranchClass != string(orchestrator.ActiveBranchClassHandoffClaude) || statusOut.ActiveBranchRef != "hnd_status" {
		t.Fatalf("expected active branch in status response, got %+v", statusOut)
	}
	if statusOut.LocalRunFinalizationState != string(orchestrator.LocalRunFinalizationStaleReconciliationNeeded) || statusOut.LocalRunFinalizationRunID != "run_123" {
		t.Fatalf("expected local run finalization in status response, got %+v", statusOut)
	}
	if statusOut.LocalResumeAuthorityState != string(orchestrator.LocalResumeAuthorityBlocked) || statusOut.LocalResumeReason == "" {
		t.Fatalf("expected local resume authority in status response, got %+v", statusOut)
	}
	if statusOut.RequiredNextOperatorAction != string(orchestrator.OperatorActionReviewHandoffFollowUp) || len(statusOut.ActionAuthority) != 2 {
		t.Fatalf("expected status action authority mapping, got %+v", statusOut)
	}
	if statusOut.OperatorExecutionPlan == nil || statusOut.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected status execution plan mapping, got %+v", statusOut.OperatorExecutionPlan)
	}
	if statusOut.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || statusOut.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_status" {
		t.Fatalf("expected status execution plan primary step mapping, got %+v", statusOut.OperatorExecutionPlan.PrimaryStep)
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
	if inspectOut.HandoffContinuity == nil || inspectOut.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughStalled) {
		t.Fatalf("expected handoff continuity in inspect response, got %+v", inspectOut.HandoffContinuity)
	}
	if inspectOut.FollowThrough == nil || inspectOut.FollowThrough.Kind != handoff.FollowThroughStalledReviewRequired {
		t.Fatalf("expected inspect follow-through mapping, got %+v", inspectOut.FollowThrough)
	}
	if inspectOut.Resolution == nil || inspectOut.Resolution.Kind != handoff.ResolutionReviewedStale {
		t.Fatalf("expected inspect resolution mapping, got %+v", inspectOut.Resolution)
	}
	if inspectOut.ActiveBranch == nil || inspectOut.ActiveBranch.Class != string(orchestrator.ActiveBranchClassHandoffClaude) || inspectOut.ActiveBranch.BranchRef != "hnd_status" {
		t.Fatalf("expected active branch in inspect response, got %+v", inspectOut.ActiveBranch)
	}
	if inspectOut.LocalRunFinalization == nil || inspectOut.LocalRunFinalization.State != string(orchestrator.LocalRunFinalizationStaleReconciliationNeeded) {
		t.Fatalf("expected inspect local run finalization, got %+v", inspectOut.LocalRunFinalization)
	}
	if inspectOut.LocalResumeAuthority == nil || inspectOut.LocalResumeAuthority.State != string(orchestrator.LocalResumeAuthorityBlocked) {
		t.Fatalf("expected inspect local resume authority, got %+v", inspectOut.LocalResumeAuthority)
	}
	if inspectOut.ActionAuthority == nil || inspectOut.ActionAuthority.RequiredNextAction != string(orchestrator.OperatorActionReviewHandoffFollowUp) {
		t.Fatalf("expected inspect action authority mapping, got %+v", inspectOut.ActionAuthority)
	}
	if inspectOut.OperatorExecutionPlan == nil || inspectOut.OperatorExecutionPlan.PrimaryStep == nil {
		t.Fatalf("expected inspect execution plan mapping, got %+v", inspectOut.OperatorExecutionPlan)
	}
	if inspectOut.OperatorExecutionPlan.PrimaryStep.Action != string(orchestrator.OperatorActionReviewHandoffFollowUp) || inspectOut.OperatorExecutionPlan.PrimaryStep.CommandHint != "tuku inspect --task tsk_status" {
		t.Fatalf("expected inspect execution plan primary step mapping, got %+v", inspectOut.OperatorExecutionPlan.PrimaryStep)
	}
}

func TestHandleRequestRecordHandoffFollowThroughRoute(t *testing.T) {
	var captured orchestrator.RecordHandoffFollowThroughRequest
	handler := &fakeOrchestratorService{
		recordHandoffFollowThroughFn: func(_ context.Context, req orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error) {
			captured = req
			return orchestrator.RecordHandoffFollowThroughResult{
				TaskID: common.TaskID(req.TaskID),
				Record: handoff.FollowThrough{
					Version:         1,
					RecordID:        "hft_1",
					HandoffID:       "hnd_1",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					TaskID:          common.TaskID(req.TaskID),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            req.Kind,
					Summary:         req.Summary,
					CreatedAt:       time.Unix(1710000200, 0).UTC(),
				},
				HandoffContinuity: orchestrator.HandoffContinuity{
					TaskID:                       common.TaskID(req.TaskID),
					HandoffID:                    "hnd_1",
					State:                        orchestrator.HandoffContinuityStateFollowThroughProofOfLife,
					LaunchAttemptID:              "hlc_1",
					LaunchID:                     "launch_1",
					FollowThroughID:              "hft_1",
					FollowThroughKind:            req.Kind,
					FollowThroughSummary:         req.Summary,
					DownstreamContinuationProven: true,
					Reason:                       "downstream proof of life observed",
				},
				RecoveryClass:         orchestrator.RecoveryClassHandoffLaunchCompleted,
				RecommendedAction:     orchestrator.RecoveryActionMonitorLaunchedHandoff,
				ReadyForNextRun:       false,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "launched handoff remains monitor-only",
				CanonicalResponse:     "follow-through recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordRequest{
		TaskID:  common.TaskID("tsk_follow"),
		Kind:    string(handoff.FollowThroughProofOfLifeObserved),
		Summary: "later Claude proof of life observed",
		Notes:   []string{"operator confirmed downstream ping"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_followthrough",
		Method:    ipc.MethodRecordHandoffFollowThrough,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp.Error)
	}
	if captured.TaskID != "tsk_follow" || captured.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("unexpected captured follow-through request: %+v", captured)
	}
	var out ipc.TaskHandoffFollowThroughRecordResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal follow-through response: %v", err)
	}
	if out.Record == nil || out.Record.Kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected recorded follow-through payload, got %+v", out.Record)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateFollowThroughProofOfLife) {
		t.Fatalf("expected follow-through continuity mapping, got %+v", out.HandoffContinuity)
	}
	if out.RecoveryClass != string(orchestrator.RecoveryClassHandoffLaunchCompleted) {
		t.Fatalf("expected monitor recovery mapping, got %+v", out)
	}
}

func TestHandleRequestRecordHandoffResolutionRoute(t *testing.T) {
	var captured orchestrator.RecordHandoffResolutionRequest
	handler := &fakeOrchestratorService{
		recordHandoffResolutionFn: func(_ context.Context, req orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error) {
			captured = req
			return orchestrator.RecordHandoffResolutionResult{
				TaskID: common.TaskID(req.TaskID),
				Record: handoff.Resolution{
					Version:         1,
					ResolutionID:    "hrs_1",
					HandoffID:       "hnd_1",
					LaunchAttemptID: "hlc_1",
					LaunchID:        "launch_1",
					TaskID:          common.TaskID(req.TaskID),
					TargetWorker:    run.WorkerKindClaude,
					Kind:            req.Kind,
					Summary:         req.Summary,
					CreatedAt:       time.Unix(1710000500, 0).UTC(),
				},
				HandoffContinuity: orchestrator.HandoffContinuity{
					TaskID:            common.TaskID(req.TaskID),
					HandoffID:         "hnd_1",
					State:             orchestrator.HandoffContinuityStateResolved,
					LaunchAttemptID:   "hlc_1",
					LaunchID:          "launch_1",
					ResolutionID:      "hrs_1",
					ResolutionKind:    req.Kind,
					ResolutionSummary: req.Summary,
					Reason:            "Claude handoff branch was explicitly resolved without claiming downstream completion",
				},
				RecoveryClass:         orchestrator.RecoveryClassReadyNextRun,
				RecommendedAction:     orchestrator.RecoveryActionStartNextRun,
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "resolved handoff no longer blocks local mutation",
				CanonicalResponse:     "handoff resolution recorded",
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)

	payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordRequest{
		TaskID:  common.TaskID("tsk_resolve"),
		Kind:    string(handoff.ResolutionSupersededByLocal),
		Summary: "operator returned local control",
		Notes:   []string{"close Claude branch"},
	})
	resp := svc.handleRequest(context.Background(), ipc.Request{
		RequestID: "req_resolve",
		Method:    ipc.MethodRecordHandoffResolution,
		Payload:   payload,
	})
	if !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp.Error)
	}
	if captured.TaskID != "tsk_resolve" || captured.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("unexpected captured handoff resolution request: %+v", captured)
	}
	var out ipc.TaskHandoffResolutionRecordResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal handoff resolution response: %v", err)
	}
	if out.Record == nil || out.Record.Kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected recorded resolution payload, got %+v", out.Record)
	}
	if out.HandoffContinuity == nil || out.HandoffContinuity.State != string(orchestrator.HandoffContinuityStateResolved) {
		t.Fatalf("expected resolved handoff continuity mapping, got %+v", out.HandoffContinuity)
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
	resolveShellTaskForRepoFn    func(context.Context, string, string) (orchestrator.ResolveShellTaskResult, error)
	createHandoffFn              func(context.Context, orchestrator.CreateHandoffRequest) (orchestrator.CreateHandoffResult, error)
	acceptHandoffFn              func(context.Context, orchestrator.AcceptHandoffRequest) (orchestrator.AcceptHandoffResult, error)
	recordHandoffFollowThroughFn func(context.Context, orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error)
	recordHandoffResolutionFn    func(context.Context, orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error)
	recordRecoveryActionFn       func(context.Context, orchestrator.RecordRecoveryActionRequest) (orchestrator.RecordRecoveryActionResult, error)
	executeRebriefFn             func(context.Context, orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error)
	executeInterruptedResumeFn   func(context.Context, orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error)
	executeContinueRecoveryFn    func(context.Context, orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error)
	executePrimaryOperatorStepFn func(context.Context, orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error)
	statusFn                     func(context.Context, string) (orchestrator.StatusTaskResult, error)
	inspectFn                    func(context.Context, string) (orchestrator.InspectTaskResult, error)
	shellSnapshotFn              func(context.Context, string) (orchestrator.ShellSnapshotResult, error)
	recordShellLifecycleFn       func(context.Context, orchestrator.RecordShellLifecycleRequest) (orchestrator.RecordShellLifecycleResult, error)
	reportShellSessionFn         func(context.Context, orchestrator.ReportShellSessionRequest) (orchestrator.ReportShellSessionResult, error)
	listShellSessionsFn          func(context.Context, string) (orchestrator.ListShellSessionsResult, error)
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

func (f *fakeOrchestratorService) ExecuteRebrief(ctx context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
	if f.executeRebriefFn != nil {
		return f.executeRebriefFn(ctx, req)
	}
	return orchestrator.ExecuteRebriefResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteInterruptedResume(ctx context.Context, req orchestrator.ExecuteInterruptedResumeRequest) (orchestrator.ExecuteInterruptedResumeResult, error) {
	if f.executeInterruptedResumeFn != nil {
		return f.executeInterruptedResumeFn(ctx, req)
	}
	return orchestrator.ExecuteInterruptedResumeResult{}, nil
}

func (f *fakeOrchestratorService) ExecuteContinueRecovery(ctx context.Context, req orchestrator.ExecuteContinueRecoveryRequest) (orchestrator.ExecuteContinueRecoveryResult, error) {
	if f.executeContinueRecoveryFn != nil {
		return f.executeContinueRecoveryFn(ctx, req)
	}
	return orchestrator.ExecuteContinueRecoveryResult{}, nil
}

func (f *fakeOrchestratorService) ExecutePrimaryOperatorStep(ctx context.Context, req orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error) {
	if f.executePrimaryOperatorStepFn != nil {
		return f.executePrimaryOperatorStepFn(ctx, req)
	}
	return orchestrator.ExecutePrimaryOperatorStepResult{}, nil
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

func (f *fakeOrchestratorService) RecordHandoffFollowThrough(ctx context.Context, req orchestrator.RecordHandoffFollowThroughRequest) (orchestrator.RecordHandoffFollowThroughResult, error) {
	if f.recordHandoffFollowThroughFn != nil {
		return f.recordHandoffFollowThroughFn(ctx, req)
	}
	return orchestrator.RecordHandoffFollowThroughResult{}, nil
}

func (f *fakeOrchestratorService) RecordHandoffResolution(ctx context.Context, req orchestrator.RecordHandoffResolutionRequest) (orchestrator.RecordHandoffResolutionResult, error) {
	if f.recordHandoffResolutionFn != nil {
		return f.recordHandoffResolutionFn(ctx, req)
	}
	return orchestrator.RecordHandoffResolutionResult{}, nil
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

func TestHandleRequestExecutePrimaryOperatorStepRoute(t *testing.T) {
	var captured orchestrator.ExecutePrimaryOperatorStepRequest
	handler := &fakeOrchestratorService{
		executePrimaryOperatorStepFn: func(_ context.Context, req orchestrator.ExecutePrimaryOperatorStepRequest) (orchestrator.ExecutePrimaryOperatorStepResult, error) {
			captured = req
			return orchestrator.ExecutePrimaryOperatorStepResult{
				TaskID: common.TaskID(req.TaskID),
				Receipt: operatorstep.Receipt{
					ReceiptID:    "orec_123",
					TaskID:       common.TaskID(req.TaskID),
					ActionHandle: string(orchestrator.OperatorActionLaunchAcceptedHandoff),
					ResultClass:  operatorstep.ResultSucceeded,
					Summary:      "launched accepted handoff hnd_123",
					CreatedAt:    time.Unix(1710000000, 0).UTC(),
				},
				ActiveBranch:               orchestrator.ActiveBranchProvenance{TaskID: common.TaskID(req.TaskID), Class: orchestrator.ActiveBranchClassHandoffClaude, BranchRef: "hnd_123"},
				OperatorDecision:           orchestrator.OperatorDecisionSummary{Headline: "Active Claude handoff pending", RequiredNextAction: orchestrator.OperatorActionResolveActiveHandoff},
				OperatorExecutionPlan:      orchestrator.OperatorExecutionPlan{PrimaryStep: &orchestrator.OperatorExecutionStep{Action: orchestrator.OperatorActionResolveActiveHandoff, Status: orchestrator.OperatorActionAuthorityAllowed}},
				RecentOperatorStepReceipts: []operatorstep.Receipt{{ReceiptID: "orec_123", TaskID: common.TaskID(req.TaskID), ActionHandle: string(orchestrator.OperatorActionLaunchAcceptedHandoff), ResultClass: operatorstep.ResultSucceeded, Summary: "launched accepted handoff hnd_123", CreatedAt: time.Unix(1710000000, 0).UTC()}},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{RequestID: "req_next", Method: ipc.MethodExecutePrimaryOperatorStep, Payload: payload})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	if captured.TaskID != "tsk_123" {
		t.Fatalf("unexpected execute-primary request: %+v", captured)
	}
	var out ipc.TaskExecutePrimaryOperatorStepResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.Receipt.ReceiptID != "orec_123" || out.Receipt.ActionHandle != string(orchestrator.OperatorActionLaunchAcceptedHandoff) {
		t.Fatalf("unexpected operator-next response: %+v", out)
	}
	if len(out.RecentOperatorStepReceipts) != 1 || out.RecentOperatorStepReceipts[0].ReceiptID != "orec_123" {
		t.Fatalf("expected recent receipt history in response, got %+v", out.RecentOperatorStepReceipts)
	}
}

func TestShellSnapshotResponseIncludesOperatorReceiptHistory(t *testing.T) {
	handler := &fakeOrchestratorService{
		shellSnapshotFn: func(_ context.Context, _ string) (orchestrator.ShellSnapshotResult, error) {
			return orchestrator.ShellSnapshotResult{
				TaskID:                    "tsk_123",
				Goal:                      "test",
				Phase:                     "BRIEF_READY",
				Status:                    "ACTIVE",
				LatestOperatorStepReceipt: &operatorstep.Receipt{ReceiptID: "orec_latest", TaskID: "tsk_123", ActionHandle: string(orchestrator.OperatorActionFinalizeContinueRecovery), ResultClass: operatorstep.ResultSucceeded, Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
				RecentOperatorStepReceipts: []operatorstep.Receipt{
					{ReceiptID: "orec_latest", TaskID: "tsk_123", ActionHandle: string(orchestrator.OperatorActionFinalizeContinueRecovery), ResultClass: operatorstep.ResultSucceeded, Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
					{ReceiptID: "orec_prev", TaskID: "tsk_123", ActionHandle: string(orchestrator.OperatorActionResumeInterruptedLineage), ResultClass: operatorstep.ResultSucceeded, Summary: "resumed interrupted lineage", CreatedAt: time.Unix(1710000000, 0).UTC()},
				},
			}, nil
		},
	}
	svc := NewService("/tmp/unused.sock", handler)
	payload, _ := json.Marshal(ipc.TaskShellSnapshotRequest{TaskID: common.TaskID("tsk_123")})
	resp := svc.handleRequest(context.Background(), ipc.Request{RequestID: "req_shell", Method: ipc.MethodTaskShellSnapshot, Payload: payload})
	if !resp.OK {
		t.Fatalf("expected OK response, got error: %+v", resp.Error)
	}
	var out ipc.TaskShellSnapshotResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if out.LatestOperatorStepReceipt == nil || out.LatestOperatorStepReceipt.ReceiptID != "orec_latest" {
		t.Fatalf("expected latest operator receipt in shell snapshot, got %+v", out.LatestOperatorStepReceipt)
	}
	if len(out.RecentOperatorStepReceipts) != 2 || out.RecentOperatorStepReceipts[1].ReceiptID != "orec_prev" {
		t.Fatalf("expected recent shell receipt history, got %+v", out.RecentOperatorStepReceipts)
	}
}
```

## internal/app/bootstrap_test.go
```go
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/ipc"
	tukushell "tuku/internal/tui/shell"
)

func TestParseShellWorkerPreference(t *testing.T) {
	preference, err := parseShellWorkerPreference("claude")
	if err != nil {
		t.Fatalf("parse claude worker preference: %v", err)
	}
	if preference != "claude" {
		t.Fatalf("expected claude preference, got %q", preference)
	}
}

func TestParseShellWorkerPreferenceRejectsInvalidWorker(t *testing.T) {
	if _, err := parseShellWorkerPreference("invalid-worker"); err == nil {
		t.Fatal("expected invalid worker error")
	}
}

func TestCLIUsageMentionsChat(t *testing.T) {
	if !strings.Contains(cliUsage(), "chat") {
		t.Fatalf("expected cli usage to mention chat, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsRecovery(t *testing.T) {
	if !strings.Contains(cliUsage(), "recovery") {
		t.Fatalf("expected cli usage to mention recovery, got %q", cliUsage())
	}
}

func TestParseRecoveryActionKind(t *testing.T) {
	kind, err := parseRecoveryActionKind("decision-regenerate-brief")
	if err != nil {
		t.Fatalf("parse recovery action kind: %v", err)
	}
	if kind != "DECISION_REGENERATE_BRIEF" {
		t.Fatalf("expected DECISION_REGENERATE_BRIEF, got %s", kind)
	}
}

func TestParseHandoffFollowThroughKind(t *testing.T) {
	kind, err := parseHandoffFollowThroughKind("proof-of-life-observed")
	if err != nil {
		t.Fatalf("parse handoff follow-through kind: %v", err)
	}
	if kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected %s, got %s", handoff.FollowThroughProofOfLifeObserved, kind)
	}
}

func TestParseHandoffResolutionKind(t *testing.T) {
	kind, err := parseHandoffResolutionKind("superseded-by-local")
	if err != nil {
		t.Fatalf("parse handoff resolution kind: %v", err)
	}
	if kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected %s, got %s", handoff.ResolutionSupersededByLocal, kind)
	}
}

func TestCLIRecoveryRecordCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_123"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_123", Kind: "FAILED_RUN_REVIEWED"},
			RecoveryClass:         "DECISION_REQUIRED",
			RecommendedAction:     "MAKE_RESUME_DECISION",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "failed run reviewed; choose next step",
			CanonicalResponse:     "recovery action recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "record",
		"--task", "tsk_123",
		"--action", "failed-run-reviewed",
		"--summary", "reviewed failed run",
		"--note", "operator reviewed logs",
	}); err != nil {
		t.Fatalf("run recovery command: %v", err)
	}
	if captured.Method != ipc.MethodRecordRecoveryAction {
		t.Fatalf("expected recovery record method, got %s", captured.Method)
	}
	var req ipc.TaskRecordRecoveryActionRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal recovery record request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.Kind != "FAILED_RUN_REVIEWED" {
		t.Fatalf("unexpected recovery record request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator reviewed logs" {
		t.Fatalf("unexpected recovery record notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryRecordCommandRejectsUnsupportedAction(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "not-a-real-action"})
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery action") {
		t.Fatalf("expected unsupported recovery action error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported recovery action")
	}
}

func TestCLIRecoveryRecordCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [RECOVERY_ACTION_FAILED]: continue decision can only be recorded while recovery class is DECISION_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "decision-continue"})
	if err == nil || !strings.Contains(err.Error(), "DECISION_REQUIRED") {
		t.Fatalf("expected daemon rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryRebriefCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRebriefResponse{
			TaskID:                common.TaskID("tsk_456"),
			PreviousBriefID:       common.BriefID("brf_old"),
			BriefID:               common.BriefID("brf_new"),
			BriefHash:             "hash_new",
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "execution brief was regenerated after operator decision",
			CanonicalResponse:     "rebrief executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"recovery", "rebrief", "--task", "tsk_456"}); err != nil {
		t.Fatalf("run recovery rebrief command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteRebrief {
		t.Fatalf("expected rebrief method, got %s", captured.Method)
	}
	var req ipc.TaskRebriefRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal rebrief request: %v", err)
	}
	if req.TaskID != "tsk_456" {
		t.Fatalf("unexpected rebrief request: %+v", req)
	}
}

func TestCLIRecoveryRebriefCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [REBRIEF_FAILED]: rebrief can only be executed while recovery class is REBRIEF_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "rebrief", "--task", "tsk_456"})
	if err == nil || !strings.Contains(err.Error(), "REBRIEF_REQUIRED") {
		t.Fatalf("expected daemon rebrief rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryResumeInterruptedCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskInterruptedResumeResponse{
			TaskID:                common.TaskID("tsk_resume_interrupt"),
			BriefID:               common.BriefID("brf_current"),
			BriefHash:             "hash_current",
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_interrupt_resume_1", Kind: string(recoveryaction.KindInterruptedResumeExecuted), Summary: "operator resumed interrupted lineage"},
			RecoveryClass:         "CONTINUE_EXECUTION_REQUIRED",
			RecommendedAction:     "EXECUTE_CONTINUE_RECOVERY",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run",
			CanonicalResponse:     "interrupted resume executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "resume-interrupted",
		"--task", "tsk_resume_interrupt",
		"--summary", "operator resumed interrupted lineage",
		"--note", "maintain interrupted lineage semantics",
	}); err != nil {
		t.Fatalf("run recovery resume-interrupted command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteInterruptedResume {
		t.Fatalf("expected interrupted-resume method, got %s", captured.Method)
	}
	var req ipc.TaskInterruptedResumeRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal interrupted resume request: %v", err)
	}
	if req.TaskID != "tsk_resume_interrupt" || req.Summary != "operator resumed interrupted lineage" {
		t.Fatalf("unexpected interrupted resume request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "maintain interrupted lineage semantics" {
		t.Fatalf("unexpected interrupted resume notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryResumeInterruptedCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [INTERRUPTED_RESUME_FAILED]: interrupted resume can only be executed while recovery class is INTERRUPTED_RUN_RECOVERABLE and recommended action is RESUME_INTERRUPTED_RUN")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "resume-interrupted", "--task", "tsk_resume_interrupt"})
	if err == nil || !strings.Contains(err.Error(), "INTERRUPTED_RUN_RECOVERABLE") {
		t.Fatalf("expected daemon interrupted-resume rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryContinueCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinueRecoveryResponse{
			TaskID:                common.TaskID("tsk_789"),
			BriefID:               common.BriefID("brf_current"),
			BriefHash:             "hash_current",
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_continue_1", TaskID: common.TaskID("tsk_789"), Kind: string(recoveryaction.KindContinueExecuted), Summary: "operator confirmed current brief"},
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
			CanonicalResponse:     "continue recovery executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_789"}); err != nil {
		t.Fatalf("run recovery continue command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteContinueRecovery {
		t.Fatalf("expected continue-recovery method, got %s", captured.Method)
	}
	var req ipc.TaskContinueRecoveryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal continue recovery request: %v", err)
	}
	if req.TaskID != "tsk_789" {
		t.Fatalf("unexpected continue recovery request: %+v", req)
	}
}

func TestCLIHandoffFollowThroughCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordResponse{
			TaskID: common.TaskID("tsk_follow"),
			Record: &handoff.FollowThrough{
				Version:         1,
				RecordID:        "hft_1",
				HandoffID:       "hnd_1",
				LaunchAttemptID: "hlc_1",
				LaunchID:        "launch_1",
				TaskID:          common.TaskID("tsk_follow"),
				TargetWorker:    "claude",
				Kind:            handoff.FollowThroughProofOfLifeObserved,
				Summary:         "later Claude proof of life observed",
				CreatedAt:       time.Unix(1710000200, 0).UTC(),
			},
			RecoveryClass:         "HANDOFF_LAUNCH_COMPLETED",
			RecommendedAction:     "MONITOR_LAUNCHED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "launched handoff remains monitor-only",
			CanonicalResponse:     "follow-through recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"handoff-followthrough",
		"--task", "tsk_follow",
		"--kind", "proof-of-life-observed",
		"--summary", "later Claude proof of life observed",
		"--note", "operator confirmed downstream ping",
	}); err != nil {
		t.Fatalf("run handoff-followthrough command: %v", err)
	}
	if captured.Method != ipc.MethodRecordHandoffFollowThrough {
		t.Fatalf("expected handoff follow-through method, got %s", captured.Method)
	}
	var req ipc.TaskHandoffFollowThroughRecordRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal handoff follow-through request: %v", err)
	}
	if req.TaskID != "tsk_follow" || req.Kind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("unexpected handoff follow-through request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator confirmed downstream ping" {
		t.Fatalf("unexpected handoff follow-through notes: %+v", req.Notes)
	}
}

func TestCLIHandoffFollowThroughCommandRejectsUnsupportedKind(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-followthrough", "--task", "tsk_follow", "--kind", "not-a-real-kind"})
	if err == nil || !strings.Contains(err.Error(), "unsupported handoff follow-through kind") {
		t.Fatalf("expected unsupported follow-through kind error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported handoff follow-through kind")
	}
}

func TestCLIHandoffFollowThroughCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [HANDOFF_FOLLOW_THROUGH_FAILED]: handoff follow-through kind PROOF_OF_LIFE_OBSERVED can only be recorded while handoff continuity state is a launched Claude follow-through posture")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-followthrough", "--task", "tsk_follow", "--kind", "proof-of-life-observed"})
	if err == nil || !strings.Contains(err.Error(), "launched Claude follow-through posture") {
		t.Fatalf("expected daemon follow-through rejection to surface, got %v", err)
	}
}

func TestCLIHandoffResolveCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordResponse{
			TaskID: common.TaskID("tsk_resolve"),
			Record: &handoff.Resolution{
				Version:         1,
				ResolutionID:    "hrs_1",
				HandoffID:       "hnd_1",
				LaunchAttemptID: "hlc_1",
				LaunchID:        "launch_1",
				TaskID:          common.TaskID("tsk_resolve"),
				TargetWorker:    "claude",
				Kind:            handoff.ResolutionSupersededByLocal,
				Summary:         "operator returned local control",
				CreatedAt:       time.Unix(1710000600, 0).UTC(),
			},
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "resolved handoff no longer blocks local mutation",
			CanonicalResponse:     "handoff resolution recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"handoff-resolve",
		"--task", "tsk_resolve",
		"--kind", "superseded-by-local",
		"--summary", "operator returned local control",
		"--note", "close Claude branch",
	}); err != nil {
		t.Fatalf("run handoff-resolve command: %v", err)
	}
	if captured.Method != ipc.MethodRecordHandoffResolution {
		t.Fatalf("expected handoff resolution method, got %s", captured.Method)
	}
	var req ipc.TaskHandoffResolutionRecordRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal handoff resolution request: %v", err)
	}
	if req.TaskID != "tsk_resolve" || req.Kind != string(handoff.ResolutionSupersededByLocal) {
		t.Fatalf("unexpected handoff resolution request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "close Claude branch" {
		t.Fatalf("unexpected handoff resolution notes: %+v", req.Notes)
	}
}

func TestCLIHandoffResolveCommandRejectsUnsupportedKind(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-resolve", "--task", "tsk_resolve", "--kind", "not-a-real-kind"})
	if err == nil || !strings.Contains(err.Error(), "unsupported handoff resolution kind") {
		t.Fatalf("expected unsupported handoff resolution kind error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported handoff resolution kind")
	}
}

func TestCLIHandoffResolveCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [HANDOFF_RESOLUTION_FAILED]: no active Claude handoff branch exists")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-resolve", "--task", "tsk_resolve", "--kind", "abandoned"})
	if err == nil || !strings.Contains(err.Error(), "no active Claude handoff branch") {
		t.Fatalf("expected daemon handoff resolution rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryContinueCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [CONTINUE_RECOVERY_FAILED]: continue recovery can only be executed while recovery class is CONTINUE_EXECUTION_REQUIRED and latest action is DECISION_CONTINUE")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_789"})
	if err == nil || !strings.Contains(err.Error(), "CONTINUE_EXECUTION_REQUIRED") {
		t.Fatalf("expected daemon continue-recovery rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryReviewInterruptedCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_interrupt"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_interrupt_1", Kind: string(recoveryaction.KindInterruptedRunReviewed)},
			RecoveryClass:         "INTERRUPTED_RUN_RECOVERABLE",
			RecommendedAction:     "RESUME_INTERRUPTED_RUN",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "interrupted execution lineage was reviewed and remains recoverable",
			CanonicalResponse:     "interrupted run reviewed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "review-interrupted",
		"--task", "tsk_interrupt",
		"--summary", "interrupted lineage reviewed",
		"--note", "preserve interrupted lineage",
	}); err != nil {
		t.Fatalf("run recovery review-interrupted command: %v", err)
	}
	if captured.Method != ipc.MethodReviewInterruptedRun {
		t.Fatalf("expected review-interrupted method, got %s", captured.Method)
	}
	var req ipc.TaskReviewInterruptedRunRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal review-interrupted request: %v", err)
	}
	if req.TaskID != "tsk_interrupt" || req.Summary != "interrupted lineage reviewed" {
		t.Fatalf("unexpected review-interrupted request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "preserve interrupted lineage" {
		t.Fatalf("unexpected review-interrupted notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryReviewInterruptedCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [INTERRUPTED_REVIEW_FAILED]: interrupted-run review can only be recorded while recovery class is INTERRUPTED_RUN_RECOVERABLE")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "review-interrupted", "--task", "tsk_interrupt"})
	if err == nil || !strings.Contains(err.Error(), "INTERRUPTED_RUN_RECOVERABLE") {
		t.Fatalf("expected daemon interrupted-review rejection to surface, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapStartsDaemonOnUnavailable(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	var calls int
	var launched int
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		calls++
		if calls == 1 || calls == 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		return mustResolveShellTaskResponse(t, "tsk_bootstrap"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		launched++
		ch := make(chan error)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err != nil {
		t.Fatalf("resolve shell task with bootstrap: %v", err)
	}
	if resolution.TaskID != common.TaskID("tsk_bootstrap") {
		t.Fatalf("expected task id tsk_bootstrap, got %s", resolution.TaskID)
	}
	if launched != 1 {
		t.Fatalf("expected daemon to be launched once, got %d", launched)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapDoesNotStartDaemonOnUnexpectedError(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [BAD_PAYLOAD]: broken request")
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be launched for unexpected IPC errors")
		return nil, nil
	}

	if _, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository"); err == nil {
		t.Fatal("expected unexpected IPC error to be returned")
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsStartupFailure(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		return nil, errors.New("launch failed")
	}

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "could not start the local Tuku daemon automatically") {
		t.Fatalf("expected daemon startup failure, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsProcessExit(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error, 1)
		ch <- errors.New("exit status 1")
		close(ch)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "local Tuku daemon failed to start") {
		t.Fatalf("expected daemon process exit failure, got %v", err)
	}
}

func TestRunPrimaryEntryStartsDaemonAndOpensShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		if req.Method != ipc.MethodResolveShellTaskForRepo {
			t.Fatalf("expected resolve shell task request, got %s", req.Method)
		}
		return mustResolveShellTaskResponse(t, "tsk_primary"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	var openedTaskID string
	app := &CLIApplication{
		openShellFn: func(_ context.Context, _ string, taskID string, _ tukushell.WorkerPreference) error {
			openedTaskID = taskID
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry: %v", err)
	}
	if openedTaskID != "tsk_primary" {
		t.Fatalf("expected shell to open task tsk_primary, got %q", openedTaskID)
	}
}

func TestRunPrimaryEntryOutsideRepoOpensFallbackShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/no-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		t.Fatal("daemon IPC should not be used outside repo fallback mode")
		return ipc.Response{}, nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be auto-started outside repo fallback mode")
		return nil, nil
	}

	var fallbackCWD string
	app := &CLIApplication{
		openFallbackShellFn: func(_ context.Context, cwd string, _ tukushell.WorkerPreference) error {
			fallbackCWD = cwd
			return nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, _ tukushell.WorkerPreference) error {
			t.Fatal("task-backed shell should not open outside repo fallback mode")
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry outside repo: %v", err)
	}
	if fallbackCWD != "/tmp/no-repo" {
		t.Fatalf("expected fallback cwd /tmp/no-repo, got %q", fallbackCWD)
	}
}

func TestResolveCurrentRepoRootReturnsPrimaryEntryMessage(t *testing.T) {
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/not-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}

	_, err := resolveCurrentRepoRoot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tuku needs a git repository for the primary entry path") {
		t.Fatalf("expected primary-entry repo error, got %v", err)
	}
}

func TestPrimaryEntryScratchSnapshotExplainsNoRepoMode(t *testing.T) {
	snapshot := primaryEntryScratchSnapshot("/tmp/no-repo")
	if snapshot.Status != "LOCAL_ONLY" || snapshot.Phase != "SCRATCH_INTAKE" {
		t.Fatalf("expected scratch intake snapshot, got %+v", snapshot)
	}
	if snapshot.Repo.RepoRoot != "" {
		t.Fatalf("expected no repo anchor in scratch mode, got %+v", snapshot.Repo)
	}
	if snapshot.IntentClass != "scratch" {
		t.Fatalf("expected scratch intent class, got %q", snapshot.IntentClass)
	}
	if !strings.Contains(snapshot.LatestCanonicalResponse, "local scratch and intake session") {
		t.Fatalf("expected scratch explanation, got %q", snapshot.LatestCanonicalResponse)
	}
	if !strings.Contains(snapshot.IntentSummary, "/tmp/no-repo") {
		t.Fatalf("expected cwd in scratch intent summary, got %q", snapshot.IntentSummary)
	}
	if len(snapshot.RecentConversation) < 3 {
		t.Fatal("expected scratch intake guidance conversation")
	}
}

func TestLoadPrimaryRepoScratchBridgeLoadsExactRepoScratchNotes(t *testing.T) {
	origResolveScratchPath := resolveScratchPath
	defer func() {
		resolveScratchPath = origResolveScratchPath
	}()

	path := filepath.Join(t.TempDir(), "scratch.json")
	resolveScratchPath = func(string) (string, error) {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "kind": "local_scratch_intake",
  "cwd": "/tmp/repo",
  "created_at": "2026-03-19T00:00:00Z",
  "updated_at": "2026-03-19T00:00:00Z",
  "notes": [
    {"role": "user", "body": "Draft the first milestone list", "created_at": "2026-03-19T00:00:00Z"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("write scratch file: %v", err)
	}

	bridge, err := loadPrimaryRepoScratchBridge("/tmp/repo")
	if err != nil {
		t.Fatalf("load primary repo scratch bridge: %v", err)
	}
	if bridge == nil || len(bridge.Notes) != 1 {
		t.Fatalf("expected one bridged scratch note, got %+v", bridge)
	}
	if bridge.Notes[0].Body != "Draft the first milestone list" {
		t.Fatalf("expected bridged note body, got %+v", bridge.Notes[0])
	}
}

func TestApplyPrimaryRepoScratchBridgeAppendsExplicitLocalOnlyMessages(t *testing.T) {
	snapshot := applyPrimaryRepoScratchBridge(tukushell.Snapshot{
		TaskID:                  "tsk_repo",
		Phase:                   "INTAKE",
		Status:                  "ACTIVE",
		LatestCanonicalResponse: "Canonical repo-backed response.",
		RecentConversation: []tukushell.ConversationItem{
			{Role: "system", Body: "Repo-backed task created."},
		},
	}, &primaryRepoScratchBridge{
		RepoRoot: "/tmp/repo",
		Notes: []tukushell.ConversationItem{
			{Role: "user", Body: "Plan project structure"},
			{Role: "user", Body: "List initial requirements"},
		},
	})

	if snapshot.LatestCanonicalResponse != "Canonical repo-backed response." {
		t.Fatalf("expected canonical response to remain unchanged, got %q", snapshot.LatestCanonicalResponse)
	}
	if snapshot.LocalScratch == nil || len(snapshot.LocalScratch.Notes) != 2 {
		t.Fatalf("expected surfaced local scratch context, got %+v", snapshot.LocalScratch)
	}
	all := make([]string, 0, len(snapshot.RecentConversation))
	for _, msg := range snapshot.RecentConversation {
		all = append(all, msg.Body)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "have not been imported into canonical task state") {
		t.Fatalf("expected explicit local-only boundary, got %q", joined)
	}
	if !strings.Contains(joined, "Sending that pending message is the explicit adoption step") {
		t.Fatalf("expected explicit adoption step, got %q", joined)
	}
	if !strings.Contains(joined, "Shell commands: stage local scratch with `a`") {
		t.Fatalf("expected shell-local adoption command copy, got %q", joined)
	}
	if !strings.Contains(joined, "local scratch note: Plan project structure") {
		t.Fatalf("expected bridged scratch note, got %q", joined)
	}
}

func mustResolveShellTaskResponse(t *testing.T, taskID common.TaskID) ipc.Response {
	t.Helper()
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoResponse{
		TaskID:   taskID,
		RepoRoot: "/tmp/repo",
		Created:  false,
	})
	if err != nil {
		t.Fatalf("marshal resolve shell task response: %v", err)
	}
	return ipc.Response{OK: true, Payload: payload}
}

func daemonUnavailableErr() error {
	return &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}
}

type capturedStdout struct {
	previous *os.File
	reader   *os.File
	writer   *os.File
	buffer   bytes.Buffer
}

func captureCLIStdout(t *testing.T) *capturedStdout {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	captured := &capturedStdout{
		previous: os.Stdout,
		reader:   reader,
		writer:   writer,
	}
	os.Stdout = writer
	return captured
}

func (c *capturedStdout) restore() {
	if c == nil {
		return
	}
	if c.previous != nil {
		os.Stdout = c.previous
	}
	if c.writer != nil {
		_ = c.writer.Close()
	}
	if c.reader != nil {
		_, _ = c.buffer.ReadFrom(c.reader)
		_ = c.reader.Close()
	}
}

func TestCLINextCommandRoutesUnifiedPrimaryExecution(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_123"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_123",
				TaskID:       common.TaskID("tsk_123"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_123",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"next", "--task", "tsk_123"}); err != nil {
		t.Fatalf("run next command: %v", err)
	}
	if captured.Method != ipc.MethodExecutePrimaryOperatorStep {
		t.Fatalf("expected unified primary-step method, got %s", captured.Method)
	}
	var req ipc.TaskExecutePrimaryOperatorStepRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal next request: %v", err)
	}
	if req.TaskID != "tsk_123" {
		t.Fatalf("unexpected next request: %+v", req)
	}
}
```

## internal/tui/shell/app_test.go
```go
package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

type stubLifecycleSink struct {
	records []persistedLifecycleRecord
	err     error
}

type persistedLifecycleRecord struct {
	taskID    string
	sessionID string
	kind      PersistedLifecycleKind
	status    HostStatus
}

type stubTaskMessageSender struct {
	sent []sentTaskMessage
	err  error
}

type sentTaskMessage struct {
	taskID  string
	message string
}

type stubPrimaryActionExecutor struct {
	calls     []executedPrimaryAction
	err       error
	outcome   PrimaryActionExecutionOutcome
	startedCh chan struct{}
	releaseCh chan struct{}
}

type executedPrimaryAction struct {
	taskID   string
	snapshot Snapshot
}

type stubSnapshotSource struct {
	snapshot Snapshot
	err      error
	loads    []string
	next     []Snapshot
}

func (s *stubSnapshotSource) Load(taskID string) (Snapshot, error) {
	s.loads = append(s.loads, taskID)
	if s.err != nil {
		return Snapshot{}, s.err
	}
	if len(s.next) > 0 {
		next := s.next[0]
		s.next = s.next[1:]
		s.snapshot = next
		return next, nil
	}
	return s.snapshot, nil
}

func (s *stubLifecycleSink) Record(taskID string, sessionID string, kind PersistedLifecycleKind, status HostStatus) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, persistedLifecycleRecord{
		taskID:    taskID,
		sessionID: sessionID,
		kind:      kind,
		status:    status,
	})
	return nil
}

func (s *stubTaskMessageSender) Send(taskID string, message string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, sentTaskMessage{taskID: taskID, message: message})
	return nil
}

func (s *stubPrimaryActionExecutor) Execute(taskID string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error) {
	if s.startedCh != nil {
		select {
		case s.startedCh <- struct{}{}:
		default:
		}
	}
	if s.releaseCh != nil {
		<-s.releaseCh
	}
	if s.err != nil {
		return PrimaryActionExecutionOutcome{}, s.err
	}
	s.calls = append(s.calls, executedPrimaryAction{taskID: taskID, snapshot: snapshot})
	outcome := s.outcome
	if strings.TrimSpace(outcome.Receipt.ActionHandle) == "" && snapshot.OperatorExecutionPlan != nil && snapshot.OperatorExecutionPlan.PrimaryStep != nil {
		outcome.Receipt.ActionHandle = snapshot.OperatorExecutionPlan.PrimaryStep.Action
	}
	if strings.TrimSpace(outcome.Receipt.ResultClass) == "" {
		outcome.Receipt.ResultClass = "SUCCEEDED"
	}
	if strings.TrimSpace(outcome.Receipt.Summary) == "" && snapshot.OperatorExecutionPlan != nil && snapshot.OperatorExecutionPlan.PrimaryStep != nil {
		outcome.Receipt.Summary = "executed " + strings.ToLower(snapshot.OperatorExecutionPlan.PrimaryStep.Action)
	}
	if outcome.Receipt.CreatedAt.IsZero() {
		outcome.Receipt.CreatedAt = time.Unix(1710000000, 0).UTC()
	}
	return outcome, nil
}

func TestHandleKeyTogglesShellUI(t *testing.T) {
	ui := UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
	}

	if action := handleShellKey(&ui, 'i'); action != actionNone {
		t.Fatalf("expected no action on inspector toggle, got %v", action)
	}
	if ui.ShowInspector {
		t.Fatal("expected inspector hidden")
	}

	if action := handleShellKey(&ui, 'p'); action != actionNone {
		t.Fatalf("expected no action on proof toggle, got %v", action)
	}
	if ui.ShowProof {
		t.Fatal("expected proof hidden")
	}

	if action := handleShellKey(&ui, 'h'); action != actionNone || !ui.ShowHelp {
		t.Fatal("expected help overlay enabled")
	}
	if action := handleShellKey(&ui, 's'); action != actionNone || !ui.ShowStatus || ui.ShowHelp {
		t.Fatal("expected status overlay enabled and help cleared")
	}
	if action := handleShellKey(&ui, 'r'); action != actionRefresh {
		t.Fatalf("expected refresh action, got %v", action)
	}
	if action := handleShellKey(&ui, 'n'); action != actionExecutePrimaryOperatorStep {
		t.Fatalf("expected execute-primary action, got %v", action)
	}
	if action := handleShellKey(&ui, 'q'); action != actionQuit {
		t.Fatalf("expected quit action, got %v", action)
	}
}

func TestExecutePrimaryOperatorStepRefreshesSnapshotAfterSuccess(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	executor := &stubPrimaryActionExecutor{}
	source := &stubSnapshotSource{
		next: []Snapshot{{
			TaskID: "tsk_1",
			Phase:  "BRIEF_READY",
			OperatorExecutionPlan: &OperatorExecutionPlan{
				PrimaryStep: &OperatorExecutionStep{
					Action:         "START_LOCAL_RUN",
					CommandSurface: "DEDICATED",
					CommandHint:    "tuku run --task tsk_1 --action start",
				},
			},
		}},
	}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Session: newSessionState(now)}

	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].taskID != "tsk_1" {
		t.Fatalf("expected one primary-action execution, got %#v", executor.calls)
	}
	if len(source.loads) != 1 || source.loads[0] != "tsk_1" {
		t.Fatalf("expected one refresh load, got %#v", source.loads)
	}
	if snapshot.Phase != "BRIEF_READY" {
		t.Fatalf("expected snapshot to refresh after action, got %+v", snapshot)
	}
	if host.snapshotSeen.Phase != "BRIEF_READY" {
		t.Fatalf("expected host snapshot to refresh after action, got %+v", host.snapshotSeen)
	}
	if len(ui.Session.Journal) == 0 || ui.Session.Journal[len(ui.Session.Journal)-1].Type != SessionEventPrimaryOperatorActionExecuted {
		t.Fatalf("expected primary-action session event, got %#v", ui.Session.Journal)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "SUCCESS" {
		t.Fatalf("expected successful primary-action result summary, got %+v", ui.LastPrimaryActionResult)
	}
	if ui.LastPrimaryActionResult.NextStep == "" {
		t.Fatalf("expected next-step summary after successful action, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestExecutePrimaryOperatorStepSurfacesBackendRejection(t *testing.T) {
	executor := &stubPrimaryActionExecutor{err: context.DeadlineExceeded}
	source := &stubSnapshotSource{}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}

	err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui)
	if err == nil || !strings.Contains(err.Error(), "primary operator step start_local_run failed") {
		t.Fatalf("expected wrapped primary-action error, got %v", err)
	}
	if len(source.loads) != 0 {
		t.Fatalf("expected no refresh after failed execution, got %#v", source.loads)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "FAILED" {
		t.Fatalf("expected failed primary-action result summary, got %+v", ui.LastPrimaryActionResult)
	}
	if len(ui.LastPrimaryActionResult.Deltas) != 0 {
		t.Fatalf("expected no delta summary for failed action, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestExecutePrimaryOperatorStepRejectsGuidanceOnlyPrimaryStep(t *testing.T) {
	executor := &stubPrimaryActionExecutor{}
	source := &stubSnapshotSource{}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "REVIEW_HANDOFF_FOLLOW_THROUGH",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_1",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}

	err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui)
	if err == nil || !strings.Contains(err.Error(), "guidance-only") {
		t.Fatalf("expected guidance-only rejection, got %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("expected no executor call for non-executable step, got %#v", executor.calls)
	}
	if ui.PrimaryActionInFlight != nil {
		t.Fatalf("expected non-executable step to never enter busy state, got %+v", ui.PrimaryActionInFlight)
	}
	if ui.LastPrimaryActionResult != nil {
		t.Fatalf("expected no execution summary for non-executable step, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestStartPrimaryOperatorStepExecutionRejectsDuplicateWhileInFlight(t *testing.T) {
	executor := &stubPrimaryActionExecutor{
		startedCh: make(chan struct{}, 1),
		releaseCh: make(chan struct{}),
	}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}
	done := make(chan primaryActionExecutionResult, 1)

	if err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done); err != nil {
		t.Fatalf("start primary operator step: %v", err)
	}
	<-executor.startedCh
	if ui.PrimaryActionInFlight == nil || ui.PrimaryActionInFlight.Action != "START_LOCAL_RUN" {
		t.Fatalf("expected in-flight primary action, got %+v", ui.PrimaryActionInFlight)
	}
	err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected duplicate in-flight rejection, got %v", err)
	}
	close(executor.releaseCh)
	result := <-done
	if err := completePrimaryOperatorStepExecution(&stubSnapshotSource{snapshot: snapshot}, "tsk_1", &stubHost{}, nil, &snapshot, &ui, result); err != nil {
		t.Fatalf("complete primary operator step: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("expected exactly one executor call, got %#v", executor.calls)
	}
}

func TestCompletePrimaryOperatorStepExecutionClearsBusyStateAfterFailure(t *testing.T) {
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "START_LOCAL_RUN", CommandSurface: "DEDICATED"},
		},
	}
	ui := UIState{
		Session:               newSessionState(time.Unix(1710000000, 0).UTC()),
		PrimaryActionInFlight: &PrimaryActionInFlightSummary{Action: "START_LOCAL_RUN", StartedAt: time.Unix(1710000000, 0).UTC()},
	}
	err := completePrimaryOperatorStepExecution(&stubSnapshotSource{snapshot: snapshot}, "tsk_1", &stubHost{}, nil, &snapshot, &ui, primaryActionExecutionResult{
		step:       OperatorExecutionStep{Action: "START_LOCAL_RUN"},
		before:     snapshot,
		err:        context.DeadlineExceeded,
		finishedAt: time.Unix(1710000001, 0).UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "primary operator step start_local_run failed") {
		t.Fatalf("expected wrapped failure, got %v", err)
	}
	if ui.PrimaryActionInFlight != nil {
		t.Fatalf("expected busy state to clear after failure, got %+v", ui.PrimaryActionInFlight)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "FAILED" {
		t.Fatalf("expected failure result summary, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestCompletePrimaryOperatorStepExecutionRefreshDuringInFlightPreservesFinalSummary(t *testing.T) {
	before := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "RESUME_INTERRUPTED_LINEAGE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery resume-interrupted --task tsk_1",
				Status:         "REQUIRED_NEXT",
			},
		},
		LocalResume: &LocalResumeAuthoritySummary{
			State:        "ALLOWED",
			Mode:         "RESUME_INTERRUPTED_LINEAGE",
			CheckpointID: "chk_1",
		},
	}
	manualRefreshSnapshot := Snapshot{
		TaskID: "tsk_1",
		Phase:  "PAUSED",
	}
	finalSnapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "FINALIZE_CONTINUE_RECOVERY",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery continue --task tsk_1",
				Status:         "REQUIRED_NEXT",
			},
		},
		LocalResume: &LocalResumeAuthoritySummary{
			State: "NOT_APPLICABLE",
			Mode:  "FINALIZE_CONTINUE_RECOVERY",
		},
	}
	source := &stubSnapshotSource{next: []Snapshot{manualRefreshSnapshot, finalSnapshot}}
	host := &stubHost{}
	ui := UIState{
		Session:               newSessionState(time.Unix(1710000000, 0).UTC()),
		PrimaryActionInFlight: &PrimaryActionInFlightSummary{Action: "RESUME_INTERRUPTED_LINEAGE", StartedAt: time.Unix(1710000000, 0).UTC()},
	}
	current := before
	if err := reloadShellSnapshot(source, "tsk_1", host, nil, &current, &ui, true); err != nil {
		t.Fatalf("manual refresh while busy: %v", err)
	}
	err := completePrimaryOperatorStepExecution(source, "tsk_1", host, nil, &current, &ui, primaryActionExecutionResult{
		step:       OperatorExecutionStep{Action: "RESUME_INTERRUPTED_LINEAGE"},
		before:     before,
		finishedAt: time.Unix(1710000001, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("complete primary operator step after refresh: %v", err)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.NextStep != "required finalize continue recovery" {
		t.Fatalf("expected final summary to use post-action refreshed snapshot, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestPrimaryOperatorStepCanRunAgainAfterPriorExecutionFinishes(t *testing.T) {
	source := &stubSnapshotSource{
		next: []Snapshot{
			{
				TaskID: "tsk_1",
				OperatorExecutionPlan: &OperatorExecutionPlan{
					PrimaryStep: &OperatorExecutionStep{
						Action:         "FINALIZE_CONTINUE_RECOVERY",
						CommandSurface: "DEDICATED",
						CommandHint:    "tuku recovery continue --task tsk_1",
					},
				},
			},
			{
				TaskID: "tsk_1",
				OperatorExecutionPlan: &OperatorExecutionPlan{
					PrimaryStep: &OperatorExecutionStep{
						Action:         "START_LOCAL_RUN",
						CommandSurface: "DEDICATED",
						CommandHint:    "tuku run --task tsk_1 --action start",
					},
				},
			},
		},
	}
	host := &stubHost{}
	executor := &stubPrimaryActionExecutor{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "RESUME_INTERRUPTED_LINEAGE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery resume-interrupted --task tsk_1",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}

	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("first primary action: %v", err)
	}
	if ui.PrimaryActionInFlight != nil {
		t.Fatalf("expected busy state cleared after first action, got %+v", ui.PrimaryActionInFlight)
	}
	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("second primary action: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("expected two sequential executor calls, got %#v", executor.calls)
	}
}

func TestInitialUIStateStartsWithCalmDefaultChrome(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := initialUIState(now, WorkerPreferenceClaude)

	if ui.ShowInspector {
		t.Fatal("expected inspector to be hidden by default")
	}
	if ui.ShowProof {
		t.Fatal("expected activity strip to be hidden by default")
	}
	if ui.Focus != FocusWorker {
		t.Fatalf("expected worker focus, got %v", ui.Focus)
	}
	if ui.Session.WorkerPreference != WorkerPreferenceClaude {
		t.Fatalf("expected worker preference to be preserved, got %q", ui.Session.WorkerPreference)
	}
	if !ui.ObservedAt.Equal(now) {
		t.Fatalf("expected observed time to initialize, got %v want %v", ui.ObservedAt, now)
	}
}

func TestNextFocusCyclesVisiblePanes(t *testing.T) {
	ui := UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
	}
	if got := nextFocus(ui); got != FocusInspector {
		t.Fatalf("expected inspector focus, got %v", got)
	}
	ui.Focus = FocusInspector
	if got := nextFocus(ui); got != FocusActivity {
		t.Fatalf("expected activity focus, got %v", got)
	}
	ui.ShowInspector = false
	ui.Focus = FocusWorker
	if got := nextFocus(ui); got != FocusActivity {
		t.Fatalf("expected activity focus when inspector hidden, got %v", got)
	}
}

func TestRouteKeyForwardsInputToLiveWorkerHost(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 'a'); action != actionNone {
		t.Fatalf("expected no shell action for worker input, got %v", action)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "a" {
		t.Fatalf("expected worker input to be forwarded, got %#v", host.writes)
	}

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if !ui.EscapePrefix {
		t.Fatal("expected escape prefix to be armed")
	}

	if action := routeKey(&ui, host, 'q'); action != actionQuit {
		t.Fatalf("expected prefixed q to quit shell, got %v", action)
	}
}

func TestRouteKeyUsesPrefixedScratchAdoptionCommand(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'a'); action != actionStageScratchAdoption {
		t.Fatalf("expected staged-scratch action, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected prefixed adoption command to stay shell-local, got %#v", host.writes)
	}
}

func TestRouteKeyUsesPrefixedPendingDraftEditCommand(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "draft",
	}

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'e'); action != actionEnterPendingTaskMessageEdit {
		t.Fatalf("expected edit-draft action, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected prefixed edit command to stay shell-local, got %#v", host.writes)
	}
}

func TestRouteKeyUsesPrefixedPrimaryOperatorExecutionCommand(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 'n'); action != actionNone {
		t.Fatalf("expected raw n to pass through to live worker input, got %v", action)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "n" {
		t.Fatalf("expected raw n to reach worker input, got %#v", host.writes)
	}

	host.writes = nil
	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'n'); action != actionExecutePrimaryOperatorStep {
		t.Fatalf("expected prefixed execute-primary action, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected prefixed execute-primary command to stay shell-local, got %#v", host.writes)
	}
}

func TestPrefixedPrimaryOperatorExecutionDoesNotDoubleExecuteWhileBusy(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	executor := &stubPrimaryActionExecutor{
		startedCh: make(chan struct{}, 1),
		releaseCh: make(chan struct{}),
	}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Focus: FocusWorker, Session: newSessionState(time.Unix(1710000000, 0).UTC())}
	done := make(chan primaryActionExecutionResult, 1)

	if err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done); err != nil {
		t.Fatalf("start primary operator step: %v", err)
	}
	<-executor.startedCh

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'n'); action != actionExecutePrimaryOperatorStep {
		t.Fatalf("expected prefixed execute-primary action, got %v", action)
	}
	err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected duplicate in-flight rejection, got %v", err)
	}

	close(executor.releaseCh)
	result := <-done
	if err := completePrimaryOperatorStepExecution(&stubSnapshotSource{snapshot: snapshot}, "tsk_1", host, nil, &snapshot, &ui, result); err != nil {
		t.Fatalf("complete primary operator step: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("expected exactly one executor call, got %#v", executor.calls)
	}
}

func TestRouteKeyExplainsUnavailableWorkerInput(t *testing.T) {
	host := &stubHost{
		canInput: false,
		status:   HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript fallback"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 'z'); action != actionNone {
		t.Fatalf("expected no shell action for unavailable worker input, got %v", action)
	}
	if ui.LastError == "" {
		t.Fatal("expected unavailable input message")
	}
}

func TestStagePendingTaskMessageFromLocalScratch(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{Session: newSessionState(now)}
	err := stagePendingTaskMessageFromLocalScratch(&ui, Snapshot{
		TaskID: "tsk_1",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure", CreatedAt: now},
				{Role: "user", Body: "List initial requirements", CreatedAt: now},
			},
		},
	})
	if err != nil {
		t.Fatalf("stage pending task message: %v", err)
	}
	if ui.PendingTaskMessageSource != "local_scratch_adoption" {
		t.Fatalf("expected local scratch adoption source, got %q", ui.PendingTaskMessageSource)
	}
	if ui.PendingTaskMessage == "" || !strings.Contains(ui.PendingTaskMessage, "Plan project structure") {
		t.Fatalf("expected staged draft to include scratch notes, got %q", ui.PendingTaskMessage)
	}
	if len(ui.Session.Journal) == 0 || ui.Session.Journal[len(ui.Session.Journal)-1].Type != SessionEventPendingMessageStaged {
		t.Fatalf("expected staged journal event, got %#v", ui.Session.Journal)
	}
}

func TestEnterPendingTaskMessageEditModeRequiresPendingDraft(t *testing.T) {
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}
	if err := enterPendingTaskMessageEditMode(&ui); err == nil {
		t.Fatal("expected missing pending draft error")
	}
}

func TestPendingTaskMessageEditInputStaysLocalUntilSaved(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "Draft",
		Session:            newSessionState(now),
	}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	if action := routeKey(&ui, host, '!'); action != actionNone {
		t.Fatalf("expected local edit input, got %v", action)
	}
	if action := routeKey(&ui, host, '\n'); action != actionNone {
		t.Fatalf("expected newline edit input, got %v", action)
	}
	if action := routeKey(&ui, host, 'X'); action != actionNone {
		t.Fatalf("expected local edit input, got %v", action)
	}
	if ui.PendingTaskMessage != "Draft" {
		t.Fatalf("expected saved draft to stay unchanged during edit, got %q", ui.PendingTaskMessage)
	}
	if ui.PendingTaskMessageEditBuffer != "Draft!\nX" {
		t.Fatalf("expected edit buffer to change locally, got %q", ui.PendingTaskMessageEditBuffer)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected edit-mode input to stay local, got %#v", host.writes)
	}
}

func TestPendingTaskMessageEditBackspaceRemovesLastRuneSafely(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "界",
		Session:            newSessionState(now),
	}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	if action := routeKey(&ui, host, 0x7f); action != actionNone {
		t.Fatalf("expected local backspace handling, got %v", action)
	}
	if ui.PendingTaskMessageEditBuffer != "" {
		t.Fatalf("expected multibyte rune to be removed cleanly, got %q", ui.PendingTaskMessageEditBuffer)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected backspace to stay local in edit mode, got %#v", host.writes)
	}
}

func TestSendPendingTaskMessageMakesDraftCanonicalOnlyOnExplicitSend(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "Explicitly adopt these notes",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err != nil {
		t.Fatalf("send pending task message: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one sent message, got %#v", sender.sent)
	}
	if sender.sent[0].taskID != "tsk_1" || sender.sent[0].message != "Explicitly adopt these notes" {
		t.Fatalf("unexpected sent payload: %#v", sender.sent[0])
	}
	if ui.PendingTaskMessage != "" || ui.PendingTaskMessageSource != "" {
		t.Fatalf("expected pending draft to clear after explicit send, got %+v", ui)
	}
	if len(ui.Session.Journal) == 0 || ui.Session.Journal[len(ui.Session.Journal)-1].Type != SessionEventPendingMessageSent {
		t.Fatalf("expected sent journal event, got %#v", ui.Session.Journal)
	}
}

func TestSendPendingTaskMessageUsesEditedDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "Original draft",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	ui.PendingTaskMessageEditBuffer = "Edited draft"
	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err != nil {
		t.Fatalf("send edited draft: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].message != "Edited draft" {
		t.Fatalf("expected edited draft to be sent, got %#v", sender.sent)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected edit mode to end after explicit send")
	}
}

func TestSendPendingTaskMessagePreservesMultilineDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "line 1\n\nline 2\n",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err != nil {
		t.Fatalf("send multiline draft: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].message != "line 1\n\nline 2\n" {
		t.Fatalf("expected multiline draft to be preserved, got %#v", sender.sent)
	}
}

func TestSendPendingTaskMessageRejectsEffectivelyEmptyDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage: " \n\t ",
		Session:            newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err == nil {
		t.Fatal("expected effectively empty draft to be rejected")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected no send for empty draft, got %#v", sender.sent)
	}
}

func TestSavePendingTaskMessageEditRestoresWorkerRouting(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "Draft",
		Session:            newSessionState(now),
	}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	if action := routeKey(&ui, host, '1'); action != actionNone {
		t.Fatalf("expected edit-mode local input, got %v", action)
	}
	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 's'); action != actionSavePendingTaskMessageEdit {
		t.Fatalf("expected save-edit action, got %v", action)
	}
	if err := savePendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("save edit mode: %v", err)
	}
	if ui.PendingTaskMessage != "Draft1" {
		t.Fatalf("expected saved draft to include edit, got %q", ui.PendingTaskMessage)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected edit mode to end after save")
	}
	if action := routeKey(&ui, host, 'z'); action != actionNone {
		t.Fatalf("expected worker input after save, got %v", action)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "z" {
		t.Fatalf("expected normal worker routing after save, got %#v", host.writes)
	}
}

func TestCancelPendingTaskMessageEditRestoresSavedDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage: "Saved draft",
		Session:            newSessionState(now),
	}
	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	ui.PendingTaskMessageEditBuffer = "Edited draft"
	if err := cancelPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("cancel edit mode: %v", err)
	}
	if ui.PendingTaskMessage != "Saved draft" {
		t.Fatalf("expected saved draft to be restored, got %q", ui.PendingTaskMessage)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected edit mode to be inactive after cancel")
	}
}

func TestClearPendingTaskMessageClearsStagedAndEditState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "Saved draft",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	ui.PendingTaskMessageEditBuffer = "Edited draft"

	clearPendingTaskMessage(&ui)

	if ui.PendingTaskMessage != "" || ui.PendingTaskMessageSource != "" {
		t.Fatalf("expected staged draft to be cleared, got %+v", ui)
	}
	if ui.PendingTaskMessageEditMode || ui.PendingTaskMessageEditBuffer != "" || ui.PendingTaskMessageEditOriginal != "" {
		t.Fatalf("expected edit state to be cleared, got %+v", ui)
	}
}

func TestReloadShellSnapshotDoesNotRebuildClearedDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	source := &stubSnapshotSource{
		snapshot: Snapshot{
			TaskID: "tsk_1",
			LocalScratch: &LocalScratchContext{
				RepoRoot: "/tmp/repo",
				Notes: []ConversationItem{
					{Role: "user", Body: "Plan project structure", CreatedAt: now},
				},
			},
		},
	}
	host := &stubHost{}
	snapshot := Snapshot{}
	ui := UIState{
		PendingTaskMessage:       "Staged draft",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}

	clearPendingTaskMessage(&ui)

	if err := reloadShellSnapshot(source, "tsk_1", host, nil, &snapshot, &ui, true); err != nil {
		t.Fatalf("reload shell snapshot: %v", err)
	}
	if ui.PendingTaskMessage != "" || ui.PendingTaskMessageSource != "" {
		t.Fatalf("expected refresh to leave cleared draft empty, got %+v", ui)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected refresh to leave edit mode inactive")
	}
	if snapshot.TaskID != "tsk_1" {
		t.Fatalf("expected refreshed snapshot to load, got %+v", snapshot)
	}
}

func TestApplyHostResizeUsesWorkerPaneDimensions(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"}}
	ui := UIState{ShowInspector: true, ShowProof: true}

	if !applyHostResize(host, 120, 32, ui) {
		t.Fatal("expected resize to be propagated")
	}
	if len(host.resizes) != 1 {
		t.Fatalf("expected one resize call, got %d", len(host.resizes))
	}
	if host.resizes[0][0] <= 0 || host.resizes[0][1] <= 0 {
		t.Fatalf("expected positive resize dimensions, got %#v", host.resizes[0])
	}
}

func TestCaptureHostLifecycleRecordsJournalAndPersistsMilestones(t *testing.T) {
	sink := &stubLifecycleSink{}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{Session: newSessionState(now)}

	live := HostStatus{
		Mode:      HostModeCodexPTY,
		State:     HostStateLive,
		Label:     "codex live",
		InputLive: true,
		Width:     80,
		Height:    24,
	}
	captureHostLifecycle(context.Background(), sink, "tsk_1", ui.Session.SessionID, &ui, HostStatus{}, live)

	if len(ui.Session.Journal) != 1 || ui.Session.Journal[0].Type != SessionEventHostLive {
		t.Fatalf("expected live host journal entry, got %#v", ui.Session.Journal)
	}
	if len(sink.records) != 1 || sink.records[0].kind != PersistedLifecycleHostStarted {
		t.Fatalf("expected persisted host-start record, got %#v", sink.records)
	}

	exitCode := 9
	exited := HostStatus{
		Mode:      HostModeCodexPTY,
		State:     HostStateExited,
		Label:     "codex exited",
		InputLive: false,
		ExitCode:  &exitCode,
	}
	captureHostLifecycle(context.Background(), sink, "tsk_1", ui.Session.SessionID, &ui, live, exited)

	if len(ui.Session.Journal) != 2 || ui.Session.Journal[1].Type != SessionEventHostExited {
		t.Fatalf("expected host-exited journal entry, got %#v", ui.Session.Journal)
	}
	if len(sink.records) != 2 || sink.records[1].kind != PersistedLifecycleHostExited {
		t.Fatalf("expected persisted host-exit record, got %#v", sink.records)
	}
}

func TestFallbackTransitionPreservesSessionIdentity(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{Session: newSessionState(now)}
	sessionID := ui.Session.SessionID
	exitCode := 7
	current := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateExited,
			Label:     "codex exited",
			ExitCode:  &exitCode,
			InputLive: false,
		},
	}
	fallback := NewTranscriptHost()

	nextHost, _, changed := transitionExitedHost(context.Background(), current, fallback, Snapshot{TaskID: "tsk_2"})
	if !changed {
		t.Fatal("expected fallback transition")
	}

	captureHostLifecycle(context.Background(), nil, "tsk_2", ui.Session.SessionID, &ui, current.Status(), nextHost.Status())

	if ui.Session.SessionID != sessionID {
		t.Fatalf("expected session identity to survive fallback, got %q want %q", ui.Session.SessionID, sessionID)
	}
	if len(ui.Session.Journal) != 1 || ui.Session.Journal[0].Type != SessionEventFallbackActivated {
		t.Fatalf("expected fallback journal entry, got %#v", ui.Session.Journal)
	}
}
```

## internal/tui/shell/primary_action_executor_test.go
```go
package shell

import (
	"context"
	"encoding/json"
	"testing"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

func TestExecutePrimaryStepIPCUsesUnifiedBackendNextRoute(t *testing.T) {
	original := primaryActionIPCCall
	defer func() { primaryActionIPCCall = original }()

	called := false
	primaryActionIPCCall = func(_ context.Context, socketPath string, req ipc.Request) (ipc.Response, error) {
		called = true
		if socketPath != "/tmp/tuku.sock" {
			t.Fatalf("unexpected socket path: %s", socketPath)
		}
		if req.Method != ipc.MethodExecutePrimaryOperatorStep {
			t.Fatalf("expected unified primary-step method, got %s", req.Method)
		}
		var payload ipc.TaskExecutePrimaryOperatorStepRequest
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.TaskID != common.TaskID("tsk_123") {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		body, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_123"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_123",
				TaskID:       common.TaskID("tsk_123"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_123",
			},
		})
		return ipc.Response{OK: true, Payload: body}, nil
	}

	out, err := executePrimaryStepIPC(context.Background(), "/tmp/tuku.sock", "tsk_123", OperatorExecutionStep{Action: "START_LOCAL_RUN", CommandSurface: "DEDICATED"})
	if err != nil {
		t.Fatalf("executePrimaryStepIPC: %v", err)
	}
	if !called {
		t.Fatal("expected unified IPC route to be called")
	}
	if out.Receipt.ReceiptID != "orec_123" || out.Receipt.ActionHandle != "START_LOCAL_RUN" || out.Receipt.ResultClass != "SUCCEEDED" {
		t.Fatalf("unexpected execution outcome: %+v", out)
	}
}

func TestExecutablePrimaryStepRejectsInspectFallback(t *testing.T) {
	_, err := executablePrimaryStep(Snapshot{
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "REVIEW_HANDOFF_FOLLOW_THROUGH",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_123",
			},
		},
	})
	if err == nil {
		t.Fatal("expected inspect-fallback primary step to be non-executable")
	}
}

func TestExecutablePrimaryStepRequiresDedicatedCommandSurface(t *testing.T) {
	_, err := executablePrimaryStep(Snapshot{
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_123",
			},
		},
	})
	if err == nil {
		t.Fatal("expected non-dedicated command surface to block direct execution")
	}
}
```

## internal/tui/shell/primary_action_result_test.go
```go
package shell

import (
	"strings"
	"testing"
	"time"
)

func TestSuccessfulPrimaryActionResultAcceptedHandoffLaunchShowsMeaningfulLaunchedDelta(t *testing.T) {
	before := Snapshot{
		ActiveBranch:      &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		LaunchControl:     &LaunchControlSummary{State: "NOT_REQUESTED", RetryDisposition: "ALLOWED"},
		HandoffContinuity: &HandoffContinuitySummary{State: "ACCEPTED_NOT_LAUNCHED"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF", Status: "REQUIRED_NEXT"},
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			RequiredNextAction: "LAUNCH_ACCEPTED_HANDOFF",
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
	}
	after := Snapshot{
		ActiveBranch:      &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		LaunchControl:     &LaunchControlSummary{State: "COMPLETED", RetryDisposition: "BLOCKED"},
		HandoffContinuity: &HandoffContinuitySummary{State: "LAUNCH_COMPLETED_ACK_CAPTURED"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF", Status: "ALLOWED"},
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.Outcome != "SUCCESS" || result.Summary != "executed launch accepted handoff" {
		t.Fatalf("unexpected launch result summary: %+v", result)
	}
	if result.NextStep != "allowed resolve active handoff" {
		t.Fatalf("expected next step to move forward truthfully after launch, got %+v", result)
	}
	joined := strings.Join(result.Deltas, "\n")
	if !strings.Contains(joined, "launch not requested | retry allowed -> completed (invocation only) | retry blocked") {
		t.Fatalf("expected launch-state delta, got %+v", result.Deltas)
	}
	if !strings.Contains(joined, "handoff accepted, not launched -> launch completed, acknowledgment captured, downstream unproven") {
		t.Fatalf("expected handoff continuity delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultInterruptedResumeShowsContinueFinalizationNext(t *testing.T) {
	before := Snapshot{
		LocalResume: &LocalResumeAuthoritySummary{
			State:        "ALLOWED",
			Mode:         "RESUME_INTERRUPTED_LINEAGE",
			CheckpointID: "chk_1",
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESUME_INTERRUPTED_LINEAGE", Status: "REQUIRED_NEXT"},
		},
	}
	after := Snapshot{
		LocalResume: &LocalResumeAuthoritySummary{
			State: "NOT_APPLICABLE",
			Mode:  "FINALIZE_CONTINUE_RECOVERY",
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "FINALIZE_CONTINUE_RECOVERY", Status: "REQUIRED_NEXT"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "RESUME_INTERRUPTED_LINEAGE"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.NextStep != "required finalize continue recovery" {
		t.Fatalf("expected continue-finalization next step after interrupted resume, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.Deltas, "\n"), "local resume allowed via checkpoint chk_1 -> not applicable | finalize continue first") {
		t.Fatalf("expected local-resume delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultContinueRecoveryShowsReadyNextRun(t *testing.T) {
	before := Snapshot{
		OperatorDecision: &OperatorDecisionSummary{Headline: "Continue finalization required"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "FINALIZE_CONTINUE_RECOVERY", Status: "REQUIRED_NEXT"},
		},
	}
	after := Snapshot{
		OperatorDecision: &OperatorDecisionSummary{Headline: "Local fresh run ready"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "START_LOCAL_RUN", Status: "REQUIRED_NEXT"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "FINALIZE_CONTINUE_RECOVERY"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.NextStep != "required start local run" {
		t.Fatalf("expected ready-next-run plan after continue recovery, got %+v", result)
	}
	joined := strings.Join(result.Deltas, "\n")
	if !strings.Contains(joined, "decision Continue finalization required -> Local fresh run ready") && !strings.Contains(joined, "decision continue finalization required -> local fresh run ready") {
		t.Fatalf("expected decision delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultActiveHandoffResolutionReturnsLocalOwnership(t *testing.T) {
	before := Snapshot{
		ActiveBranch: &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF", Status: "ALLOWED"},
		},
	}
	after := Snapshot{
		ActiveBranch: &ActiveBranchSummary{Class: "LOCAL", ActionabilityAnchor: "BRIEF", ActionabilityAnchorRef: "brf_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "ALLOWED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "START_LOCAL_RUN", Status: "REQUIRED_NEXT"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.NextStep != "required start local run" {
		t.Fatalf("expected local next step after handoff resolution, got %+v", result)
	}
	joined := strings.Join(result.Deltas, "\n")
	if !strings.Contains(joined, "branch Claude hnd_1 -> local") {
		t.Fatalf("expected branch-owner delta, got %+v", result.Deltas)
	}
	if !strings.Contains(joined, "local message blocked -> allowed") {
		t.Fatalf("expected local-mutation delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultStaysCompact(t *testing.T) {
	before := Snapshot{
		ActiveBranch:         &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		OperatorDecision:     &OperatorDecisionSummary{Headline: "Accepted handoff launch ready"},
		LaunchControl:        &LaunchControlSummary{State: "NOT_REQUESTED", RetryDisposition: "ALLOWED"},
		HandoffContinuity:    &HandoffContinuitySummary{State: "ACCEPTED_NOT_LAUNCHED"},
		LocalResume:          &LocalResumeAuthoritySummary{State: "BLOCKED", Mode: "RESUME_INTERRUPTED_LINEAGE", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_1"},
		LocalRunFinalization: &LocalRunFinalizationSummary{State: "STALE_RECONCILIATION_REQUIRED", RunID: "run_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF", Status: "REQUIRED_NEXT"},
		},
	}
	after := Snapshot{
		ActiveBranch:         &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		OperatorDecision:     &OperatorDecisionSummary{Headline: "Active Claude handoff pending"},
		LaunchControl:        &LaunchControlSummary{State: "COMPLETED", RetryDisposition: "BLOCKED"},
		HandoffContinuity:    &HandoffContinuitySummary{State: "LAUNCH_COMPLETED_ACK_CAPTURED"},
		LocalResume:          &LocalResumeAuthoritySummary{State: "BLOCKED", Mode: "RESUME_INTERRUPTED_LINEAGE", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_1"},
		LocalRunFinalization: &LocalRunFinalizationSummary{State: "STALE_RECONCILIATION_REQUIRED", RunID: "run_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF", Status: "ALLOWED"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if len(result.Deltas) == 0 || len(result.Deltas) > 4 {
		t.Fatalf("expected compact delta summary, got %+v", result.Deltas)
	}
}
```

## internal/tui/shell/viewmodel_test.go
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
		HandoffContinuity: &HandoffContinuitySummary{
			State:                        "LAUNCH_COMPLETED_ACK_CAPTURED",
			Reason:                       "Claude handoff launch completed and initial acknowledgment was captured; downstream continuation remains unproven",
			LaunchID:                     "hlc_1",
			AcknowledgmentStatus:         "CAPTURED",
			AcknowledgmentSummary:        "Claude acknowledged the handoff packet.",
			DownstreamContinuationProven: false,
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
	foundHandoffContinuity := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "continuity launch completed, acknowledgment captured, downstream unproven") {
				foundHandoffContinuity = true
				break
			}
		}
	}
	if !foundHandoffContinuity {
		t.Fatalf("expected handoff continuity line in inspector, got %#v", vm.Inspector.Sections)
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

func TestBuildViewModelSurfacesContinueExecutionRequiredState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_continue_pending",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "CONTINUE_EXECUTION_REQUIRED",
			Action:          "EXECUTE_CONTINUE_RECOVERY",
			ReadyForNextRun: false,
			Reason:          "operator chose to continue with the current brief, but explicit continue finalization is still required",
		},
		Checkpoint: &CheckpointSummary{
			ID:          "chk_continue_pending",
			IsResumable: true,
		},
		LocalResume: &LocalResumeAuthoritySummary{
			State: "NOT_APPLICABLE",
			Mode:  "FINALIZE_CONTINUE_RECOVERY",
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			RequiredNextAction: "FINALIZE_CONTINUE_RECOVERY",
			Actions: []OperatorActionAuthority{
				{Action: "FINALIZE_CONTINUE_RECOVERY", State: "REQUIRED_NEXT", Reason: "operator chose to continue with the current brief, but explicit continue finalization is still required"},
				{Action: "RESUME_INTERRUPTED_LINEAGE", State: "NOT_APPLICABLE", Reason: "local interrupted-lineage resume is not applicable; explicit continue recovery must be executed before any new bounded run"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:      "FINALIZE_CONTINUE_RECOVERY",
				Status:      "REQUIRED_NEXT",
				Domain:      "LOCAL",
				CommandHint: "tuku recovery continue --task tsk_continue_pending",
			},
			MandatoryBeforeProgress: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "continue-pending" {
		t.Fatalf("expected continue-pending continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery continue confirmation required") {
		t.Fatalf("expected continue-confirmation operator state, got %q", status)
	}
	if !strings.Contains(status, "next finalize continue") {
		t.Fatalf("expected finalize-continue operator action, got %q", status)
	}
	if !strings.Contains(status, "authority required finalize continue") {
		t.Fatalf("expected required-next authority line, got %q", status)
	}
	if !strings.Contains(status, "plan required finalize continue recovery") {
		t.Fatalf("expected execution plan line, got %q", status)
	}
	if !strings.Contains(status, "command tuku recovery continue --task tsk_continue_pending") {
		t.Fatalf("expected execution command hint line, got %q", status)
	}
	if !strings.Contains(status, "local resume not applicable | finalize continue first") {
		t.Fatalf("expected explicit local-resume distinction, got %q", status)
	}
}

func TestBuildViewModelSurfacesBlockedLocalMutationAuthorityUnderClaudeOwnership(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_block",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:  "HANDOFF_LAUNCH_COMPLETED",
			Action: "MONITOR_LAUNCHED_HANDOFF",
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_block", Reason: "Cannot send a local task message while launched Claude handoff hnd_block remains the active continuity branch."},
				{Action: "CREATE_CHECKPOINT", State: "BLOCKED", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_block", Reason: "Cannot create a local checkpoint while launched Claude handoff hnd_block remains the active continuity branch."},
				{Action: "RESOLVE_ACTIVE_HANDOFF", State: "ALLOWED", Reason: "active Claude handoff branch can be explicitly resolved without claiming downstream completion"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:      "RESOLVE_ACTIVE_HANDOFF",
				Status:      "ALLOWED",
				Domain:      "HANDOFF_CLAUDE",
				CommandHint: "tuku handoff-resolve --task tsk_handoff_block --handoff hnd_block --kind <abandoned|superseded-by-local|closed-unproven|reviewed-stale>",
			},
			MandatoryBeforeProgress: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "authority local mutation blocked by Claude handoff hnd_block") {
		t.Fatalf("expected blocked local mutation authority line, got %q", status)
	}
	if !strings.Contains(status, "plan allowed resolve active handoff") {
		t.Fatalf("expected execution plan line under Claude ownership, got %q", status)
	}
	if !strings.Contains(status, "command tuku handoff-resolve --task tsk_handoff_block --handoff hnd_block") {
		t.Fatalf("expected handoff resolution command hint, got %q", status)
	}
}

func TestBuildViewModelDifferentiatesLocalResumeModes(t *testing.T) {
	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "interrupted resume allowed",
			snapshot: Snapshot{
				TaskID: "tsk_interrupt",
				Recovery: &RecoverySummary{
					Class:  "INTERRUPTED_RUN_RECOVERABLE",
					Action: "RESUME_INTERRUPTED_RUN",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State:        "ALLOWED",
					Mode:         "RESUME_INTERRUPTED_LINEAGE",
					CheckpointID: "chk_interrupt",
				},
			},
			want: "local resume allowed via checkpoint chk_interr",
		},
		{
			name: "fresh next run",
			snapshot: Snapshot{
				TaskID: "tsk_ready",
				Recovery: &RecoverySummary{
					Class:           "READY_NEXT_RUN",
					Action:          "START_NEXT_RUN",
					ReadyForNextRun: true,
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "START_FRESH_NEXT_RUN",
				},
			},
			want: "local resume not applicable | start fresh next run",
		},
		{
			name: "blocked by Claude branch",
			snapshot: Snapshot{
				TaskID: "tsk_blocked",
				Recovery: &RecoverySummary{
					Class:  "ACCEPTED_HANDOFF_LAUNCH_READY",
					Action: "LAUNCH_ACCEPTED_HANDOFF",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State:               "BLOCKED",
					BlockingBranchClass: "HANDOFF_CLAUDE",
					BlockingBranchRef:   "hnd_block",
				},
			},
			want: "local resume blocked by Claude handoff hnd_block",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := BuildViewModel(tc.snapshot, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)
			if vm.Overlay == nil {
				t.Fatal("expected status overlay")
			}
			status := strings.Join(vm.Overlay.Lines, "\n")
			if !strings.Contains(status, tc.want) {
				t.Fatalf("expected overlay to contain %q, got %q", tc.want, status)
			}
		})
	}
}

func TestBuildViewModelDifferentiatesLocalRunFinalizationFromResumeAuthority(t *testing.T) {
	cases := []struct {
		name       string
		snapshot   Snapshot
		wantRun    string
		wantResume string
	}{
		{
			name: "stale reconciliation",
			snapshot: Snapshot{
				TaskID: "tsk_stale",
				Recovery: &RecoverySummary{
					Class:  "STALE_RUN_RECONCILIATION_REQUIRED",
					Action: "RECONCILE_STALE_RUN",
				},
				LocalRunFinalization: &LocalRunFinalizationSummary{
					State: "STALE_RECONCILIATION_REQUIRED",
					RunID: "run_stale",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "NONE",
				},
			},
			wantRun:    "local run stale reconciliation required run_stale",
			wantResume: "local resume not applicable",
		},
		{
			name: "failed review required",
			snapshot: Snapshot{
				TaskID: "tsk_failed",
				Recovery: &RecoverySummary{
					Class:  "FAILED_RUN_REVIEW_REQUIRED",
					Action: "INSPECT_FAILED_RUN",
				},
				LocalRunFinalization: &LocalRunFinalizationSummary{
					State: "FAILED_REVIEW_REQUIRED",
					RunID: "run_failed",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "NONE",
				},
			},
			wantRun:    "local run failed review required run_failed",
			wantResume: "local resume not applicable",
		},
		{
			name: "fresh next run",
			snapshot: Snapshot{
				TaskID: "tsk_ready_again",
				Recovery: &RecoverySummary{
					Class:           "READY_NEXT_RUN",
					Action:          "START_NEXT_RUN",
					ReadyForNextRun: true,
				},
				LocalRunFinalization: &LocalRunFinalizationSummary{
					State: "FINALIZED",
					RunID: "run_done",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "START_FRESH_NEXT_RUN",
				},
			},
			wantRun:    "local run finalized run_done",
			wantResume: "local resume not applicable | start fresh next run",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := BuildViewModel(tc.snapshot, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)
			if vm.Overlay == nil {
				t.Fatal("expected status overlay")
			}
			status := strings.Join(vm.Overlay.Lines, "\n")
			if !strings.Contains(status, tc.wantRun) {
				t.Fatalf("expected overlay to contain %q, got %q", tc.wantRun, status)
			}
			if !strings.Contains(status, tc.wantResume) {
				t.Fatalf("expected overlay to contain %q, got %q", tc.wantResume, status)
			}
		})
	}
}

func TestBuildViewModelSurfacesResolvedClaudeHandoffContinuity(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_resolved_handoff",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "READY_NEXT_RUN",
			Action:          "START_NEXT_RUN",
			ReadyForNextRun: true,
			Reason:          "explicitly resolved Claude handoff no longer blocks local control",
		},
		Resolution: &ResolutionSummary{
			ResolutionID: "hrs_1",
			Kind:         "SUPERSEDED_BY_LOCAL",
			Summary:      "operator returned local control",
		},
		ActiveBranch: &ActiveBranchSummary{
			Class:                  "LOCAL",
			BranchRef:              "tsk_resolved_handoff",
			ActionabilityAnchor:    "BRIEF",
			ActionabilityAnchorRef: "brf_local",
			Reason:                 "local Tuku lineage currently controls canonical progression",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:  "NOT_APPLICABLE",
			Reason: "no Claude handoff continuity is active",
		},
	}, UIState{ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "ready" {
		t.Fatalf("expected ready continuity after explicit resolution, got %q", vm.Header.Continuity)
	}
	found := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "resolution superseded-by-local") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected historical resolution line in inspector, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelSurfacesClaudeActiveBranchOwnership(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_owner",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "ACCEPTED_HANDOFF_LAUNCH_READY",
			Action:                "LAUNCH_ACCEPTED_HANDOFF",
			ReadyForHandoffLaunch: true,
			Reason:                "accepted Claude handoff is ready to launch",
		},
		ActiveBranch: &ActiveBranchSummary{
			Class:                  "HANDOFF_CLAUDE",
			BranchRef:              "hnd_1",
			ActionabilityAnchor:    "HANDOFF",
			ActionabilityAnchorRef: "hnd_1",
			Reason:                 "accepted Claude handoff branch currently owns continuity",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:  "ACCEPTED_NOT_LAUNCHED",
			Reason: "accepted Claude handoff is ready to launch",
		},
	}, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "branch Claude handoff hnd_1") {
		t.Fatalf("expected explicit Claude branch ownership in status overlay, got %q", status)
	}
}

func TestBuildViewModelSurfacesStalledClaudeFollowThroughState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_stalled",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED",
			Action:          "REVIEW_HANDOFF_FOLLOW_THROUGH",
			ReadyForNextRun: false,
			Reason:          "Claude handoff follow-through appears stalled and needs review",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:                "FOLLOW_THROUGH_STALLED",
			Reason:               "Claude handoff launch appears stalled and needs review",
			FollowThroughKind:    "STALLED_REVIEW_REQUIRED",
			FollowThroughSummary: "Claude follow-through appears stalled",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
		},
		FollowThrough: &FollowThroughSummary{
			RecordID: "hft_1",
			Kind:     "STALLED_REVIEW_REQUIRED",
			Summary:  "Claude follow-through appears stalled",
		},
	}, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "review" {
		t.Fatalf("expected review continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery handoff follow-through review required") {
		t.Fatalf("expected stalled handoff operator state, got %q", status)
	}
	if !strings.Contains(status, "next review handoff follow-through") {
		t.Fatalf("expected stalled handoff operator action, got %q", status)
	}
	found := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "continuity follow-through stalled, review required") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected stalled follow-through continuity in inspector, got %#v", vm.Inspector.Sections)
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
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "RESUME_INTERRUPTED_LINEAGE",
				Status:         "REQUIRED_NEXT",
				Domain:         "LOCAL",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery resume-interrupted --task tsk_interrupt",
			},
			MandatoryBeforeProgress: true,
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
	if !strings.Contains(joined, "plan required resume interrupted lineage") || !strings.Contains(joined, "command tuku recovery resume-interrupted --task tsk_interrupt") {
		t.Fatalf("expected interrupted execution plan in operator section, got %q", joined)
	}
	if !strings.Contains(vm.Footer, "n execute next step") {
		t.Fatalf("expected direct-execution footer cue, got %q", vm.Footer)
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
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "LAUNCH_ACCEPTED_HANDOFF",
				Status:         "REQUIRED_NEXT",
				Domain:         "HANDOFF_CLAUDE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku handoff-launch --task tsk_launch_ready --handoff hnd_1",
			},
			MandatoryBeforeProgress: true,
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
	statusOverlay := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(statusOverlay, "plan required launch accepted handoff") || !strings.Contains(statusOverlay, "command tuku handoff-launch --task tsk_launch_ready --handoff hnd_1") {
		t.Fatalf("expected launch-ready execution plan lines, got %q", statusOverlay)
	}
	if !strings.Contains(vm.Footer, "n execute next step") {
		t.Fatalf("expected launch-ready direct-execution footer cue, got %q", vm.Footer)
	}
}

func TestBuildViewModelDoesNotShowDirectExecuteCueForInspectFallbackPrimaryStep(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_review_only",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "REVIEW_HANDOFF_FOLLOW_THROUGH",
				Status:         "REQUIRED_NEXT",
				Domain:         "REVIEW",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_review_only",
			},
		},
	}, UIState{Session: SessionState{SessionID: "shs_review_only"}}, NewTranscriptHost(), 120, 32)

	if strings.Contains(vm.Footer, "execute next step") {
		t.Fatalf("expected no direct-execution footer cue for inspect fallback, got %q", vm.Footer)
	}
}

func TestBuildViewModelSurfacesPrimaryActionResultSummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_result",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "FINALIZE_CONTINUE_RECOVERY",
				Status:         "REQUIRED_NEXT",
				Domain:         "LOCAL",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery continue --task tsk_result",
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_result"},
		LastPrimaryActionResult: &PrimaryActionResultSummary{
			Action:    "RESUME_INTERRUPTED_LINEAGE",
			Outcome:   "SUCCESS",
			Summary:   "executed resume interrupted lineage",
			Deltas:    []string{"next required resume interrupted lineage -> required finalize continue recovery"},
			NextStep:  "required finalize continue recovery",
			CreatedAt: now,
		},
	}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	joined := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(joined, "result success | executed resume interrupted lineage") {
		t.Fatalf("expected result headline in status overlay, got %q", joined)
	}
	if !strings.Contains(joined, "delta next required resume interrupted lineage -> required finalize continue recovery") {
		t.Fatalf("expected delta in status overlay, got %q", joined)
	}
	if !strings.Contains(joined, "new next required finalize continue recovery") {
		t.Fatalf("expected new next-step line in status overlay, got %q", joined)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	operator := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(operator, "result success | executed resume interrupted lineage") {
		t.Fatalf("expected result headline in inspector, got %q", operator)
	}
	if vm.ProofStrip == nil || !strings.Contains(strings.Join(vm.ProofStrip.Lines, "\n"), "next    required finalize continue recovery") {
		t.Fatalf("expected activity strip to surface refreshed next step, got %+v", vm.ProofStrip)
	}
}

func TestBuildViewModelSurfacesPrimaryActionInFlightState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_busy",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "LAUNCH_ACCEPTED_HANDOFF",
				Status:         "REQUIRED_NEXT",
				Domain:         "HANDOFF_CLAUDE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku handoff-launch --task tsk_busy --handoff hnd_1",
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_busy"},
		PrimaryActionInFlight: &PrimaryActionInFlightSummary{
			Action: "LAUNCH_ACCEPTED_HANDOFF",
		},
	}, NewTranscriptHost(), 120, 32)

	if !strings.Contains(vm.Footer, "executing launch accepted handoff...") {
		t.Fatalf("expected busy cue in footer, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "execute next step") {
		t.Fatalf("expected direct execute cue to hide while busy, got %q", vm.Footer)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "progress executing launch accepted handoff...") {
		t.Fatalf("expected busy progress in status overlay, got %q", status)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	operator := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(operator, "progress executing launch accepted handoff...") {
		t.Fatalf("expected busy progress in operator inspector, got %q", operator)
	}
	if vm.ProofStrip == nil || !strings.Contains(strings.Join(vm.ProofStrip.Lines, "\n"), "progress executing launch accepted handoff...") {
		t.Fatalf("expected busy progress in activity strip, got %+v", vm.ProofStrip)
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

func TestBuildViewModelSurfacesDurableOperatorReceiptHistory(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID:                    "tsk_receipt",
		Phase:                     "BRIEF_READY",
		Status:                    "ACTIVE",
		LatestOperatorStepReceipt: &OperatorStepReceiptSummary{ReceiptID: "orec_latest", ActionHandle: "FINALIZE_CONTINUE_RECOVERY", ResultClass: "SUCCEEDED", Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
		RecentOperatorStepReceipts: []OperatorStepReceiptSummary{
			{ReceiptID: "orec_latest", ActionHandle: "FINALIZE_CONTINUE_RECOVERY", ResultClass: "SUCCEEDED", Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
			{ReceiptID: "orec_prev", ActionHandle: "RESUME_INTERRUPTED_LINEAGE", ResultClass: "SUCCEEDED", Summary: "resumed interrupted lineage", CreatedAt: time.Unix(1710000000, 0).UTC()},
		},
	}, UIState{ShowInspector: true, ShowProof: true, Session: SessionState{SessionID: "shs_receipt"}}, host, 120, 32)

	if vm.Inspector == nil || vm.ProofStrip == nil {
		t.Fatalf("expected inspector and activity strip, got %+v %+v", vm.Inspector, vm.ProofStrip)
	}
	foundReceipt := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "operator" {
			continue
		}
		joined := strings.Join(section.Lines, "\n")
		if strings.Contains(joined, "receipt succeeded | finalized continue recovery") {
			foundReceipt = true
		}
	}
	if !foundReceipt {
		t.Fatalf("expected inspector receipt line, got %#v", vm.Inspector.Sections)
	}
	joinedActivity := strings.Join(vm.ProofStrip.Lines, "\n")
	if !strings.Contains(joinedActivity, "operator succeeded finalized continue recovery") || !strings.Contains(joinedActivity, "operator succeeded resumed interrupted lineage") {
		t.Fatalf("expected operator receipt history in activity strip, got %q", joinedActivity)
	}
}
```

