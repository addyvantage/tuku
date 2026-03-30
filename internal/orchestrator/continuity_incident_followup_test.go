package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/transition"
)

func TestRecordContinuityIncidentFollowUpPersistsProofAndSurfacesAcrossReadModels(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	anchorAt := time.Unix(1719000000, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_followup_1",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffResolution,
		BranchClassBefore:       string(ActiveBranchClassHandoffClaude),
		BranchClassAfter:        string(ActiveBranchClassLocal),
		HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
		HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
		ReviewGapPresent:        true,
		ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
		AcknowledgmentPresent:   true,
		Summary:                 "resolution transition under stale review posture with explicit acknowledgment",
		CreatedAt:               anchorAt,
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}

	triageOut, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:     string(taskID),
		AnchorMode: "latest",
		Posture:    "needs_follow_up",
		Summary:    "triaged bounded incident evidence and marked follow-up open",
	})
	if err != nil {
		t.Fatalf("record continuity incident triage: %v", err)
	}

	out, err := coord.RecordContinuityIncidentFollowUp(context.Background(), RecordContinuityIncidentFollowUpRequest{
		TaskID:          string(taskID),
		AnchorMode:      "latest",
		TriageReceiptID: string(triageOut.Receipt.ReceiptID),
		ActionKind:      "progressed",
		Summary:         "operator progressed follow-up after reviewing bounded retained evidence",
	})
	if err != nil {
		t.Fatalf("record continuity incident follow-up: %v", err)
	}
	if out.Reused {
		t.Fatalf("expected first follow-up write to create new receipt, got reused result: %+v", out)
	}
	if out.Receipt.ReceiptID == "" || out.Receipt.AnchorTransitionReceiptID != "ctr_followup_1" {
		t.Fatalf("unexpected follow-up receipt summary: %+v", out.Receipt)
	}
	if out.FollowUp == nil || out.FollowUp.State != ContinuityIncidentFollowUpProgressed {
		t.Fatalf("expected progressed follow-up summary in write result, got %+v", out.FollowUp)
	}
	if out.ContinuityIncidentFollowUpHistoryRollup == nil || out.ContinuityIncidentFollowUpHistoryRollup.ReceiptsProgressed != 1 {
		t.Fatalf("expected follow-up history rollup in write result, got %+v", out.ContinuityIncidentFollowUpHistoryRollup)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if statusOut.LatestContinuityIncidentFollowUpReceipt == nil || statusOut.LatestContinuityIncidentFollowUpReceipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected status follow-up projection, got %+v", statusOut.LatestContinuityIncidentFollowUpReceipt)
	}
	if statusOut.ContinuityIncidentFollowUpHistoryRollup == nil || statusOut.ContinuityIncidentFollowUpHistoryRollup.ReceiptsProgressed != 1 {
		t.Fatalf("expected status follow-up history rollup, got %+v", statusOut.ContinuityIncidentFollowUpHistoryRollup)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestContinuityIncidentFollowUpReceipt == nil || inspectOut.LatestContinuityIncidentFollowUpReceipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected inspect follow-up projection, got %+v", inspectOut.LatestContinuityIncidentFollowUpReceipt)
	}

	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}
	if shellOut.LatestContinuityIncidentFollowUpReceipt == nil || shellOut.LatestContinuityIncidentFollowUpReceipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected shell follow-up projection, got %+v", shellOut.LatestContinuityIncidentFollowUpReceipt)
	}
	if shellOut.ContinuityIncidentFollowUp == nil || shellOut.ContinuityIncidentFollowUp.State != ContinuityIncidentFollowUpProgressed {
		t.Fatalf("expected shell follow-up summary, got %+v", shellOut.ContinuityIncidentFollowUp)
	}

	events, err := store.Proofs().ListByTask(taskID, 50)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventContinuityIncidentFollowUpRecorded) {
		t.Fatalf("expected continuity incident follow-up proof event, got %+v", events)
	}
}

