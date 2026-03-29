package orchestrator

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
)

var scopeTokenPattern = regexp.MustCompile(`(?:[A-Za-z0-9._-]+/)+[A-Za-z0-9._-]+|[A-Za-z0-9._-]+\.[A-Za-z0-9]{1,8}`)

// IntentStubCompiler now hosts the v1 deterministic intent compiler.
// The constructor name is preserved for compatibility with existing wiring.
type IntentStubCompiler struct{}

func NewIntentCompilerV1() *IntentStubCompiler {
	return &IntentStubCompiler{}
}

func NewIntentStubCompiler() *IntentStubCompiler {
	return NewIntentCompilerV1()
}

func (c *IntentStubCompiler) Compile(input intent.CompileInput) (intent.State, error) {
	latest := strings.TrimSpace(input.LatestMessage)
	latestLower := strings.ToLower(latest)
	recent := boundedRecentMessages(input.RecentMessages, latest, 12)
	evidenceCount := len(recent)
	if evidenceCount == 0 && latest != "" {
		evidenceCount = 1
	}

	class := classifyIntentClass(latestLower)
	objective := deriveIntentObjective(latest, input.CurrentGoal)
	requestedOutcome := deriveRequestedOutcome(class, latest)
	normalizedAction := deriveNormalizedAction(class, latest, requestedOutcome)

	explicitConstraints := extractExplicitConstraints(recent)
	doneCriteria := extractDoneCriteria(recent)
	scopeSummary, scopeSignals := deriveScopeSummary(recent, class)
	ambiguityFlags := deriveAmbiguityFlags(latestLower, class, scopeSignals)
	clarificationQuestions := deriveClarificationQuestions(ambiguityFlags)
	requiresClarification := len(clarificationQuestions) > 0

	posture := deriveIntentPosture(class, latestLower, ambiguityFlags, requiresClarification)
	readiness, readinessReason := deriveIntentReadiness(posture, requiresClarification)
	proposedPhase := deriveProposedPhase(class, posture, readiness)

	confidence := deriveConfidence(latestLower, class, scopeSignals, ambiguityFlags, doneCriteria)
	notes := fmt.Sprintf("Derived from latest operator message with %d bounded recent message(s).", evidenceCount)
	if requiresClarification {
		notes += " Clarification signals remain explicit in this bounded window."
	}

	return intent.State{
		Version:                 2,
		IntentID:                common.IntentID(newID("int")),
		TaskID:                  input.TaskID,
		Class:                   class,
		Posture:                 posture,
		ExecutionReadiness:      readiness,
		Objective:               objective,
		RequestedOutcome:        requestedOutcome,
		NormalizedAction:        normalizedAction,
		ScopeSummary:            scopeSummary,
		ExplicitConstraints:     explicitConstraints,
		DoneCriteria:            doneCriteria,
		AmbiguityFlags:          ambiguityFlags,
		ClarificationQuestions:  clarificationQuestions,
		RequiresClarification:   requiresClarification,
		ReadinessReason:         readinessReason,
		CompilationNotes:        notes,
		BoundedEvidenceMessages: evidenceCount,
		Confidence:              confidence,
		SourceMessageIDs:        []common.MessageID{},
		ProposedPhase:           proposedPhase,
		CreatedAt:               time.Now().UTC(),
	}, nil
}

func boundedRecentMessages(recent []string, latest string, limit int) []string {
	out := make([]string, 0, limit)
	seenLatest := false
	for _, item := range recent {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if strings.TrimSpace(latest) != "" && trimmed == strings.TrimSpace(latest) {
			seenLatest = true
		}
		out = append(out, trimmed)
	}
	if strings.TrimSpace(latest) != "" && !seenLatest {
		out = append(out, strings.TrimSpace(latest))
	}
	if len(out) <= limit {
		return out
	}
	return append([]string{}, out[len(out)-limit:]...)
}

func classifyIntentClass(messageLower string) intent.Class {
	switch {
	case hasAny(messageLower, "pause", "stop", "hold off", "wait for now"):
		return intent.ClassPause
	case hasAny(messageLower, "status", "summarize", "summary", "explain", "what happened", "where are we"):
		return intent.ClassExplain
	case hasAny(messageLower, "validate", "validation", "verify", "check", "test", "tests", "smoke test", "regression"):
		return intent.ClassValidate
	case hasAny(messageLower, "fix", "bug", "broken", "failure", "failing", "repair", "recover", "recovery"):
		return intent.ClassDebug
	case hasAny(messageLower, "approve", "approved", "lgtm"):
		return intent.ClassApproval
	case hasAny(messageLower, "replan", "re-plan", "change plan", "adjust scope"):
		return intent.ClassReplan
	case hasAny(messageLower, "continue", "proceed"):
		return intent.ClassContinueTask
	case hasAny(messageLower, "mark complete", "mark done"):
		return intent.ClassComplete
	default:
		return intent.ClassImplement
	}
}

