package shell

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func Render(vm ViewModel, width int, height int) string {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}

	layout := vm.Layout
	if layout.bodyHeight <= 0 {
		layout = computeShellLayout(width, height, UIState{
			ShowInspector: vm.Inspector != nil,
			ShowProof:     vm.ProofStrip != nil,
		})
	}
	theme := newShellTheme(layout)

	lines := make([]string, 0, height)
	lines = append(lines, renderHeaderBlock(theme, vm.Header, layout)...)
	lines = append(lines, renderMainWorkspace(theme, vm, layout)...)
	if vm.ProofStrip != nil && layout.showProof {
		lines = append(lines, renderActivityStrip(theme, *vm.ProofStrip, layout)...)
	}
	lines = append(lines, renderInputDock(theme, vm.InputDock, layout)...)
	lines = append(lines, renderFooterBlock(theme, vm.Footer, layout)...)

	if vm.Overlay != nil {
		overlayWidth := min(max(layout.contentWidth-12, 52), 96)
		overlayHeight := min(max(len(vm.Overlay.Lines)+5, 9), max(9, height-4))
		overlay := renderOverlay(theme, *vm.Overlay, overlayWidth, overlayHeight)
		lines = boxOverlay(lines, width, height, splitLines(overlay))
	}

	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}

	padded := make([]string, 0, len(lines))
	for _, line := range lines {
		padded = append(padded, theme.rootBackground.Render(ansiPadToWidth(ansiTruncate(line, width), width)))
	}

	var b strings.Builder
	b.WriteString("\x1b[H")
	b.WriteString(strings.Join(padded, "\n"))
	return b.String()
}

func renderHeaderBlock(theme shellTheme, h HeaderView, layout shellLayout) []string {
	innerWidth := max(1, layout.contentWidth)
	left := theme.headerKicker.Render("TUKU") + "  " + theme.headerTitle.Render(strings.ToUpper(nonEmpty(h.Title, "CONTROL PLANE")))
	right := ""
	if strings.TrimSpace(h.SessionID) != "" {
		right = theme.chip("session "+h.SessionID, shellToneMuted)
	}
	headerLines := []string{joinLeftRight(left, right, innerWidth)}

	meta := []string{
		theme.chip("task "+nonEmpty(h.TaskLabel, "no-task"), shellToneNeutral),
		theme.chip("phase "+strings.ToLower(nonEmpty(h.Phase, "unknown")), toneForStatus(h.Phase)),
		theme.chip(nonEmpty(h.WorkerState, "worker unknown"), toneForStatus(h.WorkerState)),
		theme.chip(nonEmpty(h.Continuity, "continuity n/a"), toneForStatus(h.Continuity)),
		theme.chip(nonEmpty(h.RepoState, "repo n/a"), toneForStatus(h.RepoState)),
		theme.chip(nonEmpty(h.Worker, "worker n/a"), shellToneMuted),
	}
	next := nonEmpty(h.NextAction, "none")
	subLine := theme.headerSubtext.Render("repo " + nonEmpty(h.Repo, "n/a") + "   ·   next " + next)

	switch {
	case layout.headerHeight <= 2:
		compact := wrapTokens(meta[:min(len(meta), 4)], innerWidth, 1)
		if len(compact) == 0 {
			compact = []string{subLine}
		}
		headerLines = append(headerLines, compact[0])
	case layout.headerHeight == 3:
		compact := wrapTokens(meta, innerWidth, 1)
		if len(compact) > 0 {
			headerLines = append(headerLines, compact[0])
		}
		headerLines = append(headerLines, subLine)
	default:
		headerLines = append(headerLines, wrapTokens(meta, innerWidth, 1)...)
		headerLines = append(headerLines, subLine)
		headerLines = append(headerLines, theme.headerRule.Render(strings.Repeat("─", innerWidth)))
	}

	headerLines = fitHeaderLines(headerLines, layout.headerHeight)
	return applyOuterPadding(headerLines, layout.outerPadding)
}

