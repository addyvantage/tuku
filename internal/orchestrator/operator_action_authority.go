package orchestrator

import (
	"fmt"
	"strings"

	"tuku/internal/domain/common"
)

type OperatorAction string

const (
	OperatorActionNone                     OperatorAction = "NONE"
	OperatorActionWaitForLocalRun          OperatorAction = "WAIT_FOR_LOCAL_RUN"
	OperatorActionLocalMessageMutation     OperatorAction = "LOCAL_MESSAGE_MUTATION"
	OperatorActionCreateCheckpoint         OperatorAction = "CREATE_CHECKPOINT"
	OperatorActionStartLocalRun            OperatorAction = "START_LOCAL_RUN"
	OperatorActionReconcileStaleRun        OperatorAction = "RECONCILE_STALE_RUN"
	OperatorActionInspectFailedRun         OperatorAction = "INSPECT_FAILED_RUN"
	OperatorActionReviewValidationState    OperatorAction = "REVIEW_VALIDATION_STATE"
	OperatorActionMakeResumeDecision       OperatorAction = "MAKE_RESUME_DECISION"
	OperatorActionResumeInterruptedLineage OperatorAction = "RESUME_INTERRUPTED_LINEAGE"
	OperatorActionFinalizeContinueRecovery OperatorAction = "FINALIZE_CONTINUE_RECOVERY"
	OperatorActionExecuteRebrief           OperatorAction = "EXECUTE_REBRIEF"
	OperatorActionLaunchAcceptedHandoff    OperatorAction = "LAUNCH_ACCEPTED_HANDOFF"
	OperatorActionReviewHandoffFollowUp    OperatorAction = "REVIEW_HANDOFF_FOLLOW_THROUGH"
	OperatorActionResolveActiveHandoff     OperatorAction = "RESOLVE_ACTIVE_HANDOFF"
	OperatorActionRepairContinuity         OperatorAction = "REPAIR_CONTINUITY"
)

type OperatorActionAuthorityState string

const (
	OperatorActionAuthorityAllowed       OperatorActionAuthorityState = "ALLOWED"
	OperatorActionAuthorityBlocked       OperatorActionAuthorityState = "BLOCKED"
	OperatorActionAuthorityNotApplicable OperatorActionAuthorityState = "NOT_APPLICABLE"
	OperatorActionAuthorityRequiredNext  OperatorActionAuthorityState = "REQUIRED_NEXT"
)

type OperatorActionAuthority struct {
	TaskID              common.TaskID                `json:"task_id"`
	Action              OperatorAction               `json:"action"`
	State               OperatorActionAuthorityState `json:"state"`
	Reason              string                       `json:"reason,omitempty"`
	BlockingBranchClass ActiveBranchClass            `json:"blocking_branch_class,omitempty"`
	BlockingBranchRef   string                       `json:"blocking_branch_ref,omitempty"`
	AnchorKind          ActiveBranchAnchorKind       `json:"anchor_kind,omitempty"`
	AnchorRef           string                       `json:"anchor_ref,omitempty"`
}

type OperatorActionAuthoritySet struct {
	TaskID             common.TaskID             `json:"task_id"`
	RequiredNextAction OperatorAction            `json:"required_next_action,omitempty"`
	Actions            []OperatorActionAuthority `json:"actions,omitempty"`
}

