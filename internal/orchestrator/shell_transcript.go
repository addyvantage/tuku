package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/shellsession"
)

const (
	defaultShellTranscriptReadLimit     = 40
	maxShellTranscriptReadLimit         = 200
	defaultTranscriptReviewHistoryLimit = 10
	maxTranscriptReviewHistoryLimit     = 100
	maxTranscriptReviewSummaryChars     = 400
)

type RecordShellTranscriptChunk struct {
	Source    shellsession.TranscriptSource
	Content   string
	CreatedAt time.Time
}

type RecordShellTranscriptRequest struct {
	TaskID    string
	SessionID string
	Chunks    []RecordShellTranscriptChunk
}

type RecordShellTranscriptResult struct {
	TaskID    common.TaskID
	SessionID string
	Summary   shellsession.TranscriptSummary
}

type ReadShellTranscriptRequest struct {
	TaskID         string
	SessionID      string
	Limit          int
	BeforeSequence int64
	Source         string
}

type ShellTranscriptSourceSummary struct {
	Source shellsession.TranscriptSource
	Chunks int
}

type ReadShellTranscriptResult struct {
	TaskID                  common.TaskID
	SessionID               string
	TranscriptState         shellsession.TranscriptState
	TranscriptOnly          bool
	Bounded                 bool
	Partial                 bool
	RetentionLimit          int
	RetainedChunkCount      int
	DroppedChunkCount       int
	LastSequence            int64
	LastChunkAt             time.Time
	OldestRetainedSequence  int64
	NewestRetainedSequence  int64
	OldestRetainedChunkAt   time.Time
	NewestRetainedChunkAt   time.Time
	SourceSummary           []ShellTranscriptSourceSummary
	RequestedLimit          int
	RequestedBeforeSequence *int64
	RequestedSource         shellsession.TranscriptSource
	PageOldestSequence      int64
	PageNewestSequence      int64
	PageChunkCount          int
	HasMoreOlder            bool
	NextBeforeSequence      *int64
	LatestReview            *ShellTranscriptReviewSummary
	HasUnreadNewerEvidence  bool
	PageFullyReviewed       bool
	PageCrossesReview       bool
	PageHasUnreviewed       bool
	Closure                 ShellTranscriptReviewClosure
	Chunks                  []shellsession.TranscriptChunk
}

type ShellTranscriptReviewSummary struct {
	ReviewID                 common.EventID
	SourceFilter             shellsession.TranscriptSource
	ReviewedUpToSequence     int64
	Summary                  string
	CreatedAt                time.Time
	TranscriptState          shellsession.TranscriptState
	RetentionLimit           int
	RetainedChunks           int
	DroppedChunks            int
	OldestRetainedSequence   int64
	NewestRetainedSequence   int64
	StaleBehindLatest        bool
	NewerRetainedCount       int
	OldestUnreviewedSequence int64
	ClosureState             shellsession.TranscriptReviewClosureState
}

type ShellTranscriptReviewClosure struct {
	State                    shellsession.TranscriptReviewClosureState
	Scope                    shellsession.TranscriptSource
	HasReview                bool
	HasUnreadNewerEvidence   bool
	ReviewedUpToSequence     int64
	OldestUnreviewedSequence int64
	NewestRetainedSequence   int64
	UnreviewedRetainedCount  int
	RetentionLimit           int
	RetainedChunkCount       int
	DroppedChunkCount        int
}

type RecordShellTranscriptReviewRequest struct {
	TaskID          string
	SessionID       string
	ReviewedUpToSeq int64
	Source          string
	Summary         string
}

type RecordShellTranscriptReviewResult struct {
	TaskID                 common.TaskID
	SessionID              string
	TranscriptState        shellsession.TranscriptState
	RetentionLimit         int
	RetainedChunkCount     int
	DroppedChunkCount      int
	OldestRetainedSequence int64
	NewestRetainedSequence int64
	LatestReview           ShellTranscriptReviewSummary
	HasUnreadNewerEvidence bool
	Closure                ShellTranscriptReviewClosure
}

type ReadShellTranscriptReviewHistoryRequest struct {
	TaskID    string
	SessionID string
	Source    string
	Limit     int
}

