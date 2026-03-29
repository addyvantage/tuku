package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tuku/internal/domain/common"
)

const (
	defaultContinuityIncidentTaskRiskReadLimit = 20
	maxContinuityIncidentTaskRiskReadLimit     = 100
	maxContinuityIncidentTaskRiskClasses       = 5
)

type ContinuityIncidentTaskRiskClass string

const (
	ContinuityIncidentTaskRiskNone                       ContinuityIncidentTaskRiskClass = "NONE"
	ContinuityIncidentTaskRiskStableBounded              ContinuityIncidentTaskRiskClass = "STABLE_BOUNDED"
	ContinuityIncidentTaskRiskRecurringWeakClosure       ContinuityIncidentTaskRiskClass = "RECURRING_WEAK_CLOSURE"
	ContinuityIncidentTaskRiskRecurringUnresolved        ContinuityIncidentTaskRiskClass = "RECURRING_UNRESOLVED"
	ContinuityIncidentTaskRiskRecurringStagnantFollowUp  ContinuityIncidentTaskRiskClass = "RECURRING_STAGNANT_FOLLOW_UP"
	ContinuityIncidentTaskRiskRecurringTriagedNoFollowUp ContinuityIncidentTaskRiskClass = "RECURRING_TRIAGED_WITHOUT_FOLLOW_UP"
)

type ContinuityIncidentTaskRiskSummary struct {
	Class                               ContinuityIncidentTaskRiskClass  `json:"class"`
	Digest                              string                           `json:"digest,omitempty"`
	WindowAdvisory                      string                           `json:"window_advisory,omitempty"`
	Detail                              string                           `json:"detail,omitempty"`
	BoundedWindow                       bool                             `json:"bounded_window"`
	WindowSize                          int                              `json:"window_size"`
	DistinctAnchors                     int                              `json:"distinct_anchors"`
	RecurringWeakClosure                bool                             `json:"recurring_weak_closure"`
	RecurringUnresolved                 bool                             `json:"recurring_unresolved"`
	RecurringStagnantFollowUp           bool                             `json:"recurring_stagnant_follow_up"`
	RecurringTriagedWithoutFollowUp     bool                             `json:"recurring_triaged_without_follow_up"`
	ReopenedAfterClosureAnchors         int                              `json:"reopened_after_closure_anchors"`
	RepeatedReopenLoopAnchors           int                              `json:"repeated_reopen_loop_anchors"`
	StagnantProgressionAnchors          int                              `json:"stagnant_progression_anchors"`
	AnchorsTriagedWithoutFollowUp       int                              `json:"anchors_triaged_without_follow_up"`
	AnchorsWithOpenFollowUp             int                              `json:"anchors_with_open_follow_up"`
	AnchorsReopened                     int                              `json:"anchors_reopened"`
	OperationallyUnresolvedAnchorSignal int                              `json:"operationally_unresolved_anchor_signal"`
	RecentAnchorClasses                 []ContinuityIncidentClosureClass `json:"recent_anchor_classes,omitempty"`
}

type ReadContinuityIncidentTaskRiskRequest struct {
	TaskID          string
	Limit           int
	BeforeReceiptID string
}

type ReadContinuityIncidentTaskRiskResult struct {
	TaskID                   common.TaskID
	Bounded                  bool
	RequestedLimit           int
	RequestedBeforeReceiptID common.EventID
	HasMoreOlder             bool
	NextBeforeReceiptID      common.EventID
	Summary                  *ContinuityIncidentTaskRiskSummary
	Closure                  *ContinuityIncidentClosureSummary
}

func (c *Coordinator) ReadContinuityIncidentTaskRisk(ctx context.Context, req ReadContinuityIncidentTaskRiskRequest) (ReadContinuityIncidentTaskRiskResult, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return ReadContinuityIncidentTaskRiskResult{}, fmt.Errorf("task id is required")
	}
	limit := req.Limit
	switch {
	case limit <= 0:
		limit = defaultContinuityIncidentTaskRiskReadLimit
	case limit > maxContinuityIncidentTaskRiskReadLimit:
		limit = maxContinuityIncidentTaskRiskReadLimit
	}
	closureOut, err := c.ReadContinuityIncidentClosure(ctx, ReadContinuityIncidentClosureRequest{
		TaskID:          taskID,
		Limit:           limit,
		BeforeReceiptID: req.BeforeReceiptID,
	})
	if err != nil {
		return ReadContinuityIncidentTaskRiskResult{}, err
	}
	return ReadContinuityIncidentTaskRiskResult{
		TaskID:                   closureOut.TaskID,
		Bounded:                  closureOut.Bounded,
		RequestedLimit:           closureOut.RequestedLimit,
		RequestedBeforeReceiptID: closureOut.RequestedBeforeReceiptID,
		HasMoreOlder:             closureOut.HasMoreOlder,
		NextBeforeReceiptID:      closureOut.NextBeforeReceiptID,
		Summary:                  deriveContinuityIncidentTaskRiskSummary(closureOut.Closure, &closureOut.Rollup),
		Closure:                  closureOut.Closure,
	}, nil
}