func deriveOperatorActionAuthoritySet(
	assessment continueAssessment,
	recovery RecoveryAssessment,
	branch ActiveBranchProvenance,
	runFinalization LocalRunFinalization,
	localResume LocalResumeAuthority,
) OperatorActionAuthoritySet {
	continuity := assessHandoffContinuity(
		assessment.TaskID,
		assessment.LatestHandoff,
		assessment.LatestLaunch,
		assessment.LatestAck,
		assessment.LatestFollowThrough,
		assessment.LatestResolution,
	)
	set := OperatorActionAuthoritySet{
		TaskID:             assessment.TaskID,
		RequiredNextAction: OperatorActionNone,
		Actions: []OperatorActionAuthority{
			operatorActionBase(assessment.TaskID, OperatorActionLocalMessageMutation, branch.ActionabilityAnchor, branch.ActionabilityAnchorRef),
			operatorActionBase(assessment.TaskID, OperatorActionCreateCheckpoint, branch.ActionabilityAnchor, branch.ActionabilityAnchorRef),
			operatorActionBase(assessment.TaskID, OperatorActionStartLocalRun, branch.ActionabilityAnchor, branch.ActionabilityAnchorRef),
			operatorActionBase(assessment.TaskID, OperatorActionReconcileStaleRun, ActiveBranchAnchorKindCheckpoint, string(runFinalization.CheckpointID)),
			operatorActionBase(assessment.TaskID, OperatorActionInspectFailedRun, ActiveBranchAnchorKindCheckpoint, string(runFinalization.RunID)),
			operatorActionBase(assessment.TaskID, OperatorActionReviewValidationState, branch.ActionabilityAnchor, branch.ActionabilityAnchorRef),
			operatorActionBase(assessment.TaskID, OperatorActionMakeResumeDecision, branch.ActionabilityAnchor, branch.ActionabilityAnchorRef),
			operatorActionBase(assessment.TaskID, OperatorActionResumeInterruptedLineage, ActiveBranchAnchorKindCheckpoint, string(localResume.CheckpointID)),
			operatorActionBase(assessment.TaskID, OperatorActionFinalizeContinueRecovery, ActiveBranchAnchorKindBrief, string(assessment.Capsule.CurrentBriefID)),
			operatorActionBase(assessment.TaskID, OperatorActionExecuteRebrief, ActiveBranchAnchorKindBrief, string(assessment.Capsule.CurrentBriefID)),
			operatorActionBase(assessment.TaskID, OperatorActionLaunchAcceptedHandoff, ActiveBranchAnchorKindHandoff, continuity.HandoffID),
			operatorActionBase(assessment.TaskID, OperatorActionReviewHandoffFollowUp, ActiveBranchAnchorKindHandoff, continuity.HandoffID),
			operatorActionBase(assessment.TaskID, OperatorActionResolveActiveHandoff, ActiveBranchAnchorKindHandoff, continuity.HandoffID),
			operatorActionBase(assessment.TaskID, OperatorActionRepairContinuity, branch.ActionabilityAnchor, branch.ActionabilityAnchorRef),
		},
	}
	if branch.Class == ActiveBranchClassHandoffClaude {
		blockLocalAction(&set, OperatorActionLocalMessageMutation, branch, continuity, "send a local task message")
		blockLocalAction(&set, OperatorActionCreateCheckpoint, branch, continuity, "create a local checkpoint")
		blockLocalAction(&set, OperatorActionStartLocalRun, branch, continuity, "start a fresh local run")
		blockLocalAction(&set, OperatorActionReconcileStaleRun, branch, continuity, "reconcile stale local run state")
		blockLocalAction(&set, OperatorActionInspectFailedRun, branch, continuity, "inspect failed local run evidence")
		blockLocalAction(&set, OperatorActionReviewValidationState, branch, continuity, "review local validation state")
		blockLocalAction(&set, OperatorActionMakeResumeDecision, branch, continuity, "make a local resume decision")
		blockLocalAction(&set, OperatorActionResumeInterruptedLineage, branch, continuity, "resume interrupted local lineage")
		blockLocalAction(&set, OperatorActionFinalizeContinueRecovery, branch, continuity, "finalize local continue recovery")
		blockLocalAction(&set, OperatorActionExecuteRebrief, branch, continuity, "regenerate the local execution brief")
	}

	if branch.Class != ActiveBranchClassHandoffClaude {
		setActionAllowed(&set, OperatorActionLocalMessageMutation, "local lineage owns canonical mutation; sending a task message is allowed")
		setActionAllowed(&set, OperatorActionCreateCheckpoint, "local lineage owns continuity; capturing a local checkpoint is allowed")
	}

	switch recovery.RecoveryClass {
	case RecoveryClassRunInProgress:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
	case RecoveryClassReadyNextRun:
		setActionAllowed(&set, OperatorActionStartLocalRun, localRunStartReason(assessment, recovery))
	case RecoveryClassInterruptedRunRecoverable:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
	case RecoveryClassContinueExecutionRequired:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
	case RecoveryClassStaleRunReconciliationRequired:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
		setActionAllowed(&set, OperatorActionReconcileStaleRun, nonEmpty(runFinalization.Reason, recovery.Reason))
	case RecoveryClassFailedRunReviewRequired:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
		setActionAllowed(&set, OperatorActionInspectFailedRun, nonEmpty(runFinalization.Reason, recovery.Reason))
	case RecoveryClassValidationReviewRequired:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
		setActionAllowed(&set, OperatorActionReviewValidationState, recovery.Reason)
	case RecoveryClassDecisionRequired, RecoveryClassBlockedDrift:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
		setActionAllowed(&set, OperatorActionMakeResumeDecision, recovery.Reason)
	case RecoveryClassRebriefRequired:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
		setActionAllowed(&set, OperatorActionExecuteRebrief, recovery.Reason)
	case RecoveryClassAcceptedHandoffLaunchReady:
		setActionAllowed(&set, OperatorActionLaunchAcceptedHandoff, recovery.Reason)
	case RecoveryClassHandoffFollowThroughReviewRequired:
		setActionAllowed(&set, OperatorActionReviewHandoffFollowUp, recovery.Reason)
	case RecoveryClassRepairRequired:
		setActionBlocked(&set, OperatorActionStartLocalRun, runStartBlockedCanonical(recovery), ActiveBranchClassNotApplicable, "")
		setActionAllowed(&set, OperatorActionRepairContinuity, recovery.Reason)
	case RecoveryClassCompletedNoAction:
		setActionNotApplicable(&set, OperatorActionStartLocalRun, "task is already completed; no fresh local run start is applicable")
	}

	switch localResume.State {
	case LocalResumeAuthorityAllowed:
		if localResume.Mode == LocalResumeModeResumeInterruptedLineage {
			setActionAllowed(&set, OperatorActionResumeInterruptedLineage, localResume.Reason)
		}
	case LocalResumeAuthorityBlocked:
		setActionBlocked(&set, OperatorActionResumeInterruptedLineage, localResume.Reason, localResume.BlockingBranchClass, localResume.BlockingBranchRef)
	default:
		switch localResume.Mode {
		case LocalResumeModeFinalizeContinueRecovery:
			setActionNotApplicable(&set, OperatorActionResumeInterruptedLineage, localResume.Reason)
			setActionAllowed(&set, OperatorActionFinalizeContinueRecovery, localResume.Reason)
		case LocalResumeModeStartFreshNextRun:
			setActionNotApplicable(&set, OperatorActionResumeInterruptedLineage, localResume.Reason)
		default:
			if runFinalization.State == LocalRunFinalizationStaleReconciliationNeeded {
				setActionBlocked(&set, OperatorActionResumeInterruptedLineage, "interrupted-lineage resume is not authorized until stale local run state is reconciled", ActiveBranchClassNotApplicable, "")
			} else if runFinalization.State == LocalRunFinalizationFailedReviewRequired {
				setActionBlocked(&set, OperatorActionResumeInterruptedLineage, "interrupted-lineage resume is not authorized while the latest failed run still requires review", ActiveBranchClassNotApplicable, "")
			} else {
				setActionNotApplicable(&set, OperatorActionResumeInterruptedLineage, localResume.Reason)
			}
		}
	}

	if branch.Class == ActiveBranchClassHandoffClaude {
		switch continuity.State {
		case HandoffContinuityStateAcceptedNotLaunched, HandoffContinuityStateLaunchFailedRetryable:
			setActionAllowed(&set, OperatorActionLaunchAcceptedHandoff, continuity.Reason)
			setActionAllowed(&set, OperatorActionResolveActiveHandoff, "active Claude handoff branch can be explicitly resolved without claiming downstream completion")
		case HandoffContinuityStateLaunchPendingOutcome, HandoffContinuityStateLaunchCompletedAckSeen, HandoffContinuityStateLaunchCompletedAckEmpty, HandoffContinuityStateFollowThroughProofOfLife, HandoffContinuityStateFollowThroughConfirmed, HandoffContinuityStateFollowThroughUnknown, HandoffContinuityStateFollowThroughStalled, HandoffContinuityStateLaunchCompletedAckLost:
			setActionNotApplicable(&set, OperatorActionLaunchAcceptedHandoff, "accepted-handoff launch is no longer applicable for the active Claude branch because launch state already exists")
			setActionAllowed(&set, OperatorActionResolveActiveHandoff, "active Claude handoff branch can be explicitly resolved without claiming downstream completion")
		default:
			setActionNotApplicable(&set, OperatorActionLaunchAcceptedHandoff, "no active accepted Claude handoff is waiting for launch")
			setActionNotApplicable(&set, OperatorActionResolveActiveHandoff, "no active Claude handoff branch currently requires explicit resolution")
		}
	} else {
		setActionNotApplicable(&set, OperatorActionLaunchAcceptedHandoff, "no active accepted Claude handoff is waiting for launch")
		setActionNotApplicable(&set, OperatorActionReviewHandoffFollowUp, "no active Claude handoff requires follow-through review")
		setActionNotApplicable(&set, OperatorActionResolveActiveHandoff, "no active Claude handoff branch currently requires explicit resolution")
	}

	if set.RequiredNextAction == OperatorActionNone {
		if action := operatorActionFromRecovery(recovery); action != OperatorActionNone {
			setActionRequiredNext(&set, action, requiredNextReason(set, action, recovery.Reason))
		}
	}
	return set
}

