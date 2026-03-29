package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/proof"
)

const (
	defaultContinuityIncidentFollowUpReadLimit = 10
	maxContinuityIncidentFollowUpReadLimit     = 100
	maxContinuityIncidentFollowUpSummaryChars  = 400
)

type RecordContinuityIncidentFollowUpRequest struct {
	TaskID                    string
	AnchorMode                string
	AnchorTransitionReceiptID string
	TriageReceiptID           string
	ActionKind                string
	Summary                   string
}

type RecordContinuityIncidentFollowUpResult struct {
	TaskID                                  common.TaskID
	AnchorMode                              incidenttriage.AnchorMode
	AnchorTransitionReceiptID               common.EventID
	TriageReceiptID                         common.EventID
	ActionKind                              incidenttriage.FollowUpActionKind
	Reused                                  bool
	Receipt                                 ContinuityIncidentFollowUpReceiptSummary
	LatestContinuityTransition              *ContinuityTransitionReceiptSummary
	LatestContinuityIncidentTriage          *ContinuityIncidentTriageReceiptSummary
	RecentContinuityIncidentTriages         []ContinuityIncidentTriageReceiptSummary
	LatestContinuityIncidentFollowUp        *ContinuityIncidentFollowUpReceiptSummary
	RecentContinuityIncidentFollowUps       []ContinuityIncidentFollowUpReceiptSummary
	ContinuityIncidentFollowUpHistoryRollup *ContinuityIncidentFollowUpHistoryRollupSummary
	FollowUp                                *ContinuityIncidentFollowUpSummary
}

type ContinuityIncidentFollowUpHistoryRollupSummary struct {
	WindowSize                        int    `json:"window_size"`
	BoundedWindow                     bool   `json:"bounded_window"`
	DistinctAnchors                   int    `json:"distinct_anchors"`
	ReceiptsRecordedPending           int    `json:"receipts_recorded_pending"`
	ReceiptsProgressed                int    `json:"receipts_progressed"`
	ReceiptsClosed                    int    `json:"receipts_closed"`
	ReceiptsReopened                  int    `json:"receipts_reopened"`
	AnchorsWithOpenFollowUp           int    `json:"anchors_with_open_follow_up"`
	AnchorsClosed                     int    `json:"anchors_closed"`
	AnchorsReopened                   int    `json:"anchors_reopened"`
	OpenAnchorsBehindLatestTransition int    `json:"open_anchors_behind_latest_transition"`
	AnchorsRepeatedWithoutProgression int    `json:"anchors_repeated_without_progression"`
	AnchorsTriagedWithoutFollowUp     int    `json:"anchors_triaged_without_follow_up"`
	OperationallyNotable              bool   `json:"operationally_notable"`
	Summary                           string `json:"summary,omitempty"`
}

type ReadContinuityIncidentFollowUpHistoryRequest struct {
	TaskID                    string
	Limit                     int
	BeforeReceiptID           string
	AnchorTransitionReceiptID string
	TriageReceiptID           string
	ActionKind                string
}

type ReadContinuityIncidentFollowUpHistoryResult struct {
	TaskID                             common.TaskID
	Bounded                            bool
	RequestedLimit                     int
	RequestedBeforeReceiptID           common.EventID
	RequestedAnchorTransitionReceiptID common.EventID
	RequestedTriageReceiptID           common.EventID
	RequestedActionKind                incidenttriage.FollowUpActionKind
	HasMoreOlder                       bool
	NextBeforeReceiptID                common.EventID
	LatestTransitionReceiptID          common.EventID
	Latest                             *ContinuityIncidentFollowUpReceiptSummary
	Receipts                           []ContinuityIncidentFollowUpReceiptSummary
	Rollup                             ContinuityIncidentFollowUpHistoryRollupSummary
}

