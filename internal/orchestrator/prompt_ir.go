package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/repoindex"
	"tuku/internal/domain/taskmemory"
)

func (c *Coordinator) buildPromptIRAndBenchmark(
	caps capsule.WorkCapsule,
	intentState intent.State,
	input brief.BuildInput,
	pack contextdomain.Pack,
	memory taskmemory.Snapshot,
	rawPrompt string,
) (promptir.Packet, repoindex.Snapshot, benchmark.Run, error) {
	paths := dedupeNonEmpty(append(append(append([]string{}, input.ScopeHints...), input.PromptTriage.CandidateFiles...), pack.IncludedFiles...), 12)
	index, err := buildLightweightRepoIndex(caps.RepoRoot, caps.HeadSHA, paths, c.clock())
	if err != nil {
		return promptir.Packet{}, repoindex.Snapshot{}, benchmark.Run{}, err
	}
	targets := promptIRTargetsFromIndex(index, input.PromptTriage.CandidateFiles)
	validatorPlan := plannedValidatorPlan(caps.RepoRoot, input.Posture, targets)
	confidence := promptIRConfidence(intentState, input, memory, targets, validatorPlan)
	packet := promptir.Packet{
		Version:            1,
		UserGoal:           strings.TrimSpace(nonEmpty(rawPrompt, caps.Goal)),
		NormalizedTaskType: string(intentState.Class),
		Objective:          strings.TrimSpace(input.Goal),
		Operation:          strings.TrimSpace(input.NormalizedAction),
		ScopeSummary:       strings.TrimSpace(input.ScopeSummary),
		RankedTargets:      append([]promptir.Target{}, targets...),
		OperationPlan: dedupeNonEmpty([]string{
			fmt.Sprintf("operate within the bounded scope: %s", strings.TrimSpace(input.ScopeSummary)),
			fmt.Sprintf("start from the ranked targets: %s", strings.Join(promptIRTargetLabels(targets, 3), ", ")),
			"avoid unrelated refactors unless evidence forces a nearby change",
			"run the planned validators before reporting completion",
		}, 6),
		Constraints: append([]string{}, input.Constraints...),
		NonGoals: dedupeNonEmpty([]string{
			"do not widen scope without concrete repo evidence",
			"do not claim completion without validation evidence",
		}, 4),
		ValidatorPlan: validatorPlan,
		MemoryDigest:  strings.TrimSpace(memory.Summary),
		OutputContract: []string{
			"return concise implementation summary",
			"list validators run and outcomes",
			"list unknowns or follow-up risk explicitly",
		},
		Confidence: confidence,
		EvidenceRequirements: []string{
			"bounded changed-files evidence",
			"validator evidence",
			"unknowns explicitly called out",
		},
		CompiledAt: c.clock(),
	}
	naturalTokens := estimateTextTokens(renderPromptIRNatural(packet))
	structuredTokens := estimateTextTokens(renderPromptIRStructured(packet))
	packet.NaturalLanguageTokens = naturalTokens
	packet.StructuredTokens = structuredTokens
	packet.StructuredCheaper = structuredTokens > 0 && structuredTokens < naturalTokens
	packet.DefaultSerializer = promptir.SerializerNaturalLanguage

	estimatedSavings := input.PromptTriage.ContextTokenSavingsEstimate
	if memory.FullHistoryTokenEstimate > memory.ResumePromptTokenEstimate {
		estimatedSavings += memory.FullHistoryTokenEstimate - memory.ResumePromptTokenEstimate
	}

	bench := benchmark.Run{
		Version:                       1,
		BenchmarkID:                   commonBenchmarkID(c.idGenerator("bmk")),
		TaskID:                        caps.TaskID,
		Source:                        "brief_compiled",
		RawPromptTokenEstimate:        input.PromptTriage.RawPromptTokenEstimate,
		DispatchPromptTokenEstimate:   naturalTokens,
		StructuredPromptTokenEstimate: structuredTokens,
		SelectedContextTokenEstimate:  input.PromptTriage.SelectedContextTokenEstimate,
		EstimatedTokenSavings:         estimatedSavings,
		FilesScanned:                  input.PromptTriage.FilesScanned,
		RankedTargetCount:             len(targets),
		StructuredCheaper:             packet.StructuredCheaper,
		DefaultSerializer:             string(packet.DefaultSerializer),
		ConfidenceValue:               packet.Confidence.Value,
		ConfidenceLevel:               packet.Confidence.Level,
		Summary: fmt.Sprintf(
			"ranked %d target(s), planned %d validator(s), estimated pre-dispatch savings %d token(s)",
			len(targets),
			len(packet.ValidatorPlan.Commands),
			estimatedSavings,
		),
		CreatedAt: c.clock(),
		UpdatedAt: c.clock(),
	}
	return packet, index, bench, nil
}

