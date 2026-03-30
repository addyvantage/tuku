package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
)

const (
	defaultContinuityIncidentClosureReadLimit = 10
	maxContinuityIncidentClosureReadLimit     = 100
	maxContinuityIncidentClosureAnchorItems   = 5
)

type ContinuityIncidentClosureClass string

const (
	ContinuityIncidentClosureNone                    ContinuityIncidentClosureClass = "NONE"
	ContinuityIncidentClosureStableBounded           ContinuityIncidentClosureClass = "STABLE_BOUNDED"
	ContinuityIncidentClosureOperationallyUnresolved ContinuityIncidentClosureClass = "OPERATIONALLY_UNRESOLVED"
	ContinuityIncidentClosureWeakReopened            ContinuityIncidentClosureClass = "WEAK_CLOSURE_REOPENED"
	ContinuityIncidentClosureWeakLoop                ContinuityIncidentClosureClass = "WEAK_CLOSURE_LOOP"
	ContinuityIncidentClosureWeakStagnant            ContinuityIncidentClosureClass = "WEAK_CLOSURE_STAGNANT"
)

type ContinuityIncidentClosureSummary struct {
	Class                             ContinuityIncidentClosureClass        `json:"class"`
	Digest                            string                                `json:"digest,omitempty"`
	WindowAdvisory                    string                                `json:"window_advisory,omitempty"`
	Detail                            string                                `json:"detail,omitempty"`
	BoundedWindow                     bool                                  `json:"bounded_window"`
	WindowSize                        int                                   `json:"window_size"`
	DistinctAnchors                   int                                   `json:"distinct_anchors"`
	OperationallyUnresolved           bool                                  `json:"operationally_unresolved"`
	ClosureAppearsWeak                bool                                  `json:"closure_appears_weak"`
	ReopenedAfterClosure              bool                                  `json:"reopened_after_closure"`
	RepeatedReopenLoop                bool                                  `json:"repeated_reopen_loop"`
	StagnantProgression               bool                                  `json:"stagnant_progression"`
	TriagedWithoutFollowUp            bool                                  `json:"triaged_without_follow_up"`
	AnchorsWithOpenFollowUp           int                                   `json:"anchors_with_open_follow_up"`
	AnchorsClosed                     int                                   `json:"anchors_closed"`
	AnchorsReopened                   int                                   `json:"anchors_reopened"`
	AnchorsBehindLatestTransition     int                                   `json:"anchors_behind_latest_transition"`
	AnchorsRepeatedWithoutProgression int                                   `json:"anchors_repeated_without_progression"`
	AnchorsTriagedWithoutFollowUp     int                                   `json:"anchors_triaged_without_follow_up"`
	ReopenedAfterClosureAnchors       int                                   `json:"reopened_after_closure_anchors"`
	RepeatedReopenLoopAnchors         int                                   `json:"repeated_reopen_loop_anchors"`
	StagnantProgressionAnchors        int                                   `json:"stagnant_progression_anchors"`
	RecentAnchors                     []ContinuityIncidentClosureAnchorItem `json:"recent_anchors,omitempty"`
}

type ContinuityIncidentClosureAnchorItem struct {
	AnchorTransitionReceiptID string                            `json:"anchor_transition_receipt_id"`
	Class                     ContinuityIncidentClosureClass    `json:"class"`
	Digest                    string                            `json:"digest,omitempty"`
	Explanation               string                            `json:"explanation,omitempty"`
	LatestFollowUpReceiptID   common.EventID                    `json:"latest_follow_up_receipt_id,omitempty"`
	LatestFollowUpActionKind  incidenttriage.FollowUpActionKind `json:"latest_follow_up_action_kind,omitempty"`
	LatestFollowUpAt          time.Time                         `json:"latest_follow_up_at,omitempty"`
}

type ReadContinuityIncidentClosureRequest struct {
	TaskID          string
	Limit           int
	BeforeReceiptID string
}

