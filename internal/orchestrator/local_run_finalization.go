package orchestrator

import (
	"fmt"

	"tuku/internal/domain/common"
	rundomain "tuku/internal/domain/run"
)

type LocalRunFinalizationState string

const (
	LocalRunFinalizationNoRelevantRun             LocalRunFinalizationState = "NO_RELEVANT_RUN"
	LocalRunFinalizationFinalized                 LocalRunFinalizationState = "FINALIZED"
	LocalRunFinalizationInterruptedRecoverable    LocalRunFinalizationState = "INTERRUPTED_RECOVERABLE"
	LocalRunFinalizationInterruptedNeedsRepair    LocalRunFinalizationState = "INTERRUPTED_NEEDS_REPAIR"
	LocalRunFinalizationFailedReviewRequired      LocalRunFinalizationState = "FAILED_REVIEW_REQUIRED"
	LocalRunFinalizationStaleReconciliationNeeded LocalRunFinalizationState = "STALE_RECONCILIATION_REQUIRED"
)

type LocalRunFinalization struct {
	TaskID       common.TaskID             `json:"task_id"`
	State        LocalRunFinalizationState `json:"state"`
	RunID        common.RunID              `json:"run_id,omitempty"`
	RunStatus    rundomain.Status          `json:"run_status,omitempty"`
	CheckpointID common.CheckpointID       `json:"checkpoint_id,omitempty"`
	Reason       string                    `json:"reason,omitempty"`
}

func deriveLocalRunFinalization(assessment continueAssessment, recovery RecoveryAssessment) LocalRunFinalization {
	out := LocalRunFinalization{
		TaskID: assessment.TaskID,
		State:  LocalRunFinalizationNoRelevantRun,
		Reason: "no local run anchor is currently controlling recovery",
	}
	if assessment.LatestRun == nil {
		return out
	}

	out.RunID = assessment.LatestRun.RunID
	out.RunStatus = assessment.LatestRun.Status
	out.CheckpointID = checkpointIDFromAssessment(assessment, recovery)

	switch assessment.LatestRun.Status {
	case rundomain.StatusRunning:
		out.State = LocalRunFinalizationStaleReconciliationNeeded
		if recovery.Reason != "" {
			out.Reason = recovery.Reason
		} else {
			out.Reason = fmt.Sprintf("latest run %s is still durably RUNNING and requires explicit stale reconciliation", assessment.LatestRun.RunID)
		}
	case rundomain.StatusInterrupted:
		if checkpointIDFromAssessment(assessment, recovery) != "" && (assessment.LatestCheckpoint == nil || assessment.LatestCheckpoint.IsResumable) {
			out.State = LocalRunFinalizationInterruptedRecoverable
			if recovery.RecoveryClass == RecoveryClassContinueExecutionRequired && recovery.RecommendedAction == RecoveryActionExecuteContinueRecovery {
				out.Reason = fmt.Sprintf("latest run %s remains an interrupted local lineage with resumable checkpoint %s, but explicit continue recovery is still required before any new bounded run", assessment.LatestRun.RunID, out.CheckpointID)
			} else if recovery.Reason != "" {
				out.Reason = recovery.Reason
			} else {
				out.Reason = fmt.Sprintf("interrupted run %s remains recoverable", assessment.LatestRun.RunID)
			}
		} else {
			out.State = LocalRunFinalizationInterruptedNeedsRepair
			if recovery.Reason != "" {
				out.Reason = recovery.Reason
			} else {
				out.Reason = fmt.Sprintf("interrupted run %s is not currently recoverable", assessment.LatestRun.RunID)
			}
		}
	case rundomain.StatusFailed:
		out.State = LocalRunFinalizationFailedReviewRequired
		if recovery.Reason != "" {
			out.Reason = recovery.Reason
		} else {
			out.Reason = fmt.Sprintf("latest run %s failed and still requires review", assessment.LatestRun.RunID)
		}
	case rundomain.StatusCompleted:
		out.State = LocalRunFinalizationFinalized
		if assessment.Capsule.CurrentPhase != "" {
			out.Reason = fmt.Sprintf("latest run %s is durably finalized with terminal status %s; current task phase is %s", assessment.LatestRun.RunID, assessment.LatestRun.Status, assessment.Capsule.CurrentPhase)
		} else {
			out.Reason = fmt.Sprintf("latest run %s is durably finalized with terminal status %s", assessment.LatestRun.RunID, assessment.LatestRun.Status)
		}
	default:
		out.State = LocalRunFinalizationNoRelevantRun
		out.Reason = fmt.Sprintf("latest local run %s has unsupported status %s", assessment.LatestRun.RunID, assessment.LatestRun.Status)
	}
	return out
}