func renderMainWorkspace(theme shellTheme, vm ViewModel, layout shellLayout) []string {
	workerTitle, workerSubtitle := splitPaneTitle(vm.WorkerPane.Title)
	worker := renderPanel(
		theme,
		workerTitle,
		workerSubtitle,
		vm.WorkerPane.Lines,
		layout.workerWidth,
		layout.bodyHeight,
		vm.WorkerPane.Focused,
		true,
	)
	workerLines := splitLines(worker)

	if vm.Inspector == nil || !layout.showInspector || layout.inspectorWidth <= 0 {
		return applyOuterPadding(workerLines, layout.outerPadding)
	}

	rightLines := renderInspectorRail(theme, *vm.Inspector, layout.inspectorWidth, layout.bodyHeight)
	joined := joinColumns(workerLines, layout.workerWidth, rightLines, layout.inspectorWidth, layout.panelGap)
	return applyOuterPadding(joined, layout.outerPadding)
}

func renderActivityStrip(theme shellTheme, strip StripView, layout shellLayout) []string {
	title, subtitle := splitPaneTitle(strip.Title)
	frameW := theme.activityBase.GetHorizontalFrameSize()
	frameH := theme.activityBase.GetVerticalFrameSize()
	innerWidth := max(1, layout.contentWidth-frameW)
	innerHeight := max(1, layout.proofHeight-frameH)

	lines := make([]string, 0, innerHeight)
	lines = append(lines, joinLeftRight(theme.activityTitle.Render(strings.ToUpper(title)), theme.railHint.Render(strings.ToLower(nonEmpty(subtitle, "activity stream"))), innerWidth))
	for _, line := range strip.Lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, "• "+line)
		if len(lines) >= innerHeight {
			break
		}
	}
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = theme.activityBody.Render(ansiPadToWidth(ansiTruncate(lines[i], innerWidth), innerWidth))
	}
	block := theme.activityBase.Render(strings.Join(lines, "\n"))
	return applyOuterPadding(splitLines(block), layout.outerPadding)
}

func renderInputDock(theme shellTheme, dock InputDockView, layout shellLayout) []string {
	innerWidth := max(1, layout.contentWidth)
	dockStyle := theme.dockBase
	if dock.Focused && !dock.ReadOnly {
		dockStyle = dockStyle.Copy().BorderForeground(lipColor("#597A9F"))
	}
	if dock.ReadOnly {
		dockStyle = dockStyle.Copy().BorderForeground(lipColor("#4A463C"))
	}

	frameW := dockStyle.GetHorizontalFrameSize()
	frameH := dockStyle.GetVerticalFrameSize()
	contentWidth := max(1, innerWidth-frameW)
	contentHeight := max(1, layout.dockHeight-frameH)

	textStyle := theme.dockPlaceholder
	promptLabel := theme.dockPrompt.Render(nonEmpty(dock.PromptLabel, "tuku>"))
	bodyText := strings.TrimSpace(dock.Placeholder)
	if len(dock.Preview) > 0 {
		bodyText = strings.TrimSpace(dock.Preview[0])
		textStyle = theme.workspaceBody
	}
	if dock.ReadOnly {
		textStyle = theme.dockReadOnly
	}

	header := joinLeftRight(
		theme.dockTitle.Render(nonEmpty(dock.Title, "Operator Input")),
		theme.chip(nonEmpty(dock.Status, "n/a"), toneForStatus(dock.Status)),
		contentWidth,
	)
	prompt := ansiPadToWidth(
		promptLabel+" "+textStyle.Render(ansiTruncate(bodyText, max(1, contentWidth-runeLen(nonEmpty(dock.PromptLabel, "tuku>"))-1))),
		contentWidth,
	)
	hint := ansiPadToWidth(theme.dockHint.Render(ansiTruncate(strings.TrimSpace(dock.Hint), contentWidth)), contentWidth)

	lines := make([]string, 0, contentHeight)
	switch {
	case contentHeight == 1:
		lines = append(lines, prompt)
	case contentHeight == 2:
		lines = append(lines, header, prompt)
	default:
		lines = append(lines, header, prompt)
	}

	for i := 1; i < len(dock.Preview) && len(lines) < contentHeight-1; i++ {
		lines = append(lines, ansiPadToWidth("      "+theme.workspaceBody.Render(ansiTruncate(dock.Preview[i], max(1, contentWidth-6))), contentWidth))
	}
	if len(lines) < contentHeight {
		lines = append(lines, hint)
	}
	for len(lines) < contentHeight {
		lines = append(lines, strings.Repeat(" ", contentWidth))
	}

	block := dockStyle.Render(strings.Join(lines[:contentHeight], "\n"))
	return applyOuterPadding(splitLines(block), layout.outerPadding)
}

