package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/conversation"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/policy"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/repoindex"
	"tuku/internal/domain/run"
	"tuku/internal/domain/taskmemory"
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
	return &contextPackRepo{q: s.db}
}

func (s *Store) RepoIndexes() storage.RepoIndexStore {
	return &repoIndexRepo{q: s.db}
}

func (s *Store) TaskMemories() storage.TaskMemoryStore {
	return &taskMemoryRepo{q: s.db}
}

func (s *Store) Benchmarks() storage.BenchmarkStore {
	return &benchmarkRepo{q: s.db}
}

func (s *Store) PolicyDecisions() storage.PolicyDecisionStore {
	return &policyDecisionRepo{q: s.db}
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
	return &contextPackRepo{q: s.tx}
}

func (s *txStore) RepoIndexes() storage.RepoIndexStore {
	return &repoIndexRepo{q: s.tx}
}

func (s *txStore) TaskMemories() storage.TaskMemoryStore {
	return &taskMemoryRepo{q: s.tx}
}

func (s *txStore) Benchmarks() storage.BenchmarkStore {
	return &benchmarkRepo{q: s.tx}
}

func (s *txStore) PolicyDecisions() storage.PolicyDecisionStore {
	return &policyDecisionRepo{q: s.tx}
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
	prompt_triage_json TEXT NOT NULL DEFAULT '{}',
	context_pack_id TEXT NOT NULL,
	task_memory_id TEXT NOT NULL DEFAULT '',
	memory_compression_json TEXT NOT NULL DEFAULT '{}',
	prompt_ir_json TEXT NOT NULL DEFAULT '{}',
	benchmark_id TEXT NOT NULL DEFAULT '',
	verbosity TEXT NOT NULL,
	policy_profile_id TEXT NOT NULL,
	brief_hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_execution_briefs_task_created
	ON execution_briefs(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS benchmark_runs (
	benchmark_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	brief_id TEXT,
	run_id TEXT,
	version INTEGER NOT NULL,
	source TEXT NOT NULL DEFAULT '',
	raw_prompt_token_estimate INTEGER NOT NULL DEFAULT 0,
	dispatch_prompt_token_estimate INTEGER NOT NULL DEFAULT 0,
	structured_prompt_token_estimate INTEGER NOT NULL DEFAULT 0,
	selected_context_token_estimate INTEGER NOT NULL DEFAULT 0,
	estimated_token_savings INTEGER NOT NULL DEFAULT 0,
	files_scanned INTEGER NOT NULL DEFAULT 0,
	ranked_target_count INTEGER NOT NULL DEFAULT 0,
	candidate_recall_at_3 REAL NOT NULL DEFAULT 0,
	structured_cheaper INTEGER NOT NULL DEFAULT 0,
	default_serializer TEXT NOT NULL DEFAULT '',
	confidence_value REAL NOT NULL DEFAULT 0,
	confidence_level TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	changed_files_json TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_benchmark_runs_task_created
	ON benchmark_runs(task_id, created_at DESC, benchmark_id DESC);

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
	repo_diff_summary TEXT,
	worktree_summary TEXT,
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

CREATE TABLE IF NOT EXISTS context_packs (
	context_pack_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	mode TEXT NOT NULL,
	token_budget INTEGER NOT NULL,
	repo_anchor_hash TEXT NOT NULL,
	freshness_state TEXT NOT NULL,
	included_files_json TEXT NOT NULL,
	included_snippets_json TEXT NOT NULL,
	selection_rationale_json TEXT NOT NULL,
	pack_hash TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_context_packs_task_created
	ON context_packs(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS task_memory_snapshots (
	memory_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	version INTEGER NOT NULL,
	brief_id TEXT,
	run_id TEXT,
	phase TEXT NOT NULL,
	source TEXT NOT NULL,
	summary TEXT NOT NULL,
	confirmed_facts_json TEXT NOT NULL,
	rejected_hypotheses_json TEXT NOT NULL,
	unknowns_json TEXT NOT NULL,
	user_constraints_json TEXT NOT NULL,
	touched_files_json TEXT NOT NULL,
	validators_run_json TEXT NOT NULL,
	candidate_files_json TEXT NOT NULL,
	last_blocker TEXT,
	next_suggested_step TEXT NOT NULL,
	full_history_token_estimate INTEGER NOT NULL,
	resume_prompt_token_estimate INTEGER NOT NULL,
	memory_compaction_ratio REAL NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_memory_snapshots_task_created
	ON task_memory_snapshots(task_id, created_at DESC, memory_id DESC);

CREATE TABLE IF NOT EXISTS policy_decisions (
	decision_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	operation_type TEXT NOT NULL,
	risk_level TEXT NOT NULL,
	requested_at TEXT NOT NULL,
	resolved_at TEXT,
	resolved_by TEXT,
	status TEXT NOT NULL,
	reason TEXT,
	scope_descriptor TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_policy_decisions_task_requested
	ON policy_decisions(task_id, requested_at DESC);
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
	if err := ensureContextPackSchema(db); err != nil {
		return err
	}
	if err := ensureRepoIndexSchema(db); err != nil {
		return err
	}
	if err := ensureTaskMemorySchema(db); err != nil {
		return err
	}
	if err := ensureBenchmarkSchema(db); err != nil {
		return err
	}
	if err := ensurePolicyDecisionSchema(db); err != nil {
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
		{Name: "repo_diff_summary", DDL: "ALTER TABLE execution_runs ADD COLUMN repo_diff_summary TEXT"},
		{Name: "worktree_summary", DDL: "ALTER TABLE execution_runs ADD COLUMN worktree_summary TEXT"},
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
		{Name: "prompt_triage_json", DDL: "ALTER TABLE execution_briefs ADD COLUMN prompt_triage_json TEXT NOT NULL DEFAULT '{}'"},
		{Name: "task_memory_id", DDL: "ALTER TABLE execution_briefs ADD COLUMN task_memory_id TEXT NOT NULL DEFAULT ''"},
		{Name: "memory_compression_json", DDL: "ALTER TABLE execution_briefs ADD COLUMN memory_compression_json TEXT NOT NULL DEFAULT '{}'"},
		{Name: "prompt_ir_json", DDL: "ALTER TABLE execution_briefs ADD COLUMN prompt_ir_json TEXT NOT NULL DEFAULT '{}'"},
		{Name: "benchmark_id", DDL: "ALTER TABLE execution_briefs ADD COLUMN benchmark_id TEXT NOT NULL DEFAULT ''"},
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

func ensureContextPackSchema(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "task_id", DDL: "ALTER TABLE context_packs ADD COLUMN task_id TEXT NOT NULL DEFAULT ''"},
		{Name: "mode", DDL: "ALTER TABLE context_packs ADD COLUMN mode TEXT NOT NULL DEFAULT 'compact'"},
		{Name: "token_budget", DDL: "ALTER TABLE context_packs ADD COLUMN token_budget INTEGER NOT NULL DEFAULT 0"},
		{Name: "repo_anchor_hash", DDL: "ALTER TABLE context_packs ADD COLUMN repo_anchor_hash TEXT NOT NULL DEFAULT ''"},
		{Name: "freshness_state", DDL: "ALTER TABLE context_packs ADD COLUMN freshness_state TEXT NOT NULL DEFAULT ''"},
		{Name: "included_files_json", DDL: "ALTER TABLE context_packs ADD COLUMN included_files_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "included_snippets_json", DDL: "ALTER TABLE context_packs ADD COLUMN included_snippets_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "selection_rationale_json", DDL: "ALTER TABLE context_packs ADD COLUMN selection_rationale_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "pack_hash", DDL: "ALTER TABLE context_packs ADD COLUMN pack_hash TEXT NOT NULL DEFAULT ''"},
		{Name: "created_at", DDL: "ALTER TABLE context_packs ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "context_packs", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add context_packs column %s: %w", item.Name, err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_context_packs_task_created ON context_packs(task_id, created_at DESC)`); err != nil {
		return fmt.Errorf("create idx_context_packs_task_created: %w", err)
	}
	return nil
}

func ensureRepoIndexSchema(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS repo_indexes (
	repo_index_id TEXT PRIMARY KEY,
	repo_root TEXT NOT NULL,
	head_sha TEXT NOT NULL,
	file_count INTEGER NOT NULL DEFAULT 0,
	symbol_count INTEGER NOT NULL DEFAULT 0,
	route_count INTEGER NOT NULL DEFAULT 0,
	component_count INTEGER NOT NULL DEFAULT 0,
	test_count INTEGER NOT NULL DEFAULT 0,
	total_token_estimate INTEGER NOT NULL DEFAULT 0,
	files_json TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL DEFAULT ''
)`); err != nil {
		return fmt.Errorf("ensure repo_indexes table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_repo_indexes_repo_head ON repo_indexes(repo_root, head_sha, created_at DESC)`); err != nil {
		return fmt.Errorf("create idx_repo_indexes_repo_head: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_repo_indexes_repo_created ON repo_indexes(repo_root, created_at DESC)`); err != nil {
		return fmt.Errorf("create idx_repo_indexes_repo_created: %w", err)
	}
	return nil
}

func ensureTaskMemorySchema(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "version", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN version INTEGER NOT NULL DEFAULT 1"},
		{Name: "brief_id", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN brief_id TEXT"},
		{Name: "run_id", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN run_id TEXT"},
		{Name: "phase", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN phase TEXT NOT NULL DEFAULT ''"},
		{Name: "source", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN source TEXT NOT NULL DEFAULT ''"},
		{Name: "summary", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN summary TEXT NOT NULL DEFAULT ''"},
		{Name: "confirmed_facts_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN confirmed_facts_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "rejected_hypotheses_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN rejected_hypotheses_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "unknowns_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN unknowns_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "user_constraints_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN user_constraints_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "touched_files_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN touched_files_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "validators_run_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN validators_run_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "candidate_files_json", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN candidate_files_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "last_blocker", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN last_blocker TEXT"},
		{Name: "next_suggested_step", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN next_suggested_step TEXT NOT NULL DEFAULT ''"},
		{Name: "full_history_token_estimate", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN full_history_token_estimate INTEGER NOT NULL DEFAULT 0"},
		{Name: "resume_prompt_token_estimate", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN resume_prompt_token_estimate INTEGER NOT NULL DEFAULT 0"},
		{Name: "memory_compaction_ratio", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN memory_compaction_ratio REAL NOT NULL DEFAULT 0"},
		{Name: "created_at", DDL: "ALTER TABLE task_memory_snapshots ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "task_memory_snapshots", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add task_memory_snapshots column %s: %w", item.Name, err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_task_memory_snapshots_task_created ON task_memory_snapshots(task_id, created_at DESC, memory_id DESC)`); err != nil {
		return fmt.Errorf("create idx_task_memory_snapshots_task_created: %w", err)
	}
	return nil
}

func ensureBenchmarkSchema(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "task_id", DDL: "ALTER TABLE benchmark_runs ADD COLUMN task_id TEXT NOT NULL DEFAULT ''"},
		{Name: "brief_id", DDL: "ALTER TABLE benchmark_runs ADD COLUMN brief_id TEXT"},
		{Name: "run_id", DDL: "ALTER TABLE benchmark_runs ADD COLUMN run_id TEXT"},
		{Name: "version", DDL: "ALTER TABLE benchmark_runs ADD COLUMN version INTEGER NOT NULL DEFAULT 1"},
		{Name: "source", DDL: "ALTER TABLE benchmark_runs ADD COLUMN source TEXT NOT NULL DEFAULT ''"},
		{Name: "raw_prompt_token_estimate", DDL: "ALTER TABLE benchmark_runs ADD COLUMN raw_prompt_token_estimate INTEGER NOT NULL DEFAULT 0"},
		{Name: "dispatch_prompt_token_estimate", DDL: "ALTER TABLE benchmark_runs ADD COLUMN dispatch_prompt_token_estimate INTEGER NOT NULL DEFAULT 0"},
		{Name: "structured_prompt_token_estimate", DDL: "ALTER TABLE benchmark_runs ADD COLUMN structured_prompt_token_estimate INTEGER NOT NULL DEFAULT 0"},
		{Name: "selected_context_token_estimate", DDL: "ALTER TABLE benchmark_runs ADD COLUMN selected_context_token_estimate INTEGER NOT NULL DEFAULT 0"},
		{Name: "estimated_token_savings", DDL: "ALTER TABLE benchmark_runs ADD COLUMN estimated_token_savings INTEGER NOT NULL DEFAULT 0"},
		{Name: "files_scanned", DDL: "ALTER TABLE benchmark_runs ADD COLUMN files_scanned INTEGER NOT NULL DEFAULT 0"},
		{Name: "ranked_target_count", DDL: "ALTER TABLE benchmark_runs ADD COLUMN ranked_target_count INTEGER NOT NULL DEFAULT 0"},
		{Name: "candidate_recall_at_3", DDL: "ALTER TABLE benchmark_runs ADD COLUMN candidate_recall_at_3 REAL NOT NULL DEFAULT 0"},
		{Name: "structured_cheaper", DDL: "ALTER TABLE benchmark_runs ADD COLUMN structured_cheaper INTEGER NOT NULL DEFAULT 0"},
		{Name: "default_serializer", DDL: "ALTER TABLE benchmark_runs ADD COLUMN default_serializer TEXT NOT NULL DEFAULT ''"},
		{Name: "confidence_value", DDL: "ALTER TABLE benchmark_runs ADD COLUMN confidence_value REAL NOT NULL DEFAULT 0"},
		{Name: "confidence_level", DDL: "ALTER TABLE benchmark_runs ADD COLUMN confidence_level TEXT NOT NULL DEFAULT ''"},
		{Name: "summary", DDL: "ALTER TABLE benchmark_runs ADD COLUMN summary TEXT NOT NULL DEFAULT ''"},
		{Name: "changed_files_json", DDL: "ALTER TABLE benchmark_runs ADD COLUMN changed_files_json TEXT NOT NULL DEFAULT '[]'"},
		{Name: "created_at", DDL: "ALTER TABLE benchmark_runs ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		{Name: "updated_at", DDL: "ALTER TABLE benchmark_runs ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "benchmark_runs", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add benchmark_runs column %s: %w", item.Name, err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_benchmark_runs_task_created ON benchmark_runs(task_id, created_at DESC, benchmark_id DESC)`); err != nil {
		return fmt.Errorf("create idx_benchmark_runs_task_created: %w", err)
	}
	return nil
}

func ensurePolicyDecisionSchema(db *sql.DB) error {
	type colDef struct {
		Name string
		DDL  string
	}
	needed := []colDef{
		{Name: "task_id", DDL: "ALTER TABLE policy_decisions ADD COLUMN task_id TEXT NOT NULL DEFAULT ''"},
		{Name: "operation_type", DDL: "ALTER TABLE policy_decisions ADD COLUMN operation_type TEXT NOT NULL DEFAULT ''"},
		{Name: "risk_level", DDL: "ALTER TABLE policy_decisions ADD COLUMN risk_level TEXT NOT NULL DEFAULT 'LOW'"},
		{Name: "requested_at", DDL: "ALTER TABLE policy_decisions ADD COLUMN requested_at TEXT NOT NULL DEFAULT ''"},
		{Name: "resolved_at", DDL: "ALTER TABLE policy_decisions ADD COLUMN resolved_at TEXT"},
		{Name: "resolved_by", DDL: "ALTER TABLE policy_decisions ADD COLUMN resolved_by TEXT"},
		{Name: "status", DDL: "ALTER TABLE policy_decisions ADD COLUMN status TEXT NOT NULL DEFAULT 'PENDING'"},
		{Name: "reason", DDL: "ALTER TABLE policy_decisions ADD COLUMN reason TEXT"},
		{Name: "scope_descriptor", DDL: "ALTER TABLE policy_decisions ADD COLUMN scope_descriptor TEXT NOT NULL DEFAULT ''"},
	}
	for _, item := range needed {
		ok, err := hasColumn(db, "policy_decisions", item.Name)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if _, err := db.Exec(item.DDL); err != nil {
			return fmt.Errorf("add policy_decisions column %s: %w", item.Name, err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_policy_decisions_task_requested ON policy_decisions(task_id, requested_at DESC)`); err != nil {
		return fmt.Errorf("create idx_policy_decisions_task_requested: %w", err)
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

type contextPackRepo struct{ q queryable }

type repoIndexRepo struct{ q queryable }

type taskMemoryRepo struct{ q queryable }

type benchmarkRepo struct{ q queryable }

type policyDecisionRepo struct{ q queryable }

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
	promptTriageJSON, err := marshalPromptTriage(b.PromptTriage)
	if err != nil {
		return err
	}
	memoryCompressionJSON, err := marshalMemoryCompression(b.MemoryCompression)
	if err != nil {
		return err
	}
	promptIRJSON, err := marshalPromptIR(b.PromptIR)
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
	prompt_triage_json, context_pack_id, task_memory_id, memory_compression_json, prompt_ir_json, benchmark_id, verbosity, policy_profile_id, brief_hash
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(b.BriefID), string(b.TaskID), string(b.IntentID), int64(b.CapsuleVersion), b.Version, b.CreatedAt.Format(sqliteTimestampLayout),
		string(b.Posture), b.Objective, b.RequestedOutcome, b.NormalizedAction, b.ScopeSummary, scopeInJSON, scopeOutJSON, constraintsJSON, doneJSON,
		ambiguityJSON, clarificationQuestionsJSON, requiresClarification, b.WorkerFraming, b.BoundedEvidenceMessages,
		promptTriageJSON, string(b.ContextPackID), string(b.TaskMemoryID), memoryCompressionJSON, promptIRJSON, string(b.BenchmarkID), string(b.Verbosity), b.PolicyProfileID, b.BriefHash,
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
	prompt_triage_json, context_pack_id, task_memory_id, memory_compression_json, prompt_ir_json, benchmark_id, verbosity, policy_profile_id, brief_hash
FROM execution_briefs WHERE brief_id = ?
`, string(briefID))
	return scanBrief(row)
}

func (r *briefRepo) LatestByTask(taskID common.TaskID) (brief.ExecutionBrief, error) {
	row := r.q.QueryRow(`
SELECT brief_id, task_id, intent_id, capsule_version, version, created_at, posture, objective,
	requested_outcome, normalized_action, scope_summary, scope_in_json, scope_out_json, constraints_json, done_criteria_json,
	ambiguity_flags_json, clarification_questions_json, requires_clarification, worker_framing, bounded_evidence_messages,
	prompt_triage_json, context_pack_id, task_memory_id, memory_compression_json, prompt_ir_json, benchmark_id, verbosity, policy_profile_id, brief_hash
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
	stdout, stderr, changed_files_json, changed_files_semantics, repo_diff_summary, worktree_summary, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(execRun.RunID), string(execRun.TaskID), string(execRun.BriefID), string(execRun.WorkerKind),
		nilIfString(execRun.WorkerRunID), nilIfString(execRun.ShellSessionID), string(execRun.Status),
		nilIfString(execRun.Command), argsJSON, nilIfInt(execRun.ExitCode),
		nilIfString(execRun.Stdout), nilIfString(execRun.Stderr), changedFilesJSON, nilIfString(execRun.ChangedFilesSemantics), nilIfString(execRun.RepoDiffSummary), nilIfString(execRun.WorktreeSummary), validationSignalsJSON, nilIfString(execRun.OutputArtifactRef), nilIfString(execRun.StructuredSummary),
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
	stdout, stderr, changed_files_json, changed_files_semantics, repo_diff_summary, worktree_summary, validation_signals_json, output_artifact_ref, structured_summary_json,
	started_at, ended_at, interruption_reason, created_from_phase, last_known_summary, created_at, updated_at
FROM execution_runs
WHERE run_id = ?
`, string(runID))
	return scanRun(row)
}

func (r *runRepo) LatestByTask(taskID common.TaskID) (run.ExecutionRun, error) {
	row := r.q.QueryRow(`
SELECT run_id, task_id, brief_id, worker_kind, worker_run_id, shell_session_id, status, command, args_json, exit_code,
	stdout, stderr, changed_files_json, changed_files_semantics, repo_diff_summary, worktree_summary, validation_signals_json, output_artifact_ref, structured_summary_json,
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
	stdout, stderr, changed_files_json, changed_files_semantics, repo_diff_summary, worktree_summary, validation_signals_json, output_artifact_ref, structured_summary_json,
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
	stdout, stderr, changed_files_json, changed_files_semantics, repo_diff_summary, worktree_summary, validation_signals_json, output_artifact_ref, structured_summary_json,
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
	stdout=?, stderr=?, changed_files_json=?, changed_files_semantics=?, repo_diff_summary=?, worktree_summary=?, validation_signals_json=?, output_artifact_ref=?, structured_summary_json=?,
	started_at=?, ended_at=?, interruption_reason=?, created_from_phase=?, last_known_summary=?, created_at=?, updated_at=?
WHERE run_id = ?
`,
		string(execRun.TaskID), string(execRun.BriefID), string(execRun.WorkerKind), nilIfString(execRun.WorkerRunID), nilIfString(execRun.ShellSessionID), string(execRun.Status),
		nilIfString(execRun.Command), argsJSON, nilIfInt(execRun.ExitCode),
		nilIfString(execRun.Stdout), nilIfString(execRun.Stderr), changedFilesJSON, nilIfString(execRun.ChangedFilesSemantics), nilIfString(execRun.RepoDiffSummary), nilIfString(execRun.WorktreeSummary), validationSignalsJSON, nilIfString(execRun.OutputArtifactRef), nilIfString(execRun.StructuredSummary),
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

func (r *contextPackRepo) Save(pack contextdomain.Pack) error {
	includedFilesJSON, err := marshalStringSlice(pack.IncludedFiles)
	if err != nil {
		return err
	}
	includedSnippetsJSON, err := marshalSnippets(pack.IncludedSnippets)
	if err != nil {
		return err
	}
	selectionRationaleJSON, err := marshalStringSlice(pack.SelectionRationale)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT OR REPLACE INTO context_packs(
	context_pack_id, task_id, mode, token_budget, repo_anchor_hash, freshness_state,
	included_files_json, included_snippets_json, selection_rationale_json, pack_hash, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
`,
		string(pack.ContextPackID), string(pack.TaskID), string(pack.Mode), pack.TokenBudget, pack.RepoAnchorHash, pack.FreshnessState,
		includedFilesJSON, includedSnippetsJSON, selectionRationaleJSON, pack.PackHash, pack.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("save context pack: %w", err)
	}
	return nil
}

func (r *contextPackRepo) Get(id common.ContextPackID) (contextdomain.Pack, error) {
	row := r.q.QueryRow(`
SELECT context_pack_id, task_id, mode, token_budget, repo_anchor_hash, freshness_state,
	included_files_json, included_snippets_json, selection_rationale_json, pack_hash, created_at
FROM context_packs
WHERE context_pack_id = ?
`, string(id))

	var pack contextdomain.Pack
	var mode string
	var includedFilesJSON string
	var includedSnippetsJSON string
	var selectionRationaleJSON string
	var createdAt string
	err := row.Scan(
		&pack.ContextPackID, &pack.TaskID, &mode, &pack.TokenBudget, &pack.RepoAnchorHash, &pack.FreshnessState,
		&includedFilesJSON, &includedSnippetsJSON, &selectionRationaleJSON, &pack.PackHash, &createdAt,
	)
	if err != nil {
		return contextdomain.Pack{}, err
	}
	pack.Mode = contextdomain.Mode(mode)
	pack.IncludedFiles, err = unmarshalStringSlice(includedFilesJSON)
	if err != nil {
		return contextdomain.Pack{}, err
	}
	pack.IncludedSnippets, err = unmarshalSnippets(includedSnippetsJSON)
	if err != nil {
		return contextdomain.Pack{}, err
	}
	pack.SelectionRationale, err = unmarshalStringSlice(selectionRationaleJSON)
	if err != nil {
		return contextdomain.Pack{}, err
	}
	pack.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return contextdomain.Pack{}, err
	}
	return pack, nil
}

func (r *repoIndexRepo) Save(snapshot repoindex.Snapshot) error {
	filesJSON, err := marshalRepoIndexFiles(snapshot.Files)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT OR REPLACE INTO repo_indexes(
	repo_index_id, repo_root, head_sha, file_count, symbol_count, route_count, component_count, test_count,
	total_token_estimate, files_json, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?)
`,
		string(snapshot.RepoIndexID), snapshot.RepoRoot, snapshot.HeadSHA, snapshot.FileCount, snapshot.SymbolCount, snapshot.RouteCount,
		snapshot.ComponentCount, snapshot.TestCount, snapshot.TotalTokenEstimate, filesJSON, snapshot.BuiltAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("save repo index: %w", err)
	}
	return nil
}

func (r *repoIndexRepo) Get(repoIndexID common.RepoIndexID) (repoindex.Snapshot, error) {
	row := r.q.QueryRow(`
SELECT repo_index_id, repo_root, head_sha, file_count, symbol_count, route_count, component_count, test_count,
	total_token_estimate, files_json, created_at
FROM repo_indexes
WHERE repo_index_id = ?
`, string(repoIndexID))
	return scanRepoIndex(row)
}

func (r *repoIndexRepo) GetByRepoHead(repoRoot string, headSHA string) (repoindex.Snapshot, error) {
	row := r.q.QueryRow(`
SELECT repo_index_id, repo_root, head_sha, file_count, symbol_count, route_count, component_count, test_count,
	total_token_estimate, files_json, created_at
FROM repo_indexes
WHERE repo_root = ? AND head_sha = ?
ORDER BY created_at DESC, repo_index_id DESC
LIMIT 1
`, filepath.Clean(strings.TrimSpace(repoRoot)), strings.TrimSpace(headSHA))
	return scanRepoIndex(row)
}

func (r *repoIndexRepo) LatestByRepo(repoRoot string) (repoindex.Snapshot, error) {
	row := r.q.QueryRow(`
SELECT repo_index_id, repo_root, head_sha, file_count, symbol_count, route_count, component_count, test_count,
	total_token_estimate, files_json, created_at
FROM repo_indexes
WHERE repo_root = ?
ORDER BY created_at DESC, repo_index_id DESC
LIMIT 1
`, filepath.Clean(strings.TrimSpace(repoRoot)))
	return scanRepoIndex(row)
}

func (r *taskMemoryRepo) Save(snapshot taskmemory.Snapshot) error {
	confirmedFactsJSON, err := marshalStringSlice(snapshot.ConfirmedFacts)
	if err != nil {
		return err
	}
	rejectedHypothesesJSON, err := marshalStringSlice(snapshot.RejectedHypotheses)
	if err != nil {
		return err
	}
	unknownsJSON, err := marshalStringSlice(snapshot.Unknowns)
	if err != nil {
		return err
	}
	userConstraintsJSON, err := marshalStringSlice(snapshot.UserConstraints)
	if err != nil {
		return err
	}
	touchedFilesJSON, err := marshalStringSlice(snapshot.TouchedFiles)
	if err != nil {
		return err
	}
	validatorsRunJSON, err := marshalStringSlice(snapshot.ValidatorsRun)
	if err != nil {
		return err
	}
	candidateFilesJSON, err := marshalStringSlice(snapshot.CandidateFiles)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(`
INSERT OR REPLACE INTO task_memory_snapshots(
	memory_id, task_id, version, brief_id, run_id, phase, source, summary, confirmed_facts_json, rejected_hypotheses_json,
	unknowns_json, user_constraints_json, touched_files_json, validators_run_json, candidate_files_json,
	last_blocker, next_suggested_step, full_history_token_estimate, resume_prompt_token_estimate, memory_compaction_ratio, created_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(snapshot.MemoryID), string(snapshot.TaskID), snapshot.Version, nilIfString(string(snapshot.BriefID)), nilIfString(string(snapshot.RunID)),
		string(snapshot.Phase), snapshot.Source, snapshot.Summary, confirmedFactsJSON, rejectedHypothesesJSON,
		unknownsJSON, userConstraintsJSON, touchedFilesJSON, validatorsRunJSON, candidateFilesJSON,
		nilIfString(snapshot.LastBlocker), snapshot.NextSuggestedStep, snapshot.FullHistoryTokenEstimate, snapshot.ResumePromptTokenEstimate,
		snapshot.MemoryCompactionRatio, snapshot.CreatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("save task memory snapshot: %w", err)
	}
	return nil
}

func (r *taskMemoryRepo) Get(memoryID common.MemoryID) (taskmemory.Snapshot, error) {
	row := r.q.QueryRow(`
SELECT memory_id, task_id, version, brief_id, run_id, phase, source, summary, confirmed_facts_json, rejected_hypotheses_json,
	unknowns_json, user_constraints_json, touched_files_json, validators_run_json, candidate_files_json,
	last_blocker, next_suggested_step, full_history_token_estimate, resume_prompt_token_estimate, memory_compaction_ratio, created_at
FROM task_memory_snapshots
WHERE memory_id = ?
`, string(memoryID))
	return scanTaskMemory(row)
}

func (r *taskMemoryRepo) LatestByTask(taskID common.TaskID) (taskmemory.Snapshot, error) {
	row := r.q.QueryRow(`
SELECT memory_id, task_id, version, brief_id, run_id, phase, source, summary, confirmed_facts_json, rejected_hypotheses_json,
	unknowns_json, user_constraints_json, touched_files_json, validators_run_json, candidate_files_json,
	last_blocker, next_suggested_step, full_history_token_estimate, resume_prompt_token_estimate, memory_compaction_ratio, created_at
FROM task_memory_snapshots
WHERE task_id = ?
ORDER BY created_at DESC, memory_id DESC
LIMIT 1
`, string(taskID))
	return scanTaskMemory(row)
}

func (r *benchmarkRepo) Save(runRec benchmark.Run) error {
	changedFilesJSON, err := marshalStringSlice(runRec.ChangedFiles)
	if err != nil {
		return err
	}
	structuredCheaper := 0
	if runRec.StructuredCheaper {
		structuredCheaper = 1
	}
	_, err = r.q.Exec(`
INSERT OR REPLACE INTO benchmark_runs(
	benchmark_id, task_id, brief_id, run_id, version, source, raw_prompt_token_estimate, dispatch_prompt_token_estimate,
	structured_prompt_token_estimate, selected_context_token_estimate, estimated_token_savings, files_scanned, ranked_target_count,
	candidate_recall_at_3, structured_cheaper, default_serializer, confidence_value, confidence_level, summary, changed_files_json,
	created_at, updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`,
		string(runRec.BenchmarkID), string(runRec.TaskID), nilIfString(string(runRec.BriefID)), nilIfString(string(runRec.RunID)), runRec.Version, runRec.Source,
		runRec.RawPromptTokenEstimate, runRec.DispatchPromptTokenEstimate, runRec.StructuredPromptTokenEstimate, runRec.SelectedContextTokenEstimate,
		runRec.EstimatedTokenSavings, runRec.FilesScanned, runRec.RankedTargetCount, runRec.CandidateRecallAt3, structuredCheaper, runRec.DefaultSerializer,
		runRec.ConfidenceValue, runRec.ConfidenceLevel, runRec.Summary, changedFilesJSON, runRec.CreatedAt.Format(sqliteTimestampLayout), runRec.UpdatedAt.Format(sqliteTimestampLayout),
	)
	if err != nil {
		return fmt.Errorf("save benchmark run: %w", err)
	}
	return nil
}

func (r *benchmarkRepo) Get(benchmarkID common.BenchmarkID) (benchmark.Run, error) {
	row := r.q.QueryRow(`
SELECT benchmark_id, task_id, brief_id, run_id, version, source, raw_prompt_token_estimate, dispatch_prompt_token_estimate,
	structured_prompt_token_estimate, selected_context_token_estimate, estimated_token_savings, files_scanned, ranked_target_count,
	candidate_recall_at_3, structured_cheaper, default_serializer, confidence_value, confidence_level, summary, changed_files_json,
	created_at, updated_at
FROM benchmark_runs
WHERE benchmark_id = ?
`, string(benchmarkID))
	return scanBenchmark(row)
}

func (r *benchmarkRepo) LatestByTask(taskID common.TaskID) (benchmark.Run, error) {
	row := r.q.QueryRow(`
SELECT benchmark_id, task_id, brief_id, run_id, version, source, raw_prompt_token_estimate, dispatch_prompt_token_estimate,
	structured_prompt_token_estimate, selected_context_token_estimate, estimated_token_savings, files_scanned, ranked_target_count,
	candidate_recall_at_3, structured_cheaper, default_serializer, confidence_value, confidence_level, summary, changed_files_json,
	created_at, updated_at
FROM benchmark_runs
WHERE task_id = ?
ORDER BY created_at DESC, benchmark_id DESC
LIMIT 1
`, string(taskID))
	return scanBenchmark(row)
}

func (r *policyDecisionRepo) Save(decision policy.Decision) error {
	_, err := r.q.Exec(`
INSERT OR REPLACE INTO policy_decisions(
	decision_id, task_id, operation_type, risk_level, requested_at, resolved_at, resolved_by, status, reason, scope_descriptor
) VALUES(?,?,?,?,?,?,?,?,?,?)
`,
		string(decision.DecisionID), string(decision.TaskID), decision.OperationType, string(decision.RiskLevel),
		decision.RequestedAt.Format(sqliteTimestampLayout), nilIfTime(decision.ResolvedAt), nilIfString(decision.ResolvedBy),
		string(decision.Status), nilIfString(decision.Reason), decision.ScopeDescriptor,
	)
	if err != nil {
		return fmt.Errorf("save policy decision: %w", err)
	}
	return nil
}

func (r *policyDecisionRepo) Get(decisionID common.DecisionID) (policy.Decision, error) {
	row := r.q.QueryRow(`
SELECT decision_id, task_id, operation_type, risk_level, requested_at, resolved_at, resolved_by, status, reason, scope_descriptor
FROM policy_decisions
WHERE decision_id = ?
`, string(decisionID))

	var decision policy.Decision
	var riskLevel string
	var requestedAt string
	var resolvedAt sql.NullString
	var resolvedBy sql.NullString
	var status string
	var reason sql.NullString
	err := row.Scan(
		&decision.DecisionID, &decision.TaskID, &decision.OperationType, &riskLevel, &requestedAt, &resolvedAt, &resolvedBy, &status, &reason, &decision.ScopeDescriptor,
	)
	if err != nil {
		return policy.Decision{}, err
	}
	decision.RiskLevel = policy.RiskLevel(riskLevel)
	decision.RequestedAt, err = time.Parse(sqliteTimestampLayout, requestedAt)
	if err != nil {
		return policy.Decision{}, err
	}
	if resolvedAt.Valid && resolvedAt.String != "" {
		parsed, err := time.Parse(sqliteTimestampLayout, resolvedAt.String)
		if err != nil {
			return policy.Decision{}, err
		}
		decision.ResolvedAt = &parsed
	}
	if resolvedBy.Valid {
		decision.ResolvedBy = resolvedBy.String
	}
	decision.Status = policy.DecisionStatus(status)
	if reason.Valid {
		decision.Reason = reason.String
	}
	return decision, nil
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

func marshalPromptTriage(value brief.PromptTriage) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal prompt triage: %w", err)
	}
	return string(raw), nil
}

func unmarshalPromptTriage(value string) (brief.PromptTriage, error) {
	if strings.TrimSpace(value) == "" {
		return brief.PromptTriage{}, nil
	}
	var out brief.PromptTriage
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return brief.PromptTriage{}, fmt.Errorf("unmarshal prompt triage: %w", err)
	}
	return out, nil
}

func marshalMemoryCompression(value brief.MemoryCompression) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal memory compression: %w", err)
	}
	return string(raw), nil
}

func unmarshalMemoryCompression(value string) (brief.MemoryCompression, error) {
	if strings.TrimSpace(value) == "" {
		return brief.MemoryCompression{}, nil
	}
	var out brief.MemoryCompression
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return brief.MemoryCompression{}, fmt.Errorf("unmarshal memory compression: %w", err)
	}
	return out, nil
}

func marshalPromptIR(value promptir.Packet) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal prompt ir: %w", err)
	}
	return string(raw), nil
}

func unmarshalPromptIR(value string) (promptir.Packet, error) {
	if strings.TrimSpace(value) == "" {
		return promptir.Packet{}, nil
	}
	var out promptir.Packet
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return promptir.Packet{}, fmt.Errorf("unmarshal prompt ir: %w", err)
	}
	return out, nil
}

func marshalRepoIndexFiles(value []repoindex.File) (string, error) {
	if value == nil {
		value = []repoindex.File{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal repo index files: %w", err)
	}
	return string(raw), nil
}

func unmarshalRepoIndexFiles(value string) ([]repoindex.File, error) {
	if strings.TrimSpace(value) == "" {
		return []repoindex.File{}, nil
	}
	var out []repoindex.File
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return nil, fmt.Errorf("unmarshal repo index files: %w", err)
	}
	return out, nil
}

func scanRepoIndex(row *sql.Row) (repoindex.Snapshot, error) {
	var out repoindex.Snapshot
	var filesJSON string
	var createdAt string
	err := row.Scan(
		&out.RepoIndexID, &out.RepoRoot, &out.HeadSHA, &out.FileCount, &out.SymbolCount, &out.RouteCount,
		&out.ComponentCount, &out.TestCount, &out.TotalTokenEstimate, &filesJSON, &createdAt,
	)
	if err != nil {
		return repoindex.Snapshot{}, err
	}
	out.Files, err = unmarshalRepoIndexFiles(filesJSON)
	if err != nil {
		return repoindex.Snapshot{}, err
	}
	out.BuiltAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return repoindex.Snapshot{}, err
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

func marshalSnippets(values []contextdomain.Snippet) (string, error) {
	if values == nil {
		values = []contextdomain.Snippet{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalSnippets(value string) ([]contextdomain.Snippet, error) {
	if value == "" {
		return []contextdomain.Snippet{}, nil
	}
	var out []contextdomain.Snippet
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
	var promptTriageJSON string
	var memoryCompressionJSON string
	var promptIRJSON string
	var requiresClarificationInt int
	var taskMemoryID sql.NullString
	var benchmarkID sql.NullString
	err := row.Scan(
		&b.BriefID, &b.TaskID, &b.IntentID, &b.CapsuleVersion, &b.Version, &createdAt, &posture, &b.Objective,
		&b.RequestedOutcome, &b.NormalizedAction, &b.ScopeSummary, &scopeInJSON, &scopeOutJSON, &constraintsJSON, &doneJSON,
		&ambiguityJSON, &clarificationQuestionsJSON, &requiresClarificationInt, &b.WorkerFraming, &b.BoundedEvidenceMessages,
		&promptTriageJSON, &b.ContextPackID, &taskMemoryID, &memoryCompressionJSON, &promptIRJSON, &benchmarkID, &b.Verbosity, &b.PolicyProfileID, &b.BriefHash,
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
	b.PromptTriage, err = unmarshalPromptTriage(promptTriageJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	if taskMemoryID.Valid {
		b.TaskMemoryID = common.MemoryID(taskMemoryID.String)
	}
	b.MemoryCompression, err = unmarshalMemoryCompression(memoryCompressionJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	b.PromptIR, err = unmarshalPromptIR(promptIRJSON)
	if err != nil {
		return brief.ExecutionBrief{}, err
	}
	if benchmarkID.Valid {
		b.BenchmarkID = common.BenchmarkID(benchmarkID.String)
	}
	b.RequiresClarification = requiresClarificationInt == 1
	return b, nil
}

func scanBenchmark(row *sql.Row) (benchmark.Run, error) {
	var out benchmark.Run
	var briefID sql.NullString
	var runID sql.NullString
	var changedFilesJSON string
	var structuredCheaperInt int
	var createdAt string
	var updatedAt string
	err := row.Scan(
		&out.BenchmarkID, &out.TaskID, &briefID, &runID, &out.Version, &out.Source, &out.RawPromptTokenEstimate,
		&out.DispatchPromptTokenEstimate, &out.StructuredPromptTokenEstimate, &out.SelectedContextTokenEstimate,
		&out.EstimatedTokenSavings, &out.FilesScanned, &out.RankedTargetCount, &out.CandidateRecallAt3,
		&structuredCheaperInt, &out.DefaultSerializer, &out.ConfidenceValue, &out.ConfidenceLevel, &out.Summary, &changedFilesJSON,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return benchmark.Run{}, err
	}
	if briefID.Valid {
		out.BriefID = common.BriefID(briefID.String)
	}
	if runID.Valid {
		out.RunID = common.RunID(runID.String)
	}
	out.StructuredCheaper = structuredCheaperInt == 1
	out.ChangedFiles, err = unmarshalStringSlice(changedFilesJSON)
	if err != nil {
		return benchmark.Run{}, err
	}
	out.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return benchmark.Run{}, err
	}
	out.UpdatedAt, err = time.Parse(sqliteTimestampLayout, updatedAt)
	if err != nil {
		return benchmark.Run{}, err
	}
	return out, nil
}

func scanTaskMemory(row *sql.Row) (taskmemory.Snapshot, error) {
	var snapshot taskmemory.Snapshot
	var phaseValue string
	var briefID sql.NullString
	var runID sql.NullString
	var confirmedFactsJSON, rejectedHypothesesJSON, unknownsJSON string
	var userConstraintsJSON, touchedFilesJSON, validatorsRunJSON, candidateFilesJSON string
	var lastBlocker sql.NullString
	var createdAt string
	err := row.Scan(
		&snapshot.MemoryID, &snapshot.TaskID, &snapshot.Version, &briefID, &runID, &phaseValue, &snapshot.Source, &snapshot.Summary,
		&confirmedFactsJSON, &rejectedHypothesesJSON, &unknownsJSON, &userConstraintsJSON, &touchedFilesJSON,
		&validatorsRunJSON, &candidateFilesJSON, &lastBlocker, &snapshot.NextSuggestedStep,
		&snapshot.FullHistoryTokenEstimate, &snapshot.ResumePromptTokenEstimate, &snapshot.MemoryCompactionRatio, &createdAt,
	)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	if briefID.Valid {
		snapshot.BriefID = common.BriefID(briefID.String)
	}
	if runID.Valid {
		snapshot.RunID = common.RunID(runID.String)
	}
	snapshot.Phase = phase.Phase(phaseValue)
	snapshot.ConfirmedFacts, err = unmarshalStringSlice(confirmedFactsJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.RejectedHypotheses, err = unmarshalStringSlice(rejectedHypothesesJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.Unknowns, err = unmarshalStringSlice(unknownsJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.UserConstraints, err = unmarshalStringSlice(userConstraintsJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.TouchedFiles, err = unmarshalStringSlice(touchedFilesJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.ValidatorsRun, err = unmarshalStringSlice(validatorsRunJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.CandidateFiles, err = unmarshalStringSlice(candidateFilesJSON)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	if lastBlocker.Valid {
		snapshot.LastBlocker = lastBlocker.String
	}
	snapshot.CreatedAt, err = time.Parse(sqliteTimestampLayout, createdAt)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	return snapshot, nil
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
	var repoDiffSummary sql.NullString
	var worktreeSummary sql.NullString
	var validationSignalsJSON sql.NullString
	var outputArtifactRef sql.NullString
	var structuredSummary sql.NullString
	var interruption sql.NullString
	var summary sql.NullString
	err := row.Scan(
		&r.RunID, &r.TaskID, &r.BriefID, &r.WorkerKind, &workerRunID, &shellSessionID, &r.Status, &command, &argsJSON, &exitCode,
		&stdout, &stderr, &changedFilesJSON, &changedFilesSemantics, &repoDiffSummary, &worktreeSummary, &validationSignalsJSON, &outputArtifactRef, &structuredSummary,
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
	if repoDiffSummary.Valid {
		r.RepoDiffSummary = repoDiffSummary.String
	}
	if worktreeSummary.Valid {
		r.WorktreeSummary = worktreeSummary.String
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
