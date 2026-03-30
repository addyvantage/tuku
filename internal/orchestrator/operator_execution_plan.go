package orchestrator

import (
	"strings"

	"tuku/internal/domain/common"
)

type OperatorExecutionDomain string

const (
	OperatorExecutionDomainNotApplicable OperatorExecutionDomain = "NOT_APPLICABLE"
	OperatorExecutionDomainLocal         OperatorExecutionDomain = "LOCAL"
	OperatorExecutionDomainHandoffClaude OperatorExecutionDomain = "HANDOFF_CLAUDE"
	OperatorExecutionDomainReview        OperatorExecutionDomain = "REVIEW"
	OperatorExecutionDomainRepair        OperatorExecutionDomain = "REPAIR"
)

type OperatorExecutionStep struct {
	Action         OperatorAction               `json:"action"`
	Status         OperatorActionAuthorityState `json:"status"`
	Domain         OperatorExecutionDomain      `json:"domain"`
	CommandSurface OperatorCommandSurfaceType   `json:"command_surface,omitempty"`
	CommandHint    string                       `json:"command_hint,omitempty"`
	Reason         string                       `json:"reason,omitempty"`
}

type OperatorExecutionPlan struct {
	TaskID                  common.TaskID           `json:"task_id"`
	PrimaryStep             *OperatorExecutionStep  `json:"primary_step,omitempty"`
	MandatoryBeforeProgress bool                    `json:"mandatory_before_progress"`
	SecondarySteps          []OperatorExecutionStep `json:"secondary_steps,omitempty"`
	BlockedSteps            []OperatorExecutionStep `json:"blocked_steps,omitempty"`
}

func deriveOperatorExecutionPlan(
	assessment continueAssessment,
	branch ActiveBranchProvenance,
	authorities OperatorActionAuthoritySet,
	decision OperatorDecisionSummary,
) OperatorExecutionPlan {
	continuity := assessHandoffContinuity(
		assessment.TaskID,
		assessment.LatestHandoff,
		assessment.LatestLaunch,
		assessment.LatestAck,
		assessment.LatestFollowThrough,
		assessment.LatestResolution,
	)
	plan := OperatorExecutionPlan{
		TaskID: assessment.TaskID,
	}

	if primary := primaryExecutionStep(assessment.TaskID, branch, continuity, authorities, decision); primary != nil {
		plan.PrimaryStep = primary
		plan.MandatoryBeforeProgress = primary.Status == OperatorActionAuthorityRequiredNext || executionPlanMandatoryByBlockingContext(*primary, authorities)
	}
	plan.SecondarySteps = secondaryExecutionSteps(assessment.TaskID, branch, continuity, authorities, plan.PrimaryStep)
	plan.BlockedSteps = blockedExecutionSteps(assessment.TaskID, branch, continuity, authorities)
	return plan
}

func primaryExecutionStep(
	taskID common.TaskID,
	branch ActiveBranchProvenance,
	continuity HandoffContinuity,
	authorities OperatorActionAuthoritySet,
	decision OperatorDecisionSummary,
) *OperatorExecutionStep {
	if decision.RequiredNextAction != OperatorActionNone {
		if authority := operatorActionAuthorityFor(authorities, decision.RequiredNextAction); authority != nil {
			step := executionStepFromAuthority(taskID, continuity, *authority)
			return &step
		}
	}

	fallbackOrder := []OperatorAction{
		OperatorActionResolveActiveHandoff,
		OperatorActionLaunchAcceptedHandoff,
		OperatorActionReviewHandoffFollowUp,
		OperatorActionResumeInterruptedLineage,
		OperatorActionFinalizeContinueRecovery,
		OperatorActionStartLocalRun,
		OperatorActionReconcileStaleRun,
		OperatorActionInspectFailedRun,
		OperatorActionExecuteRebrief,
		OperatorActionLocalMessageMutation,
		OperatorActionCreateCheckpoint,
	}
	if branch.Class != ActiveBranchClassHandoffClaude {
		fallbackOrder = []OperatorAction{
			OperatorActionResumeInterruptedLineage,
			OperatorActionFinalizeContinueRecovery,
			OperatorActionStartLocalRun,
			OperatorActionReconcileStaleRun,
			OperatorActionInspectFailedRun,
			OperatorActionExecuteRebrief,
			OperatorActionLocalMessageMutation,
			OperatorActionCreateCheckpoint,
			OperatorActionLaunchAcceptedHandoff,
			OperatorActionResolveActiveHandoff,
			OperatorActionReviewHandoffFollowUp,
		}
	}
	for _, action := range fallbackOrder {
		authority := operatorActionAuthorityFor(authorities, action)
		if authority == nil || authority.State != OperatorActionAuthorityAllowed {
			continue
		}
		step := executionStepFromAuthority(taskID, continuity, *authority)
		return &step
	}
	return nil
}

