package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/transition"
)

func TestReadContinuityIncidentTriageHistorySupportsBoundedFiltersAndRollup(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1718000000, 0).UTC()
	transitions := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               "ctr_th_410",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffLaunch,
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			CreatedAt:               base.Add(4 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_th_400",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffResolution,
			HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
			HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
			CreatedAt:               base.Add(3 * time.Second),
		},
	}
	for _, record := range transitions {
		if err := store.TransitionReceipts().Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	triages := []incidenttriage.Receipt{
		{
			Version:                   1,
			ReceiptID:                 common.EventID("citr_th_430"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_th_410",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			Posture:                   incidenttriage.PostureNeedsFollowUp,
			FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
			ReviewGapPresent:          true,
			RiskReviewGapPresent:      true,
			CreatedAt:                 base.Add(5 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("citr_th_420"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_th_410",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			Posture:                   incidenttriage.PostureNeedsFollowUp,
			FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
			ReviewGapPresent:          true,
			RiskReviewGapPresent:      true,
			AcknowledgmentPresent:     true,
			CreatedAt:                 base.Add(4 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("citr_th_410"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_th_400",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			Posture:                   incidenttriage.PostureDeferred,
			FollowUpPosture:           incidenttriage.FollowUpPostureDeferred,
			CreatedAt:                 base.Add(3 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("citr_th_405"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_th_400",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			Posture:                   incidenttriage.PostureTriaged,
			FollowUpPosture:           incidenttriage.FollowUpPostureNone,
			CreatedAt:                 base.Add(2 * time.Second),
		},
	}
	for _, record := range triages {
		if err := store.IncidentTriages().Create(record); err != nil {
			t.Fatalf("create triage receipt %s: %v", record.ReceiptID, err)
		}
	}

	firstPage, err := coord.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID: string(taskID),
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("read first triage history page: %v", err)
	}
	if len(firstPage.Receipts) != 2 || firstPage.Receipts[0].ReceiptID != "citr_th_430" || firstPage.Receipts[1].ReceiptID != "citr_th_420" {
		t.Fatalf("unexpected first-page ordering: %+v", firstPage.Receipts)
	}
	if !firstPage.HasMoreOlder || firstPage.NextBeforeReceiptID != "citr_th_420" {
		t.Fatalf("expected first-page bounded pagination metadata, got %+v", firstPage)
	}
	if firstPage.Rollup.AnchorsWithOpenFollowUp != 1 || firstPage.Rollup.AnchorsRepeatedWithoutProgression != 1 {
		t.Fatalf("unexpected first-page triage rollup: %+v", firstPage.Rollup)
	}

	secondPage, err := coord.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID:          string(taskID),
		Limit:           2,
		BeforeReceiptID: string(firstPage.NextBeforeReceiptID),
	})
	if err != nil {
		t.Fatalf("read second triage history page: %v", err)
	}
	if len(secondPage.Receipts) != 2 || secondPage.Receipts[0].ReceiptID != "citr_th_410" || secondPage.Receipts[1].ReceiptID != "citr_th_405" {
		t.Fatalf("unexpected second-page ordering: %+v", secondPage.Receipts)
	}
	if secondPage.HasMoreOlder {
		t.Fatalf("expected second page to exhaust bounded history window, got %+v", secondPage)
	}
	if secondPage.Rollup.AnchorsBehindLatestTransition != 1 {
		t.Fatalf("expected second-page rollup to flag behind-latest transition anchor, got %+v", secondPage.Rollup)
	}

	filteredByAnchorAndPosture, err := coord.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID:                    string(taskID),
		Limit:                     10,
		AnchorTransitionReceiptID: "ctr_th_400",
		Posture:                   "deferred",
	})
	if err != nil {
		t.Fatalf("read filtered triage history: %v", err)
	}
	if len(filteredByAnchorAndPosture.Receipts) != 1 || filteredByAnchorAndPosture.Receipts[0].ReceiptID != "citr_th_410" {
		t.Fatalf("unexpected filtered triage history receipts: %+v", filteredByAnchorAndPosture.Receipts)
	}
	if filteredByAnchorAndPosture.RequestedPosture != incidenttriage.PostureDeferred {
		t.Fatalf("expected requested posture filter to round-trip, got %+v", filteredByAnchorAndPosture.RequestedPosture)
	}
}

func TestReadContinuityIncidentTriageHistoryRejectsInvalidFilters(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID: "tsk_missing",
	}); err == nil {
		t.Fatal("expected missing task validation error")
	}

	if _, err := coord.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID:  string(taskID),
		Posture: "unknown_posture",
	}); err == nil || !strings.Contains(err.Error(), "unsupported triage posture filter") {
		t.Fatalf("expected unsupported posture filter error, got %v", err)
	}

	if _, err := coord.ReadContinuityIncidentTriageHistory(context.Background(), ReadContinuityIncidentTriageHistoryRequest{
		TaskID:          string(taskID),
		BeforeReceiptID: "citr_missing",
	}); err == nil || !strings.Contains(err.Error(), "was not found for task") {
		t.Fatalf("expected before-receipt validation error, got %v", err)
	}
}

