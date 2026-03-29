package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/incidenttriage"
	"tuku/internal/domain/transition"
)

func TestIncidentFollowUpReceiptPersistenceOrderingAndReopenDurability(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "incident-follow-up-receipts.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.IncidentFollowUps()
	taskID := common.TaskID("tsk_incident_follow_up")
	base := time.Unix(1715200000, 0).UTC()
	records := []incidenttriage.FollowUpReceipt{
		{
			Version:                   1,
			ReceiptID:                 "cifr_001",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeLatestTransition,
			AnchorTransitionReceiptID: "ctr_001",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			TriageReceiptID:           "citr_001",
			TriagePosture:             incidenttriage.PostureNeedsFollowUp,
			TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
			ActionKind:                incidenttriage.FollowUpActionRecordedPending,
			Summary:                   "follow-up recorded",
			CreatedAt:                 base,
		},
		{
			Version:                   1,
			ReceiptID:                 "cifr_002",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_001",
			AnchorTransitionKind:      transition.KindHandoffLaunch,
			TriageReceiptID:           "citr_001",
			TriagePosture:             incidenttriage.PostureNeedsFollowUp,
			TriageFollowUpState:       incidenttriage.FollowUpPostureAdvisory,
			ActionKind:                incidenttriage.FollowUpActionProgressed,
			Summary:                   "follow-up progressed",
			CreatedAt:                 base.Add(5 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "cifr_003",
			TaskID:                    taskID,
			AnchorMode:                incidenttriage.AnchorModeTransitionID,
			AnchorTransitionReceiptID: "ctr_002",
			AnchorTransitionKind:      transition.KindHandoffResolution,
			TriageReceiptID:           "citr_002",
			TriagePosture:             incidenttriage.PostureDeferred,
			TriageFollowUpState:       incidenttriage.FollowUpPostureDeferred,
			ActionKind:                incidenttriage.FollowUpActionClosed,
			Summary:                   "follow-up closed",
			CreatedAt:                 base.Add(5 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create incident follow-up receipt %s: %v", record.ReceiptID, err)
		}
	}

	latest, err := repo.LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest incident follow-up receipt: %v", err)
	}
	if latest.ReceiptID != "cifr_003" {
		t.Fatalf("expected latest incident follow-up receipt cifr_003, got %+v", latest)
	}

	latestByAnchor, err := repo.LatestByTaskAnchor(taskID, "ctr_001")
	if err != nil {
		t.Fatalf("latest incident follow-up receipt by anchor: %v", err)
	}
	if latestByAnchor.ReceiptID != "cifr_002" {
		t.Fatalf("expected latest by anchor cifr_002, got %+v", latestByAnchor)
	}

	page, err := repo.ListByTask(taskID, 2)
	if err != nil {
		t.Fatalf("list incident follow-up receipt page: %v", err)
	}
	if len(page) != 2 || page[0].ReceiptID != "cifr_003" || page[1].ReceiptID != "cifr_002" {
		t.Fatalf("unexpected incident follow-up receipt ordering page: %+v", page)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	reopenedLatest, err := reopened.IncidentFollowUps().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest incident follow-up receipt after reopen: %v", err)
	}
	if reopenedLatest.ReceiptID != "cifr_003" || reopenedLatest.CreatedAt.IsZero() {
		t.Fatalf("unexpected durable latest incident follow-up receipt after reopen: %+v", reopenedLatest)
	}
	reopenedHistory, err := reopened.IncidentFollowUps().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list incident follow-up receipts after reopen: %v", err)
	}
	if len(reopenedHistory) != 3 || reopenedHistory[0].ReceiptID != "cifr_003" || reopenedHistory[1].ReceiptID != "cifr_002" || reopenedHistory[2].ReceiptID != "cifr_001" {
		t.Fatalf("unexpected durable incident follow-up history after reopen: %+v", reopenedHistory)
	}
}

func TestIncidentFollowUpReceiptFilteredReadAndPagination(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "incident-follow-up-receipts-filtered.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	repo := store.IncidentFollowUps()
	taskID := common.TaskID("tsk_incident_follow_up_filter")
	base := time.Unix(1715300000, 0).UTC()
	records := []incidenttriage.FollowUpReceipt{
		{
			Version:                   1,
			ReceiptID:                 "cifr_f_300",
			TaskID:                    taskID,
			AnchorTransitionReceiptID: "ctr_a",
			TriageReceiptID:           "citr_a_2",
			ActionKind:                incidenttriage.FollowUpActionProgressed,
			CreatedAt:                 base.Add(3 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "cifr_f_290",
			TaskID:                    taskID,
			AnchorTransitionReceiptID: "ctr_a",
			TriageReceiptID:           "citr_a_1",
			ActionKind:                incidenttriage.FollowUpActionRecordedPending,
			CreatedAt:                 base.Add(2 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "cifr_f_280",
			TaskID:                    taskID,
			AnchorTransitionReceiptID: "ctr_b",
			TriageReceiptID:           "citr_b_1",
			ActionKind:                incidenttriage.FollowUpActionClosed,
			CreatedAt:                 base.Add(1 * time.Second),
		},
		{
			Version:                   1,
			ReceiptID:                 "cifr_f_270",
			TaskID:                    taskID,
			AnchorTransitionReceiptID: "ctr_a",
			TriageReceiptID:           "citr_a_1",
			ActionKind:                incidenttriage.FollowUpActionReopened,
			CreatedAt:                 base.Add(1 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create incident follow-up receipt %s: %v", record.ReceiptID, err)
		}
	}

	gotByID, err := repo.GetByTaskReceipt(taskID, "cifr_f_280")
	if err != nil {
		t.Fatalf("get incident follow-up receipt by id: %v", err)
	}
	if gotByID.AnchorTransitionReceiptID != "ctr_b" || gotByID.ActionKind != incidenttriage.FollowUpActionClosed {
		t.Fatalf("unexpected by-id follow-up receipt: %+v", gotByID)
	}

	filtered, err := repo.ListByTaskFiltered(taskID, incidenttriage.FollowUpReceiptListFilter{
		Limit:                     10,
		AnchorTransitionReceiptID: "ctr_a",
		ActionKind:                incidenttriage.FollowUpActionProgressed,
	})
	if err != nil {
		t.Fatalf("list filtered incident follow-up receipts: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ReceiptID != "cifr_f_300" {
		t.Fatalf("unexpected filtered incident follow-up receipts: %+v", filtered)
	}

	paged, err := repo.ListByTaskFiltered(taskID, incidenttriage.FollowUpReceiptListFilter{
		Limit:           2,
		BeforeReceiptID: "cifr_f_290",
		BeforeCreatedAt: base.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("list paged incident follow-up receipts: %v", err)
	}
	if len(paged) != 2 || paged[0].ReceiptID != "cifr_f_280" || paged[1].ReceiptID != "cifr_f_270" {
		t.Fatalf("unexpected paged incident follow-up receipts: %+v", paged)
	}
}
