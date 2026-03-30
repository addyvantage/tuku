package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/transition"
)

const (
	defaultContinuityTransitionReceiptHistoryLimit = 5
	maxContinuityTransitionReceiptHistoryLimit     = 50
	defaultContinuityTransitionReadLimit           = 10
	maxContinuityTransitionReadLimit               = 100
)

type ContinuityTransitionReceiptSummary struct {
	ReceiptID        common.EventID  `json:"receipt_id"`
	TaskID           common.TaskID   `json:"task_id"`
	ShellSessionID   string          `json:"shell_session_id,omitempty"`
	TransitionKind   transition.Kind `json:"transition_kind"`
	TransitionHandle string          `json:"transition_handle,omitempty"`
	TriggerAction    string          `json:"trigger_action,omitempty"`
	TriggerSource    string          `json:"trigger_source,omitempty"`

	HandoffID       string `json:"handoff_id,omitempty"`
	LaunchAttemptID string `json:"launch_attempt_id,omitempty"`
	LaunchID        string `json:"launch_id,omitempty"`
	ResolutionID    string `json:"resolution_id,omitempty"`

	BranchClassBefore   ActiveBranchClass      `json:"branch_class_before,omitempty"`
	BranchRefBefore     string                 `json:"branch_ref_before,omitempty"`
	BranchClassAfter    ActiveBranchClass      `json:"branch_class_after,omitempty"`
	BranchRefAfter      string                 `json:"branch_ref_after,omitempty"`
	HandoffStateBefore  HandoffContinuityState `json:"handoff_continuity_before,omitempty"`
	HandoffStateAfter   HandoffContinuityState `json:"handoff_continuity_after,omitempty"`
	LaunchControlBefore LaunchControlState     `json:"launch_control_before,omitempty"`
	LaunchControlAfter  LaunchControlState     `json:"launch_control_after,omitempty"`

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

	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ContinuityTransitionRiskSummary struct {
	WindowSize                           int    `json:"window_size"`
	ReviewGapTransitions                 int    `json:"review_gap_transitions"`
	AcknowledgedReviewGapTransitions     int    `json:"acknowledged_review_gap_transitions"`
	UnacknowledgedReviewGapTransitions   int    `json:"unacknowledged_review_gap_transitions"`
	StaleReviewPostureTransitions        int    `json:"stale_review_posture_transitions"`
	SourceScopedReviewPostureTransitions int    `json:"source_scoped_review_posture_transitions"`
	IntoClaudeOwnershipTransitions       int    `json:"into_claude_ownership_transitions"`
	BackToLocalOwnershipTransitions      int    `json:"back_to_local_ownership_transitions"`
	OperationallyNotable                 bool   `json:"operationally_notable"`
	Summary                              string `json:"summary,omitempty"`
}

type ReadContinuityTransitionHistoryRequest struct {
	TaskID          string
	Limit           int
	BeforeReceiptID string
	TransitionKind  string
	HandoffID       string
}

type ReadContinuityTransitionHistoryResult struct {
	TaskID                   common.TaskID
	Bounded                  bool
	RequestedLimit           int
	RequestedBeforeReceiptID common.EventID
	RequestedTransitionKind  transition.Kind
	RequestedHandoffID       string
	HasMoreOlder             bool
	NextBeforeReceiptID      common.EventID
	Latest                   *ContinuityTransitionReceiptSummary
	Receipts                 []ContinuityTransitionReceiptSummary
	RiskSummary              ContinuityTransitionRiskSummary
}

type continuityTransitionSnapshot struct {
	Branch                ActiveBranchProvenance
	HandoffContinuity     HandoffContinuity
	LaunchControl         LaunchControl
	Review                operatorReviewProgressionAssessment
	LatestReviewID        common.EventID
	LatestReviewAckID     common.EventID
	LatestReviewAckClass  shellsession.TranscriptReviewGapAcknowledgmentClass
	AcknowledgmentPresent bool
}

type continuityTransitionRecordInput struct {
	TaskID           common.TaskID
	TransitionKind   transition.Kind
	TransitionHandle string
	TriggerAction    string
	TriggerSource    string
	HandoffID        string
	LaunchAttemptID  string
	LaunchID         string
	ResolutionID     string
	Summary          string
	Before           continuityTransitionSnapshot
	After            continuityTransitionSnapshot
}

func (c *Coordinator) captureContinuityTransitionSnapshot(ctx context.Context, taskID common.TaskID) (continuityTransitionSnapshot, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return continuityTransitionSnapshot{}, err
	}
	recovery := c.recoveryFromContinueAssessment(assessment)
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	continuity := assessHandoffContinuity(
		taskID,
		assessment.LatestHandoff,
		assessment.LatestLaunch,
		assessment.LatestAck,
		assessment.LatestFollowThrough,
		assessment.LatestResolution,
	)
	control := assessLaunchControl(taskID, assessment.LatestHandoff, assessment.LatestLaunch)
	sessions, err := c.classifiedShellSessions(taskID)
	if err != nil {
		return continuityTransitionSnapshot{}, err
	}
	review := deriveOperatorReviewProgressionFromSessions(sessions)
	reviewID := transitionReviewMarkerIDForSession(sessions, review.SessionID)
	latestAck, err := c.latestShellTranscriptReviewGapAcknowledgment(taskID, strings.TrimSpace(review.SessionID))
	if err != nil {
		return continuityTransitionSnapshot{}, err
	}
	out := continuityTransitionSnapshot{
		Branch:            branch,
		HandoffContinuity: continuity,
		LaunchControl:     control,
		Review:            review,
		LatestReviewID:    reviewID,
	}
	if latestAck != nil {
		out.LatestReviewAckID = latestAck.AcknowledgmentID
		out.LatestReviewAckClass = latestAck.Class
		out.AcknowledgmentPresent = transitionReviewGapAckCurrent(review, latestAck)
	}
	return out, nil
}

