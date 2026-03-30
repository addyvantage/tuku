package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/transition"
	"tuku/internal/storage/sqlite"
)

func TestReadContinuityIncidentClosureReopenedPatternAndProjectionConsistency(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1720000000, 0).UTC()
	mustCreateTransitionReceipt(t, store, taskID, "ctr_closure_reopened", transition.KindHandoffLaunch, base)
	mustCreateIncidentTriageReceipt(t, store, taskID, "citr_closure_reopened", "ctr_closure_reopened", base.Add(time.Second))
	mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_closure_closed", "citr_closure_reopened", "ctr_closure_reopened", incidenttriage.FollowUpActionClosed, base.Add(2*time.Second))
	mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_closure_reopened", "citr_closure_reopened", "ctr_closure_reopened", incidenttriage.FollowUpActionReopened, base.Add(3*time.Second))

	readOut, err := coord.ReadContinuityIncidentClosure(context.Background(), ReadContinuityIncidentClosureRequest{
		TaskID: string(taskID),
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("read continuity incident closure: %v", err)
	}
	if readOut.Closure == nil {
		t.Fatal("expected closure intelligence summary")
	}
	if readOut.Closure.Class != ContinuityIncidentClosureWeakReopened {
		t.Fatalf("expected reopened closure class, got %+v", readOut.Closure)
	}
	if !readOut.Closure.OperationallyUnresolved || !readOut.Closure.ClosureAppearsWeak || !readOut.Closure.ReopenedAfterClosure {
		t.Fatalf("expected unresolved reopened-closure signals, got %+v", readOut.Closure)
	}
	if len(readOut.Closure.RecentAnchors) == 0 || readOut.Closure.RecentAnchors[0].Class != ContinuityIncidentClosureWeakReopened {
		t.Fatalf("expected bounded per-anchor closure timeline, got %+v", readOut.Closure.RecentAnchors)
	}
	if !strings.Contains(strings.ToLower(readOut.Closure.RecentAnchors[0].Explanation), "reopened after closure") {
		t.Fatalf("expected conservative reopened explanation, got %+v", readOut.Closure.RecentAnchors[0])
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
		t.Fatalf("shell snapshot: %v", err)
	}

	for _, item := range []struct {
		label  string
		follow *ContinuityIncidentFollowUpSummary
	}{
		{label: "status", follow: statusOut.ContinuityIncidentFollowUp},
		{label: "inspect", follow: inspectOut.ContinuityIncidentFollowUp},
		{label: "shell", follow: shellOut.ContinuityIncidentFollowUp},
	} {
		if item.follow == nil || item.follow.ClosureIntelligence == nil {
			t.Fatalf("%s: expected closure intelligence in follow-up projection, got %+v", item.label, item.follow)
		}
		if item.follow.ClosureIntelligence.Class != ContinuityIncidentClosureWeakReopened {
			t.Fatalf("%s: expected reopened closure class, got %+v", item.label, item.follow.ClosureIntelligence)
		}
		if len(item.follow.ClosureIntelligence.RecentAnchors) == 0 {
			t.Fatalf("%s: expected closure anchor timeline in follow-up projection, got %+v", item.label, item.follow.ClosureIntelligence)
		}
		if !strings.Contains(strings.ToLower(item.follow.Advisory), "operationally unresolved") {
			t.Fatalf("%s: expected bounded unresolved advisory cue, got %q", item.label, item.follow.Advisory)
		}
	}
}