func operatorActionBase(taskID common.TaskID, action OperatorAction, anchorKind ActiveBranchAnchorKind, anchorRef string) OperatorActionAuthority {
	return OperatorActionAuthority{
		TaskID:     taskID,
		Action:     action,
		State:      OperatorActionAuthorityNotApplicable,
		AnchorKind: anchorKind,
		AnchorRef:  strings.TrimSpace(anchorRef),
	}
}

func operatorActionFromRecovery(recovery RecoveryAssessment) OperatorAction {
	switch recovery.RecommendedAction {
	case RecoveryActionStartNextRun:
		return OperatorActionStartLocalRun
	case RecoveryActionWaitForLocalRun:
		return OperatorActionWaitForLocalRun
	case RecoveryActionResumeInterrupted:
		return OperatorActionResumeInterruptedLineage
	case RecoveryActionLaunchAcceptedHandoff:
		return OperatorActionLaunchAcceptedHandoff
	case RecoveryActionReviewHandoffFollowThrough:
		return OperatorActionReviewHandoffFollowUp
	case RecoveryActionInspectFailedRun:
		return OperatorActionInspectFailedRun
	case RecoveryActionReviewValidation:
		return OperatorActionReviewValidationState
	case RecoveryActionReconcileStaleRun:
		return OperatorActionReconcileStaleRun
	case RecoveryActionMakeResumeDecision:
		return OperatorActionMakeResumeDecision
	case RecoveryActionExecuteContinueRecovery:
		return OperatorActionFinalizeContinueRecovery
	case RecoveryActionRepairContinuity:
		return OperatorActionRepairContinuity
	case RecoveryActionRegenerateBrief:
		return OperatorActionExecuteRebrief
	default:
		return OperatorActionNone
	}
}

