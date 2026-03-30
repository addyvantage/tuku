1. Concise diagnosis of what was missing before this phase
- `RunTask(start)` still treated brief presence as the practical start boundary, so recovery state could be bypassed by directly starting a run.
- That made `DECISION_CONTINUE` too strong and `CONTINUE_EXECUTED` too weak, because the operator could start a run before explicitly finalizing the continue branch.
- Handoff-ready, review-required, repair-required, drift-blocked, and rebrief-required states were visible in recovery, but not enforced at the run-start boundary.

2. Exact implementation plan executed
- Tightened recovery semantics so `DECISION_CONTINUE` no longer projects as `READY_NEXT_RUN`.
- Added a new intermediate recovery posture `CONTINUE_EXECUTION_REQUIRED` with action `EXECUTE_CONTINUE_RECOVERY`.
- Updated `ExecuteContinueRecovery` to require that explicit posture instead of the old `READY_NEXT_RUN` posture.
- Added a single recovery-based run-start gate helper and applied it to both real and noop run-start paths before durable run creation.
- Kept the gate narrow: only `READY_NEXT_RUN` with `START_NEXT_RUN` and `ready_for_next_run=true` is eligible to start a fresh bounded run.
- Added blocked canonical responses for the major recovery classes so run-start failures explain the authoritative next control-plane move.
- Added focused tests covering review/decision/rebrief/drift/handoff blocking, strict continue enforcement, noop gating, and shell/operator truth for the new intermediate continue posture.

3. Files changed
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery_action.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/continue_execution.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel.go`
- `/Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/app/bootstrap_test.go`

4. Before vs after behavior summary
- Before: after `DECISION_CONTINUE`, `RunTask(start)` could still begin a new run directly.
- After: `DECISION_CONTINUE` only selects the branch; `CONTINUE_EXECUTED` is now required before a fresh run may start.
- Before: review-required, rebrief-required, handoff-launch-ready, and drift-blocked states were descriptive but not enforced at run start.
- After: those states now block both real and noop run starts with canonical control-plane responses.
- Before: recovery truth and run-start behavior could disagree.
- After: the run-start boundary uses the same recovery projection as status/inspect.

5. New run-start gating semantics
- New recovery posture:
  - `CONTINUE_EXECUTION_REQUIRED`
- New recovery action:
  - `EXECUTE_CONTINUE_RECOVERY`
- `DECISION_CONTINUE` now means:
  - operator chose the continue branch
  - the task is not yet startable
  - explicit continue finalization is still required
- `ExecuteContinueRecovery` now means:
  - operator finalized the continue branch
  - the current brief is durably cleared for the next bounded run
  - recovery becomes `READY_NEXT_RUN`
- Run-start gate rule:
  - allow only when recovery posture is genuinely `READY_NEXT_RUN`
  - `RecommendedAction == START_NEXT_RUN`
  - `ReadyForNextRun == true`
- Run-start is rejected for:
  - `DECISION_REQUIRED`
  - `FAILED_RUN_REVIEW_REQUIRED`
  - `VALIDATION_REVIEW_REQUIRED`
  - `REPAIR_REQUIRED`
  - `REBRIEF_REQUIRED`
  - `BLOCKED_DRIFT`
  - `HANDOFF_LAUNCH_PENDING_OUTCOME`
  - `HANDOFF_LAUNCH_COMPLETED`
  - `ACCEPTED_HANDOFF_LAUNCH_READY`
  - `CONTINUE_EXECUTION_REQUIRED`
  - interrupted/stale/completed postures that are not fresh bounded-run startable
- The gate applies to both:
  - real run start
  - noop run start

6. Tests added or updated
- Updated recovery progression tests so `DECISION_CONTINUE` now yields `CONTINUE_EXECUTION_REQUIRED` rather than `READY_NEXT_RUN`.
- Added `TestRunStartBlockedWhenRecoveryClassDecisionRequired`.
- Added `TestRunStartBlockedWhenRecoveryClassFailedRunReviewRequired`.
- Added `TestRunStartBlockedWhenRecoveryClassRebriefRequired`.
- Added `TestRunStartBlockedWhenRecoveryClassBlockedDrift`.
- Added `TestRunStartBlockedWhenAcceptedClaudeHandoffIsActiveRecoveryPath`.
- Added `TestRunStartStrictContinuePathRequiresExecutedContinueRecovery`.
- Added `TestRunStartNoopAlsoUsesRecoveryGate`.
- Added shell regression `TestBuildViewModelSurfacesContinueExecutionRequiredState`.
- Updated CLI continue-recovery rejection expectation to the new posture.

7. Commands run
```bash
gofmt -w internal/orchestrator/service.go internal/orchestrator/recovery.go internal/orchestrator/recovery_action.go internal/orchestrator/continue_execution.go internal/tui/shell/viewmodel.go internal/tui/shell/viewmodel_test.go internal/orchestrator/service_test.go internal/app/bootstrap_test.go

