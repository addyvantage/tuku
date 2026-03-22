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
