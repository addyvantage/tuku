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

func (s *Store) TransitionReceipts() storage.TransitionReceiptStore {
	return &transitionReceiptRepo{q: s.db}
}

func (s *Store) IncidentTriages() storage.IncidentTriageStore {
	return &incidentTriageRepo{q: s.db}
}

func (s *Store) IncidentFollowUps() storage.IncidentFollowUpStore {
	return &incidentFollowUpRepo{q: s.db}
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

func (s *txStore) TransitionReceipts() storage.TransitionReceiptStore {
	return &transitionReceiptRepo{q: s.tx}
}

func (s *txStore) IncidentTriages() storage.IncidentTriageStore {
	return &incidentTriageRepo{q: s.tx}
}

func (s *txStore) IncidentFollowUps() storage.IncidentFollowUpStore {
	return &incidentFollowUpRepo{q: s.tx}
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
	posture TEXT NOT NULL DEFAULT 'CLARIFICATION_NEEDED',
	execution_readiness TEXT NOT NULL DEFAULT 'CLARIFICATION_NEEDED',
	objective TEXT NOT NULL DEFAULT '',
	requested_outcome TEXT NOT NULL DEFAULT '',
	normalized_action TEXT NOT NULL,
	scope_summary TEXT NOT NULL DEFAULT '',
	explicit_constraints_json TEXT NOT NULL DEFAULT '[]',
	done_criteria_json TEXT NOT NULL DEFAULT '[]',
	confidence REAL NOT NULL,
	ambiguity_flags_json TEXT NOT NULL,
	clarification_questions_json TEXT NOT NULL DEFAULT '[]',
	requires_clarification INTEGER NOT NULL,
	readiness_reason TEXT NOT NULL DEFAULT '',
	compilation_notes TEXT NOT NULL DEFAULT '',
	bounded_evidence_messages INTEGER NOT NULL DEFAULT 0,
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
	posture TEXT NOT NULL DEFAULT 'CLARIFICATION_NEEDED',
	objective TEXT NOT NULL,
	requested_outcome TEXT NOT NULL DEFAULT '',
	normalized_action TEXT NOT NULL,
	scope_summary TEXT NOT NULL DEFAULT '',
	scope_in_json TEXT NOT NULL,
	scope_out_json TEXT NOT NULL,
	constraints_json TEXT NOT NULL,
	done_criteria_json TEXT NOT NULL,
	ambiguity_flags_json TEXT NOT NULL DEFAULT '[]',
	clarification_questions_json TEXT NOT NULL DEFAULT '[]',
	requires_clarification INTEGER NOT NULL DEFAULT 0,
	worker_framing TEXT NOT NULL DEFAULT '',
	bounded_evidence_messages INTEGER NOT NULL DEFAULT 0,
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
	worker_run_id TEXT,
	shell_session_id TEXT,
	status TEXT NOT NULL,
	command TEXT,
	args_json TEXT,
	exit_code INTEGER,
	stdout TEXT,
	stderr TEXT,
	changed_files_json TEXT,
	changed_files_semantics TEXT,
	validation_signals_json TEXT,
	output_artifact_ref TEXT,
	structured_summary_json TEXT,
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
	if err := ensureIntentStateColumns(db); err != nil {
		return err
	}
	if err := ensureExecutionBriefColumns(db); err != nil {
		return err
	}
	if err := ensureExecutionRunColumns(db); err != nil {
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
	if err := ensureTransitionReceiptSchema(db); err != nil {
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

func ensureExecutionRunColumns(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "worker_run_id", DDL: "ALTER TABLE execution_runs ADD COLUMN worker_run_id TEXT"},
		{Name: "shell_session_id", DDL: "ALTER TABLE execution_runs ADD COLUMN shell_session_id TEXT"},
		{Name: "command", DDL: "ALTER TABLE execution_runs ADD COLUMN command TEXT"},
		{Name: "args_json", DDL: "ALTER TABLE execution_runs ADD COLUMN args_json TEXT"},
		{Name: "exit_code", DDL: "ALTER TABLE execution_runs ADD COLUMN exit_code INTEGER"},
		{Name: "stdout", DDL: "ALTER TABLE execution_runs ADD COLUMN stdout TEXT"},
		{Name: "stderr", DDL: "ALTER TABLE execution_runs ADD COLUMN stderr TEXT"},
		{Name: "changed_files_json", DDL: "ALTER TABLE execution_runs ADD COLUMN changed_files_json TEXT"},
		{Name: "changed_files_semantics", DDL: "ALTER TABLE execution_runs ADD COLUMN changed_files_semantics TEXT"},
		{Name: "validation_signals_json", DDL: "ALTER TABLE execution_runs ADD COLUMN validation_signals_json TEXT"},
		{Name: "output_artifact_ref", DDL: "ALTER TABLE execution_runs ADD COLUMN output_artifact_ref TEXT"},
		{Name: "structured_summary_json", DDL: "ALTER TABLE execution_runs ADD COLUMN structured_summary_json TEXT"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "execution_runs", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add execution_runs column %s: %w", item.Name, err)
		}
	}
	return nil
}

func ensureIntentStateColumns(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "posture", DDL: "ALTER TABLE intent_states ADD COLUMN posture TEXT NOT NULL DEFAULT 'CLARIFICATION_NEEDED'"},
		{Name: "execution_readiness", DDL: "ALTER TABLE intent_states ADD COLUMN execution_readiness TEXT NOT NULL DEFAULT 'CLARIFICATION_NEEDED'"},
		{Name: "objective", DDL: "ALTER TABLE intent_states ADD COLUMN objective TEXT NOT NULL DEFAULT ''"},
		{Name: "requested_outcome", DDL: "ALTER TABLE intent_states ADD COLUMN requested_outcome TEXT NOT NULL DEFAULT ''"},
		{Name: "scope_summary", DDL: "ALTER TABLE intent_states ADD COLUMN scope_summary TEXT NOT NULL DEFAULT ''"},
		{Name: "explicit_constraints_json", DDL: "ALTER TABLE intent_states ADD COLUMN explicit_constraints_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "done_criteria_json", DDL: "ALTER TABLE intent_states ADD COLUMN done_criteria_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "clarification_questions_json", DDL: "ALTER TABLE intent_states ADD COLUMN clarification_questions_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "readiness_reason", DDL: "ALTER TABLE intent_states ADD COLUMN readiness_reason TEXT NOT NULL DEFAULT ''"},
		{Name: "compilation_notes", DDL: "ALTER TABLE intent_states ADD COLUMN compilation_notes TEXT NOT NULL DEFAULT ''"},
		{Name: "bounded_evidence_messages", DDL: "ALTER TABLE intent_states ADD COLUMN bounded_evidence_messages INTEGER NOT NULL DEFAULT 0"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "intent_states", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add intent_states column %s: %w", item.Name, err)
		}
	}
	return nil
}

func ensureExecutionBriefColumns(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "posture", DDL: "ALTER TABLE execution_briefs ADD COLUMN posture TEXT NOT NULL DEFAULT 'CLARIFICATION_NEEDED'"},
		{Name: "requested_outcome", DDL: "ALTER TABLE execution_briefs ADD COLUMN requested_outcome TEXT NOT NULL DEFAULT ''"},
		{Name: "scope_summary", DDL: "ALTER TABLE execution_briefs ADD COLUMN scope_summary TEXT NOT NULL DEFAULT ''"},
		{Name: "ambiguity_flags_json", DDL: "ALTER TABLE execution_briefs ADD COLUMN ambiguity_flags_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "clarification_questions_json", DDL: "ALTER TABLE execution_briefs ADD COLUMN clarification_questions_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "requires_clarification", DDL: "ALTER TABLE execution_briefs ADD COLUMN requires_clarification INTEGER NOT NULL DEFAULT 0"},
		{Name: "worker_framing", DDL: "ALTER TABLE execution_briefs ADD COLUMN worker_framing TEXT NOT NULL DEFAULT ''"},
		{Name: "bounded_evidence_messages", DDL: "ALTER TABLE execution_briefs ADD COLUMN bounded_evidence_messages INTEGER NOT NULL DEFAULT 0"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "execution_briefs", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add execution_briefs column %s: %w", item.Name, err)
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
	explicitConstraintsJSON, err := marshalStringSlice(state.ExplicitConstraints)
	if err != nil {
		return err
	}
	doneCriteriaJSON, err := marshalStringSlice(state.DoneCriteria)
	if err != nil {
		return err
	}
	clarificationQuestionsJSON, err := marshalStringSlice(state.ClarificationQuestions)
	if err != nil {
		return err
	}
	sourceJSON, err := marshalMessageSlice(state.SourceMessageIDs)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO intent_states(
	intent_id, task_id, version, class, posture, execution_readiness,
	objective, requested_outcome, normalized_action, scope_summary,
	explicit_constraints_json, done_criteria_json, confidence, ambiguity_flags_json,
	clarification_questions_json, requires_clarification, readiness_reason, compilation_notes,
	bounded_evidence_messages, source_message_ids_json, proposed_phase, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(state.IntentID), string(state.TaskID), state.Version, string(state.Class), string(state.Posture), string(state.ExecutionReadiness),
		state.Objective, state.RequestedOutcome, state.NormalizedAction, state.ScopeSummary,
		explicitConstraintsJSON, doneCriteriaJSON, state.Confidence, ambJSON,
		clarificationQuestionsJSON, boolToInt(state.RequiresClarification), state.ReadinessReason, state.CompilationNotes,
		state.BoundedEvidenceMessages, sourceJSON,
		string(state.ProposedPhase), state.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert intent state: %w", err)
	}
	return nil
}

func (r *intentRepo) LatestByTask(taskID common.TaskID) (intent.State, error) {
	row := r.q.QueryRow(`
SELECT intent_id, task_id, version, class, posture, execution_readiness,
	objective, requested_outcome, normalized_action, scope_summary,
	explicit_constraints_json, done_criteria_json, confidence, ambiguity_flags_json,
	clarification_questions_json, requires_clarification, readiness_reason, compilation_notes,
	bounded_evidence_messages, source_message_ids_json, proposed_phase, created_at
FROM intent_states
WHERE task_id = ?
ORDER BY created_at DESC
LIMIT 1
`, string(taskID))

	var st intent.State
	var explicitConstraintsJSON, doneCriteriaJSON string
	var ambJSON, clarificationQuestionsJSON string
	var sourceJSON, proposedPhase, ts string
	var posture, readiness string
	var requiresInt int
	err := row.Scan(
		&st.IntentID, &st.TaskID, &st.Version, &st.Class, &posture, &readiness,
		&st.Objective, &st.RequestedOutcome, &st.NormalizedAction, &st.ScopeSummary,
		&explicitConstraintsJSON, &doneCriteriaJSON, &st.Confidence, &ambJSON,
		&clarificationQuestionsJSON, &requiresInt, &st.ReadinessReason, &st.CompilationNotes,
		&st.BoundedEvidenceMessages, &sourceJSON,
		&proposedPhase, &ts,
	)
	if err != nil {
		return intent.State{}, err
	}
	st.Posture = intent.Posture(posture)
	st.ExecutionReadiness = intent.Readiness(readiness)
	st.ExplicitConstraints, err = unmarshalStringSlice(explicitConstraintsJSON)
	if err != nil {
		return intent.State{}, err
	}
	st.DoneCriteria, err = unmarshalStringSlice(doneCriteriaJSON)
	if err != nil {
		return intent.State{}, err
	}
	st.AmbiguityFlags, err = unmarshalStringSlice(ambJSON)
	if err != nil {
		return intent.State{}, err
	}
	st.ClarificationQuestions, err = unmarshalStringSlice(clarificationQuestionsJSON)
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
	ambiguityJSON, err := marshalStringSlice(b.AmbiguityFlags)
	if err != nil {
		return err
	}
	clarificationQuestionsJSON, err := marshalStringSlice(b.ClarificationQuestions)
	if err != nil {
		return err
	}
	requiresClarification := 0
	if b.RequiresClarification {
		requiresClarification = 1
	}
	_, err = r.q.Exec(`
INSERT INTO execution_briefs(
	brief_id, task_id, intent_id, capsule_version, version, created_at, posture, objective,
	requested_outcome, normalized_action, scope_summary, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	ambiguity_flags_json, clarification_questions_json, requires_clarification, worker_framing, bounded_evidence_messages,
	context_pack_id, verbosity, policy_profile_id, brief_hash
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(b.BriefID), string(b.TaskID), string(b.IntentID), int64(b.CapsuleVersion), b.Version, b.CreatedAt.Format(sqliteTimestampLayout),
		string(b.Posture), b.Objective, b.RequestedOutcome, b.NormalizedAction, b.ScopeSummary, scopeInJSON, scopeOutJSON, constraintsJSON, doneJSON,
		ambiguityJSON, clarificationQuestionsJSON, requiresClarification, b.WorkerFraming, b.BoundedEvidenceMessages,
		string(b.ContextPackID), string(b.Verbosity), b.PolicyProfileID, b.BriefHash,
	)
	if err != nil {
		return fmt.Errorf("insert execution brief: %w", err)
	}
	return nil
}

func (r *briefRepo) Get(briefID common.BriefID) (brief.ExecutionBrief, error) {
	row := r.q.QueryRow(`
SELECT brief_id, task_id, intent_id, capsule_version, version, created_at, posture, objective,
	requested_outcome, normalized_action, scope_summary, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	ambiguity_flags_json, clarification_questions_json, requires_clarification, worker_framing, bounded_evidence_messages,
	context_pack_id, verbosity, policy_profile_id, brief_hash
FROM execution_briefs WHERE brief_id = ?
`, string(briefID))
	return scanBrief(row)
}

func (r *briefRepo) LatestByTask(taskID common.TaskID) (brief.ExecutionBrief, error) {
	row := r.q.QueryRow(`
SELECT brief_id, task_id, intent_id, capsule_version, version, created_at, posture, objective,
	requested_outcome, normalized_action, scope_summary, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	ambiguity_flags_json, clarification_questions_json, requires_clarification, worker_framing, bounded_evidence_messages,
	context_pack_id, verbosity, policy_profile_id, brief_hash
FROM execution_briefs WHERE task_id = ?
ORDER BY created_at DESC
LIMIT 1
`, string(taskID))
	return scanBrief(row)
}

func (r *runRepo) Create(execRun run.ExecutionRun) error {
	argsJSON, err := marshalStringSlice(execRun.Args)
	if err != nil {
		return err
	}
	changedFilesJSON, err := marshalStringSlice(execRun.ChangedFiles)
	if err != nil {
		return err
	}
	validationSignalsJSON, err := marshalStringSlice(execRun.ValidationSignals)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT INTO execution_runs(
	run_id, task_id, brief_id, worker_kind, worker_run_id, shell_session_id, status, command, args_json, exit_code,
	stdout, stderr, changed_files_json, changed_files_semantics, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(execRun.RunID), string(execRun.TaskID), string(execRun.BriefID), string(execRun.WorkerKind),
		nilIfString(execRun.WorkerRunID), nilIfString(execRun.ShellSessionID), string(execRun.Status),
		nilIfString(execRun.Command), argsJSON, nilIfInt(execRun.ExitCode),
		nilIfString(execRun.Stdout), nilIfString(execRun.Stderr), changedFilesJSON, nilIfString(execRun.ChangedFilesSemantics), validationSignalsJSON, nilIfString(execRun.OutputArtifactRef), nilIfString(execRun.StructuredSummary),
		execRun.StartedAt.Format(sqliteTimestampLayout), nilIfTime(execRun.EndedAt), nilIfString(execRun.InterruptionReason), string(execRun.CreatedFromPhase), nilIfString(execRun.LastKnownSummary),
		execRun.CreatedAt.Format(sqliteTimestampLayout), execRun.UpdatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("insert execution run: %w", err)
	}
	return nil
}

func (r *runRepo) Get(runID common.RunID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, worker_run_id, shell_session_id, status, command, args_json, exit_code,
	stdout, stderr, changed_files_json, changed_files_semantics, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE run_id = ?
`, string(runID))
	return scanRun(row)
}

func (r *runRepo) LatestByTask(taskID common.TaskID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, worker_run_id, shell_session_id, status, command, args_json, exit_code,
	stdout, stderr, changed_files_json, changed_files_semantics, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE task_id = ?
ORDER BY created_at DESC, run_id DESC
LIMIT 1
`, string(taskID))
	return scanRun(row)
}

func (r *runRepo) ListByTask(taskID common.TaskID, limit int) ([]run.ExecutionRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.q.Query(`
SELECT run_id, task_id, brief_id, worker_kind, worker_run_id, shell_session_id, status, command, args_json, exit_code,
	stdout, stderr, changed_files_json, changed_files_semantics, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE task_id = ?
ORDER BY created_at DESC, run_id DESC
LIMIT ?
`, string(taskID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]run.ExecutionRun, 0, limit)
	for rows.Next() {
		record, err := scanRunScannable(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *runRepo) LatestRunningByTask(taskID common.TaskID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, worker_run_id, shell_session_id, status, command, args_json, exit_code,
	stdout, stderr, changed_files_json, changed_files_semantics, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE task_id = ? AND status = ?
ORDER BY updated_at DESC
LIMIT 1
`, string(taskID), string(run.StatusRunning))
	return scanRun(row)
}

