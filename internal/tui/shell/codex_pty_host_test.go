package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNormalizeTerminalChunkStripsANSISequences(t *testing.T) {
	result := normalizeTerminalChunk("", []byte("\x1b[32mcodex\x1b[0m ready\r\nnext line"))
	if result.partial != "next line" {
		t.Fatalf("expected partial to preserve current line, got %q", result.partial)
	}
	if len(result.lines) != 1 || result.lines[0] != "codex ready" {
		t.Fatalf("unexpected normalized lines %#v", result.lines)
	}
}

func TestNormalizeTerminalChunkHandlesCarriageReturnRedraw(t *testing.T) {
	result := normalizeTerminalChunk("", []byte("step 1\rstep 2\rstep 3\n"))
	if len(result.lines) != 1 || result.lines[0] != "step 3" {
		t.Fatalf("expected redraw collapse to last line, got %#v", result.lines)
	}
}

func TestNormalizeTerminalChunkPreservesPartialAcrossChunks(t *testing.T) {
	first := normalizeTerminalChunk("", []byte("loading 10%\rloading 20%"))
	if first.partial != "loading 20%" {
		t.Fatalf("expected partial redraw state, got %q", first.partial)
	}
	second := normalizeTerminalChunk(first.partial, []byte("\rloading 30%\n"))
	if len(second.lines) != 1 || second.lines[0] != "loading 30%" {
		t.Fatalf("expected final redraw line, got %#v", second.lines)
	}
}

func TestNormalizeTerminalChunkHandlesCursorForwardSpacing(t *testing.T) {
	result := normalizeTerminalChunk("", []byte("one\x1b[4Ctwo\n"))
	if len(result.lines) != 1 {
		t.Fatalf("expected one flushed line, got %#v", result.lines)
	}
	if result.lines[0] != "onetwo" {
		t.Fatalf("expected cursor-forward sequence to be dropped, got %q", result.lines[0])
	}
}

func TestNormalizeTerminalChunkDropsCursorPositionControl(t *testing.T) {
	result := normalizeTerminalChunk("", []byte("first\x1b[2;1Hsecond\n"))
	if len(result.lines) != 1 {
		t.Fatalf("expected one line after dropping cursor control, got %#v", result.lines)
	}
	if result.lines[0] != "firstsecond" {
		t.Fatalf("unexpected normalized cursor-position output %#v", result.lines)
	}
}

func TestNormalizeTerminalChunkPreservesEscapeStateAcrossChunks(t *testing.T) {
	first := normalizeTerminalChunkWithState("", terminalParserState{}, []byte("line\x1b"))
	if first.partial != "line" {
		t.Fatalf("expected partial line to be preserved, got %q", first.partial)
	}
	if !first.state.escaped {
		t.Fatalf("expected trailing escape byte to arm parser state")
	}

	second := normalizeTerminalChunkWithState(first.partial, first.state, []byte("[4Cnext\n"))
	if len(second.lines) != 1 {
		t.Fatalf("expected one normalized line, got %#v", second.lines)
	}
	if second.lines[0] != "linenext" {
		t.Fatalf("expected split escape sequence to strip cursor positioning, got %q", second.lines[0])
	}
}

func TestNormalizeTerminalChunkPreservesCSIStateAcrossChunks(t *testing.T) {
	first := normalizeTerminalChunkWithState("", terminalParserState{}, []byte("A\x1b["))
	if !first.state.csi {
		t.Fatalf("expected CSI parser state to remain armed across chunks")
	}
	second := normalizeTerminalChunkWithState(first.partial, first.state, []byte("2;1H"))
	third := normalizeTerminalChunkWithState(second.partial, second.state, []byte("B\n"))
	lines := append([]string{}, second.lines...)
	lines = append(lines, third.lines...)
	if len(lines) != 1 {
		t.Fatalf("expected one line after dropping split cursor control, got %#v", lines)
	}
	if lines[0] != "AB" {
		t.Fatalf("unexpected split CSI normalization %#v", lines)
	}
}

func TestNormalizeTerminalChunkHandlesSplitOSCStringTerminator(t *testing.T) {
	first := normalizeTerminalChunkWithState("", terminalParserState{}, []byte("\x1b]11;?\x1b"))
	if !first.state.str || !first.state.strEsc {
		t.Fatalf("expected control-string state to stay armed across chunks, got %#v", first.state)
	}
	second := normalizeTerminalChunkWithState(first.partial, first.state, []byte("\\ok\n"))
	if len(second.lines) != 1 || second.lines[0] != "ok" {
		t.Fatalf("expected split OSC terminator to recover clean output, got %#v", second.lines)
	}
}

func TestNormalizeTerminalChunkDropsDCSControlStringPayload(t *testing.T) {
	result := normalizeTerminalChunk("", []byte("a\x1bP1;2|garbage\x1b\\b\n"))
	if len(result.lines) != 1 || result.lines[0] != "ab" {
		t.Fatalf("expected DCS payload to be stripped, got %#v", result.lines)
	}
}

