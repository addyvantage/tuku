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

type ExecuteInterruptedResumeRequest struct {
	TaskID  string
	Summary string
	Notes   []string
}

type ExecuteInterruptedResumeResult struct {
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

func (c *Coordinator) ExecuteInterruptedResume(ctx context.Context, req ExecuteInterruptedResumeRequest) (ExecuteInterruptedResumeResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ExecuteInterruptedResumeResult{}, fmt.Errorf("task id is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecuteInterruptedResumeResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if assessment.LatestRecoveryAction != nil && assessment.LatestRecoveryAction.Kind == recoveryaction.KindInterruptedResumeExecuted && recovery.RecoveryClass == RecoveryClassContinueExecutionRequired {
		return ExecuteInterruptedResumeResult{}, fmt.Errorf("interrupted resume has already been executed for the current interrupted lineage")
	}
	if recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable || recovery.RecommendedAction != RecoveryActionResumeInterrupted {
		return ExecuteInterruptedResumeResult{}, fmt.Errorf(
			"interrupted resume can only be executed while recovery class is %s and recommended action is %s",
			RecoveryClassInterruptedRunRecoverable,
			RecoveryActionResumeInterrupted,
		)
	}

	var result ExecuteInterruptedResumeResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return fmt.Errorf("task state changed during interrupted resume preparation (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version)
		}

		currentBrief, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}

		caps.Version++
		caps.UpdatedAt = txc.clock()
		caps.CurrentPhase = phase.PhaseBriefReady
		caps.NextAction = "Interrupted lineage continuation selected. Execute continue recovery before starting the next bounded run."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		runID := runIDPointer(recovery.RunID)
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
			"phase":  caps.CurrentPhase,
			"reason": "interrupted lineage continuation selected",
		}, runID); err != nil {
			return err
		}

		actionRecord := recoveryaction.Record{
			Version:      1,
			ActionID:     txc.idGenerator("ract"),
			TaskID:       taskID,
			Kind:         recoveryaction.KindInterruptedResumeExecuted,
			RunID:        recovery.RunID,
			CheckpointID: recovery.CheckpointID,
			Summary:      nonEmptyRecoverySummary(req.Summary, fmt.Sprintf("Operator resumed interrupted lineage for run %s using current brief %s.", nonEmpty(string(recovery.RunID), "unknown"), currentBrief.BriefID)),
			Notes:        normalizedRecoveryNotes(req.Notes),
			CreatedAt:    txc.clock(),
		}
		if err := txc.store.RecoveryActions().Create(actionRecord); err != nil {
			return err
		}

		payload := map[string]any{
			"action_id":                actionRecord.ActionID,
			"action_kind":              actionRecord.Kind,
			"action_summary":           actionRecord.Summary,
			"action_notes":             actionRecord.Notes,
			"brief_id":                 currentBrief.BriefID,
			"brief_hash":               currentBrief.BriefHash,
			"run_id":                   recovery.RunID,
			"checkpoint_id":            recovery.CheckpointID,
			"ready_for_next_run":       false,
			"ready_for_handoff_launch": false,
		}
		if err := txc.appendProof(caps, proof.EventInterruptedRunResumeExecuted, proof.ActorUser, "user", payload, runID); err != nil {
			return err
		}

		afterAssessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		afterRecovery := txc.recoveryFromContinueAssessment(afterAssessment)
		canonical := fmt.Sprintf(
			"I resumed interrupted execution lineage for run %s as a control-plane step. Current brief %s remains canonical, but Tuku is not claiming fresh-run readiness. Continue recovery still must be executed before any new bounded run.",
			nonEmpty(string(recovery.RunID), "unknown"),
			currentBrief.BriefID,
		)
		payload["recovery_class"] = afterRecovery.RecoveryClass
		payload["recommended_action"] = afterRecovery.RecommendedAction
		payload["recovery_reason"] = afterRecovery.Reason
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = ExecuteInterruptedResumeResult{
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
		return ExecuteInterruptedResumeResult{}, err
	}
	return result, nil
}
