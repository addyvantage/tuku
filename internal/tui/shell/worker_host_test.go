package shell

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTranscriptHostFallbackLinesExplainReadOnlyMode(t *testing.T) {
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.UpdateSnapshot(Snapshot{
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response."},
			{Role: "worker", Body: "Worker answer."},
		},
	})

	lines := strings.Join(host.Lines(12, 80), "\n")
	if !strings.Contains(lines, "Live worker input is unavailable in this shell.") {
		t.Fatalf("expected fallback banner, got %q", lines)
	}
	if !strings.Contains(lines, "live worker exited; switched to transcript fallback") {
		t.Fatalf("expected fallback note, got %q", lines)
	}
	if strings.Contains(lines, "historical transcript only") {
		t.Fatalf("expected fallback banner to avoid repeating transcript-summary wording, got %q", lines)
	}
	if strings.Contains(lines, "Canonical response.") {
		t.Fatalf("expected system narration to stay out of transcript fallback body, got %q", lines)
	}
	if !strings.Contains(lines, "Worker   Worker answer.") {
		t.Fatalf("expected worker transcript content to remain, got %q", lines)
	}
}

func TestTranscriptHostSuppressesSystemNarrationFromConversationBody(t *testing.T) {
	host := NewTranscriptHost()
	host.markTranscriptOnly("")
	host.UpdateSnapshot(Snapshot{
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Tuku task initialized. Repo anchor captured."},
			{Role: "user", Body: "Help me build the TUI properly"},
			{Role: "worker", Body: "I can help with that."},
		},
	})

	lines := strings.Join(host.Lines(16, 80), "\n")
	if strings.Contains(lines, "Tuku task initialized.") {
		t.Fatalf("expected startup narration to stay out of transcript body, got %q", lines)
	}
	if !strings.Contains(lines, "You      Help me build the TUI properly") || !strings.Contains(lines, "Worker   I can help with that.") {
		t.Fatalf("expected user and worker turns to remain visible, got %q", lines)
	}
}

func TestTranscriptHostTitleMarksReadOnlyFallback(t *testing.T) {
	host := NewTranscriptHost()
	host.markFallback("fallback active")

	if got := host.Title(); got != "worker pane | transcript fallback | read-only" {
		t.Fatalf("unexpected fallback title %q", got)
	}
}

func TestTranscriptHostTranscriptOnlyLinesExplainHistoricalMode(t *testing.T) {
	host := NewTranscriptHost()
	host.markTranscriptOnly("")

	lines := strings.Join(host.Lines(8, 80), "\n")
	if !strings.Contains(lines, "Showing bounded transcript evidence in a read-only shell.") {
		t.Fatalf("expected historical transcript banner, got %q", lines)
	}
}

func TestTranscriptHostRendersDurableTranscriptEvidenceChunks(t *testing.T) {
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.UpdateSnapshot(Snapshot{
		ShellSessions: []KnownShellSession{
			{
				SessionID:                "shs_1",
				TranscriptState:          "transcript_only_bounded_partial",
				TranscriptRetainedChunks: 40,
				TranscriptDroppedChunks:  12,
				LastUpdatedAt:            time.Unix(1710000005, 0).UTC(),
			},
		},
		RecentShellTranscript: []ShellTranscriptChunkSummary{
			{Source: "worker_output", Content: "first durable line"},
			{Source: "fallback_note", Content: "fallback reason persisted"},
		},
	})

	lines := strings.Join(host.Lines(16, 80), "\n")
	if !strings.Contains(lines, "Recent durable transcript evidence (bounded retention):") {
		t.Fatalf("expected durable transcript evidence heading, got %q", lines)
	}
	if !strings.Contains(lines, "transcript-only partial evidence: 40 retained, 12 dropped by bounded retention") {
		t.Fatalf("expected bounded transcript summary line, got %q", lines)
	}
	if !strings.Contains(lines, "Worker   first durable line") {
		t.Fatalf("expected worker evidence transcript line, got %q", lines)
	}
	if !strings.Contains(lines, "Evidence fallback reason persisted") {
		t.Fatalf("expected fallback evidence transcript line, got %q", lines)
	}
}

func TestTranscriptHostHistoryLinesRetainEarlierConversationBeyondViewportHeight(t *testing.T) {
	host := NewTranscriptHost()
	host.markTranscriptOnly("")
	conversation := make([]ConversationItem, 0, 40)
	for i := 0; i < 20; i++ {
		conversation = append(conversation,
			ConversationItem{Role: "user", Body: fmt.Sprintf("prompt %02d", i)},
			ConversationItem{Role: "worker", Body: fmt.Sprintf("reply %02d", i)},
		)
	}
	host.UpdateSnapshot(Snapshot{RecentConversation: conversation})

	lines := host.HistoryLines(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "prompt 00") || !strings.Contains(joined, "reply 19") {
		t.Fatalf("expected full transcript history, got %q", joined)
	}
	if compact := strings.Join(host.Lines(6, 80), "\n"); strings.Contains(compact, "prompt 00") {
		t.Fatalf("expected compact Lines call to remain bottom-fitted, got %q", compact)
	}
}
