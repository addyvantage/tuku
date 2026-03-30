package orchestrator

import (
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
)

type shellSessionTranscriptStore interface {
	AppendTranscript(taskID common.TaskID, sessionID string, chunks []shellsession.TranscriptChunk, retention int) (shellsession.TranscriptSummary, error)
	ListTranscript(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptChunk, error)
	ListTranscriptPage(taskID common.TaskID, sessionID string, beforeSequence int64, limit int, source shellsession.TranscriptSource) ([]shellsession.TranscriptChunk, bool, error)
	TranscriptSummary(taskID common.TaskID, sessionID string, retention int) (shellsession.TranscriptSummary, error)
	AppendTranscriptReview(review shellsession.TranscriptReview) (shellsession.TranscriptReview, error)
	LatestTranscriptReview(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource) (*shellsession.TranscriptReview, error)
	ListTranscriptReviews(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource, limit int) ([]shellsession.TranscriptReview, error)
	LatestTranscriptReviewAnyScope(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReview, error)
	ListTranscriptReviewsAnyScope(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReview, error)
	AppendTranscriptReviewGapAcknowledgment(record shellsession.TranscriptReviewGapAcknowledgment) (shellsession.TranscriptReviewGapAcknowledgment, error)
	LatestTranscriptReviewGapAcknowledgment(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReviewGapAcknowledgment, error)
	ListTranscriptReviewGapAcknowledgments(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReviewGapAcknowledgment, error)
}

func (c *Coordinator) appendShellTranscript(taskID common.TaskID, sessionID string, chunks []shellsession.TranscriptChunk) (shellsession.TranscriptSummary, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return shellsession.TranscriptSummary{
			TaskID:         taskID,
			SessionID:      strings.TrimSpace(sessionID),
			RetentionLimit: shellsession.DefaultTranscriptRetentionChunks,
		}, nil
	}
	return store.AppendTranscript(taskID, strings.TrimSpace(sessionID), chunks, shellsession.DefaultTranscriptRetentionChunks)
}

func (c *Coordinator) listShellTranscript(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptChunk, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.ListTranscript(taskID, strings.TrimSpace(sessionID), limit)
}

func (c *Coordinator) listShellTranscriptPage(taskID common.TaskID, sessionID string, beforeSequence int64, limit int, source shellsession.TranscriptSource) ([]shellsession.TranscriptChunk, bool, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, false, nil
	}
	return store.ListTranscriptPage(taskID, strings.TrimSpace(sessionID), beforeSequence, limit, source)
}

func (c *Coordinator) shellTranscriptSummary(taskID common.TaskID, sessionID string) (shellsession.TranscriptSummary, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return shellsession.TranscriptSummary{
			TaskID:         taskID,
			SessionID:      strings.TrimSpace(sessionID),
			RetentionLimit: shellsession.DefaultTranscriptRetentionChunks,
		}, nil
	}
	return store.TranscriptSummary(taskID, strings.TrimSpace(sessionID), shellsession.DefaultTranscriptRetentionChunks)
}

func (c *Coordinator) appendShellTranscriptReview(review shellsession.TranscriptReview) (shellsession.TranscriptReview, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return shellsession.TranscriptReview{}, fmt.Errorf("shell transcript review persistence is unavailable")
	}
	return store.AppendTranscriptReview(review)
}

func (c *Coordinator) latestShellTranscriptReview(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource) (*shellsession.TranscriptReview, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.LatestTranscriptReview(taskID, strings.TrimSpace(sessionID), source)
}

func (c *Coordinator) listShellTranscriptReviews(taskID common.TaskID, sessionID string, source shellsession.TranscriptSource, limit int) ([]shellsession.TranscriptReview, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.ListTranscriptReviews(taskID, strings.TrimSpace(sessionID), source, limit)
}

func (c *Coordinator) latestShellTranscriptReviewAnyScope(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReview, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.LatestTranscriptReviewAnyScope(taskID, strings.TrimSpace(sessionID))
}

func (c *Coordinator) listShellTranscriptReviewsAnyScope(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReview, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.ListTranscriptReviewsAnyScope(taskID, strings.TrimSpace(sessionID), limit)
}

func (c *Coordinator) appendShellTranscriptReviewGapAcknowledgment(record shellsession.TranscriptReviewGapAcknowledgment) (shellsession.TranscriptReviewGapAcknowledgment, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("shell transcript review-gap acknowledgment persistence is unavailable")
	}
	return store.AppendTranscriptReviewGapAcknowledgment(record)
}

func (c *Coordinator) latestShellTranscriptReviewGapAcknowledgment(taskID common.TaskID, sessionID string) (*shellsession.TranscriptReviewGapAcknowledgment, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.LatestTranscriptReviewGapAcknowledgment(taskID, strings.TrimSpace(sessionID))
}

func (c *Coordinator) listShellTranscriptReviewGapAcknowledgments(taskID common.TaskID, sessionID string, limit int) ([]shellsession.TranscriptReviewGapAcknowledgment, error) {
	store, ok := c.shellSessions.(shellSessionTranscriptStore)
	if !ok {
		return nil, nil
	}
	return store.ListTranscriptReviewGapAcknowledgments(taskID, strings.TrimSpace(sessionID), limit)
}
