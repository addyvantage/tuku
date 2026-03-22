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
	if recovery.RecoveryClass != RecoveryClassContinueExecutionRequired || assessment.LatestRecoveryAction == nil || !continueRecoveryTriggerAllowed(assessment.LatestRecoveryAction.Kind) {
		return ExecuteContinueRecoveryResult{}, fmt.Errorf(
			"continue recovery can only be executed while recovery class is %s and latest action is %s or %s",
			RecoveryClassContinueExecutionRequired,
			recoveryaction.KindDecisionContinue,
			recoveryaction.KindInterruptedResumeExecuted,
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

func continueRecoveryTriggerAllowed(kind recoveryaction.Kind) bool {
	return kind == recoveryaction.KindDecisionContinue || kind == recoveryaction.KindInterruptedResumeExecuted
}
