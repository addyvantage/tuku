package orchestrator

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/taskmemory"
)

const taskMemoryConversationWindow = 64

func (c *Coordinator) buildTaskMemorySnapshot(caps capsule.WorkCapsule, b brief.ExecutionBrief, pack contextdomain.Pack, latestRun *rundomain.ExecutionRun, source string) (taskmemory.Snapshot, error) {
	recentMessages, err := c.store.Conversations().ListRecent(caps.ConversationID, taskMemoryConversationWindow)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}

	historyParts := make([]string, 0, len(recentMessages))
	for i := len(recentMessages) - 1; i >= 0; i-- {
		msg := recentMessages[i]
		historyParts = append(historyParts, fmt.Sprintf("%s: %s", msg.Role, strings.TrimSpace(msg.Body)))
	}
	fullHistoryTokens := estimateTextTokens(strings.Join(historyParts, "\n"))

	validatorsRun := inferValidatorsRun(latestRun)
	touchedFiles := dedupeNonEmpty(append(append(append([]string{}, caps.TouchedFiles...), b.ScopeIn...), taskMemoryRunChangedFiles(latestRun)...), 12)
	candidateFiles := dedupeNonEmpty(append(append([]string{}, b.PromptTriage.CandidateFiles...), pack.IncludedFiles...), 10)
	unknowns := buildTaskMemoryUnknowns(caps, b, latestRun)
	rejectedHypotheses := buildRejectedHypotheses(latestRun)
	confirmedFacts := buildTaskMemoryFacts(caps, b, pack, latestRun, validatorsRun, touchedFiles, candidateFiles)
	lastBlocker := deriveTaskMemoryBlocker(caps, latestRun, unknowns)
	summary := buildTaskMemorySummary(caps, b, latestRun, touchedFiles, validatorsRun, unknowns, lastBlocker)
	resumeTokens := estimateTextTokens(summary)
	ratio := 0.0
	if resumeTokens > 0 && fullHistoryTokens > 0 {
		ratio = float64(fullHistoryTokens) / float64(resumeTokens)
	}

	return taskmemory.Snapshot{
		Version:                   1,
		MemoryID:                  common.MemoryID(c.idGenerator("mem")),
		TaskID:                    caps.TaskID,
		RunID:                     runIDOrEmpty(latestRun),
		Phase:                     caps.CurrentPhase,
		Source:                    nonEmpty(strings.TrimSpace(source), "task_state"),
		Summary:                   summary,
		ConfirmedFacts:            confirmedFacts,
		RejectedHypotheses:        rejectedHypotheses,
		Unknowns:                  unknowns,
		UserConstraints:           dedupeNonEmpty(append(append([]string{}, caps.Constraints...), b.Constraints...), 12),
		TouchedFiles:              touchedFiles,
		ValidatorsRun:             validatorsRun,
		CandidateFiles:            candidateFiles,
		LastBlocker:               lastBlocker,
		NextSuggestedStep:         strings.TrimSpace(caps.NextAction),
		FullHistoryTokenEstimate:  fullHistoryTokens,
		ResumePromptTokenEstimate: resumeTokens,
		MemoryCompactionRatio:     ratio,
		CreatedAt:                 c.clock(),
	}, nil
}

func (c *Coordinator) persistTaskMemoryForBrief(caps capsule.WorkCapsule, b brief.ExecutionBrief, pack contextdomain.Pack, source string) (taskmemory.Snapshot, error) {
	snapshot, err := c.buildTaskMemorySnapshot(caps, b, pack, nil, source)
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.BriefID = b.BriefID
	if err := c.store.TaskMemories().Save(snapshot); err != nil {
		return taskmemory.Snapshot{}, err
	}
	return snapshot, nil
}

