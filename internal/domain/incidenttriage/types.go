package incidenttriage

import (
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/transition"
)

type AnchorMode string

const (
	AnchorModeLatestTransition AnchorMode = "LATEST_TRANSITION"
	AnchorModeTransitionID     AnchorMode = "TRANSITION_RECEIPT_ID"
)

type Posture string

const (
	PostureTriaged       Posture = "TRIAGED"
	PostureNeedsFollowUp Posture = "NEEDS_FOLLOW_UP"
	PostureDeferred      Posture = "DEFERRED"
)

type FollowUpPosture string

const (
	FollowUpPostureNone     FollowUpPosture = "NONE"
	FollowUpPostureAdvisory FollowUpPosture = "ADVISORY_OPEN"
	FollowUpPostureDeferred FollowUpPosture = "DEFERRED_OPEN"
)

// Receipt is a durable operator audit artifact for continuity incident triage.
// It does not prove correctness, completion, resumability, or transcript completeness.
type Receipt struct {
	Version int `json:"version"`

	ReceiptID common.EventID `json:"receipt_id"`
	TaskID    common.TaskID  `json:"task_id"`

	AnchorMode                AnchorMode      `json:"anchor_mode"`
	AnchorTransitionReceiptID common.EventID  `json:"anchor_transition_receipt_id"`
	AnchorTransitionKind      transition.Kind `json:"anchor_transition_kind"`
	AnchorHandoffID           string          `json:"anchor_handoff_id,omitempty"`
	AnchorShellSessionID      string          `json:"anchor_shell_session_id,omitempty"`

	Posture         Posture         `json:"posture"`
	FollowUpPosture FollowUpPosture `json:"follow_up_posture"`
	Summary         string          `json:"summary,omitempty"`

	ReviewGapPresent         bool                                                `json:"review_gap_present,omitempty"`
	ReviewPosture            transition.ReviewPosture                            `json:"review_posture,omitempty"`
	ReviewState              string                                              `json:"review_state,omitempty"`
	ReviewScope              shellsession.TranscriptSource                       `json:"review_scope,omitempty"`
	ReviewedUpToSequence     int64                                               `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64                                               `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64                                               `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int                                                 `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID           common.EventID                                      `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID     common.EventID                                      `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent    bool                                                `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass      shellsession.TranscriptReviewGapAcknowledgmentClass `json:"acknowledgment_class,omitempty"`

	RiskReviewGapPresent                bool   `json:"risk_review_gap_present,omitempty"`
	RiskAcknowledgmentPresent           bool   `json:"risk_acknowledgment_present,omitempty"`
	RiskStaleOrUnreviewedReviewPosture  bool   `json:"risk_stale_or_unreviewed_review_posture,omitempty"`
	RiskSourceScopedReviewPosture       bool   `json:"risk_source_scoped_review_posture,omitempty"`
	RiskIntoClaudeOwnershipTransition   bool   `json:"risk_into_claude_ownership_transition,omitempty"`
	RiskBackToLocalOwnershipTransition  bool   `json:"risk_back_to_local_ownership_transition,omitempty"`
	RiskUnresolvedContinuityAmbiguity   bool   `json:"risk_unresolved_continuity_ambiguity,omitempty"`
	RiskNearbyFailedOrInterruptedRuns   int    `json:"risk_nearby_failed_or_interrupted_runs,omitempty"`
	RiskNearbyRecoveryActions           int    `json:"risk_nearby_recovery_actions,omitempty"`
	RiskRecentFailureOrRecoveryActivity bool   `json:"risk_recent_failure_or_recovery_activity,omitempty"`
	RiskOperationallyNotable            bool   `json:"risk_operationally_notable,omitempty"`
	RiskSummary                         string `json:"risk_summary,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// ReceiptListFilter defines bounded deterministic incident triage history reads.
// Task scope is supplied by repository method parameters.
type ReceiptListFilter struct {
	Limit                     int            `json:"limit,omitempty"`
	BeforeReceiptID           common.EventID `json:"before_receipt_id,omitempty"`
	BeforeCreatedAt           time.Time      `json:"before_created_at,omitempty"`
	AnchorTransitionReceiptID common.EventID `json:"anchor_transition_receipt_id,omitempty"`
	Posture                   Posture        `json:"posture,omitempty"`
}

type FollowUpActionKind string

const (
	FollowUpActionRecordedPending FollowUpActionKind = "RECORDED_PENDING"
	FollowUpActionProgressed      FollowUpActionKind = "PROGRESSED"
	FollowUpActionClosed          FollowUpActionKind = "CLOSED"
	FollowUpActionReopened        FollowUpActionKind = "REOPENED"
)

// FollowUpReceipt is a durable append-only operator receipt for continuity
// incident follow-up progression. It does not prove correctness, completion,
// resumability, transcript completeness, or downstream worker completion.
type FollowUpReceipt struct {
	Version int `json:"version"`

	ReceiptID common.EventID `json:"receipt_id"`
	TaskID    common.TaskID  `json:"task_id"`

	AnchorMode                AnchorMode      `json:"anchor_mode"`
	AnchorTransitionReceiptID common.EventID  `json:"anchor_transition_receipt_id"`
	AnchorTransitionKind      transition.Kind `json:"anchor_transition_kind"`
	AnchorHandoffID           string          `json:"anchor_handoff_id,omitempty"`
	AnchorShellSessionID      string          `json:"anchor_shell_session_id,omitempty"`

	TriageReceiptID     common.EventID  `json:"triage_receipt_id"`
	TriagePosture       Posture         `json:"triage_posture"`
	TriageFollowUpState FollowUpPosture `json:"triage_follow_up_posture"`

	ActionKind FollowUpActionKind `json:"action_kind"`
	Summary    string             `json:"summary,omitempty"`

	ReviewGapPresent         bool                                                `json:"review_gap_present,omitempty"`
	ReviewPosture            transition.ReviewPosture                            `json:"review_posture,omitempty"`
	ReviewState              string                                              `json:"review_state,omitempty"`
	ReviewScope              shellsession.TranscriptSource                       `json:"review_scope,omitempty"`
	ReviewedUpToSequence     int64                                               `json:"reviewed_up_to_sequence,omitempty"`
	OldestUnreviewedSequence int64                                               `json:"oldest_unreviewed_sequence,omitempty"`
	NewestRetainedSequence   int64                                               `json:"newest_retained_sequence,omitempty"`
	UnreviewedRetainedCount  int                                                 `json:"unreviewed_retained_count,omitempty"`
	LatestReviewID           common.EventID                                      `json:"latest_review_id,omitempty"`
	LatestReviewGapAckID     common.EventID                                      `json:"latest_review_gap_acknowledgment_id,omitempty"`
	AcknowledgmentPresent    bool                                                `json:"acknowledgment_present,omitempty"`
	AcknowledgmentClass      shellsession.TranscriptReviewGapAcknowledgmentClass `json:"acknowledgment_class,omitempty"`
	TriagedUnderReviewRisk   bool                                                `json:"triaged_under_review_risk,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// FollowUpReceiptListFilter defines bounded deterministic continuity-incident
// follow-up receipt reads. Task scope is supplied by repository methods.
type FollowUpReceiptListFilter struct {
	Limit                     int                `json:"limit,omitempty"`
	BeforeReceiptID           common.EventID     `json:"before_receipt_id,omitempty"`
	BeforeCreatedAt           time.Time          `json:"before_created_at,omitempty"`
	AnchorTransitionReceiptID common.EventID     `json:"anchor_transition_receipt_id,omitempty"`
	TriageReceiptID           common.EventID     `json:"triage_receipt_id,omitempty"`
	ActionKind                FollowUpActionKind `json:"action_kind,omitempty"`
}

type Repository interface {
	Create(record Receipt) error
	GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (Receipt, error)
	LatestByTask(taskID common.TaskID) (Receipt, error)
	LatestByTaskAnchor(taskID common.TaskID, anchorTransitionReceiptID common.EventID) (Receipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]Receipt, error)
	ListByTaskFiltered(taskID common.TaskID, filter ReceiptListFilter) ([]Receipt, error)
}

type FollowUpRepository interface {
	Create(record FollowUpReceipt) error
	GetByTaskReceipt(taskID common.TaskID, receiptID common.EventID) (FollowUpReceipt, error)
	LatestByTask(taskID common.TaskID) (FollowUpReceipt, error)
	LatestByTaskAnchor(taskID common.TaskID, anchorTransitionReceiptID common.EventID) (FollowUpReceipt, error)
	ListByTask(taskID common.TaskID, limit int) ([]FollowUpReceipt, error)
	ListByTaskFiltered(taskID common.TaskID, filter FollowUpReceiptListFilter) ([]FollowUpReceipt, error)
}
