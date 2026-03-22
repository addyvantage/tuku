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

type RecordHandoffFollowThroughRequest struct {
	TaskID  string
	Kind    handoff.FollowThroughKind
	Summary string
	Notes   []string
}

type RecordHandoffFollowThroughResult struct {
	TaskID                common.TaskID
	Record                handoff.FollowThrough
	HandoffContinuity     HandoffContinuity
	RecoveryClass         RecoveryClass
	RecommendedAction     RecoveryAction
	ReadyForNextRun       bool
	ReadyForHandoffLaunch bool
	RecoveryReason        string
	CanonicalResponse     string
}

func (c *Coordinator) RecordHandoffFollowThrough(ctx context.Context, req RecordHandoffFollowThroughRequest) (RecordHandoffFollowThroughResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordHandoffFollowThroughResult{}, fmt.Errorf("task id is required")
	}
	if req.Kind == "" {
		return RecordHandoffFollowThroughResult{}, fmt.Errorf("follow-through kind is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return RecordHandoffFollowThroughResult{}, err
	}
	continuity := assessHandoffContinuity(taskID, assessment.LatestHandoff, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough, assessment.LatestResolution)
	if err := validateHandoffFollowThroughPosture(continuity, req.Kind); err != nil {
		return RecordHandoffFollowThroughResult{}, err
	}
	if replayableHandoffFollowThrough(req, assessment.LatestFollowThrough, continuity) {
		recovery := c.recoveryFromContinueAssessment(assessment)
		return RecordHandoffFollowThroughResult{
			TaskID:                taskID,
			Record:                *assessment.LatestFollowThrough,
			HandoffContinuity:     continuity,
			RecoveryClass:         recovery.RecoveryClass,
			RecommendedAction:     recovery.RecommendedAction,
			ReadyForNextRun:       recovery.ReadyForNextRun,
			ReadyForHandoffLaunch: recovery.ReadyForHandoffLaunch,
			RecoveryReason:        recovery.Reason,
			CanonicalResponse:     buildHandoffFollowThroughCanonical(continuity, *assessment.LatestFollowThrough),
		}, nil
	}

	var result RecordHandoffFollowThroughResult
	err = c.withTx(func(txc *Coordinator) error {
		current, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		continuity := assessHandoffContinuity(taskID, current.LatestHandoff, current.LatestLaunch, current.LatestAck, current.LatestFollowThrough, current.LatestResolution)
		if err := validateHandoffFollowThroughPosture(continuity, req.Kind); err != nil {
			return err
		}

		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}

		record := handoff.FollowThrough{
			Version:         1,
			RecordID:        txc.idGenerator("hft"),
			HandoffID:       current.LatestHandoff.HandoffID,
			LaunchAttemptID: current.LatestLaunch.AttemptID,
			LaunchID:        current.LatestLaunch.LaunchID,
			TaskID:          taskID,
			TargetWorker:    rundomain.WorkerKindClaude,
			Kind:            req.Kind,
			Summary:         normalizeHandoffFollowThroughSummary(req.Kind, req.Summary),
			Notes:           append([]string{}, req.Notes...),
			CreatedAt:       txc.clock(),
		}
		if err := txc.store.Handoffs().SaveFollowThrough(record); err != nil {
			return err
		}

		payload := map[string]any{
			"record_id":                    record.RecordID,
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
		if err := txc.appendProof(caps, proof.EventHandoffFollowThroughRecorded, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}

		current.LatestFollowThrough = &record
		updatedContinuity := assessHandoffContinuity(taskID, current.LatestHandoff, current.LatestLaunch, current.LatestAck, &record, current.LatestResolution)
		currentRecovery := txc.recoveryFromContinueAssessment(current)
		canonical := buildHandoffFollowThroughCanonical(updatedContinuity, record)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}

		result = RecordHandoffFollowThroughResult{
			TaskID:                taskID,
			Record:                record,
			HandoffContinuity:     updatedContinuity,
			RecoveryClass:         currentRecovery.RecoveryClass,
			RecommendedAction:     currentRecovery.RecommendedAction,
			ReadyForNextRun:       currentRecovery.ReadyForNextRun,
			ReadyForHandoffLaunch: currentRecovery.ReadyForHandoffLaunch,
			RecoveryReason:        currentRecovery.Reason,
			CanonicalResponse:     canonical,
		}
		return nil
	})
	if err != nil {
		return RecordHandoffFollowThroughResult{}, err
	}
	return result, nil
}