func TestRecordContinuityIncidentFollowUpRejectsInvalidTaskAnchorAndAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RecordContinuityIncidentFollowUp(context.Background(), RecordContinuityIncidentFollowUpRequest{
		TaskID:     "tsk_missing",
		ActionKind: "closed",
	}); err == nil {
		t.Fatal("expected missing task validation error")
	}

	if _, err := coord.RecordContinuityIncidentFollowUp(context.Background(), RecordContinuityIncidentFollowUpRequest{
		TaskID:     string(taskID),
		ActionKind: "closed",
	}); err == nil || !strings.Contains(err.Error(), "no continuity transition receipt exists") {
		t.Fatalf("expected missing anchor transition validation error, got %v", err)
	}

	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_followup_validate",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		CreatedAt:               time.Unix(1719000100, 0).UTC(),
	}); err != nil {
		t.Fatalf("create transition receipt for validation test: %v", err)
	}
	if _, err := coord.RecordContinuityIncidentFollowUp(context.Background(), RecordContinuityIncidentFollowUpRequest{
		TaskID:     string(taskID),
		ActionKind: "not-a-valid-action",
	}); err == nil || !strings.Contains(err.Error(), "invalid follow-up action") {
		t.Fatalf("expected invalid action validation error, got %v", err)
	}
	if _, err := coord.RecordContinuityIncidentFollowUp(context.Background(), RecordContinuityIncidentFollowUpRequest{
		TaskID:     string(taskID),
		ActionKind: "closed",
	}); err == nil || !strings.Contains(err.Error(), "no continuity incident triage receipt exists") {
		t.Fatalf("expected missing triage validation error, got %v", err)
	}
}

