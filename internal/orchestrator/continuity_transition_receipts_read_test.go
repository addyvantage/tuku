package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/transition"
)

func TestReadContinuityTransitionHistorySupportsBoundedFiltersAndRiskRollup(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1714000000, 0).UTC()
	records := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               common.EventID("ctr_400"),
			TaskID:                  taskID,
			HandoffID:               "hnd_a",
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			ReviewGapPresent:        true,
			ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
			AcknowledgmentPresent:   false,
			Summary:                 "launch transition recorded under stale review posture",
			CreatedAt:               base.Add(4 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               common.EventID("ctr_390"),
			TaskID:                  taskID,
			HandoffID:               "hnd_a",
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			ReviewGapPresent:        true,
			ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
			AcknowledgmentPresent:   true,
			Summary:                 "launch transition recorded under explicit acknowledgment",
			CreatedAt:               base.Add(3 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               common.EventID("ctr_380"),
			TaskID:                  taskID,
			HandoffID:               "hnd_a",
			TransitionKind:          transition.KindHandoffResolution,
			BranchClassBefore:       string(ActiveBranchClassHandoffClaude),
			BranchClassAfter:        string(ActiveBranchClassLocal),
			HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
			HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
			ReviewGapPresent:        false,
			ReviewPosture:           transition.ReviewPostureGlobalReviewCurrent,
			AcknowledgmentPresent:   false,
			Summary:                 "resolution transition recorded after review",
			CreatedAt:               base.Add(2 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               common.EventID("ctr_370"),
			TaskID:                  taskID,
			HandoffID:               "hnd_b",
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			ReviewGapPresent:        true,
			ReviewPosture:           transition.ReviewPostureSourceScopedReviewStale,
			AcknowledgmentPresent:   false,
			Summary:                 "launch transition recorded under source-scoped stale review posture",
			CreatedAt:               base.Add(1 * time.Second),
		},
	}
	for _, record := range records {
		if err := store.TransitionReceipts().Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	firstPage, err := coord.ReadContinuityTransitionHistory(context.Background(), ReadContinuityTransitionHistoryRequest{
		TaskID: string(taskID),
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("read continuity transition history first page: %v", err)
	}
	if len(firstPage.Receipts) != 2 || firstPage.Receipts[0].ReceiptID != "ctr_400" || firstPage.Receipts[1].ReceiptID != "ctr_390" {
		t.Fatalf("unexpected first page ordering: %+v", firstPage.Receipts)
	}
	if !firstPage.HasMoreOlder || firstPage.NextBeforeReceiptID != "ctr_390" {
		t.Fatalf("expected bounded pagination metadata on first page, got %+v", firstPage)
	}
	if firstPage.RiskSummary.ReviewGapTransitions != 2 || firstPage.RiskSummary.UnacknowledgedReviewGapTransitions != 1 || firstPage.RiskSummary.StaleReviewPostureTransitions != 2 {
		t.Fatalf("unexpected first-page risk summary: %+v", firstPage.RiskSummary)
	}

	secondPage, err := coord.ReadContinuityTransitionHistory(context.Background(), ReadContinuityTransitionHistoryRequest{
		TaskID:          string(taskID),
		Limit:           2,
		BeforeReceiptID: string(firstPage.NextBeforeReceiptID),
	})
	if err != nil {
		t.Fatalf("read continuity transition history second page: %v", err)
	}
	if len(secondPage.Receipts) != 2 || secondPage.Receipts[0].ReceiptID != "ctr_380" || secondPage.Receipts[1].ReceiptID != "ctr_370" {
		t.Fatalf("unexpected second page ordering: %+v", secondPage.Receipts)
	}
	if secondPage.HasMoreOlder {
		t.Fatalf("expected second page to exhaust bounded history window, got %+v", secondPage)
	}

	filteredByKind, err := coord.ReadContinuityTransitionHistory(context.Background(), ReadContinuityTransitionHistoryRequest{
		TaskID:         string(taskID),
		Limit:          10,
		TransitionKind: "handoff_resolution",
	})
	if err != nil {
		t.Fatalf("read filtered transition history by kind: %v", err)
	}
	if len(filteredByKind.Receipts) != 1 || filteredByKind.Receipts[0].ReceiptID != "ctr_380" {
		t.Fatalf("unexpected kind-filtered transition history: %+v", filteredByKind.Receipts)
	}

	filteredByHandoff, err := coord.ReadContinuityTransitionHistory(context.Background(), ReadContinuityTransitionHistoryRequest{
		TaskID:    string(taskID),
		Limit:     10,
		HandoffID: "hnd_b",
	})
	if err != nil {
		t.Fatalf("read filtered transition history by handoff id: %v", err)
	}
	if len(filteredByHandoff.Receipts) != 1 || filteredByHandoff.Receipts[0].ReceiptID != "ctr_370" {
		t.Fatalf("unexpected handoff-filtered transition history: %+v", filteredByHandoff.Receipts)
	}
}

func TestReadContinuityTransitionHistoryRejectsInvalidFilters(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReadContinuityTransitionHistory(context.Background(), ReadContinuityTransitionHistoryRequest{
		TaskID:         string(taskID),
		TransitionKind: "unknown_kind",
	}); err == nil || !strings.Contains(err.Error(), "unsupported transition kind filter") {
		t.Fatalf("expected unsupported transition kind filter error, got %v", err)
	}

	if _, err := coord.ReadContinuityTransitionHistory(context.Background(), ReadContinuityTransitionHistoryRequest{
		TaskID:          string(taskID),
		BeforeReceiptID: "ctr_missing",
	}); err == nil || !strings.Contains(err.Error(), "was not found for task") {
		t.Fatalf("expected invalid before-receipt validation error, got %v", err)
	}
}

func TestContinuityTransitionRiskSummaryIsConsistentAcrossStatusInspectAndShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1715000000, 0).UTC()
	records := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               "ctr_consistency_3",
			TaskID:                  taskID,
			HandoffID:               "hnd_consistency",
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			ReviewGapPresent:        true,
			ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
			AcknowledgmentPresent:   false,
			CreatedAt:               base.Add(3 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_consistency_2",
			TaskID:                  taskID,
			HandoffID:               "hnd_consistency",
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       string(ActiveBranchClassLocal),
			BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			ReviewGapPresent:        true,
			ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
			AcknowledgmentPresent:   true,
			CreatedAt:               base.Add(2 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_consistency_1",
			TaskID:                  taskID,
			HandoffID:               "hnd_consistency",
			TransitionKind:          transition.KindHandoffResolution,
			BranchClassBefore:       string(ActiveBranchClassHandoffClaude),
			BranchClassAfter:        string(ActiveBranchClassLocal),
			HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
			HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
			ReviewGapPresent:        false,
			ReviewPosture:           transition.ReviewPostureGlobalReviewCurrent,
			AcknowledgmentPresent:   false,
			CreatedAt:               base.Add(1 * time.Second),
		},
	}
	for _, record := range records {
		if err := store.TransitionReceipts().Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}

	if statusOut.ContinuityTransitionRiskSummary == nil || inspectOut.ContinuityTransitionRiskSummary == nil || shellOut.ContinuityTransitionRiskSummary == nil {
		t.Fatalf("expected transition risk summary across status/inspect/shell, got status=%+v inspect=%+v shell=%+v",
			statusOut.ContinuityTransitionRiskSummary, inspectOut.ContinuityTransitionRiskSummary, shellOut.ContinuityTransitionRiskSummary)
	}
	statusRisk := statusOut.ContinuityTransitionRiskSummary
	inspectRisk := inspectOut.ContinuityTransitionRiskSummary
	shellRisk := shellOut.ContinuityTransitionRiskSummary
	if statusRisk.ReviewGapTransitions != 2 || statusRisk.UnacknowledgedReviewGapTransitions != 1 || statusRisk.StaleReviewPostureTransitions != 2 {
		t.Fatalf("unexpected status transition risk summary: %+v", statusRisk)
	}
	if inspectRisk.ReviewGapTransitions != statusRisk.ReviewGapTransitions ||
		inspectRisk.UnacknowledgedReviewGapTransitions != statusRisk.UnacknowledgedReviewGapTransitions ||
		inspectRisk.StaleReviewPostureTransitions != statusRisk.StaleReviewPostureTransitions {
		t.Fatalf("expected inspect transition risk summary to match status, got status=%+v inspect=%+v", statusRisk, inspectRisk)
	}
	if shellRisk.ReviewGapTransitions != statusRisk.ReviewGapTransitions ||
		shellRisk.UnacknowledgedReviewGapTransitions != statusRisk.UnacknowledgedReviewGapTransitions ||
		shellRisk.StaleReviewPostureTransitions != statusRisk.StaleReviewPostureTransitions {
		t.Fatalf("expected shell transition risk summary to match status, got status=%+v shell=%+v", statusRisk, shellRisk)
	}
	if statusRisk.Summary == "" || inspectRisk.Summary == "" || shellRisk.Summary == "" {
		t.Fatalf("expected compact risk summary text across status/inspect/shell, got status=%+v inspect=%+v shell=%+v", statusRisk, inspectRisk, shellRisk)
	}
}
