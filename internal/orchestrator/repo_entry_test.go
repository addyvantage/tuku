package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/common"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
)

func TestResolveShellTaskForRepoReusesLatestActiveTask(t *testing.T) {
	store := newTestStore(t)
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	activeRepo := filepath.Clean("/tmp/repo-active")
	first, err := coord.StartTask(context.Background(), "older", activeRepo)
	if err != nil {
		t.Fatalf("start first task: %v", err)
	}
	second, err := coord.StartTask(context.Background(), "newer", activeRepo)
	if err != nil {
		t.Fatalf("start second task: %v", err)
	}
	caps, err := store.Capsules().Get(first.TaskID)
	if err != nil {
		t.Fatalf("get first capsule: %v", err)
	}
	caps.UpdatedAt = caps.UpdatedAt.Add(2 * time.Hour)
	if err := store.Capsules().Update(caps); err != nil {
		t.Fatalf("update first capsule: %v", err)
	}
	terminal, err := store.Capsules().Get(second.TaskID)
	if err != nil {
		t.Fatalf("get second capsule: %v", err)
	}
	terminal.Status = "COMPLETED"
	terminal.UpdatedAt = terminal.UpdatedAt.Add(3 * time.Hour)
	if err := store.Capsules().Update(terminal); err != nil {
		t.Fatalf("update second capsule: %v", err)
	}

	out, err := coord.ResolveShellTaskForRepo(context.Background(), activeRepo, "")
	if err != nil {
		t.Fatalf("resolve shell task for repo: %v", err)
	}
	if out.Created {
		t.Fatal("expected existing active task to be reused")
	}
	if out.TaskID != first.TaskID {
		t.Fatalf("expected active task %s, got %s", first.TaskID, out.TaskID)
	}
}

func TestResolveShellTaskForRepoFallsBackToMostRecentMatchingTask(t *testing.T) {
	store := newTestStore(t)
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	repoRoot := filepath.Clean("/tmp/repo-fallback")
	first, err := coord.StartTask(context.Background(), "older terminal", repoRoot)
	if err != nil {
		t.Fatalf("start first task: %v", err)
	}
	second, err := coord.StartTask(context.Background(), "newer terminal", repoRoot)
	if err != nil {
		t.Fatalf("start second task: %v", err)
	}
	for _, taskID := range []common.TaskID{first.TaskID, second.TaskID} {
		caps, err := store.Capsules().Get(taskID)
		if err != nil {
			t.Fatalf("get capsule %s: %v", taskID, err)
		}
		caps.Status = "COMPLETED"
		if taskID == second.TaskID {
			caps.UpdatedAt = caps.UpdatedAt.Add(time.Hour)
		}
		if err := store.Capsules().Update(caps); err != nil {
			t.Fatalf("update capsule %s: %v", taskID, err)
		}
	}

	out, err := coord.ResolveShellTaskForRepo(context.Background(), repoRoot, "")
	if err != nil {
		t.Fatalf("resolve shell task for repo: %v", err)
	}
	if out.Created {
		t.Fatal("expected most recent terminal task to be reused")
	}
	if out.TaskID != second.TaskID {
		t.Fatalf("expected most recent matching task %s, got %s", second.TaskID, out.TaskID)
	}
}

func TestResolveShellTaskForRepoCreatesTaskWhenRepoIsUnknown(t *testing.T) {
	store := newTestStore(t)
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorgit.NewGitProvider(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	repoRoot := filepath.Clean("/tmp/repo-new")
	out, err := coord.ResolveShellTaskForRepo(context.Background(), repoRoot, "")
	if err != nil {
		t.Fatalf("resolve shell task for new repo: %v", err)
	}
	if !out.Created {
		t.Fatal("expected a new task to be created")
	}
	caps, err := store.Capsules().Get(out.TaskID)
	if err != nil {
		t.Fatalf("get created capsule: %v", err)
	}
	if caps.Goal != DefaultRepoContinueGoal {
		t.Fatalf("expected default goal %q, got %q", DefaultRepoContinueGoal, caps.Goal)
	}
	if caps.RepoRoot != repoRoot {
		t.Fatalf("expected repo root %q, got %q", repoRoot, caps.RepoRoot)
	}
}
