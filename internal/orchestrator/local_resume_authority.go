package orchestrator

import (
	"fmt"

	"tuku/internal/domain/common"
)

type LocalResumeAuthorityState string

const (
	LocalResumeAuthorityAllowed       LocalResumeAuthorityState = "ALLOWED"
	LocalResumeAuthorityBlocked       LocalResumeAuthorityState = "BLOCKED"
	LocalResumeAuthorityNotApplicable LocalResumeAuthorityState = "NOT_APPLICABLE"
)

type LocalResumeMode string

const (
	LocalResumeModeNone                     LocalResumeMode = "NONE"
	LocalResumeModeResumeInterruptedLineage LocalResumeMode = "RESUME_INTERRUPTED_LINEAGE"
	LocalResumeModeFinalizeContinueRecovery LocalResumeMode = "FINALIZE_CONTINUE_RECOVERY"
	LocalResumeModeStartFreshNextRun        LocalResumeMode = "START_FRESH_NEXT_RUN"
)

type LocalResumeAuthority struct {
	TaskID              common.TaskID             `json:"task_id"`
	State               LocalResumeAuthorityState `json:"state"`
	Mode                LocalResumeMode           `json:"mode"`
	CheckpointID        common.CheckpointID       `json:"checkpoint_id,omitempty"`
	RunID               common.RunID              `json:"run_id,omitempty"`
	BlockingBranchClass ActiveBranchClass         `json:"blocking_branch_class,omitempty"`
	BlockingBranchRef   string                    `json:"blocking_branch_ref,omitempty"`
	Reason              string                    `json:"reason,omitempty"`
}

func deriveLocalResumeAuthority(assessment continueAssessment, recovery RecoveryAssessment) LocalResumeAuthority {
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	out := LocalResumeAuthority{
		TaskID: assessment.TaskID,
		State:  LocalResumeAuthorityNotApplicable,
		Mode:   LocalResumeModeNone,
	}

	if branch.Class == ActiveBranchClassHandoffClaude {
		out.State = LocalResumeAuthorityBlocked
		out.BlockingBranchClass = branch.Class
		out.BlockingBranchRef = branch.BranchRef
		out.Reason = fmt.Sprintf("local interrupted-lineage resume is blocked while Claude handoff branch %s owns continuity", branch.BranchRef)
		return out
	}

	switch {
	case recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable && recovery.RecommendedAction == RecoveryActionResumeInterrupted:
		out.State = LocalResumeAuthorityAllowed
		out.Mode = LocalResumeModeResumeInterruptedLineage
		out.CheckpointID = recovery.CheckpointID
		out.RunID = recovery.RunID
		if recovery.Reason != "" {
			out.Reason = recovery.Reason
		} else if recovery.CheckpointID != "" {
			out.Reason = fmt.Sprintf("local interrupted lineage is recoverable from checkpoint %s", recovery.CheckpointID)
		} else {
			out.Reason = "local interrupted lineage is recoverable"
		}
	case recovery.RecoveryClass == RecoveryClassContinueExecutionRequired && recovery.RecommendedAction == RecoveryActionExecuteContinueRecovery:
		out.Mode = LocalResumeModeFinalizeContinueRecovery
		out.CheckpointID = recovery.CheckpointID
		out.RunID = recovery.RunID
		out.Reason = "local interrupted-lineage resume is not applicable; explicit continue recovery must be executed before any new bounded run"
	case recovery.RecoveryClass == RecoveryClassReadyNextRun && recovery.RecommendedAction == RecoveryActionStartNextRun && recovery.ReadyForNextRun:
		out.Mode = LocalResumeModeStartFreshNextRun
		out.CheckpointID = recovery.CheckpointID
		if assessment.Capsule.CurrentBriefID != "" {
			out.Reason = fmt.Sprintf("local interrupted-lineage resume is not applicable; task is cleared for a fresh bounded run with brief %s", assessment.Capsule.CurrentBriefID)
		} else {
			out.Reason = "local interrupted-lineage resume is not applicable; task is cleared for a fresh bounded run"
		}
	default:
		out.CheckpointID = checkpointIDFromAssessment(assessment, recovery)
		out.RunID = recovery.RunID
		if recovery.Reason != "" {
			out.Reason = "local interrupted-lineage resume is not currently authorized: " + recovery.Reason
		} else {
			out.Reason = "local interrupted-lineage resume is not currently authorized"
		}
	}
	return out
}

func checkpointIDFromAssessment(assessment continueAssessment, recovery RecoveryAssessment) common.CheckpointID {
	if recovery.CheckpointID != "" {
		return recovery.CheckpointID
	}
	if assessment.LatestCheckpoint != nil {
		return assessment.LatestCheckpoint.CheckpointID
	}
	return common.CheckpointID("")
}

func localResumeDescriptorForReadyNextRun(cpID common.CheckpointID) string {
	if cpID != "" {
		return fmt.Sprintf("Fresh next bounded run is ready; checkpoint %s captures the current local recovery boundary.", cpID)
	}
	return "Fresh next bounded run is ready from current local recovery state."
}

func localResumeDescriptorForInterrupted(cpID common.CheckpointID) string {
	if cpID != "" {
		return fmt.Sprintf("Interrupted execution lineage is recoverable from checkpoint %s.", cpID)
	}
	return "Interrupted execution lineage is recoverable."
}
