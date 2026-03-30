package shell

import "testing"

func TestComputeShellLayoutCollapsesInspectorOnNarrowTerminals(t *testing.T) {
	layout := computeShellLayout(96, 32, UIState{ShowInspector: true})
	if layout.showInspector {
		t.Fatalf("expected inspector to collapse on narrow widths, got %+v", layout)
	}
}

func TestComputeShellLayoutKeepsInspectorOnWideTerminals(t *testing.T) {
	layout := computeShellLayout(160, 40, UIState{ShowInspector: true})
	if !layout.showInspector {
		t.Fatalf("expected inspector to remain on wide terminals, got %+v", layout)
	}
	if layout.inspectorWidth <= 0 || layout.workerWidth <= 0 {
		t.Fatalf("expected positive pane widths, got %+v", layout)
	}
}

func TestWorkerContentSizeAccountsForPanelChrome(t *testing.T) {
	layout := computeShellLayout(140, 36, UIState{})
	width, height := layout.workerContentSize()
	if width <= 0 || height <= 0 {
		t.Fatalf("expected positive worker content dimensions, got width=%d height=%d", width, height)
	}
	if width >= layout.workerWidth {
		t.Fatalf("expected worker content width to be smaller than outer pane width, got %d >= %d", width, layout.workerWidth)
	}
	if height >= layout.bodyHeight {
		t.Fatalf("expected worker content height to be smaller than outer body height, got %d >= %d", height, layout.bodyHeight)
	}
}

func TestComputeShellLayoutCollapsesProofWhenTerminalIsShort(t *testing.T) {
	layout := computeShellLayout(150, 18, UIState{ShowProof: true})
	if layout.showProof {
		t.Fatalf("expected proof strip to collapse on short terminals, got %+v", layout)
	}
	if layout.bodyHeight < 3 {
		t.Fatalf("expected usable body height, got %+v", layout)
	}
}

func TestComputeShellLayoutKeepsInputDockInNarrowMode(t *testing.T) {
	layout := computeShellLayout(80, 24, UIState{})
	if layout.dockHeight < 2 {
		t.Fatalf("expected dock height to remain visible in narrow mode, got %+v", layout)
	}
	if layout.bodyHeight < 3 {
		t.Fatalf("expected body height to remain usable, got %+v", layout)
	}
}
