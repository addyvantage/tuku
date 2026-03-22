package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	rundomain "tuku/internal/domain/run"
)

type LaunchControlState string

const (
	LaunchControlStateNotApplicable           LaunchControlState = "NOT_APPLICABLE"
	LaunchControlStateNotRequested            LaunchControlState = "NOT_REQUESTED"
	LaunchControlStateRequestedOutcomeUnknown LaunchControlState = "REQUESTED_OUTCOME_UNKNOWN"
	LaunchControlStateCompleted               LaunchControlState = "COMPLETED"
	LaunchControlStateFailed                  LaunchControlState = "FAILED"
)

type LaunchRetryDisposition string

const (
	LaunchRetryDispositionNotApplicable LaunchRetryDisposition = "NOT_APPLICABLE"
	LaunchRetryDispositionAllowed       LaunchRetryDisposition = "ALLOWED"
	LaunchRetryDispositionBlocked       LaunchRetryDisposition = "BLOCKED"
)

type LaunchControl struct {
	TaskID           common.TaskID          `json:"task_id"`
	HandoffID        string                 `json:"handoff_id,omitempty"`
	AttemptID        string                 `json:"attempt_id,omitempty"`
	LaunchID         string                 `json:"launch_id,omitempty"`
	State            LaunchControlState     `json:"state"`
	RetryDisposition LaunchRetryDisposition `json:"retry_disposition"`
	Reason           string                 `json:"reason,omitempty"`
	TargetWorker     rundomain.WorkerKind   `json:"target_worker,omitempty"`
	RequestedAt      time.Time              `json:"requested_at,omitempty"`
	CompletedAt      time.Time              `json:"completed_at,omitempty"`
	FailedAt         time.Time              `json:"failed_at,omitempty"`
}

func assessLaunchControl(taskID common.TaskID, packet *handoff.Packet, launch *handoff.Launch) LaunchControl {
	control := LaunchControl{
		TaskID:           taskID,
		State:            LaunchControlStateNotApplicable,
		RetryDisposition: LaunchRetryDispositionNotApplicable,
	}
	if packet == nil {
		control.Reason = "no handoff packet is present"
		return control
	}
	control.HandoffID = packet.HandoffID
	control.TargetWorker = packet.TargetWorker

	if packet.TargetWorker != rundomain.WorkerKindClaude {
		control.Reason = fmt.Sprintf("latest handoff target %s is not launchable by the Claude launcher", packet.TargetWorker)
		return control
	}
	switch packet.Status {
	case handoff.StatusCreated, handoff.StatusAccepted:
	default:
		control.Reason = fmt.Sprintf("handoff %s is not launchable in status %s", packet.HandoffID, packet.Status)
		return control
	}
	if !packet.IsResumable {
		control.Reason = fmt.Sprintf("handoff %s is not launchable because its checkpoint is not resumable", packet.HandoffID)
		return control
	}
	if launch == nil {
		control.State = LaunchControlStateNotRequested
		control.RetryDisposition = LaunchRetryDispositionAllowed
		control.Reason = fmt.Sprintf("accepted handoff %s has no durable launch attempt yet", packet.HandoffID)
		return control
	}

	control.AttemptID = launch.AttemptID
	control.LaunchID = launch.LaunchID
	control.RequestedAt = launch.RequestedAt
	switch launch.Status {
	case handoff.LaunchStatusRequested:
		control.State = LaunchControlStateRequestedOutcomeUnknown
		control.RetryDisposition = LaunchRetryDispositionBlocked
		control.Reason = fmt.Sprintf("launch attempt %s for handoff %s is durably recorded as requested, but no completion or failure outcome is persisted", launch.AttemptID, packet.HandoffID)
	case handoff.LaunchStatusCompleted:
		control.State = LaunchControlStateCompleted
		control.RetryDisposition = LaunchRetryDispositionBlocked
		control.CompletedAt = launch.EndedAt
		if strings.TrimSpace(launch.LaunchID) != "" {
			control.Reason = fmt.Sprintf("handoff %s already has durable completed launch %s", packet.HandoffID, launch.LaunchID)
		} else {
			control.Reason = fmt.Sprintf("handoff %s already has a durable completed launch attempt %s", packet.HandoffID, launch.AttemptID)
		}
	case handoff.LaunchStatusFailed:
		control.State = LaunchControlStateFailed
		control.RetryDisposition = LaunchRetryDispositionAllowed
		control.FailedAt = launch.EndedAt
		if strings.TrimSpace(launch.ErrorMessage) != "" {
			control.Reason = fmt.Sprintf("previous launch attempt %s failed durably: %s", launch.AttemptID, launch.ErrorMessage)
		} else {
			control.Reason = fmt.Sprintf("previous launch attempt %s failed durably and may be retried", launch.AttemptID)
		}
	default:
		control.State = LaunchControlStateRequestedOutcomeUnknown
		control.RetryDisposition = LaunchRetryDispositionBlocked
		control.Reason = fmt.Sprintf("launch attempt %s has unsupported durable status %s", launch.AttemptID, launch.Status)
	}
	return control
}
