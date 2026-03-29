package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type failingHost struct {
	err    error
	status HostStatus
}

func (h failingHost) Start(_ context.Context, _ Snapshot) error { return h.err }
func (h failingHost) Stop() error                               { return nil }
func (h failingHost) UpdateSnapshot(_ Snapshot)                 {}
func (h failingHost) Resize(_ int, _ int) bool                  { return false }
func (h failingHost) CanAcceptInput() bool                      { return false }
func (h failingHost) WriteInput(_ []byte) bool                  { return false }
func (h failingHost) Status() HostStatus {
	if h.status.Mode == "" {
		h.status = HostStatus{Mode: HostModeCodexPTY, State: HostStateFailed, Label: "codex failed", Note: h.err.Error()}
	}
	return h.status
}
func (h failingHost) Title() string                { return "failing" }
func (h failingHost) WorkerLabel() string          { return "" }
func (h failingHost) Lines(_ int, _ int) []string  { return nil }
func (h failingHost) ActivityLines(_ int) []string { return nil }

func TestStartPreferredHostFallsBackToTranscript(t *testing.T) {
	fallback := NewTranscriptHost()
	host, note := startPreferredHost(context.Background(), failingHost{err: errors.New("pty unavailable")}, fallback, Snapshot{TaskID: "tsk_1"})
	if host != fallback {
		t.Fatal("expected transcript fallback host")
	}
	if note == "" {
		t.Fatal("expected fallback note")
	}
	if host.Status().State != HostStateFallback {
		t.Fatalf("expected fallback state, got %s", host.Status().State)
	}
}

func TestStartPreferredHostFallsBackToTranscriptForClaudeFailure(t *testing.T) {
	fallback := NewTranscriptHost()
	host, note := startPreferredHost(context.Background(), failingHost{
		err: errors.New("claude unavailable"),
		status: HostStatus{
			Mode:  HostModeClaudePTY,
			State: HostStateFailed,
			Label: "claude failed",
			Note:  "claude unavailable",
		},
	}, fallback, Snapshot{TaskID: "tsk_claude"})
	if host != fallback {
		t.Fatal("expected transcript fallback host")
	}
	if note == "" {
		t.Fatal("expected fallback note")
	}
	if !strings.Contains(note, "Claude PTY host unavailable") {
		t.Fatalf("expected claude-specific fallback note, got %q", note)
	}
}

func TestTransitionExitedHostFallsBackToTranscript(t *testing.T) {
	exitCode := 7
	current := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateExited,
			Label:     "codex exited",
			ExitCode:  &exitCode,
			InputLive: false,
		},
	}
	fallback := NewTranscriptHost()
	host, note, changed := transitionExitedHost(context.Background(), current, fallback, Snapshot{TaskID: "tsk_2"})
	if !changed {
		t.Fatal("expected fallback transition after host exit")
	}
	if host != fallback {
		t.Fatal("expected transcript fallback host")
	}
	if note == "" {
		t.Fatal("expected transition note")
	}
	if host.Status().State != HostStateFallback {
		t.Fatalf("expected fallback state, got %s", host.Status().State)
	}
}

func TestParseWorkerPreference(t *testing.T) {
	preference, err := ParseWorkerPreference("claude")
	if err != nil {
		t.Fatalf("parse worker preference: %v", err)
	}
	if preference != WorkerPreferenceClaude {
		t.Fatalf("expected claude preference, got %q", preference)
	}
}

func TestSelectPreferredHostChoosesCodexByDefault(t *testing.T) {
	host, resolved, err := selectPreferredHost(WorkerPreferenceAuto, Snapshot{})
	if err != nil {
		t.Fatalf("select default host: %v", err)
	}
	if resolved != WorkerPreferenceCodex {
		t.Fatalf("expected codex resolution, got %q", resolved)
	}
	if _, ok := host.(*CodexPTYHost); !ok {
		t.Fatalf("expected codex host, got %T", host)
	}
}

func TestSelectPreferredHostChoosesClaudeFromPreference(t *testing.T) {
	host, resolved, err := selectPreferredHost(WorkerPreferenceClaude, Snapshot{})
	if err != nil {
		t.Fatalf("select claude host: %v", err)
	}
	if resolved != WorkerPreferenceClaude {
		t.Fatalf("expected claude resolution, got %q", resolved)
	}
	if _, ok := host.(*ClaudePTYHost); !ok {
		t.Fatalf("expected claude host, got %T", host)
	}
}

func TestSelectPreferredHostChoosesClaudeFromSnapshotContext(t *testing.T) {
	host, resolved, err := selectPreferredHost(WorkerPreferenceAuto, Snapshot{
		Run: &RunSummary{WorkerKind: "claude"},
	})
	if err != nil {
		t.Fatalf("select host from snapshot context: %v", err)
	}
	if resolved != WorkerPreferenceClaude {
		t.Fatalf("expected claude resolution, got %q", resolved)
	}
	if _, ok := host.(*ClaudePTYHost); !ok {
		t.Fatalf("expected claude host, got %T", host)
	}
}
