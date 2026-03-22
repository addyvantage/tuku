package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
)

type RecordHandoffResolutionRequest struct {
	TaskID    string
	HandoffID string
	Kind      handoff.ResolutionKind
	Summary   string
	Notes     []string
}

type RecordHandoffResolutionResult struct {
	TaskID                common.TaskID
	Record                handoff.Resolution
	HandoffContinuity     HandoffContinuity
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

func (c *Coordinator) RecordHandoffResolution(ctx context.Context, req RecordHandoffResolutionRequest) (RecordHandoffResolutionResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordHandoffResolutionResult{}, fmt.Errorf("task id is required")
	}
	if req.Kind == "" {
		return RecordHandoffResolutionResult{}, fmt.Errorf("handoff resolution kind is required")
	}
	if !validHandoffResolutionKind(req.Kind) {
		return RecordHandoffResolutionResult{}, fmt.Errorf("unsupported handoff resolution kind: %s", req.Kind)
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecordHandoffResolutionResult{}, err
	}
	continuity := assessHandoffContinuity(taskID, assessment.LatestHandoff, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough, assessment.LatestResolution)
	if replayableHandoffResolution(req, assessment.LatestResolution, continuity) {
		recovery := c.recoveryFromContinueAssessment(assessment)
		return RecordHandoffResolutionResult{
			TaskID:                taskID,
			Record:                *assessment.LatestResolution,
			HandoffContinuity:     continuity,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     buildHandoffResolutionCanonical(*assessment.LatestResolution),
		}, nil
	}
	if assessment.LatestResolution != nil && continuity.HandoffID == "" {
		requested := strings.TrimSpace(req.HandoffID)
		if requested == "" || requested == assessment.LatestResolution.HandoffID {
			return RecordHandoffResolutionResult{}, fmt.Errorf("Claude handoff branch %s is already resolved", assessment.LatestResolution.HandoffID)
		}
	}
	if err := validateHandoffResolutionTarget(req, continuity); err != nil {
		return RecordHandoffResolutionResult{}, err
	}

	var result RecordHandoffResolutionResult
	err = c.withTx(func(txc *Coordinator) error {
		current, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		continuity := assessHandoffContinuity(taskID, current.LatestHandoff, current.LatestLaunch, current.LatestAck, current.LatestFollowThrough, current.LatestResolution)
		if err := validateHandoffResolutionTarget(req, continuity); err != nil {
			return err
		}

		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}

		record := handoff.Resolution{
			Version:      1,
			ResolutionID: txc.idGenerator("hrs"),
			HandoffID:    current.LatestHandoff.HandoffID,
			TaskID:       taskID,
			TargetWorker: rundomain.WorkerKindClaude,
			Kind:         req.Kind,
			Summary:      normalizeHandoffResolutionSummary(req.Kind, req.Summary),
			Notes:        normalizedResolutionNotes(req.Notes),
			CreatedAt:    txc.clock(),
		}
		if current.LatestLaunch != nil {
			record.LaunchAttemptID = current.LatestLaunch.AttemptID
			record.LaunchID = current.LatestLaunch.LaunchID
		}
		if err := txc.store.Handoffs().SaveResolution(record); err != nil {
			return err
		}

		payload := map[string]any{
			"resolution_id":                record.ResolutionID,
			"handoff_id":                   record.HandoffID,
			"launch_attempt_id":            record.LaunchAttemptID,
			"launch_id":                    record.LaunchID,
			"target_worker":                record.TargetWorker,
			"kind":                         record.Kind,
			"summary":                      record.Summary,
			"notes":                        append([]string{}, record.Notes...),
			"downstream_completion_proven": false,
		}
		runID := runIDPointer(common.RunID(""))
		if current.LatestRun != nil {
			runID = runIDPointer(current.LatestRun.RunID)
		}
		if err := txc.appendProof(caps, proof.EventHandoffResolutionRecorded, proof.ActorUser, "user", payload, runID); err != nil {
			return err
		}

		current.LatestResolution = &record
		updatedContinuity := assessHandoffContinuity(taskID, current.LatestHandoff, current.LatestLaunch, current.LatestAck, current.LatestFollowThrough, &record)
		updatedRecovery := txc.recoveryFromContinueAssessment(current)
		canonical := buildHandoffResolutionCanonical(record)
		payload["handoff_continuity_state"] = updatedContinuity.State
		payload["recovery_class"] = updatedRecovery.RecoveryClass
		payload["recommended_action"] = updatedRecovery.RecommendedAction
		payload["ready_for_next_run"] = updatedRecovery.ReadyForNextRun
		payload["ready_for_handoff_launch"] = updatedRecovery.ReadyForHandoffLaunch
		payload["recovery_reason"] = updatedRecovery.Reason
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = RecordHandoffResolutionResult{
			TaskID:                taskID,
			Record:                record,
			HandoffContinuity:     updatedContinuity,
			RecoveryClass:         updatedRecovery.RecoveryClass,
			RecommendedAction:     updatedRecovery.RecommendedAction,
			ReadyForNextRun:       updatedRecovery.ReadyForNextRun,
			ReadyForHandoffLaunch: updatedRecovery.ReadyForHandoffLaunch,
			RecoveryReason:        updatedRecovery.Reason,
			CanonicalResponse:     canonical,
		}
		return nil
	})
	if err != nil {
		return RecordHandoffResolutionResult{}, err
	}
	return result, nil
}

