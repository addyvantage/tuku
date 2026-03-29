package orchestrator

import (
	"fmt"
	"strings"

	"tuku/internal/domain/shellsession"
)

type operatorReviewProgressionState string

const (
	operatorReviewProgressionNone                operatorReviewProgressionState = "none"
	operatorReviewProgressionNoEvidence          operatorReviewProgressionState = "no_retained_evidence"
	operatorReviewProgressionEvidenceUnreviewed  operatorReviewProgressionState = "retained_evidence_unreviewed"
	operatorReviewProgressionGlobalCurrent       operatorReviewProgressionState = "global_review_current"
	operatorReviewProgressionGlobalStale         operatorReviewProgressionState = "global_review_stale"
	operatorReviewProgressionSourceScopedCurrent operatorReviewProgressionState = "source_scoped_review_current"
	operatorReviewProgressionSourceScopedStale   operatorReviewProgressionState = "source_scoped_review_stale"
)

type operatorReviewProgressionAssessment struct {
	State                    operatorReviewProgressionState
	SessionID                string
	ReviewScope              shellsession.TranscriptSource
	HasRetainedEvidence      bool
	HasReview                bool
	ReviewStale              bool
	ReviewedUpToSequence     int64
	OldestUnreviewedSequence int64
	NewestRetainedSequence   int64
	UnreviewedRetainedCount  int
	AcknowledgmentAdvisable  bool
	AcknowledgmentClass      shellsession.TranscriptReviewGapAcknowledgmentClass
	TranscriptState          shellsession.TranscriptState
	RetentionLimit           int
	RetainedChunkCount       int
	DroppedChunkCount        int
	Advisory                 string
}

func deriveOperatorReviewProgressionFromSessions(sessions []ShellSessionView) operatorReviewProgressionAssessment {
	if len(sessions) == 0 {
		return operatorReviewProgressionAssessment{State: operatorReviewProgressionNone}
	}
	latest := latestShellSessionForReviewProgression(sessions)
	return deriveOperatorReviewProgressionFromSession(latest)
}

func deriveOperatorReviewProgressionForSessionID(sessions []ShellSessionView, sessionID string) (operatorReviewProgressionAssessment, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return deriveOperatorReviewProgressionFromSessions(sessions), nil
	}
	for _, session := range sessions {
		if strings.TrimSpace(session.SessionID) == sessionID {
			return deriveOperatorReviewProgressionFromSession(session), nil
		}
	}
	return operatorReviewProgressionAssessment{}, fmt.Errorf("shell session %s is not available for transcript review-gap acknowledgment", sessionID)
}

func deriveOperatorReviewProgressionFromSession(latest ShellSessionView) operatorReviewProgressionAssessment {
	out := operatorReviewProgressionAssessment{
		State:                  operatorReviewProgressionNoEvidence,
		SessionID:              strings.TrimSpace(latest.SessionID),
		ReviewScope:            latest.TranscriptReviewSource,
		ReviewedUpToSequence:   latest.TranscriptReviewedUpTo,
		NewestRetainedSequence: latest.TranscriptNewestSequence,
		TranscriptState:        latest.TranscriptState,
		RetentionLimit:         latest.TranscriptRetentionLimit,
		RetainedChunkCount:     latest.TranscriptRetainedChunks,
		DroppedChunkCount:      latest.TranscriptDroppedChunks,
	}
	if latest.TranscriptRetainedChunks <= 0 {
		return out
	}
	out.HasRetainedEvidence = true

	if latest.TranscriptReviewedUpTo <= 0 || strings.TrimSpace(string(latest.TranscriptReviewID)) == "" {
		out.State = operatorReviewProgressionEvidenceUnreviewed
		out.AcknowledgmentAdvisable = true
		out.AcknowledgmentClass = shellsession.TranscriptReviewGapAckMissingReviewMarker
		out.Advisory = fmt.Sprintf(
			"Retained transcript evidence exists for shell session %s and has not been operator-reviewed yet. Review recent retained evidence before progressing.",
			reviewSessionLabel(out.SessionID),
		)
		return out
	}

	out.HasReview = true
	stale := latest.TranscriptReviewStale || latest.TranscriptNewestSequence > latest.TranscriptReviewedUpTo
	out.ReviewStale = stale
	if stale {
		oldest := latest.TranscriptReviewOldestUnreviewed
		if oldest <= 0 {
			oldest = latest.TranscriptReviewedUpTo + 1
		}
		out.OldestUnreviewedSequence = oldest
		if latest.TranscriptNewestSequence >= oldest {
			out.UnreviewedRetainedCount = int(latest.TranscriptNewestSequence-oldest) + 1
		}
		if out.UnreviewedRetainedCount <= 0 {
			out.UnreviewedRetainedCount = latest.TranscriptReviewNewer
		}
		if out.UnreviewedRetainedCount <= 0 {
			out.UnreviewedRetainedCount = 1
		}
	}

	scope := strings.TrimSpace(string(latest.TranscriptReviewSource))
	if scope == "" {
		if stale {
			out.State = operatorReviewProgressionGlobalStale
			out.AcknowledgmentAdvisable = true
			out.AcknowledgmentClass = shellsession.TranscriptReviewGapAckStaleReview
			out.Advisory = fmt.Sprintf(
				"Transcript review is stale for shell session %s; newer retained evidence starts at sequence %d.",
				reviewSessionLabel(out.SessionID),
				out.OldestUnreviewedSequence,
			)
		} else {
			out.State = operatorReviewProgressionGlobalCurrent
		}
		return out
	}

	if stale {
		out.State = operatorReviewProgressionSourceScopedStale
		out.AcknowledgmentAdvisable = true
		out.AcknowledgmentClass = shellsession.TranscriptReviewGapAckSourceScopedStale
		out.Advisory = fmt.Sprintf(
			"Latest transcript review for shell session %s is source-scoped (%s) and stale; newer retained evidence starts at sequence %d.",
			reviewSessionLabel(out.SessionID),
			scope,
			out.OldestUnreviewedSequence,
		)
		return out
	}
	out.State = operatorReviewProgressionSourceScopedCurrent
	out.AcknowledgmentAdvisable = true
	out.AcknowledgmentClass = shellsession.TranscriptReviewGapAckSourceScopedOnly
	return out
}

