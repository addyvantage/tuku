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
	RecoveryClassReadyNextRun                       RecoveryClass = "READY_NEXT_RUN"
	RecoveryClassRunInProgress                      RecoveryClass = "RUN_IN_PROGRESS"
	RecoveryClassInterruptedRunRecoverable          RecoveryClass = "INTERRUPTED_RUN_RECOVERABLE"
	RecoveryClassAcceptedHandoffLaunchReady         RecoveryClass = "ACCEPTED_HANDOFF_LAUNCH_READY"
	RecoveryClassHandoffLaunchPendingOutcome        RecoveryClass = "HANDOFF_LAUNCH_PENDING_OUTCOME"
	RecoveryClassHandoffLaunchCompleted             RecoveryClass = "HANDOFF_LAUNCH_COMPLETED"
	RecoveryClassHandoffFollowThroughReviewRequired RecoveryClass = "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED"
	RecoveryClassFailedRunReviewRequired            RecoveryClass = "FAILED_RUN_REVIEW_REQUIRED"
	RecoveryClassValidationReviewRequired           RecoveryClass = "VALIDATION_REVIEW_REQUIRED"
	RecoveryClassStaleRunReconciliationRequired     RecoveryClass = "STALE_RUN_RECONCILIATION_REQUIRED"
	RecoveryClassDecisionRequired                   RecoveryClass = "DECISION_REQUIRED"
	RecoveryClassContinueExecutionRequired          RecoveryClass = "CONTINUE_EXECUTION_REQUIRED"
	RecoveryClassBlockedDrift                       RecoveryClass = "BLOCKED_DRIFT"
	RecoveryClassRepairRequired                     RecoveryClass = "REPAIR_REQUIRED"
	RecoveryClassRebriefRequired                    RecoveryClass = "REBRIEF_REQUIRED"
	RecoveryClassCompletedNoAction                  RecoveryClass = "COMPLETED_NO_ACTION"
)

type RecoveryAction string

