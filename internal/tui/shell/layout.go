package shell

type shellLayout struct {
	headerHeight   int
	bodyHeight     int
	proofHeight    int
	dockHeight     int
	footerHeight   int
	outerPadding   int
	panelGap       int
	workerWidth    int
	inspectorWidth int
	contentWidth   int
	showInspector  bool
	showProof      bool
}

const (
	shellPaneBorderRows  = 2
	shellPaneBorderCols  = 2
	shellPaneHeaderRows  = 2
	shellPanePaddingCols = 0
	shellPanePaddingRows = 0
)

func computeShellLayout(width int, height int, ui UIState) shellLayout {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}

	outerPadding, panelGap, headerHeight, dockHeight, footerHeight := layoutDensity(width)
	contentWidth := width - (outerPadding * 2)
	if contentWidth < 44 {
		outerPadding = 0
		contentWidth = width
	}

	for availableContentHeight(height, headerHeight, dockHeight, footerHeight) < 6 {
		switch {
		case dockHeight > 2:
			dockHeight--
		case headerHeight > 2:
			headerHeight--
		case outerPadding > 0:
			outerPadding--
			contentWidth = width - (outerPadding * 2)
		default:
			break
		}
		if dockHeight <= 2 && headerHeight <= 2 && outerPadding == 0 {
			break
		}
	}

	available := availableContentHeight(height, headerHeight, dockHeight, footerHeight)
	if available < 3 {
		available = 3
	}

	showProof := ui.ShowProof
	proofHeight := 0
	if showProof {
		proofHeight = defaultProofHeight(width)
		if height < 22 || available-proofHeight < 7 {
			showProof = false
			proofHeight = 0
		}
	}

	bodyHeight := available - proofHeight
	if bodyHeight < 6 {
		showProof = false
		proofHeight = 0
		bodyHeight = available
	}
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	showInspector := ui.ShowInspector && contentWidth >= 116
	workerWidth := contentWidth
	inspectorWidth := 0
	if showInspector {
		inspectorWidth = min(44, max(30, contentWidth/3))
		workerWidth = contentWidth - inspectorWidth - panelGap
		if workerWidth < 58 {
			showInspector = false
			inspectorWidth = 0
			workerWidth = contentWidth
		}
	}

	return shellLayout{
		headerHeight:   headerHeight,
		bodyHeight:     bodyHeight,
		proofHeight:    proofHeight,
		dockHeight:     dockHeight,
		footerHeight:   footerHeight,
		outerPadding:   outerPadding,
		panelGap:       panelGap,
		workerWidth:    workerWidth,
		inspectorWidth: inspectorWidth,
		contentWidth:   contentWidth,
		showInspector:  showInspector,
		showProof:      showProof,
	}
}

func (l shellLayout) workerContentSize() (int, int) {
	width := l.workerWidth - shellPaneBorderCols - (shellPanePaddingCols * 2)
	height := l.bodyHeight - shellPaneBorderRows - shellPaneHeaderRows - (shellPanePaddingRows * 2)
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

func layoutDensity(width int) (outerPadding int, panelGap int, headerHeight int, dockHeight int, footerHeight int) {
	switch {
	case width >= 160:
		return 3, 3, 4, 6, 2
	case width >= 132:
		return 2, 2, 4, 5, 2
	case width >= 108:
		return 1, 2, 3, 5, 2
	case width >= 86:
		return 1, 1, 3, 4, 1
	default:
		return 0, 1, 2, 4, 1
	}
}

func availableContentHeight(height int, headerHeight int, dockHeight int, footerHeight int) int {
	return height - headerHeight - dockHeight - footerHeight
}

func defaultProofHeight(width int) int {
	switch {
	case width >= 150:
		return 6
	case width >= 116:
		return 5
	default:
		return 4
	}
}