func transitionReviewMarkerIDForSession(sessions []ShellSessionView, sessionID string) common.EventID {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	for _, session := range sessions {
		if strings.TrimSpace(session.SessionID) == sessionID {
			return session.TranscriptReviewID
		}
	}
	return ""
}

func transitionReviewGapAckCurrent(review operatorReviewProgressionAssessment, ack *shellsession.TranscriptReviewGapAcknowledgment) bool {
	if ack == nil || !review.AcknowledgmentAdvisable {
		return false
	}
	if strings.TrimSpace(review.SessionID) == "" || strings.TrimSpace(ack.SessionID) != strings.TrimSpace(review.SessionID) {
		return false
	}
	if ack.Class == "" || ack.Class != review.AcknowledgmentClass {
		return false
	}
	if review.NewestRetainedSequence > 0 && ack.NewestRetainedSequence < review.NewestRetainedSequence {
		return false
	}
	return true
}

func transitionReviewPostureFromProgression(review operatorReviewProgressionAssessment) transition.ReviewPosture {
	switch review.State {
	case operatorReviewProgressionNoEvidence:
		return transition.ReviewPostureNoRetainedEvidence
	case operatorReviewProgressionEvidenceUnreviewed:
		return transition.ReviewPostureRetainedEvidenceUnreviewed
	case operatorReviewProgressionGlobalCurrent:
		return transition.ReviewPostureGlobalReviewCurrent
	case operatorReviewProgressionGlobalStale:
		return transition.ReviewPostureGlobalReviewStale
	case operatorReviewProgressionSourceScopedCurrent:
		return transition.ReviewPostureSourceScopedReviewCurrent
	case operatorReviewProgressionSourceScopedStale:
		return transition.ReviewPostureSourceScopedReviewStale
	default:
		return transition.ReviewPostureNone
	}
}

