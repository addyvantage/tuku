package shell

type shellLayout struct {
	bodyHeight     int
	workerWidth    int
	inspectorWidth int
	proofHeight    int
	showInspector  bool
	showProof      bool
}

func computeShellLayout(width int, height int, ui UIState) shellLayout {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}

	availableHeight := height - 2
	if availableHeight < 3 {
		availableHeight = 3
	}

	showProof := ui.ShowProof
	proofHeight := 0
	if showProof {
		proofHeight = 4
		if height < 18 || availableHeight-proofHeight < 8 {
			showProof = false
			proofHeight = 0
		}
	}

	bodyHeight := availableHeight - proofHeight
	if bodyHeight < 3 {
		bodyHeight = availableHeight
		showProof = false
		proofHeight = 0
	}

	showInspector := ui.ShowInspector
	workerWidth := width
	inspectorWidth := 0
	if showInspector {
		if width < 120 {
			showInspector = false
		}
	}
	if showInspector {
		inspectorWidth = min(36, max(28, width/4))
		workerWidth = width - inspectorWidth - 2
		if workerWidth < 72 {
			showInspector = false
			inspectorWidth = 0
			workerWidth = width
		}
	}

	return shellLayout{
		bodyHeight:     bodyHeight,
		workerWidth:    workerWidth,
		inspectorWidth: inspectorWidth,
		proofHeight:    proofHeight,
		showInspector:  showInspector,
		showProof:      showProof,
	}
}

func (l shellLayout) workerContentSize() (int, int) {
	width := l.workerWidth
	height := l.bodyHeight - 1
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}
