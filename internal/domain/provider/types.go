package provider

import (
	"math"
	"strings"
)

type WorkerKind string

const (
	WorkerUnknown WorkerKind = "unknown"
	WorkerCodex   WorkerKind = "codex"
	WorkerClaude  WorkerKind = "claude"
)

type CapabilitySet struct {
	LocalCLI             bool `json:"local_cli,omitempty"`
	InteractiveShell     bool `json:"interactive_shell,omitempty"`
	DirectExecution      bool `json:"direct_execution,omitempty"`
	ReattachSessions     bool `json:"reattach_sessions,omitempty"`
	ValidationFriendly   bool `json:"validation_friendly,omitempty"`
	StrongPlanning       bool `json:"strong_planning,omitempty"`
	StrongImplementation bool `json:"strong_implementation,omitempty"`
	StrongReview         bool `json:"strong_review,omitempty"`
	HandoffTarget        bool `json:"handoff_target,omitempty"`
}

type Candidate struct {
	Worker       WorkerKind    `json:"worker"`
	Title        string        `json:"title,omitempty"`
	Summary      string        `json:"summary,omitempty"`
	Capabilities CapabilitySet `json:"capabilities,omitempty"`
}

type Signals struct {
	NormalizedTaskType    string     `json:"normalized_task_type,omitempty"`
	BriefPosture          string     `json:"brief_posture,omitempty"`
	RequiresClarification bool       `json:"requires_clarification,omitempty"`
	ValidatorCount        int        `json:"validator_count,omitempty"`
	RankedTargetCount     int        `json:"ranked_target_count,omitempty"`
	EstimatedTokenSavings int        `json:"estimated_token_savings,omitempty"`
	ConfidenceLevel       string     `json:"confidence_level,omitempty"`
	LatestRunWorker       WorkerKind `json:"latest_run_worker,omitempty"`
	LatestRunStatus       string     `json:"latest_run_status,omitempty"`
	HandoffTarget         WorkerKind `json:"handoff_target,omitempty"`
	HandoffStatus         string     `json:"handoff_status,omitempty"`
	RememberedWorker      WorkerKind `json:"remembered_worker,omitempty"`
}

type Recommendation struct {
	Worker     WorkerKind  `json:"worker"`
	Confidence string      `json:"confidence,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Why        []string    `json:"why,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
}

func Registry() []Candidate {
	return []Candidate{
		{
			Worker:  WorkerCodex,
			Title:   "Codex",
			Summary: "Best when Tuku has already narrowed the repo and the next step is implementation-heavy.",
			Capabilities: CapabilitySet{
				LocalCLI:             true,
				InteractiveShell:     true,
				DirectExecution:      true,
				ReattachSessions:     true,
				ValidationFriendly:   true,
				StrongImplementation: true,
				HandoffTarget:        false,
			},
		},
		{
			Worker:  WorkerClaude,
			Title:   "Claude",
			Summary: "Best when the task is still planning, review, or continuity-heavy before mutation begins.",
			Capabilities: CapabilitySet{
				LocalCLI:           true,
				InteractiveShell:   true,
				DirectExecution:    true,
				ReattachSessions:   true,
				ValidationFriendly: false,
				StrongPlanning:     true,
				StrongReview:       true,
				HandoffTarget:      true,
			},
		},
	}
}

func Label(worker WorkerKind) string {
	switch worker {
	case WorkerClaude:
		return "Claude"
	case WorkerCodex:
		return "Codex"
	default:
		return "Unknown"
	}
}