func TestReadContinuityIncidentFollowUpHistorySupportsBoundedFiltersAndRollup(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1719001000, 0).UTC()
	transitions := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               "ctr_fh_410",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffLaunch,
			HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
			HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
			CreatedAt:               base.Add(4 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_fh_400",
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
			ReceiptID:                 common.EventID("citr_fh_430"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_fh_410",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			Posture:                   incidenttriage.PostureNeedsFollowUp,
			FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
			CreatedAt:                 base.Add(5 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("citr_fh_410"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_fh_400",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			Posture:                   incidenttriage.PostureDeferred,
			FollowUpPosture:           incidenttriage.FollowUpPostureDeferred,
			CreatedAt:                 base.Add(3 * time.Second),
		},
	}
	for _, record := range triages {
		if err := store.IncidentTriages().Create(record); err != nil {
			t.Fatalf("create triage receipt %s: %v", record.ReceiptID, err)
		}
	}
	followUps := []incidenttriage.FollowUpReceipt{
		{
			Version:                   1,
			ReceiptID:                 common.EventID("cifr_fh_430"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_fh_410",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			TriageReceiptID:           common.EventID("citr_fh_430"),
			TriagePosture:             incidenttriage.PostureNeedsFollowUp,
			TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
			ActionKind:                incidenttriage.FollowUpActionProgressed,
			CreatedAt:                 base.Add(5 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("cifr_fh_420"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_fh_410",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			TriageReceiptID:           common.EventID("citr_fh_430"),
			TriagePosture:             incidenttriage.PostureNeedsFollowUp,
			TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
			ActionKind:                incidenttriage.FollowUpActionProgressed,
			CreatedAt:                 base.Add(4 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("cifr_fh_410"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_fh_400",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			TriageReceiptID:           common.EventID("citr_fh_410"),
			TriagePosture:             incidenttriage.PostureDeferred,
			TriageFollowUpState:       incidenttriage.FollowUpPostureDeferred,
			ActionKind:                incidenttriage.FollowUpActionClosed,
			CreatedAt:                 base.Add(3 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 common.EventID("cifr_fh_405"),
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_fh_400",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			TriageReceiptID:           common.EventID("citr_fh_410"),
			TriagePosture:             incidenttriage.PostureDeferred,
			TriageFollowUpState:       incidenttriage.FollowUpPostureDeferred,
			ActionKind:                incidenttriage.FollowUpActionReopened,
			CreatedAt:                 base.Add(2 * time.Second),
		},
	}
	for _, record := range followUps {
		if err := store.IncidentFollowUps().Create(record); err != nil {
			t.Fatalf("create follow-up receipt %s: %v", record.ReceiptID, err)
		}
	}

	firstPage, err := coord.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID: string(taskID),
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("read first follow-up history page: %v", err)
	}
	if len(firstPage.Receipts) != 2 || firstPage.Receipts[0].ReceiptID != "cifr_fh_430" || firstPage.Receipts[1].ReceiptID != "cifr_fh_420" {
		t.Fatalf("unexpected first-page follow-up ordering: %+v", firstPage.Receipts)
	}
	if !firstPage.HasMoreOlder || firstPage.NextBeforeReceiptID != "cifr_fh_420" {
		t.Fatalf("expected first-page bounded pagination metadata, got %+v", firstPage)
	}
	if firstPage.Rollup.AnchorsWithOpenFollowUp != 1 || firstPage.Rollup.AnchorsRepeatedWithoutProgression != 1 {
		t.Fatalf("unexpected first-page follow-up rollup: %+v", firstPage.Rollup)
	}

	secondPage, err := coord.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID:          string(taskID),
		Limit:           2,
		BeforeReceiptID: string(firstPage.NextBeforeReceiptID),
	})
	if err != nil {
		t.Fatalf("read second follow-up history page: %v", err)
	}
	if len(secondPage.Receipts) != 2 || secondPage.Receipts[0].ReceiptID != "cifr_fh_410" || secondPage.Receipts[1].ReceiptID != "cifr_fh_405" {
		t.Fatalf("unexpected second-page follow-up ordering: %+v", secondPage.Receipts)
	}
	if secondPage.HasMoreOlder {
		t.Fatalf("expected second page to exhaust bounded follow-up history window, got %+v", secondPage)
	}

	filteredByAnchorAndAction, err := coord.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID:                    string(taskID),
		Limit:                     10,
		AnchorTransitionReceiptID: "ctr_fh_400",
		ActionKind:                "closed",
	})
	if err != nil {
		t.Fatalf("read filtered follow-up history: %v", err)
	}
	if len(filteredByAnchorAndAction.Receipts) != 1 || filteredByAnchorAndAction.Receipts[0].ReceiptID != "cifr_fh_410" {
		t.Fatalf("unexpected filtered follow-up history receipts: %+v", filteredByAnchorAndAction.Receipts)
	}
	if filteredByAnchorAndAction.RequestedActionKind != incidenttriage.FollowUpActionClosed {
		t.Fatalf("expected requested action filter to round-trip, got %+v", filteredByAnchorAndAction.RequestedActionKind)
	}
}

func TestReadContinuityIncidentFollowUpHistoryRejectsInvalidFilters(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID: "tsk_missing",
	}); err == nil {
		t.Fatal("expected missing task validation error")
	}

	if _, err := coord.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID:     string(taskID),
		ActionKind: "unknown_action",
	}); err == nil || !strings.Contains(err.Error(), "unsupported follow-up action filter") {
		t.Fatalf("expected unsupported action filter error, got %v", err)
	}

	if _, err := coord.ReadContinuityIncidentFollowUpHistory(context.Background(), ReadContinuityIncidentFollowUpHistoryRequest{
		TaskID:          string(taskID),
		BeforeReceiptID: "cifr_missing",
	}); err == nil || !strings.Contains(err.Error(), "was not found for task") {
		t.Fatalf("expected before-receipt validation error, got %v", err)
	}
}

