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

func TestReadContinuityIncidentTaskRiskPatternDerivations(t *testing.T) {
	tests := []struct {
		name          string
		seed          func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time)
		expectedClass ContinuityIncidentTaskRiskClass
	}{
		{
			name: "recurring weak closure across anchors",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_weak_a", transition.KindHandoffLaunch, base)
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_weak_b", transition.KindHandoffResolution, base.Add(time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_weak_a", "ctr_task_risk_weak_a", base.Add(2*time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_weak_b", "ctr_task_risk_weak_b", base.Add(3*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_weak_a_closed", "citr_task_risk_weak_a", "ctr_task_risk_weak_a", incidenttriage.FollowUpActionClosed, base.Add(4*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_weak_a_reopened", "citr_task_risk_weak_a", "ctr_task_risk_weak_a", incidenttriage.FollowUpActionReopened, base.Add(5*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_weak_b_closed", "citr_task_risk_weak_b", "ctr_task_risk_weak_b", incidenttriage.FollowUpActionClosed, base.Add(6*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_weak_b_reopened", "citr_task_risk_weak_b", "ctr_task_risk_weak_b", incidenttriage.FollowUpActionReopened, base.Add(7*time.Second))
			},
			expectedClass: ContinuityIncidentTaskRiskRecurringWeakClosure,
		},
		{
			name: "recurring unresolved anchors without weak-closure signals",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_unresolved_a", transition.KindHandoffLaunch, base)
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_unresolved_b", transition.KindHandoffResolution, base.Add(time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_unresolved_a", "ctr_task_risk_unresolved_a", base.Add(2*time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_unresolved_b", "ctr_task_risk_unresolved_b", base.Add(3*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_unresolved_a", "citr_task_risk_unresolved_a", "ctr_task_risk_unresolved_a", incidenttriage.FollowUpActionRecordedPending, base.Add(4*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_unresolved_b", "citr_task_risk_unresolved_b", "ctr_task_risk_unresolved_b", incidenttriage.FollowUpActionRecordedPending, base.Add(5*time.Second))
			},
			expectedClass: ContinuityIncidentTaskRiskRecurringUnresolved,
		},
		{
			name: "recurring stagnant follow-up progression across anchors",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_stagnant_a", transition.KindHandoffLaunch, base)
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_stagnant_b", transition.KindHandoffResolution, base.Add(time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_stagnant_a", "ctr_task_risk_stagnant_a", base.Add(2*time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_stagnant_b", "ctr_task_risk_stagnant_b", base.Add(3*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_stagnant_a_1", "citr_task_risk_stagnant_a", "ctr_task_risk_stagnant_a", incidenttriage.FollowUpActionRecordedPending, base.Add(4*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_stagnant_a_2", "citr_task_risk_stagnant_a", "ctr_task_risk_stagnant_a", incidenttriage.FollowUpActionProgressed, base.Add(5*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_stagnant_b_1", "citr_task_risk_stagnant_b", "ctr_task_risk_stagnant_b", incidenttriage.FollowUpActionRecordedPending, base.Add(6*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_stagnant_b_2", "citr_task_risk_stagnant_b", "ctr_task_risk_stagnant_b", incidenttriage.FollowUpActionProgressed, base.Add(7*time.Second))
			},
			expectedClass: ContinuityIncidentTaskRiskRecurringStagnantFollowUp,
		},
		{
			name: "recurring triaged without follow-up across anchors",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_triaged_a", transition.KindHandoffLaunch, base)
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_triaged_b", transition.KindHandoffResolution, base.Add(time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_triaged_a", "ctr_task_risk_triaged_a", base.Add(2*time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_triaged_b", "ctr_task_risk_triaged_b", base.Add(3*time.Second))
			},
			expectedClass: ContinuityIncidentTaskRiskRecurringTriagedNoFollowUp,
		},
		{
			name: "stable bounded posture across recent anchors",
			seed: func(t *testing.T, store *sqlite.Store, taskID common.TaskID, base time.Time) {
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_stable_a", transition.KindHandoffLaunch, base)
				mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_stable_b", transition.KindHandoffResolution, base.Add(time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_stable_a", "ctr_task_risk_stable_a", base.Add(2*time.Second))
				mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_stable_b", "ctr_task_risk_stable_b", base.Add(3*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_stable_a_closed", "citr_task_risk_stable_a", "ctr_task_risk_stable_a", incidenttriage.FollowUpActionClosed, base.Add(4*time.Second))
				mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_stable_b_closed", "citr_task_risk_stable_b", "ctr_task_risk_stable_b", incidenttriage.FollowUpActionClosed, base.Add(5*time.Second))
			},
			expectedClass: ContinuityIncidentTaskRiskStableBounded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)
			coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
			taskID := setupTaskWithBrief(t, coord)
			base := time.Unix(1720100000, 0).UTC()

			tc.seed(t, store, taskID, base)

			out, err := coord.ReadContinuityIncidentTaskRisk(context.Background(), ReadContinuityIncidentTaskRiskRequest{
				TaskID: string(taskID),
				Limit:  20,
			})
			if err != nil {
				t.Fatalf("read continuity incident task risk: %v", err)
			}
			if out.Summary == nil {
				t.Fatal("expected task-level continuity incident risk summary")
			}
			if out.Summary.Class != tc.expectedClass {
				t.Fatalf("expected task-risk class %s, got %+v", tc.expectedClass, out.Summary)
			}
			if out.Summary.WindowAdvisory == "" || !strings.Contains(strings.ToLower(out.Summary.WindowAdvisory), "bounded incident window") {
				t.Fatalf("expected bounded window advisory in task-risk summary, got %+v", out.Summary)
			}
		})
	}
}

