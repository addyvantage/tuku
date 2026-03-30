1. Concise diagnosis of what was weak before this phase
- Phase 1 made continuity contradictions explicit, but it still answered mostly a continuity-validity question. It did not give Tuku a first-class, operator-usable recovery model.
- `ContinueTask` could still return a continuity-safe outcome even when the task was not actually ready to start the next bounded run. Failed execution was the clearest example: continuity might be coherent while operational recovery still required review.
- `InspectTask` remained too raw. It exposed latest objects, but not the actual recovery classification, recommended action, or the full set of continuity issues that justified a blocked/repair state.
- Status transport exposed resumability without separating checkpoint resumability from next-run readiness.
- Failed execution checkpoints and follow-on handoff behavior were still too optimistic for a recovery-control-plane product.

2. Exact implementation plan you executed
- Added a first-class recovery assessment layer on top of continuity assessment.
- Defined explicit recovery classifications and recommended recovery actions.
- Threaded recovery semantics into `ContinueTask`, `StatusTask`, and `InspectTask`.
- Enriched inspect output with latest handoff, latest acknowledgment, recovery assessment, and continuity issues.
- Split raw checkpoint resumability from assessed next-run readiness in status output.
- Tightened failed-run truth by making failed execution checkpoints non-resumable and by treating failed execution as review-required, not next-run-ready.
- Blocked creation of fresh resumable handoff checkpoints from blocked recovery state.
- Added focused tests for interrupted recovery, failed recovery, accepted-handoff launch readiness, broken handoff inspectability, and failed-run handoff blocking.