type ReadShellTranscriptReviewHistoryResult struct {
	TaskID                 common.TaskID
	SessionID              string
	TranscriptState        shellsession.TranscriptState
	TranscriptOnly         bool
	Bounded                bool
	Partial                bool
	RetentionLimit         int
	RetainedChunkCount     int
	DroppedChunkCount      int
	OldestRetainedSequence int64
	NewestRetainedSequence int64
	RequestedLimit         int
	RequestedSource        shellsession.TranscriptSource
	Closure                ShellTranscriptReviewClosure
	LatestReview           *ShellTranscriptReviewSummary
	Reviews                []ShellTranscriptReviewSummary
}

func (c *Coordinator) RecordShellTranscript(ctx context.Context, req RecordShellTranscriptRequest) (RecordShellTranscriptResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordShellTranscriptResult{}, fmt.Errorf("task id is required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return RecordShellTranscriptResult{}, fmt.Errorf("session id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return RecordShellTranscriptResult{}, err
	}
	if _, err := c.loadShellSessionRecord(taskID, sessionID); err != nil {
		return RecordShellTranscriptResult{}, err
	}
	normalized := make([]shellsession.TranscriptChunk, 0, len(req.Chunks))
	for _, chunk := range req.Chunks {
		content := strings.TrimSpace(chunk.Content)
		if content == "" {
			continue
		}
		normalized = append(normalized, shellsession.TranscriptChunk{
			TaskID:    taskID,
			SessionID: sessionID,
			Source:    chunk.Source,
			Content:   content,
			CreatedAt: chunk.CreatedAt.UTC(),
		})
	}
	var (
		summary shellsession.TranscriptSummary
		err     error
	)
	if len(normalized) == 0 {
		summary, err = c.shellTranscriptSummary(taskID, sessionID)
	} else {
		summary, err = c.appendShellTranscript(taskID, sessionID, normalized)
	}
	if err != nil {
		return RecordShellTranscriptResult{}, err
	}
	return RecordShellTranscriptResult{
		TaskID:    taskID,
		SessionID: sessionID,
		Summary:   summary,
	}, nil
}

func (c *Coordinator) ReadShellTranscript(ctx context.Context, req ReadShellTranscriptRequest) (ReadShellTranscriptResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadShellTranscriptResult{}, fmt.Errorf("task id is required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return ReadShellTranscriptResult{}, fmt.Errorf("session id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReadShellTranscriptResult{}, err
	}
	record, err := c.loadShellSessionRecord(taskID, sessionID)
	if err != nil {
		return ReadShellTranscriptResult{}, err
	}

	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultShellTranscriptReadLimit
	case limit > maxShellTranscriptReadLimit:
		limit = maxShellTranscriptReadLimit
	}
	var requestedBefore *int64
	if req.BeforeSequence > 0 {
		value := req.BeforeSequence
		requestedBefore = &value
	}

	sourceFilter, err := parseTranscriptSourceFilter(strings.TrimSpace(req.Source))
	if err != nil {
		return ReadShellTranscriptResult{}, err
	}

	summary, err := c.shellTranscriptSummary(taskID, sessionID)
	if err != nil {
		return ReadShellTranscriptResult{}, err
	}
	view := classifyShellSession(record, c.clock(), c.shellSessionStaleAfter)
	applyTranscriptSummaryToView(&view, summary)

	chunks, hasMoreOlder, err := c.listShellTranscriptPage(taskID, sessionID, req.BeforeSequence, limit, sourceFilter)
	if err != nil {
		return ReadShellTranscriptResult{}, err
	}
	sourceSummary := make([]ShellTranscriptSourceSummary, 0, len(summary.SourceCounts))
	for _, entry := range summary.SourceCounts {
		sourceSummary = append(sourceSummary, ShellTranscriptSourceSummary{
			Source: entry.Source,
			Chunks: entry.Chunks,
		})
	}

	result := ReadShellTranscriptResult{
		TaskID:                  taskID,
		SessionID:               sessionID,
		TranscriptState:         view.TranscriptState,
		TranscriptOnly:          isTranscriptOnlyMode(view.HostMode, view.HostState),
		Bounded:                 true,
		Partial:                 summary.DroppedChunks > 0,
		RetentionLimit:          summary.RetentionLimit,
		RetainedChunkCount:      summary.RetainedChunks,
		DroppedChunkCount:       summary.DroppedChunks,
		LastSequence:            summary.LastSequenceNo,
		LastChunkAt:             summary.LastChunkAt,
		OldestRetainedSequence:  summary.OldestSequenceNo,
		NewestRetainedSequence:  summary.NewestSequenceNo,
		OldestRetainedChunkAt:   summary.OldestChunkAt,
		NewestRetainedChunkAt:   summary.NewestChunkAt,
		SourceSummary:           sourceSummary,
		RequestedLimit:          limit,
		RequestedSource:         sourceFilter,
		RequestedBeforeSequence: requestedBefore,
		HasMoreOlder:            hasMoreOlder,
		Chunks:                  chunks,
	}
	if len(chunks) > 0 {
		result.PageChunkCount = len(chunks)
		result.PageOldestSequence = chunks[0].SequenceNo
		result.PageNewestSequence = chunks[len(chunks)-1].SequenceNo
		if hasMoreOlder {
			next := result.PageOldestSequence
			result.NextBeforeSequence = &next
		}
	}
	latestReview, reviewScopeNewest, err := c.latestShellTranscriptReviewForRead(taskID, sessionID, sourceFilter, summary)
	if err != nil {
		return ReadShellTranscriptResult{}, err
	}
	result.Closure = deriveTranscriptReviewClosureFromLatest(nil, sourceFilter, summary.NewestSequenceNo, summary)
	if latestReview != nil {
		result.LatestReview = latestReview
		result.HasUnreadNewerEvidence = latestReview.StaleBehindLatest
		result.Closure = deriveTranscriptReviewClosureFromLatest(latestReview, sourceFilter, reviewScopeNewest, summary)
		if len(chunks) > 0 {
			result.PageFullyReviewed = result.PageNewestSequence <= latestReview.ReviewedUpToSequence
			result.PageCrossesReview = result.PageOldestSequence <= latestReview.ReviewedUpToSequence && result.PageNewestSequence > latestReview.ReviewedUpToSequence
			result.PageHasUnreviewed = result.PageNewestSequence > latestReview.ReviewedUpToSequence
			if reviewScopeNewest > latestReview.ReviewedUpToSequence {
				result.HasUnreadNewerEvidence = true
			}
		}
	}
	return result, nil
}