func validateHandoffResolutionTarget(req RecordHandoffResolutionRequest, continuity HandoffContinuity) error {
	if continuity.State == HandoffContinuityStateNotApplicable || continuity.HandoffID == "" {
		return fmt.Errorf("no active Claude handoff branch exists for handoff resolution")
	}
	if continuity.TargetWorker != rundomain.WorkerKindClaude {
		return fmt.Errorf("handoff resolution can only be recorded for Claude handoff continuity")
	}
	if handoffID := strings.TrimSpace(req.HandoffID); handoffID != "" && handoffID != continuity.HandoffID {
		return fmt.Errorf("handoff resolution target %s does not match active Claude handoff branch %s", handoffID, continuity.HandoffID)
	}
	switch continuity.State {
	case HandoffContinuityStateAcceptedNotLaunched,
		HandoffContinuityStateLaunchPendingOutcome,
		HandoffContinuityStateLaunchFailedRetryable,
		HandoffContinuityStateLaunchCompletedAckSeen,
		HandoffContinuityStateLaunchCompletedAckEmpty,
		HandoffContinuityStateLaunchCompletedAckLost,
		HandoffContinuityStateFollowThroughProofOfLife,
		HandoffContinuityStateFollowThroughConfirmed,
		HandoffContinuityStateFollowThroughUnknown,
		HandoffContinuityStateFollowThroughStalled:
		return nil
	case HandoffContinuityStateResolved:
		return fmt.Errorf("Claude handoff branch %s is already resolved", continuity.HandoffID)
	default:
		return fmt.Errorf("handoff resolution kind %s requires an active Claude handoff branch, got %s", req.Kind, continuity.State)
	}
}

func replayableHandoffResolution(req RecordHandoffResolutionRequest, latest *handoff.Resolution, continuity HandoffContinuity) bool {
	if latest == nil {
		return false
	}
	if continuity.HandoffID != "" {
		return false
	}
	if strings.TrimSpace(req.HandoffID) != "" && strings.TrimSpace(req.HandoffID) != latest.HandoffID {
		return false
	}
	if latest.Kind != req.Kind {
		return false
	}
	summary := strings.TrimSpace(req.Summary)
	return summary == "" || summary == strings.TrimSpace(latest.Summary)
}

func normalizeHandoffResolutionSummary(kind handoff.ResolutionKind, requested string) string {
	if summary := strings.TrimSpace(requested); summary != "" {
		return summary
	}
	switch kind {
	case handoff.ResolutionAbandoned:
		return "operator explicitly abandoned the Claude handoff branch"
	case handoff.ResolutionSupersededByLocal:
		return "operator explicitly returned canonical control to the local Tuku branch"
	case handoff.ResolutionClosedUnproven:
		return "operator explicitly closed the Claude handoff branch without downstream completion proof"
	case handoff.ResolutionReviewedStale:
		return "operator reviewed the Claude handoff branch as stale and closed it"
	default:
		return "operator explicitly resolved the Claude handoff branch"
	}
}

func validHandoffResolutionKind(kind handoff.ResolutionKind) bool {
	switch kind {
	case handoff.ResolutionAbandoned,
		handoff.ResolutionSupersededByLocal,
		handoff.ResolutionClosedUnproven,
		handoff.ResolutionReviewedStale:
		return true
	default:
		return false
	}
}

func normalizedResolutionNotes(notes []string) []string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		if trimmed := strings.TrimSpace(note); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func buildHandoffResolutionCanonical(record handoff.Resolution) string {
	switch record.Kind {
	case handoff.ResolutionAbandoned:
		return fmt.Sprintf("I explicitly abandoned Claude handoff branch %s: %s. Tuku is not claiming downstream completion, but the Claude continuity branch is no longer active-blocking for local control.", record.HandoffID, record.Summary)
	case handoff.ResolutionSupersededByLocal:
		return fmt.Sprintf("I explicitly superseded Claude handoff branch %s with local Tuku control: %s. This closes the Claude continuity branch without claiming downstream completion.", record.HandoffID, record.Summary)
	case handoff.ResolutionClosedUnproven:
		return fmt.Sprintf("I explicitly closed Claude handoff branch %s as unproven: %s. Tuku is not claiming downstream completion.", record.HandoffID, record.Summary)
	case handoff.ResolutionReviewedStale:
		return fmt.Sprintf("I reviewed Claude handoff branch %s as stale and explicitly closed it: %s. Tuku is not claiming downstream completion.", record.HandoffID, record.Summary)
	default:
		return fmt.Sprintf("I explicitly resolved Claude handoff branch %s: %s.", record.HandoffID, record.Summary)
	}
}