func materialContinuityTransition(before continuityTransitionSnapshot, after continuityTransitionSnapshot) bool {
	if before.Branch.Class != after.Branch.Class || before.Branch.BranchRef != after.Branch.BranchRef {
		return true
	}
	if before.HandoffContinuity.State != after.HandoffContinuity.State || before.HandoffContinuity.HandoffID != after.HandoffContinuity.HandoffID {
		return true
	}
	if before.HandoffContinuity.LaunchAttemptID != after.HandoffContinuity.LaunchAttemptID || before.HandoffContinuity.LaunchID != after.HandoffContinuity.LaunchID {
		return true
	}
	if before.HandoffContinuity.ResolutionID != after.HandoffContinuity.ResolutionID {
		return true
	}
	if before.LaunchControl.State != after.LaunchControl.State || before.LaunchControl.AttemptID != after.LaunchControl.AttemptID || before.LaunchControl.LaunchID != after.LaunchControl.LaunchID {
		return true
	}
	return false
}

func (c *Coordinator) recordContinuityTransitionReceipt(caps capsule.WorkCapsule, input continuityTransitionRecordInput) (transition.Receipt, bool, error) {
	if !materialContinuityTransition(input.Before, input.After) {
		return transition.Receipt{}, false, nil
	}
	now := c.clock()
	record := transition.Receipt{
		Version:                  1,
		ReceiptID:                common.EventID(c.idGenerator("ctr")),
		TaskID:                   input.TaskID,
		ShellSessionID:           strings.TrimSpace(input.Before.Review.SessionID),
		TransitionKind:           input.TransitionKind,
		TransitionHandle:         strings.TrimSpace(input.TransitionHandle),
		TriggerAction:            strings.TrimSpace(input.TriggerAction),
		TriggerSource:            strings.TrimSpace(input.TriggerSource),
		HandoffID:                strings.TrimSpace(input.HandoffID),
		LaunchAttemptID:          strings.TrimSpace(input.LaunchAttemptID),
		LaunchID:                 strings.TrimSpace(input.LaunchID),
		ResolutionID:             strings.TrimSpace(input.ResolutionID),
		BranchClassBefore:        string(input.Before.Branch.Class),
		BranchRefBefore:          input.Before.Branch.BranchRef,
		BranchClassAfter:         string(input.After.Branch.Class),
		BranchRefAfter:           input.After.Branch.BranchRef,
		HandoffContinuityBefore:  string(input.Before.HandoffContinuity.State),
		HandoffContinuityAfter:   string(input.After.HandoffContinuity.State),
		LaunchControlBefore:      string(input.Before.LaunchControl.State),
		LaunchControlAfter:       string(input.After.LaunchControl.State),
		ReviewGapPresent:         input.Before.Review.AcknowledgmentAdvisable,
		ReviewPosture:            transitionReviewPostureFromProgression(input.Before.Review),
		ReviewState:              string(input.Before.Review.State),
		ReviewScope:              input.Before.Review.ReviewScope,
		ReviewedUpToSequence:     input.Before.Review.ReviewedUpToSequence,
		OldestUnreviewedSequence: input.Before.Review.OldestUnreviewedSequence,
		NewestRetainedSequence:   input.Before.Review.NewestRetainedSequence,
		UnreviewedRetainedCount:  input.Before.Review.UnreviewedRetainedCount,
		LatestReviewID:           input.Before.LatestReviewID,
		LatestReviewAckID:        input.Before.LatestReviewAckID,
		AcknowledgmentPresent:    input.Before.AcknowledgmentPresent,
		AcknowledgmentClass:      input.Before.LatestReviewAckClass,
		Summary:                  strings.TrimSpace(input.Summary),
		CreatedAt:                now,
	}
	if record.Summary == "" {
		record.Summary = fmt.Sprintf(
			"%s transition recorded (%s -> %s)",
			record.TransitionKind,
			record.HandoffContinuityBefore,
			record.HandoffContinuityAfter,
		)
	}
	if err := c.store.TransitionReceipts().Create(record); err != nil {
		return transition.Receipt{}, false, err
	}
	payload := map[string]any{
		"transition_receipt_id":      record.ReceiptID,
		"transition_kind":            record.TransitionKind,
		"transition_handle":          record.TransitionHandle,
		"trigger_action":             record.TriggerAction,
		"trigger_source":             record.TriggerSource,
		"handoff_id":                 record.HandoffID,
		"launch_attempt_id":          record.LaunchAttemptID,
		"launch_id":                  record.LaunchID,
		"resolution_id":              record.ResolutionID,
		"branch_class_before":        record.BranchClassBefore,
		"branch_ref_before":          record.BranchRefBefore,
		"branch_class_after":         record.BranchClassAfter,
		"branch_ref_after":           record.BranchRefAfter,
		"handoff_continuity_before":  record.HandoffContinuityBefore,
		"handoff_continuity_after":   record.HandoffContinuityAfter,
		"launch_control_before":      record.LaunchControlBefore,
		"launch_control_after":       record.LaunchControlAfter,
		"review_gap_present":         record.ReviewGapPresent,
		"review_posture":             record.ReviewPosture,
		"review_state":               record.ReviewState,
		"review_scope":               record.ReviewScope,
		"reviewed_up_to_sequence":    record.ReviewedUpToSequence,
		"oldest_unreviewed_sequence": record.OldestUnreviewedSequence,
		"newest_retained_sequence":   record.NewestRetainedSequence,
		"unreviewed_retained_count":  record.UnreviewedRetainedCount,
		"latest_review_id":           record.LatestReviewID,
		"latest_review_gap_ack_id":   record.LatestReviewAckID,
		"acknowledgment_present":     record.AcknowledgmentPresent,
		"acknowledgment_class":       record.AcknowledgmentClass,
		"summary":                    record.Summary,
	}
	if err := c.appendProof(caps, proof.EventBranchHandoffTransitionRecorded, proof.ActorUser, "user", payload, nil); err != nil {
		return transition.Receipt{}, false, err
	}
	return record, true, nil
}