func renderFooterBlock(theme shellTheme, footer string, layout shellLayout) []string {
	innerWidth := max(1, layout.contentWidth)
	parts := splitFooterParts(footer)

	keys := ""
	refresh := ""
	primary := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(strings.ToLower(p), "keys "):
			keys = p
		case strings.HasPrefix(strings.ToLower(p), "refreshed "):
			refresh = p
		default:
			primary = append(primary, p)
		}
	}

	left := strings.Join(primary, " · ")
	left = truncateWithEllipsis(left, max(20, innerWidth-22))
	line := joinLeftRight(theme.footerText.Render(left), theme.footerMuted.Render(refresh), innerWidth)
	keyLine := ""
	if strings.TrimSpace(keys) != "" {
		keyLine = renderFooterKeyHints(theme, keys, innerWidth)
	}

	lines := make([]string, 0, max(1, layout.footerHeight))
	switch {
	case layout.footerHeight <= 1:
		lines = []string{line}
	case layout.footerHeight == 2:
		lines = append(lines, line)
		if keyLine != "" {
			lines = append(lines, keyLine)
		} else {
			lines = append(lines, theme.footerRule.Render(strings.Repeat("─", innerWidth)))
		}
	default:
		lines = append(lines,
			theme.footerRule.Render(strings.Repeat("─", innerWidth)),
			line,
		)
		if keyLine != "" {
			lines = append(lines, keyLine)
		}
	}
	if len(lines) > layout.footerHeight {
		lines = lines[:layout.footerHeight]
	}
	for len(lines) < layout.footerHeight {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = ansiPadToWidth(lines[i], innerWidth)
	}
	return applyOuterPadding(lines, layout.outerPadding)
}

func renderFooterKeyHints(theme shellTheme, keyPart string, width int) string {
	hints := parseFooterKeyHints(keyPart)
	if len(hints) == 0 {
		return ""
	}
	tokens := make([]string, 0, len(hints))
	for _, hint := range hints {
		tokens = append(tokens, theme.keyHint(hint[0], hint[1]))
	}
	wrapped := wrapTokens(tokens, width, 2)
	if len(wrapped) == 0 {
		return ""
	}
	return theme.footerMuted.Render(ansiPadToWidth(ansiTruncate(wrapped[0], width), width))
}

func parseFooterKeyHints(keysPart string) [][2]string {
	keysPart = strings.TrimSpace(keysPart)
	if keysPart == "" {
		return nil
	}
	lower := strings.ToLower(keysPart)
	if strings.HasPrefix(lower, "keys ") {
		keysPart = strings.TrimSpace(keysPart[len("keys "):])
	}
	fields := strings.Fields(keysPart)
	if len(fields) == 0 {
		return nil
	}

	hints := make([][2]string, 0, len(fields)/2)
	for i := 0; i < len(fields); {
		key := fields[i]
		label := ""
		if i+1 < len(fields) {
			label = fields[i+1]
			i += 2
		} else {
			i++
		}
		hints = append(hints, [2]string{key, label})
	}
	return hints
}

func renderOverlay(theme shellTheme, overlay OverlayView, width int, height int) string {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	frameW := theme.overlayBase.GetHorizontalFrameSize()
	frameH := theme.overlayBase.GetVerticalFrameSize()
	innerWidth := max(1, width-frameW)
	innerHeight := max(1, height-frameH)

	lines := []string{
		theme.overlayTitle.Render(strings.ToUpper(nonEmpty(overlay.Title, "status"))),
		"",
	}
	bodyLimit := max(0, innerHeight-len(lines))
	body := fitWrappedLines(overlay.Lines, innerWidth, bodyLimit, false)
	for _, line := range body {
		lines = append(lines, theme.overlayText.Render(ansiTruncate(line, innerWidth)))
	}
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}
	return theme.overlayBase.Render(strings.Join(lines[:innerHeight], "\n"))
}

