package orchestrator

import (
	"fmt"
	"strings"

	"tuku/internal/domain/common"
)

type OperatorDecisionBlockedAction struct {
	Action OperatorAction `json:"action"`
	Reason string         `json:"reason,omitempty"`
}

type OperatorDecisionSummary struct {
	TaskID             common.TaskID                   `json:"task_id"`
	ActiveOwnerClass   ActiveBranchClass               `json:"active_owner_class"`
	ActiveOwnerRef     string                          `json:"active_owner_ref,omitempty"`
	Headline           string                          `json:"headline,omitempty"`
	RequiredNextAction OperatorAction                  `json:"required_next_action,omitempty"`
	PrimaryReason      string                          `json:"primary_reason,omitempty"`
	Guidance           string                          `json:"guidance,omitempty"`
	IntegrityNote      string                          `json:"integrity_note,omitempty"`
	BlockedActions     []OperatorDecisionBlockedAction `json:"blocked_actions,omitempty"`
}

func deriveOperatorDecisionSummary(
	assessment continueAssessment,
	recovery RecoveryAssessment,
	branch ActiveBranchProvenance,
	runFinalization LocalRunFinalization,
	localResume LocalResumeAuthority,
	authorities OperatorActionAuthoritySet,
) OperatorDecisionSummary {
	continuity := assessHandoffContinuity(
		assessment.TaskID,
		assessment.LatestHandoff,
		assessment.LatestLaunch,
		assessment.LatestAck,
		assessment.LatestFollowThrough,
		assessment.LatestResolution,
	)
	out := OperatorDecisionSummary{
		TaskID:             assessment.TaskID,
		ActiveOwnerClass:   branch.Class,
		ActiveOwnerRef:     branch.BranchRef,
		RequiredNextAction: authorities.RequiredNextAction,
		PrimaryReason:      primaryDecisionReason(branch, recovery, authorities),
		BlockedActions:     summarizeBlockedActions(authorities),
	}

	switch recovery.RecoveryClass {
	case RecoveryClassReadyNextRun:
		out.Headline = "Local fresh run ready"
		out.Guidance = "Start the next bounded local run."
	case RecoveryClassInterruptedRunRecoverable:
		out.Headline = "Interrupted local lineage recoverable"
		out.Guidance = "Resume the interrupted local lineage; do not start a fresh run yet."
	case RecoveryClassContinueExecutionRequired:
		out.Headline = "Continue finalization required"
		out.Guidance = "Finalize continue recovery before any new bounded run."
	case RecoveryClassStaleRunReconciliationRequired:
		out.Headline = "Stale local run reconciliation required"
		out.Guidance = "Reconcile stale run state before starting a new local run."
	case RecoveryClassFailedRunReviewRequired:
		out.Headline = "Failed local run review required"
		out.Guidance = "Inspect the latest failed run before retrying or regenerating the brief."
	case RecoveryClassValidationReviewRequired:
		out.Headline = "Validation review required"
		out.Guidance = "Review validation state before another bounded run."
	case RecoveryClassAcceptedHandoffLaunchReady:
		out.Headline = "Accepted Claude handoff launch ready"
		out.Guidance = "Launch the accepted Claude handoff before local canonical work continues."
	case RecoveryClassHandoffLaunchPendingOutcome:
		out.Headline = "Claude handoff launch pending"
		out.Guidance = "Wait for launch outcome or explicitly resolve the active Claude branch before local work continues."
	case RecoveryClassHandoffLaunchCompleted:
		out.Headline = "Claude handoff branch active"
		out.Guidance = "Monitor or explicitly resolve the active Claude branch before local canonical work continues."
	case RecoveryClassHandoffFollowThroughReviewRequired:
		out.Headline = "Claude follow-through review required"
		out.Guidance = "Review the stalled Claude follow-through before resuming local work."
	case RecoveryClassDecisionRequired:
		out.Headline = "Operator decision required"
		out.Guidance = "Choose whether to continue with the current brief or take the alternative recovery path."
	case RecoveryClassBlockedDrift:
		out.Headline = "Recovery blocked by repository drift"
		out.Guidance = "Make an explicit recovery decision before continuing local execution."
	case RecoveryClassRebriefRequired:
		out.Headline = "Execution brief regeneration required"
		out.Guidance = "Regenerate the execution brief before another bounded run."
	case RecoveryClassRepairRequired:
		out.Headline = "Continuity repair required"
		out.Guidance = "Repair continuity before relying on local or handoff execution state."
	case RecoveryClassCompletedNoAction:
		out.Headline = "Task completed"
		out.Guidance = "No further control-plane action is currently required."
	default:
		if branch.Class == ActiveBranchClassHandoffClaude {
			out.Headline = "Claude handoff branch active"
			out.Guidance = "Review the active Claude branch before local canonical work continues."
		} else {
			out.Headline = "Local lineage active"
			out.Guidance = "Follow the current local recovery guidance."
		}
	}

	out.IntegrityNote = operatorDecisionIntegrityNote(assessment, recovery, continuity, branch, runFinalization, localResume)
	if strings.TrimSpace(out.PrimaryReason) == "" {
		out.PrimaryReason = recovery.Reason
	}
	return out
}

