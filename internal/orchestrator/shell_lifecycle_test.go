package orchestrator

import (
	"context"
	"testing"

	"tuku/internal/domain/proof"
)

func TestRecordShellLifecyclePersistsMajorShellMilestones(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	before, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events before: %v", err)
	}

	exitCode := 11
	requests := []RecordShellLifecycleRequest{
		{
			TaskID:     string(taskID),
			SessionID:  "shs_1",
			Kind:       ShellLifecycleHostStarted,
			HostMode:   "codex-pty",
			HostState:  "live",
			InputLive:  true,
			PaneWidth:  80,
			PaneHeight: 24,
		},
		{
			TaskID:     string(taskID),
			SessionID:  "shs_1",
			Kind:       ShellLifecycleHostExited,
			HostMode:   "codex-pty",
			HostState:  "exited",
			Note:       "codex exited with code 11",
			InputLive:  false,
			ExitCode:   &exitCode,
			PaneWidth:  80,
			PaneHeight: 24,
		},
		{
			TaskID:     string(taskID),
			SessionID:  "shs_1",
			Kind:       ShellLifecycleFallback,
			HostMode:   "transcript",
			HostState:  "fallback",
			Note:       "switched to transcript fallback",
			InputLive:  false,
			PaneWidth:  80,
			PaneHeight: 24,
		},
	}

	for _, req := range requests {
		if _, err := coord.RecordShellLifecycle(context.Background(), req); err != nil {
			t.Fatalf("record shell lifecycle %+v: %v", req, err)
		}
	}

	after, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events after: %v", err)
	}
	if len(after) != len(before)+3 {
		t.Fatalf("expected exactly three additional proof events, got before=%d after=%d", len(before), len(after))
	}
	if !hasEvent(after, proof.EventShellHostStarted) {
		t.Fatal("expected shell-host-started proof event")
	}
	if !hasEvent(after, proof.EventShellHostExited) {
		t.Fatal("expected shell-host-exited proof event")
	}
	if !hasEvent(after, proof.EventShellFallbackActivated) {
		t.Fatal("expected shell-fallback proof event")
	}
}

func TestRecordShellLifecycleRequiresSessionID(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.RecordShellLifecycle(context.Background(), RecordShellLifecycleRequest{
		TaskID:    string(taskID),
		Kind:      ShellLifecycleHostStarted,
		HostMode:  "codex-pty",
		HostState: "live",
	})
	if err == nil {
		t.Fatal("expected missing session id error")
	}
}
