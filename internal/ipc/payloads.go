package ipc

import (
	"time"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/run"
	"tuku/internal/domain/taskmemory"
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

type TaskCompiledIntentSummary struct {
	IntentID                common.IntentID `json:"intent_id,omitempty"`
	Class                   string          `json:"class,omitempty"`
	Posture                 string          `json:"posture,omitempty"`
	ExecutionReadiness      string          `json:"execution_readiness,omitempty"`
	Objective               string          `json:"objective,omitempty"`
	RequestedOutcome        string          `json:"requested_outcome,omitempty"`
	NormalizedAction        string          `json:"normalized_action,omitempty"`
	ScopeSummary            string          `json:"scope_summary,omitempty"`
	ExplicitConstraints     []string        `json:"explicit_constraints,omitempty"`
	DoneCriteria            []string        `json:"done_criteria,omitempty"`
	AmbiguityFlags          []string        `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions  []string        `json:"clarification_questions,omitempty"`
	RequiresClarification   bool            `json:"requires_clarification,omitempty"`
	BoundedEvidenceMessages int             `json:"bounded_evidence_messages,omitempty"`
	ReadinessReason         string          `json:"readiness_reason,omitempty"`
	CompilationNotes        string          `json:"compilation_notes,omitempty"`
	Digest                  string          `json:"digest,omitempty"`
	Advisory                string          `json:"advisory,omitempty"`
	CreatedAtUnixMs         int64           `json:"created_at_unix_ms,omitempty"`
}

type TaskCompiledBriefSummary struct {
	BriefID                 common.BriefID           `json:"brief_id,omitempty"`
	IntentID                common.IntentID          `json:"intent_id,omitempty"`
	Posture                 string                   `json:"posture,omitempty"`
	Objective               string                   `json:"objective,omitempty"`
	RequestedOutcome        string                   `json:"requested_outcome,omitempty"`
	NormalizedAction        string                   `json:"normalized_action,omitempty"`
	ScopeSummary            string                   `json:"scope_summary,omitempty"`
	Constraints             []string                 `json:"constraints,omitempty"`
	DoneCriteria            []string                 `json:"done_criteria,omitempty"`
	AmbiguityFlags          []string                 `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions  []string                 `json:"clarification_questions,omitempty"`
	RequiresClarification   bool                     `json:"requires_clarification,omitempty"`
	WorkerFraming           string                   `json:"worker_framing,omitempty"`
	BoundedEvidenceMessages int                      `json:"bounded_evidence_messages,omitempty"`
	PromptTriage            *brief.PromptTriage      `json:"prompt_triage,omitempty"`
	MemoryCompression       *brief.MemoryCompression `json:"memory_compression,omitempty"`
	PromptIR                *promptir.Packet         `json:"prompt_ir,omitempty"`
	Digest                  string                   `json:"digest,omitempty"`
	Advisory                string                   `json:"advisory,omitempty"`
	CreatedAtUnixMs         int64                    `json:"created_at_unix_ms,omitempty"`
}

type TaskIntentRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskIntentResponse struct {
	TaskID          common.TaskID              `json:"task_id"`
	CurrentIntentID common.IntentID            `json:"current_intent_id,omitempty"`
	Bounded         bool                       `json:"bounded"`
	Intent          *intent.State              `json:"intent,omitempty"`
	CompiledIntent  *TaskCompiledIntentSummary `json:"compiled_intent,omitempty"`
}

type TaskBriefRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskBriefResponse struct {
	TaskID         common.TaskID             `json:"task_id"`
	CurrentBriefID common.BriefID            `json:"current_brief_id,omitempty"`
	Bounded        bool                      `json:"bounded"`
	Brief          *brief.ExecutionBrief     `json:"brief,omitempty"`
	CompiledBrief  *TaskCompiledBriefSummary `json:"compiled_brief,omitempty"`
}

type TaskBenchmarkRequest struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskBenchmarkResponse struct {
	TaskID        common.TaskID             `json:"task_id"`
	Benchmark     *benchmark.Run            `json:"benchmark,omitempty"`
	Brief         *brief.ExecutionBrief     `json:"brief,omitempty"`
	CompiledBrief *TaskCompiledBriefSummary `json:"compiled_brief,omitempty"`
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
	TaskID                                      common.TaskID                                `json:"task_id"`
	ConversationID                              common.ConversationID                        `json:"conversation_id"`
	Goal                                        string                                       `json:"goal"`
	Phase                                       phase.Phase                                  `json:"phase"`
	Status                                      string                                       `json:"status"`
	CurrentIntentID                             common.IntentID                              `json:"current_intent_id"`
	CurrentIntentClass                          string                                       `json:"current_intent_class,omitempty"`
	CurrentIntentSummary                        string                                       `json:"current_intent_summary,omitempty"`
	CompiledIntent                              *TaskCompiledIntentSummary                   `json:"compiled_intent,omitempty"`
	CurrentBriefID                              common.BriefID                               `json:"current_brief_id,omitempty"`
	CurrentBriefHash                            string                                       `json:"current_brief_hash,omitempty"`
	CompiledBrief                               *TaskCompiledBriefSummary                    `json:"compiled_brief,omitempty"`
	LatestRunID                                 common.RunID                                 `json:"latest_run_id,omitempty"`
	LatestRunStatus                             run.Status                                   `json:"latest_run_status,omitempty"`
	LatestRunSummary                            string                                       `json:"latest_run_summary,omitempty"`
	LatestRunWorkerRunID                        string                                       `json:"latest_run_worker_run_id,omitempty"`
	LatestRunShellSessionID                     string                                       `json:"latest_run_shell_session_id,omitempty"`
	LatestRunCommand                            string                                       `json:"latest_run_command,omitempty"`
	LatestRunArgs                               []string                                     `json:"latest_run_args,omitempty"`
	LatestRunExitCode                           *int                                         `json:"latest_run_exit_code,omitempty"`
	LatestRunChangedFiles                       []string                                     `json:"latest_run_changed_files,omitempty"`
	LatestRunChangedFilesSemantics              string                                       `json:"latest_run_changed_files_semantics,omitempty"`
	LatestRunRepoDiffSummary                    string                                       `json:"latest_run_repo_diff_summary,omitempty"`
	LatestRunWorktreeSummary                    string                                       `json:"latest_run_worktree_summary,omitempty"`
	LatestRunValidationSignals                  []string                                     `json:"latest_run_validation_signals,omitempty"`
	LatestRunOutputArtifactRef                  string                                       `json:"latest_run_output_artifact_ref,omitempty"`
	LatestRunStructuredSummary                  string                                       `json:"latest_run_structured_summary,omitempty"`
	CurrentContextPackID                        common.ContextPackID                         `json:"current_context_pack_id,omitempty"`
	CurrentContextPackMode                      string                                       `json:"current_context_pack_mode,omitempty"`
	CurrentContextPackFileCount                 int                                          `json:"current_context_pack_file_count,omitempty"`
	CurrentContextPackHash                      string                                       `json:"current_context_pack_hash,omitempty"`
	CurrentTaskMemoryID                         common.MemoryID                              `json:"current_task_memory_id,omitempty"`
	CurrentTaskMemorySource                     string                                       `json:"current_task_memory_source,omitempty"`
	CurrentTaskMemorySummary                    string                                       `json:"current_task_memory_summary,omitempty"`
	CurrentTaskMemoryFullHistoryTokens          int                                          `json:"current_task_memory_full_history_tokens,omitempty"`
	CurrentTaskMemoryResumePromptTokens         int                                          `json:"current_task_memory_resume_prompt_tokens,omitempty"`
	CurrentTaskMemoryCompactionRatio            float64                                      `json:"current_task_memory_compaction_ratio,omitempty"`
	CurrentBenchmarkID                          common.BenchmarkID                           `json:"current_benchmark_id,omitempty"`
	CurrentBenchmarkSource                      string                                       `json:"current_benchmark_source,omitempty"`
	CurrentBenchmarkSummary                     string                                       `json:"current_benchmark_summary,omitempty"`
	CurrentBenchmarkRawPromptTokens             int                                          `json:"current_benchmark_raw_prompt_tokens,omitempty"`
	CurrentBenchmarkDispatchPromptTokens        int                                          `json:"current_benchmark_dispatch_prompt_tokens,omitempty"`
	CurrentBenchmarkStructuredPromptTokens      int                                          `json:"current_benchmark_structured_prompt_tokens,omitempty"`
	CurrentBenchmarkSelectedContextTokens       int                                          `json:"current_benchmark_selected_context_tokens,omitempty"`
	CurrentBenchmarkEstimatedTokenSavings       int                                          `json:"current_benchmark_estimated_token_savings,omitempty"`
	CurrentBenchmarkFilesScanned                int                                          `json:"current_benchmark_files_scanned,omitempty"`
	CurrentBenchmarkRankedTargetCount           int                                          `json:"current_benchmark_ranked_target_count,omitempty"`
	CurrentBenchmarkCandidateRecallAt3          float64                                      `json:"current_benchmark_candidate_recall_at_3,omitempty"`
	CurrentBenchmarkDefaultSerializer           string                                       `json:"current_benchmark_default_serializer,omitempty"`
	CurrentBenchmarkStructuredCheaper           bool                                         `json:"current_benchmark_structured_cheaper,omitempty"`
	CurrentBenchmarkConfidenceValue             float64                                      `json:"current_benchmark_confidence_value,omitempty"`
	CurrentBenchmarkConfidenceLevel             string                                       `json:"current_benchmark_confidence_level,omitempty"`
	LatestPolicyDecisionID                      common.DecisionID                            `json:"latest_policy_decision_id,omitempty"`
	LatestPolicyDecisionStatus                  string                                       `json:"latest_policy_decision_status,omitempty"`
	LatestPolicyDecisionRiskLevel               string                                       `json:"latest_policy_decision_risk_level,omitempty"`
	LatestPolicyDecisionReason                  string                                       `json:"latest_policy_decision_reason,omitempty"`
	LatestShellSessionID                        string                                       `json:"latest_shell_session_id,omitempty"`
	LatestShellSessionClass                     string                                       `json:"latest_shell_session_class,omitempty"`
	LatestShellSessionReason                    string                                       `json:"latest_shell_session_reason,omitempty"`
	LatestShellSessionGuidance                  string                                       `json:"latest_shell_session_guidance,omitempty"`
	LatestShellSessionWorkerSessionID           string                                       `json:"latest_shell_session_worker_session_id,omitempty"`
	LatestShellSessionWorkerSessionIDSource     string                                       `json:"latest_shell_session_worker_session_id_source,omitempty"`
	LatestShellTranscriptState                  string                                       `json:"latest_shell_transcript_state,omitempty"`
	LatestShellTranscriptRetainedChunks         int                                          `json:"latest_shell_transcript_retained_chunks,omitempty"`
	LatestShellTranscriptDroppedChunks          int                                          `json:"latest_shell_transcript_dropped_chunks,omitempty"`
	LatestShellTranscriptRetentionLimit         int                                          `json:"latest_shell_transcript_retention_limit,omitempty"`
	LatestShellTranscriptOldestSequence         int64                                        `json:"latest_shell_transcript_oldest_retained_sequence,omitempty"`
	LatestShellTranscriptNewestSequence         int64                                        `json:"latest_shell_transcript_newest_retained_sequence,omitempty"`
	LatestShellTranscriptLastChunkAtUnixMs      int64                                        `json:"latest_shell_transcript_last_chunk_at_unix_ms,omitempty"`
	LatestShellTranscriptReviewID               common.EventID                               `json:"latest_shell_transcript_review_id,omitempty"`
	LatestShellTranscriptReviewSource           string                                       `json:"latest_shell_transcript_review_source,omitempty"`
	LatestShellTranscriptReviewedUpTo           int64                                        `json:"latest_shell_transcript_reviewed_up_to_sequence,omitempty"`
	LatestShellTranscriptReviewSummary          string                                       `json:"latest_shell_transcript_review_summary,omitempty"`
	LatestShellTranscriptReviewAtUnixMs         int64                                        `json:"latest_shell_transcript_review_at_unix_ms,omitempty"`
	LatestShellTranscriptReviewStale            bool                                         `json:"latest_shell_transcript_review_stale,omitempty"`
	LatestShellTranscriptReviewNewer            int                                          `json:"latest_shell_transcript_review_newer_retained_count,omitempty"`
	LatestShellTranscriptReviewClosureState     string                                       `json:"latest_shell_transcript_review_closure_state,omitempty"`
	LatestShellTranscriptReviewOldestUnreviewed int64                                        `json:"latest_shell_transcript_review_oldest_unreviewed_sequence,omitempty"`
	LatestShellSessionState                     string                                       `json:"latest_shell_session_state,omitempty"`
	LatestShellSessionUpdatedAtUnixMs           int64                                        `json:"latest_shell_session_updated_at_unix_ms,omitempty"`
	LatestShellEventID                          common.EventID                               `json:"latest_shell_event_id,omitempty"`
	LatestShellEventKind                        string                                       `json:"latest_shell_event_kind,omitempty"`
	LatestShellEventSessionID                   string                                       `json:"latest_shell_event_session_id,omitempty"`
	LatestShellEventAtUnixMs                    int64                                        `json:"latest_shell_event_at_unix_ms,omitempty"`
	LatestShellEventNote                        string                                       `json:"latest_shell_event_note,omitempty"`
	RepoAnchor                                  RepoAnchor                                   `json:"repo_anchor"`
	LatestCheckpointID                          common.CheckpointID                          `json:"latest_checkpoint_id,omitempty"`
	LatestCheckpointAtUnixMs                    int64                                        `json:"latest_checkpoint_at_unix_ms,omitempty"`
	LatestCheckpointTrigger                     string                                       `json:"latest_checkpoint_trigger,omitempty"`
	CheckpointResumable                         bool                                         `json:"checkpoint_resumable,omitempty"`
	ResumeDescriptor                            string                                       `json:"resume_descriptor,omitempty"`
	LatestLaunchAttemptID                       string                                       `json:"latest_launch_attempt_id,omitempty"`
	LatestLaunchID                              string                                       `json:"latest_launch_id,omitempty"`
	LatestLaunchStatus                          string                                       `json:"latest_launch_status,omitempty"`
	LatestAcknowledgmentID                      string                                       `json:"latest_acknowledgment_id,omitempty"`
	LatestAcknowledgmentStatus                  string                                       `json:"latest_acknowledgment_status,omitempty"`
	LatestAcknowledgmentSummary                 string                                       `json:"latest_acknowledgment_summary,omitempty"`
	LatestFollowThroughID                       string                                       `json:"latest_follow_through_id,omitempty"`
	LatestFollowThroughKind                     string                                       `json:"latest_follow_through_kind,omitempty"`
	LatestFollowThroughSummary                  string                                       `json:"latest_follow_through_summary,omitempty"`
	LatestResolutionID                          string                                       `json:"latest_resolution_id,omitempty"`
	LatestResolutionKind                        string                                       `json:"latest_resolution_kind,omitempty"`
	LatestResolutionSummary                     string                                       `json:"latest_resolution_summary,omitempty"`
	LatestResolutionAtUnixMs                    int64                                        `json:"latest_resolution_at_unix_ms,omitempty"`
	LaunchControlState                          string                                       `json:"launch_control_state,omitempty"`
	LaunchRetryDisposition                      string                                       `json:"launch_retry_disposition,omitempty"`
	LaunchControlReason                         string                                       `json:"launch_control_reason,omitempty"`
	HandoffContinuityState                      string                                       `json:"handoff_continuity_state,omitempty"`
	HandoffContinuityReason                     string                                       `json:"handoff_continuity_reason,omitempty"`
	HandoffContinuationProven                   bool                                         `json:"handoff_continuation_proven"`
	ActiveBranchClass                           string                                       `json:"active_branch_class,omitempty"`
	ActiveBranchRef                             string                                       `json:"active_branch_ref,omitempty"`
	ActiveBranchAnchorKind                      string                                       `json:"active_branch_anchor_kind,omitempty"`
	ActiveBranchAnchorRef                       string                                       `json:"active_branch_anchor_ref,omitempty"`
	ActiveBranchReason                          string                                       `json:"active_branch_reason,omitempty"`
	LocalRunFinalizationState                   string                                       `json:"local_run_finalization_state,omitempty"`
	LocalRunFinalizationRunID                   common.RunID                                 `json:"local_run_finalization_run_id,omitempty"`
	LocalRunFinalizationStatus                  run.Status                                   `json:"local_run_finalization_status,omitempty"`
	LocalRunFinalizationCheckpointID            common.CheckpointID                          `json:"local_run_finalization_checkpoint_id,omitempty"`
	LocalRunFinalizationReason                  string                                       `json:"local_run_finalization_reason,omitempty"`
	LocalResumeAuthorityState                   string                                       `json:"local_resume_authority_state,omitempty"`
	LocalResumeMode                             string                                       `json:"local_resume_mode,omitempty"`
	LocalResumeCheckpointID                     common.CheckpointID                          `json:"local_resume_checkpoint_id,omitempty"`
	LocalResumeRunID                            common.RunID                                 `json:"local_resume_run_id,omitempty"`
	LocalResumeReason                           string                                       `json:"local_resume_reason,omitempty"`
	RequiredNextOperatorAction                  string                                       `json:"required_next_operator_action,omitempty"`
	ActionAuthority                             []TaskOperatorActionAuthority                `json:"action_authority,omitempty"`
	OperatorDecision                            *TaskOperatorDecisionSummary                 `json:"operator_decision,omitempty"`
	OperatorExecutionPlan                       *TaskOperatorExecutionPlan                   `json:"operator_execution_plan,omitempty"`
	LatestOperatorStepReceipt                   *TaskOperatorStepReceipt                     `json:"latest_operator_step_receipt,omitempty"`
	RecentOperatorStepReceipts                  []TaskOperatorStepReceipt                    `json:"recent_operator_step_receipts,omitempty"`
	LatestContinuityTransitionReceipt           *TaskContinuityTransitionReceipt             `json:"latest_continuity_transition_receipt,omitempty"`
	RecentContinuityTransitionReceipts          []TaskContinuityTransitionReceipt            `json:"recent_continuity_transition_receipts,omitempty"`
	ContinuityTransitionRiskSummary             *TaskContinuityTransitionRiskSummary         `json:"continuity_transition_risk_summary,omitempty"`
	ContinuityIncidentSummary                   *TaskContinuityIncidentRiskSummary           `json:"continuity_incident_summary,omitempty"`
	LatestContinuityIncidentTriageReceipt       *TaskContinuityIncidentTriageReceipt         `json:"latest_continuity_incident_triage_receipt,omitempty"`
	RecentContinuityIncidentTriageReceipts      []TaskContinuityIncidentTriageReceipt        `json:"recent_continuity_incident_triage_receipts,omitempty"`
	ContinuityIncidentTriageHistoryRollup       *TaskContinuityIncidentTriageHistoryRollup   `json:"continuity_incident_triage_history_rollup,omitempty"`
	LatestContinuityIncidentFollowUpReceipt     *TaskContinuityIncidentFollowUpReceipt       `json:"latest_continuity_incident_follow_up_receipt,omitempty"`
	RecentContinuityIncidentFollowUpReceipts    []TaskContinuityIncidentFollowUpReceipt      `json:"recent_continuity_incident_follow_up_receipts,omitempty"`
	ContinuityIncidentFollowUpHistoryRollup     *TaskContinuityIncidentFollowUpHistoryRollup `json:"continuity_incident_follow_up_history_rollup,omitempty"`
	ContinuityIncidentFollowUp                  *TaskContinuityIncidentFollowUpSummary       `json:"continuity_incident_follow_up,omitempty"`
	ContinuityIncidentTaskRisk                  *TaskContinuityIncidentTaskRiskSummary       `json:"continuity_incident_task_risk,omitempty"`
	LatestTranscriptReviewGapAcknowledgment     *TaskTranscriptReviewGapAcknowledgment       `json:"latest_transcript_review_gap_acknowledgment,omitempty"`
	RecentTranscriptReviewGapAcknowledgments    []TaskTranscriptReviewGapAcknowledgment      `json:"recent_transcript_review_gap_acknowledgments,omitempty"`
	IsResumable                                 bool                                         `json:"is_resumable,omitempty"`
	RecoveryClass                               string                                       `json:"recovery_class,omitempty"`
	RecommendedAction                           string                                       `json:"recommended_action,omitempty"`
	ReadyForNextRun                             bool                                         `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch                       bool                                         `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason                              string                                       `json:"recovery_reason,omitempty"`
	LatestRecoveryAction                        *TaskRecoveryActionRecord                    `json:"latest_recovery_action,omitempty"`
	LastEventType                               string                                       `json:"last_event_type,omitempty"`
	LastEventID                                 common.EventID                               `json:"last_event_id,omitempty"`
	LastEventAtUnixMs                           int64                                        `json:"last_event_at_unix_ms,omitempty"`
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
	ReceiptID                        string              `json:"receipt_id"`
	TaskID                           common.TaskID       `json:"task_id"`
	ActionHandle                     string              `json:"action_handle"`
	ExecutionDomain                  string              `json:"execution_domain,omitempty"`
	CommandSurfaceKind               string              `json:"command_surface_kind,omitempty"`
	ExecutionAttempted               bool                `json:"execution_attempted"`
	ResultClass                      string              `json:"result_class"`
	Summary                          string              `json:"summary,omitempty"`
	Reason                           string              `json:"reason,omitempty"`
	RunID                            common.RunID        `json:"run_id,omitempty"`
	CheckpointID                     common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID                          common.BriefID      `json:"brief_id,omitempty"`
	HandoffID                        string              `json:"handoff_id,omitempty"`
	LaunchAttemptID                  string              `json:"launch_attempt_id,omitempty"`
	LaunchID                         string              `json:"launch_id,omitempty"`
	ReviewGapState                   string              `json:"review_gap_state,omitempty"`
	ReviewGapSessionID               string              `json:"review_gap_session_id,omitempty"`
	ReviewGapClass                   string              `json:"review_gap_class,omitempty"`
	ReviewGapPresent                 bool                `json:"review_gap_present,omitempty"`
	ReviewGapReviewedUpTo            int64               `json:"review_gap_reviewed_up_to_sequence,omitempty"`
	ReviewGapOldestUnreviewed        int64               `json:"review_gap_oldest_unreviewed_sequence,omitempty"`
	ReviewGapNewestRetained          int64               `json:"review_gap_newest_retained_sequence,omitempty"`
	ReviewGapUnreviewedRetainedCount int                 `json:"review_gap_unreviewed_retained_count,omitempty"`
	ReviewGapAcknowledged            bool                `json:"review_gap_acknowledged,omitempty"`
	ReviewGapAcknowledgmentID        common.EventID      `json:"review_gap_acknowledgment_id,omitempty"`
	ReviewGapAcknowledgmentClass     string              `json:"review_gap_acknowledgment_class,omitempty"`
	TransitionReceiptID              common.EventID      `json:"transition_receipt_id,omitempty"`
	TransitionKind                   string              `json:"transition_kind,omitempty"`
	CreatedAt                        time.Time           `json:"created_at"`
	CompletedAt                      time.Time           `json:"completed_at,omitempty"`
}

type TaskTranscriptReviewGapAcknowledgment struct {
	AcknowledgmentID common.EventID `json:"acknowledgment_id"`
	TaskID           common.TaskID  `json:"task_id"`
	SessionID        string         `json:"session_id"`
	Class            string         `json:"class"`
	ReviewState      string         `json:"review_state,omitempty"`
	ReviewScope      string         `json:"review_scope,omitempty"`

	ReviewedUpToSequence     int64 `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64 `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64 `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int   `json:"unreviewed_retained_count,omitempty"`

	TranscriptState string `json:"transcript_state,omitempty"`
	RetentionLimit  int    `json:"retention_limit,omitempty"`
	RetainedChunks  int    `json:"retained_chunks,omitempty"`
	DroppedChunks   int    `json:"dropped_chunks,omitempty"`

	ActionContext string    `json:"action_context,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	CreatedAt     time.Time `json:"created_at"`

	StaleBehindCurrent bool `json:"stale_behind_current,omitempty"`
	NewerRetainedCount int  `json:"newer_retained_count,omitempty"`
}

type TaskContinuityTransitionReceipt struct {
	ReceiptID                common.EventID `json:"receipt_id"`
	TaskID                   common.TaskID  `json:"task_id"`
	ShellSessionID           string         `json:"shell_session_id,omitempty"`
	TransitionKind           string         `json:"transition_kind"`
	TransitionHandle         string         `json:"transition_handle,omitempty"`
	TriggerAction            string         `json:"trigger_action,omitempty"`
	TriggerSource            string         `json:"trigger_source,omitempty"`
	HandoffID                string         `json:"handoff_id,omitempty"`
	LaunchAttemptID          string         `json:"launch_attempt_id,omitempty"`
	LaunchID                 string         `json:"launch_id,omitempty"`
	ResolutionID             string         `json:"resolution_id,omitempty"`
	BranchClassBefore        string         `json:"branch_class_before,omitempty"`
	BranchRefBefore          string         `json:"branch_ref_before,omitempty"`
	BranchClassAfter         string         `json:"branch_class_after,omitempty"`
	BranchRefAfter           string         `json:"branch_ref_after,omitempty"`
	HandoffStateBefore       string         `json:"handoff_continuity_before,omitempty"`
	HandoffStateAfter        string         `json:"handoff_continuity_after,omitempty"`
	LaunchControlBefore      string         `json:"launch_control_before,omitempty"`
	LaunchControlAfter       string         `json:"launch_control_after,omitempty"`
	ReviewGapPresent         bool           `json:"review_gap_present,omitempty"`
	ReviewPosture            string         `json:"review_posture,omitempty"`
	ReviewState              string         `json:"review_state,omitempty"`
	ReviewScope              string         `json:"review_scope,omitempty"`
	ReviewedUpToSequence     int64          `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64          `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64          `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int            `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID           common.EventID `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID     common.EventID `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent    bool           `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass      string         `json:"acknowledgment_class,omitempty"`
	Summary                  string         `json:"summary,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
}

type TaskContinuityTransitionRiskSummary struct {
	WindowSize                           int    `json:"window_size"`
	ReviewGapTransitions                 int    `json:"review_gap_transitions"`
	AcknowledgedReviewGapTransitions     int    `json:"acknowledged_review_gap_transitions"`
	UnacknowledgedReviewGapTransitions   int    `json:"unacknowledged_review_gap_transitions"`
	StaleReviewPostureTransitions        int    `json:"stale_review_posture_transitions"`
	SourceScopedReviewPostureTransitions int    `json:"source_scoped_review_posture_transitions"`
	IntoClaudeOwnershipTransitions       int    `json:"into_claude_ownership_transitions"`
	BackToLocalOwnershipTransitions      int    `json:"back_to_local_ownership_transitions"`
	OperationallyNotable                 bool   `json:"operationally_notable"`
	Summary                              string `json:"summary,omitempty"`
}

type TaskContinuityIncidentRiskSummary struct {
	ReviewGapPresent                bool   `json:"review_gap_present"`
	AcknowledgmentPresent           bool   `json:"acknowledgment_present"`
	StaleOrUnreviewedReviewPosture  bool   `json:"stale_or_unreviewed_review_posture"`
	SourceScopedReviewPosture       bool   `json:"source_scoped_review_posture"`
	IntoClaudeOwnershipTransition   bool   `json:"into_claude_ownership_transition"`
	BackToLocalOwnershipTransition  bool   `json:"back_to_local_ownership_transition"`
	UnresolvedContinuityAmbiguity   bool   `json:"unresolved_continuity_ambiguity"`
	NearbyFailedOrInterruptedRuns   int    `json:"nearby_failed_or_interrupted_runs"`
	NearbyRecoveryActions           int    `json:"nearby_recovery_actions"`
	RecentFailureOrRecoveryActivity bool   `json:"recent_failure_or_recovery_activity"`
	OperationallyNotable            bool   `json:"operationally_notable"`
	Summary                         string `json:"summary,omitempty"`
}

type TaskContinuityIncidentTriageReceipt struct {
	ReceiptID                 common.EventID                    `json:"receipt_id"`
	TaskID                    common.TaskID                     `json:"task_id"`
	AnchorMode                string                            `json:"anchor_mode"`
	AnchorTransitionReceiptID common.EventID                    `json:"anchor_transition_receipt_id"`
	AnchorTransitionKind      string                            `json:"anchor_transition_kind,omitempty"`
	AnchorHandoffID           string                            `json:"anchor_handoff_id,omitempty"`
	AnchorShellSessionID      string                            `json:"anchor_shell_session_id,omitempty"`
	Posture                   string                            `json:"posture"`
	FollowUpPosture           string                            `json:"follow_up_posture,omitempty"`
	Summary                   string                            `json:"summary,omitempty"`
	ReviewGapPresent          bool                              `json:"review_gap_present,omitempty"`
	ReviewPosture             string                            `json:"review_posture,omitempty"`
	ReviewState               string                            `json:"review_state,omitempty"`
	ReviewScope               string                            `json:"review_scope,omitempty"`
	ReviewedUpToSequence      int64                             `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence  int64                             `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence    int64                             `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount   int                               `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID            common.EventID                    `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID      common.EventID                    `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent     bool                              `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass       string                            `json:"acknowledgment_class,omitempty"`
	RiskSummary               TaskContinuityIncidentRiskSummary `json:"risk_summary"`
	CreatedAt                 time.Time                         `json:"created_at"`
}

type TaskContinuityIncidentFollowUpSummary struct {
	State                     string                                `json:"state"`
	Digest                    string                                `json:"digest,omitempty"`
	WindowAdvisory            string                                `json:"window_advisory,omitempty"`
	Advisory                  string                                `json:"advisory,omitempty"`
	ClosureIntelligence       *TaskContinuityIncidentClosureSummary `json:"closure_intelligence,omitempty"`
	FollowUpAdvised           bool                                  `json:"follow_up_advised"`
	NeedsFollowUp             bool                                  `json:"needs_follow_up"`
	Deferred                  bool                                  `json:"deferred"`
	TriageBehindLatest        bool                                  `json:"triage_behind_latest"`
	TriagedUnderReviewRisk    bool                                  `json:"triaged_under_review_risk"`
	LatestTransitionReceiptID common.EventID                        `json:"latest_transition_receipt_id,omitempty"`
	LatestTriageReceiptID     common.EventID                        `json:"latest_triage_receipt_id,omitempty"`
	TriageAnchorReceiptID     common.EventID                        `json:"triage_anchor_receipt_id,omitempty"`
	TriagePosture             string                                `json:"triage_posture,omitempty"`
	LatestFollowUpReceiptID   common.EventID                        `json:"latest_follow_up_receipt_id,omitempty"`
	LatestFollowUpActionKind  string                                `json:"latest_follow_up_action_kind,omitempty"`
	LatestFollowUpSummary     string                                `json:"latest_follow_up_summary,omitempty"`
	LatestFollowUpAt          time.Time                             `json:"latest_follow_up_at,omitempty"`
	FollowUpReceiptPresent    bool                                  `json:"follow_up_receipt_present,omitempty"`
	FollowUpOpen              bool                                  `json:"follow_up_open,omitempty"`
	FollowUpClosed            bool                                  `json:"follow_up_closed,omitempty"`
	FollowUpReopened          bool                                  `json:"follow_up_reopened,omitempty"`
	FollowUpProgressed        bool                                  `json:"follow_up_progressed,omitempty"`
}

type TaskContinuityIncidentClosureSummary struct {
	Class                             string                                    `json:"class"`
	Digest                            string                                    `json:"digest,omitempty"`
	WindowAdvisory                    string                                    `json:"window_advisory,omitempty"`
	Detail                            string                                    `json:"detail,omitempty"`
	BoundedWindow                     bool                                      `json:"bounded_window"`
	WindowSize                        int                                       `json:"window_size"`
	DistinctAnchors                   int                                       `json:"distinct_anchors"`
	OperationallyUnresolved           bool                                      `json:"operationally_unresolved"`
	ClosureAppearsWeak                bool                                      `json:"closure_appears_weak"`
	ReopenedAfterClosure              bool                                      `json:"reopened_after_closure"`
	RepeatedReopenLoop                bool                                      `json:"repeated_reopen_loop"`
	StagnantProgression               bool                                      `json:"stagnant_progression"`
	TriagedWithoutFollowUp            bool                                      `json:"triaged_without_follow_up"`
	AnchorsWithOpenFollowUp           int                                       `json:"anchors_with_open_follow_up"`
	AnchorsClosed                     int                                       `json:"anchors_closed"`
	AnchorsReopened                   int                                       `json:"anchors_reopened"`
	AnchorsBehindLatestTransition     int                                       `json:"anchors_behind_latest_transition"`
	AnchorsRepeatedWithoutProgression int                                       `json:"anchors_repeated_without_progression"`
	AnchorsTriagedWithoutFollowUp     int                                       `json:"anchors_triaged_without_follow_up"`
	ReopenedAfterClosureAnchors       int                                       `json:"reopened_after_closure_anchors"`
	RepeatedReopenLoopAnchors         int                                       `json:"repeated_reopen_loop_anchors"`
	StagnantProgressionAnchors        int                                       `json:"stagnant_progression_anchors"`
	RecentAnchors                     []TaskContinuityIncidentClosureAnchorItem `json:"recent_anchors,omitempty"`
}

type TaskContinuityIncidentTaskRiskSummary struct {
	Class                               string   `json:"class"`
	Digest                              string   `json:"digest,omitempty"`
	WindowAdvisory                      string   `json:"window_advisory,omitempty"`
	Detail                              string   `json:"detail,omitempty"`
	BoundedWindow                       bool     `json:"bounded_window"`
	WindowSize                          int      `json:"window_size"`
	DistinctAnchors                     int      `json:"distinct_anchors"`
	RecurringWeakClosure                bool     `json:"recurring_weak_closure"`
	RecurringUnresolved                 bool     `json:"recurring_unresolved"`
	RecurringStagnantFollowUp           bool     `json:"recurring_stagnant_follow_up"`
	RecurringTriagedWithoutFollowUp     bool     `json:"recurring_triaged_without_follow_up"`
	ReopenedAfterClosureAnchors         int      `json:"reopened_after_closure_anchors"`
	RepeatedReopenLoopAnchors           int      `json:"repeated_reopen_loop_anchors"`
	StagnantProgressionAnchors          int      `json:"stagnant_progression_anchors"`
	AnchorsTriagedWithoutFollowUp       int      `json:"anchors_triaged_without_follow_up"`
	AnchorsWithOpenFollowUp             int      `json:"anchors_with_open_follow_up"`
	AnchorsReopened                     int      `json:"anchors_reopened"`
	OperationallyUnresolvedAnchorSignal int      `json:"operationally_unresolved_anchor_signal"`
	RecentAnchorClasses                 []string `json:"recent_anchor_classes,omitempty"`
}

type TaskContinuityIncidentClosureAnchorItem struct {
	AnchorTransitionReceiptID string         `json:"anchor_transition_receipt_id"`
	Class                     string         `json:"class"`
	Digest                    string         `json:"digest,omitempty"`
	Explanation               string         `json:"explanation,omitempty"`
	LatestFollowUpReceiptID   common.EventID `json:"latest_follow_up_receipt_id,omitempty"`
	LatestFollowUpActionKind  string         `json:"latest_follow_up_action_kind,omitempty"`
	LatestFollowUpAt          time.Time      `json:"latest_follow_up_at,omitempty"`
}

type TaskContinuityIncidentFollowUpReceipt struct {
	ReceiptID                 common.EventID `json:"receipt_id"`
	TaskID                    common.TaskID  `json:"task_id"`
	AnchorMode                string         `json:"anchor_mode"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id"`
	AnchorTransitionKind      string         `json:"anchor_transition_kind,omitempty"`
	AnchorHandoffID           string         `json:"anchor_handoff_id,omitempty"`
	AnchorShellSessionID      string         `json:"anchor_shell_session_id,omitempty"`
	TriageReceiptID           common.EventID `json:"triage_receipt_id,omitempty"`
	TriagePosture             string         `json:"triage_posture,omitempty"`
	TriageFollowUpPosture     string         `json:"triage_follow_up_posture,omitempty"`
	ActionKind                string         `json:"action_kind"`
	Summary                   string         `json:"summary,omitempty"`
	ReviewGapPresent          bool           `json:"review_gap_present,omitempty"`
	ReviewPosture             string         `json:"review_posture,omitempty"`
	ReviewState               string         `json:"review_state,omitempty"`
	ReviewScope               string         `json:"review_scope,omitempty"`
	ReviewedUpToSequence      int64          `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence  int64          `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence    int64          `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount   int            `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID            common.EventID `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID      common.EventID `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent     bool           `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass       string         `json:"acknowledgment_class,omitempty"`
	TriagedUnderReviewRisk    bool           `json:"triaged_under_review_risk,omitempty"`
	CreatedAt                 time.Time      `json:"created_at"`
}

type TaskContinuityIncidentTriageHistoryRollup struct {
	WindowSize                        int    `json:"window_size"`
	BoundedWindow                     bool   `json:"bounded_window"`
	DistinctAnchors                   int    `json:"distinct_anchors"`
	AnchorsTriagedCurrent             int    `json:"anchors_triaged_current"`
	AnchorsNeedsFollowUp              int    `json:"anchors_needs_follow_up"`
	AnchorsDeferred                   int    `json:"anchors_deferred"`
	AnchorsBehindLatestTransition     int    `json:"anchors_behind_latest_transition"`
	AnchorsWithOpenFollowUp           int    `json:"anchors_with_open_follow_up"`
	AnchorsRepeatedWithoutProgression int    `json:"anchors_repeated_without_progression"`
	ReviewRiskReceipts                int    `json:"review_risk_receipts"`
	AcknowledgedReviewGapReceipts     int    `json:"acknowledged_review_gap_receipts"`
	OperationallyNotable              bool   `json:"operationally_notable"`
	Summary                           string `json:"summary,omitempty"`
}

type TaskContinuityIncidentFollowUpHistoryRollup struct {
	WindowSize                        int    `json:"window_size"`
	BoundedWindow                     bool   `json:"bounded_window"`
	DistinctAnchors                   int    `json:"distinct_anchors"`
	ReceiptsRecordedPending           int    `json:"receipts_recorded_pending"`
	ReceiptsProgressed                int    `json:"receipts_progressed"`
	ReceiptsClosed                    int    `json:"receipts_closed"`
	ReceiptsReopened                  int    `json:"receipts_reopened"`
	AnchorsWithOpenFollowUp           int    `json:"anchors_with_open_follow_up"`
	AnchorsClosed                     int    `json:"anchors_closed"`
	AnchorsReopened                   int    `json:"anchors_reopened"`
	OpenAnchorsBehindLatestTransition int    `json:"open_anchors_behind_latest_transition"`
	AnchorsRepeatedWithoutProgression int    `json:"anchors_repeated_without_progression"`
	AnchorsTriagedWithoutFollowUp     int    `json:"anchors_triaged_without_follow_up"`
	OperationallyNotable              bool   `json:"operationally_notable"`
	Summary                           string `json:"summary,omitempty"`
}

type TaskContinuityIncidentRun struct {
	RunID          common.RunID   `json:"run_id"`
	WorkerKind     run.WorkerKind `json:"worker_kind"`
	Status         run.Status     `json:"status"`
	ShellSessionID string         `json:"shell_session_id,omitempty"`
	ExitCode       *int           `json:"exit_code,omitempty"`
	OccurredAt     time.Time      `json:"occurred_at"`
	StartedAt      time.Time      `json:"started_at"`
	EndedAt        *time.Time     `json:"ended_at,omitempty"`
	Summary        string         `json:"summary,omitempty"`
}

type TaskContinuityIncidentProof struct {
	EventID    common.EventID `json:"event_id"`
	Type       string         `json:"type"`
	ActorType  string         `json:"actor_type"`
	ActorID    string         `json:"actor_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Summary    string         `json:"summary,omitempty"`
	SequenceNo int64          `json:"sequence_no"`
}

type TaskInspectResponse struct {
	TaskID                                   common.TaskID                                `json:"task_id"`
	RepoAnchor                               RepoAnchor                                   `json:"repo_anchor"`
	Intent                                   *intent.State                                `json:"intent,omitempty"`
	CompiledIntent                           *TaskCompiledIntentSummary                   `json:"compiled_intent,omitempty"`
	Brief                                    *brief.ExecutionBrief                        `json:"brief,omitempty"`
	CompiledBrief                            *TaskCompiledBriefSummary                    `json:"compiled_brief,omitempty"`
	TaskMemory                               *taskmemory.Snapshot                         `json:"task_memory,omitempty"`
	Benchmark                                *benchmark.Run                               `json:"benchmark,omitempty"`
	Run                                      *run.ExecutionRun                            `json:"run,omitempty"`
	Checkpoint                               *checkpoint.Checkpoint                       `json:"checkpoint,omitempty"`
	Handoff                                  *handoff.Packet                              `json:"handoff,omitempty"`
	Launch                                   *handoff.Launch                              `json:"launch,omitempty"`
	Acknowledgment                           *handoff.Acknowledgment                      `json:"acknowledgment,omitempty"`
	FollowThrough                            *handoff.FollowThrough                       `json:"follow_through,omitempty"`
	Resolution                               *handoff.Resolution                          `json:"resolution,omitempty"`
	ActiveBranch                             *TaskActiveBranch                            `json:"active_branch,omitempty"`
	LocalRunFinalization                     *TaskLocalRunFinalization                    `json:"local_run_finalization,omitempty"`
	LocalResumeAuthority                     *TaskLocalResumeAuthority                    `json:"local_resume_authority,omitempty"`
	ActionAuthority                          *TaskOperatorActionAuthoritySet              `json:"action_authority,omitempty"`
	OperatorDecision                         *TaskOperatorDecisionSummary                 `json:"operator_decision,omitempty"`
	OperatorExecutionPlan                    *TaskOperatorExecutionPlan                   `json:"operator_execution_plan,omitempty"`
	LatestOperatorStepReceipt                *TaskOperatorStepReceipt                     `json:"latest_operator_step_receipt,omitempty"`
	RecentOperatorStepReceipts               []TaskOperatorStepReceipt                    `json:"recent_operator_step_receipts,omitempty"`
	LatestContinuityTransitionReceipt        *TaskContinuityTransitionReceipt             `json:"latest_continuity_transition_receipt,omitempty"`
	RecentContinuityTransitionReceipts       []TaskContinuityTransitionReceipt            `json:"recent_continuity_transition_receipts,omitempty"`
	ContinuityTransitionRiskSummary          *TaskContinuityTransitionRiskSummary         `json:"continuity_transition_risk_summary,omitempty"`
	ContinuityIncidentSummary                *TaskContinuityIncidentRiskSummary           `json:"continuity_incident_summary,omitempty"`
	LatestContinuityIncidentTriageReceipt    *TaskContinuityIncidentTriageReceipt         `json:"latest_continuity_incident_triage_receipt,omitempty"`
	RecentContinuityIncidentTriageReceipts   []TaskContinuityIncidentTriageReceipt        `json:"recent_continuity_incident_triage_receipts,omitempty"`
	ContinuityIncidentTriageHistoryRollup    *TaskContinuityIncidentTriageHistoryRollup   `json:"continuity_incident_triage_history_rollup,omitempty"`
	LatestContinuityIncidentFollowUpReceipt  *TaskContinuityIncidentFollowUpReceipt       `json:"latest_continuity_incident_follow_up_receipt,omitempty"`
	RecentContinuityIncidentFollowUpReceipts []TaskContinuityIncidentFollowUpReceipt      `json:"recent_continuity_incident_follow_up_receipts,omitempty"`
	ContinuityIncidentFollowUpHistoryRollup  *TaskContinuityIncidentFollowUpHistoryRollup `json:"continuity_incident_follow_up_history_rollup,omitempty"`
	ContinuityIncidentFollowUp               *TaskContinuityIncidentFollowUpSummary       `json:"continuity_incident_follow_up,omitempty"`
	ContinuityIncidentTaskRisk               *TaskContinuityIncidentTaskRiskSummary       `json:"continuity_incident_task_risk,omitempty"`
	LatestTranscriptReviewGapAcknowledgment  *TaskTranscriptReviewGapAcknowledgment       `json:"latest_transcript_review_gap_acknowledgment,omitempty"`
	RecentTranscriptReviewGapAcknowledgments []TaskTranscriptReviewGapAcknowledgment      `json:"recent_transcript_review_gap_acknowledgments,omitempty"`
	LaunchControl                            *TaskLaunchControl                           `json:"launch_control,omitempty"`
	HandoffContinuity                        *TaskHandoffContinuity                       `json:"handoff_continuity,omitempty"`
	Recovery                                 *TaskRecoveryAssessment                      `json:"recovery,omitempty"`
	LatestRecoveryAction                     *TaskRecoveryActionRecord                    `json:"latest_recovery_action,omitempty"`
	RecentRecoveryActions                    []TaskRecoveryActionRecord                   `json:"recent_recovery_actions,omitempty"`
	ShellSessions                            []TaskShellSessionRecord                     `json:"shell_sessions,omitempty"`
	RecentShellEvents                        []TaskShellSessionEventRecord                `json:"recent_shell_events,omitempty"`
	RecentShellTranscript                    []TaskShellTranscriptChunk                   `json:"recent_shell_transcript,omitempty"`
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
	TaskID                      common.TaskID `json:"task_id"`
	AcknowledgeReviewGap        bool          `json:"acknowledge_review_gap,omitempty"`
	ReviewGapSessionID          string        `json:"review_gap_session_id,omitempty"`
	ReviewGapAcknowledgmentKind string        `json:"review_gap_acknowledgment_kind,omitempty"`
	ReviewGapSummary            string        `json:"review_gap_summary,omitempty"`
}

type TaskExecutePrimaryOperatorStepResponse struct {
	TaskID                                   common.TaskID                                `json:"task_id"`
	Receipt                                  TaskOperatorStepReceipt                      `json:"receipt"`
	ActiveBranch                             *TaskActiveBranch                            `json:"active_branch,omitempty"`
	OperatorDecision                         *TaskOperatorDecisionSummary                 `json:"operator_decision,omitempty"`
	OperatorExecutionPlan                    *TaskOperatorExecutionPlan                   `json:"operator_execution_plan,omitempty"`
	RecoveryClass                            string                                       `json:"recovery_class,omitempty"`
	RecommendedAction                        string                                       `json:"recommended_action,omitempty"`
	ReadyForNextRun                          bool                                         `json:"ready_for_next_run,omitempty"`
	ReadyForHandoffLaunch                    bool                                         `json:"ready_for_handoff_launch,omitempty"`
	RecoveryReason                           string                                       `json:"recovery_reason,omitempty"`
	CanonicalResponse                        string                                       `json:"canonical_response,omitempty"`
	RecentOperatorStepReceipts               []TaskOperatorStepReceipt                    `json:"recent_operator_step_receipts,omitempty"`
	LatestContinuityTransitionReceipt        *TaskContinuityTransitionReceipt             `json:"latest_continuity_transition_receipt,omitempty"`
	RecentContinuityTransitionReceipts       []TaskContinuityTransitionReceipt            `json:"recent_continuity_transition_receipts,omitempty"`
	LatestContinuityIncidentTriageReceipt    *TaskContinuityIncidentTriageReceipt         `json:"latest_continuity_incident_triage_receipt,omitempty"`
	RecentContinuityIncidentTriageReceipts   []TaskContinuityIncidentTriageReceipt        `json:"recent_continuity_incident_triage_receipts,omitempty"`
	ContinuityIncidentTriageHistoryRollup    *TaskContinuityIncidentTriageHistoryRollup   `json:"continuity_incident_triage_history_rollup,omitempty"`
	LatestContinuityIncidentFollowUpReceipt  *TaskContinuityIncidentFollowUpReceipt       `json:"latest_continuity_incident_follow_up_receipt,omitempty"`
	RecentContinuityIncidentFollowUpReceipts []TaskContinuityIncidentFollowUpReceipt      `json:"recent_continuity_incident_follow_up_receipts,omitempty"`
	ContinuityIncidentFollowUpHistoryRollup  *TaskContinuityIncidentFollowUpHistoryRollup `json:"continuity_incident_follow_up_history_rollup,omitempty"`
	ContinuityIncidentFollowUp               *TaskContinuityIncidentFollowUpSummary       `json:"continuity_incident_follow_up,omitempty"`
	ContinuityIncidentTaskRisk               *TaskContinuityIncidentTaskRiskSummary       `json:"continuity_incident_task_risk,omitempty"`
	LatestTranscriptReviewGapAcknowledgment  *TaskTranscriptReviewGapAcknowledgment       `json:"latest_transcript_review_gap_acknowledgment,omitempty"`
	RecentTranscriptReviewGapAcknowledgments []TaskTranscriptReviewGapAcknowledgment      `json:"recent_transcript_review_gap_acknowledgments,omitempty"`
}

type TaskOperatorAcknowledgeReviewGapRequest struct {
	TaskID        common.TaskID `json:"task_id"`
	SessionID     string        `json:"session_id,omitempty"`
	Kind          string        `json:"kind,omitempty"`
	Summary       string        `json:"summary,omitempty"`
	ActionContext string        `json:"action_context,omitempty"`
}

type TaskOperatorAcknowledgeReviewGapResponse struct {
	TaskID                 common.TaskID                           `json:"task_id"`
	SessionID              string                                  `json:"session_id"`
	Acknowledgment         TaskTranscriptReviewGapAcknowledgment   `json:"acknowledgment"`
	ReviewGapState         string                                  `json:"review_gap_state"`
	ReviewGapClass         string                                  `json:"review_gap_class"`
	ReviewScope            string                                  `json:"review_scope,omitempty"`
	ReviewedUpToSequence   int64                                   `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSeq    int64                                   `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence int64                                   `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetained     int                                     `json:"unreviewed_retained_count,omitempty"`
	Advisory               string                                  `json:"advisory,omitempty"`
	RecentAcknowledgments  []TaskTranscriptReviewGapAcknowledgment `json:"recent_acknowledgments,omitempty"`
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
	TransitionReceiptID   common.EventID         `json:"transition_receipt_id,omitempty"`
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
	BriefID                 common.BriefID `json:"brief_id"`
	Posture                 string         `json:"posture,omitempty"`
	Objective               string         `json:"objective"`
	RequestedOutcome        string         `json:"requested_outcome,omitempty"`
	NormalizedAction        string         `json:"normalized_action"`
	ScopeSummary            string         `json:"scope_summary,omitempty"`
	Constraints             []string       `json:"constraints,omitempty"`
	DoneCriteria            []string       `json:"done_criteria,omitempty"`
	AmbiguityFlags          []string       `json:"ambiguity_flags,omitempty"`
	ClarificationQuestions  []string       `json:"clarification_questions,omitempty"`
	RequiresClarification   bool           `json:"requires_clarification,omitempty"`
	WorkerFraming           string         `json:"worker_framing,omitempty"`
	BoundedEvidenceMessages int            `json:"bounded_evidence_messages,omitempty"`
	PromptTargets           []string       `json:"prompt_targets,omitempty"`
	ValidatorCommands       []string       `json:"validator_commands,omitempty"`
	ConfidenceLevel         string         `json:"confidence_level,omitempty"`
	ConfidenceReason        string         `json:"confidence_reason,omitempty"`
	EstimatedTokenSavings   int            `json:"estimated_token_savings,omitempty"`
	DispatchPromptTokens    int            `json:"dispatch_prompt_tokens,omitempty"`
	StructuredPromptTokens  int            `json:"structured_prompt_tokens,omitempty"`
	DefaultSerializer       string         `json:"default_serializer,omitempty"`
	StructuredCheaper       bool           `json:"structured_cheaper,omitempty"`
}

type TaskShellRun struct {
	RunID              common.RunID   `json:"run_id"`
	WorkerKind         run.WorkerKind `json:"worker_kind"`
	Status             run.Status     `json:"status"`
	WorkerRunID        string         `json:"worker_run_id,omitempty"`
	ShellSessionID     string         `json:"shell_session_id,omitempty"`
	Command            string         `json:"command,omitempty"`
	Args               []string       `json:"args,omitempty"`
	ExitCode           *int           `json:"exit_code,omitempty"`
	Stdout             string         `json:"stdout,omitempty"`
	Stderr             string         `json:"stderr,omitempty"`
	ChangedFiles       []string       `json:"changed_files,omitempty"`
	ValidationSignals  []string       `json:"validation_signals,omitempty"`
	OutputArtifactRef  string         `json:"output_artifact_ref,omitempty"`
	StructuredSummary  string         `json:"structured_summary,omitempty"`
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
	ReceiptID                        string         `json:"receipt_id"`
	ActionHandle                     string         `json:"action_handle"`
	ResultClass                      string         `json:"result_class"`
	Summary                          string         `json:"summary,omitempty"`
	Reason                           string         `json:"reason,omitempty"`
	ReviewGapState                   string         `json:"review_gap_state,omitempty"`
	ReviewGapSessionID               string         `json:"review_gap_session_id,omitempty"`
	ReviewGapClass                   string         `json:"review_gap_class,omitempty"`
	ReviewGapPresent                 bool           `json:"review_gap_present,omitempty"`
	ReviewGapReviewedUpTo            int64          `json:"review_gap_reviewed_up_to_sequence,omitempty"`
	ReviewGapOldestUnreviewed        int64          `json:"review_gap_oldest_unreviewed_sequence,omitempty"`
	ReviewGapNewestRetained          int64          `json:"review_gap_newest_retained_sequence,omitempty"`
	ReviewGapUnreviewedRetainedCount int            `json:"review_gap_unreviewed_retained_count,omitempty"`
	ReviewGapAcknowledged            bool           `json:"review_gap_acknowledged,omitempty"`
	ReviewGapAcknowledgmentID        common.EventID `json:"review_gap_acknowledgment_id,omitempty"`
	ReviewGapAcknowledgmentClass     string         `json:"review_gap_acknowledgment_class,omitempty"`
	TransitionReceiptID              common.EventID `json:"transition_receipt_id,omitempty"`
	TransitionKind                   string         `json:"transition_kind,omitempty"`
	CreatedAt                        time.Time      `json:"created_at"`
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
	TaskID                                   common.TaskID                                `json:"task_id"`
	Goal                                     string                                       `json:"goal"`
	Phase                                    string                                       `json:"phase"`
	Status                                   string                                       `json:"status"`
	RepoAnchor                               RepoAnchor                                   `json:"repo_anchor"`
	IntentClass                              string                                       `json:"intent_class,omitempty"`
	IntentSummary                            string                                       `json:"intent_summary,omitempty"`
	CompiledIntent                           *TaskCompiledIntentSummary                   `json:"compiled_intent,omitempty"`
	Brief                                    *TaskShellBrief                              `json:"brief,omitempty"`
	Run                                      *TaskShellRun                                `json:"run,omitempty"`
	Checkpoint                               *TaskShellCheckpoint                         `json:"checkpoint,omitempty"`
	Handoff                                  *TaskShellHandoff                            `json:"handoff,omitempty"`
	Launch                                   *TaskShellLaunch                             `json:"launch,omitempty"`
	LaunchControl                            *TaskShellLaunchControl                      `json:"launch_control,omitempty"`
	Acknowledgment                           *TaskShellAcknowledgment                     `json:"acknowledgment,omitempty"`
	FollowThrough                            *TaskShellFollowThrough                      `json:"follow_through,omitempty"`
	Resolution                               *TaskShellResolution                         `json:"resolution,omitempty"`
	ActiveBranch                             *TaskShellActiveBranch                       `json:"active_branch,omitempty"`
	LocalRunFinalization                     *TaskShellLocalRunFinalization               `json:"local_run_finalization,omitempty"`
	LocalResume                              *TaskShellLocalResumeAuthority               `json:"local_resume,omitempty"`
	ActionAuthority                          *TaskShellOperatorActionAuthoritySet         `json:"action_authority,omitempty"`
	OperatorDecision                         *TaskShellOperatorDecisionSummary            `json:"operator_decision,omitempty"`
	OperatorExecutionPlan                    *TaskShellOperatorExecutionPlan              `json:"operator_execution_plan,omitempty"`
	LatestOperatorStepReceipt                *TaskShellOperatorStepReceipt                `json:"latest_operator_step_receipt,omitempty"`
	RecentOperatorStepReceipts               []TaskShellOperatorStepReceipt               `json:"recent_operator_step_receipts,omitempty"`
	LatestContinuityTransitionReceipt        *TaskContinuityTransitionReceipt             `json:"latest_continuity_transition_receipt,omitempty"`
	RecentContinuityTransitionReceipts       []TaskContinuityTransitionReceipt            `json:"recent_continuity_transition_receipts,omitempty"`
	ContinuityTransitionRiskSummary          *TaskContinuityTransitionRiskSummary         `json:"continuity_transition_risk_summary,omitempty"`
	ContinuityIncidentSummary                *TaskContinuityIncidentRiskSummary           `json:"continuity_incident_summary,omitempty"`
	LatestContinuityIncidentTriageReceipt    *TaskContinuityIncidentTriageReceipt         `json:"latest_continuity_incident_triage_receipt,omitempty"`
	RecentContinuityIncidentTriageReceipts   []TaskContinuityIncidentTriageReceipt        `json:"recent_continuity_incident_triage_receipts,omitempty"`
	ContinuityIncidentTriageHistoryRollup    *TaskContinuityIncidentTriageHistoryRollup   `json:"continuity_incident_triage_history_rollup,omitempty"`
	LatestContinuityIncidentFollowUpReceipt  *TaskContinuityIncidentFollowUpReceipt       `json:"latest_continuity_incident_follow_up_receipt,omitempty"`
	RecentContinuityIncidentFollowUpReceipts []TaskContinuityIncidentFollowUpReceipt      `json:"recent_continuity_incident_follow_up_receipts,omitempty"`
	ContinuityIncidentFollowUpHistoryRollup  *TaskContinuityIncidentFollowUpHistoryRollup `json:"continuity_incident_follow_up_history_rollup,omitempty"`
	ContinuityIncidentFollowUp               *TaskContinuityIncidentFollowUpSummary       `json:"continuity_incident_follow_up,omitempty"`
	ContinuityIncidentTaskRisk               *TaskContinuityIncidentTaskRiskSummary       `json:"continuity_incident_task_risk,omitempty"`
	LatestTranscriptReviewGapAcknowledgment  *TaskTranscriptReviewGapAcknowledgment       `json:"latest_transcript_review_gap_acknowledgment,omitempty"`
	RecentTranscriptReviewGapAcknowledgments []TaskTranscriptReviewGapAcknowledgment      `json:"recent_transcript_review_gap_acknowledgments,omitempty"`
	HandoffContinuity                        *TaskShellHandoffContinuity                  `json:"handoff_continuity,omitempty"`
	Recovery                                 *TaskShellRecovery                           `json:"recovery,omitempty"`
	ShellSessions                            []TaskShellSessionRecord                     `json:"shell_sessions,omitempty"`
	RecentShellEvents                        []TaskShellSessionEventRecord                `json:"recent_shell_events,omitempty"`
	RecentShellTranscript                    []TaskShellTranscriptChunk                   `json:"recent_shell_transcript,omitempty"`
	RecentProofs                             []TaskShellProof                             `json:"recent_proofs,omitempty"`
	RecentConversation                       []TaskShellConversation                      `json:"recent_conversation,omitempty"`
	LatestCanonicalResponse                  string                                       `json:"latest_canonical_response,omitempty"`
}

type TaskShellLifecycleRequest struct {
	TaskID                common.TaskID `json:"task_id"`
	SessionID             string        `json:"session_id"`
	Kind                  string        `json:"kind"`
	HostMode              string        `json:"host_mode"`
	HostState             string        `json:"host_state"`
	WorkerSessionID       string        `json:"worker_session_id,omitempty"`
	WorkerSessionIDSource string        `json:"worker_session_id_source,omitempty"`
	AttachCapability      string        `json:"attach_capability,omitempty"`
	Note                  string        `json:"note,omitempty"`
	InputLive             bool          `json:"input_live"`
	ExitCode              *int          `json:"exit_code,omitempty"`
	PaneWidth             int           `json:"pane_width,omitempty"`
	PaneHeight            int           `json:"pane_height,omitempty"`
}

type TaskShellLifecycleResponse struct {
	TaskID common.TaskID `json:"task_id"`
}

type TaskShellTranscriptChunk struct {
	ChunkID    common.EventID `json:"chunk_id"`
	TaskID     common.TaskID  `json:"task_id"`
	SessionID  string         `json:"session_id"`
	SequenceNo int64          `json:"sequence_no"`
	Source     string         `json:"source"`
	Content    string         `json:"content"`
	CreatedAt  time.Time      `json:"created_at"`
}

type TaskShellTranscriptChunkAppend struct {
	Source    string    `json:"source,omitempty"`
	Content   string    `json:"content,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type TaskShellTranscriptAppendRequest struct {
	TaskID    common.TaskID                    `json:"task_id"`
	SessionID string                           `json:"session_id"`
	Chunks    []TaskShellTranscriptChunkAppend `json:"chunks,omitempty"`
}

type TaskShellTranscriptAppendResponse struct {
	TaskID         common.TaskID `json:"task_id"`
	SessionID      string        `json:"session_id"`
	RetainedChunks int           `json:"retained_chunks"`
	DroppedChunks  int           `json:"dropped_chunks"`
	RetentionLimit int           `json:"retention_limit"`
	LastSequenceNo int64         `json:"last_sequence_no"`
	LastChunkAt    time.Time     `json:"last_chunk_at,omitempty"`
}

type TaskShellTranscriptSourceCount struct {
	Source string `json:"source"`
	Chunks int    `json:"chunks"`
}

type TaskShellTranscriptReviewMarker struct {
	ReviewID                 common.EventID `json:"review_id"`
	SourceFilter             string         `json:"source_filter,omitempty"`
	ReviewedUpToSequence     int64          `json:"reviewed_up_to_sequence"`
	Summary                  string         `json:"summary,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
	TranscriptState          string         `json:"transcript_state,omitempty"`
	RetentionLimit           int            `json:"retention_limit,omitempty"`
	RetainedChunks           int            `json:"retained_chunks,omitempty"`
	DroppedChunks            int            `json:"dropped_chunks,omitempty"`
	OldestRetainedSequence   int64          `json:"oldest_retained_sequence,omitempty"`
	NewestRetainedSequence   int64          `json:"newest_retained_sequence,omitempty"`
	StaleBehindLatest        bool           `json:"stale_behind_latest"`
	NewerRetainedCount       int            `json:"newer_retained_count,omitempty"`
	OldestUnreviewedSequence int64          `json:"oldest_unreviewed_sequence,omitempty"`
	ClosureState             string         `json:"closure_state,omitempty"`
}

type TaskShellTranscriptReviewClosure struct {
	State                    string `json:"state"`
	Scope                    string `json:"scope,omitempty"`
	HasReview                bool   `json:"has_review"`
	HasUnreadNewerEvidence   bool   `json:"has_unread_newer_evidence"`
	ReviewedUpToSequence     int64  `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64  `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64  `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int    `json:"unreviewed_retained_count,omitempty"`
	RetentionLimit           int    `json:"retention_limit,omitempty"`
	RetainedChunkCount       int    `json:"retained_chunk_count,omitempty"`
	DroppedChunkCount        int    `json:"dropped_chunk_count,omitempty"`
}

type TaskShellTranscriptReadRequest struct {
	TaskID         common.TaskID `json:"task_id"`
	SessionID      string        `json:"session_id"`
	Limit          int           `json:"limit,omitempty"`
	BeforeSequence int64         `json:"before_sequence,omitempty"`
	Source         string        `json:"source,omitempty"`
}

type TaskShellTranscriptReadResponse struct {
	TaskID                  common.TaskID                    `json:"task_id"`
	SessionID               string                           `json:"session_id"`
	TranscriptState         string                           `json:"transcript_state"`
	TranscriptOnly          bool                             `json:"transcript_only"`
	Bounded                 bool                             `json:"bounded"`
	Partial                 bool                             `json:"partial"`
	RetentionLimit          int                              `json:"retention_limit"`
	RetainedChunkCount      int                              `json:"retained_chunk_count"`
	DroppedChunkCount       int                              `json:"dropped_chunk_count"`
	LastSequence            int64                            `json:"last_sequence,omitempty"`
	LastChunkAt             time.Time                        `json:"last_chunk_at,omitempty"`
	OldestRetainedSequence  int64                            `json:"oldest_retained_sequence,omitempty"`
	NewestRetainedSequence  int64                            `json:"newest_retained_sequence,omitempty"`
	OldestRetainedChunkAt   time.Time                        `json:"oldest_retained_chunk_at,omitempty"`
	NewestRetainedChunkAt   time.Time                        `json:"newest_retained_chunk_at,omitempty"`
	SourceSummary           []TaskShellTranscriptSourceCount `json:"source_summary,omitempty"`
	RequestedLimit          int                              `json:"requested_limit"`
	RequestedBeforeSequence int64                            `json:"requested_before_sequence,omitempty"`
	RequestedSource         string                           `json:"requested_source,omitempty"`
	PageOldestSequence      int64                            `json:"page_oldest_sequence,omitempty"`
	PageNewestSequence      int64                            `json:"page_newest_sequence,omitempty"`
	PageChunkCount          int                              `json:"page_chunk_count"`
	HasMoreOlder            bool                             `json:"has_more_older"`
	NextBeforeSequence      int64                            `json:"next_before_sequence,omitempty"`
	LatestReview            *TaskShellTranscriptReviewMarker `json:"latest_review,omitempty"`
	HasUnreadNewerEvidence  bool                             `json:"has_unread_newer_evidence"`
	PageFullyReviewed       bool                             `json:"page_fully_reviewed"`
	PageCrossesReview       bool                             `json:"page_crosses_review_boundary"`
	PageHasUnreviewed       bool                             `json:"page_has_unreviewed_evidence"`
	Closure                 TaskShellTranscriptReviewClosure `json:"closure"`
	Chunks                  []TaskShellTranscriptChunk       `json:"chunks,omitempty"`
}

type TaskShellTranscriptReviewRequest struct {
	TaskID          common.TaskID `json:"task_id"`
	SessionID       string        `json:"session_id"`
	ReviewedUpToSeq int64         `json:"reviewed_up_to_sequence"`
	Source          string        `json:"source,omitempty"`
	Summary         string        `json:"summary,omitempty"`
}

type TaskShellTranscriptReviewResponse struct {
	TaskID                 common.TaskID                    `json:"task_id"`
	SessionID              string                           `json:"session_id"`
	TranscriptState        string                           `json:"transcript_state"`
	RetentionLimit         int                              `json:"retention_limit"`
	RetainedChunkCount     int                              `json:"retained_chunk_count"`
	DroppedChunkCount      int                              `json:"dropped_chunk_count"`
	OldestRetainedSequence int64                            `json:"oldest_retained_sequence,omitempty"`
	NewestRetainedSequence int64                            `json:"newest_retained_sequence,omitempty"`
	LatestReview           TaskShellTranscriptReviewMarker  `json:"latest_review"`
	HasUnreadNewerEvidence bool                             `json:"has_unread_newer_evidence"`
	Closure                TaskShellTranscriptReviewClosure `json:"closure"`
}

type TaskShellTranscriptHistoryRequest struct {
	TaskID    common.TaskID `json:"task_id"`
	SessionID string        `json:"session_id"`
	Source    string        `json:"source,omitempty"`
	Limit     int           `json:"limit,omitempty"`
}

type TaskShellTranscriptHistoryResponse struct {
	TaskID                 common.TaskID                     `json:"task_id"`
	SessionID              string                            `json:"session_id"`
	TranscriptState        string                            `json:"transcript_state"`
	TranscriptOnly         bool                              `json:"transcript_only"`
	Bounded                bool                              `json:"bounded"`
	Partial                bool                              `json:"partial"`
	RetentionLimit         int                               `json:"retention_limit"`
	RetainedChunkCount     int                               `json:"retained_chunk_count"`
	DroppedChunkCount      int                               `json:"dropped_chunk_count"`
	OldestRetainedSequence int64                             `json:"oldest_retained_sequence,omitempty"`
	NewestRetainedSequence int64                             `json:"newest_retained_sequence,omitempty"`
	RequestedLimit         int                               `json:"requested_limit"`
	RequestedSource        string                            `json:"requested_source,omitempty"`
	Closure                TaskShellTranscriptReviewClosure  `json:"closure"`
	LatestReview           *TaskShellTranscriptReviewMarker  `json:"latest_review,omitempty"`
	Reviews                []TaskShellTranscriptReviewMarker `json:"reviews,omitempty"`
}

type TaskTransitionHistoryRequest struct {
	TaskID          common.TaskID  `json:"task_id"`
	Limit           int            `json:"limit,omitempty"`
	BeforeReceiptID common.EventID `json:"before_receipt_id,omitempty"`
	TransitionKind  string         `json:"transition_kind,omitempty"`
	HandoffID       string         `json:"handoff_id,omitempty"`
}

type TaskTransitionHistoryResponse struct {
	TaskID                   common.TaskID                       `json:"task_id"`
	Bounded                  bool                                `json:"bounded"`
	RequestedLimit           int                                 `json:"requested_limit"`
	RequestedBeforeReceiptID common.EventID                      `json:"requested_before_receipt_id,omitempty"`
	RequestedTransitionKind  string                              `json:"requested_transition_kind,omitempty"`
	RequestedHandoffID       string                              `json:"requested_handoff_id,omitempty"`
	HasMoreOlder             bool                                `json:"has_more_older"`
	NextBeforeReceiptID      common.EventID                      `json:"next_before_receipt_id,omitempty"`
	Latest                   *TaskContinuityTransitionReceipt    `json:"latest,omitempty"`
	Receipts                 []TaskContinuityTransitionReceipt   `json:"receipts,omitempty"`
	RiskSummary              TaskContinuityTransitionRiskSummary `json:"risk_summary"`
}

type TaskContinuityIncidentSliceRequest struct {
	TaskID                    common.TaskID  `json:"task_id"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id,omitempty"`
	TransitionNeighborLimit   int            `json:"transition_neighbor_limit,omitempty"`
	RunLimit                  int            `json:"run_limit,omitempty"`
	RecoveryLimit             int            `json:"recovery_limit,omitempty"`
	ProofLimit                int            `json:"proof_limit,omitempty"`
	AckLimit                  int            `json:"ack_limit,omitempty"`
}

type TaskContinuityIncidentSliceResponse struct {
	TaskID                             common.TaskID                           `json:"task_id"`
	Bounded                            bool                                    `json:"bounded"`
	AnchorMode                         string                                  `json:"anchor_mode"`
	RequestedAnchorTransitionReceiptID common.EventID                          `json:"requested_anchor_transition_receipt_id,omitempty"`
	Anchor                             TaskContinuityTransitionReceipt         `json:"anchor"`
	TransitionNeighborLimit            int                                     `json:"transition_neighbor_limit"`
	RunLimit                           int                                     `json:"run_limit"`
	RecoveryLimit                      int                                     `json:"recovery_limit"`
	ProofLimit                         int                                     `json:"proof_limit"`
	AckLimit                           int                                     `json:"ack_limit"`
	HasOlderTransitionsOutsideWindow   bool                                    `json:"has_older_transitions_outside_window"`
	HasNewerTransitionsOutsideWindow   bool                                    `json:"has_newer_transitions_outside_window"`
	WindowStartAt                      time.Time                               `json:"window_start_at,omitempty"`
	WindowEndAt                        time.Time                               `json:"window_end_at,omitempty"`
	Transitions                        []TaskContinuityTransitionReceipt       `json:"transitions,omitempty"`
	Runs                               []TaskContinuityIncidentRun             `json:"runs,omitempty"`
	RecoveryActions                    []TaskRecoveryActionRecord              `json:"recovery_actions,omitempty"`
	ProofEvents                        []TaskContinuityIncidentProof           `json:"proof_events,omitempty"`
	LatestTranscriptReview             *TaskShellTranscriptReviewMarker        `json:"latest_transcript_review,omitempty"`
	LatestTranscriptReviewGapAck       *TaskTranscriptReviewGapAcknowledgment  `json:"latest_transcript_review_gap_acknowledgment,omitempty"`
	RecentTranscriptReviewGapAcks      []TaskTranscriptReviewGapAcknowledgment `json:"recent_transcript_review_gap_acknowledgments,omitempty"`
	RiskSummary                        TaskContinuityIncidentRiskSummary       `json:"risk_summary"`
	Caveat                             string                                  `json:"caveat,omitempty"`
}

type TaskContinuityIncidentTriageRequest struct {
	TaskID                    common.TaskID  `json:"task_id"`
	AnchorMode                string         `json:"anchor_mode,omitempty"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id,omitempty"`
	Posture                   string         `json:"posture"`
	Summary                   string         `json:"summary,omitempty"`
}

type TaskContinuityIncidentTriageResponse struct {
	TaskID                            common.TaskID                          `json:"task_id"`
	AnchorMode                        string                                 `json:"anchor_mode"`
	AnchorTransitionReceiptID         common.EventID                         `json:"anchor_transition_receipt_id"`
	Posture                           string                                 `json:"posture"`
	Reused                            bool                                   `json:"reused"`
	Receipt                           TaskContinuityIncidentTriageReceipt    `json:"receipt"`
	LatestContinuityTransitionReceipt *TaskContinuityTransitionReceipt       `json:"latest_continuity_transition_receipt,omitempty"`
	RecentContinuityIncidentTriages   []TaskContinuityIncidentTriageReceipt  `json:"recent_continuity_incident_triages,omitempty"`
	ContinuityIncidentFollowUp        *TaskContinuityIncidentFollowUpSummary `json:"continuity_incident_follow_up,omitempty"`
}

type TaskContinuityIncidentTriageHistoryRequest struct {
	TaskID                    common.TaskID  `json:"task_id"`
	Limit                     int            `json:"limit,omitempty"`
	BeforeReceiptID           common.EventID `json:"before_receipt_id,omitempty"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id,omitempty"`
	Posture                   string         `json:"posture,omitempty"`
}

type TaskContinuityIncidentTriageHistoryResponse struct {
	TaskID                             common.TaskID                             `json:"task_id"`
	Bounded                            bool                                      `json:"bounded"`
	RequestedLimit                     int                                       `json:"requested_limit"`
	RequestedBeforeReceiptID           common.EventID                            `json:"requested_before_receipt_id,omitempty"`
	RequestedAnchorTransitionReceiptID common.EventID                            `json:"requested_anchor_transition_receipt_id,omitempty"`
	RequestedPosture                   string                                    `json:"requested_posture,omitempty"`
	HasMoreOlder                       bool                                      `json:"has_more_older"`
	NextBeforeReceiptID                common.EventID                            `json:"next_before_receipt_id,omitempty"`
	LatestTransitionReceiptID          common.EventID                            `json:"latest_transition_receipt_id,omitempty"`
	Latest                             *TaskContinuityIncidentTriageReceipt      `json:"latest,omitempty"`
	Receipts                           []TaskContinuityIncidentTriageReceipt     `json:"receipts,omitempty"`
	Rollup                             TaskContinuityIncidentTriageHistoryRollup `json:"rollup"`
}

type TaskContinuityIncidentFollowUpRequest struct {
	TaskID                    common.TaskID  `json:"task_id"`
	AnchorMode                string         `json:"anchor_mode,omitempty"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id,omitempty"`
	TriageReceiptID           common.EventID `json:"triage_receipt_id,omitempty"`
	ActionKind                string         `json:"action_kind"`
	Summary                   string         `json:"summary,omitempty"`
}

type TaskContinuityIncidentFollowUpResponse struct {
	TaskID                                  common.TaskID                                `json:"task_id"`
	AnchorMode                              string                                       `json:"anchor_mode"`
	AnchorTransitionReceiptID               common.EventID                               `json:"anchor_transition_receipt_id"`
	TriageReceiptID                         common.EventID                               `json:"triage_receipt_id"`
	ActionKind                              string                                       `json:"action_kind"`
	Reused                                  bool                                         `json:"reused"`
	Receipt                                 TaskContinuityIncidentFollowUpReceipt        `json:"receipt"`
	LatestContinuityTransitionReceipt       *TaskContinuityTransitionReceipt             `json:"latest_continuity_transition_receipt,omitempty"`
	LatestContinuityIncidentTriageReceipt   *TaskContinuityIncidentTriageReceipt         `json:"latest_continuity_incident_triage_receipt,omitempty"`
	RecentContinuityIncidentTriages         []TaskContinuityIncidentTriageReceipt        `json:"recent_continuity_incident_triages,omitempty"`
	LatestContinuityIncidentFollowUpReceipt *TaskContinuityIncidentFollowUpReceipt       `json:"latest_continuity_incident_follow_up_receipt,omitempty"`
	RecentContinuityIncidentFollowUps       []TaskContinuityIncidentFollowUpReceipt      `json:"recent_continuity_incident_follow_ups,omitempty"`
	ContinuityIncidentFollowUpHistoryRollup *TaskContinuityIncidentFollowUpHistoryRollup `json:"continuity_incident_follow_up_history_rollup,omitempty"`
	ContinuityIncidentFollowUp              *TaskContinuityIncidentFollowUpSummary       `json:"continuity_incident_follow_up,omitempty"`
}

type TaskContinuityIncidentFollowUpHistoryRequest struct {
	TaskID                    common.TaskID  `json:"task_id"`
	Limit                     int            `json:"limit,omitempty"`
	BeforeReceiptID           common.EventID `json:"before_receipt_id,omitempty"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id,omitempty"`
	TriageReceiptID           common.EventID `json:"triage_receipt_id,omitempty"`
	ActionKind                string         `json:"action_kind,omitempty"`
}

type TaskContinuityIncidentFollowUpHistoryResponse struct {
	TaskID                             common.TaskID                               `json:"task_id"`
	Bounded                            bool                                        `json:"bounded"`
	RequestedLimit                     int                                         `json:"requested_limit"`
	RequestedBeforeReceiptID           common.EventID                              `json:"requested_before_receipt_id,omitempty"`
	RequestedAnchorTransitionReceiptID common.EventID                              `json:"requested_anchor_transition_receipt_id,omitempty"`
	RequestedTriageReceiptID           common.EventID                              `json:"requested_triage_receipt_id,omitempty"`
	RequestedActionKind                string                                      `json:"requested_action_kind,omitempty"`
	HasMoreOlder                       bool                                        `json:"has_more_older"`
	NextBeforeReceiptID                common.EventID                              `json:"next_before_receipt_id,omitempty"`
	LatestTransitionReceiptID          common.EventID                              `json:"latest_transition_receipt_id,omitempty"`
	Latest                             *TaskContinuityIncidentFollowUpReceipt      `json:"latest,omitempty"`
	Receipts                           []TaskContinuityIncidentFollowUpReceipt     `json:"receipts,omitempty"`
	Rollup                             TaskContinuityIncidentFollowUpHistoryRollup `json:"rollup"`
}

type TaskContinuityIncidentClosureRequest struct {
	TaskID          common.TaskID  `json:"task_id"`
	Limit           int            `json:"limit,omitempty"`
	BeforeReceiptID common.EventID `json:"before_receipt_id,omitempty"`
}

type TaskContinuityIncidentClosureResponse struct {
	TaskID                    common.TaskID                               `json:"task_id"`
	Bounded                   bool                                        `json:"bounded"`
	RequestedLimit            int                                         `json:"requested_limit"`
	RequestedBeforeReceiptID  common.EventID                              `json:"requested_before_receipt_id,omitempty"`
	HasMoreOlder              bool                                        `json:"has_more_older"`
	NextBeforeReceiptID       common.EventID                              `json:"next_before_receipt_id,omitempty"`
	LatestTransitionReceiptID common.EventID                              `json:"latest_transition_receipt_id,omitempty"`
	Latest                    *TaskContinuityIncidentFollowUpReceipt      `json:"latest,omitempty"`
	Receipts                  []TaskContinuityIncidentFollowUpReceipt     `json:"receipts,omitempty"`
	Rollup                    TaskContinuityIncidentFollowUpHistoryRollup `json:"rollup"`
	FollowUp                  *TaskContinuityIncidentFollowUpSummary      `json:"follow_up,omitempty"`
	Closure                   *TaskContinuityIncidentClosureSummary       `json:"closure,omitempty"`
}

type TaskContinuityIncidentTaskRiskRequest struct {
	TaskID          common.TaskID  `json:"task_id"`
	Limit           int            `json:"limit,omitempty"`
	BeforeReceiptID common.EventID `json:"before_receipt_id,omitempty"`
}

type TaskContinuityIncidentTaskRiskResponse struct {
	TaskID                   common.TaskID                          `json:"task_id"`
	Bounded                  bool                                   `json:"bounded"`
	RequestedLimit           int                                    `json:"requested_limit"`
	RequestedBeforeReceiptID common.EventID                         `json:"requested_before_receipt_id,omitempty"`
	HasMoreOlder             bool                                   `json:"has_more_older"`
	NextBeforeReceiptID      common.EventID                         `json:"next_before_receipt_id,omitempty"`
	Summary                  *TaskContinuityIncidentTaskRiskSummary `json:"summary,omitempty"`
	Closure                  *TaskContinuityIncidentClosureSummary  `json:"closure,omitempty"`
}

type TaskShellSessionRecord struct {
	SessionID                        string                            `json:"session_id"`
	TaskID                           common.TaskID                     `json:"task_id"`
	WorkerPreference                 string                            `json:"worker_preference,omitempty"`
	ResolvedWorker                   string                            `json:"resolved_worker,omitempty"`
	WorkerSessionID                  string                            `json:"worker_session_id,omitempty"`
	WorkerSessionIDSource            string                            `json:"worker_session_id_source,omitempty"`
	AttachCapability                 string                            `json:"attach_capability,omitempty"`
	HostMode                         string                            `json:"host_mode,omitempty"`
	HostState                        string                            `json:"host_state,omitempty"`
	SessionClass                     string                            `json:"session_class,omitempty"`
	SessionClassReason               string                            `json:"session_class_reason,omitempty"`
	ReattachGuidance                 string                            `json:"reattach_guidance,omitempty"`
	OperatorSummary                  string                            `json:"operator_summary,omitempty"`
	TranscriptState                  string                            `json:"transcript_state,omitempty"`
	TranscriptRetainedChunks         int                               `json:"transcript_retained_chunks,omitempty"`
	TranscriptDroppedChunks          int                               `json:"transcript_dropped_chunks,omitempty"`
	TranscriptRetentionLimit         int                               `json:"transcript_retention_limit,omitempty"`
	TranscriptOldestSequence         int64                             `json:"transcript_oldest_retained_sequence,omitempty"`
	TranscriptNewestSequence         int64                             `json:"transcript_newest_retained_sequence,omitempty"`
	TranscriptLastChunkAt            time.Time                         `json:"transcript_last_chunk_at,omitempty"`
	TranscriptReviewID               common.EventID                    `json:"transcript_review_id,omitempty"`
	TranscriptReviewSource           string                            `json:"transcript_review_source,omitempty"`
	TranscriptReviewedUpTo           int64                             `json:"transcript_reviewed_up_to_sequence,omitempty"`
	TranscriptReviewSummary          string                            `json:"transcript_review_summary,omitempty"`
	TranscriptReviewAt               time.Time                         `json:"transcript_review_at,omitempty"`
	TranscriptReviewStale            bool                              `json:"transcript_review_stale,omitempty"`
	TranscriptReviewNewer            int                               `json:"transcript_review_newer_retained_count,omitempty"`
	TranscriptReviewClosureState     string                            `json:"transcript_review_closure_state,omitempty"`
	TranscriptReviewOldestUnreviewed int64                             `json:"transcript_review_oldest_unreviewed_sequence,omitempty"`
	TranscriptRecentReviews          []TaskShellTranscriptReviewMarker `json:"transcript_recent_reviews,omitempty"`
	StartedAt                        time.Time                         `json:"started_at"`
	LastUpdatedAt                    time.Time                         `json:"last_updated_at"`
	Active                           bool                              `json:"active"`
	Note                             string                            `json:"note,omitempty"`
	LatestEventID                    common.EventID                    `json:"latest_event_id,omitempty"`
	LatestEventKind                  string                            `json:"latest_event_kind,omitempty"`
	LatestEventAt                    time.Time                         `json:"latest_event_at,omitempty"`
	LatestEventNote                  string                            `json:"latest_event_note,omitempty"`
}

type TaskShellSessionEventRecord struct {
	EventID               common.EventID `json:"event_id"`
	TaskID                common.TaskID  `json:"task_id"`
	SessionID             string         `json:"session_id"`
	Kind                  string         `json:"kind"`
	HostMode              string         `json:"host_mode,omitempty"`
	HostState             string         `json:"host_state,omitempty"`
	WorkerSessionID       string         `json:"worker_session_id,omitempty"`
	WorkerSessionIDSource string         `json:"worker_session_id_source,omitempty"`
	AttachCapability      string         `json:"attach_capability,omitempty"`
	Active                bool           `json:"active"`
	InputLive             bool           `json:"input_live"`
	ExitCode              *int           `json:"exit_code,omitempty"`
	PaneWidth             int            `json:"pane_width,omitempty"`
	PaneHeight            int            `json:"pane_height,omitempty"`
	Note                  string         `json:"note,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
}

type TaskShellSessionReportRequest struct {
	TaskID                common.TaskID `json:"task_id"`
	SessionID             string        `json:"session_id"`
	WorkerPreference      string        `json:"worker_preference,omitempty"`
	ResolvedWorker        string        `json:"resolved_worker,omitempty"`
	WorkerSessionID       string        `json:"worker_session_id,omitempty"`
	WorkerSessionIDSource string        `json:"worker_session_id_source,omitempty"`
	AttachCapability      string        `json:"attach_capability,omitempty"`
	HostMode              string        `json:"host_mode,omitempty"`
	HostState             string        `json:"host_state,omitempty"`
	StartedAt             time.Time     `json:"started_at"`
	Active                bool          `json:"active"`
	Note                  string        `json:"note,omitempty"`
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
	ShellSessionID     string        `json:"shell_session_id,omitempty"`
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
	TaskID              common.TaskID          `json:"task_id"`
	HandoffID           string                 `json:"handoff_id"`
	TargetWorker        run.WorkerKind         `json:"target_worker"`
	LaunchStatus        string                 `json:"launch_status"`
	LaunchID            string                 `json:"launch_id,omitempty"`
	TransitionReceiptID common.EventID         `json:"transition_receipt_id,omitempty"`
	CanonicalResponse   string                 `json:"canonical_response"`
	Payload             *handoff.LaunchPayload `json:"payload,omitempty"`
}
