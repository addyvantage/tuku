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
	result := adapter_contract.ExecutionResult{
		WorkerRunID: commonWorkerRunID(req.RunID),
		StartedAt:   startedAt,
		Command:     a.binary,
		Args:        append([]string{}, a.args...),
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
		Args:       a.args,
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

func buildPrompt(req adapter_contract.ExecutionRequest) string {
	b := req.Brief
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
		"Return concise implementation summary, tests run, and unknowns.",
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