func (r *runRepo) Update(execRun run.ExecutionRun) error {
	argsJSON, err := marshalStringSlice(execRun.Args)
	if err != nil {
		return err
	}
	changedFilesJSON, err := marshalStringSlice(execRun.ChangedFiles)
	if err != nil {
		return err
	}
	validationSignalsJSON, err := marshalStringSlice(execRun.ValidationSignals)
	if err != nil {
		return err
	}
	res, err := r.q.Exec(`
UPDATE execution_runs SET
	task_id=?, brief_id=?, worker_kind=?, worker_run_id=?, shell_session_id=?, status=?, command=?, args_json=?, exit_code=?,
	stdout=?, stderr=?, changed_files_json=?, changed_files_semantics=?, validation_signals_json=?, output_artifact_ref=?, structured_summary_json=?,
	started_at=?, ended_at=?, interruption_reason=?, created_from_phase=?, last_known_summary=?, created_at=?, updated_at=?
WHERE run_id = ?
`,
		string(execRun.TaskID), string(execRun.BriefID), string(execRun.WorkerKind), nilIfString(execRun.WorkerRunID), nilIfString(execRun.ShellSessionID), string(execRun.Status),
		nilIfString(execRun.Command), argsJSON, nilIfInt(execRun.ExitCode),
		nilIfString(execRun.Stdout), nilIfString(execRun.Stderr), changedFilesJSON, nilIfString(execRun.ChangedFilesSemantics), validationSignalsJSON, nilIfString(execRun.OutputArtifactRef), nilIfString(execRun.StructuredSummary),
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

func nilIfInt(value *int) any {
	if value == nil {
		return nil
	}
	return int64(*value)
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
	var posture string
	var scopeInJSON, scopeOutJSON, constraintsJSON, doneJSON string
	var ambiguityJSON, clarificationQuestionsJSON string
	var requiresClarificationInt int
	err := row.Scan(
		&b.BriefID, &b.TaskID, &b.IntentID, &b.CapsuleVersion, &b.Version, &createdAt, &posture, &b.Objective,
		&b.RequestedOutcome, &b.NormalizedAction, &b.ScopeSummary, &scopeInJSON, &scopeOutJSON, &constraintsJSON, &doneJSON,
		&ambiguityJSON, &clarificationQuestionsJSON, &requiresClarificationInt, &b.WorkerFraming, &b.BoundedEvidenceMessages,
		&b.ContextPackID, &b.Verbosity, &b.PolicyProfileID, &b.BriefHash,
	)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.Posture = brief.Posture(posture)
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
	b.AmbiguityFlags, err = unmarshalStringSlice(ambiguityJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.ClarificationQuestions, err = unmarshalStringSlice(clarificationQuestionsJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.RequiresClarification = requiresClarificationInt == 1
	return b, nil
}

type sqlScannable interface {
	Scan(dest ...any) error
}

func scanRun(row *sql.Row) (run.ExecutionRun, error) {
	return scanRunScannable(row)
}

func scanRunScannable(row sqlScannable) (run.ExecutionRun, error) {
	var r run.ExecutionRun
	var startedAt, createdAt, updatedAt string
	var endedAt sql.NullString
	var workerRunID sql.NullString
	var shellSessionID sql.NullString
	var command sql.NullString
	var argsJSON sql.NullString
	var exitCode sql.NullInt64
	var stdout sql.NullString
	var stderr sql.NullString
	var changedFilesJSON sql.NullString
	var changedFilesSemantics sql.NullString
	var validationSignalsJSON sql.NullString
	var outputArtifactRef sql.NullString
	var structuredSummary sql.NullString
	var interruption sql.NullString
	var summary sql.NullString
	err := row.Scan(
		&r.RunID, &r.TaskID, &r.BriefID, &r.WorkerKind, &workerRunID, &shellSessionID, &r.Status, &command, &argsJSON, &exitCode,
		&stdout, &stderr, &changedFilesJSON, &changedFilesSemantics, &validationSignalsJSON, &outputArtifactRef, &structuredSummary,
		&startedAt, &endedAt, &interruption, &r.CreatedFromPhase, &summary, &createdAt, &updatedAt,
	)
	if err != nil {
		return run.ExecutionRun{}, err
	}
	if workerRunID.Valid {
		r.WorkerRunID = workerRunID.String
	}
	if shellSessionID.Valid {
		r.ShellSessionID = shellSessionID.String
	}
	if command.Valid {
		r.Command = command.String
	}
	if argsJSON.Valid {
		r.Args, err = unmarshalStringSlice(argsJSON.String)
		if err != nil {
			return run.ExecutionRun{}, err
		}
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		r.ExitCode = &code
	}
	if stdout.Valid {
		r.Stdout = stdout.String
	}
	if stderr.Valid {
		r.Stderr = stderr.String
	}
	if changedFilesJSON.Valid {
		r.ChangedFiles, err = unmarshalStringSlice(changedFilesJSON.String)
		if err != nil {
			return run.ExecutionRun{}, err
		}
	}
	if changedFilesSemantics.Valid {
		r.ChangedFilesSemantics = changedFilesSemantics.String
	}
	if validationSignalsJSON.Valid {
		r.ValidationSignals, err = unmarshalStringSlice(validationSignalsJSON.String)
		if err != nil {
			return run.ExecutionRun{}, err
		}
	}
	if outputArtifactRef.Valid {
		r.OutputArtifactRef = outputArtifactRef.String
	}
	if structuredSummary.Valid {
		r.StructuredSummary = structuredSummary.String
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