func operatorActionAuthorityFor(set OperatorActionAuthoritySet, action OperatorAction) *OperatorActionAuthority {
	for i := range set.Actions {
		if set.Actions[i].Action == action {
			return &set.Actions[i]
		}
	}
	return nil
}

func setActionAllowed(set *OperatorActionAuthoritySet, action OperatorAction, reason string) {
	setOperatorActionState(set, action, OperatorActionAuthorityAllowed, reason, ActiveBranchClassNotApplicable, "")
}

func setActionBlocked(set *OperatorActionAuthoritySet, action OperatorAction, reason string, branchClass ActiveBranchClass, branchRef string) {
	setOperatorActionState(set, action, OperatorActionAuthorityBlocked, reason, branchClass, branchRef)
}

func setActionNotApplicable(set *OperatorActionAuthoritySet, action OperatorAction, reason string) {
	setOperatorActionState(set, action, OperatorActionAuthorityNotApplicable, reason, ActiveBranchClassNotApplicable, "")
}

func setActionRequiredNext(set *OperatorActionAuthoritySet, action OperatorAction, reason string) {
	set.RequiredNextAction = action
	setOperatorActionState(set, action, OperatorActionAuthorityRequiredNext, reason, ActiveBranchClassNotApplicable, "")
}

func setOperatorActionState(set *OperatorActionAuthoritySet, action OperatorAction, state OperatorActionAuthorityState, reason string, branchClass ActiveBranchClass, branchRef string) {
	if set == nil {
		return
	}
	for i := range set.Actions {
		if set.Actions[i].Action != action {
			continue
		}
		set.Actions[i].State = state
		set.Actions[i].Reason = strings.TrimSpace(reason)
		set.Actions[i].BlockingBranchClass = branchClass
		set.Actions[i].BlockingBranchRef = strings.TrimSpace(branchRef)
		return
	}
}