const (
	RecoveryActionNone                       RecoveryAction = "NONE"
	RecoveryActionWaitForLocalRun            RecoveryAction = "WAIT_FOR_LOCAL_RUN"
	RecoveryActionStartNextRun               RecoveryAction = "START_NEXT_RUN"
	RecoveryActionResumeInterrupted          RecoveryAction = "RESUME_INTERRUPTED_RUN"
	RecoveryActionLaunchAcceptedHandoff      RecoveryAction = "LAUNCH_ACCEPTED_HANDOFF"
	RecoveryActionWaitForLaunchOutcome       RecoveryAction = "WAIT_FOR_LAUNCH_OUTCOME"
	RecoveryActionMonitorLaunchedHandoff     RecoveryAction = "MONITOR_LAUNCHED_HANDOFF"
	RecoveryActionReviewHandoffFollowThrough RecoveryAction = "REVIEW_HANDOFF_FOLLOW_THROUGH"
	RecoveryActionInspectFailedRun           RecoveryAction = "INSPECT_FAILED_RUN"
	RecoveryActionReviewValidation           RecoveryAction = "REVIEW_VALIDATION_STATE"
	RecoveryActionReconcileStaleRun          RecoveryAction = "RECONCILE_STALE_RUN"
	RecoveryActionMakeResumeDecision         RecoveryAction = "MAKE_RESUME_DECISION"
	RecoveryActionExecuteContinueRecovery    RecoveryAction = "EXECUTE_CONTINUE_RECOVERY"
	RecoveryActionRepairContinuity           RecoveryAction = "REPAIR_CONTINUITY"
	RecoveryActionRegenerateBrief            RecoveryAction = "REGENERATE_BRIEF"
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
	case ContinueOutcomeRunInProgress:
		recovery.RecoveryClass = RecoveryClassRunInProgress
		recovery.RecommendedAction = RecoveryActionWaitForLocalRun
		if recovery.Reason == "" {
			recovery.Reason = "latest local run is still actively executing"
		}
		return applyRecoveryActionProgression(recovery)
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
		handoffContinuity := assessHandoffContinuity(assessment.TaskID, packet, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough, assessment.LatestResolution)
		switch handoffContinuity.State {
		case HandoffContinuityStateResolved:
			// Explicitly resolved Claude continuity is no longer the active blocking branch.
		case HandoffContinuityStateAcceptedNotLaunched, HandoffContinuityStateLaunchFailedRetryable:
			recovery.RecoveryClass = RecoveryClassAcceptedHandoffLaunchReady
			recovery.RecommendedAction = RecoveryActionLaunchAcceptedHandoff
			recovery.ReadyForHandoffLaunch = true
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateLaunchPendingOutcome:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchPendingOutcome
			recovery.RecommendedAction = RecoveryActionWaitForLaunchOutcome
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateLaunchCompletedAckSeen, HandoffContinuityStateLaunchCompletedAckEmpty, HandoffContinuityStateFollowThroughProofOfLife, HandoffContinuityStateFollowThroughConfirmed, HandoffContinuityStateFollowThroughUnknown:
			recovery.RecoveryClass = RecoveryClassHandoffLaunchCompleted
			recovery.RecommendedAction = RecoveryActionMonitorLaunchedHandoff
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateFollowThroughStalled:
			recovery.RecoveryClass = RecoveryClassHandoffFollowThroughReviewRequired
			recovery.RecommendedAction = RecoveryActionReviewHandoffFollowThrough
			recovery.RequiresReview = true
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		case HandoffContinuityStateLaunchCompletedAckLost:
			recovery.RecoveryClass = RecoveryClassRepairRequired
			recovery.RecommendedAction = RecoveryActionRepairContinuity
			recovery.RequiresRepair = true
			recovery.Reason = handoffContinuity.Reason
			return applyRecoveryActionProgression(recovery)
		}
	}

	if assessment.Capsule.CurrentPhase == phase.PhaseBriefReady {
		if override, ok := briefReadyRecoveryOverride(recovery); ok {
			return override
		}
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
				recovery.ReadyForNextRun = false
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
				if override, ok := briefReadyRecoveryOverride(recovery); ok {
					return override
				}
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
		if assessment.LatestCheckpoint != nil && assessment.LatestCheckpoint.IsResumable {
			recovery.ReadyForNextRun = false
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

func briefReadyRecoveryOverride(recovery RecoveryAssessment) (RecoveryAssessment, bool) {
	if recovery.LatestAction == nil {
		return recovery, false
	}
	switch recovery.LatestAction.Kind {
	case recoveryaction.KindDecisionContinue:
		recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
		recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.RequiresReconciliation = false
		recovery.Reason = fmt.Sprintf("operator chose to continue with the current brief, but explicit continue finalization is still required before the next bounded run: %s", recovery.LatestAction.Summary)
		return recovery, true
	case recoveryaction.KindInterruptedResumeExecuted:
		recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
		recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
		recovery.ReadyForNextRun = false
		recovery.ReadyForHandoffLaunch = false
		recovery.RequiresDecision = false
		recovery.RequiresReview = false
		recovery.RequiresRepair = false
		recovery.RequiresReconciliation = false
		recovery.Reason = fmt.Sprintf("operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run: %s", recovery.LatestAction.Summary)
		return recovery, true
	default:
		return recovery, false
	}
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
	case recoveryaction.KindInterruptedRunReviewed:
		if recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable && (recovery.RunID == "" || action.RunID == recovery.RunID) {
			recovery.RecommendedAction = RecoveryActionResumeInterrupted
			recovery.ReadyForNextRun = false
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("interrupted execution lineage was reviewed and remains recoverable from checkpoint %s: %s", nonEmpty(string(recovery.CheckpointID), "unknown"), action.Summary)
		}
	case recoveryaction.KindInterruptedResumeExecuted:
		switch recovery.RecoveryClass {
		case RecoveryClassInterruptedRunRecoverable, RecoveryClassReadyNextRun, RecoveryClassContinueExecutionRequired:
			recovery.RecoveryClass = RecoveryClassContinueExecutionRequired
			recovery.RecommendedAction = RecoveryActionExecuteContinueRecovery
			recovery.ReadyForNextRun = false
			recovery.ReadyForHandoffLaunch = false
			recovery.RequiresDecision = false
			recovery.RequiresReview = false
			recovery.RequiresRepair = false
			recovery.RequiresReconciliation = false
			recovery.Reason = fmt.Sprintf("operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run: %s", action.Summary)
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
	case RecoveryClassRunInProgress:
		return "Execution cannot start yet because a local run is still actively executing."
	case RecoveryClassStaleRunReconciliationRequired:
		return "Execution cannot start yet because stale run reconciliation is still required."
	case RecoveryClassCompletedNoAction:
		return "Execution cannot start because the task is already completed."
	default:
		return fmt.Sprintf("Execution cannot start yet because recovery posture is %s.", recovery.RecoveryClass)
	}
}

func runStartEligibility(recovery RecoveryAssessment, authorities OperatorActionAuthoritySet) (bool, string) {
	if authority := operatorActionAuthorityFor(authorities, OperatorActionStartLocalRun); authority != nil {
		switch authority.State {
		case OperatorActionAuthorityAllowed, OperatorActionAuthorityRequiredNext:
			return true, ""
		case OperatorActionAuthorityBlocked:
			return false, authority.Reason
		case OperatorActionAuthorityNotApplicable:
			if strings.TrimSpace(authority.Reason) != "" {
				return false, authority.Reason
			}
		}
	}
	if recovery.RecoveryClass == RecoveryClassReadyNextRun && recovery.RecommendedAction == RecoveryActionStartNextRun && recovery.ReadyForNextRun {
		return true, ""
	}
	return false, runStartBlockedCanonical(recovery)
}

func (c *Coordinator) localMutationBlockedByClaudeHandoff(ctx context.Context, taskID common.TaskID, mutation string) (string, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return "", err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	runFinalization := deriveLocalRunFinalization(assessment, recovery)
	localResume := deriveLocalResumeAuthority(assessment, recovery)
	actions := deriveOperatorActionAuthoritySet(assessment, recovery, branch, runFinalization, localResume)
	var action OperatorAction
	switch strings.TrimSpace(mutation) {
	case "compile a new local execution brief":
		action = OperatorActionLocalMessageMutation
	case "capture a new local checkpoint":
		action = OperatorActionCreateCheckpoint
	default:
		return "", nil
	}
	if authority := operatorActionAuthorityFor(actions, action); authority != nil && authority.State == OperatorActionAuthorityBlocked {
		return authority.Reason, nil
	}
	return "", nil
}
