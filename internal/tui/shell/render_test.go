package shell

import (
	"strings"
	"testing"
)

func TestRenderUsesLightweightChromeWithoutBoxedPanes(t *testing.T) {
	vm := ViewModel{
		Header: HeaderView{
			Title:       "Tuku Control Plane",
			TaskLabel:   "tsk_123",
			Phase:       "EXECUTING",
			Worker:      "codex live",
			Repo:        "Tuku@main",
			Continuity:  "active",
			WorkerState: "worker live",
			RepoState:   "repo clean",
			NextAction:  "repair continuity",
			SessionID:   "shs_123",
		},
		WorkerPane: PaneView{
			Title: "worker pane | codex live session",
			Lines: []string{"codex> hello"},
		},
		Footer: "session shs_123 | worker live input | next repair continuity | keys q quit",
		Layout: computeShellLayout(100, 16, UIState{}),
	}

	rendered := Render(vm, 100, 16)
	if !strings.Contains(rendered, "TUKU") {
		t.Fatalf("expected premium header kicker, got %q", rendered)
	}
	if !strings.Contains(rendered, "task tsk_123") {
		t.Fatalf("expected task chip in header, got %q", rendered)
	}
	if !strings.Contains(rendered, "worker pane") {
		t.Fatalf("expected worker panel title in render, got %q", rendered)
	}
	if !strings.Contains(rendered, "codex> hello") {
		t.Fatalf("expected worker content in render, got %q", rendered)
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

func TestRenderShowsInputDockState(t *testing.T) {
	layout := computeShellLayout(120, 28, UIState{})
	vm := ViewModel{
		Header: HeaderView{
			Title:       "Tuku Control Plane",
			TaskLabel:   "tsk_456",
			WorkerState: "worker live",
		},
		WorkerPane: PaneView{
			Title: "worker pane | live",
			Lines: []string{"codex> ready"},
		},
		InputDock: InputDockView{
			Title:       "Operator Input",
			Status:      "worker live input",
			PromptLabel: "tuku>",
			Placeholder: "Input goes directly to worker.",
			Hint:        "ctrl-g n execute next step",
		},
		Footer: "session shs_1 | refreshed 10:00:00",
		Layout: layout,
	}

	rendered := Render(vm, 120, 28)
	if !strings.Contains(rendered, "Operator Input") {
		t.Fatalf("expected input dock title in render, got %q", rendered)
	}
	if !strings.Contains(rendered, "tuku>") {
		t.Fatalf("expected input prompt label in render, got %q", rendered)
	}
}

func TestRenderRespectsCollapsedInspectorLayout(t *testing.T) {
	layout := computeShellLayout(96, 28, UIState{ShowInspector: true})
	if layout.showInspector {
		t.Fatalf("expected narrow layout to collapse inspector, got %+v", layout)
	}
	vm := ViewModel{
		Header: HeaderView{
			Title: "Tuku Control Plane",
		},
		WorkerPane: PaneView{
			Title: "worker pane | transcript",
			Lines: []string{"historical transcript below"},
		},
		Inspector: &InspectorView{
			Title: "inspector",
			Sections: []SectionView{
				{Title: "operator", Lines: []string{"next repair continuity"}},
			},
		},
		InputDock: InputDockView{
			Title:       "Read-Only Session",
			Status:      "fallback active",
			PromptLabel: "tuku>",
			Placeholder: "worker session is in transcript fallback mode; live input is unavailable",
		},
		Footer: "session shs_2",
		Layout: layout,
	}

	rendered := Render(vm, 96, 28)
	if strings.Contains(rendered, "context rail") {
		t.Fatalf("did not expect inspector rail copy when collapsed, got %q", rendered)
	}
	if !strings.Contains(rendered, "historical transcript below") {
		t.Fatalf("expected worker content in collapsed layout, got %q", rendered)
	}
}