func (c *Coordinator) RecordContinuityIncidentFollowUp(ctx context.Context, req RecordContinuityIncidentFollowUpRequest) (RecordContinuityIncidentFollowUpResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordContinuityIncidentFollowUpResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return RecordContinuityIncidentFollowUpResult{}, err
	}
	actionKind, err := parseContinuityIncidentFollowUpActionKind(req.ActionKind)
	if err != nil {
		return RecordContinuityIncidentFollowUpResult{}, err
	}
	summary := normalizeContinuityIncidentFollowUpSummary(req.Summary)

	anchor, anchorMode, err := c.resolveContinuityIncidentTriageAnchor(taskID, req.AnchorMode, req.AnchorTransitionReceiptID)
	if err != nil {
		return RecordContinuityIncidentFollowUpResult{}, err
	}
	triageReceiptID := common.EventID(strings.TrimSpace(req.TriageReceiptID))
	var triageRecord incidenttriage.Receipt
	if triageReceiptID != "" {
		triageRecord, err = c.store.IncidentTriages().GetByTaskReceipt(taskID, triageReceiptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RecordContinuityIncidentFollowUpResult{}, fmt.Errorf("triage receipt %s was not found for task %s", triageReceiptID, taskID)
			}
			return RecordContinuityIncidentFollowUpResult{}, err
		}
		if triageRecord.AnchorTransitionReceiptID != anchor.ReceiptID {
			return RecordContinuityIncidentFollowUpResult{}, fmt.Errorf("triage receipt %s is anchored to %s, expected %s", triageRecord.ReceiptID, triageRecord.AnchorTransitionReceiptID, anchor.ReceiptID)
		}
	} else {
		triageRecord, err = c.store.IncidentTriages().LatestByTaskAnchor(taskID, anchor.ReceiptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return RecordContinuityIncidentFollowUpResult{}, fmt.Errorf("no continuity incident triage receipt exists for anchor %s on task %s", anchor.ReceiptID, taskID)
			}
			return RecordContinuityIncidentFollowUpResult{}, err
		}
	}
	if latestForAnchor, err := c.store.IncidentFollowUps().LatestByTaskAnchor(taskID, anchor.ReceiptID); err == nil {
		if continuityIncidentFollowUpEquivalent(latestForAnchor, anchorMode, triageRecord.ReceiptID, actionKind, summary) {
			latestTransition, recentTriages, followUpSummary, latestFollowUpForAnchor, projectionErr := c.continuityIncidentTriageReadProjection(taskID)
			if projectionErr != nil {
				return RecordContinuityIncidentFollowUpResult{}, projectionErr
			}
			latestFollowUp, recentFollowUps, rollup, historyErr := c.continuityIncidentFollowUpHistoryProjection(taskID, defaultContinuityIncidentFollowUpReadLimit)
			if historyErr != nil {
				return RecordContinuityIncidentFollowUpResult{}, historyErr
			}
			var latestTriage *ContinuityIncidentTriageReceiptSummary
			if len(recentTriages) > 0 {
				copyLatest := recentTriages[0]
				latestTriage = &copyLatest
			}
			if latestFollowUpForAnchor != nil {
				latestFollowUp = latestFollowUpForAnchor
			}
			followUpSummary = deriveFollowUpAwareAdvisory(followUpSummary, rollup, recentFollowUps)
			return RecordContinuityIncidentFollowUpResult{
				TaskID:                                  taskID,
				AnchorMode:                              anchorMode,
				AnchorTransitionReceiptID:               anchor.ReceiptID,
				TriageReceiptID:                         triageRecord.ReceiptID,
				ActionKind:                              actionKind,
				Reused:                                  true,
				Receipt:                                 continuityIncidentFollowUpReceiptSummary(latestForAnchor),
				LatestContinuityTransition:              latestTransition,
				LatestContinuityIncidentTriage:          latestTriage,
				RecentContinuityIncidentTriages:         recentTriages,
				LatestContinuityIncidentFollowUp:        latestFollowUp,
				RecentContinuityIncidentFollowUps:       recentFollowUps,
				ContinuityIncidentFollowUpHistoryRollup: rollup,
				FollowUp:                                followUpSummary,
			}, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RecordContinuityIncidentFollowUpResult{}, err
	}

	record := incidenttriage.FollowUpReceipt{
		Version:                   1,
		ReceiptID:                 common.EventID(c.idGenerator("cifr")),
		TaskID:                    taskID,
		AnchorMode:                anchorMode,
		AnchorTransitionReceiptID: anchor.ReceiptID,
		AnchorTransitionKind:      anchor.TransitionKind,
		AnchorHandoffID:           anchor.HandoffID,
		AnchorShellSessionID:      anchor.ShellSessionID,
		TriageReceiptID:           triageRecord.ReceiptID,
		TriagePosture:             triageRecord.Posture,
		TriageFollowUpState:       triageRecord.FollowUpPosture,
		ActionKind:                actionKind,
		Summary:                   summary,
		ReviewGapPresent:          triageRecord.ReviewGapPresent,
		ReviewPosture:             triageRecord.ReviewPosture,
		ReviewState:               triageRecord.ReviewState,
		ReviewScope:               triageRecord.ReviewScope,
		ReviewedUpToSequence:      triageRecord.ReviewedUpToSequence,
		OldestUnreviewedSequence:  triageRecord.OldestUnreviewedSequence,
		NewestRetainedSequence:    triageRecord.NewestRetainedSequence,
		UnreviewedRetainedCount:   triageRecord.UnreviewedRetainedCount,
		LatestReviewID:            triageRecord.LatestReviewID,
		LatestReviewGapAckID:      triageRecord.LatestReviewGapAckID,
		AcknowledgmentPresent:     triageRecord.AcknowledgmentPresent,
		AcknowledgmentClass:       triageRecord.AcknowledgmentClass,
		TriagedUnderReviewRisk:    triageRecord.RiskStaleOrUnreviewedReviewPosture || triageRecord.RiskSourceScopedReviewPosture || triageRecord.RiskReviewGapPresent,
		CreatedAt:                 c.clock(),
	}
	if record.Summary == "" {
		actionLabel := strings.ToLower(strings.ReplaceAll(string(record.ActionKind), "_", " "))
		record.Summary = fmt.Sprintf("continuity incident follow-up %s at transition %s", actionLabel, anchor.ReceiptID)
	}

	if err := c.withTx(func(txc *Coordinator) error {
		if err := txc.store.IncidentFollowUps().Create(record); err != nil {
			return err
		}
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"incident_follow_up_receipt_id": record.ReceiptID,
			"anchor_mode":                   record.AnchorMode,
			"anchor_transition_receipt_id":  record.AnchorTransitionReceiptID,
			"anchor_transition_kind":        record.AnchorTransitionKind,
			"triage_receipt_id":             record.TriageReceiptID,
			"triage_posture":                record.TriagePosture,
			"triage_follow_up_posture":      record.TriageFollowUpState,
			"action_kind":                   record.ActionKind,
			"summary":                       record.Summary,
			"review_gap_present":            record.ReviewGapPresent,
			"review_posture":                record.ReviewPosture,
			"review_state":                  record.ReviewState,
			"review_scope":                  record.ReviewScope,
			"reviewed_up_to_sequence":       record.ReviewedUpToSequence,
			"oldest_unreviewed_sequence":    record.OldestUnreviewedSequence,
			"newest_retained_sequence":      record.NewestRetainedSequence,
			"unreviewed_retained_count":     record.UnreviewedRetainedCount,
			"latest_review_id":              record.LatestReviewID,
			"latest_review_gap_ack_id":      record.LatestReviewGapAckID,
			"acknowledgment_present":        record.AcknowledgmentPresent,
			"acknowledgment_class":          record.AcknowledgmentClass,
			"triaged_under_review_risk":     record.TriagedUnderReviewRisk,
		}
		return txc.appendProof(caps, proof.EventContinuityIncidentFollowUpRecorded, proof.ActorUser, "user", payload, nil)
	}); err != nil {
		return RecordContinuityIncidentFollowUpResult{}, err
	}

	latestTransition, recentTriages, followUpSummary, latestFollowUpForAnchor, err := c.continuityIncidentTriageReadProjection(taskID)
	if err != nil {
		return RecordContinuityIncidentFollowUpResult{}, err
	}
	latestFollowUp, recentFollowUps, rollup, err := c.continuityIncidentFollowUpHistoryProjection(taskID, defaultContinuityIncidentFollowUpReadLimit)
	if err != nil {
		return RecordContinuityIncidentFollowUpResult{}, err
	}
	var latestTriage *ContinuityIncidentTriageReceiptSummary
	if len(recentTriages) > 0 {
		copyLatest := recentTriages[0]
		latestTriage = &copyLatest
	}
	if latestFollowUpForAnchor != nil {
		latestFollowUp = latestFollowUpForAnchor
	}
	followUpSummary = deriveFollowUpAwareAdvisory(followUpSummary, rollup, recentFollowUps)
	return RecordContinuityIncidentFollowUpResult{
		TaskID:                                  taskID,
		AnchorMode:                              anchorMode,
		AnchorTransitionReceiptID:               anchor.ReceiptID,
		TriageReceiptID:                         triageRecord.ReceiptID,
		ActionKind:                              actionKind,
		Receipt:                                 continuityIncidentFollowUpReceiptSummary(record),
		LatestContinuityTransition:              latestTransition,
		LatestContinuityIncidentTriage:          latestTriage,
		RecentContinuityIncidentTriages:         recentTriages,
		LatestContinuityIncidentFollowUp:        latestFollowUp,
		RecentContinuityIncidentFollowUps:       recentFollowUps,
		ContinuityIncidentFollowUpHistoryRollup: rollup,
		FollowUp:                                followUpSummary,
	}, nil
}

