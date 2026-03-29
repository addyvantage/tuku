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
	shellEvents, err := coord.listShellSessionEvents(taskID, "shs_1", 10)
	if err != nil {
		t.Fatalf("list durable shell session events: %v", err)
	}
	if len(shellEvents) < 3 {
		t.Fatalf("expected at least three durable shell events, got %d", len(shellEvents))
	}
	if shellEvents[0].SessionID != "shs_1" {
		t.Fatalf("expected shell event session linkage, got %+v", shellEvents[0])
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

func TestRecordShellLifecycleSupportsReattachEvents(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RecordShellLifecycle(context.Background(), RecordShellLifecycleRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_reattach",
		Kind:                  ShellLifecycleReattachRequested,
		HostMode:              "codex-pty",
		HostState:             "live",
		WorkerSessionID:       "wks_reattach",
		WorkerSessionIDSource: "authoritative",
		Note:                  "reattach requested by operator",
		InputLive:             false,
	}); err != nil {
		t.Fatalf("record reattach-requested lifecycle: %v", err)
	}

	if _, err := coord.RecordShellLifecycle(context.Background(), RecordShellLifecycleRequest{
		TaskID:                string(taskID),
		SessionID:             "shs_reattach",
		Kind:                  ShellLifecycleReattachFailed,
		HostMode:              "codex-pty",
		HostState:             "failed",
		WorkerSessionID:       "wks_reattach",
		WorkerSessionIDSource: "authoritative",
		Note:                  "reattach failed because host rejected resume in this runtime",
		InputLive:             false,
	}); err != nil {
		t.Fatalf("record reattach-failed lifecycle: %v", err)
	}

	events, err := store.Proofs().ListByTask(taskID, 50)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventShellReattachRequested) {
		t.Fatal("expected reattach-requested proof event")
	}
	if !hasEvent(events, proof.EventShellReattachFailed) {
		t.Fatal("expected reattach-failed proof event")
	}

	shellEvents, err := coord.listShellSessionEvents(taskID, "shs_reattach", 10)
	if err != nil {
		t.Fatalf("list shell events: %v", err)
	}
	if len(shellEvents) == 0 {
		t.Fatal("expected durable shell session events for reattach lifecycle")
	}
	if shellEvents[0].Kind != "reattach_failed" {
		t.Fatalf("expected latest shell event kind reattach_failed, got %s", shellEvents[0].Kind)
	}
}
