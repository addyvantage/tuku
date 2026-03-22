package orchestrator

import (
	"fmt"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	rundomain "tuku/internal/domain/run"
)

type HandoffContinuityState string

const (
	HandoffContinuityStateNotApplicable            HandoffContinuityState = "NOT_APPLICABLE"
	HandoffContinuityStateAcceptedNotLaunched      HandoffContinuityState = "ACCEPTED_NOT_LAUNCHED"
	HandoffContinuityStateLaunchPendingOutcome     HandoffContinuityState = "LAUNCH_PENDING_OUTCOME"
	HandoffContinuityStateLaunchFailedRetryable    HandoffContinuityState = "LAUNCH_FAILED_RETRYABLE"
	HandoffContinuityStateLaunchCompletedAckSeen   HandoffContinuityState = "LAUNCH_COMPLETED_ACK_CAPTURED"
	HandoffContinuityStateLaunchCompletedAckEmpty  HandoffContinuityState = "LAUNCH_COMPLETED_ACK_UNAVAILABLE"
	HandoffContinuityStateLaunchCompletedAckLost   HandoffContinuityState = "LAUNCH_COMPLETED_ACK_MISSING"
	HandoffContinuityStateFollowThroughProofOfLife HandoffContinuityState = "FOLLOW_THROUGH_PROOF_OF_LIFE"
	HandoffContinuityStateFollowThroughConfirmed   HandoffContinuityState = "FOLLOW_THROUGH_CONFIRMED"
	HandoffContinuityStateFollowThroughUnknown     HandoffContinuityState = "FOLLOW_THROUGH_UNKNOWN"
	HandoffContinuityStateFollowThroughStalled     HandoffContinuityState = "FOLLOW_THROUGH_STALLED"
	HandoffContinuityStateResolved                 HandoffContinuityState = "RESOLVED"
)

type HandoffContinuity struct {
	TaskID                       common.TaskID                `json:"task_id"`
	HandoffID                    string                       `json:"handoff_id,omitempty"`
	TargetWorker                 rundomain.WorkerKind         `json:"target_worker,omitempty"`
	State                        HandoffContinuityState       `json:"state"`
	LaunchAttemptID              string                       `json:"launch_attempt_id,omitempty"`
	LaunchID                     string                       `json:"launch_id,omitempty"`
	LaunchStatus                 handoff.LaunchStatus         `json:"launch_status,omitempty"`
	AcknowledgmentID             string                       `json:"acknowledgment_id,omitempty"`
	AcknowledgmentStatus         handoff.AcknowledgmentStatus `json:"acknowledgment_status,omitempty"`
	AcknowledgmentSummary        string                       `json:"acknowledgment_summary,omitempty"`
	FollowThroughID              string                       `json:"follow_through_id,omitempty"`
	FollowThroughKind            handoff.FollowThroughKind    `json:"follow_through_kind,omitempty"`
	FollowThroughSummary         string                       `json:"follow_through_summary,omitempty"`
	ResolutionID                 string                       `json:"resolution_id,omitempty"`
	ResolutionKind               handoff.ResolutionKind       `json:"resolution_kind,omitempty"`
	ResolutionSummary            string                       `json:"resolution_summary,omitempty"`
	DownstreamContinuationProven bool                         `json:"downstream_continuation_proven"`
	Reason                       string                       `json:"reason,omitempty"`
}

