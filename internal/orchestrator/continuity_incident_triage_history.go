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
)

const (
	defaultContinuityIncidentTriageReadLimit = 10
	maxContinuityIncidentTriageReadLimit     = 100
)

type ContinuityIncidentTriageHistoryRollupSummary struct {
	WindowSize                        int    `json:"window_size"`
	BoundedWindow                     bool   `json:"bounded_window"`
	DistinctAnchors                   int    `json:"distinct_anchors"`
	AnchorsTriagedCurrent             int    `json:"anchors_triaged_current"`
	AnchorsNeedsFollowUp              int    `json:"anchors_needs_follow_up"`
	AnchorsDeferred                   int    `json:"anchors_deferred"`
	AnchorsBehindLatestTransition     int    `json:"anchors_behind_latest_transition"`
	AnchorsWithOpenFollowUp           int    `json:"anchors_with_open_follow_up"`
	AnchorsRepeatedWithoutProgression int    `json:"anchors_repeated_without_progression"`
	ReviewRiskReceipts                int    `json:"review_risk_receipts"`
	AcknowledgedReviewGapReceipts     int    `json:"acknowledged_review_gap_receipts"`
	OperationallyNotable              bool   `json:"operationally_notable"`
	Summary                           string `json:"summary,omitempty"`
}

type ReadContinuityIncidentTriageHistoryRequest struct {
	TaskID                    string
	Limit                     int
	BeforeReceiptID           string
	AnchorTransitionReceiptID string
	Posture                   string
}

type ReadContinuityIncidentTriageHistoryResult struct {
	TaskID                             common.TaskID
	Bounded                            bool
	RequestedLimit                     int
	RequestedBeforeReceiptID           common.EventID
	RequestedAnchorTransitionReceiptID common.EventID
	RequestedPosture                   incidenttriage.Posture
	HasMoreOlder                       bool
	NextBeforeReceiptID                common.EventID
	Latest                             *ContinuityIncidentTriageReceiptSummary
	Receipts                           []ContinuityIncidentTriageReceiptSummary
	LatestTransitionReceiptID          common.EventID
	Rollup                             ContinuityIncidentTriageHistoryRollupSummary
}

func (c *Coordinator) ReadContinuityIncidentTriageHistory(ctx context.Context, req ReadContinuityIncidentTriageHistoryRequest) (ReadContinuityIncidentTriageHistoryResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ReadContinuityIncidentTriageHistoryResult{}, fmt.Errorf("task id is required")
	}
	if _, err := c.store.Capsules().Get(taskID); err != nil {
		return ReadContinuityIncidentTriageHistoryResult{}, err
	}
	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultContinuityIncidentTriageReadLimit
	case limit > maxContinuityIncidentTriageReadLimit:
		limit = maxContinuityIncidentTriageReadLimit
	}
	beforeReceiptID := common.EventID(strings.TrimSpace(req.BeforeReceiptID))
	var beforeCreatedAt time.Time
	if beforeReceiptID != "" {
		anchor, err := c.store.IncidentTriages().GetByTaskReceipt(taskID, beforeReceiptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ReadContinuityIncidentTriageHistoryResult{}, fmt.Errorf("before receipt %s was not found for task %s", beforeReceiptID, taskID)
			}
			return ReadContinuityIncidentTriageHistoryResult{}, err
		}
		beforeCreatedAt = anchor.CreatedAt
	}
	anchorTransitionReceiptID := common.EventID(strings.TrimSpace(req.AnchorTransitionReceiptID))
	postureFilter, err := parseContinuityIncidentTriagePostureFilter(req.Posture)
	if err != nil {
		return ReadContinuityIncidentTriageHistoryResult{}, err
	}
	records, err := c.store.IncidentTriages().ListByTaskFiltered(taskID, incidenttriage.ReceiptListFilter{
		Limit:                     limit + 1,
		BeforeReceiptID:           beforeReceiptID,
		BeforeCreatedAt:           beforeCreatedAt,
		AnchorTransitionReceiptID: anchorTransitionReceiptID,
		Posture:                   postureFilter,
	})
	if err != nil {
		return ReadContinuityIncidentTriageHistoryResult{}, err
	}
	hasMoreOlder := false
	if len(records) > limit {
		hasMoreOlder = true
		records = records[:limit]
	}
	receipts := make([]ContinuityIncidentTriageReceiptSummary, 0, len(records))
	for _, record := range records {
		receipts = append(receipts, continuityIncidentTriageReceiptSummary(record))
	}
	var latest *ContinuityIncidentTriageReceiptSummary
	if len(receipts) > 0 {
		copyLatest := receipts[0]
		latest = &copyLatest
	}

	var latestTransition *ContinuityTransitionReceiptSummary
	if transitionRecord, err := c.store.TransitionReceipts().LatestByTask(taskID); err == nil {
		summary := continuityTransitionReceiptSummary(transitionRecord)
		latestTransition = &summary
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ReadContinuityIncidentTriageHistoryResult{}, err
	}

	result := ReadContinuityIncidentTriageHistoryResult{
		TaskID:                             taskID,
		Bounded:                            true,
		RequestedLimit:                     limit,
		RequestedBeforeReceiptID:           beforeReceiptID,
		RequestedAnchorTransitionReceiptID: anchorTransitionReceiptID,
		RequestedPosture:                   postureFilter,
		HasMoreOlder:                       hasMoreOlder,
		Latest:                             latest,
		Receipts:                           receipts,
		Rollup:                             deriveContinuityIncidentTriageHistoryRollup(receipts, latestTransition),
	}
	if latestTransition != nil {
		result.LatestTransitionReceiptID = latestTransition.ReceiptID
	}
	if hasMoreOlder && len(receipts) > 0 {
		result.NextBeforeReceiptID = receipts[len(receipts)-1].ReceiptID
	}
	return result, nil
}