func TestReadContinuityIncidentTaskRiskProjectionConsistencyAcrossStatusInspectShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	base := time.Unix(1720200000, 0).UTC()
	mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_projection_a", transition.KindHandoffLaunch, base)
	mustCreateTransitionReceipt(t, store, taskID, "ctr_task_risk_projection_b", transition.KindHandoffResolution, base.Add(time.Second))
	mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_projection_a", "ctr_task_risk_projection_a", base.Add(2*time.Second))
	mustCreateIncidentTriageReceipt(t, store, taskID, "citr_task_risk_projection_b", "ctr_task_risk_projection_b", base.Add(3*time.Second))
	mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_projection_a_closed", "citr_task_risk_projection_a", "ctr_task_risk_projection_a", incidenttriage.FollowUpActionClosed, base.Add(4*time.Second))
	mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_projection_a_reopened", "citr_task_risk_projection_a", "ctr_task_risk_projection_a", incidenttriage.FollowUpActionReopened, base.Add(5*time.Second))
	mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_projection_b_closed", "citr_task_risk_projection_b", "ctr_task_risk_projection_b", incidenttriage.FollowUpActionClosed, base.Add(6*time.Second))
	mustCreateIncidentFollowUpReceipt(t, store, taskID, "cifr_task_risk_projection_b_reopened", "citr_task_risk_projection_b", "ctr_task_risk_projection_b", incidenttriage.FollowUpActionReopened, base.Add(7*time.Second))

	readOut, err := coord.ReadContinuityIncidentTaskRisk(context.Background(), ReadContinuityIncidentTaskRiskRequest{
		TaskID: string(taskID),
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("read continuity incident task risk: %v", err)
	}
	if readOut.Summary == nil {
		t.Fatal("expected task-risk summary from dedicated read")
	}
	expectedClass := readOut.Summary.Class

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
		label   string
		summary *ContinuityIncidentTaskRiskSummary
	}{
		{label: "status", summary: statusOut.ContinuityIncidentTaskRisk},
		{label: "inspect", summary: inspectOut.ContinuityIncidentTaskRisk},
		{label: "shell", summary: shellOut.ContinuityIncidentTaskRisk},
	} {
		if item.summary == nil {
			t.Fatalf("%s: expected task-risk summary projection", item.label)
		}
		if item.summary.Class != expectedClass {
			t.Fatalf("%s: expected task-risk class %s, got %+v", item.label, expectedClass, item.summary)
		}
		if item.summary.Detail == "" || !strings.Contains(strings.ToLower(item.summary.Detail), "recent bounded evidence") {
			t.Fatalf("%s: expected bounded conservative detail, got %+v", item.label, item.summary)
		}
	}
}

func TestReadContinuityIncidentTaskRiskRejectsInvalidRequest(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	if _, err := coord.ReadContinuityIncidentTaskRisk(context.Background(), ReadContinuityIncidentTaskRiskRequest{
		TaskID: "",
		Limit:  5,
	}); err == nil || !strings.Contains(err.Error(), "task id is required") {
		t.Fatalf("expected task id validation error, got %v", err)
	}
}
