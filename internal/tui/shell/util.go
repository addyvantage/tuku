package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func humanizeConstant(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "_", " ")
}

func truncateWithEllipsis(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
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
				if lipgloss.Width(current)+1+lipgloss.Width(segment) <= width {
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
	prefixWidth := lipgloss.Width(prefix)
	indent := strings.Repeat(" ", prefixWidth)
	available := max(1, width-prefixWidth)
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
	if width <= 0 || lipgloss.Width(token) <= width {
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

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