func TestContinuityIncidentFollowUpHistoryRollupConsistencyAcrossStatusInspectAndShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1719002000, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_followup_consistency_new",
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
		ReceiptID:               "ctr_followup_consistency_old",
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
		ReceiptID:                 "citr_followup_consistency",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_consistency_old",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		Posture:                   incidenttriage.PostureNeedsFollowUp,
		FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
		CreatedAt:                 base.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("create triage receipt: %v", err)
	}
	if err := store.IncidentFollowUps().Create(incidenttriage.FollowUpReceipt{
		Version:                   1,
		ReceiptID:                 "cifr_followup_consistency",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_consistency_old",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		TriageReceiptID:           "citr_followup_consistency",
		TriagePosture:             incidenttriage.PostureNeedsFollowUp,
		TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
		ActionKind:                incidenttriage.FollowUpActionRecordedPending,
		CreatedAt:                 base.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("create follow-up receipt: %v", err)
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
	if statusOut.ContinuityIncidentFollowUpHistoryRollup == nil || inspectOut.ContinuityIncidentFollowUpHistoryRollup == nil || shellOut.ContinuityIncidentFollowUpHistoryRollup == nil {
		t.Fatalf("expected follow-up-history rollup across status/inspect/shell, got status=%+v inspect=%+v shell=%+v",
			statusOut.ContinuityIncidentFollowUpHistoryRollup, inspectOut.ContinuityIncidentFollowUpHistoryRollup, shellOut.ContinuityIncidentFollowUpHistoryRollup)
	}
	statusRollup := statusOut.ContinuityIncidentFollowUpHistoryRollup
	inspectRollup := inspectOut.ContinuityIncidentFollowUpHistoryRollup
	shellRollup := shellOut.ContinuityIncidentFollowUpHistoryRollup
	if statusRollup.AnchorsWithOpenFollowUp != 1 || statusRollup.OpenAnchorsBehindLatestTransition != 1 || statusRollup.ReceiptsRecordedPending != 1 {
		t.Fatalf("unexpected status follow-up-history rollup: %+v", statusRollup)
	}
	if inspectRollup.AnchorsWithOpenFollowUp != statusRollup.AnchorsWithOpenFollowUp ||
		inspectRollup.OpenAnchorsBehindLatestTransition != statusRollup.OpenAnchorsBehindLatestTransition ||
		inspectRollup.ReceiptsRecordedPending != statusRollup.ReceiptsRecordedPending {
		t.Fatalf("expected inspect follow-up-history rollup to match status, got status=%+v inspect=%+v", statusRollup, inspectRollup)
	}
	if shellRollup.AnchorsWithOpenFollowUp != statusRollup.AnchorsWithOpenFollowUp ||
		shellRollup.OpenAnchorsBehindLatestTransition != statusRollup.OpenAnchorsBehindLatestTransition ||
		shellRollup.ReceiptsRecordedPending != statusRollup.ReceiptsRecordedPending {
		t.Fatalf("expected shell follow-up-history rollup to match status, got status=%+v shell=%+v", statusRollup, shellRollup)
	}
	if statusOut.LatestContinuityIncidentFollowUpReceipt == nil || inspectOut.LatestContinuityIncidentFollowUpReceipt == nil || shellOut.LatestContinuityIncidentFollowUpReceipt == nil {
		t.Fatalf("expected latest follow-up receipt across status/inspect/shell, got status=%+v inspect=%+v shell=%+v",
			statusOut.LatestContinuityIncidentFollowUpReceipt, inspectOut.LatestContinuityIncidentFollowUpReceipt, shellOut.LatestContinuityIncidentFollowUpReceipt)
	}
}

func TestFollowUpAwareAdvisoryTriagedNoFollowUpConsistencyAndNoAuthorityOverride(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	baseline, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("baseline status task: %v", err)
	}

	base := time.Unix(1719003000, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_followup_no_receipt",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffResolution,
		HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
		HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
		CreatedAt:               base,
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}
	if err := store.IncidentTriages().Create(incidenttriage.Receipt{
		Version:                   1,
		ReceiptID:                 "citr_followup_no_receipt",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_no_receipt",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		Posture:                   incidenttriage.PostureTriaged,
		FollowUpPosture:           incidenttriage.FollowUpPostureNone,
		Summary:                   "triaged latest anchor in bounded incident workflow",
		CreatedAt:                 base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create triage receipt: %v", err)
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

	for _, item := range []struct {
		label    string
		follow   *ContinuityIncidentFollowUpSummary
		rollup   *ContinuityIncidentFollowUpHistoryRollupSummary
		guidance string
		reason   string
	}{
		{
			label:    "status",
			follow:   statusOut.ContinuityIncidentFollowUp,
			rollup:   statusOut.ContinuityIncidentFollowUpHistoryRollup,
			guidance: nonNilOperatorDecisionGuidance(statusOut.OperatorDecision),
			reason:   nonNilOperatorPlanReason(statusOut.OperatorExecutionPlan),
		},
		{
			label:    "inspect",
			follow:   inspectOut.ContinuityIncidentFollowUp,
			rollup:   inspectOut.ContinuityIncidentFollowUpHistoryRollup,
			guidance: nonNilOperatorDecisionGuidance(inspectOut.OperatorDecision),
			reason:   nonNilOperatorPlanReason(inspectOut.OperatorExecutionPlan),
		},
		{
			label:    "shell",
			follow:   shellOut.ContinuityIncidentFollowUp,
			rollup:   shellOut.ContinuityIncidentFollowUpHistoryRollup,
			guidance: nonNilShellDecisionGuidance(shellOut.OperatorDecision),
			reason:   nonNilShellPlanReason(shellOut.OperatorExecutionPlan),
		},
	} {
		if item.follow == nil {
			t.Fatalf("%s: expected continuity follow-up summary", item.label)
		}
		if item.follow.Digest != "triaged without follow-up" {
			t.Fatalf("%s: expected harmonized follow-up digest, got %q", item.label, item.follow.Digest)
		}
		if !strings.Contains(item.follow.Advisory, "no follow-up receipt is recorded yet") {
			t.Fatalf("%s: expected triaged-without-follow-up advisory cue, got %q", item.label, item.follow.Advisory)
		}
		if item.rollup == nil || item.rollup.AnchorsTriagedWithoutFollowUp != 1 {
			t.Fatalf("%s: expected triaged-without-follow-up rollup count, got %+v", item.label, item.rollup)
		}
		if !strings.Contains(item.guidance, "no follow-up receipt is recorded yet") {
			t.Fatalf("%s: expected operator guidance to include no-follow-up advisory, got %q", item.label, item.guidance)
		}
		if !strings.Contains(item.reason, "no follow-up receipt is recorded yet") {
			t.Fatalf("%s: expected operator plan reason to include no-follow-up advisory, got %q", item.label, item.reason)
		}
	}

	if statusOut.RequiredNextOperatorAction != baseline.RequiredNextOperatorAction {
		t.Fatalf("expected advisory-only behavior with no authority override, baseline=%s after=%s", baseline.RequiredNextOperatorAction, statusOut.RequiredNextOperatorAction)
	}
}

func TestFollowUpAwareAdvisoryReopenedConsistency(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1719003200, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_followup_reopened",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		CreatedAt:               base,
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}
	if err := store.IncidentTriages().Create(incidenttriage.Receipt{
		Version:                   1,
		ReceiptID:                 "citr_followup_reopened",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_reopened",
		AnchorTransitionKind:      transition.KindHandoffLaunch,
		Posture:                   incidenttriage.PostureNeedsFollowUp,
		FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
		CreatedAt:                 base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create triage receipt: %v", err)
	}
	if err := store.IncidentFollowUps().Create(incidenttriage.FollowUpReceipt{
		Version:                   1,
		ReceiptID:                 "cifr_followup_reopened",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_reopened",
		AnchorTransitionKind:      transition.KindHandoffLaunch,
		TriageReceiptID:           "citr_followup_reopened",
		TriagePosture:             incidenttriage.PostureNeedsFollowUp,
		TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
		ActionKind:                incidenttriage.FollowUpActionReopened,
		Summary:                   "reopened for additional bounded review",
		CreatedAt:                 base.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("create follow-up receipt: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if statusOut.ContinuityIncidentFollowUp == nil || statusOut.ContinuityIncidentFollowUp.State != ContinuityIncidentFollowUpReopened {
		t.Fatalf("expected reopened follow-up state, got %+v", statusOut.ContinuityIncidentFollowUp)
	}
	if statusOut.ContinuityIncidentFollowUp.Digest != "follow-up reopened" {
		t.Fatalf("expected reopened follow-up digest, got %q", statusOut.ContinuityIncidentFollowUp.Digest)
	}
	if !strings.Contains(statusOut.ContinuityIncidentFollowUp.Advisory, "REOPENED; follow-up remains explicitly open") {
		t.Fatalf("expected reopened advisory wording, got %q", statusOut.ContinuityIncidentFollowUp.Advisory)
	}
	if !strings.Contains(nonNilOperatorDecisionGuidance(statusOut.OperatorDecision), "REOPENED; follow-up remains explicitly open") {
		t.Fatalf("expected operator guidance to reflect reopened posture, got %q", nonNilOperatorDecisionGuidance(statusOut.OperatorDecision))
	}
}

func TestFollowUpAwareAdvisoryClosedRemainsConservative(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	baseline, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("baseline status task: %v", err)
	}

	base := time.Unix(1719003400, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_followup_closed",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffResolution,
		HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
		HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
		CreatedAt:               base,
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}
	if err := store.IncidentTriages().Create(incidenttriage.Receipt{
		Version:                   1,
		ReceiptID:                 "citr_followup_closed",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_closed",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		Posture:                   incidenttriage.PostureNeedsFollowUp,
		FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
		CreatedAt:                 base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create triage receipt: %v", err)
	}
	if err := store.IncidentFollowUps().Create(incidenttriage.FollowUpReceipt{
		Version:                   1,
		ReceiptID:                 "cifr_followup_closed",
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: "ctr_followup_closed",
		AnchorTransitionKind:      transition.KindHandoffResolution,
		TriageReceiptID:           "citr_followup_closed",
		TriagePosture:             incidenttriage.PostureNeedsFollowUp,
		TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
		ActionKind:                incidenttriage.FollowUpActionClosed,
		Summary:                   "closure marker recorded",
		CreatedAt:                 base.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("create follow-up receipt: %v", err)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if statusOut.ContinuityIncidentFollowUp == nil || !statusOut.ContinuityIncidentFollowUp.FollowUpClosed {
		t.Fatalf("expected closed follow-up summary, got %+v", statusOut.ContinuityIncidentFollowUp)
	}
	if statusOut.ContinuityIncidentFollowUp.Digest != "follow-up closed (audit only)" {
		t.Fatalf("expected closed follow-up digest, got %q", statusOut.ContinuityIncidentFollowUp.Digest)
	}
	if !strings.Contains(statusOut.ContinuityIncidentFollowUp.Advisory, "does not certify correctness, completion, or resumability") {
		t.Fatalf("expected conservative closure wording, got %q", statusOut.ContinuityIncidentFollowUp.Advisory)
	}
	if strings.Contains(strings.ToLower(statusOut.ContinuityIncidentFollowUp.Advisory), "problem solved") || strings.Contains(strings.ToLower(statusOut.ContinuityIncidentFollowUp.Advisory), "incident resolved") {
		t.Fatalf("follow-up closure wording must not overclaim resolution, got %q", statusOut.ContinuityIncidentFollowUp.Advisory)
	}
	if statusOut.RequiredNextOperatorAction != baseline.RequiredNextOperatorAction {
		t.Fatalf("expected advisory-only follow-up closure with no authority override, baseline=%s after=%s", baseline.RequiredNextOperatorAction, statusOut.RequiredNextOperatorAction)
	}
}

func nonNilOperatorDecisionGuidance(in *OperatorDecisionSummary) string {
	if in == nil {
		return ""
	}
	return in.Guidance
}

func nonNilOperatorPlanReason(in *OperatorExecutionPlan) string {
	if in == nil || in.PrimaryStep == nil {
		return ""
	}
	return in.PrimaryStep.Reason
}

func nonNilShellDecisionGuidance(in *ShellOperatorDecisionSummary) string {
	if in == nil {
		return ""
	}
	return in.Guidance
}

func nonNilShellPlanReason(in *ShellOperatorExecutionPlan) string {
	if in == nil || in.PrimaryStep == nil {
		return ""
	}
	return in.PrimaryStep.Reason
}