func TestContinuityIncidentTriageHistoryRollupConsistencyAcrossStatusInspectAndShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1718001000, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_consistency_new",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		CreatedAt:               base.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("create latest transition receipt: %v", err)
	}
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_consistency_old",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffResolution,
		HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
		HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
		CreatedAt:               base.Add(1 * time.Second),
	}); err != nil {
		t.Fatalf("create older transition receipt: %v", err)
	}
	if err := store.IncidentTriages().Create(incidenttriage.Receipt{
		Version:                   1,
		ReceiptID:                 "citr_consistency_a",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_consistency_old",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		Posture:                   incidenttriage.PostureNeedsFollowUp,
		FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
		CreatedAt:                 base.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("create triage receipt: %v", err)
	}
	if err := store.IncidentTriages().Create(incidenttriage.Receipt{
		Version:                   1,
		ReceiptID:                 "citr_consistency_b",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_consistency_old",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		Posture:                   incidenttriage.PostureNeedsFollowUp,
		FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
		CreatedAt:                 base.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("create repeated triage receipt: %v", err)
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
	if statusOut.ContinuityIncidentTriageHistoryRollup == nil || inspectOut.ContinuityIncidentTriageHistoryRollup == nil || shellOut.ContinuityIncidentTriageHistoryRollup == nil {
		t.Fatalf("expected triage-history rollup across status/inspect/shell, got status=%+v inspect=%+v shell=%+v",
			statusOut.ContinuityIncidentTriageHistoryRollup, inspectOut.ContinuityIncidentTriageHistoryRollup, shellOut.ContinuityIncidentTriageHistoryRollup)
	}
	statusRollup := statusOut.ContinuityIncidentTriageHistoryRollup
	inspectRollup := inspectOut.ContinuityIncidentTriageHistoryRollup
	shellRollup := shellOut.ContinuityIncidentTriageHistoryRollup
	if statusRollup.AnchorsBehindLatestTransition != 1 || statusRollup.AnchorsRepeatedWithoutProgression != 1 || statusRollup.AnchorsWithOpenFollowUp != 1 {
		t.Fatalf("unexpected status triage-history rollup: %+v", statusRollup)
	}
	if inspectRollup.AnchorsBehindLatestTransition != statusRollup.AnchorsBehindLatestTransition ||
		inspectRollup.AnchorsRepeatedWithoutProgression != statusRollup.AnchorsRepeatedWithoutProgression ||
		inspectRollup.AnchorsWithOpenFollowUp != statusRollup.AnchorsWithOpenFollowUp {
		t.Fatalf("expected inspect triage-history rollup to match status, got status=%+v inspect=%+v", statusRollup, inspectRollup)
	}
	if shellRollup.AnchorsBehindLatestTransition != statusRollup.AnchorsBehindLatestTransition ||
		shellRollup.AnchorsRepeatedWithoutProgression != statusRollup.AnchorsRepeatedWithoutProgression ||
		shellRollup.AnchorsWithOpenFollowUp != statusRollup.AnchorsWithOpenFollowUp {
		t.Fatalf("expected shell triage-history rollup to match status, got status=%+v shell=%+v", statusRollup, shellRollup)
	}
	if strings.TrimSpace(statusRollup.Summary) == "" || strings.TrimSpace(inspectRollup.Summary) == "" || strings.TrimSpace(shellRollup.Summary) == "" {
		t.Fatalf("expected triage-history rollup summary across status/inspect/shell, got status=%+v inspect=%+v shell=%+v", statusRollup, inspectRollup, shellRollup)
	}
}