func continuityTransitionReceiptSummary(record transition.Receipt) ContinuityTransitionReceiptSummary {
	return ContinuityTransitionReceiptSummary{
		ReceiptID:                record.ReceiptID,
		TaskID:                   record.TaskID,
		ShellSessionID:           record.ShellSessionID,
		TransitionKind:           record.TransitionKind,
		TransitionHandle:         record.TransitionHandle,
		TriggerAction:            record.TriggerAction,
		TriggerSource:            record.TriggerSource,
		HandoffID:                record.HandoffID,
		LaunchAttemptID:          record.LaunchAttemptID,
		LaunchID:                 record.LaunchID,
		ResolutionID:             record.ResolutionID,
		BranchClassBefore:        ActiveBranchClass(record.BranchClassBefore),
		BranchRefBefore:          record.BranchRefBefore,
		BranchClassAfter:         ActiveBranchClass(record.BranchClassAfter),
		BranchRefAfter:           record.BranchRefAfter,
		HandoffStateBefore:       HandoffContinuityState(record.HandoffContinuityBefore),
		HandoffStateAfter:        HandoffContinuityState(record.HandoffContinuityAfter),
		LaunchControlBefore:      LaunchControlState(record.LaunchControlBefore),
		LaunchControlAfter:       LaunchControlState(record.LaunchControlAfter),
		ReviewGapPresent:         record.ReviewGapPresent,
		ReviewPosture:            record.ReviewPosture,
		ReviewState:              record.ReviewState,
		ReviewScope:              record.ReviewScope,
		ReviewedUpToSequence:     record.ReviewedUpToSequence,
		OldestUnreviewedSequence: record.OldestUnreviewedSequence,
		NewestRetainedSequence:   record.NewestRetainedSequence,
		UnreviewedRetainedCount:  record.UnreviewedRetainedCount,
		LatestReviewID:           record.LatestReviewID,
		LatestReviewGapAckID:     record.LatestReviewAckID,
		AcknowledgmentPresent:    record.AcknowledgmentPresent,
		AcknowledgmentClass:      record.AcknowledgmentClass,
		Summary:                  record.Summary,
		CreatedAt:                record.CreatedAt,
	}
}

