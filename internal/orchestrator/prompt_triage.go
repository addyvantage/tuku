package orchestrator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/intent"
)

const (
	promptTriageMaxCandidateFiles       = 5
	promptTriageSearchSpaceFiles        = 12
	promptTriageMaxFileBytes      int64 = 256 * 1024
)

type promptTriageCandidate struct {
	Path          string
	Score         int
	TokenEstimate int
}

type promptTriageResult struct {
	IntentState   intent.State
	PromptTriage  brief.PromptTriage
	ScopeHints    []string
	WorkerFraming string
}

func (c *Coordinator) sharpenPromptTriage(caps capsule.WorkCapsule, st intent.State, latest string) (promptTriageResult, error) {
	result := promptTriageResult{
		IntentState: st,
		PromptTriage: brief.PromptTriage{
			RawPromptTokenEstimate: estimateTextTokens(latest),
		},
	}
	if !shouldPromptTriage(latest, st.Class, st.AmbiguityFlags) {
		return result, nil
	}

	searchTerms := extractPromptTriageSearchTerms(latest, st.Class)
	if len(searchTerms) == 0 {
		return result, nil
	}

	repoRoot := strings.TrimSpace(caps.RepoRoot)
	if repoRoot == "" {
		repoRoot = "."
	}
	candidates, filesScanned, scannedTokenEstimate, err := rankPromptTriageCandidates(repoRoot, searchTerms, st.Class)
	if err != nil {
		return result, err
	}
	if len(candidates) == 0 {
		return result, nil
	}

	result.IntentState = recomputeIntentStateAfterPromptTriage(st, latest, candidates)
	result.ScopeHints = candidatePaths(candidates, promptTriageMaxCandidateFiles)
	result.WorkerFraming = augmentWorkerFramingWithPromptTriage(
		defaultWorkerFramingForPosture(deriveBriefPostureFromIntent(result.IntentState), result.IntentState.RequiresClarification),
		result.ScopeHints,
	)
	result.PromptTriage = brief.PromptTriage{
		Applied:                  true,
		Reason:                   promptTriageReason(st.AmbiguityFlags, st.Class),
		Summary:                  promptTriageSummary(filesScanned, result.ScopeHints, st.Class),
		SearchTerms:              append([]string{}, searchTerms...),
		CandidateFiles:           append([]string{}, result.ScopeHints...),
		FilesScanned:             filesScanned,
		RawPromptTokenEstimate:   estimateTextTokens(latest),
		SearchSpaceTokenEstimate: estimateSearchSpaceTokens(candidates, filesScanned, scannedTokenEstimate, searchTerms),
	}
	return result, nil
}

func shouldPromptTriage(latest string, class intent.Class, ambiguityFlags []string) bool {
	latest = strings.TrimSpace(latest)
	if latest == "" {
		return false
	}
	switch class {
	case intent.ClassImplement, intent.ClassDebug, intent.ClassValidate, intent.ClassReplan:
	default:
		return false
	}
	if len(scopeTokenPattern.FindAllString(latest, -1)) > 0 {
		return false
	}
	lower := strings.ToLower(latest)
	if stringSliceContainsFold(ambiguityFlags, "scope_not_explicit") || stringSliceContainsFold(ambiguityFlags, "missing_explicit_action") {
		return true
	}
	if len(lower) <= 96 {
		return true
	}
	return hasAny(lower, "ui", "frontend", "screen", "page", "button", "modal", "dialog", "api", "endpoint", "handler", "bug", "issue", "broken", "failing")
}

func extractPromptTriageSearchTerms(latest string, class intent.Class) []string {
	raw := promptTriageTermsFromText(strings.ToLower(latest))
	expanded := make([]string, 0, len(raw)+8)
	for _, term := range raw {
		expanded = append(expanded, term)
		switch term {
		case "ui", "ux", "frontend", "front", "front-end":
			expanded = append(expanded, "component", "page", "screen", "modal", "dialog", "form", "button", "layout", "style", "css")
		case "css", "style", "styles":
			expanded = append(expanded, "scss", "theme", "layout")
		case "api", "backend", "server", "endpoint":
			expanded = append(expanded, "handler", "route", "http", "service")
		case "login", "auth", "authentication":
			expanded = append(expanded, "session", "signin", "sign-in", "oauth")
		case "test", "tests", "validation":
			expanded = append(expanded, "spec")
		}
	}
	if len(expanded) == 0 {
		switch class {
		case intent.ClassDebug:
			expanded = append(expanded, "error", "failure")
		case intent.ClassValidate:
			expanded = append(expanded, "test", "spec")
		}
	}
	return dedupeNonEmpty(expanded, 14)
}

