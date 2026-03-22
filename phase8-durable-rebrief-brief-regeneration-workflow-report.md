1. Concise diagnosis of what was missing before this phase
- Tuku could record `DECISION_REGENERATE_BRIEF`, but that only changed posture. It did not execute an actual rebrief workflow.
- The control plane could say "rebrief required" without being able to perform the durable mutation that creates a new canonical brief and returns the task to a real executable posture.
- There was no dedicated CLI/operator command for executing rebrief, no dedicated IPC/daemon route, and no proof event capturing successful brief regeneration.
- Recovery projection also lacked the precedence rule needed to let a new `BRIEF_READY` state outrank historical failed-run residue after explicit operator progression.

2. Exact implementation plan executed
- Added a first-class orchestrator mutation `ExecuteRebrief` with a dedicated request/result shape.
- Restricted rebrief execution to `REBRIEF_REQUIRED` posture with latest durable recovery action `DECISION_REGENERATE_BRIEF`.
- Built the regenerated brief from current durable state by reusing current capsule + current intent + current brief builder inputs, rather than inventing a new planning path.
- Persisted a new brief, updated capsule current brief pointer, and moved the task to `BRIEF_READY` transactionally.
- Added a new proof event `BRIEF_REGENERATED` and canonical response emission for the workflow.
- Added dedicated IPC/daemon/CLI command surface: `tuku recovery rebrief --task <id>`.
- Made replay safe by rejecting repeated rebrief execution once the task has already been advanced out of `REBRIEF_REQUIRED`.
- Updated recovery projection so current `BRIEF_READY` posture outranks stale failed-run history after successful rebrief.
- Added focused tests across orchestrator, daemon, app CLI, and shell-facing proof summary.

3. Files changed
- `/Users/kagaya/Desktop/Tuku/internal/domain/proof/types.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/orchestrator.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/rebrief.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/shell.go`
- `/Users/kagaya/Desktop/Tuku/internal/ipc/types.go`
- `/Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go`
- `/Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go`
- `/Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/app/bootstrap.go`
- `/Users/kagaya/Desktop/Tuku/internal/app/bootstrap_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service_test.go`

4. Before vs after behavior summary
- Before: `DECISION_REGENERATE_BRIEF` meant only "posture changed to rebrief required".
- After: it can be executed into a real, durable rebrief mutation that creates a new canonical brief.
- Before: there was no operator command path for executing the rebrief workflow.
- After: operators can run `tuku recovery rebrief --task <id>` end-to-end through CLI -> IPC -> daemon -> orchestrator.
- Before: successful rebrief could still look rebrief-required because historical failed-run truth kept winning in recovery projection.
- After: successful rebrief advances the task to `BRIEF_READY` / `READY_NEXT_RUN` and status/inspect reflect that correctly.
- Before: duplicate rebrief invocation could not be handled cleanly because execution did not exist.
- After: replay is safe by rejection once the task has already left `REBRIEF_REQUIRED`.

5. New rebrief / regeneration semantics introduced
- New orchestrator mutation:
  - `ExecuteRebrief(ctx, ExecuteRebriefRequest)`
- New CLI command:
  - `tuku recovery rebrief --task <id>`
- New IPC method:
  - `task.recovery.rebrief`
- New proof event:
  - `BRIEF_REGENERATED`
- Rebrief execution is allowed only when:
  - current recovery class is `REBRIEF_REQUIRED`
  - latest durable recovery action is `DECISION_REGENERATE_BRIEF`
- Rebrief input source is intentionally narrow and durable:
  - current capsule
  - current intent
  - current brief builder parameters from the current brief
- Post-rebrief canonical state:
  - new brief persisted
  - capsule current brief pointer updated
  - capsule phase moved to `BRIEF_READY`
  - recovery posture becomes `READY_NEXT_RUN`
- Replay safety rule:
  - repeat execution after success is rejected because the task is no longer in `REBRIEF_REQUIRED`
  - this avoids uncontrolled brief churn
- Truth boundary preserved:
  - rebrief means a new brief was created and current posture changed
  - it does not imply any execution has happened yet

6. Tests added or updated
- Orchestrator:
  - successful rebrief execution from valid posture
  - invalid-posture rejection
  - replay-safe repeat rejection after success
  - current brief pointer update
  - proof event emission
  - status/inspect reflect new brief and ready posture
- Daemon:
  - `task.recovery.rebrief` route mapping test
- App CLI:
  - `tuku recovery rebrief --task <id>` request routing test
  - daemon rejection surfacing test
- Shell-facing:
  - proof summary mapping updated for `BRIEF_REGENERATED`

