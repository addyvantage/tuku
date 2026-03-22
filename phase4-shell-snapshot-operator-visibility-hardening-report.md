1. Concise diagnosis of what was weak before this phase
- Shell snapshot only exposed shallow run/checkpoint/handoff/ack data and then patched checkpoint resumability ad hoc.
- The strongest operator truth already existed in status/inspect, but shell-facing transport could not carry recovery assessment, continuity issues, launch-control state, or latest launch metadata.
- Raw checkpoint resumability and operational readiness were still easy to conflate in shell-facing surfaces.
- The shell header continuity label could still overclaim by reading raw checkpoint resumability without considering recovery truth.

2. Exact implementation plan you executed
- Extended orchestrator shell snapshot models with recovery summary, launch-control summary, and latest launch summary.
- Stopped mutating shell checkpoint resumability from continuity assessment; shell snapshot now keeps raw checkpoint state and exposes assessed readiness separately through recovery.
- Threaded the richer shell snapshot data through IPC payloads and daemon shell snapshot routing.
- Extended the shell-side snapshot model and IPC adapter mapping to carry the new truth without adding new orchestration commands.
- Made the shell continuity label prefer recovery truth over raw checkpoint resumability so it does not overclaim on failed/review/repair states.
- Added failure-oriented tests across orchestrator shell snapshot, daemon shell snapshot payloads, and shell adapter/viewmodel mapping.

3. Files changed
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/shell.go
- /Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go
- /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go
- /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service_test.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/types.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/adapter.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel.go
- /Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel_test.go
- /Users/kagaya/Desktop/Tuku/internal/orchestrator/shell_test.go

4. Before vs after behavior summary
- Before: shell snapshot mostly mirrored persisted basics and hid the stronger recovery/launch truth.
- After: shell snapshot carries recovery class/action/readiness, continuity issues, latest launch attempt summary, and launch-control state/disposition.
- Before: shell snapshot rewrote checkpoint resumability heuristically.
- After: shell snapshot preserves raw checkpoint resumability and exposes operational readiness separately.
- Before: shell-facing continuity labels could overclaim because they only looked at raw checkpoint resumability.
- After: the shell continuity label prefers recovery truth and will show review/repair/decision/launch-pending states instead of generic “resumable” when appropriate.

5. New shell snapshot / operator truth semantics introduced
- Shell snapshot now includes:
  - recovery summary
  - continuity outcome inside recovery summary
  - continuity issue list inside recovery summary
  - recovery class
  - recovery action
  - `ready_for_next_run`
  - `ready_for_handoff_launch`
  - latest launch summary
  - launch-control state
  - launch retry disposition
  - explicit reason strings for recovery and launch control
- Raw checkpoint resumability remains separate from assessed operational readiness.
- Shell snapshot now cleanly distinguishes:
  - raw checkpoint resumability
  - assessed next-run readiness
  - accepted handoff launch readiness
  - launch pending unknown outcome
  - completed launch step
  - blocked/repair/review/decision states
- Completed launch shell truth remains narrow: launch completion means launcher invocation completed, not downstream worker completion.

6. Tests added or updated
- Added orchestrator shell snapshot coverage for:
  - interrupted recoverable state
  - failed run review-required state
  - accepted handoff launch-ready state
  - pending launch unknown-outcome state
  - completed launch state
  - broken continuity / repair-required state
- Updated daemon shell snapshot route test to assert recovery and launch-control payload mapping.
- Updated shell adapter snapshot mapping test to assert launch, launch-control, and recovery mapping.
- Added a viewmodel continuity-label regression test to ensure recovery truth overrides raw checkpoint resumability.

7. Commands run
```bash
gofmt -w internal/orchestrator/shell.go internal/ipc/payloads.go internal/runtime/daemon/service.go internal/runtime/daemon/service_test.go internal/tui/shell/types.go internal/tui/shell/adapter.go internal/tui/shell/viewmodel.go internal/tui/shell/viewmodel_test.go internal/orchestrator/shell_test.go
go test ./internal/orchestrator ./internal/runtime/daemon ./internal/tui/shell ./internal/app -count=1
```

8. Remaining limitations / next risks
- Shell rendering itself still uses only a small slice of the richer snapshot; this phase made the data available and corrected the continuity label, but it did not redesign the UI.
- Recovery and launch-control are still projected on read, not durably persisted as a consolidated assessment object.
- Launch completion still only proves launcher invocation completion, not downstream worker execution completion.
- Shell snapshot still carries summaries rather than a generic operator event/attempt model.

9. Full code for every changed file

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/shell.go
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
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
)