3. Files changed
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/continuity.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff.go`
- `/Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go`
- `/Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_test.go`

4. Before vs after behavior summary
- Before: continuity-safe and next-run-ready were effectively conflated in many paths.
- After: Tuku separately reports continuity outcome, recovery class, recommended action, and readiness for next run or handoff launch.
- Before: failed execution could still look like resumable continuation territory.
- After: failed execution is explicitly `FAILED_RUN_REVIEW_REQUIRED`, not ready for next run, and its checkpoint no longer claims resumability.
- Before: interrupted execution and accepted handoff continuation were not modeled as distinct operational recovery states.
- After: interrupted recovery and accepted-handoff launch readiness are explicit, typed recovery states.
- Before: inspect output was object-oriented but not operationally explanatory.
- After: inspect output includes latest handoff/ack plus a recovery assessment with exact issues and next action guidance.
- Before: a blocked failed task could still try to mint a fresh resumable handoff checkpoint.
- After: blocked recovery state no longer creates fresh resumable handoff continuity.

5. New recovery / resume semantics introduced
- Added `RecoveryClass` with explicit categories:
  - `READY_NEXT_RUN`
  - `INTERRUPTED_RUN_RECOVERABLE`
  - `ACCEPTED_HANDOFF_LAUNCH_READY`
  - `FAILED_RUN_REVIEW_REQUIRED`
  - `VALIDATION_REVIEW_REQUIRED`
  - `STALE_RUN_RECONCILIATION_REQUIRED`
  - `DECISION_REQUIRED`
  - `BLOCKED_DRIFT`
  - `REPAIR_REQUIRED`
  - `COMPLETED_NO_ACTION`
- Added `RecoveryAction` with explicit operator/daemon next steps:
  - `START_NEXT_RUN`
  - `RESUME_INTERRUPTED_RUN`
  - `LAUNCH_ACCEPTED_HANDOFF`
  - `INSPECT_FAILED_RUN`
  - `REVIEW_VALIDATION_STATE`
  - `RECONCILE_STALE_RUN`
  - `MAKE_RESUME_DECISION`
  - `REPAIR_CONTINUITY`
  - `NONE`
- Added `RecoveryAssessment` as a deterministic projection from continuity state to operational recovery truth.
- `ContinueTask` now distinguishes continuity-safe-but-not-ready states from genuine next-run readiness.
- `StatusTask` now reports both `CheckpointResumable` and assessed readiness (`ReadyForNextRun`, `ReadyForHandoffLaunch`) instead of forcing consumers to infer everything from one boolean.
- `InspectTask` now exposes recovery assessment and issue lists so a human can see the exact reason recovery is ready, blocked, or review-gated.

6. Tests added or updated
- Added interrupted recovery readiness test.
- Added failed-run review-required recovery test.
- Added inspect-time recovery issue surfacing test for broken handoff state.
- Added accepted handoff launch-ready recovery test.
- Added failed-run handoff block regression test.
- Re-ran orchestrator, daemon, and app package tests.

7. Commands run
```bash
gofmt -w internal/orchestrator/recovery.go internal/orchestrator/service.go internal/orchestrator/continuity.go internal/orchestrator/handoff.go internal/ipc/payloads.go internal/runtime/daemon/service.go internal/orchestrator/service_test.go internal/orchestrator/handoff_test.go
go test ./internal/orchestrator -count=1
go test ./internal/orchestrator ./internal/runtime/daemon ./internal/app -count=1
```

8. Remaining limitations / next risks
- Recovery assessment is currently derived in memory from persisted state; there is still no separately persisted recovery-state table.
- `ContinueOutcome` still represents continuity semantics, while recovery readiness is now carried in additional fields. That separation is explicit, but not yet fully normalized into a dedicated external API surface.
- Launch replay state still relies on proof-history scan rather than a dedicated launch state machine.
- Recovery actions are deterministic and typed, but Tuku still does not execute automatic repair/regeneration flows.
- Shell snapshot transport was not expanded in this phase; `status` and `inspect` are now the strongest operator-facing recovery surfaces.

9. Full code for every changed file

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go`

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
	RecoveryActionNone                  RecoveryAction = "NONE"
	RecoveryActionStartNextRun          RecoveryAction = "START_NEXT_RUN"
	RecoveryActionResumeInterrupted     RecoveryAction = "RESUME_INTERRUPTED_RUN"
	RecoveryActionLaunchAcceptedHandoff RecoveryAction = "LAUNCH_ACCEPTED_HANDOFF"
	RecoveryActionInspectFailedRun      RecoveryAction = "INSPECT_FAILED_RUN"
	RecoveryActionReviewValidation      RecoveryAction = "REVIEW_VALIDATION_STATE"
	RecoveryActionReconcileStaleRun     RecoveryAction = "RECONCILE_STALE_RUN"
	RecoveryActionMakeResumeDecision    RecoveryAction = "MAKE_RESUME_DECISION"
	RecoveryActionRepairContinuity      RecoveryAction = "REPAIR_CONTINUITY"
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
		recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
		recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
		recovery.ReadyForHandoffLaunch = true
		recovery.Reason = fmt.Sprintf("accepted handoff %s is ready to launch for %s", packet.HandoffID, packet.TargetWorker)
		return recovery
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

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go`

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
	Acknowledgment *handoff.Acknowledgment
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

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/continuity.go`

```go
package orchestrator

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
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
	violations := make([]continuityViolation, 0, 6)

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

func decodeProofPayload(event proof.Event) map[string]any {
	if strings.TrimSpace(event.PayloadJSON) == "" {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func proofPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func proofPayloadMatchesHandoff(event proof.Event, handoffID string) bool {
	if handoffID == "" {
		return false
	}
	return proofPayloadString(decodeProofPayload(event), "handoff_id") == handoffID
}

func (c *Coordinator) latestHandoffLaunchEvent(taskID common.TaskID, handoffID string) (*proof.Event, map[string]any, error) {
	events, err := c.store.Proofs().ListByTask(taskID, 1000)
	if err != nil {
		return nil, nil, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		evt := events[i]
		switch evt.Type {
		case proof.EventHandoffLaunchRequested, proof.EventHandoffLaunchCompleted, proof.EventHandoffLaunchFailed:
			if !proofPayloadMatchesHandoff(evt, handoffID) {
				continue
			}
			payload := decodeProofPayload(evt)
			evtCopy := evt
			return &evtCopy, payload, nil
		}
	}
	return nil, nil, nil
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

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff.go`

