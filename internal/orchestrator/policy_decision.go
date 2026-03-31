package orchestrator

import (
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/policy"
	"tuku/internal/domain/proof"
)

func (c *Coordinator) recordRunStartPolicyDecision(caps capsule.WorkCapsule, b brief.ExecutionBrief, pack contextdomain.Pack) error {
	now := c.clock()
	decision := policy.Decision{
		DecisionID:      common.DecisionID(c.idGenerator("pdec")),
		TaskID:          caps.TaskID,
		OperationType:   "task.run.start",
		RiskLevel:       riskLevelForRunStart(caps, b),
		RequestedAt:     now,
		ResolvedAt:      &now,
		ResolvedBy:      "tuku-policy-v1",
		Status:          policy.DecisionApproved,
		Reason:          policyReasonForRunStart(caps, b, pack),
		ScopeDescriptor: "local real execution run against bounded repo context",
	}
	if err := c.store.PolicyDecisions().Save(decision); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventPolicyDecisionRequested, proof.ActorSystem, "tuku-policy-v1", map[string]any{
		"decision_id":      decision.DecisionID,
		"operation_type":   decision.OperationType,
		"risk_level":       decision.RiskLevel,
		"scope_descriptor": decision.ScopeDescriptor,
	}, nil); err != nil {
		return err
	}
	if err := c.appendProof(caps, proof.EventPolicyDecisionResolved, proof.ActorSystem, "tuku-policy-v1", map[string]any{
		"decision_id":     decision.DecisionID,
		"status":          decision.Status,
		"resolved_by":     decision.ResolvedBy,
		"reason":          decision.Reason,
		"context_pack_id": pack.ContextPackID,
	}, nil); err != nil {
		return err
	}
	return nil
}

func riskLevelForRunStart(caps capsule.WorkCapsule, b brief.ExecutionBrief) policy.RiskLevel {
	if b.RequiresClarification {
		return policy.RiskHigh
	}
	if caps.WorkingTreeDirty {
		return policy.RiskMedium
	}
	return policy.RiskLow
}

func policyReasonForRunStart(caps capsule.WorkCapsule, b brief.ExecutionBrief, pack contextdomain.Pack) string {
	switch {
	case b.RequiresClarification:
		return "approved with elevated caution because the brief still carries clarification gaps; operator proof and bounded context are preserved"
	case caps.WorkingTreeDirty:
		return "approved against a dirty worktree because Tuku captured repo anchor and bounded context before execution"
	case len(pack.IncludedFiles) == 0:
		return "approved with minimal repo context because no bounded file set was readable for this task"
	default:
		return "approved because Tuku prepared a bounded local context pack and current repo anchor for execution"
	}
}