func (c *Coordinator) continuityIncidentTaskRiskProjection(ctx context.Context, taskID common.TaskID, limit int) (*ContinuityIncidentTaskRiskSummary, error) {
	out, err := c.ReadContinuityIncidentTaskRisk(ctx, ReadContinuityIncidentTaskRiskRequest{
		TaskID: string(taskID),
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	return out.Summary, nil
}

func deriveContinuityIncidentTaskRiskSummary(closure *ContinuityIncidentClosureSummary, rollup *ContinuityIncidentFollowUpHistoryRollupSummary) *ContinuityIncidentTaskRiskSummary {
	if closure == nil && rollup == nil {
		return nil
	}
	out := &ContinuityIncidentTaskRiskSummary{}
	if closure != nil {
		out.BoundedWindow = closure.BoundedWindow
		out.WindowSize = closure.WindowSize
		out.DistinctAnchors = closure.DistinctAnchors
		out.ReopenedAfterClosureAnchors = closure.ReopenedAfterClosureAnchors
		out.RepeatedReopenLoopAnchors = closure.RepeatedReopenLoopAnchors
		out.StagnantProgressionAnchors = closure.StagnantProgressionAnchors
		out.AnchorsTriagedWithoutFollowUp = closure.AnchorsTriagedWithoutFollowUp
		out.AnchorsWithOpenFollowUp = closure.AnchorsWithOpenFollowUp
		out.AnchorsReopened = closure.AnchorsReopened
		out.OperationallyUnresolvedAnchorSignal = closure.AnchorsWithOpenFollowUp + closure.AnchorsReopened + closure.AnchorsTriagedWithoutFollowUp
		out.RecurringStagnantFollowUp = closure.StagnantProgressionAnchors >= 2 || closure.AnchorsRepeatedWithoutProgression >= 2
		out.RecurringTriagedWithoutFollowUp = closure.AnchorsTriagedWithoutFollowUp >= 2
		out.RecurringWeakClosure = closure.ReopenedAfterClosureAnchors >= 2 || closure.RepeatedReopenLoopAnchors >= 1
		out.RecurringUnresolved = closure.OperationallyUnresolved && closure.DistinctAnchors >= 2 && out.OperationallyUnresolvedAnchorSignal >= 2
		if len(closure.RecentAnchors) > 0 {
			classes := make([]ContinuityIncidentClosureClass, 0, len(closure.RecentAnchors))
			seen := map[ContinuityIncidentClosureClass]struct{}{}
			for _, item := range closure.RecentAnchors {
				if item.Class == ContinuityIncidentClosureNone {
					continue
				}
				if _, ok := seen[item.Class]; ok {
					continue
				}
				seen[item.Class] = struct{}{}
				classes = append(classes, item.Class)
			}
			sort.SliceStable(classes, func(i, j int) bool {
				return classes[i] < classes[j]
			})
			if len(classes) > maxContinuityIncidentTaskRiskClasses {
				classes = classes[:maxContinuityIncidentTaskRiskClasses]
			}
			out.RecentAnchorClasses = classes
		}
	} else if rollup != nil {
		out.BoundedWindow = rollup.BoundedWindow
		out.WindowSize = rollup.WindowSize
		out.DistinctAnchors = rollup.DistinctAnchors
		out.AnchorsTriagedWithoutFollowUp = rollup.AnchorsTriagedWithoutFollowUp
		out.AnchorsWithOpenFollowUp = rollup.AnchorsWithOpenFollowUp
		out.AnchorsReopened = rollup.AnchorsReopened
		out.OperationallyUnresolvedAnchorSignal = rollup.AnchorsWithOpenFollowUp + rollup.AnchorsReopened + rollup.AnchorsTriagedWithoutFollowUp
		out.RecurringTriagedWithoutFollowUp = rollup.AnchorsTriagedWithoutFollowUp >= 2
	}

	switch {
	case out.RecurringWeakClosure:
		out.Class = ContinuityIncidentTaskRiskRecurringWeakClosure
		out.Digest = "recurring continuity weakness in recent bounded evidence"
		out.Detail = "Recent bounded evidence suggests recurring weak closure posture across incidents."
	case out.RecurringStagnantFollowUp:
		out.Class = ContinuityIncidentTaskRiskRecurringStagnantFollowUp
		out.Digest = "repeated stagnant follow-up posture in recent bounded evidence"
		out.Detail = "Recent bounded evidence suggests repeated stagnant follow-up progression across incidents."
	case out.RecurringTriagedWithoutFollowUp:
		out.Class = ContinuityIncidentTaskRiskRecurringTriagedNoFollowUp
		out.Digest = "repeated triaged-without-follow-up posture in recent bounded evidence"
		out.Detail = "Recent bounded evidence suggests repeated triaged incidents still lack follow-up receipts."
	case out.RecurringUnresolved:
		out.Class = ContinuityIncidentTaskRiskRecurringUnresolved
		out.Digest = "repeated unresolved incident posture in recent bounded evidence"
		out.Detail = "Recent bounded evidence suggests multiple unresolved incident postures across anchors."
	case closure != nil && closure.DistinctAnchors > 0 && !closure.OperationallyUnresolved:
		out.Class = ContinuityIncidentTaskRiskStableBounded
		out.Digest = "stable bounded recent incident posture"
		out.Detail = "Recent bounded evidence suggests stable incident posture across recent anchors."
	default:
		out.Class = ContinuityIncidentTaskRiskNone
		out.Digest = "no recurring incident-risk signals"
		out.Detail = "No recurring task-level incident-risk signals were detected in this bounded window."
	}

	out.WindowAdvisory = fmt.Sprintf(
		"bounded incident window anchors=%d open=%d reopened=%d triaged-without-follow-up=%d stagnant=%d",
		out.DistinctAnchors,
		out.AnchorsWithOpenFollowUp,
		out.AnchorsReopened,
		out.AnchorsTriagedWithoutFollowUp,
		out.StagnantProgressionAnchors,
	)
	return out
}
