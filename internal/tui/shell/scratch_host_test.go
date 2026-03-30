package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalScratchHostPersistsCommittedNotes(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	path := filepath.Join(t.TempDir(), "scratch.json")
	host := NewLocalScratchHost(path, "/tmp/no-repo")
	host.clock = func() time.Time { return now }

	if err := host.Start(context.Background(), Snapshot{
		Phase: "SCRATCH_INTAKE",
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "scratch guidance"},
		},
	}); err != nil {
		t.Fatalf("start scratch host: %v", err)
	}

	for _, b := range []byte("plan initial repo layout") {
		if !host.WriteInput([]byte{b}) {
			t.Fatal("expected scratch host to accept input")
		}
	}
	host.WriteInput([]byte{'\n'})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read scratch file: %v", err)
	}
	var session persistedScratchSession
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("unmarshal scratch file: %v", err)
	}
	if session.Kind != "local_scratch_intake" {
		t.Fatalf("expected local scratch kind, got %q", session.Kind)
	}
	if session.CWD != "/tmp/no-repo" {
		t.Fatalf("expected persisted cwd, got %q", session.CWD)
	}
	if len(session.Notes) != 1 || session.Notes[0].Body != "plan initial repo layout" {
		t.Fatalf("expected one persisted note, got %#v", session.Notes)
	}
	if !strings.Contains(strings.Join(host.Lines(20, 80), "\n"), "plan initial repo layout") {
		t.Fatal("expected host lines to include persisted note")
	}
}

func TestLocalScratchHostLoadsPersistedNotesOnStart(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	path := filepath.Join(t.TempDir(), "scratch.json")
	if err := saveScratchNotes(path, "/tmp/no-repo", []ConversationItem{
		{Role: "user", Body: "draft milestone list", CreatedAt: now},
	}, func() time.Time { return now }); err != nil {
		t.Fatalf("seed scratch notes: %v", err)
	}

	host := NewLocalScratchHost(path, "/tmp/no-repo")
	host.clock = func() time.Time { return now }
	if err := host.Start(context.Background(), Snapshot{
		Phase: "SCRATCH_INTAKE",
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "scratch guidance"},
		},
	}); err != nil {
		t.Fatalf("start scratch host: %v", err)
	}

	if !host.CanAcceptInput() {
		t.Fatal("expected scratch host input to be live")
	}
	if host.Status().State != HostStateLive {
		t.Fatalf("expected live scratch host state, got %s", host.Status().State)
	}
	lines := strings.Join(host.Lines(20, 80), "\n")
	if !strings.Contains(lines, "draft milestone list") {
		t.Fatalf("expected loaded note in scratch lines, got %q", lines)
	}
}
