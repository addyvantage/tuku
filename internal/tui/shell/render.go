package shell

import (
	"fmt"
	"strings"
)

func Render(vm ViewModel, width int, height int) string {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}
	lines := make([]string, 0, height)
	lines = append(lines, renderHeader(vm.Header, width))

	layout := vm.Layout
	if layout.bodyHeight <= 0 {
		layout = computeShellLayout(width, height, UIState{
			ShowInspector: vm.Inspector != nil,
			ShowProof:     vm.ProofStrip != nil,
		})
	}
	bodyHeight := layout.bodyHeight
	workerWidth := layout.workerWidth
	inspectorWidth := layout.inspectorWidth
	workerLines := renderPane(vm.WorkerPane, workerWidth, bodyHeight)
	var inspectorLines []string
	if vm.Inspector != nil && inspectorWidth > 0 {
		inspectorLines = renderInspector(*vm.Inspector, inspectorWidth, bodyHeight)
	}
	for i := 0; i < bodyHeight; i++ {
		left := paddedLine(workerLines, i, workerWidth)
		if len(inspectorLines) == 0 {
			lines = append(lines, left)
			continue
		}
		right := paddedLine(inspectorLines, i, inspectorWidth)
		lines = append(lines, left+" "+right)
	}

	if vm.ProofStrip != nil {
		lines = append(lines, renderStrip(*vm.ProofStrip, width, layout.proofHeight)...)
	}
	lines = append(lines, truncateToWidth(vm.Footer, width))

	if vm.Overlay != nil {
		overlayWidth := min(max(width-12, 24), 72)
		lines = overlay(lines, width, height, renderOverlay(*vm.Overlay, overlayWidth))
	}

	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J")
	if len(lines) > height {
		lines = lines[:height]
	}
	for idx, line := range lines {
		b.WriteString(padToWidth(truncateToWidth(line, width), width))
		if idx < len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderHeader(h HeaderView, width int) string {
	parts := []string{
		fmt.Sprintf(" %s ", h.Title),
		fmt.Sprintf("task %s", h.TaskLabel),
		fmt.Sprintf("phase %s", h.Phase),
		fmt.Sprintf("worker %s", h.Worker),
		fmt.Sprintf("repo %s", h.Repo),
		fmt.Sprintf("state %s", h.Continuity),
	}
	return joinAndPad(parts, " | ", width)
}

func renderPane(p PaneView, width int, height int) []string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	lines := []string{sectionTitleLine(p.Title, width, p.Focused)}
	contentHeight := height - 1
	for i := 0; i < contentHeight; i++ {
		line := ""
		if i < len(p.Lines) {
			line = p.Lines[i]
		}
		lines = append(lines, padToWidth(truncateToWidth(line, width), width))
	}
	return lines
}

func renderInspector(ins InspectorView, width int, height int) []string {
	lines := []string{sectionTitleLine(ins.Title, width, ins.Focused)}
	for _, section := range ins.Sections {
		lines = append(lines, padToWidth(truncateToWidth(section.Title+":", width), width))
		for _, line := range section.Lines {
			for _, wrapped := range wrapText(line, max(1, width-2)) {
				lines = append(lines, padToWidth(truncateToWidth("  "+wrapped, width), width))
			}
		}
		lines = append(lines, "")
	}
	if len(lines) < height {
		for len(lines) < height {
			lines = append(lines, "")
		}
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func renderStrip(strip StripView, width int, height int) []string {
	if height < 1 {
		height = 1
	}
	lines := []string{sectionTitleLine(strip.Title, width, strip.Focused)}
	for _, line := range strip.Lines {
		for _, wrapped := range wrapText(line, width) {
			lines = append(lines, padToWidth(truncateToWidth(wrapped, width), width))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func renderOverlay(overlay OverlayView, width int) []string {
	lines := []string{borderTop(width, overlay.Title)}
	for _, line := range overlay.Lines {
		for _, wrapped := range wrapText(line, width-4) {
			lines = append(lines, borderLine(width, wrapped))
		}
	}
	lines = append(lines, borderBottom(width))
	return lines
}

func overlay(base []string, width int, height int, box []string) []string {
	if len(base) == 0 || len(box) == 0 {
		return base
	}
	top := max(1, (height-len(box))/2)
	left := max(1, (width-len(box[0]))/2)
	for i := 0; i < len(box) && top+i < len(base); i++ {
		row := []rune(padToWidth(base[top+i], width))
		boxRow := []rune(box[i])
		for j := 0; j < len(boxRow) && left+j < len(row); j++ {
			row[left+j] = boxRow[j]
		}
		base[top+i] = string(row)
	}
	return base
}

func titleWithFocus(title string, focused bool) string {
	if focused {
		return title + " [focus]"
	}
	return title
}

func borderTop(width int, title string) string {
	if width < 4 {
		return strings.Repeat("-", width)
	}
	title = " " + truncateWithEllipsis(title, width-6) + " "
	if len(title) >= width-2 {
		title = title[:width-2]
	}
	fill := width - 2 - len(title)
	return "+" + title + strings.Repeat("-", fill) + "+"
}

func borderBottom(width int) string {
	if width < 2 {
		return ""
	}
	return "+" + strings.Repeat("-", width-2) + "+"
}

func borderLine(width int, text string) string {
	if width < 2 {
		return truncateToWidth(text, width)
	}
	return "|" + padToWidth(truncateToWidth(text, width-2), width-2) + "|"
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
	if focused {
		title = title + " [focus]"
	}
	return padToWidth(truncateToWidth(title, width), width)
}

func runeLen(value string) int {
	return len([]rune(value))
}