func promptTriageTermsFromText(text string) []string {
	matches := scopeTokenPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		matches = strings.FieldsFunc(text, func(r rune) bool {
			switch {
			case r >= 'a' && r <= 'z':
				return false
			case r >= '0' && r <= '9':
				return false
			case r == '-', r == '_', r == '/':
				return false
			default:
				return true
			}
		})
	}
	stopwords := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "any": {}, "bug": {}, "bugs": {}, "do": {}, "fix": {}, "for": {}, "help": {},
		"i": {}, "in": {}, "is": {}, "issue": {}, "issues": {}, "it": {}, "make": {}, "me": {}, "please": {}, "problem": {},
		"the": {}, "this": {}, "to": {}, "up": {}, "with": {},
	}
	keepShort := map[string]struct{}{"ui": {}, "ux": {}, "db": {}, "go": {}, "js": {}, "ts": {}, "api": {}}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		term := strings.Trim(strings.ToLower(match), "`\"'.,:;()[]{}")
		if term == "" {
			continue
		}
		if _, ok := stopwords[term]; ok {
			continue
		}
		if len(term) < 3 {
			if _, ok := keepShort[term]; !ok {
				continue
			}
		}
		out = append(out, term)
	}
	return dedupeNonEmpty(out, 10)
}

func rankPromptTriageCandidates(repoRoot string, terms []string, class intent.Class) ([]promptTriageCandidate, int, int, error) {
	repoRoot = filepath.Clean(repoRoot)
	candidates := make([]promptTriageCandidate, 0, 16)
	filesScanned := 0
	scannedTokenEstimate := 0
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".svn", ".hg", "node_modules", "vendor", "dist", "build", ".next", ".turbo", ".cache", "coverage", "tmp":
				if path != repoRoot {
					return fs.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isPromptTriageCandidateFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > promptTriageMaxFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinaryLike(data) {
			return nil
		}
		filesScanned++
		tokenEstimate := estimateTextTokens(string(data))
		scannedTokenEstimate += tokenEstimate
		score := scorePromptTriageCandidate(rel, data, terms, class)
		if score <= 0 {
			return nil
		}
		candidates = append(candidates, promptTriageCandidate{
			Path:          rel,
			Score:         score,
			TokenEstimate: tokenEstimate,
		})
		return nil
	})
	if err != nil {
		return nil, filesScanned, scannedTokenEstimate, fmt.Errorf("prompt triage walk repo: %w", err)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Score > candidates[j].Score
	})
	return candidates, filesScanned, scannedTokenEstimate, nil
}

func isPromptTriageCandidateFile(rel string) bool {
	base := filepath.Base(rel)
	switch base {
	case "Dockerfile", "Makefile", "README.md", "AGENTS.md":
		return true
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".json", ".yml", ".yaml", ".toml", ".md", ".html", ".css", ".scss", ".less", ".vue", ".svelte", ".sh", ".py", ".rb", ".java", ".kt", ".swift", ".rs", ".sql":
		return true
	default:
		return false
	}
}