```go
package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
)

type CreateHandoffRequest struct {
	TaskID       string
	TargetWorker rundomain.WorkerKind
	Reason       string
	Mode         handoff.Mode
	Notes        []string
}

type CreateHandoffResult struct {
	TaskID            common.TaskID
	HandoffID         string
	SourceWorker      rundomain.WorkerKind
	TargetWorker      rundomain.WorkerKind
	Status            handoff.Status
	CheckpointID      common.CheckpointID
	BriefID           common.BriefID
	CanonicalResponse string
	Packet            *handoff.Packet
}

type AcceptHandoffRequest struct {
	TaskID     string
	HandoffID  string
	AcceptedBy rundomain.WorkerKind
	Notes      []string
}

type AcceptHandoffResult struct {
	TaskID            common.TaskID
	HandoffID         string
	Status            handoff.Status
	CanonicalResponse string
}

func (c *Coordinator) CreateHandoff(ctx context.Context, req CreateHandoffRequest) (CreateHandoffResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return CreateHandoffResult{}, fmt.Errorf("task id is required")
	}

	targetWorker := req.TargetWorker
	if targetWorker == "" {
		targetWorker = rundomain.WorkerKindClaude
	}
	if targetWorker != rundomain.WorkerKindClaude {
		return CreateHandoffResult{}, fmt.Errorf("unsupported handoff target worker: %s", targetWorker)
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return CreateHandoffResult{}, err
	}
	if blockedReason, err := c.validateHandoffSafety(assessment); err != nil {
		return CreateHandoffResult{}, err
	} else if blockedReason != "" {
		return c.recordBlockedHandoffReason(ctx, taskID, blockedReason, targetWorker, req)
	}

	var result CreateHandoffResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return txc.recordBlockedHandoffTx(caps, "task state changed during handoff assessment", targetWorker, req, &result)
		}
		b, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			if err == sql.ErrNoRows {
				return txc.recordBlockedHandoffTx(caps, fmt.Sprintf("brief %s not found", caps.CurrentBriefID), targetWorker, req, &result)
			}
			return err
		}

		if reused, ok, err := txc.tryReuseExistingHandoff(taskID, caps, assessment, targetWorker, req); err != nil {
			return err
		} else if ok {
			result = reused
			return nil
		}

		var cp checkpoint.Checkpoint
		if assessment.LatestCheckpoint != nil {
			latestCP, err := txc.store.Checkpoints().LatestByTask(taskID)
			if err != nil {
				return err
			}
			if txc.canReuseHandoffCheckpoint(caps, assessment, latestCP) {
				cp = latestCP
			}
		}
		if cp.CheckpointID == "" {
			runID := common.RunID("")
			if assessment.LatestRun != nil {
				runID = assessment.LatestRun.RunID
			}
			newCP, err := txc.createCheckpoint(caps, runID, checkpoint.TriggerHandoff, true, "Checkpoint created for cross-worker handoff packet generation.")
			if err != nil {
				return err
			}
			cp = newCP
		}

		var latestRun *rundomain.ExecutionRun
		if assessment.LatestRun != nil {
			runCopy := *assessment.LatestRun
			latestRun = &runCopy
		}
		packet := txc.buildHandoffPacket(caps, b, cp, latestRun, targetWorker, req)
		if err := txc.store.Handoffs().Create(packet); err != nil {
			return err
		}

		payload := map[string]any{
			"handoff_id":    packet.HandoffID,
			"source_worker": packet.SourceWorker,
			"target_worker": packet.TargetWorker,
			"checkpoint_id": packet.CheckpointID,
			"brief_id":      packet.BriefID,
			"mode":          packet.HandoffMode,
			"reason":        packet.Reason,
			"is_resumable":  packet.IsResumable,
		}
		runIDPtr := runIDPointer(packet.LatestRunID)
		if err := txc.appendProof(caps, proof.EventHandoffCreated, proof.ActorSystem, "tuku-daemon", payload, runIDPtr); err != nil {
			return err
		}

		canonical := fmt.Sprintf(
			"I created handoff packet %s for %s. It is anchored to checkpoint %s and brief %s on branch %s (head %s).",
			packet.HandoffID,
			packet.TargetWorker,
			packet.CheckpointID,
			packet.BriefID,
			packet.RepoAnchor.BranchName,
			packet.RepoAnchor.HeadSHA,
		)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runIDPtr); err != nil {
			return err
		}

		packetCopy := packet
		result = CreateHandoffResult{
			TaskID:            packet.TaskID,
			HandoffID:         packet.HandoffID,
			SourceWorker:      packet.SourceWorker,
			TargetWorker:      packet.TargetWorker,
			Status:            packet.Status,
			CheckpointID:      packet.CheckpointID,
			BriefID:           packet.BriefID,
			CanonicalResponse: canonical,
			Packet:            &packetCopy,
		}
		return nil
	})
	if err != nil {
		return CreateHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) AcceptHandoff(ctx context.Context, req AcceptHandoffRequest) (AcceptHandoffResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	handoffID := strings.TrimSpace(req.HandoffID)
	if taskID == "" {
		return AcceptHandoffResult{}, fmt.Errorf("task id is required")
	}
	if handoffID == "" {
		return AcceptHandoffResult{}, fmt.Errorf("handoff id is required")
	}

	var result AcceptHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		packet, err := txc.store.Handoffs().Get(handoffID)
		if err != nil {
			return err
		}
		if packet.TaskID != taskID {
			return fmt.Errorf("handoff task mismatch: packet task=%s request task=%s", packet.TaskID, taskID)
		}
		acceptedBy := req.AcceptedBy
		if acceptedBy == "" {
			acceptedBy = packet.TargetWorker
		}
		if packet.Status == handoff.StatusAccepted {
			if packet.AcceptedBy != "" && acceptedBy != "" && packet.AcceptedBy != acceptedBy {
				return fmt.Errorf("handoff %s is already accepted by %s", handoffID, packet.AcceptedBy)
			}
			result = AcceptHandoffResult{
				TaskID:            taskID,
				HandoffID:         handoffID,
				Status:            handoff.StatusAccepted,
				CanonicalResponse: fmt.Sprintf("Handoff %s was already accepted by %s. Reusing the durable acceptance anchored at checkpoint %s.", handoffID, packet.AcceptedBy, packet.CheckpointID),
			}
			return nil
		}
		if packet.Status != handoff.StatusCreated {
			return fmt.Errorf("handoff %s is not accept-ready in status %s", handoffID, packet.Status)
		}
		now := txc.clock()
		if err := txc.store.Handoffs().UpdateStatus(taskID, handoffID, handoff.StatusAccepted, acceptedBy, req.Notes, now); err != nil {
			return err
		}

		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"handoff_id":    handoffID,
			"accepted_by":   acceptedBy,
			"target_worker": packet.TargetWorker,
			"checkpoint_id": packet.CheckpointID,
			"brief_id":      packet.BriefID,
		}
		if err := txc.appendProof(caps, proof.EventHandoffAccepted, proof.ActorSystem, "tuku-daemon", payload, runIDPointer(packet.LatestRunID)); err != nil {
			return err
		}
		canonical := fmt.Sprintf("Handoff %s accepted by %s. Continuity remains anchored at checkpoint %s.", handoffID, acceptedBy, packet.CheckpointID)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runIDPointer(packet.LatestRunID)); err != nil {
			return err
		}
		result = AcceptHandoffResult{
			TaskID:            taskID,
			HandoffID:         handoffID,
			Status:            handoff.StatusAccepted,
			CanonicalResponse: canonical,
		}
		return nil
	})
	if err != nil {
		return AcceptHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) tryReuseExistingHandoff(taskID common.TaskID, caps capsule.WorkCapsule, assessment continueAssessment, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) (CreateHandoffResult, bool, error) {
	packet, err := c.store.Handoffs().LatestByTask(taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CreateHandoffResult{}, false, nil
		}
		return CreateHandoffResult{}, false, err
	}
	if !c.canReuseHandoffPacket(packet, caps, assessment, targetWorker, req) {
		return CreateHandoffResult{}, false, nil
	}

	packetCopy := packet
	return CreateHandoffResult{
		TaskID:            packet.TaskID,
		HandoffID:         packet.HandoffID,
		SourceWorker:      packet.SourceWorker,
		TargetWorker:      packet.TargetWorker,
		Status:            packet.Status,
		CheckpointID:      packet.CheckpointID,
		BriefID:           packet.BriefID,
		CanonicalResponse: fmt.Sprintf("Reused existing handoff packet %s for %s. Continuity remains anchored to checkpoint %s and brief %s.", packet.HandoffID, packet.TargetWorker, packet.CheckpointID, packet.BriefID),
		Packet:            &packetCopy,
	}, true, nil
}

func (c *Coordinator) recordBlockedHandoffReason(_ context.Context, taskID common.TaskID, reason string, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) (CreateHandoffResult, error) {
	var result CreateHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		return txc.recordBlockedHandoffTx(caps, reason, targetWorker, req, &result)
	})
	if err != nil {
		return CreateHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) recordBlockedHandoffTx(caps capsule.WorkCapsule, reason string, targetWorker rundomain.WorkerKind, req CreateHandoffRequest, out *CreateHandoffResult) error {
	payload := map[string]any{
		"target_worker": targetWorker,
		"reason":        strings.TrimSpace(reason),
		"mode":          normalizeHandoffMode(req.Mode),
	}
	if err := c.appendProof(caps, proof.EventHandoffBlocked, proof.ActorSystem, "tuku-daemon", payload, nil); err != nil {
		return err
	}
	canonical := fmt.Sprintf("Handoff to %s is blocked: %s", targetWorker, strings.TrimSpace(reason))
	if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
		return err
	}
	*out = CreateHandoffResult{
		TaskID:            caps.TaskID,
		SourceWorker:      rundomain.WorkerKindUnknown,
		TargetWorker:      targetWorker,
		Status:            handoff.StatusBlocked,
		CanonicalResponse: canonical,
	}
	return nil
}

func (c *Coordinator) buildHandoffPacket(caps capsule.WorkCapsule, b brief.ExecutionBrief, cp checkpoint.Checkpoint, latestRun *rundomain.ExecutionRun, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) handoff.Packet {
	sourceWorker := rundomain.WorkerKindUnknown
	latestRunID := common.RunID("")
	latestRunStatus := rundomain.Status("")
	if latestRun != nil {
		sourceWorker = latestRun.WorkerKind
		latestRunID = latestRun.RunID
		latestRunStatus = latestRun.Status
	}

	notes := append([]string{}, req.Notes...)
	unknowns := buildHandoffUnknowns(caps, cp, latestRun)
	return handoff.Packet{
		Version:          1,
		HandoffID:        c.idGenerator("hnd"),
		TaskID:           caps.TaskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     sourceWorker,
		TargetWorker:     targetWorker,
		HandoffMode:      normalizeHandoffMode(req.Mode),
		Reason:           strings.TrimSpace(req.Reason),
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     cp.CheckpointID,
		BriefID:          b.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       cp.Anchor,
		IsResumable:      cp.IsResumable,
		ResumeDescriptor: cp.ResumeDescriptor,
		LatestRunID:      latestRunID,
		LatestRunStatus:  latestRunStatus,
		Goal:             caps.Goal,
		BriefObjective:   b.Objective,
		NormalizedAction: b.NormalizedAction,
		Constraints:      append([]string{}, b.Constraints...),
		DoneCriteria:     append([]string{}, b.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		Unknowns:         unknowns,
		HandoffNotes:     notes,
		CreatedAt:        c.clock(),
	}
}

func normalizeHandoffMode(mode handoff.Mode) handoff.Mode {
	switch mode {
	case handoff.ModeResume, handoff.ModeReview, handoff.ModeTakeover:
		return mode
	default:
		return handoff.ModeResume
	}
}

func buildHandoffUnknowns(caps capsule.WorkCapsule, cp checkpoint.Checkpoint, latestRun *rundomain.ExecutionRun) []string {
	unknowns := []string{}

	if latestRun == nil {
		unknowns = append(unknowns, "No prior worker run is recorded for this task.")
	} else {
		switch latestRun.Status {
		case rundomain.StatusInterrupted:
			unknowns = append(unknowns, "Latest run is INTERRUPTED; completion status is unresolved.")
		case rundomain.StatusFailed:
			unknowns = append(unknowns, "Latest run FAILED; target worker should validate root cause before proceeding.")
		}
	}
	if caps.CurrentPhase != phase.PhaseCompleted {
		unknowns = append(unknowns, "End-to-end validation is not marked complete in continuity state.")
	}
	if isRepoAnchorDirty(cp.Anchor) || caps.WorkingTreeDirty {
		unknowns = append(unknowns, "Repository is currently dirty; handoff may include uncommitted state.")
	}
	if len(caps.Blockers) > 0 {
		unknowns = append(unknowns, "Task blockers are present and may require human decision.")
	}
	return unknowns
}

func (c *Coordinator) validateHandoffSafety(assessment continueAssessment) (string, error) {
	hasReusableCheckpoint := c.hasValidResumableHandoffCheckpoint(assessment.Capsule, assessment.LatestCheckpoint)

	switch assessment.Outcome {
	case ContinueOutcomeBlockedInconsistent:
		return fmt.Sprintf("handoff blocked by inconsistent continuity state: %s", assessment.Reason), nil
	case ContinueOutcomeStaleReconciled:
		return "handoff blocked because latest run is still unresolved and requires reconciliation", nil
	case ContinueOutcomeBlockedDrift:
		if !hasReusableCheckpoint {
			return "handoff blocked by major repository drift", nil
		}
	case ContinueOutcomeNeedsDecision:
		if !hasReusableCheckpoint {
			return "handoff blocked while task is in decision-gated continuity state", nil
		}
	case ContinueOutcomeSafe:
		// Continue with explicit handoff checks below.
	default:
		return fmt.Sprintf("handoff blocked by unsupported continuity outcome: %s", assessment.Outcome), nil
	}

	if assessment.DriftClass == checkpoint.DriftMajor {
		return "handoff blocked by major repository drift", nil
	}
	if assessment.LatestRun != nil && assessment.LatestRun.Status == rundomain.StatusRunning {
		return "handoff blocked because a RUNNING execution state is unresolved", nil
	}
	if assessment.Capsule.CurrentPhase == phase.PhaseExecuting {
		return "handoff blocked because task phase is EXECUTING", nil
	}
	if assessment.Capsule.CurrentBriefID == "" {
		return "handoff blocked because no current brief exists", nil
	}
	if _, err := c.store.Briefs().Get(assessment.Capsule.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Sprintf("handoff blocked because current brief %s is missing", assessment.Capsule.CurrentBriefID), nil
		}
		return "", err
	}

	if hasReusableCheckpoint {
		return "", nil
	}
	if c.canCreateHandoffCheckpoint(assessment) {
		return "", nil
	}

	return "handoff blocked because no reusable or safely creatable resumable checkpoint is available", nil
}

func (c *Coordinator) canReuseHandoffCheckpoint(caps capsule.WorkCapsule, assessment continueAssessment, latestCP checkpoint.Checkpoint) bool {
	if assessment.LatestCheckpoint == nil {
		return false
	}
	if latestCP.CheckpointID != assessment.LatestCheckpoint.CheckpointID {
		return false
	}
	return c.hasValidResumableHandoffCheckpoint(caps, &latestCP)
}

func (c *Coordinator) canReuseHandoffPacket(packet handoff.Packet, caps capsule.WorkCapsule, assessment continueAssessment, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) bool {
	if packet.Status != handoff.StatusCreated && packet.Status != handoff.StatusAccepted {
		return false
	}
	if packet.TaskID != caps.TaskID || packet.TargetWorker != targetWorker {
		return false
	}
	if packet.HandoffMode != normalizeHandoffMode(req.Mode) {
		return false
	}
	if strings.TrimSpace(packet.Reason) != strings.TrimSpace(req.Reason) {
		return false
	}
	if !stringSlicesEqual(packet.HandoffNotes, req.Notes) {
		return false
	}
	if packet.BriefID != caps.CurrentBriefID || packet.IntentID != caps.CurrentIntentID {
		return false
	}
	if packet.CurrentPhase != caps.CurrentPhase || packet.CapsuleVersion != caps.Version {
		return false
	}
	if assessment.LatestRun != nil {
		if packet.LatestRunID != assessment.LatestRun.RunID || packet.LatestRunStatus != assessment.LatestRun.Status {
			return false
		}
	} else if packet.LatestRunID != "" || packet.LatestRunStatus != "" {
		return false
	}
	cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
	if err != nil {
		return false
	}
	if !c.hasValidResumableHandoffCheckpoint(caps, &cp) {
		return false
	}
	if !repoAnchorsEqual(packet.RepoAnchor, cp.Anchor) {
		return false
	}
	return true
}

func (c *Coordinator) hasValidResumableHandoffCheckpoint(caps capsule.WorkCapsule, cp *checkpoint.Checkpoint) bool {
	if cp == nil {
		return false
	}
	if !cp.IsResumable {
		return false
	}
	if cp.TaskID != caps.TaskID {
		return false
	}
	if cp.BriefID == "" || cp.BriefID != caps.CurrentBriefID {
		return false
	}
	if cp.IntentID != "" && caps.CurrentIntentID != "" && cp.IntentID != caps.CurrentIntentID {
		return false
	}
	if cp.Phase != caps.CurrentPhase {
		return false
	}
	return true
}

func (c *Coordinator) canCreateHandoffCheckpoint(assessment continueAssessment) bool {
	if assessment.Outcome != ContinueOutcomeSafe {
		return false
	}
	if assessment.DriftClass == checkpoint.DriftMajor {
		return false
	}
	if assessment.Capsule.CurrentBriefID == "" {
		return false
	}
	if assessment.Capsule.CurrentPhase == phase.PhaseExecuting || assessment.Capsule.CurrentPhase == phase.PhaseAwaitingDecision || assessment.Capsule.CurrentPhase == phase.PhaseBlocked {
		return false
	}
	if assessment.LatestRun != nil && assessment.LatestRun.Status == rundomain.StatusRunning {
		return false
	}
	return true
}

func isRepoAnchorDirty(anchor checkpoint.RepoAnchor) bool {
	dirty := strings.TrimSpace(strings.ToLower(anchor.DirtyHash))
	return dirty == "true" || dirty == "1" || dirty == "yes"
}

```