type ReadContinuityIncidentClosureResult struct {
	TaskID                    common.TaskID
	Bounded                   bool
	RequestedLimit            int
	RequestedBeforeReceiptID  common.EventID
	HasMoreOlder              bool
	NextBeforeReceiptID       common.EventID
	LatestTransitionReceiptID common.EventID
	Latest                    *ContinuityIncidentFollowUpReceiptSummary
	Receipts                  []ContinuityIncidentFollowUpReceiptSummary
	Rollup                    ContinuityIncidentFollowUpHistoryRollupSummary
	FollowUp                  *ContinuityIncidentFollowUpSummary
	Closure                   *ContinuityIncidentClosureSummary
}

func (c *Coordinator) ReadContinuityIncidentClosure(ctx context.Context, req ReadContinuityIncidentClosureRequest) (ReadContinuityIncidentClosureResult, error) {
	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultContinuityIncidentClosureReadLimit
	case limit > maxContinuityIncidentClosureReadLimit:
		limit = maxContinuityIncidentClosureReadLimit
	}
	history, err := c.ReadContinuityIncidentFollowUpHistory(ctx, ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID:          req.TaskID,
		Limit:           limit,
		BeforeReceiptID: req.BeforeReceiptID,
	})
	if err != nil {
		return ReadContinuityIncidentClosureResult{}, err
	}
	latestTransition, _, err := c.continuityTransitionReceiptProjection(history.TaskID, 1)
	if err != nil {
		return ReadContinuityIncidentClosureResult{}, err
	}
	latestTriage, _, err := c.continuityIncidentTriageProjection(history.TaskID, 1)
	if err != nil {
		return ReadContinuityIncidentClosureResult{}, err
	}
	followUp := deriveContinuityIncidentFollowUpSummary(latestTransition, latestTriage, history.Latest)
	followUp = deriveFollowUpAwareAdvisory(followUp, &history.Rollup, history.Receipts)
	var closure *ContinuityIncidentClosureSummary
	if followUp != nil {
		closure = followUp.ClosureIntelligence
	}
	return ReadContinuityIncidentClosureResult{
		TaskID:                    history.TaskID,
		Bounded:                   history.Bounded,
		RequestedLimit:            history.RequestedLimit,
		RequestedBeforeReceiptID:  history.RequestedBeforeReceiptID,
		HasMoreOlder:              history.HasMoreOlder,
		NextBeforeReceiptID:       history.NextBeforeReceiptID,
		LatestTransitionReceiptID: history.LatestTransitionReceiptID,
		Latest:                    history.Latest,
		Receipts:                  append([]ContinuityIncidentFollowUpReceiptSummary{}, history.Receipts...),
		Rollup:                    history.Rollup,
		FollowUp:                  followUp,
		Closure:                   closure,
	}, nil
}