func (c *Coordinator) refreshTaskMemoryForCurrentState(caps capsule.WorkCapsule, latestRun *rundomain.ExecutionRun, source string) (*taskmemory.Snapshot, error) {
	if caps.CurrentBriefID == "" {
		return nil, nil
	}
	b, err := c.store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	pack, err := c.resolveExecutionContextPack(caps, b)
	if err != nil {
		return nil, err
	}
	snapshot, err := c.buildTaskMemorySnapshot(caps, b, pack, latestRun, source)
	if err != nil {
		return nil, err
	}
	snapshot.BriefID = b.BriefID
	if latestRun != nil {
		snapshot.RunID = latestRun.RunID
	}
	if err := c.store.TaskMemories().Save(snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (c *Coordinator) resolveExecutionTaskMemory(caps capsule.WorkCapsule, b brief.ExecutionBrief, pack contextdomain.Pack) (taskmemory.Snapshot, error) {
	if latest, err := c.store.TaskMemories().LatestByTask(caps.TaskID); err == nil {
		return latest, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return taskmemory.Snapshot{}, err
	}
	if b.TaskMemoryID != "" {
		snapshot, err := c.store.TaskMemories().Get(b.TaskMemoryID)
		if err == nil {
			return snapshot, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return taskmemory.Snapshot{}, err
		}
	}
	snapshot, err := c.buildTaskMemorySnapshot(caps, b, pack, nil, "execution_fallback")
	if err != nil {
		return taskmemory.Snapshot{}, err
	}
	snapshot.BriefID = b.BriefID
	if err := c.store.TaskMemories().Save(snapshot); err != nil {
		return taskmemory.Snapshot{}, err
	}
	return snapshot, nil
}

func memoryCompressionFromSnapshot(snapshot taskmemory.Snapshot) brief.MemoryCompression {
	return brief.MemoryCompression{
		Applied:                   true,
		Summary:                   snapshot.Summary,
		FullHistoryTokenEstimate:  snapshot.FullHistoryTokenEstimate,
		ResumePromptTokenEstimate: snapshot.ResumePromptTokenEstimate,
		MemoryCompactionRatio:     snapshot.MemoryCompactionRatio,
		ConfirmedFactsCount:       len(snapshot.ConfirmedFacts),
		TouchedFilesCount:         len(snapshot.TouchedFiles),
		ValidatorsRunCount:        len(snapshot.ValidatorsRun),
		CandidateFilesCount:       len(snapshot.CandidateFiles),
		RejectedHypothesesCount:   len(snapshot.RejectedHypotheses),
		UnknownsCount:             len(snapshot.Unknowns),
	}
}

func buildTaskMemoryFacts(caps capsule.WorkCapsule, b brief.ExecutionBrief, pack contextdomain.Pack, latestRun *rundomain.ExecutionRun, validatorsRun []string, touchedFiles []string, candidateFiles []string) []string {
	facts := []string{
		fmt.Sprintf("goal: %s", strings.TrimSpace(caps.Goal)),
		fmt.Sprintf("brief posture: %s", b.Posture),
		fmt.Sprintf("normalized action: %s", strings.TrimSpace(b.NormalizedAction)),
		fmt.Sprintf("scope summary: %s", strings.TrimSpace(b.ScopeSummary)),
	}
	if len(candidateFiles) > 0 {
		facts = append(facts, "ranked candidate files: "+strings.Join(limitStrings(candidateFiles, 5), ", "))
	}
	if len(pack.IncludedFiles) > 0 {
		facts = append(facts, "bounded context files: "+strings.Join(limitStrings(pack.IncludedFiles, 5), ", "))
	}
	if len(touchedFiles) > 0 {
		facts = append(facts, "touched files: "+strings.Join(limitStrings(touchedFiles, 5), ", "))
	}
	if len(validatorsRun) > 0 {
		facts = append(facts, "validators run: "+strings.Join(validatorsRun, ", "))
	}
	if latestRun != nil {
		runState := fmt.Sprintf("latest run %s", strings.ToLower(string(latestRun.Status)))
		if latestRun.ExitCode != nil {
			runState = fmt.Sprintf("%s with exit code %d", runState, *latestRun.ExitCode)
		}
		facts = append(facts, runState)
		if strings.TrimSpace(latestRun.RepoDiffSummary) != "" {
			facts = append(facts, "repo diff: "+strings.TrimSpace(latestRun.RepoDiffSummary))
		}
		if strings.TrimSpace(latestRun.WorktreeSummary) != "" {
			facts = append(facts, "worktree: "+strings.TrimSpace(latestRun.WorktreeSummary))
		}
	}
	return dedupeNonEmpty(facts, 10)
}

func buildTaskMemoryUnknowns(caps capsule.WorkCapsule, b brief.ExecutionBrief, latestRun *rundomain.ExecutionRun) []string {
	unknowns := append([]string{}, b.ClarificationQuestions...)
	if latestRun != nil {
		unknowns = append(unknowns, extractExecutionStructuredList(latestRun.StructuredSummary, "unknowns")...)
		for _, signal := range latestRun.ValidationSignals {
			lower := strings.ToLower(strings.TrimSpace(signal))
			if strings.Contains(lower, "unknown") || strings.Contains(lower, "not possible") || strings.Contains(lower, "not matched") {
				unknowns = append(unknowns, strings.TrimSpace(signal))
			}
		}
	}
	unknowns = append(unknowns, caps.Blockers...)
	return dedupeNonEmpty(unknowns, 6)
}

func buildRejectedHypotheses(latestRun *rundomain.ExecutionRun) []string {
	if latestRun == nil {
		return nil
	}
	rejected := extractExecutionStructuredList(latestRun.StructuredSummary, "rejected_hypotheses")
	if len(rejected) > 0 {
		return dedupeNonEmpty(rejected, 6)
	}
	return nil
}

func inferValidatorsRun(latestRun *rundomain.ExecutionRun) []string {
	if latestRun == nil {
		return nil
	}
	structured := extractExecutionStructuredList(latestRun.StructuredSummary, "validators_run")
	if len(structured) > 0 {
		return dedupeNonEmpty(structured, 8)
	}
	inferred := make([]string, 0, len(latestRun.ValidationSignals))
	for _, signal := range latestRun.ValidationSignals {
		lower := strings.ToLower(strings.TrimSpace(signal))
		switch {
		case strings.Contains(lower, "gofmt"):
			inferred = append(inferred, "gofmt -l")
		case strings.Contains(lower, "go test"):
			inferred = append(inferred, "go test")
		case strings.Contains(lower, "shell") && strings.Contains(lower, "syntax"):
			inferred = append(inferred, "sh -n")
		case strings.Contains(lower, "javascript") && strings.Contains(lower, "syntax"):
			inferred = append(inferred, "node --check")
		case strings.Contains(lower, "json"):
			inferred = append(inferred, "json parse")
		}
	}
	return dedupeNonEmpty(inferred, 8)
}

func deriveTaskMemoryBlocker(caps capsule.WorkCapsule, latestRun *rundomain.ExecutionRun, unknowns []string) string {
	if len(caps.Blockers) > 0 {
		return strings.TrimSpace(caps.Blockers[0])
	}
	if latestRun != nil && latestRun.Status == rundomain.StatusInterrupted && strings.TrimSpace(latestRun.InterruptionReason) != "" {
		return strings.TrimSpace(latestRun.InterruptionReason)
	}
	if latestRun != nil && latestRun.Status == rundomain.StatusFailed && strings.TrimSpace(latestRun.LastKnownSummary) != "" {
		return strings.TrimSpace(latestRun.LastKnownSummary)
	}
	if len(unknowns) > 0 {
		return strings.TrimSpace(unknowns[0])
	}
	return ""
}

func buildTaskMemorySummary(caps capsule.WorkCapsule, b brief.ExecutionBrief, latestRun *rundomain.ExecutionRun, touchedFiles []string, validatorsRun []string, unknowns []string, blocker string) string {
	parts := []string{
		fmt.Sprintf("phase=%s", caps.CurrentPhase),
		fmt.Sprintf("action=%s", strings.TrimSpace(b.NormalizedAction)),
	}
	if len(touchedFiles) > 0 {
		parts = append(parts, "files="+strings.Join(limitStrings(touchedFiles, 4), ","))
	}
	if latestRun != nil {
		runPart := "run=" + strings.ToLower(string(latestRun.Status))
		if latestRun.ExitCode != nil {
			runPart = fmt.Sprintf("%s(%d)", runPart, *latestRun.ExitCode)
		}
		parts = append(parts, runPart)
	}
	if len(validatorsRun) > 0 {
		parts = append(parts, "validators="+strings.Join(limitStrings(validatorsRun, 3), ","))
	}
	if strings.TrimSpace(blocker) != "" {
		parts = append(parts, "blocker="+strings.TrimSpace(blocker))
	}
	if len(unknowns) > 0 {
		parts = append(parts, "unknowns="+strings.Join(limitStrings(unknowns, 2), " | "))
	}
	if strings.TrimSpace(caps.NextAction) != "" {
		parts = append(parts, "next="+strings.TrimSpace(caps.NextAction))
	}
	return strings.Join(parts, "; ")
}

func extractExecutionStructuredList(raw string, key string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.TrimSpace(key) == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	value, ok := payload[key]
	if !ok {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" && text != "<nil>" {
			out = append(out, text)
		}
	}
	return out
}

func runIDOrEmpty(latestRun *rundomain.ExecutionRun) common.RunID {
	if latestRun == nil {
		return ""
	}
	return latestRun.RunID
}

func taskMemoryRunChangedFiles(latestRun *rundomain.ExecutionRun) []string {
	if latestRun == nil {
		return nil
	}
	return append([]string{}, latestRun.ChangedFiles...)
}

func limitStrings(values []string, max int) []string {
	if max <= 0 || len(values) <= max {
		return append([]string{}, values...)
	}
	out := append([]string{}, values[:max]...)
	sort.Strings(out)
	return out
}