type ShellSnapshotResult struct {
	TaskID                  common.TaskID
	Goal                    string
	Phase                   string
	Status                  string
	RepoAnchor              anchorgit.Snapshot
	IntentClass             string
	IntentSummary           string
	Brief                   *ShellBriefSummary
	Run                     *ShellRunSummary
	Checkpoint              *ShellCheckpointSummary
	Handoff                 *ShellHandoffSummary
	Launch                  *ShellLaunchSummary
	LaunchControl           *ShellLaunchControlSummary
	Acknowledgment          *ShellAcknowledgmentSummary
	Recovery                *ShellRecoverySummary
	RecentProofs            []ShellProofSummary
	RecentConversation      []ShellConversationSummary
	LatestCanonicalResponse string
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
		if launch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.Launch = &ShellLaunchSummary{
				AttemptID:         launch.AttemptID,
				LaunchID:          launch.LaunchID,
				Status:            launch.Status,
				RequestedAt:       launch.RequestedAt,
				StartedAt:         launch.StartedAt,
				EndedAt:           launch.EndedAt,
				Summary:           launch.Summary,
				ErrorMessage:      launch.ErrorMessage,
				OutputArtifactRef: launch.OutputArtifactRef,
			}
		}
		if ack, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID); err != nil {
			if err != sql.ErrNoRows {
				return ShellSnapshotResult{}, err
			}
		} else {
			result.Acknowledgment = &ShellAcknowledgmentSummary{
				Status:    ack.Status,
				Summary:   ack.Summary,
				CreatedAt: ack.CreatedAt,
			}
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
		recovery := c.recoveryFromContinueAssessment(assessment)
		result.Recovery = shellRecoverySummary(recovery)
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

File: /Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service_test.go
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

func (f *fakeOrchestratorService) StatusTask(_ context.Context, _ string) (orchestrator.StatusTaskResult, error) {
	return orchestrator.StatusTaskResult{
		Phase:                   phase.PhaseIntake,
		LatestCheckpointTrigger: checkpoint.TriggerManual,
	}, nil
}

func (f *fakeOrchestratorService) InspectTask(_ context.Context, _ string) (orchestrator.InspectTaskResult, error) {
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

File: /Users/kagaya/Desktop/Tuku/internal/tui/shell/types.go
```go
package shell

import (
	"context"
	"time"
)

type Snapshot struct {
	TaskID                  string
	Goal                    string
	Phase                   string
	Status                  string
	Repo                    RepoAnchor
	LocalScratch            *LocalScratchContext
	IntentClass             string
	IntentSummary           string
	Brief                   *BriefSummary
	Run                     *RunSummary
	Checkpoint              *CheckpointSummary
	Handoff                 *HandoffSummary
	Launch                  *LaunchSummary
	LaunchControl           *LaunchControlSummary
	Acknowledgment          *AcknowledgmentSummary
	Recovery                *RecoverySummary
	RecentProofs            []ProofItem
	RecentConversation      []ConversationItem
	LatestCanonicalResponse string
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
	SessionEventShellStarted               SessionEventType = "shell_started"
	SessionEventHostStartupAttempted       SessionEventType = "host_startup_attempted"
	SessionEventHostLive                   SessionEventType = "host_live"
	SessionEventResizeApplied              SessionEventType = "resize_applied"
	SessionEventHostExited                 SessionEventType = "host_exited"
	SessionEventHostFailed                 SessionEventType = "host_failed"
	SessionEventFallbackActivated          SessionEventType = "fallback_activated"
	SessionEventManualRefresh              SessionEventType = "manual_refresh"
	SessionEventPendingMessageStaged       SessionEventType = "pending_message_staged"
	SessionEventPendingMessageEditStarted  SessionEventType = "pending_message_edit_started"
	SessionEventPendingMessageEditSaved    SessionEventType = "pending_message_edit_saved"
	SessionEventPendingMessageEditCanceled SessionEventType = "pending_message_edit_canceled"
	SessionEventPendingMessageSent         SessionEventType = "pending_message_sent"
	SessionEventPendingMessageCleared      SessionEventType = "pending_message_cleared"
	SessionEventPriorPersistedProof        SessionEventType = "prior_persisted_proof"
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

File: /Users/kagaya/Desktop/Tuku/internal/tui/shell/adapter.go
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

File: /Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel.go
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
				{Title: "worker session", Lines: inspectorWorkerSession(host, ui.Session)},
				{Title: "brief", Lines: inspectorBrief(snapshot)},
				{Title: "intent", Lines: inspectorIntent(snapshot)},
				{Title: "pending message", Lines: inspectorPendingMessage(snapshot, ui)},
				{Title: "checkpoint", Lines: inspectorCheckpoint(snapshot)},
				{Title: "handoff", Lines: inspectorHandoff(snapshot)},
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
				fmt.Sprintf("registry %s", sessionRegistrySummary(ui.Session)),
				fmt.Sprintf("phase %s", nonEmpty(snapshot.Phase, "UNKNOWN")),
				fmt.Sprintf("worker %s", effectiveWorkerLabel(snapshot, host)),
				fmt.Sprintf("host %s", hostStatusLine(snapshot, ui, host)),
				fmt.Sprintf("repo %s", repoLabel(snapshot.Repo)),
				fmt.Sprintf("continuity %s", continuityLabel(snapshot)),
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
		case "READY_NEXT_RUN", "INTERRUPTED_RUN_RECOVERABLE":
			if snapshot.Recovery.ReadyForNextRun {
				return "resumable"
			}
		case "ACCEPTED_HANDOFF_LAUNCH_READY":
			return "handoff-ready"
		case "HANDOFF_LAUNCH_PENDING_OUTCOME":
			return "launch-pending"
		case "HANDOFF_LAUNCH_COMPLETED":
			return "launched"
		case "FAILED_RUN_REVIEW_REQUIRED", "VALIDATION_REVIEW_REQUIRED":
			return "review"
		case "DECISION_REQUIRED", "BLOCKED_DRIFT":
			return "decision"
		case "REPAIR_REQUIRED":
			return "repair"
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
	if snapshot.Checkpoint.ResumeDescriptor != "" {
		lines = append(lines, snapshot.Checkpoint.ResumeDescriptor)
	}
	if snapshot.Checkpoint.IsResumable {
		lines = append(lines, "resume-safe")
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
		return label + " resumable"
	}
	return label
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
	if cue == "" {
		return label
	}
	return label + " | " + cue
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

File: /Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel_test.go
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
	if vm.Header.Continuity != "resumable" {
		t.Fatalf("expected resumable continuity, got %q", vm.Header.Continuity)
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
	if vm.Inspector.Sections[0].Title != "worker session" {
		t.Fatalf("expected worker session section first, got %q", vm.Inspector.Sections[0].Title)
	}
	foundSessionLine := false
	for _, line := range vm.Inspector.Sections[0].Lines {
		if strings.Contains(line, "new shell session shs_1234567890") {
			foundSessionLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected worker-session inspector to include session id, got %#v", vm.Inspector.Sections[0].Lines)
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
		TaskID:                  "tsk_overlay",
		Phase:                   "EXECUTING",
		Status:                  "ACTIVE",
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
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "new shell session shs_overlay") {
			foundSessionLine = true
		}
		if strings.Contains(line, "previous shell outcome Shell transcript fallback activated") {
			foundPriorLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected session id in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundPriorLine {
		t.Fatalf("expected previous shell outcome in overlay, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "read-only") {
		t.Fatalf("expected footer to clarify read-only fallback, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "fallback active") {
		t.Fatalf("expected footer to include short fallback cue, got %q", vm.Footer)
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
	if !strings.Contains(vm.WorkerPane.Lines[0], "transcript fallback | historical transcript below | fallback active") {
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

File: /Users/kagaya/Desktop/Tuku/internal/orchestrator/shell_test.go
```go
package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	rundomain "tuku/internal/domain/run"
)

func TestShellSnapshotTaskBuildsShellStateFromPersistedTaskState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Mode:         handoff.ModeResume,
		Reason:       "shell test",
	}); err != nil {
		t.Fatalf("create handoff: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.TaskID != taskID {
		t.Fatalf("expected task id %s, got %s", taskID, snapshot.TaskID)
	}
	if snapshot.Brief == nil || snapshot.Brief.BriefID == "" {
		t.Fatal("expected brief summary in shell snapshot")
	}
	if snapshot.Run == nil || snapshot.Run.RunID != runRes.RunID {
		t.Fatalf("expected latest run summary, got %+v", snapshot.Run)
	}
	if snapshot.Checkpoint == nil || snapshot.Checkpoint.CheckpointID == "" {
		t.Fatal("expected checkpoint summary in shell snapshot")
	}
	if snapshot.Handoff == nil || snapshot.Handoff.TargetWorker != rundomain.WorkerKindClaude {
		t.Fatalf("expected latest handoff summary, got %+v", snapshot.Handoff)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary in shell snapshot")
	}
	if snapshot.LaunchControl == nil {
		t.Fatal("expected launch control summary in shell snapshot")
	}
	if len(snapshot.RecentProofs) == 0 {
		t.Fatal("expected proof highlights")
	}
	if len(snapshot.RecentConversation) == 0 {
		t.Fatal("expected recent conversation")
	}
	if snapshot.LatestCanonicalResponse == "" {
		t.Fatal("expected latest canonical response")
	}
}

