package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/transition"
)

func TestIncidentTriageReceiptPersistenceOrderingAndReopenDurability(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "incident-triage-receipts.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.IncidentTriages()
	taskID := common.TaskID("tsk_incident_triage")
	base := time.Unix(1714200000, 0).UTC()
	records := []incidenttriage.Receipt{
		{
			Version:                   1,
			ReceiptID:                 "citr_001",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeLatestTransition,
			AnchorTransitionReceiptID: "ctr_001",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			Posture:                   incidenttriage.PostureTriaged,
			FollowUpPosture:           incidenttriage.FollowUpPostureNone,
			Summary:                   "triaged launch incident",
			CreatedAt:                 base,
		},
		{
			Version:                   1,
			ReceiptID:                 "citr_002",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_001",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			Posture:                   incidenttriage.PostureNeedsFollowUp,
			FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
			Summary:                   "needs follow-up for launch incident",
			CreatedAt:                 base.Add(5 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "citr_003",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_002",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			Posture:                   incidenttriage.PostureDeferred,
			FollowUpPosture:           incidenttriage.FollowUpPostureDeferred,
			Summary:                   "deferred resolution incident",
			CreatedAt:                 base.Add(5 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create incident triage receipt %s: %v", record.ReceiptID, err)
		}
	}

	latest, err := repo.LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest incident triage receipt: %v", err)
	}
	if latest.ReceiptID != "citr_003" {
		t.Fatalf("expected latest incident triage receipt citr_003, got %+v", latest)
	}

	latestByAnchor, err := repo.LatestByTaskAnchor(taskID, "ctr_001")
	if err != nil {
		t.Fatalf("latest incident triage receipt by anchor: %v", err)
	}
	if latestByAnchor.ReceiptID != "citr_002" {
		t.Fatalf("expected latest by anchor citr_002, got %+v", latestByAnchor)
	}

	page, err := repo.ListByTask(taskID, 2)
	if err != nil {
		t.Fatalf("list incident triage receipt page: %v", err)
	}
	if len(page) != 2 || page[0].ReceiptID != "citr_003" || page[1].ReceiptID != "citr_002" {
		t.Fatalf("unexpected incident triage receipt ordering page: %+v", page)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	reopenedLatest, err := reopened.IncidentTriages().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest incident triage receipt after reopen: %v", err)
	}
	if reopenedLatest.ReceiptID != "citr_003" || reopenedLatest.CreatedAt.IsZero() {
		t.Fatalf("unexpected durable latest incident triage receipt after reopen: %+v", reopenedLatest)
	}
	reopenedHistory, err := reopened.IncidentTriages().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list incident triage receipts after reopen: %v", err)
	}
	if len(reopenedHistory) != 3 || reopenedHistory[0].ReceiptID != "citr_003" || reopenedHistory[1].ReceiptID != "citr_002" || reopenedHistory[2].ReceiptID != "citr_001" {
		t.Fatalf("unexpected durable incident triage history after reopen: %+v", reopenedHistory)
	}
}

func TestIncidentTriageReceiptFilteredReadAndPagination(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "incident-triage-receipts-filtered.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	repo := store.IncidentTriages()
	taskID := common.TaskID("tsk_incident_triage_filter")
	base := time.Unix(1714300000, 0).UTC()
	records := []incidenttriage.Receipt{
		{
			Version:                   1,
			ReceiptID:                 "citr_f_300",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_a",
			Posture:                   incidenttriage.PostureNeedsFollowUp,
			FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
			CreatedAt:                 base.Add(3 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "citr_f_290",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_a",
			Posture:                   incidenttriage.PostureNeedsFollowUp,
			FollowUpPosture:           incidenttriage.FollowUpPostureAdvisory,
			CreatedAt:                 base.Add(2 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "citr_f_280",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_b",
			Posture:                   incidenttriage.PostureDeferred,
			FollowUpPosture:           incidenttriage.FollowUpPostureDeferred,
			CreatedAt:                 base.Add(1 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "citr_f_270",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_a",
			Posture:                   incidenttriage.PostureTriaged,
			FollowUpPosture:           incidenttriage.FollowUpPostureNone,
			CreatedAt:                 base.Add(1 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create incident triage receipt %s: %v", record.ReceiptID, err)
		}
	}

	gotByID, err := repo.GetByTaskReceipt(taskID, "citr_f_280")
	if err != nil {
		t.Fatalf("get incident triage receipt by id: %v", err)
	}
	if gotByID.AnchorTransitionReceiptID != "ctr_b" || gotByID.Posture != incidenttriage.PostureDeferred {
		t.Fatalf("unexpected by-id triage receipt: %+v", gotByID)
	}

	filtered, err := repo.ListByTaskFiltered(taskID, incidenttriage.ReceiptListFilter{
		Limit:                     10,
		AnchorTransitionReceiptID: "ctr_a",
		Posture:                   incidenttriage.PostureNeedsFollowUp,
	})
	if err != nil {
		t.Fatalf("list filtered incident triage receipts: %v", err)
	}
	if len(filtered) != 2 || filtered[0].ReceiptID != "citr_f_300" || filtered[1].ReceiptID != "citr_f_290" {
		t.Fatalf("unexpected filtered incident triage receipts: %+v", filtered)
	}

	paged, err := repo.ListByTaskFiltered(taskID, incidenttriage.ReceiptListFilter{
		Limit:           2,
		BeforeReceiptID: "citr_f_290",
		BeforeCreatedAt: base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("list paged incident triage receipts: %v", err)
	}
	if len(paged) != 2 || paged[0].ReceiptID != "citr_f_280" || paged[1].ReceiptID != "citr_f_270" {
		t.Fatalf("unexpected paged incident triage receipts: %+v", paged)
	}
}