func deriveContinuityIncidentClosureSummary(rollup *ContinuityIncidentFollowUpHistoryRollupSummary, receipts []ContinuityIncidentFollowUpReceiptSummary) *ContinuityIncidentClosureSummary {
	if rollup == nil {
		return nil
	}
	out := &ContinuityIncidentClosureSummary{
		BoundedWindow:                     rollup.BoundedWindow,
		WindowSize:                        rollup.WindowSize,
		DistinctAnchors:                   rollup.DistinctAnchors,
		AnchorsWithOpenFollowUp:           rollup.AnchorsWithOpenFollowUp,
		AnchorsClosed:                     rollup.AnchorsClosed,
		AnchorsReopened:                   rollup.AnchorsReopened,
		AnchorsBehindLatestTransition:     rollup.OpenAnchorsBehindLatestTransition,
		AnchorsRepeatedWithoutProgression: rollup.AnchorsRepeatedWithoutProgression,
		AnchorsTriagedWithoutFollowUp:     rollup.AnchorsTriagedWithoutFollowUp,
		TriagedWithoutFollowUp:            rollup.AnchorsTriagedWithoutFollowUp > 0,
	}
	type anchorSignals struct {
		anchorID                  common.EventID
		closedSeen                bool
		closedCount               int
		reopenedCount             int
		progressedOrPendingCount  int
		reopenedAfterClosureCount int
		latestFollowUpReceiptID   common.EventID
		latestFollowUpActionKind  incidenttriage.FollowUpActionKind
		latestFollowUpAt          time.Time
	}
	anchors := map[common.EventID]*anchorSignals{}
	for i := len(receipts) - 1; i >= 0; i-- { // oldest -> newest for order-aware reopen detection
		item := receipts[i]
		anchorID := item.AnchorTransitionReceiptID
		if anchorID == "" {
			continue
		}
		sig := anchors[anchorID]
		if sig == nil {
			sig = &anchorSignals{anchorID: anchorID}
			anchors[anchorID] = sig
		}
		if sig.latestFollowUpAt.IsZero() || item.CreatedAt.After(sig.latestFollowUpAt) {
			sig.latestFollowUpAt = item.CreatedAt
			sig.latestFollowUpReceiptID = item.ReceiptID
			sig.latestFollowUpActionKind = item.ActionKind
		}
		switch item.ActionKind {
		case incidenttriage.FollowUpActionClosed:
			sig.closedCount++
			sig.closedSeen = true
		case incidenttriage.FollowUpActionReopened:
			sig.reopenedCount++
			if sig.closedSeen {
				sig.reopenedAfterClosureCount++
			}
		case incidenttriage.FollowUpActionProgressed, incidenttriage.FollowUpActionRecordedPending:
			sig.progressedOrPendingCount++
		}
	}
	anchorItems := make([]ContinuityIncidentClosureAnchorItem, 0, len(anchors))
	for _, sig := range anchors {
		if sig.reopenedAfterClosureCount > 0 {
			out.ReopenedAfterClosureAnchors++
		}
		if sig.reopenedAfterClosureCount > 1 {
			out.RepeatedReopenLoopAnchors++
		}
		if sig.progressedOrPendingCount >= 2 && sig.closedCount == 0 && sig.reopenedCount == 0 {
			out.StagnantProgressionAnchors++
		}
		anchorItems = append(anchorItems, deriveContinuityIncidentClosureAnchorItem(
			sig.anchorID,
			sig.latestFollowUpReceiptID,
			sig.latestFollowUpActionKind,
			sig.latestFollowUpAt,
			sig.closedCount,
			sig.reopenedCount,
			sig.progressedOrPendingCount,
			sig.reopenedAfterClosureCount,
		))
	}
	sort.SliceStable(anchorItems, func(i, j int) bool {
		left := anchorItems[i].LatestFollowUpAt
		right := anchorItems[j].LatestFollowUpAt
		if left.Equal(right) {
			return anchorItems[i].AnchorTransitionReceiptID > anchorItems[j].AnchorTransitionReceiptID
		}
		return left.After(right)
	})
	if len(anchorItems) > maxContinuityIncidentClosureAnchorItems {
		anchorItems = anchorItems[:maxContinuityIncidentClosureAnchorItems]
	}
	out.RecentAnchors = anchorItems
	out.ReopenedAfterClosure = out.ReopenedAfterClosureAnchors > 0
	out.RepeatedReopenLoop = out.RepeatedReopenLoopAnchors > 0
	out.StagnantProgression = out.StagnantProgressionAnchors > 0 || out.AnchorsRepeatedWithoutProgression > 0
	out.ClosureAppearsWeak = out.ReopenedAfterClosure || out.RepeatedReopenLoop || out.StagnantProgression || out.AnchorsBehindLatestTransition > 0
	out.OperationallyUnresolved = out.ClosureAppearsWeak || out.TriagedWithoutFollowUp || out.AnchorsWithOpenFollowUp > 0 || out.AnchorsReopened > 0

	notes := make([]string, 0, 5)
	if out.RepeatedReopenLoop {
		out.Class = ContinuityIncidentClosureWeakLoop
		out.Digest = "closure loop signals"
		notes = append(notes, fmt.Sprintf("%d anchor(s) show repeated reopen-after-closure patterns in this bounded window", out.RepeatedReopenLoopAnchors))
	} else if out.ReopenedAfterClosure {
		out.Class = ContinuityIncidentClosureWeakReopened
		out.Digest = "closure reopened after close"
		notes = append(notes, fmt.Sprintf("%d anchor(s) were reopened after closure in this bounded window", out.ReopenedAfterClosureAnchors))
	} else if out.StagnantProgression {
		out.Class = ContinuityIncidentClosureWeakStagnant
		out.Digest = "follow-up progression stagnant"
		notes = append(notes, "follow-up progression appears stagnant in this bounded window")
	} else if out.OperationallyUnresolved {
		out.Class = ContinuityIncidentClosureOperationallyUnresolved
		out.Digest = "operationally unresolved"
	} else if out.AnchorsClosed > 0 {
		out.Class = ContinuityIncidentClosureStableBounded
		out.Digest = "stable within bounded evidence"
	} else {
		out.Class = ContinuityIncidentClosureNone
		out.Digest = "no closure intelligence signals"
	}

	if out.TriagedWithoutFollowUp {
		notes = append(notes, fmt.Sprintf("%d triaged anchor(s) still have no follow-up receipts in this bounded window", out.AnchorsTriagedWithoutFollowUp))
	}
	if out.AnchorsWithOpenFollowUp > 0 {
		notes = append(notes, fmt.Sprintf("%d anchor(s) remain in open follow-up posture in this bounded window", out.AnchorsWithOpenFollowUp))
	}
	if out.AnchorsBehindLatestTransition > 0 {
		notes = append(notes, fmt.Sprintf("%d open anchor(s) are behind the latest transition anchor in this bounded window", out.AnchorsBehindLatestTransition))
	}

	out.WindowAdvisory = fmt.Sprintf(
		"bounded window anchors=%d open=%d closed=%d reopened=%d triaged-without-follow-up=%d repeated=%d behind-latest=%d",
		out.DistinctAnchors,
		out.AnchorsWithOpenFollowUp,
		out.AnchorsClosed,
		out.AnchorsReopened,
		out.AnchorsTriagedWithoutFollowUp,
		out.AnchorsRepeatedWithoutProgression,
		out.AnchorsBehindLatestTransition,
	)
	switch {
	case out.OperationallyUnresolved:
		out.Detail = "Recent bounded evidence suggests incident closure remains operationally unresolved."
		if len(notes) > 0 {
			out.Detail = out.Detail + " " + strings.Join(notes, "; ") + "."
		}
	case out.Class == ContinuityIncidentClosureStableBounded:
		out.Detail = "Recent bounded evidence does not show reopen or stagnation closure signals."
	default:
		out.Detail = "No closure-intelligence signals were detected in this bounded window."
	}
	return out
}

