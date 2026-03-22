package operatorstep

import (
	"time"

	"tuku/internal/domain/common"
)

type ResultClass string

const (
	ResultSucceeded  ResultClass = "SUCCEEDED"
	ResultRejected   ResultClass = "REJECTED"
	ResultFailed     ResultClass = "FAILED"
	ResultNoopReused ResultClass = "NOOP_REUSED"
)

type Receipt struct {
	Version int `json:"version"`

	ReceiptID          string              `json:"receipt_id"`
	TaskID             common.TaskID       `json:"task_id"`
	ActionHandle       string              `json:"action_handle"`
	ExecutionDomain    string              `json:"execution_domain,omitempty"`
	CommandSurfaceKind string              `json:"command_surface_kind,omitempty"`
	ExecutionAttempted bool                `json:"execution_attempted"`
	ResultClass        ResultClass         `json:"result_class"`
	Summary            string              `json:"summary,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	RunID              common.RunID        `json:"run_id,omitempty"`
	CheckpointID       common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID            common.BriefID      `json:"brief_id,omitempty"`
	HandoffID          string              `json:"handoff_id,omitempty"`
	LaunchAttemptID    string              `json:"launch_attempt_id,omitempty"`
	LaunchID           string              `json:"launch_id,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	CompletedAt        *time.Time          `json:"completed_at,omitempty"`
}