func buildLightweightRepoIndex(repoRoot string, headSHA string, paths []string, now time.Time) (repoindex.Snapshot, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	files := make([]repoindex.File, 0, len(paths))
	for _, rel := range dedupeNonEmpty(paths, 16) {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		text := string(data)
		files = append(files, repoindex.File{
			Path:          rel,
			TokenEstimate: estimateTextTokens(text),
			Kinds:         inferIndexedKinds(rel, text),
			Symbols:       inferIndexedSymbols(text),
		})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return repoindex.Snapshot{
		RepoRoot: repoRoot,
		HeadSHA:  strings.TrimSpace(headSHA),
		Files:    files,
		BuiltAt:  now,
	}, nil
}

func inferIndexedKinds(path string, text string) []string {
	lowerPath := strings.ToLower(strings.TrimSpace(path))
	lowerText := strings.ToLower(text)
	kinds := []string{"file"}
	switch {
	case strings.Contains(lowerPath, "/pages/"), strings.Contains(lowerPath, "/routes/"), strings.Contains(lowerPath, "router"), strings.Contains(lowerText, "httprouter"), strings.Contains(lowerText, "mux.router"):
		kinds = append(kinds, "route")
	case strings.Contains(lowerPath, "component"), strings.HasSuffix(lowerPath, ".tsx"), strings.HasSuffix(lowerPath, ".jsx"), strings.Contains(lowerText, "export function "), strings.Contains(lowerText, "export default function"):
		kinds = append(kinds, "component")
	}
	if strings.HasSuffix(lowerPath, "_test.go") || strings.Contains(lowerPath, ".test.") || strings.Contains(lowerPath, ".spec.") {
		kinds = append(kinds, "test")
	}
	return dedupeNonEmpty(kinds, 4)
}

func inferIndexedSymbols(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, 8)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "func "):
			out = append(out, extractIdentifierAfter(trimmed, "func "))
		case strings.HasPrefix(trimmed, "type "):
			out = append(out, extractIdentifierAfter(trimmed, "type "))
		case strings.HasPrefix(trimmed, "class "):
			out = append(out, extractIdentifierAfter(trimmed, "class "))
		case strings.HasPrefix(trimmed, "export function "):
			out = append(out, extractIdentifierAfter(trimmed, "export function "))
		case strings.HasPrefix(trimmed, "export default function "):
			out = append(out, extractIdentifierAfter(trimmed, "export default function "))
		}
		if len(out) >= 8 {
			break
		}
	}
	return dedupeNonEmpty(out, 8)
}

func extractIdentifierAfter(line string, prefix string) string {
	value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	for i, r := range value {
		if !(r == '_' || r == '-' || r == '$' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return strings.TrimSpace(value[:i])
		}
	}
	return strings.TrimSpace(value)
}

func promptIRTargetsFromIndex(index repoindex.Snapshot, preferred []string) []promptir.Target {
	preferredSet := map[string]int{}
	for idx, path := range dedupeNonEmpty(preferred, 12) {
		preferredSet[strings.ToLower(path)] = idx
	}
	targets := make([]promptir.Target, 0, len(index.Files)*2)
	for _, file := range index.Files {
		score := 40
		reasons := []string{"selected from bounded repo index"}
		if idx, ok := preferredSet[strings.ToLower(file.Path)]; ok {
			score = 100 - idx*5
			reasons = append(reasons, "ranked by prompt triage")
		}
		targetKind := promptir.TargetFile
		if containsFold(file.Kinds, "component") {
			targetKind = promptir.TargetComponent
		} else if containsFold(file.Kinds, "route") {
			targetKind = promptir.TargetRoute
		} else if containsFold(file.Kinds, "test") {
			targetKind = promptir.TargetTest
		}
		targets = append(targets, promptir.Target{
			Path:    file.Path,
			Kind:    targetKind,
			Score:   score,
			Reasons: dedupeNonEmpty(reasons, 4),
		})
		for _, symbol := range file.Symbols {
			targets = append(targets, promptir.Target{
				Path:    file.Path,
				Name:    symbol,
				Kind:    promptir.TargetSymbol,
				Score:   score - 5,
				Reasons: []string{"symbol extracted from ranked file"},
			})
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].Score == targets[j].Score {
			if targets[i].Path == targets[j].Path {
				return targets[i].Name < targets[j].Name
			}
			return targets[i].Path < targets[j].Path
		}
		return targets[i].Score > targets[j].Score
	})
	return dedupePromptTargets(targets, 10)
}

