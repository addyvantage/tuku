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
