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

type RecordRecoveryActionRequest struct {
	TaskID  string
	Kind    recoveryaction.Kind
	Summary string
	Notes   []string
}

type RecordRecoveryActionResult struct {
	TaskID                common.TaskID
	Action                recoveryaction.Record
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

type recoveryPhaseUpdate struct {
	Phase      phase.Phase
	NextAction string
	Reason     string
}

func (c *Coordinator) RecordRecoveryAction(ctx context.Context, req RecordRecoveryActionRequest) (RecordRecoveryActionResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordRecoveryActionResult{}, fmt.Errorf("task id is required")
	}
	if req.Kind == "" {
		return RecordRecoveryActionResult{}, fmt.Errorf("recovery action kind is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecordRecoveryActionResult{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	if replayableLatestRecoveryAction(req, assessment.LatestRecoveryAction) && recoveryActionReplayAllowed(req.Kind, recovery) {
		return RecordRecoveryActionResult{
			TaskID:                taskID,
			Action:                *assessment.LatestRecoveryAction,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     recoveryActionCanonical(*assessment.LatestRecoveryAction, recovery),
		}, nil
	}
	prepared, err := c.prepareRecoveryActionRecord(assessment, recovery, req)
	if err != nil {
		return RecordRecoveryActionResult{}, err
	}
	if reusableRecoveryAction(prepared.Template, assessment.LatestRecoveryAction) {
		return RecordRecoveryActionResult{
			TaskID:                taskID,
			Action:                *assessment.LatestRecoveryAction,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     recoveryActionCanonical(*assessment.LatestRecoveryAction, recovery),
		}, nil
	}

	var result RecordRecoveryActionResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return fmt.Errorf("task state changed during recovery action preparation (capsule version %d -> %d)", assessment.Capsule.Version, caps.Version)
		}

		runID := recoveryActionRunID(prepared.Template)
		if prepared.PhaseUpdate != nil {
			caps.Version++
			caps.UpdatedAt = txc.clock()
			caps.CurrentPhase = prepared.PhaseUpdate.Phase
			caps.NextAction = prepared.PhaseUpdate.NextAction
			if err := txc.store.Capsules().Update(caps); err != nil {
				return err
			}
			if err := txc.appendProof(caps, proof.EventTaskPhaseTransitioned, proof.ActorSystem, "tuku-daemon", map[string]any{
				"phase":  caps.CurrentPhase,
				"reason": prepared.PhaseUpdate.Reason,
			}, runID); err != nil {
				return err
			}
		}

		actionRecord := prepared.Template
		actionRecord.ActionID = txc.idGenerator("ract")
		actionRecord.CreatedAt = txc.clock()
		if err := txc.store.RecoveryActions().Create(actionRecord); err != nil {
			return err
		}

		afterAssessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		afterRecovery := txc.recoveryFromContinueAssessment(afterAssessment)
		canonical := recoveryActionCanonical(actionRecord, afterRecovery)
		eventType := proof.EventRecoveryActionRecorded
		if actionRecord.Kind == recoveryaction.KindInterruptedRunReviewed {
			eventType = proof.EventInterruptedRunReviewed
		}
		payload := map[string]any{
			"action_id":                actionRecord.ActionID,
			"kind":                     actionRecord.Kind,
			"summary":                  actionRecord.Summary,
			"notes":                    actionRecord.Notes,
			"run_id":                   actionRecord.RunID,
			"checkpoint_id":            actionRecord.CheckpointID,
			"handoff_id":               actionRecord.HandoffID,
			"launch_attempt_id":        actionRecord.LaunchAttemptID,
			"recovery_class":           afterRecovery.RecoveryClass,
			"recommended_action":       afterRecovery.RecommendedAction,
			"ready_for_next_run":       afterRecovery.ReadyForNextRun,
			"ready_for_handoff_launch": afterRecovery.ReadyForHandoffLaunch,
			"recovery_reason":          afterRecovery.Reason,
		}
		if err := txc.appendProof(caps, eventType, proof.ActorUser, "user", payload, runID); err != nil {
			return err
		}
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = RecordRecoveryActionResult{
			TaskID:                taskID,
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
		return RecordRecoveryActionResult{}, err
	}
	return result, nil
}