func deriveContinuityIncidentClosureAnchorItem(
	anchorID common.EventID,
	latestReceiptID common.EventID,
	latestActionKind incidenttriage.FollowUpActionKind,
	latestAt time.Time,
	closedCount int,
	reopenedCount int,
	progressedOrPendingCount int,
	reopenedAfterClosureCount int,
) ContinuityIncidentClosureAnchorItem {
	out := ContinuityIncidentClosureAnchorItem{
		AnchorTransitionReceiptID: string(anchorID),
		LatestFollowUpReceiptID:   latestReceiptID,
		LatestFollowUpActionKind:  latestActionKind,
		LatestFollowUpAt:          latestAt,
	}
	switch {
	case reopenedAfterClosureCount > 1:
		out.Class = ContinuityIncidentClosureWeakLoop
		out.Digest = "closure loop signals"
		out.Explanation = "repeated reopen-after-closure pattern in recent bounded evidence"
	case reopenedAfterClosureCount > 0:
		out.Class = ContinuityIncidentClosureWeakReopened
		out.Digest = "closure reopened after close"
		out.Explanation = "reopened after closure in recent bounded evidence"
	case progressedOrPendingCount >= 2 && closedCount == 0 && reopenedCount == 0:
		out.Class = ContinuityIncidentClosureWeakStagnant
		out.Digest = "follow-up progression stagnant"
		out.Explanation = "follow-up progression remains stagnant in recent bounded evidence"
	case closedCount > 0 && reopenedCount == 0:
		out.Class = ContinuityIncidentClosureStableBounded
		out.Digest = "stable within bounded evidence"
		out.Explanation = "stable bounded closure progression"
	default:
		out.Class = ContinuityIncidentClosureOperationallyUnresolved
		out.Digest = "operationally unresolved"
		out.Explanation = "follow-up posture remains operationally unresolved in recent bounded evidence"
	}
	return out
}