func renderInspectorRail(theme shellTheme, inspector InspectorView, width int, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	lines := make([]string, 0, height)
	title := strings.ToUpper(nonEmpty(inspector.Title, "inspector"))
	lines = append(lines, theme.railTitle.Render(ansiPadToWidth(ansiTruncate(title, width), width)))
	lines = append(lines, theme.railHint.Render(ansiPadToWidth("context rail", width)))
	lines = append(lines, "")

	sections := orderedInspectorSections(inspector.Sections)
	remaining := height - len(lines)
	for idx, section := range sections {
		if remaining <= 0 {
			break
		}
		leftSections := len(sections) - idx
		minReserve := max(0, leftSections-1) * 2
		cardHeight := 2
		if remaining-minReserve >= 4 {
			cardHeight = 4
		} else if remaining-minReserve >= 3 {
			cardHeight = 3
		}
		if cardHeight > remaining {
			cardHeight = remaining
		}
		bodyLines := compactSectionLines(section.Lines, cardHeight-1, max(20, width-4))
		card := renderRailCard(theme, section.Title, bodyLines, width, cardHeight)
		lines = append(lines, card...)
		remaining = height - len(lines)
	}

	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = theme.railBase.Width(width).Render(ansiPadToWidth(ansiTruncate(lines[i], width), width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func applyOuterPadding(lines []string, padding int) []string {
	if padding <= 0 {
		return lines
	}
	prefix := strings.Repeat(" ", padding)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, prefix+line)
	}
	return out
}

func splitFooterParts(footer string) []string {
	parts := strings.Split(footer, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func splitPaneTitle(title string) (string, string) {
	parts := strings.Split(title, "|")
	head := strings.TrimSpace(title)
	sub := ""
	if len(parts) > 0 {
		head = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		rest := make([]string, 0, len(parts)-1)
		for _, part := range parts[1:] {
			p := strings.TrimSpace(part)
			if p != "" {
				rest = append(rest, p)
			}
		}
		sub = strings.Join(rest, " · ")
	}
	return nonEmpty(head, "panel"), sub
}

func compactSectionLines(lines []string, maxLines int, maxWidth int) []string {
	if maxLines <= 0 {
		return nil
	}
	out := make([]string, 0, maxLines)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		out = append(out, truncateWithEllipsis(trimmed, maxWidth))
		if len(out) >= maxLines {
			break
		}
	}
	if len(out) == 0 {
		out = []string{"n/a"}
	}
	return out
}

func orderedInspectorSections(sections []SectionView) []SectionView {
	priority := map[string]int{
		"operator":        0,
		"worker session":  1,
		"pending message": 2,
		"brief":           3,
		"intent":          4,
		"run":             5,
		"handoff":         6,
		"launch":          7,
		"checkpoint":      8,
		"proof":           9,
	}
	out := append([]SectionView{}, sections...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, iok := priority[strings.ToLower(strings.TrimSpace(out[i].Title))]
		pj, jok := priority[strings.ToLower(strings.TrimSpace(out[j].Title))]
		if iok && jok {
			return pi < pj
		}
		if iok {
			return true
		}
		if jok {
			return false
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func fitHeaderLines(lines []string, target int) []string {
	if target <= 0 {
		return nil
	}
	if len(lines) >= target {
		return lines[:target]
	}
	for len(lines) < target {
		lines = append(lines, "")
	}
	return lines
}

func lipColor(v string) lipgloss.TerminalColor {
	return lipgloss.Color(v)
}

func joinAndPad(parts []string, sep string, width int) string {
	line := strings.Join(parts, sep)
	return padToWidth(truncateToWidth(line, width), width)
}

func paddedLine(lines []string, idx int, width int) string {
	if idx >= 0 && idx < len(lines) {
		return padToWidth(lines[idx], width)
	}
	return strings.Repeat(" ", max(0, width))
}

func padToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if runeLen(value) >= width {
		return truncateToWidth(value, width)
	}
	return value + strings.Repeat(" ", width-runeLen(value))
}

func truncateToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width])
}

func sectionTitleLine(title string, width int, focused bool) string {
	prefix := "  "
	if focused {
		prefix = "> "
	}
	line := prefix + title
	if runeLen(line) < width {
		line += " " + strings.Repeat("-", width-runeLen(line)-1)
	}
	return padToWidth(truncateToWidth(line, width), width)
}

func runeLen(value string) int {
	return len([]rune(value))
}
