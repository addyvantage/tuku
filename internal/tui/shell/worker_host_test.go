package shell

import (
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
		},
	})

	lines := strings.Join(host.Lines(12, 80), "\n")
	if !strings.Contains(lines, "Live input is unavailable in this pane.") {
		t.Fatalf("expected fallback banner, got %q", lines)
	}
	if !strings.Contains(lines, "live worker exited; switched to transcript fallback") {
		t.Fatalf("expected fallback note, got %q", lines)
	}
	if strings.Contains(lines, "historical transcript only") {
		t.Fatalf("expected fallback banner to avoid repeating transcript-summary wording, got %q", lines)
	}
	if !strings.Contains(lines, "tuku> Canonical response.") {
		t.Fatalf("expected transcript content, got %q", lines)
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
	if !strings.Contains(lines, "No live worker is attached to this pane.") {
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
	if !strings.Contains(lines, "worker> first durable line") {
		t.Fatalf("expected worker evidence transcript line, got %q", lines)
	}
	if !strings.Contains(lines, "evidence> fallback reason persisted") {
		t.Fatalf("expected fallback evidence transcript line, got %q", lines)
	}
}
