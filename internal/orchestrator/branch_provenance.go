package orchestrator

import (
	"fmt"
	"strings"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
)

type ActiveBranchClass string

const (
	ActiveBranchClassNotApplicable ActiveBranchClass = "NOT_APPLICABLE"
	ActiveBranchClassLocal         ActiveBranchClass = "LOCAL"
	ActiveBranchClassHandoffClaude ActiveBranchClass = "HANDOFF_CLAUDE"
)

type ActiveBranchAnchorKind string

const (
	ActiveBranchAnchorKindUnknown    ActiveBranchAnchorKind = "UNKNOWN"
	ActiveBranchAnchorKindCapsule    ActiveBranchAnchorKind = "CAPSULE"
	ActiveBranchAnchorKindBrief      ActiveBranchAnchorKind = "BRIEF"
	ActiveBranchAnchorKindCheckpoint ActiveBranchAnchorKind = "CHECKPOINT"
	ActiveBranchAnchorKindHandoff    ActiveBranchAnchorKind = "HANDOFF"
)

type ActiveBranchProvenance struct {
	TaskID                 common.TaskID          `json:"task_id"`
	Class                  ActiveBranchClass      `json:"class"`
	BranchRef              string                 `json:"branch_ref,omitempty"`
	ActionabilityAnchor    ActiveBranchAnchorKind `json:"actionability_anchor_kind,omitempty"`
	ActionabilityAnchorRef string                 `json:"actionability_anchor_ref,omitempty"`
	Reason                 string                 `json:"reason,omitempty"`
}

func deriveActiveBranchProvenance(caps capsule.WorkCapsule, recovery RecoveryAssessment) ActiveBranchProvenance {
	out := ActiveBranchProvenance{
		TaskID: caps.TaskID,
		Class:  ActiveBranchClassLocal,
	}
	if recovery.HandoffID != "" {
		out.Class = ActiveBranchClassHandoffClaude
		out.BranchRef = recovery.HandoffID
		out.ActionabilityAnchor = ActiveBranchAnchorKindHandoff
		out.ActionabilityAnchorRef = recovery.HandoffID
		if reason := strings.TrimSpace(recovery.Reason); reason != "" {
			out.Reason = reason
		} else {
			out.Reason = fmt.Sprintf("Claude handoff branch %s currently owns continuity", recovery.HandoffID)
		}
		return out
	}

	out.BranchRef = string(caps.TaskID)
	switch {
	case recovery.CheckpointID != "":
		out.ActionabilityAnchor = ActiveBranchAnchorKindCheckpoint
		out.ActionabilityAnchorRef = string(recovery.CheckpointID)
	case caps.CurrentBriefID != "":
		out.ActionabilityAnchor = ActiveBranchAnchorKindBrief
		out.ActionabilityAnchorRef = string(caps.CurrentBriefID)
	default:
		out.ActionabilityAnchor = ActiveBranchAnchorKindCapsule
		out.ActionabilityAnchorRef = string(caps.TaskID)
	}
	if reason := strings.TrimSpace(recovery.Reason); reason != "" {
		out.Reason = "local Tuku lineage currently controls canonical progression: " + reason
	} else {
		out.Reason = "local Tuku lineage currently controls canonical progression"
	}
	return out
}

func deriveActiveBranchProvenanceFromAssessment(assessment continueAssessment, recovery RecoveryAssessment) ActiveBranchProvenance {
	if packet := assessment.LatestHandoff; packet != nil {
		continuity := assessHandoffContinuity(assessment.TaskID, packet, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough, assessment.LatestResolution)
		if continuity.State != HandoffContinuityStateNotApplicable && continuity.State != HandoffContinuityStateResolved {
			return ActiveBranchProvenance{
				TaskID:                 assessment.TaskID,
				Class:                  ActiveBranchClassHandoffClaude,
				BranchRef:              continuity.HandoffID,
				ActionabilityAnchor:    ActiveBranchAnchorKindHandoff,
				ActionabilityAnchorRef: continuity.HandoffID,
				Reason:                 continuity.Reason,
			}
		}
	}
	return deriveActiveBranchProvenance(assessment.Capsule, recovery)
}