func parseContinuityIncidentTriagePostureFilter(raw string) (incidenttriage.Posture, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	posture, err := parseContinuityIncidentTriagePosture(raw)
	if err != nil {
		return "", fmt.Errorf("unsupported triage posture filter %q (expected triaged|needs_follow_up|deferred)", raw)
	}
	return posture, nil
}

func deriveContinuityIncidentTriageHistoryRollup(receipts []ContinuityIncidentTriageReceiptSummary, latestTransition *ContinuityTransitionReceiptSummary) ContinuityIncidentTriageHistoryRollupSummary {
	rollup := ContinuityIncidentTriageHistoryRollupSummary{
		WindowSize:    len(receipts),
		BoundedWindow: true,
	}
	if len(receipts) == 0 {
		rollup.Summary = "No continuity incident triage receipts were found in this bounded window."
		return rollup
	}

	type anchorAggregate struct {
		count         int
		postures      map[incidenttriage.Posture]struct{}
		latestReceipt ContinuityIncidentTriageReceiptSummary
	}
	anchors := make(map[common.EventID]*anchorAggregate)
	anchorOrder := make([]common.EventID, 0, len(receipts))

	for _, receipt := range receipts {
		if receipt.RiskSummary.StaleOrUnreviewedReviewPosture || receipt.RiskSummary.SourceScopedReviewPosture || receipt.ReviewGapPresent {
			rollup.ReviewRiskReceipts++
		}
		if receipt.ReviewGapPresent && receipt.AcknowledgmentPresent {
			rollup.AcknowledgedReviewGapReceipts++
		}

		anchorID := receipt.AnchorTransitionReceiptID
		if anchorID == "" {
			anchorID = common.EventID("receipt:" + string(receipt.ReceiptID))
		}
		aggregate, seen := anchors[anchorID]
		if !seen {
			aggregate = &anchorAggregate{
				postures:      map[incidenttriage.Posture]struct{}{},
				latestReceipt: receipt,
			}
			anchors[anchorID] = aggregate
			anchorOrder = append(anchorOrder, anchorID)
		}
		aggregate.count++
		aggregate.postures[receipt.Posture] = struct{}{}
	}

	rollup.DistinctAnchors = len(anchorOrder)
	for _, anchorID := range anchorOrder {
		aggregate := anchors[anchorID]
		openFollowUp := false
		switch aggregate.latestReceipt.Posture {
		case incidenttriage.PostureNeedsFollowUp:
			rollup.AnchorsNeedsFollowUp++
			openFollowUp = true
		case incidenttriage.PostureDeferred:
			rollup.AnchorsDeferred++
			openFollowUp = true
		default:
			rollup.AnchorsTriagedCurrent++
		}
		if latestTransition != nil && aggregate.latestReceipt.AnchorTransitionReceiptID != "" && aggregate.latestReceipt.AnchorTransitionReceiptID != latestTransition.ReceiptID {
			rollup.AnchorsBehindLatestTransition++
			openFollowUp = true
		}
		if openFollowUp {
			rollup.AnchorsWithOpenFollowUp++
		}
		if aggregate.count > 1 && len(aggregate.postures) == 1 {
			rollup.AnchorsRepeatedWithoutProgression++
		}
	}

	rollup.OperationallyNotable =
		rollup.AnchorsWithOpenFollowUp > 0 ||
			rollup.AnchorsRepeatedWithoutProgression > 0 ||
			rollup.ReviewRiskReceipts > 0

	notes := make([]string, 0, 5)
	if rollup.AnchorsWithOpenFollowUp > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) remain in open follow-up posture", rollup.AnchorsWithOpenFollowUp))
	}
	if rollup.AnchorsBehindLatestTransition > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) are behind the latest transition anchor", rollup.AnchorsBehindLatestTransition))
	}
	if rollup.AnchorsRepeatedWithoutProgression > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) have repeated triage receipts without posture progression in this window", rollup.AnchorsRepeatedWithoutProgression))
	}
	if rollup.ReviewRiskReceipts > 0 {
		notes = append(notes, fmt.Sprintf("%d triage receipt(s) were recorded under retained transcript review-risk posture", rollup.ReviewRiskReceipts))
	}
	if len(notes) == 0 {
		rollup.Summary = "Incident triage receipts in this bounded window do not show open follow-up posture signals."
		return rollup
	}
	rollup.Summary = strings.Join(notes, "; ")
	return rollup
}
