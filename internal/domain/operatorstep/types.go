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

	ReceiptID                        string              `json:"receipt_id"`
	TaskID                           common.TaskID       `json:"task_id"`
	ActionHandle                     string              `json:"action_handle"`
	ExecutionDomain                  string              `json:"execution_domain,omitempty"`
	CommandSurfaceKind               string              `json:"command_surface_kind,omitempty"`
	ExecutionAttempted               bool                `json:"execution_attempted"`
	ResultClass                      ResultClass         `json:"result_class"`
	Summary                          string              `json:"summary,omitempty"`
	Reason                           string              `json:"reason,omitempty"`
	RunID                            common.RunID        `json:"run_id,omitempty"`
	CheckpointID                     common.CheckpointID `json:"checkpoint_id,omitempty"`
	BriefID                          common.BriefID      `json:"brief_id,omitempty"`
	HandoffID                        string              `json:"handoff_id,omitempty"`
	LaunchAttemptID                  string              `json:"launch_attempt_id,omitempty"`
	LaunchID                         string              `json:"launch_id,omitempty"`
	ReviewGapState                   string              `json:"review_gap_state,omitempty"`
	ReviewGapSessionID               string              `json:"review_gap_session_id,omitempty"`
	ReviewGapClass                   string              `json:"review_gap_class,omitempty"`
	ReviewGapPresent                 bool                `json:"review_gap_present,omitempty"`
	ReviewGapReviewedUpTo            int64               `json:"review_gap_reviewed_up_to_sequence,omitempty"`
	ReviewGapOldestUnreviewed        int64               `json:"review_gap_oldest_unreviewed_sequence,omitempty"`
	ReviewGapNewestRetained          int64               `json:"review_gap_newest_retained_sequence,omitempty"`
	ReviewGapUnreviewedRetainedCount int                 `json:"review_gap_unreviewed_retained_count,omitempty"`
	ReviewGapAcknowledged            bool                `json:"review_gap_acknowledged,omitempty"`
	ReviewGapAcknowledgmentID        common.EventID      `json:"review_gap_acknowledgment_id,omitempty"`
	ReviewGapAcknowledgmentClass     string              `json:"review_gap_acknowledgment_class,omitempty"`
	TransitionReceiptID              common.EventID      `json:"transition_receipt_id,omitempty"`
	TransitionKind                   string              `json:"transition_kind,omitempty"`
	CreatedAt                        time.Time           `json:"created_at"`
	CompletedAt                      *time.Time          `json:"completed_at,omitempty"`
}