func (c *Coordinator) RecordShellTranscriptReview(ctx context.Context, req RecordShellTranscriptReviewRequest) (RecordShellTranscriptReviewResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordShellTranscriptReviewResult{}, fmt.Errorf("task id is required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return RecordShellTranscriptReviewResult{}, fmt.Errorf("session id is required")
	}
	if req.ReviewedUpToSeq <= 0 {
		return RecordShellTranscriptReviewResult{}, fmt.Errorf("reviewed sequence must be greater than zero")
	}
	sourceFilter, err := parseTranscriptSourceFilter(strings.TrimSpace(req.Source))
	if err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	record, err := c.loadShellSessionRecord(taskID, sessionID)
	if err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	summary, err := c.shellTranscriptSummary(taskID, sessionID)
	if err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	if summary.RetainedChunks <= 0 {
		return RecordShellTranscriptReviewResult{}, fmt.Errorf("no retained transcript evidence exists for this shell session")
	}

	validationLimit := summary.RetentionLimit
	if validationLimit <= 0 {
		validationLimit = shellsession.DefaultTranscriptRetentionChunks
	}
	if summary.RetainedChunks > validationLimit {
		validationLimit = summary.RetainedChunks
	}
	chunks, _, err := c.listShellTranscriptPage(taskID, sessionID, 0, validationLimit+1, sourceFilter)
	if err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	if len(chunks) == 0 {
		if sourceFilter != "" {
			return RecordShellTranscriptReviewResult{}, fmt.Errorf("no retained transcript evidence exists for source %s", sourceFilter)
		}
		return RecordShellTranscriptReviewResult{}, fmt.Errorf("no retained transcript evidence exists for this shell session")
	}
	if req.ReviewedUpToSeq < chunks[0].SequenceNo || req.ReviewedUpToSeq > chunks[len(chunks)-1].SequenceNo {
		return RecordShellTranscriptReviewResult{}, fmt.Errorf(
			"reviewed sequence %d is outside retained transcript window %d-%d for selected scope",
			req.ReviewedUpToSeq,
			chunks[0].SequenceNo,
			chunks[len(chunks)-1].SequenceNo,
		)
	}
	sequenceExists := false
	for _, chunk := range chunks {
		if chunk.SequenceNo == req.ReviewedUpToSeq {
			sequenceExists = true
			break
		}
	}
	if !sequenceExists {
		return RecordShellTranscriptReviewResult{}, fmt.Errorf(
			"reviewed sequence %d is not present in retained transcript evidence for selected scope",
			req.ReviewedUpToSeq,
		)
	}

	view := classifyShellSession(record, c.clock(), c.shellSessionStaleAfter)
	applyTranscriptSummaryToView(&view, summary)
	review := shellsession.TranscriptReview{
		ReviewID:               common.EventID(c.idGenerator("srev")),
		TaskID:                 taskID,
		SessionID:              sessionID,
		SourceFilter:           sourceFilter,
		ReviewedUpToSequence:   req.ReviewedUpToSeq,
		Summary:                normalizeTranscriptReviewSummary(req.Summary),
		TranscriptState:        view.TranscriptState,
		RetentionLimit:         summary.RetentionLimit,
		RetainedChunks:         summary.RetainedChunks,
		DroppedChunks:          summary.DroppedChunks,
		OldestRetainedSequence: summary.OldestSequenceNo,
		NewestRetainedSequence: summary.NewestSequenceNo,
		CreatedAt:              c.clock().UTC(),
	}
	storedReview, err := c.appendShellTranscriptReview(review)
	if err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	reviewSummary := shellTranscriptReviewSummary(storedReview, summary.NewestSequenceNo)
	payload := map[string]any{
		"review_id":                  storedReview.ReviewID,
		"session_id":                 sessionID,
		"source_filter":              storedReview.SourceFilter,
		"reviewed_up_to_sequence":    storedReview.ReviewedUpToSequence,
		"summary":                    storedReview.Summary,
		"transcript_state":           storedReview.TranscriptState,
		"retention_limit":            storedReview.RetentionLimit,
		"retained_chunks":            storedReview.RetainedChunks,
		"dropped_chunks":             storedReview.DroppedChunks,
		"oldest_retained_sequence":   storedReview.OldestRetainedSequence,
		"newest_retained_sequence":   storedReview.NewestRetainedSequence,
		"newer_retained_evidence":    reviewSummary.StaleBehindLatest,
		"newer_retained_chunk_count": reviewSummary.NewerRetainedCount,
	}
	if err := c.appendProof(caps, proof.EventTranscriptEvidenceReviewed, proof.ActorUser, "user", payload, nil); err != nil {
		return RecordShellTranscriptReviewResult{}, err
	}
	return RecordShellTranscriptReviewResult{
		TaskID:                 taskID,
		SessionID:              sessionID,
		TranscriptState:        view.TranscriptState,
		RetentionLimit:         summary.RetentionLimit,
		RetainedChunkCount:     summary.RetainedChunks,
		DroppedChunkCount:      summary.DroppedChunks,
		OldestRetainedSequence: summary.OldestSequenceNo,
		NewestRetainedSequence: summary.NewestSequenceNo,
		LatestReview:           reviewSummary,
		HasUnreadNewerEvidence: reviewSummary.StaleBehindLatest,
		Closure:                deriveTranscriptReviewClosureFromLatest(&reviewSummary, sourceFilter, reviewSummary.NewestRetainedSequence, summary),
	}, nil
}

