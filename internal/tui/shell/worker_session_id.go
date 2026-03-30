package shell

import (
	"regexp"
	"strings"
)

var sessionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)session(?:\s+id)?\s*[:=]\s*([a-z0-9][a-z0-9._:-]{5,})`),
	regexp.MustCompile(`(?i)resume(?:\s+with)?\s*[:=]?\s*(?:codex\s+resume|claude\s+--resume\s+--session-id)\s+([a-z0-9][a-z0-9._:-]{5,})`),
	regexp.MustCompile(`(?i)--session-id\s+([a-z0-9][a-z0-9._:-]{5,})`),
}

func detectWorkerSessionID(text string) string {
	id, _ := detectWorkerSessionIDWithSource(text)
	return id
}

func detectWorkerSessionIDWithSource(text string) (string, WorkerSessionIDSource) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", WorkerSessionIDSourceNone
	}
	for _, pattern := range sessionPatterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) < 2 {
			continue
		}
		candidate := strings.TrimSpace(match[1])
		if candidate == "" {
			continue
		}
		return candidate, WorkerSessionIDSourceHeuristic
	}
	return "", WorkerSessionIDSourceNone
}