func dedupePromptTargets(targets []promptir.Target, limit int) []promptir.Target {
	if limit <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]promptir.Target, 0, min(limit, len(targets)))
	for _, target := range targets {
		key := strings.ToLower(strings.TrimSpace(string(target.Kind) + "|" + target.Path + "|" + target.Name))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, target)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func promptIRTargetLabels(targets []promptir.Target, limit int) []string {
	out := make([]string, 0, min(limit, len(targets)))
	for _, target := range targets {
		label := strings.TrimSpace(target.Path)
		if strings.TrimSpace(target.Name) != "" {
			label = fmt.Sprintf("%s#%s", label, target.Name)
		}
		if label != "" {
			out = append(out, label)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func plannedValidatorPlan(repoRoot string, posture brief.Posture, targets []promptir.Target) promptir.ValidatorPlan {
	targetFiles := make([]string, 0, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.Path) != "" {
			targetFiles = append(targetFiles, target.Path)
		}
	}
	targetFiles = uniqueStrings(targetFiles)
	evidence := []string{"changed files summary"}
	commands := plannedValidatorCommands(repoRoot, posture, targetFiles)
	evidence = append(evidence, "validator output")
	summary := "manual review only"
	strategy := "manual_review"
	estimated := "low"
	if len(commands) > 0 {
		summary = fmt.Sprintf("run %d bounded validator(s)", len(commands))
		strategy = "bounded_local_validation"
	}
	if len(commands) >= 3 {
		estimated = "medium"
	}
	return promptir.ValidatorPlan{
		Summary:   summary,
		Strategy:  strategy,
		Commands:  dedupeNonEmpty(commands, 8),
		Evidence:  dedupeNonEmpty(evidence, 4),
		Estimated: estimated,
	}
}

func promptIRConfidence(intentState intent.State, input brief.BuildInput, memory taskmemory.Snapshot, targets []promptir.Target, validatorPlan promptir.ValidatorPlan) promptir.ConfidenceScore {
	score := intentState.Confidence
	if input.PromptTriage.Applied {
		score += 0.15
	}
	if len(targets) >= 2 {
		score += 0.10
	}
	if len(validatorPlan.Commands) > 0 {
		score += 0.10
	}
	if memory.ResumePromptTokenEstimate > 0 {
		score += 0.05
	}
	if input.RequiresClarification || len(input.ClarificationQuestions) > 0 {
		score -= 0.20
	}
	if len(input.AmbiguityFlags) > 0 {
		score -= 0.10
	}
	if score < 0 {
		score = 0
	}
	if score > 0.99 {
		score = 0.99
	}
	level := "low"
	if score >= 0.80 {
		level = "high"
	} else if score >= 0.55 {
		level = "medium"
	}
	reason := fmt.Sprintf("targets=%d validators=%d ambiguity=%d", len(targets), len(validatorPlan.Commands), len(input.AmbiguityFlags))
	if input.RequiresClarification {
		reason = "clarification remains open despite bounded targeting"
	}
	return promptir.ConfidenceScore{Value: score, Level: level, Reason: reason}
}

func renderPromptIRNatural(packet promptir.Packet) string {
	parts := []string{
		packet.UserGoal,
		packet.Objective,
		packet.Operation,
		packet.ScopeSummary,
		strings.Join(promptIRTargetLabels(packet.RankedTargets, 5), "\n"),
		strings.Join(packet.OperationPlan, "\n"),
		strings.Join(packet.Constraints, "\n"),
		strings.Join(packet.ValidatorPlan.Commands, "\n"),
		packet.MemoryDigest,
		strings.Join(packet.OutputContract, "\n"),
		strings.Join(packet.EvidenceRequirements, "\n"),
		packet.Confidence.Reason,
	}
	return strings.Join(parts, "\n")
}

func renderPromptIRStructured(packet promptir.Packet) string {
	clone := clonePromptIR(packet)
	clone.NaturalLanguageTokens = 0
	clone.StructuredTokens = 0
	raw, _ := json.Marshal(clone)
	return string(raw)
}

func containsFold(values []string, needle string) bool {
	needle = strings.TrimSpace(strings.ToLower(needle))
	for _, item := range values {
		if strings.TrimSpace(strings.ToLower(item)) == needle {
			return true
		}
	}
	return false
}

func commonBenchmarkID(raw string) common.BenchmarkID {
	return common.BenchmarkID(strings.TrimSpace(raw))
}