func TestReadContinuityIncidentClosurePatternDerivations(t *testing.T) {
	tests := []struct {
		name          string
		seed          func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time)
		expectedClass ContinuityIncidentClosureClass
		expectRecent  bool
	}{
		{
			name: "repeated reopen loop",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_loop", transition.KindHandoffResolution, base)
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_loop", "ctr_loop", base.Add(time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_loop_closed_1", "citr_loop", "ctr_loop", incidenttriage.FollowUpActionClosed, base.Add(2*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_loop_reopened_1", "citr_loop", "ctr_loop", incidenttriage.FollowUpActionReopened, base.Add(3*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_loop_closed_2", "citr_loop", "ctr_loop", incidenttriage.FollowUpActionClosed, base.Add(4*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_loop_reopened_2", "citr_loop", "ctr_loop", incidenttriage.FollowUpActionReopened, base.Add(5*time.Second))
			},
			expectedClass: ContinuityIncidentClosureWeakLoop,
			expectRecent:  true,
		},
		{
			name: "stagnant progression without closure",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_stagnant", transition.KindHandoffLaunch, base)
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_stagnant", "ctr_stagnant", base.Add(time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_stagnant_1", "citr_stagnant", "ctr_stagnant", incidenttriage.FollowUpActionRecordedPending, base.Add(2*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_stagnant_2", "citr_stagnant", "ctr_stagnant", incidenttriage.FollowUpActionProgressed, base.Add(3*time.Second))
			},
			expectedClass: ContinuityIncidentClosureWeakStagnant,
			expectRecent:  true,
		},
		{
			name: "triaged without follow-up is operationally unresolved",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_no_followup", transition.KindHandoffLaunch, base)
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_no_followup", "ctr_no_followup", base.Add(time.Second))
			},
			expectedClass: ContinuityIncidentClosureOperationallyUnresolved,
			expectRecent:  false,
		},
		{
			name: "closed anchor can remain stable within bounded evidence",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_stable", transition.KindHandoffResolution, base)
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_stable", "ctr_stable", base.Add(time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_stable_closed", "citr_stable", "ctr_stable", incidenttriage.FollowUpActionClosed, base.Add(2*time.Second))
			},
			expectedClass: ContinuityIncidentClosureStableBounded,
			expectRecent:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
			taskID := setupTaskWithBrief(t, coord)
			base := time.Unix(1720002000, 0).UTC()

			tc.seed(t, store, taskID, base)

			out, err := coord.ReadContinuityIncidentClosure(context.Background(), ReadContinuityIncidentClosureRequest{
				TaskID: string(taskID),
				Limit:  20,
			})
			if err != nil {
				t.Fatalf("read continuity incident closure: %v", err)
			}
			if out.Closure == nil {
				t.Fatal("expected closure intelligence summary")
			}
			if out.Closure.Class != tc.expectedClass {
				t.Fatalf("expected closure class %s, got %+v", tc.expectedClass, out.Closure)
			}
			if tc.expectRecent && len(out.Closure.RecentAnchors) == 0 {
				t.Fatalf("expected bounded recent-anchor closure timeline, got %+v", out.Closure)
			}
			if !tc.expectRecent && len(out.Closure.RecentAnchors) > 0 {
				t.Fatalf("did not expect per-anchor timeline items for this case, got %+v", out.Closure.RecentAnchors)
			}
		})
	}
}

func TestReadContinuityIncidentClosureRejectsInvalidRequest(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	if _, err := coord.ReadContinuityIncidentClosure(context.Background(), ReadContinuityIncidentClosureRequest{
		TaskID: "",
		Limit:  5,
	}); err == nil || !strings.Contains(err.Error(), "task id is required") {
		t.Fatalf("expected task id validation error, got %v", err)
	}
}

func mustCreateTransitionReceipt(t *testing.T, store *sqlite.Store, taskID common.TaskID, receiptID string, kind transition.Kind, at time.Time) {
	t.Helper()
	if err := store.TransitionReceipts().Create(transition.Receipt{
		Version:        1,
		ReceiptID:      common.EventID(receiptID),
		TaskID:         taskID,
		TransitionKind: kind,
		CreatedAt:      at,
	}); err != nil {
		t.Fatalf("create transition receipt: %v", err)
	}
}

func mustCreateIncidentTriageReceipt(t *testing.T, store *sqlite.Store, taskID common.TaskID, receiptID, anchorID string, at time.Time) {
	t.Helper()
	if err := store.IncidentTriages().Create(incidenttriage.Receipt{
		Version:                   1,
		ReceiptID:                 common.EventID(receiptID),
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: common.EventID(anchorID),
		Posture:                   incidenttriage.PostureNeedsFollowUp,
		FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
		CreatedAt:                 at,
	}); err != nil {
		t.Fatalf("create incident triage receipt: %v", err)
	}
}

func mustCreateIncidentFollowUpReceipt(t *testing.T, store *sqlite.Store, taskID common.TaskID, receiptID, triageID, anchorID string, kind incidenttriage.FollowUpActionKind, at time.Time) {
	t.Helper()
	if err := store.IncidentFollowUps().Create(incidenttriage.FollowUpReceipt{
		Version:                   1,
		ReceiptID:                 common.EventID(receiptID),
		TaskID:                    taskID,
		AnchorMode:                incidenttriage.AnchorModeTransitionID,
		AnchorTransitionReceiptID: common.EventID(anchorID),
		TriageReceiptID:           common.EventID(triageID),
		TriagePosture:             incidenttriage.PostureNeedsFollowUp,
		TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
		ActionKind:                kind,
		CreatedAt:                 at,
	}); err != nil {
		t.Fatalf("create incident follow-up receipt: %v", err)
	}
}