func (c *Coordinator) ReadShellTranscriptReviewHistory(ctx context.Context, req ReadShellTranscriptReviewHistoryRequest) (ReadShellTranscriptReviewHistoryResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadShellTranscriptReviewHistoryResult{}, fmt.Errorf("task id is required")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return ReadShellTranscriptReviewHistoryResult{}, fmt.Errorf("session id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReadShellTranscriptReviewHistoryResult{}, err
	}
	record, err := c.loadShellSessionRecord(taskID, sessionID)
	if err != nil {
		return ReadShellTranscriptReviewHistoryResult{}, err
	}
	sourceFilter, err := parseTranscriptSourceFilter(strings.TrimSpace(req.Source))
	if err != nil {
		return ReadShellTranscriptReviewHistoryResult{}, err
	}
	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultTranscriptReviewHistoryLimit
	case limit > maxTranscriptReviewHistoryLimit:
		limit = maxTranscriptReviewHistoryLimit
	}

	summary, err := c.shellTranscriptSummary(taskID, sessionID)
	if err != nil {
		return ReadShellTranscriptReviewHistoryResult{}, err
	}
	view := classifyShellSession(record, c.clock(), c.shellSessionStaleAfter)
	applyTranscriptSummaryToView(&view, summary)

	var reviews []shellsession.TranscriptReview
	if sourceFilter == "" {
		reviews, err = c.listShellTranscriptReviewsAnyScope(taskID, sessionID, limit)
	} else {
		reviews, err = c.listShellTranscriptReviews(taskID, sessionID, sourceFilter, limit)
	}
	if err != nil {
		return ReadShellTranscriptReviewHistoryResult{}, err
	}
	sourceNewest, err := c.latestShellTranscriptSequenceForSource(taskID, sessionID, sourceFilter)
	if err != nil {
		return ReadShellTranscriptReviewHistoryResult{}, err
	}
	closureNewest := summary.NewestSequenceNo
	if sourceFilter != "" && sourceNewest > 0 {
		closureNewest = sourceNewest
	}

	result := ReadShellTranscriptReviewHistoryResult{
		TaskID:                 taskID,
		SessionID:              sessionID,
		TranscriptState:        view.TranscriptState,
		TranscriptOnly:         isTranscriptOnlyMode(view.HostMode, view.HostState),
		Bounded:                true,
		Partial:                summary.DroppedChunks > 0,
		RetentionLimit:         summary.RetentionLimit,
		RetainedChunkCount:     summary.RetainedChunks,
		DroppedChunkCount:      summary.DroppedChunks,
		OldestRetainedSequence: summary.OldestSequenceNo,
		NewestRetainedSequence: summary.NewestSequenceNo,
		RequestedLimit:         limit,
		RequestedSource:        sourceFilter,
	}
	if len(reviews) == 0 {
		result.Closure = deriveTranscriptReviewClosureFromLatest(nil, sourceFilter, closureNewest, summary)
		return result, nil
	}

	result.Reviews = make([]ShellTranscriptReviewSummary, 0, len(reviews))
	sourceNewestByReviewScope := map[shellsession.TranscriptSource]int64{}
	for _, review := range reviews {
		reviewScopeNewest := summary.NewestSequenceNo
		if review.SourceFilter != "" {
			if cached, ok := sourceNewestByReviewScope[review.SourceFilter]; ok {
				reviewScopeNewest = cached
			} else {
				value, lookupErr := c.latestShellTranscriptSequenceForSource(taskID, sessionID, review.SourceFilter)
				if lookupErr != nil {
					return ReadShellTranscriptReviewHistoryResult{}, lookupErr
				}
				sourceNewestByReviewScope[review.SourceFilter] = value
				if value > 0 {
					reviewScopeNewest = value
				}
			}
		}
		result.Reviews = append(result.Reviews, shellTranscriptReviewSummary(review, reviewScopeNewest))
	}
	latest := result.Reviews[0]
	result.LatestReview = &latest
	if sourceFilter == "" && latest.SourceFilter != "" {
		sourceScopeNewest, err := c.latestShellTranscriptSequenceForSource(taskID, sessionID, latest.SourceFilter)
		if err != nil {
			return ReadShellTranscriptReviewHistoryResult{}, err
		}
		if sourceScopeNewest > 0 {
			closureNewest = sourceScopeNewest
		}
	}
	result.Closure = deriveTranscriptReviewClosureFromLatest(&latest, sourceFilter, closureNewest, summary)
	return result, nil
}

