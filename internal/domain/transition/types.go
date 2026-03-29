package transition

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
)

type Kind string

const (
	KindHandoffLaunch     Kind = "HANDOFF_LAUNCH"
	KindHandoffResolution Kind = "HANDOFF_RESOLUTION"
)

type ReviewPosture string

const (
	ReviewPostureNone                       ReviewPosture = "NONE"
	ReviewPostureNoRetainedEvidence         ReviewPosture = "NO_RETAINED_EVIDENCE"
	ReviewPostureRetainedEvidenceUnreviewed ReviewPosture = "RETAINED_EVIDENCE_UNREVIEWED"
	ReviewPostureGlobalReviewCurrent        ReviewPosture = "GLOBAL_REVIEW_CURRENT"
	ReviewPostureGlobalReviewStale          ReviewPosture = "GLOBAL_REVIEW_STALE"
	ReviewPostureSourceScopedReviewCurrent  ReviewPosture = "SOURCE_SCOPED_REVIEW_CURRENT"
	ReviewPostureSourceScopedReviewStale    ReviewPosture = "SOURCE_SCOPED_REVIEW_STALE"
)

// Receipt is a durable audit record for branch/handoff continuity transitions.
// It captures transition state and review-awareness posture at transition time.
type Receipt struct {
	Version int `json:"version"`

	ReceiptID        common.EventID `json:"receipt_id"`
	TaskID           common.TaskID  `json:"task_id"`
	ShellSessionID   string         `json:"shell_session_id,omitempty"`
	TransitionKind   Kind           `json:"transition_kind"`
	TransitionHandle string         `json:"transition_handle,omitempty"`
	TriggerAction    string         `json:"trigger_action,omitempty"`
	TriggerSource    string         `json:"trigger_source,omitempty"`
	HandoffID        string         `json:"handoff_id,omitempty"`
	LaunchAttemptID  string         `json:"launch_attempt_id,omitempty"`
	LaunchID         string         `json:"launch_id,omitempty"`
	ResolutionID     string         `json:"resolution_id,omitempty"`

	BranchClassBefore       string `json:"branch_class_before,omitempty"`
	BranchRefBefore         string `json:"branch_ref_before,omitempty"`
	BranchClassAfter        string `json:"branch_class_after,omitempty"`
	BranchRefAfter          string `json:"branch_ref_after,omitempty"`
	HandoffContinuityBefore string `json:"handoff_continuity_before,omitempty"`
	HandoffContinuityAfter  string `json:"handoff_continuity_after,omitempty"`
	LaunchControlBefore     string `json:"launch_control_before,omitempty"`
	LaunchControlAfter      string `json:"launch_control_after,omitempty"`

	ReviewGapPresent         bool                                                `json:"review_gap_present,omitempty"`
	ReviewPosture            ReviewPosture                                       `json:"review_posture,omitempty"`
	ReviewState              string                                              `json:"review_state,omitempty"`
	ReviewScope              shellsession.TranscriptSource                       `json:"review_scope,omitempty"`
	ReviewedUpToSequence     int64                                               `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64                                               `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64                                               `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int                                                 `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID           common.EventID                                      `json:"latest_review_id,omitempty"`
	LatestReviewAckID        common.EventID                                      `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent    bool                                                `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass      shellsession.TranscriptReviewGapAcknowledgmentClass `json:"acknowledgment_class,omitempty"`

	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ReceiptListFilter defines bounded deterministic transition receipt reads.
// It is intentionally narrow: task scope is required by the store method.
type ReceiptListFilter struct {
	Limit           int            `json:"limit,omitempty"`
	BeforeReceiptID common.EventID `json:"before_receipt_id,omitempty"`
	BeforeCreatedAt time.Time      `json:"before_created_at,omitempty"`
	TransitionKind  Kind           `json:"transition_kind,omitempty"`
	HandoffID       string         `json:"handoff_id,omitempty"`
}