func assessHandoffContinuity(taskID common.TaskID, packet *handoff.Packet, launch *handoff.Launch, ack *handoff.Acknowledgment, followThrough *handoff.FollowThrough, resolution *handoff.Resolution) HandoffContinuity {
	out := HandoffContinuity{
		TaskID:                       taskID,
		State:                        HandoffContinuityStateNotApplicable,
		DownstreamContinuationProven: false,
	}
	if packet == nil {
		out.Reason = "no Claude handoff continuity is active"
		return out
	}

	out.HandoffID = packet.HandoffID
	out.TargetWorker = packet.TargetWorker
	if packet.TargetWorker != rundomain.WorkerKindClaude {
		out.Reason = fmt.Sprintf("latest handoff target %s is not Claude", packet.TargetWorker)
		return out
	}
	if packet.Status != handoff.StatusAccepted {
		out.Reason = fmt.Sprintf("latest Claude handoff %s is in status %s and is not yet launch-active", packet.HandoffID, packet.Status)
		return out
	}
	if !packet.IsResumable {
		out.Reason = fmt.Sprintf("accepted Claude handoff %s is not launchable because its checkpoint is not resumable", packet.HandoffID)
		return out
	}

	control := assessLaunchControl(taskID, packet, launch)
	out.LaunchAttemptID = control.AttemptID
	out.LaunchID = control.LaunchID
	if launch != nil {
		out.LaunchStatus = launch.Status
	}
	if ack != nil {
		out.AcknowledgmentID = ack.AckID
		out.AcknowledgmentStatus = ack.Status
		out.AcknowledgmentSummary = ack.Summary
	}
	if followThrough != nil {
		out.FollowThroughID = followThrough.RecordID
		out.FollowThroughKind = followThrough.Kind
		out.FollowThroughSummary = followThrough.Summary
	}
	if resolution != nil && resolution.HandoffID == packet.HandoffID {
		out.ResolutionID = resolution.ResolutionID
		out.ResolutionKind = resolution.Kind
		out.ResolutionSummary = resolution.Summary
	}

	switch control.State {
	case LaunchControlStateNotRequested:
		out.State = HandoffContinuityStateAcceptedNotLaunched
		out.Reason = fmt.Sprintf("accepted Claude handoff %s is ready to launch, but no durable launch attempt exists yet", packet.HandoffID)
	case LaunchControlStateRequestedOutcomeUnknown:
		out.State = HandoffContinuityStateLaunchPendingOutcome
		out.Reason = fmt.Sprintf("Claude handoff launch attempt %s is durably recorded as requested, but completion and acknowledgment are still unproven", control.AttemptID)
	case LaunchControlStateFailed:
		out.State = HandoffContinuityStateLaunchFailedRetryable
		out.Reason = fmt.Sprintf("Claude handoff launch failed durably for attempt %s. Retry is allowed, but downstream continuation is still unproven", control.AttemptID)
	case LaunchControlStateCompleted:
		switch {
		case ack == nil:
			out.State = HandoffContinuityStateLaunchCompletedAckLost
			out.Reason = fmt.Sprintf("Claude handoff launch %s completed durably, but no persisted acknowledgment is available. Downstream continuation remains unproven and continuity repair is required", nonEmpty(control.LaunchID, control.AttemptID))
		default:
			if followThrough != nil {
				switch followThrough.Kind {
				case handoff.FollowThroughProofOfLifeObserved:
					out.State = HandoffContinuityStateFollowThroughProofOfLife
					out.DownstreamContinuationProven = true
					out.Reason = fmt.Sprintf("Claude handoff launch %s has downstream proof-of-life evidence: %s. This proves some downstream follow-through occurred, but not downstream task completion", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				case handoff.FollowThroughContinuationConfirmed:
					out.State = HandoffContinuityStateFollowThroughConfirmed
					out.DownstreamContinuationProven = true
					out.Reason = fmt.Sprintf("Claude handoff launch %s has operator-confirmed downstream continuation: %s. This does not prove downstream task completion or full transcript visibility", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				case handoff.FollowThroughContinuationUnknown:
					out.State = HandoffContinuityStateFollowThroughUnknown
					out.Reason = fmt.Sprintf("Claude handoff launch %s still has unknown downstream follow-through: %s", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				case handoff.FollowThroughStalledReviewRequired:
					out.State = HandoffContinuityStateFollowThroughStalled
					out.Reason = fmt.Sprintf("Claude handoff launch %s appears stalled and needs review: %s", nonEmpty(control.LaunchID, control.AttemptID), followThrough.Summary)
					return out
				}
			}
			if ack.Status == handoff.AcknowledgmentCaptured {
				out.State = HandoffContinuityStateLaunchCompletedAckSeen
				out.Reason = fmt.Sprintf("Claude handoff launch %s completed and an initial acknowledgment was captured. This proves launcher invocation and initial worker acknowledgment only; downstream continuation remains unproven", nonEmpty(control.LaunchID, control.AttemptID))
			} else {
				out.State = HandoffContinuityStateLaunchCompletedAckEmpty
				out.Reason = fmt.Sprintf("Claude handoff launch %s completed, but no usable initial acknowledgment was captured. Downstream continuation remains unproven", nonEmpty(control.LaunchID, control.AttemptID))
			}
		}
	default:
		out.Reason = control.Reason
	}
	if resolution != nil && resolution.HandoffID == packet.HandoffID {
		out.State = HandoffContinuityStateResolved
		out.Reason = buildResolvedHandoffContinuityReason(out, *resolution)
	}
	return out
}

func buildResolvedHandoffContinuityReason(out HandoffContinuity, resolution handoff.Resolution) string {
	switch resolution.Kind {
	case handoff.ResolutionAbandoned:
		return fmt.Sprintf("Claude handoff %s was explicitly abandoned: %s. This closes the Claude continuity branch without claiming downstream completion", out.HandoffID, resolution.Summary)
	case handoff.ResolutionSupersededByLocal:
		return fmt.Sprintf("Claude handoff %s was explicitly superseded by local control: %s. This closes the Claude continuity branch and unblocks local canonical mutation without claiming downstream completion", out.HandoffID, resolution.Summary)
	case handoff.ResolutionClosedUnproven:
		return fmt.Sprintf("Claude handoff %s was explicitly closed as unproven: %s. Tuku is not claiming downstream completion", out.HandoffID, resolution.Summary)
	case handoff.ResolutionReviewedStale:
		return fmt.Sprintf("Claude handoff %s was reviewed as stale and explicitly closed: %s. Tuku is not claiming downstream completion", out.HandoffID, resolution.Summary)
	default:
		return fmt.Sprintf("Claude handoff %s was explicitly resolved: %s", out.HandoffID, resolution.Summary)
	}
}
