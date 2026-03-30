package shellsession

import (
	"time"

	"tuku/internal/domain/common"
)

type TranscriptReviewGapAcknowledgmentClass string

const (
	TranscriptReviewGapAckMissingReviewMarker TranscriptReviewGapAcknowledgmentClass = "missing_review_marker"
	TranscriptReviewGapAckStaleReview         TranscriptReviewGapAcknowledgmentClass = "stale_review"
	TranscriptReviewGapAckSourceScopedOnly    TranscriptReviewGapAcknowledgmentClass = "source_scoped_only"
	TranscriptReviewGapAckSourceScopedStale   TranscriptReviewGapAcknowledgmentClass = "source_scoped_stale"
)

// TranscriptReviewGapAcknowledgment records explicit operator acknowledgment that
// progression happened despite retained transcript review gaps. It does not
// certify correctness, completion, resumability, or full transcript coverage.
type TranscriptReviewGapAcknowledgment struct {
	AcknowledgmentID common.EventID
	TaskID           common.TaskID
	SessionID        string
	Class            TranscriptReviewGapAcknowledgmentClass
	ReviewState      string
	ReviewScope      TranscriptSource

	ReviewedUpToSequence     int64
	OldestUnreviewedSequence int64
	NewestRetainedSequence   int64
	UnreviewedRetainedCount  int

	TranscriptState TranscriptState
	RetentionLimit  int
	RetainedChunks  int
	DroppedChunks   int

	ActionContext string
	Summary       string
	CreatedAt     time.Time
}
