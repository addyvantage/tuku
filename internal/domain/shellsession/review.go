package shellsession

import (
	"time"

	"tuku/internal/domain/common"
)

type TranscriptReviewClosureState string

const (
	TranscriptReviewClosureNone                TranscriptReviewClosureState = "none"
	TranscriptReviewClosureGlobalCurrent       TranscriptReviewClosureState = "global_review_current_within_retained"
	TranscriptReviewClosureGlobalStale         TranscriptReviewClosureState = "global_review_stale_within_retained"
	TranscriptReviewClosureSourceScopedCurrent TranscriptReviewClosureState = "source_scoped_review_current_within_retained"
	TranscriptReviewClosureSourceScopedStale   TranscriptReviewClosureState = "source_scoped_review_stale_within_retained"
)

// TranscriptReview records operator attestation of bounded transcript evidence.
// It does not imply task completion, transcript correctness, or resumability.
type TranscriptReview struct {
	ReviewID  common.EventID
	TaskID    common.TaskID
	SessionID string
	// SourceFilter is empty when the review applies to all retained sources.
	SourceFilter           TranscriptSource
	ReviewedUpToSequence   int64
	Summary                string
	TranscriptState        TranscriptState
	RetentionLimit         int
	RetainedChunks         int
	DroppedChunks          int
	OldestRetainedSequence int64
	NewestRetainedSequence int64
	CreatedAt              time.Time
}