func (c *Coordinator) continuityTransitionReceiptProjection(taskID common.TaskID, limit int) (*ContinuityTransitionReceiptSummary, []ContinuityTransitionReceiptSummary, error) {
	if limit <= 0 {
		limit = defaultContinuityTransitionReceiptHistoryLimit
	}
	if limit > maxContinuityTransitionReceiptHistoryLimit {
		limit = maxContinuityTransitionReceiptHistoryLimit
	}
	records, err := c.store.TransitionReceipts().ListByTask(taskID, limit)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, nil
	}
	out := make([]ContinuityTransitionReceiptSummary, 0, len(records))
	for _, record := range records {
		out = append(out, continuityTransitionReceiptSummary(record))
	}
	latest := out[0]
	return &latest, out, nil
}

func (c *Coordinator) ReadContinuityTransitionHistory(ctx context.Context, req ReadContinuityTransitionHistoryRequest) (ReadContinuityTransitionHistoryResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadContinuityTransitionHistoryResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReadContinuityTransitionHistoryResult{}, err
	}
	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultContinuityTransitionReadLimit
	case limit > maxContinuityTransitionReadLimit:
		limit = maxContinuityTransitionReadLimit
	}
	beforeReceiptID := common.EventID(strings.TrimSpace(req.BeforeReceiptID))
	var beforeCreatedAt time.Time
	if beforeReceiptID != "" {
		anchor, err := c.store.TransitionReceipts().GetByTaskReceipt(taskID, beforeReceiptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReadContinuityTransitionHistoryResult{}, fmt.Errorf("before receipt %s was not found for task %s", beforeReceiptID, taskID)
			}
			return ReadContinuityTransitionHistoryResult{}, err
		}
		beforeCreatedAt = anchor.CreatedAt
	}
	kindFilter, err := parseContinuityTransitionKindFilter(req.TransitionKind)
	if err != nil {
		return ReadContinuityTransitionHistoryResult{}, err
	}
	handoffID := strings.TrimSpace(req.HandoffID)
	records, err := c.store.TransitionReceipts().ListByTaskFiltered(taskID, transition.ReceiptListFilter{
		Limit:           limit + 1,
		BeforeReceiptID: beforeReceiptID,
		BeforeCreatedAt: beforeCreatedAt,
		TransitionKind:  kindFilter,
		HandoffID:       handoffID,
	})
	if err != nil {
		return ReadContinuityTransitionHistoryResult{}, err
	}
	hasMoreOlder := false
	if len(records) > limit {
		hasMoreOlder = true
		records = records[:limit]
	}
	receipts := make([]ContinuityTransitionReceiptSummary, 0, len(records))
	for _, record := range records {
		receipts = append(receipts, continuityTransitionReceiptSummary(record))
	}
	var latest *ContinuityTransitionReceiptSummary
	if len(receipts) > 0 {
		copyLatest := receipts[0]
		latest = &copyLatest
	}
	result := ReadContinuityTransitionHistoryResult{
		TaskID:                   taskID,
		Bounded:                  true,
		RequestedLimit:           limit,
		RequestedBeforeReceiptID: beforeReceiptID,
		RequestedTransitionKind:  kindFilter,
		RequestedHandoffID:       handoffID,
		HasMoreOlder:             hasMoreOlder,
		Latest:                   latest,
		Receipts:                 receipts,
		RiskSummary:              deriveContinuityTransitionRiskSummary(receipts),
	}
	if hasMoreOlder && len(receipts) > 0 {
		result.NextBeforeReceiptID = receipts[len(receipts)-1].ReceiptID
	}
	return result, nil
}

func parseContinuityTransitionKindFilter(raw string) (transition.Kind, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "handoff-launch":
		return transition.KindHandoffLaunch, nil
	case "handoff-resolution":
		return transition.KindHandoffResolution, nil
	default:
		return "", fmt.Errorf("unsupported transition kind filter %q", raw)
	}
}

