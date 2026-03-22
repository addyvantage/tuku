package shell

import (
	"strings"
	"testing"
)

func TestRenderUsesLightweightChromeWithoutBoxedPanes(t *testing.T) {
	vm := ViewModel{
		Header: HeaderView{
			Title:      "Tuku",
			TaskLabel:  "tsk_123",
			Phase:      "EXECUTING",
			Worker:     "codex live",
			Repo:       "Tuku@main",
			Continuity: "active",
		},
		WorkerPane: PaneView{
			Title: "worker pane | codex live session",
			Lines: []string{"codex> hello"},
		},
		Footer: "q quit | h help",
		Layout: computeShellLayout(100, 16, UIState{}),
	}

	rendered := Render(vm, 100, 16)
	if strings.Contains(rendered, "+--") || strings.Contains(rendered, "|codex>") {
		t.Fatalf("expected lightweight shell chrome without boxed panes, got %q", rendered)
	}
	if !strings.Contains(rendered, "worker pane | codex live session") {
		t.Fatalf("expected worker title in render, got %q", rendered)
	}
}

func TestWrapTextBreaksLongTokensToWidth(t *testing.T) {
	lines := wrapText("superlongtokenvalue", 5)
	if len(lines) < 2 {
		t.Fatalf("expected long token to wrap, got %#v", lines)
	}
	for _, line := range lines {
		if runeLen(line) > 5 {
			t.Fatalf("expected wrapped line width <= 5, got %q", line)
		}
	}
}

func TestWrapOutputLinePreservesWhitespace(t *testing.T) {
	lines := wrapOutputLine("    indented output", 8)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output lines, got %#v", lines)
	}
	if !strings.HasPrefix(lines[0], "    ") {
		t.Fatalf("expected leading spaces to be preserved, got %#v", lines)
	}
}