7. Commands run
```bash
gofmt -w internal/domain/proof/types.go internal/orchestrator/orchestrator.go internal/orchestrator/rebrief.go internal/orchestrator/recovery.go internal/orchestrator/shell.go internal/ipc/types.go internal/ipc/payloads.go internal/runtime/daemon/service.go internal/runtime/daemon/service_test.go internal/app/bootstrap.go internal/app/bootstrap_test.go internal/orchestrator/service_test.go

go test ./internal/orchestrator ./internal/runtime/daemon ./internal/app ./internal/tui/shell -count=1
```

8. Remaining limitations / next risks
- Rebrief currently regenerates the brief from durable current state plus existing brief builder inputs. It does not yet incorporate a richer re-planning model or context VM.
- Repeated rebrief after success is safely rejected, not reused; that is the conservative choice for now.
- The workflow does not automatically regenerate intent or context packs.
- Rebrief remains operator-triggered; there is still no higher-level interactive recovery assistant.

9. Full code for every changed file

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
	ExecuteRebrief(ctx context.Context, req ExecuteRebriefRequest) (ExecuteRebriefResult, error)
	StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error)
	InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error)
	ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error)
	RecordShellLifecycle(ctx context.Context, req RecordShellLifecycleRequest) (RecordShellLifecycleResult, error)
	ReportShellSession(ctx context.Context, req ReportShellSessionRequest) (ReportShellSessionResult, error)
	ListShellSessions(ctx context.Context, taskID string) (ListShellSessionsResult, error)
}

```

**internal/orchestrator/rebrief.go**

```go
package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
)

type ExecuteRebriefRequest struct {
	TaskID string
}

