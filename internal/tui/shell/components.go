package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func splitLines(block string) []string {
	if block == "" {
		return []string{""}
	}
	return strings.Split(block, "\n")
}

func ansiPadToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	visible := lipgloss.Width(value)
	if visible > width {
		return xansi.Truncate(value, width, "")
	}
	if visible < width {
		return value + strings.Repeat(" ", width-visible)
	}
	return value
}

func ansiTruncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return xansi.Truncate(value, width, "")
}

func joinColumns(left []string, leftWidth int, right []string, rightWidth int, gap int) []string {
	height := max(len(left), len(right))
	if height == 0 {
		return nil
	}

	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		if rightWidth <= 0 {
			out = append(out, ansiPadToWidth(l, leftWidth))
			continue
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out = append(out, ansiPadToWidth(l, leftWidth)+strings.Repeat(" ", max(0, gap))+ansiPadToWidth(r, rightWidth))
	}
	return out
}

func wrapTokens(tokens []string, width int, gap int) []string {
	if width <= 0 {
		return []string{strings.Join(tokens, " ")}
	}
	if gap < 0 {
		gap = 0
	}
	lines := make([]string, 0, len(tokens))
	current := ""
	currentWidth := 0
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokenWidth := lipgloss.Width(token)
		if currentWidth == 0 {
			current = token
			currentWidth = tokenWidth
			continue
		}
		needed := gap + tokenWidth
		if currentWidth+needed > width {
			lines = append(lines, current)
			current = token
			currentWidth = tokenWidth
			continue
		}
		current += strings.Repeat(" ", gap) + token
		currentWidth += needed
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func renderPanel(
	theme shellTheme,
	title string,
	subtitle string,
	lines []string,
	width int,
	height int,
	focused bool,
	preserveWhitespace bool,
) string {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}

	panelStyle := theme.workspaceBase
	if focused {
		panelStyle = theme.workspaceFocused
	}

	frameW := panelStyle.GetHorizontalFrameSize()
	frameH := panelStyle.GetVerticalFrameSize()
	innerW := max(1, width-frameW)
	innerH := max(1, height-frameH)

	content := make([]string, 0, innerH)
	content = append(content, joinLeftRight(theme.workspaceTitle.Render(ansiTruncate(strings.TrimSpace(title), innerW)), theme.workspaceSub.Render(ansiTruncate(strings.TrimSpace(subtitle), innerW)), innerW))
	content = append(content, strings.Repeat(" ", innerW))

	bodyLimit := max(0, innerH-len(content))
	body := fitWrappedLines(lines, innerW, bodyLimit, preserveWhitespace)
	for _, line := range body {
		content = append(content, ansiPadToWidth(theme.workspaceBody.Render(ansiTruncate(line, innerW)), innerW))
	}

	for len(content) < innerH {
		content = append(content, strings.Repeat(" ", innerW))
	}
	if len(content) > innerH {
		content = content[:innerH]
	}
	return panelStyle.Render(strings.Join(content, "\n"))
}

func fitWrappedLines(lines []string, width int, height int, preserveWhitespace bool) []string {
	if height <= 0 || width <= 0 {
		return nil
	}
	out := make([]string, 0, height)
	for _, line := range lines {
		var wrapped []string
		if preserveWhitespace {
			wrapped = wrapOutputLine(line, width)
		} else {
			wrapped = wrapText(line, width)
		}
		out = append(out, wrapped...)
		if len(out) >= height {
			break
		}
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}

func joinLeftRight(left string, right string, width int) string {
	if width <= 0 {
		return ""
	}
	left = ansiTruncate(left, width)
	right = ansiTruncate(right, width)
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	if right == "" || leftW+1+rightW > width {
		return ansiPadToWidth(left, width)
	}
	gap := width - leftW - rightW
	return left + strings.Repeat(" ", max(1, gap)) + right
}

func boxOverlay(base []string, width int, height int, overlay []string) []string {
	if len(base) == 0 {
		return base
	}
	if len(overlay) == 0 {
		return base
	}

	overlayWidth := 0
	for _, line := range overlay {
		overlayWidth = max(overlayWidth, lipgloss.Width(line))
	}
	overlayHeight := len(overlay)

	top := max(0, (height-overlayHeight)/2)
	left := max(0, (width-overlayWidth)/2)

	out := append([]string{}, base...)
	for i := 0; i < overlayHeight && top+i < len(out); i++ {
		overlayLine := ansiPadToWidth(overlay[i], overlayWidth)
		out[top+i] = ansiPadToWidth(strings.Repeat(" ", left)+overlayLine, width)
	}
	return out
}

func renderRailCard(theme shellTheme, title string, lines []string, width int, maxHeight int) []string {
	if width <= 0 || maxHeight <= 0 {
		return nil
	}
	innerWidth := max(1, width-2)
	cardLines := []string{
		theme.railCardTitle.Render(ansiTruncate(strings.ToUpper(strings.TrimSpace(title)), innerWidth)),
	}
	bodyLimit := maxHeight - len(cardLines)
	if bodyLimit > 0 {
		body := fitWrappedLines(lines, innerWidth, bodyLimit, false)
		for _, line := range body {
			cardLines = append(cardLines, theme.railCardBody.Render(ansiTruncate(line, innerWidth)))
		}
	}
	for len(cardLines) < maxHeight {
		cardLines = append(cardLines, "")
	}
	rendered := theme.railCard.Width(width).Render(strings.Join(cardLines, "\n"))
	return splitLines(rendered)
}