func isBinaryLike(data []byte) bool {
	limit := len(data)
	if limit > 512 {
		limit = 512
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

func scorePromptTriageCandidate(rel string, data []byte, terms []string, class intent.Class) int {
	pathLower := strings.ToLower(filepath.ToSlash(rel))
	contentLower := strings.ToLower(string(data))
	score := 0
	for _, term := range terms {
		if term == "" {
			continue
		}
		if containsPathToken(pathLower, term) {
			score += 12
		} else if strings.Contains(pathLower, term) {
			score += 7
		}
		if count := strings.Count(contentLower, term); count > 0 {
			if count > 3 {
				count = 3
			}
			score += count * 3
		}
	}

	ext := strings.ToLower(filepath.Ext(rel))
	if promptTriageHasFrontendSignal(terms) {
		if promptTriageLooksFrontendPath(pathLower) {
			score += 8
		}
		if isPromptTriageFrontendExt(ext) {
			score += 5
		}
	}
	if promptTriageHasBackendSignal(terms) {
		if promptTriageLooksBackendPath(pathLower) {
			score += 8
		}
		if ext == ".go" || ext == ".ts" || ext == ".js" || ext == ".py" {
			score += 3
		}
	}
	if class == intent.ClassDebug && (strings.Contains(pathLower, "bug") || strings.Contains(pathLower, "error") || strings.Contains(pathLower, "fix")) {
		score += 3
	}
	return score
}

func containsPathToken(pathLower string, term string) bool {
	if pathLower == term {
		return true
	}
	for _, token := range strings.FieldsFunc(pathLower, func(r rune) bool {
		return r == '/' || r == '.' || r == '_' || r == '-'
	}) {
		if token == term {
			return true
		}
	}
	return false
}

func promptTriageHasFrontendSignal(terms []string) bool {
	for _, term := range terms {
		switch term {
		case "ui", "ux", "frontend", "component", "page", "screen", "modal", "dialog", "button", "layout", "style", "css", "theme":
			return true
		}
	}
	return false
}

func promptTriageHasBackendSignal(terms []string) bool {
	for _, term := range terms {
		switch term {
		case "api", "backend", "server", "endpoint", "handler", "route", "http", "service":
			return true
		}
	}
	return false
}

func promptTriageLooksFrontendPath(pathLower string) bool {
	return hasAny(pathLower, "/ui/", "/component", "/components/", "/page", "/pages/", "/screen", "/screens/", "/view", "/views/", "/styles", ".css", ".scss", ".tsx", ".jsx", ".vue", ".svelte")
}

func promptTriageLooksBackendPath(pathLower string) bool {
	return hasAny(pathLower, "/api/", "/server", "/handler", "/handlers/", "/route", "/routes/", "/service", "/services/", "/http/")
}

func isPromptTriageFrontendExt(ext string) bool {
	switch ext {
	case ".tsx", ".jsx", ".css", ".scss", ".less", ".html", ".vue", ".svelte":
		return true
	default:
		return false
	}
}

func candidatePaths(candidates []promptTriageCandidate, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := make([]string, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		out = append(out, candidate.Path)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func estimateSearchSpaceTokens(candidates []promptTriageCandidate, filesScanned int, scannedTokenEstimate int, searchTerms []string) int {
	total := 0
	for idx, candidate := range candidates {
		if idx >= promptTriageSearchSpaceFiles {
			break
		}
		total += candidate.TokenEstimate
	}
	total = max(total, scannedTokenEstimate)
	total += filesScanned * 12
	total += len(searchTerms) * 3
	return total
}

func recomputeIntentStateAfterPromptTriage(st intent.State, latest string, candidates []promptTriageCandidate) intent.State {
	out := st
	out.ScopeSummary = promptTriageScopeSummary(st.Class, candidates)
	out.NormalizedAction = promptTriageNormalizedAction(st.Class, latest, candidates)
	out.AmbiguityFlags = removeStrings(out.AmbiguityFlags, "scope_not_explicit")
	if strings.TrimSpace(out.NormalizedAction) != "" {
		out.AmbiguityFlags = removeStrings(out.AmbiguityFlags, "missing_explicit_action")
	}
	out.ClarificationQuestions = deriveClarificationQuestions(out.AmbiguityFlags)
	out.RequiresClarification = len(out.ClarificationQuestions) > 0

	lower := strings.ToLower(strings.TrimSpace(latest))
	out.Posture = deriveIntentPosture(out.Class, lower, out.AmbiguityFlags, out.RequiresClarification)
	out.ExecutionReadiness, out.ReadinessReason = deriveIntentReadiness(out.Posture, out.RequiresClarification)
	out.ProposedPhase = deriveProposedPhase(out.Class, out.Posture, out.ExecutionReadiness)
	out.Confidence = deriveConfidence(lower, out.Class, len(candidates), out.AmbiguityFlags, out.DoneCriteria)
	out.CompilationNotes = strings.TrimSpace(strings.TrimSpace(out.CompilationNotes) + " " + fmt.Sprintf("Prompt triage ranked %d candidate file(s) from repo-local search.", min(promptTriageMaxCandidateFiles, len(candidates))))
	return out
}

func promptTriageScopeSummary(class intent.Class, candidates []promptTriageCandidate) string {
	files := candidatePaths(candidates, 3)
	switch class {
	case intent.ClassDebug:
		return "bounded repair scope inferred from repo-local triage: " + strings.Join(files, ", ")
	case intent.ClassValidate:
		return "bounded validation scope inferred from repo-local triage: " + strings.Join(files, ", ")
	default:
		return "bounded scope inferred from repo-local triage: " + strings.Join(files, ", ")
	}
}

func promptTriageNormalizedAction(class intent.Class, latest string, candidates []promptTriageCandidate) string {
	files := candidatePaths(candidates, 2)
	targets := strings.Join(files, " and ")
	switch class {
	case intent.ClassDebug:
		return "investigate and repair the issue in " + targets
	case intent.ClassValidate:
		return "validate the likely impact around " + targets
	case intent.ClassReplan:
		return "replan the immediate change around " + targets
	default:
		if strings.TrimSpace(targets) == "" {
			return truncateSentence(strings.TrimSpace(latest), 140)
		}
		return "implement the bounded change in " + targets
	}
}

func promptTriageReason(flags []string, class intent.Class) string {
	if stringSliceContainsFold(flags, "scope_not_explicit") {
		return "scope_not_explicit"
	}
	if stringSliceContainsFold(flags, "missing_explicit_action") {
		return "missing_explicit_action"
	}
	switch class {
	case intent.ClassDebug:
		return "vague_debug_request"
	case intent.ClassValidate:
		return "vague_validation_request"
	default:
		return "vague_implementation_request"
	}
}

func promptTriageSummary(filesScanned int, candidateFiles []string, class intent.Class) string {
	action := "implementation"
	switch class {
	case intent.ClassDebug:
		action = "repair"
	case intent.ClassValidate:
		action = "validation"
	case intent.ClassReplan:
		action = "planning"
	}
	return fmt.Sprintf("searched %d repo-local file(s) and narrowed %s context to %d ranked candidate(s)", filesScanned, action, len(candidateFiles))
}

func augmentWorkerFramingWithPromptTriage(base string, candidateFiles []string) string {
	if len(candidateFiles) == 0 {
		return base
	}
	extra := fmt.Sprintf("Tuku pre-triaged a vague request; start with ranked candidate files %s before widening scope.", strings.Join(candidateFiles, ", "))
	base = strings.TrimSpace(base)
	if base == "" {
		return extra
	}
	return base + " " + extra
}

func finalizePromptTriageTelemetry(in brief.PromptTriage, input brief.BuildInput, pack contextdomain.Pack) brief.PromptTriage {
	if !in.Applied {
		return in
	}
	out := clonePromptTriage(in)
	out.SelectedContextTokenEstimate = estimateContextPackTokens(pack)
	out.RewrittenPromptTokenEstimate = estimateRewrittenBriefTokens(input, out)
	if out.SearchSpaceTokenEstimate > out.SelectedContextTokenEstimate {
		out.ContextTokenSavingsEstimate = out.SearchSpaceTokenEstimate - out.SelectedContextTokenEstimate
	}
	return out
}

func estimateRewrittenBriefTokens(input brief.BuildInput, triage brief.PromptTriage) int {
	parts := []string{
		strings.TrimSpace(input.Goal),
		strings.TrimSpace(input.RequestedOutcome),
		strings.TrimSpace(input.NormalizedAction),
		strings.TrimSpace(input.ScopeSummary),
		strings.Join(input.Constraints, "\n"),
		strings.Join(input.DoneCriteria, "\n"),
		strings.TrimSpace(input.WorkerFraming),
	}
	if len(triage.CandidateFiles) > 0 {
		parts = append(parts, strings.Join(triage.CandidateFiles, "\n"))
	}
	return estimateTextTokens(strings.Join(parts, "\n"))
}

func estimateContextPackTokens(pack contextdomain.Pack) int {
	total := estimateTextTokens(strings.Join(pack.IncludedFiles, "\n"))
	for _, snippet := range pack.IncludedSnippets {
		total += estimateTextTokens(snippet.Path)
		total += estimateTextTokens(snippet.Content)
	}
	return total
}

func estimateTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return max(1, (len([]rune(text))+3)/4)
}

func clonePromptTriage(in brief.PromptTriage) brief.PromptTriage {
	return brief.PromptTriage{
		Applied:                      in.Applied,
		Reason:                       in.Reason,
		Summary:                      in.Summary,
		SearchTerms:                  append([]string{}, in.SearchTerms...),
		CandidateFiles:               append([]string{}, in.CandidateFiles...),
		FilesScanned:                 in.FilesScanned,
		RawPromptTokenEstimate:       in.RawPromptTokenEstimate,
		RewrittenPromptTokenEstimate: in.RewrittenPromptTokenEstimate,
		SearchSpaceTokenEstimate:     in.SearchSpaceTokenEstimate,
		SelectedContextTokenEstimate: in.SelectedContextTokenEstimate,
		ContextTokenSavingsEstimate:  in.ContextTokenSavingsEstimate,
	}
}

func hasMeaningfulPromptTriage(in brief.PromptTriage) bool {
	return in.Applied || strings.TrimSpace(in.Reason) != "" || strings.TrimSpace(in.Summary) != "" || len(in.SearchTerms) > 0 || len(in.CandidateFiles) > 0 || in.FilesScanned > 0 || in.RawPromptTokenEstimate > 0 || in.RewrittenPromptTokenEstimate > 0 || in.SearchSpaceTokenEstimate > 0 || in.SelectedContextTokenEstimate > 0 || in.ContextTokenSavingsEstimate > 0
}

func removeStrings(values []string, target string) []string {
	out := make([]string, 0, len(values))
	target = strings.TrimSpace(strings.ToLower(target))
	for _, value := range values {
		if strings.TrimSpace(strings.ToLower(value)) == target {
			continue
		}
		out = append(out, value)
	}
	return dedupeNonEmpty(out, len(values))
}

func stringSliceContainsFold(values []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, value := range values {
		if strings.TrimSpace(strings.ToLower(value)) == target {
			return true
		}
	}
	return false
}