type ExecuteRebriefResult struct {
	TaskID                common.TaskID
	PreviousBriefID       common.BriefID
	BriefID               common.BriefID
	BriefHash             string
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

func (c *Coordinator) ExecuteRebrief(ctx context.Context, req ExecuteRebriefRequest) (ExecuteRebriefResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ExecuteRebriefResult{}, fmt.Errorf("task id is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecuteRebriefResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if recovery.RecoveryClass != RecoveryClassRebriefRequired {
		return ExecuteRebriefResult{}, fmt.Errorf("rebrief can only be executed while recovery class is %s", RecoveryClassRebriefRequired)
	}
	if assessment.LatestRecoveryAction == nil || assessment.LatestRecoveryAction.Kind != recoveryaction.KindDecisionRegenerateBrief {
		return ExecuteRebriefResult{}, fmt.Errorf("rebrief requires latest recovery action %s", recoveryaction.KindDecisionRegenerateBrief)
	}

	var result ExecuteRebriefResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return fmt.Errorf("task state changed during rebrief preparation (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version)
		}

		currentBrief, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}
		intentState, err := txc.store.Intents().LatestByTask(taskID)
		if err != nil {
			return err
		}
		if caps.CurrentIntentID != "" && intentState.IntentID != caps.CurrentIntentID {
			return fmt.Errorf("latest intent %s does not match capsule current intent %s", intentState.IntentID, caps.CurrentIntentID)
		}

		nextCapsuleVersion := caps.Version + 1
		buildInput := brief.BuildInput{
			TaskID:           caps.TaskID,
			IntentID:         intentState.IntentID,
			CapsuleVersion:   nextCapsuleVersion,
			Goal:             caps.Goal,
			NormalizedAction: nonEmpty(currentBrief.NormalizedAction, intentState.NormalizedAction),
			Constraints:      append([]string{}, currentBrief.Constraints...),
			ScopeHints:       append([]string{}, currentBrief.ScopeIn...),
			ScopeOutHints:    append([]string{}, currentBrief.ScopeOut...),
			DoneCriteria:     append([]string{}, currentBrief.DoneCriteria...),
			ContextPackID:    currentBrief.ContextPackID,
			Verbosity:        currentBrief.Verbosity,
			PolicyProfileID:  currentBrief.PolicyProfileID,
		}
		if len(buildInput.Constraints) == 0 {
			buildInput.Constraints = append([]string{}, caps.Constraints...)
		}
		if len(buildInput.ScopeHints) == 0 {
			buildInput.ScopeHints = append([]string{}, caps.TouchedFiles...)
		}
		if len(buildInput.DoneCriteria) == 0 {
			buildInput.DoneCriteria = []string{"Execution plan is prepared and ready for worker dispatch"}
		}
		if buildInput.Verbosity == "" {
			buildInput.Verbosity = brief.VerbosityStandard
		}
		if strings.TrimSpace(buildInput.PolicyProfileID) == "" {
			buildInput.PolicyProfileID = "default-safe-v1"
		}
		newBrief, err := txc.briefBuilder.Build(buildInput)
		if err != nil {
			return err
		}
		if err := txc.store.Briefs().Save(newBrief); err != nil {
			return err
		}

		caps.Version = nextCapsuleVersion
		caps.UpdatedAt = txc.clock()
		caps.CurrentBriefID = newBrief.BriefID
		caps.CurrentPhase = phase.PhaseBriefReady
		caps.NextAction = "Execution brief regenerated. Start a run with `tuku run --task <id>`."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		runID := recoveryActionRunID(*assessment.LatestRecoveryAction)
		if err := txc.appendProof(caps, proof.EventBriefRegenerated, proof.ActorSystem, "tuku-brief-builder", map[string]any{
			"previous_brief_id":      currentBrief.BriefID,
			"previous_brief_hash":    currentBrief.BriefHash,
			"new_brief_id":           newBrief.BriefID,
			"new_brief_hash":         newBrief.BriefHash,
			"trigger_action_id":      assessment.LatestRecoveryAction.ActionID,
			"trigger_action_kind":    assessment.LatestRecoveryAction.Kind,
			"trigger_action_summary": assessment.LatestRecoveryAction.Summary,
		}, runID); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
			"phase":  caps.CurrentPhase,
			"reason": "rebrief executed from durable operator decision",
		}, runID); err != nil {
			return err
		}

		afterAssessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		afterRecovery := txc.recoveryFromContinueAssessment(afterAssessment)
		canonical := fmt.Sprintf(
			"I regenerated the execution brief after operator decision %s. New brief %s is now canonical current state. Recovery posture is %s, and the next recommended action is %s.",
			assessment.LatestRecoveryAction.ActionID,
			newBrief.BriefID,
			afterRecovery.RecoveryClass,
			afterRecovery.RecommendedAction,
		)
		if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{
			"previous_brief_id":        currentBrief.BriefID,
			"new_brief_id":             newBrief.BriefID,
			"new_brief_hash":           newBrief.BriefHash,
			"recovery_class":           afterRecovery.RecoveryClass,
			"recommended_action":       afterRecovery.RecommendedAction,
			"ready_for_next_run":       afterRecovery.ReadyForNextRun,
			"ready_for_handoff_launch": afterRecovery.ReadyForHandoffLaunch,
		}, runID); err != nil {
			return err
		}

		result = ExecuteRebriefResult{
			TaskID:                taskID,
			PreviousBriefID:       currentBrief.BriefID,
			BriefID:               newBrief.BriefID,
			BriefHash:             newBrief.BriefHash,
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
		return ExecuteRebriefResult{}, err
	}
	return result, nil
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

	if assessment.Capsule.CurrentPhase == phase.PhaseBriefReady {
		recovery.RecoveryClass = RecoveryClassReadyNextRun
		recovery.RecommendedAction = RecoveryActionStartNextRun
		recovery.ReadyForNextRun = true
		recovery.Reason = fmt.Sprintf("task is ready for the next bounded run with brief %s", assessment.Capsule.CurrentBriefID)
		return applyRecoveryActionProgression(recovery)
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
		if recovery.RecoveryClass == RecoveryClassReadyNextRun {
			recovery.Reason = fmt.Sprintf("execution brief was regenerated after operator decision: %s", action.Summary)
			return recovery
		}
		recovery.RecoveryClass = RecoveryClassRebriefRequired
		recovery.RecommendedAction = RecoveryActionRegenerateBrief
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.RequiresReconciliation = false
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

**internal/orchestrator/shell.go**

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
	MethodExecuteRebrief          Method = "task.recovery.rebrief"
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

type TaskRebriefRequest struct {
	TaskID common.TaskID `json:"task_id"`
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
	executeRebriefFn          func(context.Context, orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error)
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

func (f *fakeOrchestratorService) ExecuteRebrief(ctx context.Context, req orchestrator.ExecuteRebriefRequest) (orchestrator.ExecuteRebriefResult, error) {
	if f.executeRebriefFn != nil {
		return f.executeRebriefFn(ctx, req)
	}
	return orchestrator.ExecuteRebriefResult{}, nil
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

**internal/app/bootstrap.go**

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
			return errors.New("usage: tuku recovery <record|rebrief> ...")
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
		default:
			return fmt.Errorf("unknown recovery command: %s", args[1])
		}

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
	return "usage: tuku [chat] | tuku <start|message|shell|shell-sessions|run|continue|checkpoint|recovery|handoff-create|handoff-accept|handoff-launch|status|inspect|help> [flags]"
}

func parseRecoveryActionKind(value string) (recoveryaction.Kind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "failed-run-reviewed":
		return recoveryaction.KindFailedRunReviewed, nil
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

**internal/app/bootstrap_test.go**

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