type preparedRecoveryAction struct {
	Template    recoveryaction.Record
	PhaseUpdate *recoveryPhaseUpdate
}

func (c *Coordinator) prepareRecoveryActionRecord(assessment continueAssessment, recovery RecoveryAssessment, req RecordRecoveryActionRequest) (preparedRecoveryAction, error) {
	template := recoveryaction.Record{
		Version: 1,
		TaskID:  assessment.TaskID,
		Kind:    req.Kind,
		Notes:   normalizedRecoveryNotes(req.Notes),
	}
	var phaseUpdate *recoveryPhaseUpdate

	switch req.Kind {
	case recoveryaction.KindFailedRunReviewed:
		if recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
			return preparedRecoveryAction{}, fmt.Errorf("failed-run review can only be recorded while recovery class is %s", RecoveryClassFailedRunReviewRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, fmt.Sprintf("Failed run %s reviewed.", nonEmpty(string(recovery.RunID), "unknown")))
	case recoveryaction.KindInterruptedRunReviewed:
		if recovery.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
			return preparedRecoveryAction{}, fmt.Errorf("interrupted-run review can only be recorded while recovery class is %s", RecoveryClassInterruptedRunRecoverable)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, fmt.Sprintf("Interrupted run %s reviewed.", nonEmpty(string(recovery.RunID), "unknown")))
	case recoveryaction.KindValidationReviewed:
		if recovery.RecoveryClass != RecoveryClassValidationReviewRequired {
			return preparedRecoveryAction{}, fmt.Errorf("validation review can only be recorded while recovery class is %s", RecoveryClassValidationReviewRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, fmt.Sprintf("Validation state for run %s reviewed.", nonEmpty(string(recovery.RunID), "unknown")))
	case recoveryaction.KindDecisionContinue:
		if recovery.RecoveryClass != RecoveryClassDecisionRequired {
			return preparedRecoveryAction{}, fmt.Errorf("continue decision can only be recorded while recovery class is %s", RecoveryClassDecisionRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.HandoffID = recovery.HandoffID
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Operator chose to continue with the current brief.")
		phaseUpdate = &recoveryPhaseUpdate{
			Phase:      phase.PhaseBriefReady,
			NextAction: "Continue branch selected. Execute continue recovery before starting the next bounded run.",
			Reason:     "operator recorded decision to continue with current brief",
		}
	case recoveryaction.KindDecisionRegenerateBrief:
		if recovery.RecoveryClass != RecoveryClassDecisionRequired {
			return preparedRecoveryAction{}, fmt.Errorf("regenerate-brief decision can only be recorded while recovery class is %s", RecoveryClassDecisionRequired)
		}
		template.RunID = recovery.RunID
		template.CheckpointID = recovery.CheckpointID
		template.HandoffID = recovery.HandoffID
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Operator chose to regenerate the execution brief before another run.")
		phaseUpdate = &recoveryPhaseUpdate{
			Phase:      phase.PhaseBlocked,
			NextAction: "Regenerate or replace the execution brief before starting another bounded run.",
			Reason:     "operator recorded decision to regenerate brief before continuing",
		}
	case recoveryaction.KindRepairIntentRecorded:
		if recovery.RecoveryClass != RecoveryClassRepairRequired && recovery.RecoveryClass != RecoveryClassBlockedDrift {
			return preparedRecoveryAction{}, fmt.Errorf("repair intent can only be recorded while recovery class is %s or %s", RecoveryClassRepairRequired, RecoveryClassBlockedDrift)
		}
		template.CheckpointID = recovery.CheckpointID
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Operator recorded repair intent for the current continuity issue.")
	case recoveryaction.KindPendingLaunchReviewed:
		if recovery.RecoveryClass != RecoveryClassHandoffLaunchPendingOutcome {
			return preparedRecoveryAction{}, fmt.Errorf("pending-launch review can only be recorded while recovery class is %s", RecoveryClassHandoffLaunchPendingOutcome)
		}
		template.HandoffID = recovery.HandoffID
		if assessment.LatestLaunch != nil {
			template.LaunchAttemptID = assessment.LatestLaunch.AttemptID
		}
		template.Summary = nonEmptyRecoverySummary(req.Summary, "Pending handoff launch reviewed; waiting for durable outcome.")
	default:
		return preparedRecoveryAction{}, fmt.Errorf("unsupported recovery action kind: %s", req.Kind)
	}

	return preparedRecoveryAction{Template: template, PhaseUpdate: phaseUpdate}, nil
}

func reusableRecoveryAction(candidate recoveryaction.Record, latest *recoveryaction.Record) bool {
	if latest == nil {
		return false
	}
	if latest.TaskID != candidate.TaskID || latest.Kind != candidate.Kind {
		return false
	}
	if latest.RunID != candidate.RunID || latest.CheckpointID != candidate.CheckpointID {
		return false
	}
	if latest.HandoffID != candidate.HandoffID || latest.LaunchAttemptID != candidate.LaunchAttemptID {
		return false
	}
	if strings.TrimSpace(latest.Summary) != strings.TrimSpace(candidate.Summary) {
		return false
	}
	if len(latest.Notes) != len(candidate.Notes) {
		return false
	}
	for i := range latest.Notes {
		if latest.Notes[i] != candidate.Notes[i] {
			return false
		}
	}
	return true
}

func normalizedRecoveryNotes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func replayableLatestRecoveryAction(req RecordRecoveryActionRequest, latest *recoveryaction.Record) bool {
	if latest == nil || latest.Kind != req.Kind {
		return false
	}
	if !stringSlicesEqual(normalizedRecoveryNotes(req.Notes), latest.Notes) {
		return false
	}
	summary := strings.TrimSpace(req.Summary)
	return summary == "" || summary == strings.TrimSpace(latest.Summary)
}

func recoveryActionReplayAllowed(kind recoveryaction.Kind, recovery RecoveryAssessment) bool {
	switch kind {
	case recoveryaction.KindInterruptedRunReviewed:
		return recovery.RecoveryClass == RecoveryClassInterruptedRunRecoverable
	default:
		return true
	}
}

func nonEmptyRecoverySummary(requested string, fallback string) string {
	if strings.TrimSpace(requested) == "" {
		return fallback
	}
	return strings.TrimSpace(requested)
}

func recoveryActionRunID(record recoveryaction.Record) *common.RunID {
	if record.RunID == "" {
		return nil
	}
	id := record.RunID
	return &id
}

func recoveryActionCanonical(record recoveryaction.Record, recovery RecoveryAssessment) string {
	switch record.Kind {
	case recoveryaction.KindFailedRunReviewed:
		return fmt.Sprintf("I recorded review of failed run %s. The next explicit recovery step is to decide whether to continue with the current brief or regenerate it.", nonEmpty(string(record.RunID), "unknown"))
	case recoveryaction.KindInterruptedRunReviewed:
		return fmt.Sprintf("I recorded review of interrupted run %s. The task remains in interrupted recovery posture; Tuku is preserving the interrupted execution path rather than claiming fresh-run readiness.", nonEmpty(string(record.RunID), "unknown"))
	case recoveryaction.KindValidationReviewed:
		return fmt.Sprintf("I recorded validation review for run %s. The next explicit recovery step is to decide whether to continue with the current brief or regenerate it.", nonEmpty(string(record.RunID), "unknown"))
	case recoveryaction.KindDecisionContinue:
		return "I recorded the operator decision to continue with the current brief. Execute continue recovery before starting the next bounded run."
	case recoveryaction.KindDecisionRegenerateBrief:
		return "I recorded the operator decision to regenerate the execution brief before another run. Tuku will not claim next-run readiness until a new or revised brief exists."
	case recoveryaction.KindRepairIntentRecorded:
		return fmt.Sprintf("I recorded repair intent for the current continuity issue. The task remains blocked until the repair is carried out. %s", strings.TrimSpace(recovery.Reason))
	case recoveryaction.KindPendingLaunchReviewed:
		return "I recorded review of the pending handoff launch. Retry remains blocked until Tuku has a durable launch outcome."
	default:
		return fmt.Sprintf("I recorded recovery action %s. Current recovery state is %s.", record.Kind, recovery.RecoveryClass)
	}
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
