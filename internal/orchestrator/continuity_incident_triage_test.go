package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/transition"
)

func TestRecordContinuityIncidentTriagePersistsProofAndSurfacesAcrossReadModels(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	anchorAt := time.Unix(1717000000, 0).UTC()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_triage_1",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		BranchClassBefore:       string(ActiveBranchClassLocal),
		BranchClassAfter:        string(ActiveBranchClassHandoffClaude),
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		ReviewGapPresent:        true,
		ReviewPosture:           transition.ReviewPostureGlobalReviewStale,
		AcknowledgmentPresent:   false,
		Summary:                 "launch transition recorded under stale review posture",
		CreatedAt:               anchorAt,
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}

	out, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:     string(taskID),
		AnchorMode: "latest",
		Posture:    "triaged",
		Summary:    "reviewed bounded incident evidence",
	})
	if err != nil {
		t.Fatalf("record continuity incident triage: %v", err)
	}
	if out.Reused {
		t.Fatalf("expected first triage write to create new receipt, got reused result: %+v", out)
	}
	if out.Receipt.ReceiptID == "" || out.Receipt.AnchorTransitionReceiptID != "ctr_triage_1" {
		t.Fatalf("unexpected triage receipt summary: %+v", out.Receipt)
	}
	if out.FollowUp == nil || out.FollowUp.State == "" {
		t.Fatalf("expected follow-up projection in triage write result, got %+v", out.FollowUp)
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if statusOut.LatestContinuityIncidentTriageReceipt == nil || statusOut.LatestContinuityIncidentTriageReceipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected status triage projection, got %+v", statusOut.LatestContinuityIncidentTriageReceipt)
	}
	if statusOut.ContinuityIncidentFollowUp == nil {
		t.Fatalf("expected status incident follow-up projection")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestContinuityIncidentTriageReceipt == nil || inspectOut.LatestContinuityIncidentTriageReceipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected inspect triage projection, got %+v", inspectOut.LatestContinuityIncidentTriageReceipt)
	}
	if inspectOut.ContinuityIncidentFollowUp == nil {
		t.Fatalf("expected inspect incident follow-up projection")
	}

	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}
	if shellOut.LatestContinuityIncidentTriageReceipt == nil || shellOut.LatestContinuityIncidentTriageReceipt.ReceiptID != out.Receipt.ReceiptID {
		t.Fatalf("expected shell triage projection, got %+v", shellOut.LatestContinuityIncidentTriageReceipt)
	}
	if shellOut.ContinuityIncidentFollowUp == nil {
		t.Fatalf("expected shell incident follow-up projection")
	}

	events, err := store.Proofs().ListByTask(taskID, 50)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventContinuityIncidentTriaged) {
		t.Fatalf("expected continuity incident triaged proof event, got %+v", events)
	}
}

func TestRecordContinuityIncidentTriageRejectsInvalidTaskAnchorAndPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:  "tsk_missing",
		Posture: "triaged",
	}); err == nil {
		t.Fatal("expected missing task validation error")
	}

	if _, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:  string(taskID),
		Posture: "triaged",
	}); err == nil || !strings.Contains(err.Error(), "no continuity transition receipt exists") {
		t.Fatalf("expected missing anchor transition validation error, got %v", err)
	}

	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_triage_validate",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		CreatedAt:               time.Unix(1717000100, 0).UTC(),
	}); err != nil {
		t.Fatalf("create transition receipt for validation test: %v", err)
	}
	if _, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:  string(taskID),
		Posture: "not-a-valid-posture",
	}); err == nil || !strings.Contains(err.Error(), "invalid posture") {
		t.Fatalf("expected invalid posture validation error, got %v", err)
	}
}

