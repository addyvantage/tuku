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
	defaultTranscriptReviewGapAckHistoryLimit = 5
	maxTranscriptReviewGapAckHistoryLimit     = 50
	maxTranscriptReviewGapAckSummaryChars     = 400
)

type TranscriptReviewGapAcknowledgmentSummary struct {
	AcknowledgmentID common.EventID
	TaskID           common.TaskID
	SessionID        string
	Class            shellsession.TranscriptReviewGapAcknowledgmentClass
	ReviewState      string
	ReviewScope      shellsession.TranscriptSource

	ReviewedUpToSequence     int64
	OldestUnreviewedSequence int64
	NewestRetainedSequence   int64
	UnreviewedRetainedCount  int

	TranscriptState shellsession.TranscriptState
	RetentionLimit  int
	RetainedChunks  int
	DroppedChunks   int

	ActionContext string
	Summary       string
	CreatedAt     time.Time

	StaleBehindCurrent bool
	NewerRetainedCount int
}

type RecordOperatorReviewGapAcknowledgmentRequest struct {
	TaskID        string
	SessionID     string
	Kind          string
	Summary       string
	ActionContext string
}

type RecordOperatorReviewGapAcknowledgmentResult struct {
	TaskID                 common.TaskID
	SessionID              string
	Acknowledgment         TranscriptReviewGapAcknowledgmentSummary
	ReviewGapState         string
	ReviewGapClass         shellsession.TranscriptReviewGapAcknowledgmentClass
	ReviewScope            shellsession.TranscriptSource
	ReviewedUpToSequence   int64
	OldestUnreviewedSeq    int64
	NewestRetainedSequence int64
	UnreviewedRetained     int
	Advisory               string
	RecentAcknowledgments  []TranscriptReviewGapAcknowledgmentSummary
}

func (c *Coordinator) RecordOperatorReviewGapAcknowledgment(ctx context.Context, req RecordOperatorReviewGapAcknowledgmentRequest) (RecordOperatorReviewGapAcknowledgmentResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordOperatorReviewGapAcknowledgmentResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return RecordOperatorReviewGapAcknowledgmentResult{}, err
	}
	sessions, err := c.classifiedShellSessions(taskID)
	if err != nil {
		return RecordOperatorReviewGapAcknowledgmentResult{}, err
	}
	progression, err := deriveOperatorReviewProgressionForSessionID(sessions, strings.TrimSpace(req.SessionID))
	if err != nil {
		return RecordOperatorReviewGapAcknowledgmentResult{}, err
	}
	stored, err := c.recordOperatorReviewGapAcknowledgmentFromProgression(taskID, progression, req.Kind, req.Summary, req.ActionContext)
	if err != nil {
		return RecordOperatorReviewGapAcknowledgmentResult{}, err
	}
	currentNewest := progression.NewestRetainedSequence
	if currentNewest <= 0 {
		currentNewest = stored.NewestRetainedSequence
	}
	latest := transcriptReviewGapAcknowledgmentSummary(stored, currentNewest)
	recentLimit := defaultTranscriptReviewGapAckHistoryLimit
	recentRecords, err := c.listShellTranscriptReviewGapAcknowledgments(taskID, progression.SessionID, recentLimit)
	if err != nil {
		return RecordOperatorReviewGapAcknowledgmentResult{}, err
	}
	recent := make([]TranscriptReviewGapAcknowledgmentSummary, 0, len(recentRecords))
	for _, item := range recentRecords {
		recent = append(recent, transcriptReviewGapAcknowledgmentSummary(item, currentNewest))
	}
	return RecordOperatorReviewGapAcknowledgmentResult{
		TaskID:                 taskID,
		SessionID:              progression.SessionID,
		Acknowledgment:         latest,
		ReviewGapState:         string(progression.State),
		ReviewGapClass:         progression.AcknowledgmentClass,
		ReviewScope:            progression.ReviewScope,
		ReviewedUpToSequence:   progression.ReviewedUpToSequence,
		OldestUnreviewedSeq:    progression.OldestUnreviewedSequence,
		NewestRetainedSequence: progression.NewestRetainedSequence,
		UnreviewedRetained:     progression.UnreviewedRetainedCount,
		Advisory:               progression.Advisory,
		RecentAcknowledgments:  recent,
	}, nil
}