func parseTranscriptSourceFilter(raw string) (shellsession.TranscriptSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	source := shellsession.TranscriptSource(raw)
	switch source {
	case shellsession.TranscriptSourceWorkerOutput, shellsession.TranscriptSourceSystemNote, shellsession.TranscriptSourceFallback:
		return source, nil
	default:
		return "", fmt.Errorf("unsupported transcript source filter %q", raw)
	}
}

func normalizeTranscriptReviewSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxTranscriptReviewSummaryChars {
		return trimmed[:maxTranscriptReviewSummaryChars]
	}
	return trimmed
}

func shellTranscriptReviewSummary(review shellsession.TranscriptReview, newestSequence int64) ShellTranscriptReviewSummary {
	out := ShellTranscriptReviewSummary{
		ReviewID:               review.ReviewID,
		SourceFilter:           review.SourceFilter,
		ReviewedUpToSequence:   review.ReviewedUpToSequence,
		Summary:                strings.TrimSpace(review.Summary),
		CreatedAt:              review.CreatedAt,
		TranscriptState:        review.TranscriptState,
		RetentionLimit:         review.RetentionLimit,
		RetainedChunks:         review.RetainedChunks,
		DroppedChunks:          review.DroppedChunks,
		OldestRetainedSequence: review.OldestRetainedSequence,
		NewestRetainedSequence: newestSequence,
	}
	if out.NewestRetainedSequence == 0 {
		out.NewestRetainedSequence = review.NewestRetainedSequence
	}
	if out.NewestRetainedSequence > review.ReviewedUpToSequence {
		out.StaleBehindLatest = true
		out.NewerRetainedCount = int(out.NewestRetainedSequence - review.ReviewedUpToSequence)
		out.OldestUnreviewedSequence = review.ReviewedUpToSequence + 1
	}
	out.ClosureState = deriveTranscriptReviewClosureState(review.SourceFilter, out.StaleBehindLatest)
	return out
}