func primaryDecisionReason(branch ActiveBranchProvenance, recovery RecoveryAssessment, authorities OperatorActionAuthoritySet) string {
	if authority := operatorActionAuthorityFor(authorities, authorities.RequiredNextAction); authority != nil && strings.TrimSpace(authority.Reason) != "" {
		return authority.Reason
	}
	if strings.TrimSpace(branch.Reason) != "" {
		return branch.Reason
	}
	return strings.TrimSpace(recovery.Reason)
}

func summarizeBlockedActions(authorities OperatorActionAuthoritySet) []OperatorDecisionBlockedAction {
	prioritized := []OperatorAction{
		OperatorActionLocalMessageMutation,
		OperatorActionCreateCheckpoint,
		OperatorActionStartLocalRun,
		OperatorActionResumeInterruptedLineage,
	}
	blocked := make([]OperatorDecisionBlockedAction, 0, 2)
	seen := map[string]struct{}{}
	for _, action := range prioritized {
		authority := operatorActionAuthorityFor(authorities, action)
		if authority == nil || authority.State != OperatorActionAuthorityBlocked {
			continue
		}
		reason := strings.TrimSpace(authority.Reason)
		key := string(action) + "|" + reason
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		blocked = append(blocked, OperatorDecisionBlockedAction{Action: action, Reason: reason})
		if len(blocked) == 2 {
			break
		}
	}
	return blocked
}

func operatorDecisionIntegrityNote(
	assessment continueAssessment,
	recovery RecoveryAssessment,
	continuity HandoffContinuity,
	branch ActiveBranchProvenance,
	runFinalization LocalRunFinalization,
	localResume LocalResumeAuthority,
) string {
	switch continuity.State {
	case HandoffContinuityStateAcceptedNotLaunched, HandoffContinuityStateLaunchPendingOutcome, HandoffContinuityStateLaunchFailedRetryable, HandoffContinuityStateLaunchCompletedAckSeen, HandoffContinuityStateLaunchCompletedAckEmpty, HandoffContinuityStateFollowThroughProofOfLife, HandoffContinuityStateFollowThroughConfirmed, HandoffContinuityStateFollowThroughUnknown, HandoffContinuityStateFollowThroughStalled:
		return "Downstream Claude completion remains unproven."
	case HandoffContinuityStateLaunchCompletedAckLost:
		return "Launch completion without durable acknowledgment is a continuity inconsistency, not proof of downstream execution."
	}
	if assessment.LatestResolution != nil && branch.Class == ActiveBranchClassLocal {
		return "Historical Claude branch resolution closed that branch without proving downstream completion."
	}
	if runFinalization.State == LocalRunFinalizationStaleReconciliationNeeded {
		return "A stale durably RUNNING run is not treated as resumable interrupted lineage until reconciled."
	}
	if runFinalization.State == LocalRunFinalizationInterruptedRecoverable || localResume.State == LocalResumeAuthorityAllowed || recovery.RecoveryClass == RecoveryClassContinueExecutionRequired {
		return "Checkpoint resumability is evidence for local recovery, not blanket authority for other actions."
	}
	return ""
}

func operatorDecisionHeadlineLine(summary OperatorDecisionSummary) string {
	if strings.TrimSpace(summary.Headline) == "" {
		return "n/a"
	}
	return summary.Headline
}

func operatorDecisionGuidanceLine(summary OperatorDecisionSummary) string {
	if strings.TrimSpace(summary.Guidance) == "" {
		return "n/a"
	}
	return summary.Guidance
}

func operatorDecisionIntegrityLine(summary OperatorDecisionSummary) string {
	if strings.TrimSpace(summary.IntegrityNote) == "" {
		return "n/a"
	}
	return summary.IntegrityNote
}

func operatorDecisionBlockedLine(summary OperatorDecisionSummary) string {
	if len(summary.BlockedActions) == 0 {
		return "n/a"
	}
	parts := make([]string, 0, len(summary.BlockedActions))
	for _, blocked := range summary.BlockedActions {
		switch blocked.Action {
		case OperatorActionLocalMessageMutation:
			parts = append(parts, "local message")
		case OperatorActionCreateCheckpoint:
			parts = append(parts, "checkpoint")
		case OperatorActionStartLocalRun:
			parts = append(parts, "fresh run")
		case OperatorActionResumeInterruptedLineage:
			parts = append(parts, "resume")
		default:
			parts = append(parts, humanizeOperatorDecisionConstant(string(blocked.Action)))
		}
	}
	return fmt.Sprintf("blocked %s", strings.Join(parts, ", "))
}

func humanizeOperatorDecisionConstant(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "_", " ")
}