func validateHandoffFollowThroughPosture(continuity HandoffContinuity, kind handoff.FollowThroughKind) error {
	if continuity.TargetWorker != rundomain.WorkerKindClaude {
		return fmt.Errorf("handoff follow-through can only be recorded for Claude handoffs")
	}
	switch continuity.State {
	case HandoffContinuityStateLaunchCompletedAckSeen,
		HandoffContinuityStateLaunchCompletedAckEmpty,
		HandoffContinuityStateFollowThroughProofOfLife,
		HandoffContinuityStateFollowThroughConfirmed,
		HandoffContinuityStateFollowThroughUnknown,
		HandoffContinuityStateFollowThroughStalled:
	default:
		return fmt.Errorf("handoff follow-through kind %s can only be recorded while handoff continuity state is a launched Claude follow-through posture, got %s", kind, continuity.State)
	}
	return nil
}

func replayableHandoffFollowThrough(req RecordHandoffFollowThroughRequest, latest *handoff.FollowThrough, continuity HandoffContinuity) bool {
	if latest == nil {
		return false
	}
	if err := validateHandoffFollowThroughPosture(continuity, req.Kind); err != nil {
		return false
	}
	if latest.Kind != req.Kind {
		return false
	}
	if latest.HandoffID != continuity.HandoffID || latest.LaunchAttemptID != continuity.LaunchAttemptID {
		return false
	}
	summary := strings.TrimSpace(req.Summary)
	return summary == "" || summary == strings.TrimSpace(latest.Summary)
}

func normalizeHandoffFollowThroughSummary(kind handoff.FollowThroughKind, requested string) string {
	if summary := strings.TrimSpace(requested); summary != "" {
		return summary
	}
	switch kind {
	case handoff.FollowThroughProofOfLifeObserved:
		return "post-launch downstream proof of life observed"
	case handoff.FollowThroughContinuationConfirmed:
		return "operator confirmed downstream continuation occurred"
	case handoff.FollowThroughContinuationUnknown:
		return "downstream follow-through remains unknown"
	case handoff.FollowThroughStalledReviewRequired:
		return "downstream follow-through appears stalled and needs review"
	default:
		return "handoff follow-through recorded"
	}
}

func buildHandoffFollowThroughCanonical(continuity HandoffContinuity, record handoff.FollowThrough) string {
	switch record.Kind {
	case handoff.FollowThroughProofOfLifeObserved:
		return fmt.Sprintf("I recorded downstream proof-of-life evidence for Claude handoff %s: %s. This is stronger than initial launch acknowledgment, but it still does not prove downstream task completion.", continuity.HandoffID, record.Summary)
	case handoff.FollowThroughContinuationConfirmed:
		return fmt.Sprintf("I recorded operator confirmation that downstream Claude continuation occurred for handoff %s: %s. This does not prove downstream task completion or full transcript visibility.", continuity.HandoffID, record.Summary)
	case handoff.FollowThroughContinuationUnknown:
		return fmt.Sprintf("I recorded that downstream Claude follow-through remains unknown for handoff %s: %s. Tuku still does not have proof of downstream task completion.", continuity.HandoffID, record.Summary)
	case handoff.FollowThroughStalledReviewRequired:
		return fmt.Sprintf("I recorded that downstream Claude follow-through appears stalled for handoff %s: %s. Local fresh-run readiness remains blocked while this launched handoff needs review.", continuity.HandoffID, record.Summary)
	default:
		return fmt.Sprintf("I recorded downstream follow-through evidence for Claude handoff %s.", continuity.HandoffID)
	}
}