func continuityIncidentFollowUpEquivalent(existing incidenttriage.FollowUpReceipt, anchorMode incidenttriage.AnchorMode, triageReceiptID common.EventID, actionKind incidenttriage.FollowUpActionKind, summary string) bool {
	return existing.AnchorMode == anchorMode &&
		existing.TriageReceiptID == triageReceiptID &&
		existing.ActionKind == actionKind &&
		strings.TrimSpace(existing.Summary) == summary
}

func normalizeContinuityIncidentFollowUpSummary(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > maxContinuityIncidentFollowUpSummaryChars {
		return trimmed[:maxContinuityIncidentFollowUpSummaryChars]
	}
	return trimmed
}

func parseContinuityIncidentFollowUpActionKind(raw string) (incidenttriage.FollowUpActionKind, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "recorded", "pending", "recorded_pending":
		return incidenttriage.FollowUpActionRecordedPending, nil
	case "progressed":
		return incidenttriage.FollowUpActionProgressed, nil
	case "closed":
		return incidenttriage.FollowUpActionClosed, nil
	case "reopened":
		return incidenttriage.FollowUpActionReopened, nil
	default:
		return "", fmt.Errorf("invalid follow-up action %q (expected recorded_pending|progressed|closed|reopened)", raw)
	}
}

