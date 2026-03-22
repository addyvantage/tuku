package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
)

type ExecuteRebriefRequest struct {
	TaskID string
}

type ExecuteRebriefResult struct {
	TaskID                common.TaskID
	PreviousBriefID       common.BriefID
	BriefID               common.BriefID
	BriefHash             string
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

func (c *Coordinator) ExecuteRebrief(ctx context.Context, req ExecuteRebriefRequest) (ExecuteRebriefResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ExecuteRebriefResult{}, fmt.Errorf("task id is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecuteRebriefResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if recovery.RecoveryClass != RecoveryClassRebriefRequired {
		return ExecuteRebriefResult{}, fmt.Errorf("rebrief can only be executed while recovery class is %s", RecoveryClassRebriefRequired)
	}
	if assessment.LatestRecoveryAction == nil || assessment.LatestRecoveryAction.Kind != recoveryaction.KindDecisionRegenerateBrief {
		return ExecuteRebriefResult{}, fmt.Errorf("rebrief requires latest recovery action %s", recoveryaction.KindDecisionRegenerateBrief)
	}

	var result ExecuteRebriefResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return fmt.Errorf("task state changed during rebrief preparation (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version)
		}

		currentBrief, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			return err
		}
		intentState, err := txc.store.Intents().LatestByTask(taskID)
		if err != nil {
			return err
		}
		if caps.CurrentIntentID != "" && intentState.IntentID != caps.CurrentIntentID {
			return fmt.Errorf("latest intent %s does not match capsule current intent %s", intentState.IntentID, caps.CurrentIntentID)
		}

		nextCapsuleVersion := caps.Version + 1
		buildInput := brief.BuildInput{
			TaskID:           caps.TaskID,
			IntentID:         intentState.IntentID,
			CapsuleVersion:   nextCapsuleVersion,
			Goal:             caps.Goal,
			NormalizedAction: nonEmpty(currentBrief.NormalizedAction, intentState.NormalizedAction),
			Constraints:      append([]string{}, currentBrief.Constraints...),
			ScopeHints:       append([]string{}, currentBrief.ScopeIn...),
			ScopeOutHints:    append([]string{}, currentBrief.ScopeOut...),
			DoneCriteria:     append([]string{}, currentBrief.DoneCriteria...),
			ContextPackID:    currentBrief.ContextPackID,
			Verbosity:        currentBrief.Verbosity,
			PolicyProfileID:  currentBrief.PolicyProfileID,
		}
		if len(buildInput.Constraints) == 0 {
			buildInput.Constraints = append([]string{}, caps.Constraints...)
		}
		if len(buildInput.ScopeHints) == 0 {
			buildInput.ScopeHints = append([]string{}, caps.TouchedFiles...)
		}
		if len(buildInput.DoneCriteria) == 0 {
			buildInput.DoneCriteria = []string{"Execution plan is prepared and ready for worker dispatch"}
		}
		if buildInput.Verbosity == "" {
			buildInput.Verbosity = brief.VerbosityStandard
		}
		if strings.TrimSpace(buildInput.PolicyProfileID) == "" {
			buildInput.PolicyProfileID = "default-safe-v1"
		}
		newBrief, err := txc.briefBuilder.Build(buildInput)
		if err != nil {
			return err
		}
		if err := txc.store.Briefs().Save(newBrief); err != nil {
			return err
		}

		caps.Version = nextCapsuleVersion
		caps.UpdatedAt = txc.clock()
		caps.CurrentBriefID = newBrief.BriefID
		caps.CurrentPhase = phase.PhaseBriefReady
		caps.NextAction = "Execution brief regenerated. Start a run with `tuku run --task <id>`."
		if err := txc.store.Capsules().Update(caps); err != nil {
			return err
		}

		runID := recoveryActionRunID(*assessment.LatestRecoveryAction)
		if err := txc.appendProof(caps, proof.EventBriefRegenerated, proof.ActorSystem, "tuku-brief-builder", map[string]any{
			"previous_brief_id":      currentBrief.BriefID,
			"previous_brief_hash":    currentBrief.BriefHash,
			"new_brief_id":           newBrief.BriefID,
			"new_brief_hash":         newBrief.BriefHash,
			"trigger_action_id":      assessment.LatestRecoveryAction.ActionID,
			"trigger_action_kind":    assessment.LatestRecoveryAction.Kind,
			"trigger_action_summary": assessment.LatestRecoveryAction.Summary,
		}, runID); err != nil {
			return err
		}
		if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
			"phase":  caps.CurrentPhase,
			"reason": "rebrief executed from durable operator decision",
		}, runID); err != nil {
			return err
		}

		afterAssessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		afterRecovery := txc.recoveryFromContinueAssessment(afterAssessment)
		canonical := fmt.Sprintf(
			"I regenerated the execution brief after operator decision %s. New brief %s is now canonical current state. Recovery posture is %s, and the next recommended action is %s.",
			assessment.LatestRecoveryAction.ActionID,
			newBrief.BriefID,
			afterRecovery.RecoveryClass,
			afterRecovery.RecommendedAction,
		)
		if err := txc.emitCanonicalConversation(caps, canonical, map[string]any{
			"previous_brief_id":        currentBrief.BriefID,
			"new_brief_id":             newBrief.BriefID,
			"new_brief_hash":           newBrief.BriefHash,
			"recovery_class":           afterRecovery.RecoveryClass,
			"recommended_action":       afterRecovery.RecommendedAction,
			"ready_for_next_run":       afterRecovery.ReadyForNextRun,
			"ready_for_handoff_launch": afterRecovery.ReadyForHandoffLaunch,
		}, runID); err != nil {
			return err
		}

		result = ExecuteRebriefResult{
			TaskID:                taskID,
			PreviousBriefID:       currentBrief.BriefID,
			BriefID:               newBrief.BriefID,
			BriefHash:             newBrief.BriefHash,
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
		return ExecuteRebriefResult{}, err
	}
	return result, nil
}