func (c *Coordinator) recordOperatorReviewGapAcknowledgmentFromProgression(taskID common.TaskID, progression operatorReviewProgressionAssessment, requestedClass string, summary string, actionContext string) (shellsession.TranscriptReviewGapAcknowledgment, error) {
	if !progression.AcknowledgmentAdvisable || progression.AcknowledgmentClass == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("no transcript review gap acknowledgment is required for the current retained evidence posture")
	}
	if strings.TrimSpace(progression.SessionID) == "" {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf("no shell session is available for transcript review-gap acknowledgment")
	}
	if parsed, ok := parseTranscriptReviewGapAckClass(requestedClass); strings.TrimSpace(requestedClass) != "" && (!ok || parsed != progression.AcknowledgmentClass) {
		return shellsession.TranscriptReviewGapAcknowledgment{}, fmt.Errorf(
			"requested acknowledgment kind %q does not match current review-gap class %s",
			strings.TrimSpace(requestedClass),
			progression.AcknowledgmentClass,
		)
	}
	record := shellsession.TranscriptReviewGapAcknowledgment{
		AcknowledgmentID:         common.EventID(c.idGenerator("sack")),
		TaskID:                   taskID,
		SessionID:                strings.TrimSpace(progression.SessionID),
		Class:                    progression.AcknowledgmentClass,
		ReviewState:              string(progression.State),
		ReviewScope:              progression.ReviewScope,
		ReviewedUpToSequence:     progression.ReviewedUpToSequence,
		OldestUnreviewedSequence: progression.OldestUnreviewedSequence,
		NewestRetainedSequence:   progression.NewestRetainedSequence,
		UnreviewedRetainedCount:  progression.UnreviewedRetainedCount,
		TranscriptState:          progression.TranscriptState,
		RetentionLimit:           progression.RetentionLimit,
		RetainedChunks:           progression.RetainedChunkCount,
		DroppedChunks:            progression.DroppedChunkCount,
		ActionContext:            normalizeTranscriptReviewGapAckActionContext(actionContext),
		Summary:                  normalizeTranscriptReviewGapAckSummary(summary),
		CreatedAt:                c.clock(),
	}
	stored, err := c.appendShellTranscriptReviewGapAcknowledgment(record)
	if err != nil {
		return shellsession.TranscriptReviewGapAcknowledgment{}, err
	}
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"ack_id":                     stored.AcknowledgmentID,
			"session_id":                 stored.SessionID,
			"ack_class":                  stored.Class,
			"review_state":               stored.ReviewState,
			"review_scope":               stored.ReviewScope,
			"reviewed_up_to_sequence":    stored.ReviewedUpToSequence,
			"oldest_unreviewed_sequence": stored.OldestUnreviewedSequence,
			"newest_retained_sequence":   stored.NewestRetainedSequence,
			"unreviewed_retained_count":  stored.UnreviewedRetainedCount,
			"action_context":             stored.ActionContext,
			"summary":                    stored.Summary,
		}
		return txc.appendProof(caps, proof.EventTranscriptReviewGapAcknowledged, proof.ActorUser, "user", payload, nil)
	})
	if err != nil {
		return shellsession.TranscriptReviewGapAcknowledgment{}, err
	}
	return stored, nil
}

func normalizeTranscriptReviewGapAckSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > maxTranscriptReviewGapAckSummaryChars {
		return trimmed[:maxTranscriptReviewGapAckSummaryChars]
	}
	return trimmed
}

func normalizeTranscriptReviewGapAckActionContext(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "operator_acknowledge_review_gap"
	}
	return trimmed
}

func parseTranscriptReviewGapAckClass(raw string) (shellsession.TranscriptReviewGapAcknowledgmentClass, bool) {
	switch strings.TrimSpace(raw) {
	case "":
		return "", true
	case string(shellsession.TranscriptReviewGapAckMissingReviewMarker):
		return shellsession.TranscriptReviewGapAckMissingReviewMarker, true
	case string(shellsession.TranscriptReviewGapAckStaleReview):
		return shellsession.TranscriptReviewGapAckStaleReview, true
	case string(shellsession.TranscriptReviewGapAckSourceScopedOnly):
		return shellsession.TranscriptReviewGapAckSourceScopedOnly, true
	case string(shellsession.TranscriptReviewGapAckSourceScopedStale):
		return shellsession.TranscriptReviewGapAckSourceScopedStale, true
	default:
		return "", false
	}
}

func transcriptReviewGapAcknowledgmentSummary(record shellsession.TranscriptReviewGapAcknowledgment, currentNewest int64) TranscriptReviewGapAcknowledgmentSummary {
	out := TranscriptReviewGapAcknowledgmentSummary{
		AcknowledgmentID:         record.AcknowledgmentID,
		TaskID:                   record.TaskID,
		SessionID:                record.SessionID,
		Class:                    record.Class,
		ReviewState:              record.ReviewState,
		ReviewScope:              record.ReviewScope,
		ReviewedUpToSequence:     record.ReviewedUpToSequence,
		OldestUnreviewedSequence: record.OldestUnreviewedSequence,
		NewestRetainedSequence:   record.NewestRetainedSequence,
		UnreviewedRetainedCount:  record.UnreviewedRetainedCount,
		TranscriptState:          record.TranscriptState,
		RetentionLimit:           record.RetentionLimit,
		RetainedChunks:           record.RetainedChunks,
		DroppedChunks:            record.DroppedChunks,
		ActionContext:            record.ActionContext,
		Summary:                  strings.TrimSpace(record.Summary),
		CreatedAt:                record.CreatedAt,
	}
	if currentNewest <= 0 {
		currentNewest = record.NewestRetainedSequence
	}
	if currentNewest > record.NewestRetainedSequence {
		out.StaleBehindCurrent = true
		out.NewerRetainedCount = int(currentNewest - record.NewestRetainedSequence)
	}
	return out
}

func (c *Coordinator) reviewGapAcknowledgmentProjection(taskID common.TaskID, sessions []ShellSessionView, limit int) (*TranscriptReviewGapAcknowledgmentSummary, []TranscriptReviewGapAcknowledgmentSummary, error) {
	if limit <= 0 {
		limit = defaultTranscriptReviewGapAckHistoryLimit
	}
	if limit > maxTranscriptReviewGapAckHistoryLimit {
		limit = maxTranscriptReviewGapAckHistoryLimit
	}
	currentNewestBySession := make(map[string]int64, len(sessions))
	for _, session := range sessions {
		currentNewestBySession[strings.TrimSpace(session.SessionID)] = session.TranscriptNewestSequence
	}
	records, err := c.listShellTranscriptReviewGapAcknowledgments(taskID, "", limit)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, nil
	}
	out := make([]TranscriptReviewGapAcknowledgmentSummary, 0, len(records))
	for _, item := range records {
		currentNewest := currentNewestBySession[strings.TrimSpace(item.SessionID)]
		out = append(out, transcriptReviewGapAcknowledgmentSummary(item, currentNewest))
	}
	latest := out[0]
	return &latest, out, nil
}