func TestCodexPTYHostAppendOutputFiltersCursorNoiseRunes(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	host.appendOutput([]byte("[A[A[A[A[A[A[A[A\nclean line\n"))

	lines := host.Lines(10, 80)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "[A[A[A") {
		t.Fatalf("expected cursor-noise runes to be filtered, got %q", joined)
	}
	if !strings.Contains(joined, "clean line") {
		t.Fatalf("expected non-noise output to be preserved, got %q", joined)
	}
}

func TestSanitizeRenderedLineStripsInlineCursorArtifacts(t *testing.T) {
	line := "MCP startup incomplete (failed: figma)[A[A[A[A[A"
	sanitized := sanitizeRenderedLine(line)
	if strings.Contains(sanitized, "[A[A[A") {
		t.Fatalf("expected repeated cursor artifacts to be stripped, got %q", sanitized)
	}
	if !strings.Contains(sanitized, "MCP startup incomplete") {
		t.Fatalf("expected content to survive sanitization, got %q", sanitized)
	}
}

func TestSanitizeRenderedLineDropsSingleCursorArtifacts(t *testing.T) {
	for _, token := range []string{"A", "[B", "  [A  ", "OB"} {
		if got := sanitizeRenderedLine(token); got != "" {
			t.Fatalf("expected token %q to be dropped, got %q", token, got)
		}
	}
}

func TestNormalizeTerminalChunkStripsSS3ControlSequences(t *testing.T) {
	result := normalizeTerminalChunk("", []byte("left\x1bOAright\n"))
	if len(result.lines) != 1 {
		t.Fatalf("expected one line, got %#v", result.lines)
	}
	if result.lines[0] != "leftright" {
		t.Fatalf("expected SS3 sequence to be stripped, got %q", result.lines[0])
	}
}

func TestCodexPTYHostStartRequiresRepoRoot(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	err := host.Start(context.Background(), Snapshot{})
	if err == nil {
		t.Fatal("expected missing repo root to block PTY host start")
	}
	if !strings.Contains(err.Error(), "repo root is required") {
		t.Fatalf("unexpected start error %q", err)
	}
	if host.Status().State != HostStateFailed {
		t.Fatalf("expected failed host state, got %s", host.Status().State)
	}
}

func TestTranscriptHostDefaultsToTranscriptOnlyState(t *testing.T) {
	host := NewTranscriptHost()
	if host.Status().State != HostStateTranscriptOnly {
		t.Fatalf("expected transcript-only state, got %s", host.Status().State)
	}
}

func TestCodexPTYHostLinesPreserveIndentedOutput(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	host.lines = []string{"    indented output"}
	host.status.State = HostStateLive

	lines := host.Lines(10, 8)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output, got %#v", lines)
	}
	if !strings.HasPrefix(lines[0], "    ") {
		t.Fatalf("expected indentation to be preserved, got %#v", lines)
	}
}

func TestCodexPTYHostLiveQuietStateExplainsWaiting(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	host.status.State = HostStateLive
	host.status.StateChangedAt = time.Now().UTC().Add(-18 * time.Second)

	lines := strings.Join(host.Lines(10, 80), "\n")
	if !strings.Contains(lines, "Input goes directly to the worker.") {
		t.Fatalf("expected live quiet-state banner, got %q", lines)
	}
	if strings.Contains(lines, "awaiting first visible output") {
		t.Fatalf("expected body copy to leave waiting-state phrasing to the pane summary, got %q", lines)
	}
}

func TestCodexPTYHostAppendOutputTracksLastOutputTime(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	host.appendOutput([]byte("codex ready\n"))

	if host.Status().LastOutputAt.IsZero() {
		t.Fatal("expected last output timestamp to be recorded")
	}
}

func TestCodexPTYHostDetectsHeuristicWorkerSessionIDSource(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	host.appendOutput([]byte("Session ID: wks_123456\n"))

	status := host.Status()
	if status.WorkerSessionID != "wks_123456" {
		t.Fatalf("expected detected worker session id, got %q", status.WorkerSessionID)
	}
	if status.WorkerSessionIDSource != WorkerSessionIDSourceHeuristic {
		t.Fatalf("expected heuristic source, got %s", status.WorkerSessionIDSource)
	}
}

func TestCodexPTYHostStartingStateUsesConciseBodyCopy(t *testing.T) {
	host := NewDefaultCodexPTYHost()

	lines := strings.Join(host.Lines(10, 80), "\n")
	if !strings.Contains(lines, "Launching Codex PTY session.") {
		t.Fatalf("expected concise starting copy, got %q", lines)
	}
	if strings.Contains(lines, "awaiting first visible output") {
		t.Fatalf("expected starting body to avoid repeating summary wording, got %q", lines)
	}
}

func TestCodexPTYHostExitedStateUsesConciseBodyCopy(t *testing.T) {
	host := NewDefaultCodexPTYHost()
	host.status.State = HostStateExited

	lines := strings.Join(host.Lines(10, 80), "\n")
	if !strings.Contains(lines, "The session ended before any visible output arrived.") {
		t.Fatalf("expected concise exited copy, got %q", lines)
	}
	if strings.Contains(lines, "is not live") {
		t.Fatalf("expected exited body to avoid repetitive not-live wording, got %q", lines)
	}
}
