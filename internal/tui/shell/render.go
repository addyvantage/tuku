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
	innerWidth := max(1, layout.contentWidth)
	lines := make([]string, 0, layout.proofHeight)
	lines = append(lines, joinLeftRight(theme.activityTitle.Render(strings.ToUpper(title)), theme.railHint.Render(strings.ToLower(nonEmpty(subtitle, "activity stream"))), innerWidth))
	for _, line := range strip.Lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, "• "+line)
		if len(lines) >= layout.proofHeight {
			break
		}
	}
	for len(lines) < layout.proofHeight {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = theme.activityBody.Render(ansiPadToWidth(ansiTruncate(lines[i], innerWidth), innerWidth))
	}
	block := theme.activityBase.Width(innerWidth).Render(strings.Join(lines, "\n"))
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

	lines := []string{
		theme.footerRule.Render(strings.Repeat("─", innerWidth)),
		line,
	}
	if strings.TrimSpace(keys) != "" && layout.footerHeight > 2 {
		lines = append(lines, theme.footerMuted.Render(ansiTruncate(keys, innerWidth)))
	}
	if layout.footerHeight <= 1 {
		lines = []string{line}
	} else if len(lines) > layout.footerHeight {
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

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\t", "  ")
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimRight(part, " ")
		if part == "" {
			lines = append(lines, "")
			continue
		}
		words := strings.Fields(part)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := ""
		for _, segment := range wrapLongToken(words[0], width) {
			if current == "" {
				current = segment
				continue
			}
			lines = append(lines, current)
			current = segment
		}
		for _, word := range words[1:] {
			for _, segment := range wrapLongToken(word, width) {
				if current == "" {
					current = segment
					continue
				}
				if runeLen(current)+1+runeLen(segment) <= width {
					current += " " + segment
					continue
				}
				lines = append(lines, current)
				current = segment
			}
		}
		if current != "" {
			lines = append(lines, current)
		}
	}
	return lines
}

func wrapOutputLine(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	text = strings.ReplaceAll(text, "\t", "  ")
	if text == "" {
		return []string{""}
	}
	runes := []rune(text)
	lines := make([]string, 0, (len(runes)/width)+1)
	for len(runes) > 0 {
		chunk := min(width, len(runes))
		lines = append(lines, string(runes[:chunk]))
		runes = runes[chunk:]
	}
	return lines
}

func wrapPrefixedOutput(prefix string, text string, width int) []string {
	if width <= 0 {
		return []string{prefix + text}
	}
	text = strings.ReplaceAll(text, "\t", "  ")
	indent := strings.Repeat(" ", runeLen(prefix))
	available := max(1, width-runeLen(prefix))
	parts := strings.Split(text, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		wrapped := wrapOutputLine(part, available)
		for idx, segment := range wrapped {
			if idx == 0 {
				lines = append(lines, prefix+segment)
				continue
			}
			lines = append(lines, indent+segment)
		}
	}
	return lines
}

func wrapLongToken(token string, width int) []string {
	if width <= 0 || runeLen(token) <= width {
		return []string{token}
	}
	runes := []rune(token)
	lines := make([]string, 0, (len(runes)/width)+1)
	for len(runes) > 0 {
		chunk := min(width, len(runes))
		lines = append(lines, string(runes[:chunk]))
		runes = runes[chunk:]
	}
	return lines
}

func fitBottom(lines []string, height int) []string {
	if height <= 0 {
		return nil
	}
	if len(lines) <= height {
		return lines
	}
	return lines[len(lines)-height:]
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