func latestShellSessionForReviewProgression(sessions []ShellSessionView) ShellSessionView {
	latest := sessions[0]
	for _, session := range sessions[1:] {
		if session.LastUpdatedAt.After(latest.LastUpdatedAt) {
			latest = session
		}
	}
	return latest
}

func reviewSessionLabel(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return "latest"
	}
	return trimmed
}

func applyReviewProgressionToOperatorDecision(summary *OperatorDecisionSummary, progression operatorReviewProgressionAssessment) {
	if summary == nil {
		return
	}
	if progression.Advisory != "" {
		summary.Guidance = appendOperatorSentence(summary.Guidance, progression.Advisory)
	}
	if note := reviewProgressionIntegrityNote(progression); note != "" {
		summary.IntegrityNote = appendOperatorSentence(summary.IntegrityNote, note)
	}
	if note := reviewProgressionScopeIntegrityNote(progression); note != "" {
		summary.IntegrityNote = appendOperatorSentence(summary.IntegrityNote, note)
	}
}

func applyReviewProgressionToOperatorExecutionPlan(plan *OperatorExecutionPlan, progression operatorReviewProgressionAssessment) {
	if plan == nil || plan.PrimaryStep == nil {
		return
	}
	if note := reviewProgressionPlanReason(progression); note != "" {
		plan.PrimaryStep.Reason = appendOperatorSentence(plan.PrimaryStep.Reason, note)
	}
}

func reviewProgressionIntegrityNote(progression operatorReviewProgressionAssessment) string {
	switch progression.State {
	case operatorReviewProgressionEvidenceUnreviewed:
		return "Retained transcript evidence has no review marker yet; progression guidance remains advisory and evidence-bounded."
	case operatorReviewProgressionGlobalStale, operatorReviewProgressionSourceScopedStale:
		return fmt.Sprintf(
			"Transcript review is behind retained evidence (oldest unreviewed sequence %d).",
			progression.OldestUnreviewedSequence,
		)
	}
	return ""
}

func reviewProgressionScopeIntegrityNote(progression operatorReviewProgressionAssessment) string {
	switch progression.State {
	case operatorReviewProgressionSourceScopedCurrent, operatorReviewProgressionSourceScopedStale:
		if progression.ReviewScope != "" {
			return fmt.Sprintf(
				"Latest transcript review is source-scoped to %s and does not certify other sources.",
				progression.ReviewScope,
			)
		}
	}
	return ""
}

func reviewProgressionPlanReason(progression operatorReviewProgressionAssessment) string {
	switch progression.State {
	case operatorReviewProgressionEvidenceUnreviewed:
		return "Retained transcript evidence is unreviewed; proceed with explicit operator awareness."
	case operatorReviewProgressionGlobalStale, operatorReviewProgressionSourceScopedStale:
		if progression.OldestUnreviewedSequence > 0 {
			return fmt.Sprintf(
				"Newer retained transcript evidence exists starting at sequence %d; review awareness is recommended while progressing.",
				progression.OldestUnreviewedSequence,
			)
		}
		return "Newer retained transcript evidence exists beyond the latest review boundary; review awareness is recommended while progressing."
	}
	return ""
}

func appendOperatorSentence(base string, addition string) string {
	base = strings.TrimSpace(base)
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return base
	}
	if base == "" {
		return addition
	}
	if strings.Contains(base, addition) {
		return base
	}
	if strings.HasSuffix(base, ".") {
		return base + " " + addition
	}
	return base + ". " + addition
}
