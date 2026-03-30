package shell

import (
	"strconv"
	"strings"
	"unicode"

	xansi "github.com/charmbracelet/x/ansi"
)

type terminalChunkResult struct {
	lines   []string
	partial string
	state   terminalParserState
}

type terminalParserState struct {
	escaped bool
	str     bool
	strEsc  bool
	csi     bool
	ss3     bool
	csiBuf  []byte
}

func normalizeTerminalChunk(partial string, chunk []byte) terminalChunkResult {
	return normalizeTerminalChunkWithState(partial, terminalParserState{}, chunk)
}

func normalizeTerminalChunkWithState(partial string, state terminalParserState, chunk []byte) terminalChunkResult {
	if len(chunk) == 0 {
		return terminalChunkResult{partial: partial, state: cloneTerminalParserState(state)}
	}

	var (
		lines   []string
		current = []rune(partial)
		escaped = state.escaped
		strMode = state.str
		strEsc  = state.strEsc
		csi     = state.csi
		ss3     = state.ss3
		csiBuf  = append([]byte(nil), state.csiBuf...)
	)

	flush := func() {
		line := strings.TrimRight(string(current), " ")
		lines = append(lines, line)
		current = current[:0]
	}

	for i := 0; i < len(chunk); i++ {
		ch := chunk[i]

		if strMode {
			if ch == 0x07 {
				strMode = false
				strEsc = false
				continue
			}
			if ch == 0x9c {
				strMode = false
				strEsc = false
				continue
			}
			if strEsc {
				if ch == '\\' {
					strMode = false
				}
				strEsc = ch == 0x1b
				continue
			}
			if ch == 0x1b {
				strEsc = true
			}
			continue
		}
		if csi {
			if ch >= 0x40 && ch <= 0x7e {
				applyCSI(&current, &lines, ch, string(csiBuf), flush)
				csiBuf = csiBuf[:0]
				csi = false
			} else {
				csiBuf = append(csiBuf, ch)
				if len(csiBuf) > 64 {
					// Drop malformed CSI payloads to avoid unbounded buffer growth.
					csiBuf = csiBuf[:0]
					csi = false
				}
			}
			continue
		}
		if ss3 {
			if ch >= 0x40 && ch <= 0x7e {
				ss3 = false
			}
			continue
		}
		if escaped {
			escaped = false
			switch ch {
			case '[':
				csi = true
				continue
			case ']', 'P', 'X', '^', '_':
				strMode = true
				strEsc = false
				continue
			case 'O':
				ss3 = true
				continue
			default:
				continue
			}
		}
		if ch == 0x9b {
			csi = true
			csiBuf = csiBuf[:0]
			continue
		}
		if ch == 0x9d || ch == 0x90 || ch == 0x98 || ch == 0x9e || ch == 0x9f {
			strMode = true
			strEsc = false
			continue
		}
		if ch == 0x8f {
			ss3 = true
			continue
		}
		if ch == 0x1b {
			escaped = true
			continue
		}

		switch ch {
		case '\r':
			if i+1 < len(chunk) && chunk[i+1] == '\n' {
				flush()
				i++
				continue
			}
			current = current[:0]
		case '\n':
			flush()
		case '\b', 0x7f:
			if len(current) > 0 {
				current = current[:len(current)-1]
			}
		case '\t':
			current = append(current, ' ', ' ')
		default:
			if ch >= 32 && ch < 127 {
				current = append(current, rune(ch))
			}
		}
	}

	return terminalChunkResult{
		lines:   lines,
		partial: strings.TrimRight(string(current), " "),
		state: terminalParserState{
			escaped: escaped,
			str:     strMode,
			strEsc:  strEsc,
			csi:     csi,
			ss3:     ss3,
			csiBuf:  append([]byte(nil), csiBuf...),
		},
	}
}

func cloneTerminalParserState(state terminalParserState) terminalParserState {
	return terminalParserState{
		escaped: state.escaped,
		str:     state.str,
		strEsc:  state.strEsc,
		csi:     state.csi,
		ss3:     state.ss3,
		csiBuf:  append([]byte(nil), state.csiBuf...),
	}
}

func applyCSI(current *[]rune, lines *[]string, final byte, params string, flush func()) {
	_, _ = parseCSIParams(params)

	switch final {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'f', 'm', 'K', 'J', 'h', 'l', 's', 'u', 't', 'n':
		// Drop cursor movement/presentation/device control sequences.
		// They are frequently used by interactive TUIs and preserving their
		// positioning semantics here introduces severe rendering artifacts.
		_ = current
		_ = lines
		_ = flush
		return
	default:
		return
	}
}

func parseCSIParams(raw string) (int, int) {
	if raw == "" {
		return 0, 0
	}
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "?")
	parts := strings.Split(clean, ";")

	first := parseInt(parts, 0)
	second := parseInt(parts, 1)
	return first, second
}

func parseInt(parts []string, idx int) int {
	if idx >= len(parts) {
		return 0
	}
	value := strings.TrimSpace(parts[idx])
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func isLikelyCursorNoiseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 8 {
		return false
	}

	cursorRunes := 0
	alphaNum := 0
	for i := 0; i < len(trimmed); i++ {
		if i+1 < len(trimmed) && trimmed[i] == '[' {
			next := trimmed[i+1]
			if next >= 'A' && next <= 'D' {
				cursorRunes++
				i++
				continue
			}
		}
		r := rune(trimmed[i])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			alphaNum++
		}
	}
	if cursorRunes < 6 {
		return false
	}
	return cursorRunes*2 > alphaNum
}

func sanitizeRenderedLine(line string) string {
	line = xansi.Strip(line)
	line = stripControlRunes(line)
	line = strings.TrimRight(line, " ")
	if line == "" {
		return ""
	}
	line = stripRepeatedCursorArtifacts(line)
	if isSingleCursorArtifactToken(line) {
		return ""
	}
	return strings.TrimRight(line, " ")
}

func stripControlRunes(line string) string {
	if line == "" {
		return line
	}
	var b strings.Builder
	for _, r := range line {
		switch {
		case r == '\t':
			b.WriteRune(' ')
			b.WriteRune(' ')
		case r < 32 || r == 127:
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stripRepeatedCursorArtifacts(line string) string {
	if line == "" {
		return line
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		if i+1 < len(line) && line[i] == '[' {
			start := i
			count := 0
			for i+1 < len(line) && line[i] == '[' && line[i+1] >= 'A' && line[i+1] <= 'D' {
				count++
				i += 2
			}
			if count >= 3 {
				continue
			}
			b.WriteString(line[start:i])
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

func isSingleCursorArtifactToken(line string) bool {
	token := strings.TrimSpace(line)
	switch token {
	case "A", "B", "C", "D", "[A", "[B", "[C", "[D", "OA", "OB", "OC", "OD":
		return true
	default:
		return false
	}
}