func deriveIntentObjective(latest, currentGoal string) string {
	trimmed := strings.TrimSpace(latest)
	if trimmed == "" {
		goal := strings.TrimSpace(currentGoal)
		if goal != "" {
			return goal
		}
		return "Clarify the current task objective from operator input."
	}
	firstLine := strings.TrimSpace(strings.Split(trimmed, "\n")[0])
	if firstLine == "" {
		firstLine = trimmed
	}
	return truncateSentence(firstLine, 180)
}

func deriveRequestedOutcome(class intent.Class, latest string) string {
	switch class {
	case intent.ClassValidate:
		return "produce a validation-focused result summary with bounded evidence"
	case intent.ClassDebug:
		return "produce a repair-oriented change set with bounded evidence"
	case intent.ClassExplain:
		return "produce an operator-facing status/explanation summary"
	case intent.ClassPause:
		return "pause active execution and preserve continuity truth"
	case intent.ClassContinueTask:
		return "continue execution from current canonical task state"
	case intent.ClassReplan:
		return "restructure task scope and execution approach conservatively"
	default:
		if text := strings.TrimSpace(latest); text != "" {
			return truncateSentence(text, 140)
		}
		return "prepare the next bounded implementation step"
	}
}

func deriveNormalizedAction(class intent.Class, latest, requestedOutcome string) string {
	switch class {
	case intent.ClassValidate:
		return "prepare validation execution and evidence review"
	case intent.ClassDebug:
		return "debug and patch the reported issue"
	case intent.ClassExplain:
		return "summarize current task and continuity state"
	case intent.ClassPause:
		return "pause active work and retain continuity evidence"
	case intent.ClassContinueTask:
		return "continue from current task continuity state"
	case intent.ClassReplan:
		return "replan scope and execution sequence conservatively"
	default:
		text := strings.TrimSpace(latest)
		if text == "" {
			text = strings.TrimSpace(requestedOutcome)
		}
		if text == "" {
			return "prepare bounded implementation execution"
		}
		return truncateSentence(text, 140)
	}
}

func extractExplicitConstraints(recent []string) []string {
	items := make([]string, 0, 8)
	for _, msg := range recent {
		lines := splitCandidateLines(msg)
		for _, line := range lines {
			lower := strings.ToLower(line)
			switch {
			case strings.Contains(lower, "do not "):
				items = append(items, normalizeConstraintLine(line))
			case strings.Contains(lower, "don't "):
				items = append(items, normalizeConstraintLine(line))
			case strings.Contains(lower, "without "):
				items = append(items, normalizeConstraintLine(line))
			case strings.Contains(lower, "must not "):
				items = append(items, normalizeConstraintLine(line))
			}
		}
	}
	return dedupeNonEmpty(items, 8)
}

func extractDoneCriteria(recent []string) []string {
	items := make([]string, 0, 6)
	for _, msg := range recent {
		lines := splitCandidateLines(msg)
		for _, line := range lines {
			lower := strings.ToLower(line)
			switch {
			case strings.Contains(lower, "done when"):
				items = append(items, normalizeConstraintLine(line))
			case strings.Contains(lower, "success when"):
				items = append(items, normalizeConstraintLine(line))
			case strings.Contains(lower, "must include"):
				items = append(items, normalizeConstraintLine(line))
			case strings.Contains(lower, "should include"):
				items = append(items, normalizeConstraintLine(line))
			}
		}
	}
	return dedupeNonEmpty(items, 6)
}

func deriveScopeSummary(recent []string, class intent.Class) (string, int) {
	signalMap := map[string]struct{}{}
	signals := make([]string, 0, 8)
	for _, msg := range recent {
		matches := scopeTokenPattern.FindAllString(msg, -1)
		for _, match := range matches {
			token := strings.TrimSpace(strings.Trim(match, "`\"'.,:;()[]{}"))
			if token == "" {
				continue
			}
			if _, ok := signalMap[token]; ok {
				continue
			}
			signalMap[token] = struct{}{}
			signals = append(signals, token)
			if len(signals) >= 8 {
				break
			}
		}
		if len(signals) >= 8 {
			break
		}
	}
	if len(signals) > 0 {
		limit := min(3, len(signals))
		return "bounded scope signals: " + strings.Join(signals[:limit], ", "), len(signals)
	}
	switch class {
	case intent.ClassValidate:
		return "bounded scope inferred as validation over current task changes (no explicit file scope provided)", 0
	case intent.ClassDebug:
		return "bounded scope inferred as failing behavior and nearby repair targets (explicit file scope not provided)", 0
	default:
		return "bounded scope not explicitly provided; operator clarification may improve execution targeting", 0
	}
}

