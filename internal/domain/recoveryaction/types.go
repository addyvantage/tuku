package recoveryaction

import (
	"time"

	"tuku/internal/domain/common"
)

type Kind string

const (
	KindFailedRunReviewed         Kind = "FAILED_RUN_REVIEWED"
	KindInterruptedRunReviewed    Kind = "INTERRUPTED_RUN_REVIEWED"
	KindInterruptedResumeExecuted Kind = "INTERRUPTED_RESUME_EXECUTED"
	KindValidationReviewed        Kind = "VALIDATION_REVIEWED"
	KindDecisionContinue          Kind = "DECISION_CONTINUE"
	KindContinueExecuted          Kind = "CONTINUE_EXECUTED"
	KindDecisionRegenerateBrief   Kind = "DECISION_REGENERATE_BRIEF"
	KindRepairIntentRecorded      Kind = "REPAIR_INTENT_RECORDED"
	KindPendingLaunchReviewed     Kind = "PENDING_LAUNCH_REVIEWED"
)

type Record struct {
	Version int `json:"version"`

	ActionID string        `json:"action_id"`
	TaskID   common.TaskID `json:"task_id"`
	Kind     Kind          `json:"kind"`

	RunID           common.RunID        `json:"run_id,omitempty"`
	CheckpointID    common.CheckpointID `json:"checkpoint_id,omitempty"`
	HandoffID       string              `json:"handoff_id,omitempty"`
	LaunchAttemptID string              `json:"launch_attempt_id,omitempty"`

	Summary   string    `json:"summary"`
	Notes     []string  `json:"notes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Repository interface {
	Create(record Record) error
	LatestByTask(taskID common.TaskID) (Record, error)
	ListByTask(taskID common.TaskID, limit int) ([]Record, error)
}