## `/Users/kagaya/Desktop/Tuku/internal/ipc/payloads.go`

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

type TaskInspectResponse struct {
	TaskID         common.TaskID           `json:"task_id"`
	RepoAnchor     RepoAnchor              `json:"repo_anchor"`
	Intent         *intent.State           `json:"intent,omitempty"`
	Brief          *brief.ExecutionBrief   `json:"brief,omitempty"`
	Run            *run.ExecutionRun       `json:"run,omitempty"`
	Checkpoint     *checkpoint.Checkpoint  `json:"checkpoint,omitempty"`
	Handoff        *handoff.Packet         `json:"handoff,omitempty"`
	Acknowledgment *handoff.Acknowledgment `json:"acknowledgment,omitempty"`
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

## `/Users/kagaya/Desktop/Tuku/internal/runtime/daemon/service.go`

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
			Acknowledgment: out.Acknowledgment,
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

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service_test.go`

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

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/handoff_test.go`

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

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	runID := runIDPointer(createOut.Packet.LatestRunID)
	if err := store.Proofs().Append(proof.Event{
		EventID:        common.EventID("evt_launch_requested_unknown"),
		TaskID:         taskID,
		RunID:          runID,
		Timestamp:      time.Now().UTC(),
		Type:           proof.EventHandoffLaunchRequested,
		ActorType:      proof.ActorSystem,
		ActorID:        "tuku-daemon",
		PayloadJSON:    mustJSON(map[string]any{"handoff_id": createOut.HandoffID, "target_worker": createOut.TargetWorker, "checkpoint_id": createOut.CheckpointID, "brief_id": createOut.BriefID}),
		CapsuleVersion: caps.Version,
	}); err != nil {
		t.Fatalf("append launch requested proof: %v", err)
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