func parseContinuityIncidentFollowUpActionFilter(raw string) (incidenttriage.FollowUpActionKind, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	actionKind, err := parseContinuityIncidentFollowUpActionKind(raw)
	if err != nil {
		return "", fmt.Errorf("unsupported follow-up action filter %q (expected recorded_pending|progressed|closed|reopened)", raw)
	}
	return actionKind, nil
}

func (c *Coordinator) ReadContinuityIncidentFollowUpHistory(ctx context.Context, req ReadContinuityIncidentFollowUpHistoryRequest) (ReadContinuityIncidentFollowUpHistoryResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadContinuityIncidentFollowUpHistoryResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReadContinuityIncidentFollowUpHistoryResult{}, err
	}
	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultContinuityIncidentFollowUpReadLimit
	case limit > maxContinuityIncidentFollowUpReadLimit:
		limit = maxContinuityIncidentFollowUpReadLimit
	}
	beforeReceiptID := common.EventID(strings.TrimSpace(req.BeforeReceiptID))
	var beforeCreatedAt time.Time
	if beforeReceiptID != "" {
		anchor, err := c.store.IncidentFollowUps().GetByTaskReceipt(taskID, beforeReceiptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReadContinuityIncidentFollowUpHistoryResult{}, fmt.Errorf("before receipt %s was not found for task %s", beforeReceiptID, taskID)
			}
			return ReadContinuityIncidentFollowUpHistoryResult{}, err
		}
		beforeCreatedAt = anchor.CreatedAt
	}
	anchorTransitionReceiptID := common.EventID(strings.TrimSpace(req.AnchorTransitionReceiptID))
	triageReceiptID := common.EventID(strings.TrimSpace(req.TriageReceiptID))
	actionKindFilter, err := parseContinuityIncidentFollowUpActionFilter(req.ActionKind)
	if err != nil {
		return ReadContinuityIncidentFollowUpHistoryResult{}, err
	}
	records, err := c.store.IncidentFollowUps().ListByTaskFiltered(taskID, incidenttriage.FollowUpReceiptListFilter{
		Limit:                     limit + 1,
		BeforeReceiptID:           beforeReceiptID,
		BeforeCreatedAt:           beforeCreatedAt,
		AnchorTransitionReceiptID: anchorTransitionReceiptID,
		TriageReceiptID:           triageReceiptID,
		ActionKind:                actionKindFilter,
	})
	if err != nil {
		return ReadContinuityIncidentFollowUpHistoryResult{}, err
	}
	hasMoreOlder := false
	if len(records) > limit {
		hasMoreOlder = true
		records = records[:limit]
	}
	receipts := make([]ContinuityIncidentFollowUpReceiptSummary, 0, len(records))
	for _, record := range records {
		receipts = append(receipts, continuityIncidentFollowUpReceiptSummary(record))
	}
	var latest *ContinuityIncidentFollowUpReceiptSummary
	if len(receipts) > 0 {
		copyLatest := receipts[0]
		latest = &copyLatest
	}

	var latestTransition *ContinuityTransitionReceiptSummary
	if transitionRecord, err := c.store.TransitionReceipts().LatestByTask(taskID); err == nil {
		summary := continuityTransitionReceiptSummary(transitionRecord)
		latestTransition = &summary
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ReadContinuityIncidentFollowUpHistoryResult{}, err
	}

	triageWindow := limit * 3
	if triageWindow < defaultContinuityIncidentTriageReadLimit {
		triageWindow = defaultContinuityIncidentTriageReadLimit
	}
	triageRecords, err := c.store.IncidentTriages().ListByTask(taskID, triageWindow)
	if err != nil {
		return ReadContinuityIncidentFollowUpHistoryResult{}, err
	}
	triageReceipts := make([]ContinuityIncidentTriageReceiptSummary, 0, len(triageRecords))
	for _, record := range triageRecords {
		triageReceipts = append(triageReceipts, continuityIncidentTriageReceiptSummary(record))
	}

	result := ReadContinuityIncidentFollowUpHistoryResult{
		TaskID:                             taskID,
		Bounded:                            true,
		RequestedLimit:                     limit,
		RequestedBeforeReceiptID:           beforeReceiptID,
		RequestedAnchorTransitionReceiptID: anchorTransitionReceiptID,
		RequestedTriageReceiptID:           triageReceiptID,
		RequestedActionKind:                actionKindFilter,
		HasMoreOlder:                       hasMoreOlder,
		Latest:                             latest,
		Receipts:                           receipts,
		Rollup:                             deriveContinuityIncidentFollowUpHistoryRollup(receipts, latestTransition, triageReceipts),
	}
	if latestTransition != nil {
		result.LatestTransitionReceiptID = latestTransition.ReceiptID
	}
	if hasMoreOlder && len(receipts) > 0 {
		result.NextBeforeReceiptID = receipts[len(receipts)-1].ReceiptID
	}
	return result, nil
}