func (c *Coordinator) latestShellTranscriptReviewForRead(taskID common.TaskID, sessionID string, sourceFilter shellsession.TranscriptSource, summary shellsession.TranscriptSummary) (*ShellTranscriptReviewSummary, int64, error) {
	var (
		review *shellsession.TranscriptReview
		err    error
	)
	if sourceFilter == "" {
		review, err = c.latestShellTranscriptReviewAnyScope(taskID, sessionID)
		if err != nil {
			return nil, 0, err
		}
	} else {
		review, err = c.latestShellTranscriptReview(taskID, sessionID, sourceFilter)
		if err != nil {
			return nil, 0, err
		}
		if review == nil {
			review, err = c.latestShellTranscriptReview(taskID, sessionID, "")
			if err != nil {
				return nil, 0, err
			}
		}
	}
	if review == nil {
		return nil, 0, nil
	}
	relevantNewest := summary.NewestSequenceNo
	effectiveScope := sourceFilter
	if effectiveScope == "" {
		effectiveScope = review.SourceFilter
	}
	if effectiveScope != "" {
		if sourceNewest, err := c.latestShellTranscriptSequenceForSource(taskID, sessionID, effectiveScope); err != nil {
			return nil, 0, err
		} else if sourceNewest > 0 {
			relevantNewest = sourceNewest
		}
	}
	summaryView := shellTranscriptReviewSummary(*review, relevantNewest)
	return &summaryView, relevantNewest, nil
}

func deriveTranscriptReviewClosureState(scope shellsession.TranscriptSource, stale bool) shellsession.TranscriptReviewClosureState {
	if scope != "" {
		if stale {
			return shellsession.TranscriptReviewClosureSourceScopedStale
		}
		return shellsession.TranscriptReviewClosureSourceScopedCurrent
	}
	if stale {
		return shellsession.TranscriptReviewClosureGlobalStale
	}
	return shellsession.TranscriptReviewClosureGlobalCurrent
}

func deriveTranscriptReviewClosureFromLatest(latest *ShellTranscriptReviewSummary, requestedScope shellsession.TranscriptSource, newestForScope int64, summary shellsession.TranscriptSummary) ShellTranscriptReviewClosure {
	closure := ShellTranscriptReviewClosure{
		State:                  shellsession.TranscriptReviewClosureNone,
		Scope:                  requestedScope,
		RetentionLimit:         summary.RetentionLimit,
		RetainedChunkCount:     summary.RetainedChunks,
		DroppedChunkCount:      summary.DroppedChunks,
		NewestRetainedSequence: newestForScope,
	}
	if closure.NewestRetainedSequence == 0 {
		closure.NewestRetainedSequence = summary.NewestSequenceNo
	}
	if latest == nil {
		return closure
	}
	scope := latest.SourceFilter
	if requestedScope != "" {
		scope = requestedScope
	}
	closure.Scope = scope
	closure.HasReview = true
	closure.ReviewedUpToSequence = latest.ReviewedUpToSequence
	closure.NewestRetainedSequence = newestForScope
	if closure.NewestRetainedSequence == 0 {
		closure.NewestRetainedSequence = latest.NewestRetainedSequence
	}
	if closure.NewestRetainedSequence > latest.ReviewedUpToSequence {
		closure.HasUnreadNewerEvidence = true
		closure.OldestUnreviewedSequence = latest.ReviewedUpToSequence + 1
		closure.UnreviewedRetainedCount = int(closure.NewestRetainedSequence - latest.ReviewedUpToSequence)
	}
	closure.State = deriveTranscriptReviewClosureState(scope, closure.HasUnreadNewerEvidence)
	return closure
}

func (c *Coordinator) latestShellTranscriptSequenceForSource(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource) (int64, error) {
	chunks, _, err := c.listShellTranscriptPage(taskID, sessionID, 0, 1, source)
	if err != nil {
		return 0, err
	}
	if len(chunks) == 0 {
		return 0, nil
	}
	return chunks[len(chunks)-1].SequenceNo, nil
}
