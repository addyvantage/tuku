package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/common"
	"tuku/internal/domain/promptir"
	"tuku/internal/runtime/process"
)

type Adapter struct {
	binary  string
	args    []string
	timeout time.Duration
	runner  process.Runner
}

type Config struct {
	Binary  string
	Args    []string
	Timeout time.Duration
	Runner  process.Runner
}

const codexSafeReasoningEffortConfig = `model_reasoning_effort="high"`

func NewAdapter() *Adapter {
	cfg := Config{}
	cfg.Binary = strings.TrimSpace(os.Getenv("TUKU_CODEX_BIN"))
	if cfg.Binary == "" {
		cfg.Binary = "codex"
	}
	if envArgs := strings.TrimSpace(os.Getenv("TUKU_CODEX_ARGS")); envArgs != "" {
		cfg.Args = strings.Fields(envArgs)
	}
	cfg.Timeout = 120 * time.Second
	if sec := strings.TrimSpace(os.Getenv("TUKU_CODEX_TIMEOUT_SEC")); sec != "" {
		if n, err := strconv.Atoi(sec); err == nil && n > 0 {
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}
	cfg.Runner = process.NewLocalRunner()
	return NewAdapterWithConfig(cfg)
}

func NewAdapterWithConfig(cfg Config) *Adapter {
	if strings.TrimSpace(cfg.Binary) == "" {
		cfg.Binary = "codex"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.Runner == nil {
		cfg.Runner = process.NewLocalRunner()
	}
	return &Adapter{binary: cfg.Binary, args: append([]string{}, cfg.Args...), timeout: cfg.Timeout, runner: cfg.Runner}
}

func (a *Adapter) Name() adapter_contract.WorkerKind {
	return adapter_contract.WorkerCodex
}

func (a *Adapter) Execute(ctx context.Context, req adapter_contract.ExecutionRequest, sink adapter_contract.WorkerEventSink) (adapter_contract.ExecutionResult, error) {
	startedAt := time.Now().UTC()
	commandArgs := a.commandArgs()
	result := adapter_contract.ExecutionResult{
		WorkerRunID: commonWorkerRunID(req.RunID),
		StartedAt:   startedAt,
		Command:     a.binary,
		Args:        append([]string{}, commandArgs...),
	}

	if _, err := exec.LookPath(a.binary); err != nil {
		result.EndedAt = time.Now().UTC()
		result.ErrorMessage = fmt.Sprintf("codex executable not found: %v", err)
		if sink != nil {
			_ = sink.OnWorkerEvent(ctx, adapter_contract.WorkerEvent{Type: adapter_contract.WorkerEventFailed, RunID: req.RunID, Payload: result.ErrorMessage})
		}
		return result, fmt.Errorf(result.ErrorMessage)
	}

	prompt := buildPrompt(req)
	before, beforeErr := changedFiles(req.RepoAnchor.WorktreePath)

	if sink != nil {
		_ = sink.OnWorkerEvent(ctx, adapter_contract.WorkerEvent{Type: adapter_contract.WorkerEventStarted, RunID: req.RunID, Payload: "codex run started"})
	}

	runCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	procRes, runErr := a.runner.Run(runCtx, process.Spec{
		Command:    a.binary,
		Args:       commandArgs,
		WorkingDir: req.RepoAnchor.WorktreePath,
		Stdin:      prompt,
	})

	endedAt := time.Now().UTC()
	after, afterErr := changedFiles(req.RepoAnchor.WorktreePath)
	changedHints, changedSemantics := changedByRunHint(before, after, beforeErr, afterErr)

	result.ExitCode = procRes.ExitCode
	result.Stdout = procRes.Stdout
	result.Stderr = procRes.Stderr
	result.EndedAt = endedAt
	result.ChangedFiles = changedHints
	result.ChangedFilesSemantics = changedSemantics
	result.ValidationSignals = parseValidationSignals(procRes.Stdout, procRes.Stderr)
	result.Summary = summarize(procRes.Stdout, procRes.Stderr, procRes.ExitCode)
	result.StructuredSummary = parseStructuredSummary(procRes.Stdout)
	result.OutputArtifactRef = ""

	if sink != nil {
		_ = sink.OnWorkerEvent(ctx, adapter_contract.WorkerEvent{Type: adapter_contract.WorkerEventOutput, RunID: req.RunID, Payload: fmt.Sprintf("stdout_bytes=%d stderr_bytes=%d", len(procRes.Stdout), len(procRes.Stderr))})
	}

	if runErr != nil {
		result.ErrorMessage = runErr.Error()
		if sink != nil {
			_ = sink.OnWorkerEvent(ctx, adapter_contract.WorkerEvent{Type: adapter_contract.WorkerEventFailed, RunID: req.RunID, Payload: runErr.Error()})
		}
		return result, runErr
	}

	if sink != nil {
		_ = sink.OnWorkerEvent(ctx, adapter_contract.WorkerEvent{Type: adapter_contract.WorkerEventCompleted, RunID: req.RunID, Payload: fmt.Sprintf("exit_code=%d", result.ExitCode)})
	}
	return result, nil
}

func (a *Adapter) commandArgs() []string {
	args := ensureCodexReasoningEffortArgs(append([]string{}, a.args...))
	for _, arg := range args {
		switch strings.TrimSpace(strings.ToLower(arg)) {
		case "exec", "help", "review", "resume":
			return args
		}
	}
	return append(args, "exec", "--color", "never", "-")
}

func ensureCodexReasoningEffortArgs(args []string) []string {
	if hasCodexReasoningEffortOverride(args) {
		return append([]string{}, args...)
	}
	return append([]string{"-c", codexSafeReasoningEffortConfig}, args...)
}

func hasCodexReasoningEffortOverride(args []string) bool {
	for idx, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if strings.Contains(lower, "model_reasoning_effort") {
			return true
		}
		if (lower == "-c" || lower == "--config") && idx+1 < len(args) {
			if strings.Contains(strings.ToLower(strings.TrimSpace(args[idx+1])), "model_reasoning_effort") {
				return true
			}
		}
	}
	return false
}

func buildPrompt(req adapter_contract.ExecutionRequest) string {
	b := req.Brief
	contextFiles := "none"
	if len(req.ContextPack.IncludedFiles) > 0 {
		contextFiles = strings.Join(req.ContextPack.IncludedFiles, ", ")
	}
	contextRationale := "none"
	if len(req.ContextPack.SelectionRationale) > 0 {
		contextRationale = strings.Join(req.ContextPack.SelectionRationale, " | ")
	}
	taskMemorySummary := "none"
	if strings.TrimSpace(req.TaskMemory.Summary) != "" {
		taskMemorySummary = strings.TrimSpace(req.TaskMemory.Summary)
	}
	sections := []string{
		"You are Codex executing one bounded Tuku run.",
		"Follow the brief exactly and do not start unrelated work.",
		fmt.Sprintf("Task ID: %s", req.TaskID),
		fmt.Sprintf("Run ID: %s", req.RunID),
		fmt.Sprintf("Brief ID: %s", b.BriefID),
		fmt.Sprintf("Objective: %s", b.Objective),
		fmt.Sprintf("Normalized action: %s", b.NormalizedAction),
		fmt.Sprintf("Constraints: %s", strings.Join(b.Constraints, "; ")),
		fmt.Sprintf("Done criteria: %s", strings.Join(b.DoneCriteria, "; ")),
		fmt.Sprintf("Repo root: %s", req.RepoAnchor.RepoRoot),
		fmt.Sprintf("Worktree: %s", req.RepoAnchor.WorktreePath),
		fmt.Sprintf("Branch: %s", req.RepoAnchor.BranchName),
		fmt.Sprintf("Head SHA: %s", req.RepoAnchor.HeadSHA),
		fmt.Sprintf("Policy profile: %s", req.PolicyProfileID),
		fmt.Sprintf("AGENTS checksum: %s", req.AgentsChecksum),
		fmt.Sprintf("AGENTS constraints: %s", req.AgentsInstructions),
		fmt.Sprintf("Context summary: %s", req.ContextSummary),
		fmt.Sprintf("Context pack: id=%s mode=%s files=%s", req.ContextPack.ContextPackID, req.ContextPack.Mode, contextFiles),
		fmt.Sprintf("Context rationale: %s", contextRationale),
		fmt.Sprintf("Task memory: id=%s source=%s", req.TaskMemory.MemoryID, req.TaskMemory.Source),
		fmt.Sprintf("Task memory summary: %s", taskMemorySummary),
		fmt.Sprintf("Task memory compaction: full_history_tokens=%d resume_prompt_tokens=%d ratio=%.2f",
			req.TaskMemory.FullHistoryTokenEstimate,
			req.TaskMemory.ResumePromptTokenEstimate,
			req.TaskMemory.MemoryCompactionRatio,
		),
		"Return concise implementation summary, tests run, and unknowns.",
	}
	if len(req.TaskMemory.ConfirmedFacts) > 0 {
		sections = append(sections, "Task memory facts: "+strings.Join(req.TaskMemory.ConfirmedFacts, " | "))
	}
	if len(req.TaskMemory.ValidatorsRun) > 0 {
		sections = append(sections, "Task memory validators: "+strings.Join(req.TaskMemory.ValidatorsRun, ", "))
	}
	if len(req.TaskMemory.CandidateFiles) > 0 {
		sections = append(sections, "Task memory candidates: "+strings.Join(req.TaskMemory.CandidateFiles, ", "))
	}
	if strings.TrimSpace(req.TaskMemory.LastBlocker) != "" {
		sections = append(sections, "Task memory blocker: "+strings.TrimSpace(req.TaskMemory.LastBlocker))
	}
	if strings.TrimSpace(req.TaskMemory.NextSuggestedStep) != "" {
		sections = append(sections, "Task memory next step: "+strings.TrimSpace(req.TaskMemory.NextSuggestedStep))
	}
	if len(req.TaskMemory.Unknowns) > 0 {
		sections = append(sections, "Task memory unknowns: "+strings.Join(req.TaskMemory.Unknowns, " | "))
	}
	if b.PromptTriage.Applied {
		triageSummary := strings.TrimSpace(b.PromptTriage.Summary)
		if triageSummary == "" {
			triageSummary = strings.TrimSpace(b.PromptTriage.Reason)
		}
		triageSearchTerms := strings.Join(b.PromptTriage.SearchTerms, ", ")
		if strings.TrimSpace(triageSearchTerms) == "" {
			triageSearchTerms = "none"
		}
		triageCandidates := strings.Join(b.PromptTriage.CandidateFiles, ", ")
		if strings.TrimSpace(triageCandidates) == "" {
			triageCandidates = "none"
		}
		sections = append(sections,
			fmt.Sprintf("Prompt triage: %s", triageSummary),
			fmt.Sprintf("Prompt triage search terms: %s", triageSearchTerms),
			fmt.Sprintf("Prompt triage candidates: %s", triageCandidates),
			fmt.Sprintf("Prompt triage token estimates: raw=%d rewritten=%d search_space=%d selected_context=%d savings=%d",
				b.PromptTriage.RawPromptTokenEstimate,
				b.PromptTriage.RewrittenPromptTokenEstimate,
				b.PromptTriage.SearchSpaceTokenEstimate,
				b.PromptTriage.SelectedContextTokenEstimate,
				b.PromptTriage.ContextTokenSavingsEstimate,
			),
		)
	}
	if strings.TrimSpace(b.PromptIR.NormalizedTaskType) != "" || len(b.PromptIR.RankedTargets) > 0 || len(b.PromptIR.ValidatorPlan.Commands) > 0 {
		targets := promptIRTargetLabels(b.PromptIR.RankedTargets, 6)
		if len(targets) == 0 {
			targets = []string{"none"}
		}
		plan := b.PromptIR.OperationPlan
		if len(plan) == 0 {
			plan = []string{"none"}
		}
		validators := b.PromptIR.ValidatorPlan.Commands
		if len(validators) == 0 {
			validators = []string{"none"}
		}
		sections = append(sections,
			fmt.Sprintf("Prompt IR task type: %s", nonEmpty(strings.TrimSpace(b.PromptIR.NormalizedTaskType), "unknown")),
			fmt.Sprintf("Prompt IR objective: %s", nonEmpty(strings.TrimSpace(b.PromptIR.Objective), "none")),
			fmt.Sprintf("Prompt IR operation: %s", nonEmpty(strings.TrimSpace(b.PromptIR.Operation), "none")),
			fmt.Sprintf("Prompt IR repo index: id=%s summary=%s",
				nonEmpty(strings.TrimSpace(string(b.PromptIR.RepoIndexID)), "none"),
				nonEmpty(strings.TrimSpace(b.PromptIR.RepoIndexSummary), "none"),
			),
			fmt.Sprintf("Prompt IR targets: %s", strings.Join(targets, ", ")),
			fmt.Sprintf("Prompt IR plan: %s", strings.Join(plan, " | ")),
			fmt.Sprintf("Prompt IR validators: %s", strings.Join(validators, " | ")),
			fmt.Sprintf("Prompt IR confidence: level=%s value=%.2f reason=%s",
				nonEmpty(strings.TrimSpace(b.PromptIR.Confidence.Level), "unknown"),
				b.PromptIR.Confidence.Value,
				nonEmpty(strings.TrimSpace(b.PromptIR.Confidence.Reason), "none"),
			),
			fmt.Sprintf("Prompt IR serializer benchmark: default=%s natural=%d structured=%d structured_cheaper=%t",
				nonEmpty(strings.TrimSpace(string(b.PromptIR.DefaultSerializer)), "natural_language"),
				b.PromptIR.NaturalLanguageTokens,
				b.PromptIR.StructuredTokens,
				b.PromptIR.StructuredCheaper,
			),
		)
	}
	if len(req.ContextPack.IncludedSnippets) > 0 {
		sections = append(sections, "Context snippets:")
		for _, snippet := range req.ContextPack.IncludedSnippets {
			sections = append(sections,
				fmt.Sprintf("FILE %s:%d-%d", snippet.Path, snippet.StartLine, snippet.EndLine),
				snippet.Content,
			)
		}
	}
	return strings.Join(sections, "\n") + "\n"
}

func changedFiles(worktree string) ([]string, error) {
	if strings.TrimSpace(worktree) == "" {
		worktree = "."
	}
	cmd := exec.Command("git", "-C", worktree, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 3 {
			paths = append(paths, strings.TrimSpace(line[3:]))
		}
	}
	return paths, nil
}

func promptIRTargetLabels(targets []promptir.Target, limit int) []string {
	if limit <= 0 || limit > len(targets) {
		limit = len(targets)
	}
	out := make([]string, 0, limit)
	for _, target := range targets {
		label := strings.TrimSpace(target.Path)
		if strings.TrimSpace(target.Name) != "" {
			if label != "" {
				label = fmt.Sprintf("%s#%s", label, strings.TrimSpace(target.Name))
			} else {
				label = strings.TrimSpace(target.Name)
			}
		}
		if label == "" {
			continue
		}
		out = append(out, label)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func changedByRunHint(before, after []string, beforeErr, afterErr error) ([]string, string) {
	if beforeErr != nil || afterErr != nil {
		return []string{}, "unknown: pre/post git-status snapshot unavailable"
	}
	beforeSet := make(map[string]struct{}, len(before))
	for _, f := range before {
		if strings.TrimSpace(f) != "" {
			beforeSet[f] = struct{}{}
		}
	}
	out := make([]string, 0, len(after))
	for _, f := range after {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := beforeSet[f]; ok {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return out, "hint: no new dirty paths compared with pre-run dirty baseline"
	}
	return out, "hint: paths became newly dirty compared with pre-run dirty baseline"
}

func parseValidationSignals(stdout, stderr string) []string {
	joined := strings.ToLower(stdout + "\n" + stderr)
	signals := []string{}
	if strings.Contains(joined, "test") {
		signals = append(signals, "worker mentioned test activity")
	}
	if strings.Contains(joined, "passed") {
		signals = append(signals, "worker reported pass signal")
	}
	if strings.Contains(joined, "failed") {
		signals = append(signals, "worker reported fail signal")
	}
	return signals
}

func parseStructuredSummary(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TUKU_SUMMARY_JSON:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "TUKU_SUMMARY_JSON:"))
		}
	}
	return ""
}

func summarize(stdout, stderr string, exitCode int) string {
	if structured := parseStructuredSummary(stdout); structured != "" {
		return structured
	}
	for _, source := range []string{stdout, stderr} {
		lines := strings.Split(strings.TrimSpace(source), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line != "" {
				return fmt.Sprintf("exit=%d summary=%s", exitCode, line)
			}
		}
	}
	return fmt.Sprintf("exit=%d no output summary", exitCode)
}

func commonWorkerRunID(runID common.RunID) common.WorkerRunID {
	sum := sha256.Sum256([]byte(string(runID)))
	return common.WorkerRunID("wrk_" + hex.EncodeToString(sum[:8]))
}