func (c *Coordinator) continuityIncidentFollowUpHistoryProjection(taskID common.TaskID, limit int) (*ContinuityIncidentFollowUpReceiptSummary, []ContinuityIncidentFollowUpReceiptSummary, *ContinuityIncidentFollowUpHistoryRollupSummary, error) {
	readOut, err := c.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID: string(taskID),
		Limit:  limit,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if len(readOut.Receipts) == 0 {
		rollup := readOut.Rollup
		return nil, nil, &rollup, nil
	}
	rollup := readOut.Rollup
	return readOut.Latest, append([]ContinuityIncidentFollowUpReceiptSummary{}, readOut.Receipts...), &rollup, nil
}

func deriveContinuityIncidentFollowUpHistoryRollup(receipts []ContinuityIncidentFollowUpReceiptSummary, latestTransition *ContinuityTransitionReceiptSummary, triages []ContinuityIncidentTriageReceiptSummary) ContinuityIncidentFollowUpHistoryRollupSummary {
	rollup := ContinuityIncidentFollowUpHistoryRollupSummary{
		WindowSize:    len(receipts),
		BoundedWindow: true,
	}
	if len(receipts) == 0 {
		rollup.Summary = "No continuity incident follow-up receipts were found in this bounded window."
		if len(triages) > 0 {
			rollup.AnchorsTriagedWithoutFollowUp = len(distinctIncidentTriageAnchors(triages))
		}
		return rollup
	}

	type anchorAggregate struct {
		count         int
		actions       map[incidenttriage.FollowUpActionKind]struct{}
		latestReceipt ContinuityIncidentFollowUpReceiptSummary
	}
	anchors := make(map[common.EventID]*anchorAggregate)
	anchorOrder := make([]common.EventID, 0, len(receipts))
	for _, receipt := range receipts {
		switch receipt.ActionKind {
		case incidenttriage.FollowUpActionRecordedPending:
			rollup.ReceiptsRecordedPending++
		case incidenttriage.FollowUpActionProgressed:
			rollup.ReceiptsProgressed++
		case incidenttriage.FollowUpActionClosed:
			rollup.ReceiptsClosed++
		case incidenttriage.FollowUpActionReopened:
			rollup.ReceiptsReopened++
		}
		anchorID := receipt.AnchorTransitionReceiptID
		if anchorID == "" {
			anchorID = common.EventID("receipt:" + string(receipt.ReceiptID))
		}
		agg, seen := anchors[anchorID]
		if !seen {
			agg = &anchorAggregate{
				actions:       map[incidenttriage.FollowUpActionKind]struct{}{},
				latestReceipt: receipt,
			}
			anchors[anchorID] = agg
			anchorOrder = append(anchorOrder, anchorID)
		}
		agg.count++
		agg.actions[receipt.ActionKind] = struct{}{}
	}

	rollup.DistinctAnchors = len(anchorOrder)
	for _, anchorID := range anchorOrder {
		agg := anchors[anchorID]
		open := false
		switch agg.latestReceipt.ActionKind {
		case incidenttriage.FollowUpActionClosed:
			rollup.AnchorsClosed++
		case incidenttriage.FollowUpActionReopened:
			rollup.AnchorsReopened++
			open = true
		case incidenttriage.FollowUpActionProgressed, incidenttriage.FollowUpActionRecordedPending:
			open = true
		}
		if open {
			rollup.AnchorsWithOpenFollowUp++
			if latestTransition != nil && agg.latestReceipt.AnchorTransitionReceiptID != "" && agg.latestReceipt.AnchorTransitionReceiptID != latestTransition.ReceiptID {
				rollup.OpenAnchorsBehindLatestTransition++
			}
		}
		if agg.count > 1 && len(agg.actions) == 1 {
			rollup.AnchorsRepeatedWithoutProgression++
		}
	}

	triagedAnchors := distinctIncidentTriageAnchors(triages)
	for anchorID := range triagedAnchors {
		if _, ok := anchors[anchorID]; !ok {
			rollup.AnchorsTriagedWithoutFollowUp++
		}
	}

	rollup.OperationallyNotable =
		rollup.AnchorsWithOpenFollowUp > 0 ||
			rollup.OpenAnchorsBehindLatestTransition > 0 ||
			rollup.AnchorsRepeatedWithoutProgression > 0 ||
			rollup.AnchorsTriagedWithoutFollowUp > 0

	notes := make([]string, 0, 6)
	if rollup.AnchorsWithOpenFollowUp > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) have open follow-up receipts", rollup.AnchorsWithOpenFollowUp))
	}
	if rollup.OpenAnchorsBehindLatestTransition > 0 {
		notes = append(notes, fmt.Sprintf("%d open anchor(s) are behind the latest transition anchor", rollup.OpenAnchorsBehindLatestTransition))
	}
	if rollup.AnchorsTriagedWithoutFollowUp > 0 {
		notes = append(notes, fmt.Sprintf("%d triaged anchor(s) have no follow-up receipts in this bounded window", rollup.AnchorsTriagedWithoutFollowUp))
	}
	if rollup.AnchorsRepeatedWithoutProgression > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) have repeated follow-up receipts without action progression in this window", rollup.AnchorsRepeatedWithoutProgression))
	}
	if len(notes) == 0 {
		rollup.Summary = "Incident follow-up receipts in this bounded window do not show open or lagging closure posture signals."
		return rollup
	}
	rollup.Summary = strings.Join(notes, "; ")
	return rollup
}

func distinctIncidentTriageAnchors(receipts []ContinuityIncidentTriageReceiptSummary) map[common.EventID]struct{} {
	out := make(map[common.EventID]struct{})
	for _, receipt := range receipts {
		if receipt.AnchorTransitionReceiptID == "" {
			continue
		}
		out[receipt.AnchorTransitionReceiptID] = struct{}{}
	}
	return out
}