func blockLocalAction(set *OperatorActionAuthoritySet, action OperatorAction, branch ActiveBranchProvenance, continuity HandoffContinuity, actionLabel string) {
	setActionBlocked(set, action, blockedByClaudeHandoffReason(continuity, actionLabel), branch.Class, branch.BranchRef)
}

func blockedByClaudeHandoffReason(continuity HandoffContinuity, action string) string {
	switch continuity.State {
	case HandoffContinuityStateAcceptedNotLaunched, HandoffContinuityStateLaunchFailedRetryable:
		return fmt.Sprintf("Cannot %s while Claude handoff %s is the active continuity branch. Launch or explicitly resolve that handoff first.", action, continuity.HandoffID)
	case HandoffContinuityStateLaunchPendingOutcome:
		return fmt.Sprintf("Cannot %s while Claude handoff launch attempt %s is still pending outcome. Downstream continuation remains unproven.", action, nonEmpty(continuity.LaunchAttemptID, continuity.HandoffID))
	case HandoffContinuityStateLaunchCompletedAckSeen, HandoffContinuityStateLaunchCompletedAckEmpty, HandoffContinuityStateFollowThroughProofOfLife, HandoffContinuityStateFollowThroughConfirmed, HandoffContinuityStateFollowThroughUnknown:
		return fmt.Sprintf("Cannot %s while launched Claude handoff %s remains the active continuity branch. Tuku has not proven downstream completion, so monitor or explicitly resolve that handoff first.", action, continuity.HandoffID)
	case HandoffContinuityStateFollowThroughStalled:
		return fmt.Sprintf("Cannot %s while Claude handoff %s is stalled and requires review.", action, continuity.HandoffID)
	case HandoffContinuityStateLaunchCompletedAckLost:
		return fmt.Sprintf("Cannot %s while Claude handoff %s has inconsistent continuity and must be repaired first.", action, continuity.HandoffID)
	default:
		return fmt.Sprintf("Cannot %s while Claude handoff branch %s owns continuity.", action, nonEmpty(continuity.HandoffID, "unknown"))
	}
}

func localRunStartReason(assessment continueAssessment, recovery RecoveryAssessment) string {
	if recovery.Reason != "" {
		return recovery.Reason
	}
	if assessment.Capsule.CurrentBriefID != "" {
		return fmt.Sprintf("task is cleared for a fresh bounded local run with brief %s", assessment.Capsule.CurrentBriefID)
	}
	return "task is cleared for a fresh bounded local run"
}

func requiredNextReason(set OperatorActionAuthoritySet, action OperatorAction, fallback string) string {
	if authority := operatorActionAuthorityFor(set, action); authority != nil && strings.TrimSpace(authority.Reason) != "" {
		return authority.Reason
	}
	return strings.TrimSpace(fallback)
}