func secondaryExecutionSteps(
	taskID common.TaskID,
	branch ActiveBranchProvenance,
	continuity HandoffContinuity,
	authorities OperatorActionAuthoritySet,
	primary *OperatorExecutionStep,
) []OperatorExecutionStep {
	preferred := []OperatorAction{
		OperatorActionResolveActiveHandoff,
		OperatorActionLaunchAcceptedHandoff,
		OperatorActionResumeInterruptedLineage,
		OperatorActionFinalizeContinueRecovery,
		OperatorActionStartLocalRun,
		OperatorActionReconcileStaleRun,
		OperatorActionInspectFailedRun,
		OperatorActionExecuteRebrief,
		OperatorActionLocalMessageMutation,
		OperatorActionCreateCheckpoint,
	}
	out := make([]OperatorExecutionStep, 0, 2)
	for _, action := range preferred {
		if primary != nil && action == primary.Action {
			continue
		}
		authority := operatorActionAuthorityFor(authorities, action)
		if authority == nil || authority.State != OperatorActionAuthorityAllowed {
			continue
		}
		out = append(out, executionStepFromAuthority(taskID, continuity, *authority))
		if len(out) == 2 {
			break
		}
	}
	return out
}

func blockedExecutionSteps(
	taskID common.TaskID,
	branch ActiveBranchProvenance,
	continuity HandoffContinuity,
	authorities OperatorActionAuthoritySet,
) []OperatorExecutionStep {
	prioritized := []OperatorAction{
		OperatorActionLocalMessageMutation,
		OperatorActionCreateCheckpoint,
		OperatorActionStartLocalRun,
		OperatorActionResumeInterruptedLineage,
	}
	out := make([]OperatorExecutionStep, 0, 3)
	for _, action := range prioritized {
		authority := operatorActionAuthorityFor(authorities, action)
		if authority == nil || authority.State != OperatorActionAuthorityBlocked {
			continue
		}
		out = append(out, executionStepFromAuthority(taskID, continuity, *authority))
		if len(out) == 3 {
			break
		}
	}
	return out
}

func executionStepFromAuthority(taskID common.TaskID, continuity HandoffContinuity, authority OperatorActionAuthority) OperatorExecutionStep {
	command := canonicalOperatorCommandSurface(taskID, continuity, authority.Action)
	return OperatorExecutionStep{
		Action:         authority.Action,
		Status:         authority.State,
		Domain:         operatorExecutionDomainForAction(authority.Action),
		CommandSurface: command.SurfaceType,
		CommandHint:    command.CanonicalCLI,
		Reason:         strings.TrimSpace(authority.Reason),
	}
}

func operatorExecutionDomainForAction(action OperatorAction) OperatorExecutionDomain {
	switch action {
	case OperatorActionLaunchAcceptedHandoff, OperatorActionResolveActiveHandoff:
		return OperatorExecutionDomainHandoffClaude
	case OperatorActionInspectFailedRun, OperatorActionReviewValidationState, OperatorActionMakeResumeDecision, OperatorActionReviewHandoffFollowUp:
		return OperatorExecutionDomainReview
	case OperatorActionRepairContinuity:
		return OperatorExecutionDomainRepair
	case OperatorActionNone:
		return OperatorExecutionDomainNotApplicable
	default:
		return OperatorExecutionDomainLocal
	}
}

func executionPlanMandatoryByBlockingContext(primary OperatorExecutionStep, authorities OperatorActionAuthoritySet) bool {
	if primary.Action == OperatorActionResolveActiveHandoff || primary.Action == OperatorActionLaunchAcceptedHandoff || primary.Action == OperatorActionReviewHandoffFollowUp {
		for _, action := range []OperatorAction{OperatorActionLocalMessageMutation, OperatorActionCreateCheckpoint, OperatorActionStartLocalRun} {
			authority := operatorActionAuthorityFor(authorities, action)
			if authority != nil && authority.State == OperatorActionAuthorityBlocked {
				return true
			}
		}
	}
	return false
}
