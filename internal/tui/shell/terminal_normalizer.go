package shell

import "strings"

type terminalChunkResult struct {
	lines   []string
	partial string
}

func normalizeTerminalChunk(partial string, chunk []byte) terminalChunkResult {
	if len(chunk) == 0 {
		return terminalChunkResult{partial: partial}
	}

	var (
		lines   []string
		current = []rune(partial)
		escaped bool
		osc     bool
		csi     bool
	)

	flush := func() {
		line := strings.TrimRight(string(current), " ")
		lines = append(lines, line)
		current = current[:0]
	}

	for i := 0; i < len(chunk); i++ {
		ch := chunk[i]

		if osc {
			if ch == 0x07 {
				osc = false
			}
			if ch == '\\' && i > 0 && chunk[i-1] == 0x1b {
				osc = false
			}
			continue
		}
		if csi {
			if ch >= 0x40 && ch <= 0x7e {
				csi = false
			}
			continue
		}
		if escaped {
			escaped = false
			switch ch {
			case '[':
				csi = true
				continue
			case ']':
				osc = true
				continue
			default:
				continue
			}
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
	}
}