func TestRecordContinuityIncidentTriageReusesEquivalentReplayAndPreservesDistinctUpdates(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:                 1,
		ReceiptID:               "ctr_triage_replay",
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		CreatedAt:               time.Unix(1717000200, 0).UTC(),
	}); err != nil {
		t.Fatalf("create transition receipt for replay test: %v", err)
	}

	first, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:  string(taskID),
		Posture: "triaged",
		Summary: "same replay payload",
	})
	if err != nil {
		t.Fatalf("first triage write: %v", err)
	}
	second, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:  string(taskID),
		Posture: "triaged",
		Summary: "same replay payload",
	})
	if err != nil {
		t.Fatalf("second triage write replay: %v", err)
	}
	if !second.Reused || second.Receipt.ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("expected deterministic replay reuse, first=%+v second=%+v", first, second)
	}
	items, err := store.IncidentTriages().ListByTask(taskID, 20)
	if err != nil {
		t.Fatalf("list incident triages after replay: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one triage receipt after replay reuse, got %d", len(items))
	}

	third, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:  string(taskID),
		Posture: "needs_follow_up",
		Summary: "changed posture should produce a distinct receipt",
	})
	if err != nil {
		t.Fatalf("third triage write distinct posture: %v", err)
	}
	if third.Reused {
		t.Fatalf("expected distinct posture to create new receipt, got %+v", third)
	}
	if third.Receipt.ReceiptID == first.Receipt.ReceiptID {
		t.Fatalf("expected distinct receipt id for posture change, got same id %s", third.Receipt.ReceiptID)
	}
}

func TestContinuityIncidentFollowUpConsistencyAcrossStatusInspectAndShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1717000300, 0).UTC()
	older := transition.Receipt{
		Version:                 1,
		ReceiptID:               common.EventID("ctr_triage_old"),
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffLaunch,
		HandoffContinuityBefore: string(HandoffContinuityStateAcceptedNotLaunched),
		HandoffContinuityAfter:  string(HandoffContinuityStateLaunchCompletedAckSeen),
		CreatedAt:               base.Add(1 * time.Second),
	}
	newer := transition.Receipt{
		Version:                 1,
		ReceiptID:               common.EventID("ctr_triage_new"),
		TaskID:                  taskID,
		TransitionKind:          transition.KindHandoffResolution,
		HandoffContinuityBefore: string(HandoffContinuityStateFollowThroughStalled),
		HandoffContinuityAfter:  string(HandoffContinuityStateResolved),
		CreatedAt:               base.Add(2 * time.Second),
	}
	if err := store.TransitionReceipts().Create(older); err != nil {
		t.Fatalf("create older transition: %v", err)
	}
	if err := store.TransitionReceipts().Create(newer); err != nil {
		t.Fatalf("create newer transition: %v", err)
	}

	if _, err := coord.RecordContinuityIncidentTriage(context.Background(), RecordContinuityIncidentTriageRequest{
		TaskID:                    string(taskID),
		AnchorMode:                "receipt",
		AnchorTransitionReceiptID: string(older.ReceiptID),
		Posture:                   "deferred",
		Summary:                   "defer older incident while newer transition remains",
	}); err != nil {
		t.Fatalf("record triage for older anchor: %v", err)
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

	if statusOut.ContinuityIncidentFollowUp == nil || inspectOut.ContinuityIncidentFollowUp == nil || shellOut.ContinuityIncidentFollowUp == nil {
		t.Fatalf("expected follow-up projection across status/inspect/shell, got status=%+v inspect=%+v shell=%+v", statusOut.ContinuityIncidentFollowUp, inspectOut.ContinuityIncidentFollowUp, shellOut.ContinuityIncidentFollowUp)
	}
	if statusOut.ContinuityIncidentFollowUp.State != ContinuityIncidentFollowUpTriageBehindLatest ||
		inspectOut.ContinuityIncidentFollowUp.State != ContinuityIncidentFollowUpTriageBehindLatest ||
		shellOut.ContinuityIncidentFollowUp.State != ContinuityIncidentFollowUpTriageBehindLatest {
		t.Fatalf("expected triage-behind-latest follow-up state consistency, got status=%+v inspect=%+v shell=%+v", statusOut.ContinuityIncidentFollowUp, inspectOut.ContinuityIncidentFollowUp, shellOut.ContinuityIncidentFollowUp)
	}
	decision := requireOperatorDecision(t, statusOut.OperatorDecision)
	if !strings.Contains(decision.Guidance, "triage the latest incident anchor") {
		t.Fatalf("expected operator guidance to include triage-behind-latest advisory, got %q", decision.Guidance)
	}
	if decision.RequiredNextAction != OperatorActionStartLocalRun {
		t.Fatalf("incident triage follow-up advisory must not override required-next action, got %+v", decision)
	}
}
