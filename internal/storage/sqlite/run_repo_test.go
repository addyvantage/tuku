package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/phase"
	rundomain "tuku/internal/domain/run"
)

func TestRunRepoListByTaskAndLatestUseDeterministicTieBreakAndReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "run-repo-list-latest.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.Runs()
	taskID := common.TaskID("tsk_run_repo_ordering")
	briefID := common.BriefID("brf_run_repo_ordering")
	base := time.Unix(1713700000, 0).UTC()

	records := []rundomain.ExecutionRun{
		{
			RunID:            "run_100",
			TaskID:           taskID,
			BriefID:          briefID,
			WorkerKind:       rundomain.WorkerKindCodex,
			Status:           rundomain.StatusCompleted,
			CreatedFromPhase: phase.PhaseExecuting,
			StartedAt:        base.Add(1 * time.Second),
			CreatedAt:        base.Add(1 * time.Second),
			UpdatedAt:        base.Add(1 * time.Second),
			LastKnownSummary: "run 100",
		},
		{
			RunID:            "run_200",
			TaskID:           taskID,
			BriefID:          briefID,
			WorkerKind:       rundomain.WorkerKindCodex,
			Status:           rundomain.StatusCompleted,
			CreatedFromPhase: phase.PhaseExecuting,
			StartedAt:        base.Add(2 * time.Second),
			CreatedAt:        base.Add(2 * time.Second),
			UpdatedAt:        base.Add(2 * time.Second),
			LastKnownSummary: "run 200",
		},
		{
			RunID:            "run_300",
			TaskID:           taskID,
			BriefID:          briefID,
			WorkerKind:       rundomain.WorkerKindCodex,
			Status:           rundomain.StatusFailed,
			CreatedFromPhase: phase.PhaseExecuting,
			StartedAt:        base.Add(2 * time.Second),
			CreatedAt:        base.Add(2 * time.Second),
			UpdatedAt:        base.Add(2*time.Second + 500*time.Millisecond),
			LastKnownSummary: "run 300",
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create run %s: %v", record.RunID, err)
		}
	}

	latest, err := repo.LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run by task: %v", err)
	}
	if latest.RunID != "run_300" {
		t.Fatalf("expected deterministic latest run run_300, got %+v", latest)
	}

	listed, err := repo.ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list runs by task: %v", err)
	}
	if len(listed) != 3 || listed[0].RunID != "run_300" || listed[1].RunID != "run_200" || listed[2].RunID != "run_100" {
		t.Fatalf("unexpected deterministic run ordering: %+v", listed)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	reopenedLatest, err := reopened.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest run by task after reopen: %v", err)
	}
	if reopenedLatest.RunID != "run_300" {
		t.Fatalf("unexpected durable latest run after reopen: %+v", reopenedLatest)
	}
	reopenedList, err := reopened.Runs().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list runs by task after reopen: %v", err)
	}
	if len(reopenedList) != 3 || reopenedList[0].RunID != "run_300" || reopenedList[1].RunID != "run_200" || reopenedList[2].RunID != "run_100" {
		t.Fatalf("unexpected durable run ordering after reopen: %+v", reopenedList)
	}
}