func deriveContinuityTransitionRiskSummary(receipts []ContinuityTransitionReceiptSummary) ContinuityTransitionRiskSummary {
	summary := ContinuityTransitionRiskSummary{
		WindowSize: len(receipts),
	}
	if len(receipts) == 0 {
		summary.Summary = "No continuity transition receipts were found in this bounded window."
		return summary
	}

	for _, receipt := range receipts {
		if receipt.ReviewGapPresent {
			summary.ReviewGapTransitions++
			if receipt.AcknowledgmentPresent {
				summary.AcknowledgedReviewGapTransitions++
			} else {
				summary.UnacknowledgedReviewGapTransitions++
			}
		}
		if continuityTransitionPostureIsStale(receipt.ReviewPosture) {
			summary.StaleReviewPostureTransitions++
		}
		if continuityTransitionPostureIsSourceScoped(receipt.ReviewPosture) {
			summary.SourceScopedReviewPostureTransitions++
		}
		if receipt.BranchClassAfter == ActiveBranchClassHandoffClaude && receipt.BranchClassBefore != ActiveBranchClassHandoffClaude {
			summary.IntoClaudeOwnershipTransitions++
		}
		if receipt.BranchClassBefore == ActiveBranchClassHandoffClaude && receipt.BranchClassAfter == ActiveBranchClassLocal {
			summary.BackToLocalOwnershipTransitions++
		}
	}
	summary.OperationallyNotable =
		summary.ReviewGapTransitions > 0 ||
			summary.StaleReviewPostureTransitions > 0 ||
			summary.SourceScopedReviewPostureTransitions > 0 ||
			summary.IntoClaudeOwnershipTransitions > 0 ||
			summary.BackToLocalOwnershipTransitions > 0

	notes := make([]string, 0, 5)
	switch {
	case summary.UnacknowledgedReviewGapTransitions > 0:
		notes = append(notes, fmt.Sprintf("%d transition(s) recorded with unacknowledged transcript review gaps", summary.UnacknowledgedReviewGapTransitions))
	case summary.AcknowledgedReviewGapTransitions > 0:
		notes = append(notes, fmt.Sprintf("%d transition(s) recorded under explicit review-gap acknowledgment", summary.AcknowledgedReviewGapTransitions))
	}
	if summary.StaleReviewPostureTransitions > 0 {
		notes = append(notes, fmt.Sprintf("%d transition(s) carried stale or unreviewed retained transcript posture", summary.StaleReviewPostureTransitions))
	}
	if summary.SourceScopedReviewPostureTransitions > 0 {
		notes = append(notes, fmt.Sprintf("%d transition(s) used source-scoped transcript review posture", summary.SourceScopedReviewPostureTransitions))
	}
	if summary.IntoClaudeOwnershipTransitions > 0 {
		notes = append(notes, fmt.Sprintf("%d transition(s) moved continuity ownership into Claude handoff branch", summary.IntoClaudeOwnershipTransitions))
	}
	if summary.BackToLocalOwnershipTransitions > 0 {
		notes = append(notes, fmt.Sprintf("%d transition(s) moved continuity ownership back to local lineage", summary.BackToLocalOwnershipTransitions))
	}
	if len(notes) == 0 {
		summary.Summary = "Recent continuity transitions in this bounded window did not carry explicit transcript review-gap risk signals."
		return summary
	}
	summary.Summary = strings.Join(notes, "; ")
	return summary
}

func continuityTransitionPostureIsStale(posture transition.ReviewPosture) bool {
	switch posture {
	case transition.ReviewPostureRetainedEvidenceUnreviewed,
		transition.ReviewPostureGlobalReviewStale,
		transition.ReviewPostureSourceScopedReviewStale:
		return true
	default:
		return false
	}
}

func continuityTransitionPostureIsSourceScoped(posture transition.ReviewPosture) bool {
	switch posture {
	case transition.ReviewPostureSourceScopedReviewCurrent, transition.ReviewPostureSourceScopedReviewStale:
		return true
	default:
		return false
	}
}