func Recommend(signals Signals) Recommendation {
	codexScore := 0.60
	claudeScore := 0.55
	codexReasons := []string{}
	claudeReasons := []string{}

	taskType := strings.ToUpper(strings.TrimSpace(signals.NormalizedTaskType))
	posture := strings.ToUpper(strings.TrimSpace(signals.BriefPosture))
	confidence := strings.ToLower(strings.TrimSpace(signals.ConfidenceLevel))
	latestRunStatus := strings.ToUpper(strings.TrimSpace(signals.LatestRunStatus))
	handoffStatus := strings.ToUpper(strings.TrimSpace(signals.HandoffStatus))

	switch {
	case signals.HandoffTarget == WorkerClaude && handoffStatus != "" && handoffStatus != "RESOLVED":
		claudeScore += 0.45
		claudeReasons = append(claudeReasons, "active continuity already points to Claude")
	case signals.HandoffTarget == WorkerCodex && handoffStatus != "" && handoffStatus != "RESOLVED":
		codexScore += 0.45
		codexReasons = append(codexReasons, "active continuity already points to Codex")
	}

	switch {
	case signals.LatestRunWorker == WorkerClaude && latestRunStatus == "RUNNING":
		claudeScore += 0.45
		claudeReasons = append(claudeReasons, "the current active run is already on Claude")
	case signals.LatestRunWorker == WorkerCodex && latestRunStatus == "RUNNING":
		codexScore += 0.45
		codexReasons = append(codexReasons, "the current active run is already on Codex")
	}

	if containsAny(taskType, "PLAN", "ANALYSIS", "REVIEW", "AUDIT", "RESEARCH") {
		claudeScore += 0.22
		claudeReasons = append(claudeReasons, "the task is still planning/review heavy")
	}
	if containsAny(taskType, "BUG", "FIX", "REPAIR", "IMPLEMENT", "EXECUTE", "BUILD", "UI", "FEATURE") {
		codexScore += 0.22
		codexReasons = append(codexReasons, "the task is implementation heavy and already narrowed")
	}

	switch posture {
	case "PLANNING_ORIENTED", "CLARIFICATION_NEEDED":
		claudeScore += 0.16
		claudeReasons = append(claudeReasons, "brief posture still favors planning before mutation")
	case "EXECUTION_READY", "REPAIR_ORIENTED":
		codexScore += 0.16
		codexReasons = append(codexReasons, "brief posture is ready for direct repo changes")
	case "VALIDATION_ORIENTED":
		if signals.ValidatorCount > 0 {
			codexScore += 0.12
			codexReasons = append(codexReasons, "bounded validators are ready to run immediately after execution")
		} else {
			claudeScore += 0.08
			claudeReasons = append(claudeReasons, "validation posture still benefits from review-oriented reasoning")
		}
	}

	if signals.RequiresClarification {
		claudeScore += 0.10
		claudeReasons = append(claudeReasons, "open clarification risk favors a planning-oriented worker")
	}
	if signals.ValidatorCount > 0 {
		codexScore += 0.08
		codexReasons = append(codexReasons, "validator plan is concrete enough for direct execution")
	}
	if signals.RankedTargetCount >= 2 {
		codexScore += 0.06
		codexReasons = append(codexReasons, "Tuku already ranked multiple likely targets")
	}
	if signals.EstimatedTokenSavings >= 200 {
		codexScore += 0.04
		codexReasons = append(codexReasons, "Tuku already compressed enough context to make execution efficient")
	}
	if confidence == "low" {
		claudeScore += 0.05
		claudeReasons = append(claudeReasons, "low confidence favors more review-oriented reasoning before execution")
	}

	switch signals.RememberedWorker {
	case WorkerClaude:
		claudeScore += 0.03
		claudeReasons = append(claudeReasons, "recent operator preference leaned toward Claude")
	case WorkerCodex:
		codexScore += 0.03
		codexReasons = append(codexReasons, "recent operator preference leaned toward Codex")
	}

	chosen := WorkerCodex
	reasons := codexReasons
	scoreGap := codexScore - claudeScore
	if claudeScore > codexScore {
		chosen = WorkerClaude
		reasons = claudeReasons
		scoreGap = claudeScore - codexScore
	}
	if len(reasons) == 0 {
		reasons = []string{"defaulting to Codex for direct local execution"}
		if chosen == WorkerClaude {
			reasons = []string{"defaulting to Claude for planning-oriented continuity"}
		}
	}

	confidenceBand := "medium"
	switch {
	case scoreGap >= 0.25:
		confidenceBand = "high"
	case scoreGap < 0.10:
		confidenceBand = "low"
	}

	return Recommendation{
		Worker:     chosen,
		Confidence: confidenceBand,
		Reason:     reasons[0],
		Why:        dedupeNonEmpty(reasons, 3),
		Candidates: Registry(),
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToUpper(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func dedupeNonEmpty(values []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, int(math.Min(float64(limit), float64(len(values)))))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
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