func deriveAmbiguityFlags(messageLower string, class intent.Class, scopeSignals int) []string {
	flags := []string{}
	if strings.Contains(messageLower, "?") {
		flags = append(flags, "contains_open_question")
	}
	if hasAny(messageLower, "maybe", "not sure", "unclear", "explore", "brainstorm", "idea", "half-formed", "rough plan") {
		flags = append(flags, "exploratory_language")
	}
	if hasAny(messageLower, "something", "anything", "whatever") {
		flags = append(flags, "outcome_underspecified")
	}
	if !hasExplicitActionVerb(messageLower) && class != intent.ClassExplain && class != intent.ClassPause {
		flags = append(flags, "missing_explicit_action")
	}
	if scopeSignals == 0 && (class == intent.ClassImplement || class == intent.ClassDebug || class == intent.ClassValidate || class == intent.ClassReplan) {
		flags = append(flags, "scope_not_explicit")
	}
	return dedupeNonEmpty(flags, 6)
}

func deriveClarificationQuestions(ambiguityFlags []string) []string {
	questions := make([]string, 0, 4)
	for _, flag := range ambiguityFlags {
		switch flag {
		case "missing_explicit_action":
			questions = append(questions, "What concrete next action should bounded execution perform?")
		case "scope_not_explicit":
			questions = append(questions, "Which files or subsystems are explicitly in scope?")
		case "outcome_underspecified":
			questions = append(questions, "What result should count as done for the immediate step?")
		case "contains_open_question":
			questions = append(questions, "Is this request exploratory, or should Tuku prepare an execution-ready brief now?")
		}
	}
	return dedupeNonEmpty(questions, 4)
}

func deriveIntentPosture(class intent.Class, messageLower string, ambiguityFlags []string, requiresClarification bool) intent.Posture {
	if requiresClarification && hasAny(messageLower, "plan", "planning", "approach", "design", "milestone", "roadmap") {
		return intent.PosturePlanning
	}
	switch class {
	case intent.ClassDebug:
		return intent.PostureRepairRecovery
	case intent.ClassValidate:
		return intent.PostureValidationFocused
	case intent.ClassReplan:
		return intent.PosturePlanning
	case intent.ClassExplain, intent.ClassApproval:
		return intent.PostureExploratoryAmbiguous
	case intent.ClassPause:
		return intent.PostureClarificationNeeded
	}
	if hasAny(messageLower, "plan", "planning", "approach", "design", "brainstorm", "milestone", "roadmap") {
		return intent.PosturePlanning
	}
	if requiresClarification || len(ambiguityFlags) > 0 {
		return intent.PostureExploratoryAmbiguous
	}
	return intent.PostureExecutionReady
}

func deriveIntentReadiness(posture intent.Posture, requiresClarification bool) (intent.Readiness, string) {
	if requiresClarification || posture == intent.PostureClarificationNeeded || posture == intent.PostureExploratoryAmbiguous {
		return intent.ReadinessClarificationNeeded, "clarification signals remain in bounded recent operator input"
	}
	switch posture {
	case intent.PosturePlanning:
		return intent.ReadinessPlanningInProgress, "request remains in planning posture within bounded recent operator input"
	case intent.PostureValidationFocused:
		return intent.ReadinessValidationFocused, "request is validation-focused in bounded recent operator input"
	case intent.PostureRepairRecovery:
		return intent.ReadinessRepairRecovery, "request is repair/recovery-focused in bounded recent operator input"
	default:
		return intent.ReadinessExecutionReady, "bounded recent operator input is sufficiently specific for execution preparation"
	}
}

func deriveProposedPhase(class intent.Class, posture intent.Posture, readiness intent.Readiness) phase.Phase {
	if class == intent.ClassPause {
		return phase.PhasePaused
	}
	if posture == intent.PostureValidationFocused || readiness == intent.ReadinessValidationFocused {
		return phase.PhaseValidating
	}
	return phase.PhaseInterpreting
}

func deriveConfidence(messageLower string, class intent.Class, scopeSignals int, ambiguityFlags []string, doneCriteria []string) float64 {
	conf := 0.62
	if hasExplicitActionVerb(messageLower) {
		conf += 0.14
	}
	if scopeSignals > 0 {
		conf += 0.08
	}
	if len(doneCriteria) > 0 {
		conf += 0.05
	}
	switch class {
	case intent.ClassValidate, intent.ClassDebug, intent.ClassContinueTask:
		conf += 0.04
	case intent.ClassExplain:
		conf += 0.02
	}
	conf -= 0.08 * float64(len(ambiguityFlags))
	conf = math.Max(0.20, math.Min(0.95, conf))
	return math.Round(conf*100) / 100
}

func splitCandidateLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "-"), "*"), "•"))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func normalizeConstraintLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	return truncateSentence(trimmed, 180)
}

func dedupeNonEmpty(values []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, min(limit, len(values)))
	for _, item := range values {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func hasExplicitActionVerb(messageLower string) bool {
	return hasAny(
		messageLower,
		"implement", "build", "add", "create", "update", "refactor", "wire", "integrate",
		"fix", "patch", "debug", "test", "validate", "verify", "check", "review", "continue",
	)
}

func truncateSentence(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	cut := string(runes[:limit])
	lastSpace := strings.LastIndex(cut, " ")
	if lastSpace > 0 {
		cut = strings.TrimSpace(cut[:lastSpace])
	}
	return strings.TrimSpace(cut) + "..."
}