func TestShellSnapshotInterruptedRunExposesRecoverableState(t *testing.T) {
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary")
	}
	if snapshot.Recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recoverable class, got %s", snapshot.Recovery.RecoveryClass)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected interrupted recovery action, got %s", snapshot.Recovery.RecommendedAction)
	}
	if !snapshot.Recovery.ReadyForNextRun {
		t.Fatal("expected interrupted recovery to be next-run ready")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotApplicable {
		t.Fatalf("expected non-applicable launch control, got %+v", snapshot.LaunchControl)
	}
}

func TestShellSnapshotFailedRunDoesNotOverclaimReadiness(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Checkpoint == nil {
		t.Fatal("expected checkpoint summary")
	}
	if snapshot.Checkpoint.IsResumable {
		t.Fatalf("failed run checkpoint should not look resumable: %+v", snapshot.Checkpoint)
	}
	if snapshot.Recovery == nil {
		t.Fatal("expected recovery summary")
	}
	if snapshot.Recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run recovery class, got %s", snapshot.Recovery.RecoveryClass)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("failed run should not look ready for next run")
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionInspectFailedRun {
		t.Fatalf("expected inspect-failed-run action, got %s", snapshot.Recovery.RecommendedAction)
	}
}

func TestShellSnapshotAcceptedHandoffLaunchReadyState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "launch ready shell snapshot",
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassAcceptedHandoffLaunchReady {
		t.Fatalf("expected accepted handoff launch-ready recovery, got %+v", snapshot.Recovery)
	}
	if !snapshot.Recovery.ReadyForHandoffLaunch {
		t.Fatal("expected handoff launch readiness")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotRequested {
		t.Fatalf("expected not-requested launch control state, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionAllowed {
		t.Fatalf("expected allowed launch retry disposition, got %+v", snapshot.LaunchControl)
	}
}

func TestShellSnapshotPendingLaunchUnknownOutcomeState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "pending launch shell snapshot",
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
		AttemptID:    "hlc_shell_pending",
		HandoffID:    createOut.HandoffID,
		TaskID:       taskID,
		TargetWorker: rundomain.WorkerKindClaude,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  "shell_pending_hash",
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create launch record: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
		t.Fatalf("expected pending launch recovery class, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.ReadyForNextRun {
		t.Fatal("pending launch should not look ready for next run")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateRequestedOutcomeUnknown {
		t.Fatalf("expected requested-outcome-unknown launch control, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked launch retry, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Launch == nil || snapshot.Launch.Status != handoff.LaunchStatusRequested {
		t.Fatalf("expected latest launch summary, got %+v", snapshot.Launch)
	}
}

func TestShellSnapshotCompletedLaunchStateDoesNotOverclaimDownstreamCompletion(t *testing.T) {
	store := newTestStore(t)
	launcher := newFakeHandoffLauncherSuccess()
	coord := newTestCoordinatorWithLauncher(t, store, defaultAnchor(), newFakeAdapterSuccess(), launcher)
	taskID := setupTaskWithBrief(t, coord)

	createOut, err := coord.CreateHandoff(context.Background(), CreateHandoffRequest{
		TaskID:       string(taskID),
		TargetWorker: rundomain.WorkerKindClaude,
		Reason:       "completed launch shell snapshot",
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

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassHandoffLaunchCompleted {
		t.Fatalf("expected completed launch recovery class, got %+v", snapshot.Recovery)
	}
	if snapshot.Recovery.RecommendedAction != RecoveryActionMonitorLaunchedHandoff {
		t.Fatalf("expected monitor launched handoff action, got %+v", snapshot.Recovery)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateCompleted {
		t.Fatalf("expected completed launch control, got %+v", snapshot.LaunchControl)
	}
	if snapshot.LaunchControl.RetryDisposition != LaunchRetryDispositionBlocked {
		t.Fatalf("expected blocked retry after durable completion, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Acknowledgment == nil {
		t.Fatal("expected persisted acknowledgment after completed launch")
	}
	if snapshot.Recovery.Reason == "" || !strings.Contains(strings.ToLower(snapshot.Recovery.Reason), "launch") {
		t.Fatalf("expected launch-specific recovery reason, got %+v", snapshot.Recovery)
	}
}

func TestShellSnapshotBrokenContinuityExposesRepairIssues(t *testing.T) {
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
		CheckpointID:       common.CheckpointID("chk_shell_bad_brief"),
		TaskID:             taskID,
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_shell"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "broken shell continuity test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	snapshot, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot: %v", err)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery class, got %+v", snapshot.Recovery)
	}
	if len(snapshot.Recovery.Issues) == 0 {
		t.Fatal("expected continuity issues in shell snapshot")
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State != LaunchControlStateNotApplicable {
		t.Fatalf("expected non-applicable launch control in broken continuity state, got %+v", snapshot.LaunchControl)
	}
}
```