go test ./internal/orchestrator ./internal/tui/shell ./internal/app -count=1
```

8. Remaining limitations / next risks
- `READY_NEXT_RUN` is now stricter for fresh bounded-run start, but `INTERRUPTED_RUN_RECOVERABLE` still uses the existing recovery field shape and remains conceptually recoverable rather than fresh-start eligible.
- The run-start gate is recovery-authoritative, but there is still no broader generic execution-clearance model beyond recovery projection.
- Canonical blocked responses are explicit, but they are still one-shot messages, not a richer guided operator workflow.

9. Full code for every changed file

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

func (c *Coordinator) assessRunStartRecovery(ctx context.Context, taskID common.TaskID) (RecoveryAssessment, bool, string, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecoveryAssessment{}, false, "", err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	allowed, canonical := runStartEligibility(recovery)
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
	RecoveryClassContinueExecutionRequired      RecoveryClass = "CONTINUE_EXECUTION_REQUIRED"
	RecoveryClassBlockedDrift                   RecoveryClass = "BLOCKED_DRIFT"
	RecoveryClassRepairRequired                 RecoveryClass = "REPAIR_REQUIRED"
	RecoveryClassRebriefRequired                RecoveryClass = "REBRIEF_REQUIRED"
	RecoveryClassCompletedNoAction              RecoveryClass = "COMPLETED_NO_ACTION"
)

type RecoveryAction string

const (
	RecoveryActionNone                    RecoveryAction = "NONE"
	RecoveryActionStartNextRun            RecoveryAction = "START_NEXT_RUN"
	RecoveryActionResumeInterrupted       RecoveryAction = "RESUME_INTERRUPTED_RUN"
	RecoveryActionLaunchAcceptedHandoff   RecoveryAction = "LAUNCH_ACCEPTED_HANDOFF"
	RecoveryActionWaitForLaunchOutcome    RecoveryAction = "WAIT_FOR_LAUNCH_OUTCOME"
	RecoveryActionMonitorLaunchedHandoff  RecoveryAction = "MONITOR_LAUNCHED_HANDOFF"
	RecoveryActionInspectFailedRun        RecoveryAction = "INSPECT_FAILED_RUN"
	RecoveryActionReviewValidation        RecoveryAction = "REVIEW_VALIDATION_STATE"
	RecoveryActionReconcileStaleRun       RecoveryAction = "RECONCILE_STALE_RUN"
	RecoveryActionMakeResumeDecision      RecoveryAction = "MAKE_RESUME_DECISION"
	RecoveryActionExecuteContinueRecovery RecoveryAction = "EXECUTE_CONTINUE_RECOVERY"
	RecoveryActionRepairContinuity        RecoveryAction = "REPAIR_CONTINUITY"
	RecoveryActionRegenerateBrief         RecoveryAction = "REGENERATE_BRIEF"
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
			recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
			recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
			recovery.ReadyForNextRun = false
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator chose to continue with the current brief, but explicit continue finalization is still required before the next bounded run: %s", action.Summary)
		}
	case recoveryaction.KindContinueExecuted:
		if recovery.RecoveryClass == RecoveryClassReadyNextRun {
			recovery.RecommendedAction = RecoveryActionStartNextRun
			recovery.ReadyForNextRun = true
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator explicitly confirmed the current brief for the next bounded run: %s", action.Summary)
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

func runStartBlockedCanonical(recovery RecoveryAssessment) string {
	switch recovery.RecoveryClass {
	case RecoveryClassContinueExecutionRequired:
		return "Execution cannot start yet because operator continue finalization is still required. Execute continue recovery first so Tuku can durably clear the current brief for the next bounded run."
	case RecoveryClassDecisionRequired:
		return "Execution cannot start yet because recovery still requires an explicit operator decision."
	case RecoveryClassFailedRunReviewRequired:
		return "Execution cannot start yet because the latest failed run still requires review."
	case RecoveryClassValidationReviewRequired:
		return "Execution cannot start yet because validation review is still required."
	case RecoveryClassRebriefRequired:
		return "Execution cannot start yet because the execution brief must be regenerated or replaced first."
	case RecoveryClassBlockedDrift:
		return "Execution cannot start yet because repository drift is blocking deterministic recovery."
	case RecoveryClassRepairRequired:
		return "Execution cannot start yet because continuity repair is still required."
	case RecoveryClassAcceptedHandoffLaunchReady:
		return "Execution cannot start yet because the active recovery path is accepted handoff launch, not a new local run."
	case RecoveryClassHandoffLaunchPendingOutcome:
		return "Execution cannot start yet because the latest handoff launch outcome is still pending."
	case RecoveryClassHandoffLaunchCompleted:
		return "Execution cannot start yet because the latest handoff launch step is already complete and should be monitored rather than replaced by a new local run."
	case RecoveryClassInterruptedRunRecoverable:
		return "Execution cannot start yet because the task is in interrupted-run recovery, not cleared for a fresh bounded run."
	case RecoveryClassStaleRunReconciliationRequired:
		return "Execution cannot start yet because stale run reconciliation is still required."
	case RecoveryClassCompletedNoAction:
		return "Execution cannot start because the task is already completed."
	default:
		return fmt.Sprintf("Execution cannot start yet because recovery posture is %s.", recovery.RecoveryClass)
	}
}

func runStartEligibility(recovery RecoveryAssessment) (bool, string) {
	if recovery.RecoveryClass == RecoveryClassReadyNextRun && recovery.RecommendedAction == RecoveryActionStartNextRun && recovery.ReadyForNextRun {
		return true, ""
	}
	return false, runStartBlockedCanonical(recovery)
}

```

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/recovery_action.go`
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
		return "I recorded the operator decision to continue with the current brief. Execute continue recovery before starting the next bounded run."
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

## `/Users/kagaya/Desktop/Tuku/internal/orchestrator/continue_execution.go`
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

type ExecuteContinueRecoveryRequest struct {
	TaskID string
}

type ExecuteContinueRecoveryResult struct {
	TaskID                common.TaskID
	BriefID               common.BriefID
	BriefHash             string
	Action                recoveryaction.Record
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

func (c *Coordinator) ExecuteContinueRecovery(ctx context.Context, req ExecuteContinueRecoveryRequest) (ExecuteContinueRecoveryResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ExecuteContinueRecoveryResult{}, fmt.Errorf("task id is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecuteContinueRecoveryResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if assessment.LatestRecoveryAction != nil && assessment.LatestRecoveryAction.Kind == recoveryaction.KindContinueExecuted {
		return ExecuteContinueRecoveryResult{}, fmt.Errorf("continue recovery has already been executed for the current brief")
	}
	if recovery.RecoveryClass != RecoveryClassContinueExecutionRequired || assessment.LatestRecoveryAction == nil || assessment.LatestRecoveryAction.Kind != recoveryaction.KindDecisionContinue {
		return ExecuteContinueRecoveryResult{}, fmt.Errorf(
			"continue recovery can only be executed while recovery class is %s and latest action is %s",
			RecoveryClassContinueExecutionRequired,
			recoveryaction.KindDecisionContinue,
		)
	}

	var result ExecuteContinueRecoveryResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return fmt.Errorf("task state changed during continue recovery preparation (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version)
		}

		currentBrief, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = txc.clock()
		caps.CurrentPhase = phase.PhaseBriefReady
		caps.NextAction = "Current brief confirmed for the next bounded run. Start a run with `tuku run --task <id>`."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		runID := recoveryActionRunID(*assessment.LatestRecoveryAction)
		actionRecord := recoveryaction.Record{
			Version:      1,
			ActionID:     txc.idGenerator("ract"),
			TaskID:       taskID,
			Kind:         recoveryaction.KindContinueExecuted,
			RunID:        recovery.RunID,
			CheckpointID: recovery.CheckpointID,
			HandoffID:    recovery.HandoffID,
			Summary:      fmt.Sprintf("Operator confirmed current brief %s for the next bounded run.", currentBrief.BriefID),
			CreatedAt:    txc.clock(),
		}
		if err := txc.store.RecoveryActions().Create(actionRecord); err != nil {
			return err
		}

		payload := map[string]any{
			"action_id":                actionRecord.ActionID,
			"action_kind":              actionRecord.Kind,
			"action_summary":           actionRecord.Summary,
			"brief_id":                 currentBrief.BriefID,
			"brief_hash":               currentBrief.BriefHash,
			"trigger_action_id":        assessment.LatestRecoveryAction.ActionID,
			"trigger_action_kind":      assessment.LatestRecoveryAction.Kind,
			"trigger_action_summary":   assessment.LatestRecoveryAction.Summary,
			"ready_for_next_run":       true,
			"ready_for_handoff_launch": false,
		}
		if err := txc.appendProof(caps, proof.EventRecoveryContinueExecuted, proof.ActorUser, "user", payload, runID); err != nil {
			return err
		}

		afterAssessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		afterRecovery := txc.recoveryFromContinueAssessment(afterAssessment)
		canonical := fmt.Sprintf(
			"I confirmed continuation with current brief %s. The task remains in %s and is explicitly cleared for the next bounded run. The next recommended action is %s.",
			currentBrief.BriefID,
			caps.CurrentPhase,
			afterRecovery.RecommendedAction,
		)
		payload["recovery_class"] = afterRecovery.RecoveryClass
		payload["recommended_action"] = afterRecovery.RecommendedAction
		payload["recovery_reason"] = afterRecovery.Reason
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = ExecuteContinueRecoveryResult{
			TaskID:                taskID,
			BriefID:               currentBrief.BriefID,
			BriefHash:             currentBrief.BriefHash,
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
		return ExecuteContinueRecoveryResult{}, err
	}
	return result, nil
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
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassContinueExecutionRequired)) || !strings.Contains(err.Error(), string(recoveryaction.KindDecisionContinue)) {
		t.Fatalf("expected continue-execution-required/decision-continue rejection, got %v", err)
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

## `/Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel.go`
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

## `/Users/kagaya/Desktop/Tuku/internal/tui/shell/viewmodel_test.go`
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

## `/Users/kagaya/Desktop/Tuku/internal/app/bootstrap_test.go`
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
