package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/runtime/process"
)

type Launcher struct {
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

func NewLauncher() *Launcher {
	cfg := Config{}
	cfg.Binary = strings.TrimSpace(os.Getenv("TUKU_CLAUDE_BIN"))
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	if envArgs := strings.TrimSpace(os.Getenv("TUKU_CLAUDE_ARGS")); envArgs != "" {
		cfg.Args = strings.Fields(envArgs)
	}
	cfg.Timeout = 90 * time.Second
	if sec := strings.TrimSpace(os.Getenv("TUKU_CLAUDE_TIMEOUT_SEC")); sec != "" {
		if n, err := strconv.Atoi(sec); err == nil && n > 0 {
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}
	cfg.Runner = process.NewLocalRunner()
	return NewLauncherWithConfig(cfg)
}

func NewLauncherWithConfig(cfg Config) *Launcher {
	if strings.TrimSpace(cfg.Binary) == "" {
		cfg.Binary = "claude"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.Runner == nil {
		cfg.Runner = process.NewLocalRunner()
	}
	return &Launcher{
		binary:  cfg.Binary,
		args:    append([]string{}, cfg.Args...),
		timeout: cfg.Timeout,
		runner:  cfg.Runner,
	}
}

func (l *Launcher) Name() adapter_contract.WorkerKind {
	return adapter_contract.WorkerClaude
}

func (l *Launcher) LaunchHandoff(ctx context.Context, req adapter_contract.HandoffLaunchRequest) (adapter_contract.HandoffLaunchResult, error) {
	startedAt := time.Now().UTC()
	res := adapter_contract.HandoffLaunchResult{
		LaunchID:     fmt.Sprintf("hlc_%d", startedAt.UnixNano()),
		TargetWorker: req.TargetWorker,
		StartedAt:    startedAt,
		Command:      l.binary,
		Args:         append([]string{}, l.args...),
		ExitCode:     -1,
	}
	if req.TargetWorker != adapter_contract.WorkerClaude {
		res.EndedAt = time.Now().UTC()
		res.ErrorMessage = fmt.Sprintf("unsupported launch target worker: %s", req.TargetWorker)
		return res, fmt.Errorf(res.ErrorMessage)
	}
	if _, err := exec.LookPath(l.binary); err != nil {
		res.EndedAt = time.Now().UTC()
		res.ErrorMessage = fmt.Sprintf("claude executable not found: %v", err)
		return res, fmt.Errorf(res.ErrorMessage)
	}

	prompt := buildLaunchPrompt(req)
	runCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	proc, runErr := l.runner.Run(runCtx, process.Spec{
		Command:    l.binary,
		Args:       l.args,
		WorkingDir: req.Payload.RepoAnchor.WorktreePath,
		Stdin:      prompt,
	})

	res.EndedAt = time.Now().UTC()
	res.ExitCode = proc.ExitCode
	res.Stdout = proc.Stdout
	res.Stderr = proc.Stderr
	res.Summary = summarizeLaunch(proc.Stdout, proc.Stderr, proc.ExitCode)

	if runErr != nil {
		res.ErrorMessage = runErr.Error()
		return res, runErr
	}
	if proc.ExitCode != 0 {
		res.ErrorMessage = fmt.Sprintf("claude exited with code %d", proc.ExitCode)
		return res, fmt.Errorf(res.ErrorMessage)
	}
	return res, nil
}

func buildLaunchPrompt(req adapter_contract.HandoffLaunchRequest) string {
	payloadJSON, _ := json.MarshalIndent(req.Payload, "", "  ")
	parts := []string{
		"You are Claude receiving a bounded Tuku handoff payload.",
		"Use the payload as canonical continuity state.",
		"Do not infer missing context beyond this packet.",
		fmt.Sprintf("Task ID: %s", req.TaskID),
		fmt.Sprintf("Handoff ID: %s", req.HandoffID),
		fmt.Sprintf("Source worker: %s", req.SourceWorker),
		fmt.Sprintf("Target worker: %s", req.TargetWorker),
		"Payload JSON:",
		string(payloadJSON),
		"Return a concise acknowledgement, a continuation plan, and explicit unknowns.",
	}
	return strings.Join(parts, "\n") + "\n"
}

func summarizeLaunch(stdout, stderr string, exitCode int) string {
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

