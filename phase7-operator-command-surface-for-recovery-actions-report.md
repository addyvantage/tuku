1. Concise diagnosis of what was missing before this phase
- Tuku had durable recovery-action logic in the control plane, but operators could not drive it through the real CLI.
- The recovery action path existed below the shell line, which meant the main operator loop was still incomplete.
- There was no typed command surface for explicit recovery progression, so review/decision/repair posture could not be intentionally advanced through the normal Tuku entrypoint.

2. Exact implementation plan executed
- Added a single coherent CLI namespace: `tuku recovery record`.
- Added typed CLI parsing for existing recovery action kinds, with optional `--summary` and `--note` support.
- Wired the CLI command into the existing `task.recovery.record` IPC method and existing daemon/orchestrator backend path instead of duplicating business logic.
- Added focused app tests for successful request routing, unsupported action rejection, and daemon-side posture rejection surfacing.
- Added an orchestrator regression test for invalid posture rejection so the command surface is backed by explicit backend guard coverage.

3. Files changed
- `/Users/kagaya/Desktop/Tuku/internal/app/bootstrap.go`
- `/Users/kagaya/Desktop/Tuku/internal/app/bootstrap_test.go`
- `/Users/kagaya/Desktop/Tuku/internal/orchestrator/service_test.go`

4. Before vs after behavior summary
- Before: recovery actions were durable internally but not executable from the main operator CLI.
- After: operators can record typed recovery actions through `tuku recovery record`.
- Before: there was no first-class CLI path for review acknowledgment, continue decision, regenerate-brief decision, repair intent, or pending-launch review.
- After: those actions are reachable through one narrow typed command surface.
- Before: invalid recovery actions were only a backend concept.
- After: the CLI surfaces backend rejection cleanly and preserves control-plane truth.

5. New CLI / IPC / daemon / orchestrator semantics introduced
- New CLI command surface:
  - `tuku recovery record --task <id> --action <kind> [--summary <text>] [--note <text>]`
- Supported CLI action values:
  - `failed-run-reviewed`
  - `validation-reviewed`
  - `decision-continue`
  - `decision-regenerate-brief`
  - `repair-intent-recorded`
  - `pending-launch-reviewed`
- The CLI remains thin:
  - typed parsing only
  - request packaging only
  - no business-logic duplication
- The command reuses the existing backend path:
  - CLI -> IPC `task.recovery.record` -> daemon -> orchestrator `RecordRecoveryAction`
- Success output stays canonical and machine-stable by returning the persisted response payload.
- Invalid posture remains backend-governed and is returned cleanly to the CLI.

6. Tests added or updated
- App tests:
  - CLI usage mentions `recovery`
  - recovery action parsing
  - successful CLI recovery-record routing
  - unsupported action rejected before IPC
  - daemon/orchestrator rejection surfaced back through CLI
- Orchestrator test:
  - invalid recovery action for current posture is rejected cleanly

7. Commands run
```bash
gofmt -w internal/app/bootstrap.go internal/app/bootstrap_test.go internal/orchestrator/service_test.go

go test ./internal/orchestrator ./internal/runtime/daemon ./internal/app -count=1
```

8. Remaining limitations / next risks
- The CLI surface is intentionally narrow; there is still no higher-level interactive recovery workflow assistant.
- Output is JSON and canonical, but not yet optimized into a more operator-guided textual summary.
- Recovery actions are now executable from CLI, but shell-native shortcuts for invoking them still do not exist.
- `DECISION_REGENERATE_BRIEF` still records posture only; it does not regenerate a brief automatically.

9. Full code for every changed file

**internal/app/bootstrap.go**

```go
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"tuku/internal/adapters/claude"
	"tuku/internal/adapters/codex"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	daemonruntime "tuku/internal/runtime/daemon"
	"tuku/internal/storage/sqlite"
	tukushell "tuku/internal/tui/shell"
)

// CLIApplication is the top-level command host for the user-facing Tuku CLI.
type CLIApplication struct {
	openShellFn         func(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error
	openFallbackShellFn func(ctx context.Context, cwd string, preference tukushell.WorkerPreference) error
}

type repoShellTaskResolution struct {
	TaskID   common.TaskID
	RepoRoot string
	Created  bool
}

// DaemonApplication is the top-level process host for the local Tuku daemon.
type DaemonApplication struct{}

func NewCLIApplication() *CLIApplication {
	return &CLIApplication{}
}

func NewDaemonApplication() *DaemonApplication {
	return &DaemonApplication{}
}

var (
	getWorkingDir          = os.Getwd
	resolveRepoRootFromDir = anchorgit.ResolveRepoRoot
	ipcCall                = ipc.CallUnix
	startLocalDaemon       = launchLocalDaemonProcess
	resolveScratchPath     = defaultScratchSessionPath
	daemonReadyTimeout     = 5 * time.Second
	daemonRetryInterval    = 150 * time.Millisecond
)

func (a *CLIApplication) Run(ctx context.Context, args []string) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		_, _ = fmt.Fprintln(os.Stdout, cliUsage())
		return nil
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return a.runPrimaryEntry(ctx, socketPath, nil)
	}

	switch args[0] {
	case "chat":
		return a.runPrimaryEntry(ctx, socketPath, args[1:])
	case "start":
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		goal := fs.String("goal", "", "task goal")
		repo := fs.String("repo", ".", "repo root")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		payload, _ := json.Marshal(ipc.StartTaskRequest{Goal: *goal, RepoRoot: *repo})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodStartTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.StartTaskResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "message":
		fs := flag.NewFlagSet("message", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		message := fs.String("text", "", "user message")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *message == "" {
			return errors.New("--text is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task, "message": *message})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodSendMessage, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskMessageResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "status":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskStatus, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskStatusResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "shell":
		fs := flag.NewFlagSet("shell", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		preference, err := parseShellWorkerPreference(*worker)
		if err != nil {
			return err
		}
		return a.openShell(ctx, socketPath, *task, preference)

	case "shell-sessions":
		fs := flag.NewFlagSet("shell-sessions", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskShellSessions, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskShellSessionsResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		action := fs.String("action", "start", "run action: start|complete|interrupt")
		mode := fs.String("mode", "real", "run mode: real|noop")
		runID := fs.String("run-id", "", "run id for complete/interrupt actions")
		simInterrupt := fs.Bool("simulate-interrupt", false, "start then immediately interrupt")
		reason := fs.String("reason", "", "interruption reason")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{
			"task_id":             *task,
			"action":              *action,
			"mode":                *mode,
			"run_id":              *runID,
			"simulate_interrupt":  *simInterrupt,
			"interruption_reason": *reason,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskRun, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskRunResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "continue":
		fs := flag.NewFlagSet("continue", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskContinueRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodContinueTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskContinueResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "checkpoint":
		fs := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskCheckpointRequest{TaskID: common.TaskID(*task)})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateCheckpoint, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskCheckpointResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "recovery":
		if len(args) < 2 {
			return errors.New("usage: tuku recovery record --task <id> --action <kind> [--summary <text>] [--note <text>]")
		}
		switch args[1] {
		case "record":
			fs := flag.NewFlagSet("recovery record", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			action := fs.String("action", "", "recovery action kind")
			summary := fs.String("summary", "", "optional recovery action summary")
			note := fs.String("note", "", "optional recovery action note")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			if *action == "" {
				return errors.New("--action is required")
			}
			kind, err := parseRecoveryActionKind(*action)
			if err != nil {
				return err
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionRequest{
				TaskID:  common.TaskID(*task),
				Kind:    string(kind),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordRecoveryAction, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRecordRecoveryActionResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeJSON(os.Stdout, out)
		default:
			return fmt.Errorf("unknown recovery command: %s", args[1])
		}

	case "handoff-create":
		fs := flag.NewFlagSet("handoff-create", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		target := fs.String("target", string(rundomain.WorkerKindClaude), "target worker (claude)")
		mode := fs.String("mode", string(handoff.ModeResume), "handoff mode: resume|review|takeover")
		reason := fs.String("reason", "", "handoff reason")
		note := fs.String("note", "", "optional handoff note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffCreateRequest{
			TaskID:       common.TaskID(*task),
			TargetWorker: rundomain.WorkerKind(*target),
			Reason:       *reason,
			Mode:         handoff.Mode(*mode),
			Notes:        notes,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffCreateResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-accept":
		fs := flag.NewFlagSet("handoff-accept", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id")
		acceptedBy := fs.String("by", string(rundomain.WorkerKindClaude), "accepted-by worker")
		note := fs.String("note", "", "optional acceptance note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *handoffID == "" {
			return errors.New("--handoff is required")
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffAcceptRequest{
			TaskID:     common.TaskID(*task),
			HandoffID:  *handoffID,
			AcceptedBy: rundomain.WorkerKind(*acceptedBy),
			Notes:      notes,
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodAcceptHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffAcceptResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "handoff-launch":
		fs := flag.NewFlagSet("handoff-launch", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id (optional; defaults to latest for task)")
		target := fs.String("target", "", "target worker override (optional; must match packet target if set)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskHandoffLaunchRequest{
			TaskID:       common.TaskID(*task),
			HandoffID:    *handoffID,
			TargetWorker: rundomain.WorkerKind(*target),
		})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodLaunchHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffLaunchResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	case "inspect":
		fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipc.CallUnix(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskInspect, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskInspectResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeJSON(os.Stdout, out)

	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cliUsage() string {
	return "usage: tuku [chat] | tuku <start|message|shell|shell-sessions|run|continue|checkpoint|recovery|handoff-create|handoff-accept|handoff-launch|status|inspect|help> [flags]"
}

func parseRecoveryActionKind(value string) (recoveryaction.Kind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "failed-run-reviewed":
		return recoveryaction.KindFailedRunReviewed, nil
	case "validation-reviewed":
		return recoveryaction.KindValidationReviewed, nil
	case "decision-continue":
		return recoveryaction.KindDecisionContinue, nil
	case "decision-regenerate-brief":
		return recoveryaction.KindDecisionRegenerateBrief, nil
	case "repair-intent-recorded":
		return recoveryaction.KindRepairIntentRecorded, nil
	case "pending-launch-reviewed":
		return recoveryaction.KindPendingLaunchReviewed, nil
	default:
		return "", fmt.Errorf("unsupported recovery action %q", value)
	}
}

func (a *DaemonApplication) Run(ctx context.Context) error {
	dbPath, err := defaultDBPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}

	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	coord, err := orchestrator.NewCoordinator(orchestrator.Dependencies{
		Store:           store,
		IntentCompiler:  orchestrator.NewIntentStubCompiler(),
		BriefBuilder:    orchestrator.NewBriefBuilderV1(nil, nil),
		WorkerAdapter:   codex.NewAdapter(),
		HandoffLauncher: claude.NewLauncher(),
		Synthesizer:     canonical.NewSimpleSynthesizer(),
		AnchorProvider:  anchorgit.NewGitProvider(),
		ShellSessions:   store.ShellSessions(),
	})
	if err != nil {
		return err
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		return err
	}
	service := daemonruntime.NewService(socketPath, coord)
	return service.Run(ctx)
}

func defaultDataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Tuku"), nil
}

func defaultDBPath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tuku.db"), nil
}

func defaultSocketPath() (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "run", "tukud.sock"), nil
}

func requestID() string {
	return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
}

func (a *CLIApplication) runPrimaryEntry(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
	if err := fs.Parse(args); err != nil {
		return err
	}
	preference, err := parseShellWorkerPreference(*worker)
	if err != nil {
		return err
	}
	cwd, repoRoot, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return err
	}
	if !repoDetected {
		openFallback := a.openPrimaryFallbackShell
		if a.openFallbackShellFn != nil {
			openFallback = a.openFallbackShellFn
		}
		return openFallback(ctx, cwd, preference)
	}
	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(ctx, socketPath, repoRoot, orchestrator.DefaultRepoContinueGoal)
	if err != nil {
		return err
	}
	if a.openShellFn != nil {
		return a.openShellFn(ctx, socketPath, string(resolution.TaskID), preference)
	}
	source, err := newPrimaryRepoSnapshotSource(socketPath, repoRoot, resolution.Created)
	if err != nil {
		return err
	}
	return a.openShellWithSource(ctx, string(resolution.TaskID), preference, source)
}

func (a *CLIApplication) openPrimaryFallbackShell(ctx context.Context, cwd string, _ tukushell.WorkerPreference) error {
	scratchPath, err := resolveScratchPath(cwd)
	if err != nil {
		return err
	}
	return newPrimaryScratchIntake(cwd, scratchPath).Run(ctx)
}

func (a *CLIApplication) openShell(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error {
	return a.openShellWithSource(ctx, taskID, preference, tukushell.NewIPCSnapshotSource(socketPath))
}

func (a *CLIApplication) openShellWithSource(ctx context.Context, taskID string, preference tukushell.WorkerPreference, source tukushell.SnapshotSource) error {
	shellApp := tukushell.NewApp(taskID, source)
	shellApp.WorkerPreference = preference
	if socketPath := snapshotSourceSocketPath(source); socketPath != "" {
		shellApp.MessageSender = tukushell.NewIPCTaskMessageSender(socketPath)
		shellApp.LifecycleSink = tukushell.NewIPCLifecycleSink(socketPath)
		shellApp.RegistrySink = tukushell.NewIPCSessionRegistryClient(socketPath)
		shellApp.RegistrySource = tukushell.NewIPCSessionRegistryClient(socketPath)
	}
	return shellApp.Run(ctx)
}

func resolvePrimaryEntryContext(ctx context.Context) (string, string, bool, error) {
	cwd, err := getWorkingDir()
	if err != nil {
		return "", "", false, err
	}
	root, err := resolveRepoRootFromDir(ctx, cwd)
	if err != nil {
		return cwd, "", false, nil
	}
	return cwd, root, true, nil
}

func resolveCurrentRepoRoot(ctx context.Context) (string, error) {
	_, root, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return "", err
	}
	if !repoDetected {
		return "", fmt.Errorf("tuku needs a git repository for the primary entry path; current directory is not inside one")
	}
	return root, nil
}

func resolveShellTaskForRepo(ctx context.Context, socketPath string, repoRoot string, defaultGoal string) (repoShellTaskResolution, error) {
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoRequest{
		RepoRoot:    repoRoot,
		DefaultGoal: defaultGoal,
	})
	if err != nil {
		return repoShellTaskResolution{}, err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodResolveShellTaskForRepo,
		Payload:   payload,
	})
	if err != nil {
		return repoShellTaskResolution{}, err
	}
	var out ipc.ResolveShellTaskForRepoResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return repoShellTaskResolution{}, err
	}
	return repoShellTaskResolution{
		TaskID:   out.TaskID,
		RepoRoot: out.RepoRoot,
		Created:  out.Created,
	}, nil
}

func resolveShellTaskForRepoWithDaemonBootstrap(ctx context.Context, socketPath string, repoRoot string, defaultGoal string) (repoShellTaskResolution, error) {
	resolution, err := resolveShellTaskForRepo(ctx, socketPath, repoRoot, defaultGoal)
	if err == nil {
		return resolution, nil
	}
	if !isDaemonUnavailableError(err) {
		return repoShellTaskResolution{}, err
	}

	waitCh, err := startLocalDaemon()
	if err != nil {
		return repoShellTaskResolution{}, fmt.Errorf("could not start the local Tuku daemon automatically: %w", err)
	}

	deadline := time.Now().Add(daemonReadyTimeout)
	for {
		resolution, err = resolveShellTaskForRepo(ctx, socketPath, repoRoot, defaultGoal)
		if err == nil {
			return resolution, nil
		}
		if !isDaemonUnavailableError(err) {
			return repoShellTaskResolution{}, err
		}
		select {
		case waitErr, ok := <-waitCh:
			if ok && waitErr != nil {
				return repoShellTaskResolution{}, fmt.Errorf("local Tuku daemon failed to start: %w", waitErr)
			}
			return repoShellTaskResolution{}, errors.New("local Tuku daemon exited before becoming ready")
		default:
		}
		if time.Now().After(deadline) {
			return repoShellTaskResolution{}, fmt.Errorf("local Tuku daemon did not become ready within %s", daemonReadyTimeout)
		}
		if err := sleepWithContext(ctx, daemonRetryInterval); err != nil {
			return repoShellTaskResolution{}, err
		}
	}
}

func isDaemonUnavailableError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENOTCONN)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func launchLocalDaemonProcess() (<-chan error, error) {
	spec, err := resolveDaemonLaunchSpec()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(spec.Command, spec.Args...)
	if spec.WorkingDir != "" {
		cmd.Dir = spec.WorkingDir
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch %s: %w", spec.Label, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				err = fmt.Errorf("%w: %s", err, msg)
			}
		}
		waitCh <- err
		close(waitCh)
	}()
	return waitCh, nil
}

type daemonLaunchSpec struct {
	Command    string
	Args       []string
	WorkingDir string
	Label      string
}

func resolveDaemonLaunchSpec() (daemonLaunchSpec, error) {
	if path, err := exec.LookPath("tukud"); err == nil {
		return daemonLaunchSpec{
			Command: path,
			Label:   path,
		}, nil
	}
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "tukud")
		if fileExists(sibling) {
			return daemonLaunchSpec{
				Command: sibling,
				Label:   sibling,
			}, nil
		}
	}
	if root, ok := sourceTreeRoot(); ok {
		goBin, err := exec.LookPath("go")
		if err == nil {
			return daemonLaunchSpec{
				Command:    goBin,
				Args:       []string{"run", "./cmd/tukud"},
				WorkingDir: root,
				Label:      "go run ./cmd/tukud",
			}, nil
		}
	}
	return daemonLaunchSpec{}, errors.New("could not locate `tukud`; build or install it, or continue starting `tukud` manually")
}

func sourceTreeRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	if !fileExists(filepath.Join(root, "go.mod")) {
		return "", false
	}
	if !fileExists(filepath.Join(root, "cmd", "tukud", "main.go")) {
		return "", false
	}
	return root, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func defaultScratchSessionPath(cwd string) (string, error) {
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	normalized := filepath.Clean(strings.TrimSpace(cwd))
	sum := sha256.Sum256([]byte(normalized))
	return filepath.Join(root, "scratch", fmt.Sprintf("%x.json", sum[:])), nil
}

type primaryRepoScratchBridge struct {
	RepoRoot string
	Notes    []tukushell.ConversationItem
}

type primaryRepoScratchBridgeSource struct {
	base   tukushell.SnapshotSource
	bridge *primaryRepoScratchBridge
}

func snapshotSourceSocketPath(source tukushell.SnapshotSource) string {
	switch src := source.(type) {
	case *tukushell.IPCSnapshotSource:
		return src.SocketPath
	case *primaryRepoScratchBridgeSource:
		return snapshotSourceSocketPath(src.base)
	default:
		return ""
	}
}

func newPrimaryRepoSnapshotSource(socketPath string, repoRoot string, created bool) (tukushell.SnapshotSource, error) {
	base := tukushell.NewIPCSnapshotSource(socketPath)
	if !created {
		return base, nil
	}
	bridge, err := loadPrimaryRepoScratchBridge(repoRoot)
	if err != nil {
		return nil, err
	}
	if bridge == nil {
		return base, nil
	}
	return &primaryRepoScratchBridgeSource{
		base:   base,
		bridge: bridge,
	}, nil
}

func loadPrimaryRepoScratchBridge(repoRoot string) (*primaryRepoScratchBridge, error) {
	scratchPath, err := resolveScratchPath(repoRoot)
	if err != nil {
		return nil, err
	}
	notes, err := tukushell.LoadLocalScratchNotes(scratchPath)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, nil
	}
	return &primaryRepoScratchBridge{
		RepoRoot: filepath.Clean(strings.TrimSpace(repoRoot)),
		Notes:    notes,
	}, nil
}

func (s *primaryRepoScratchBridgeSource) Load(taskID string) (tukushell.Snapshot, error) {
	snapshot, err := s.base.Load(taskID)
	if err != nil {
		return tukushell.Snapshot{}, err
	}
	return applyPrimaryRepoScratchBridge(snapshot, s.bridge), nil
}

func applyPrimaryRepoScratchBridge(snapshot tukushell.Snapshot, bridge *primaryRepoScratchBridge) tukushell.Snapshot {
	if bridge == nil || len(bridge.Notes) == 0 {
		return snapshot
	}
	surfacedNotes := surfacedScratchBridgeNotes(bridge.Notes, 3)
	out := snapshot
	out.LocalScratch = &tukushell.LocalScratchContext{
		RepoRoot: bridge.RepoRoot,
		Notes:    surfacedNotes,
	}
	out.RecentConversation = append([]tukushell.ConversationItem{}, snapshot.RecentConversation...)
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Local scratch notes were found for this repo root when this task was first created. They have not been imported into canonical task state.",
	})
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Use the shell adopt command to stage them into a pending task message. Sending that pending message is the explicit adoption step into real Tuku continuity.",
	})
	out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
		Role: "system",
		Body: "Shell commands: stage local scratch with `a`, send the pending task message with `m`, clear it with `x`. When worker input is live, press Ctrl-G before the command key.",
	})
	for _, note := range surfacedNotes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		out.RecentConversation = append(out.RecentConversation, tukushell.ConversationItem{
			Role:      "system",
			Body:      "local scratch note: " + body,
			CreatedAt: note.CreatedAt,
		})
	}
	return out
}

func surfacedScratchBridgeNotes(notes []tukushell.ConversationItem, limit int) []tukushell.ConversationItem {
	if limit <= 0 || len(notes) <= limit {
		return append([]tukushell.ConversationItem{}, notes...)
	}
	start := len(notes) - limit
	return append([]tukushell.ConversationItem{}, notes[start:]...)
}

func parseShellWorkerPreference(raw string) (tukushell.WorkerPreference, error) {
	preference, err := tukushell.ParseWorkerPreference(raw)
	if err != nil {
		return "", fmt.Errorf("invalid --worker: %w", err)
	}
	return preference, nil
}

func writeJSON(out *os.File, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func primaryEntryScratchSnapshot(cwd string) tukushell.Snapshot {
	message := "No git repository was detected. Tuku opened a local scratch and intake session instead of repo-backed continuity."
	return tukushell.Snapshot{
		Goal:                    "Local scratch and intake session",
		Phase:                   "SCRATCH_INTAKE",
		Status:                  "LOCAL_ONLY",
		IntentClass:             "scratch",
		IntentSummary:           fmt.Sprintf("Use this local scratch session to plan work, sketch a new project, or prepare to clone or initialize a repository. Current directory: %s", cwd),
		LatestCanonicalResponse: message,
		RecentConversation: []tukushell.ConversationItem{
			{
				Role: "system",
				Body: message,
			},
			{
				Role: "system",
				Body: "This session is local-only. Tuku is not starting the daemon, not creating a task, and not claiming repo-backed continuity here.",
			},
			{
				Role: "system",
				Body: "Good uses for this mode: outline a new project, define milestones, list requirements, or prepare the next step before a repository exists.",
			},
			{
				Role: "system",
				Body: "Type one line and press Enter to save a local scratch note on this machine. Use /help, /list, or /quit as needed. This is scratch history only, not a Tuku task.",
			},
			{
				Role: "system",
				Body: fmt.Sprintf("Current directory: %s", cwd),
			},
		},
	}
}
```

**internal/app/bootstrap_test.go**

```go
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
	tukushell "tuku/internal/tui/shell"
)

func TestParseShellWorkerPreference(t *testing.T) {
	preference, err := parseShellWorkerPreference("claude")
	if err != nil {
		t.Fatalf("parse claude worker preference: %v", err)
	}
	if preference != "claude" {
		t.Fatalf("expected claude preference, got %q", preference)
	}
}

func TestParseShellWorkerPreferenceRejectsInvalidWorker(t *testing.T) {
	if _, err := parseShellWorkerPreference("invalid-worker"); err == nil {
		t.Fatal("expected invalid worker error")
	}
}

func TestCLIUsageMentionsChat(t *testing.T) {
	if !strings.Contains(cliUsage(), "chat") {
		t.Fatalf("expected cli usage to mention chat, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsRecovery(t *testing.T) {
	if !strings.Contains(cliUsage(), "recovery") {
		t.Fatalf("expected cli usage to mention recovery, got %q", cliUsage())
	}
}

func TestParseRecoveryActionKind(t *testing.T) {
	kind, err := parseRecoveryActionKind("decision-regenerate-brief")
	if err != nil {
		t.Fatalf("parse recovery action kind: %v", err)
	}
	if kind != "DECISION_REGENERATE_BRIEF" {
		t.Fatalf("expected DECISION_REGENERATE_BRIEF, got %s", kind)
	}
}

func TestCLIRecoveryRecordCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_123"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_123", Kind: "FAILED_RUN_REVIEWED"},
			RecoveryClass:         "DECISION_REQUIRED",
			RecommendedAction:     "MAKE_RESUME_DECISION",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "failed run reviewed; choose next step",
			CanonicalResponse:     "recovery action recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "record",
		"--task", "tsk_123",
		"--action", "failed-run-reviewed",
		"--summary", "reviewed failed run",
		"--note", "operator reviewed logs",
	}); err != nil {
		t.Fatalf("run recovery command: %v", err)
	}
	if captured.Method != ipc.MethodRecordRecoveryAction {
		t.Fatalf("expected recovery record method, got %s", captured.Method)
	}
	var req ipc.TaskRecordRecoveryActionRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal recovery record request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.Kind != "FAILED_RUN_REVIEWED" {
		t.Fatalf("unexpected recovery record request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator reviewed logs" {
		t.Fatalf("unexpected recovery record notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryRecordCommandRejectsUnsupportedAction(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "not-a-real-action"})
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery action") {
		t.Fatalf("expected unsupported recovery action error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported recovery action")
	}
}

func TestCLIRecoveryRecordCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [RECOVERY_ACTION_FAILED]: continue decision can only be recorded while recovery class is DECISION_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "decision-continue"})
	if err == nil || !strings.Contains(err.Error(), "DECISION_REQUIRED") {
		t.Fatalf("expected daemon rejection to surface, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapStartsDaemonOnUnavailable(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	var calls int
	var launched int
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		calls++
		if calls == 1 || calls == 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		return mustResolveShellTaskResponse(t, "tsk_bootstrap"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		launched++
		ch := make(chan error)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err != nil {
		t.Fatalf("resolve shell task with bootstrap: %v", err)
	}
	if resolution.TaskID != common.TaskID("tsk_bootstrap") {
		t.Fatalf("expected task id tsk_bootstrap, got %s", resolution.TaskID)
	}
	if launched != 1 {
		t.Fatalf("expected daemon to be launched once, got %d", launched)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapDoesNotStartDaemonOnUnexpectedError(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [BAD_PAYLOAD]: broken request")
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be launched for unexpected IPC errors")
		return nil, nil
	}

	if _, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository"); err == nil {
		t.Fatal("expected unexpected IPC error to be returned")
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsStartupFailure(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		return nil, errors.New("launch failed")
	}

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "could not start the local Tuku daemon automatically") {
		t.Fatalf("expected daemon startup failure, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsProcessExit(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error, 1)
		ch <- errors.New("exit status 1")
		close(ch)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "local Tuku daemon failed to start") {
		t.Fatalf("expected daemon process exit failure, got %v", err)
	}
}

func TestRunPrimaryEntryStartsDaemonAndOpensShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		if req.Method != ipc.MethodResolveShellTaskForRepo {
			t.Fatalf("expected resolve shell task request, got %s", req.Method)
		}
		return mustResolveShellTaskResponse(t, "tsk_primary"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	var openedTaskID string
	app := &CLIApplication{
		openShellFn: func(_ context.Context, _ string, taskID string, _ tukushell.WorkerPreference) error {
			openedTaskID = taskID
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry: %v", err)
	}
	if openedTaskID != "tsk_primary" {
		t.Fatalf("expected shell to open task tsk_primary, got %q", openedTaskID)
	}
}

func TestRunPrimaryEntryOutsideRepoOpensFallbackShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/no-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		t.Fatal("daemon IPC should not be used outside repo fallback mode")
		return ipc.Response{}, nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be auto-started outside repo fallback mode")
		return nil, nil
	}

	var fallbackCWD string
	app := &CLIApplication{
		openFallbackShellFn: func(_ context.Context, cwd string, _ tukushell.WorkerPreference) error {
			fallbackCWD = cwd
			return nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, _ tukushell.WorkerPreference) error {
			t.Fatal("task-backed shell should not open outside repo fallback mode")
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry outside repo: %v", err)
	}
	if fallbackCWD != "/tmp/no-repo" {
		t.Fatalf("expected fallback cwd /tmp/no-repo, got %q", fallbackCWD)
	}
}

func TestResolveCurrentRepoRootReturnsPrimaryEntryMessage(t *testing.T) {
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/not-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}

	_, err := resolveCurrentRepoRoot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tuku needs a git repository for the primary entry path") {
		t.Fatalf("expected primary-entry repo error, got %v", err)
	}
}

func TestPrimaryEntryScratchSnapshotExplainsNoRepoMode(t *testing.T) {
	snapshot := primaryEntryScratchSnapshot("/tmp/no-repo")
	if snapshot.Status != "LOCAL_ONLY" || snapshot.Phase != "SCRATCH_INTAKE" {
		t.Fatalf("expected scratch intake snapshot, got %+v", snapshot)
	}
	if snapshot.Repo.RepoRoot != "" {
		t.Fatalf("expected no repo anchor in scratch mode, got %+v", snapshot.Repo)
	}
	if snapshot.IntentClass != "scratch" {
		t.Fatalf("expected scratch intent class, got %q", snapshot.IntentClass)
	}
	if !strings.Contains(snapshot.LatestCanonicalResponse, "local scratch and intake session") {
		t.Fatalf("expected scratch explanation, got %q", snapshot.LatestCanonicalResponse)
	}
	if !strings.Contains(snapshot.IntentSummary, "/tmp/no-repo") {
		t.Fatalf("expected cwd in scratch intent summary, got %q", snapshot.IntentSummary)
	}
	if len(snapshot.RecentConversation) < 3 {
		t.Fatal("expected scratch intake guidance conversation")
	}
}

func TestLoadPrimaryRepoScratchBridgeLoadsExactRepoScratchNotes(t *testing.T) {
	origResolveScratchPath := resolveScratchPath
	defer func() {
		resolveScratchPath = origResolveScratchPath
	}()

	path := filepath.Join(t.TempDir(), "scratch.json")
	resolveScratchPath = func(string) (string, error) {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "kind": "local_scratch_intake",
  "cwd": "/tmp/repo",
  "created_at": "2026-03-19T00:00:00Z",
  "updated_at": "2026-03-19T00:00:00Z",
  "notes": [
    {"role": "user", "body": "Draft the first milestone list", "created_at": "2026-03-19T00:00:00Z"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("write scratch file: %v", err)
	}

	bridge, err := loadPrimaryRepoScratchBridge("/tmp/repo")
	if err != nil {
		t.Fatalf("load primary repo scratch bridge: %v", err)
	}
	if bridge == nil || len(bridge.Notes) != 1 {
		t.Fatalf("expected one bridged scratch note, got %+v", bridge)
	}
	if bridge.Notes[0].Body != "Draft the first milestone list" {
		t.Fatalf("expected bridged note body, got %+v", bridge.Notes[0])
	}
}

func TestApplyPrimaryRepoScratchBridgeAppendsExplicitLocalOnlyMessages(t *testing.T) {
	snapshot := applyPrimaryRepoScratchBridge(tukushell.Snapshot{
		TaskID:                  "tsk_repo",
		Phase:                   "INTAKE",
		Status:                  "ACTIVE",
		LatestCanonicalResponse: "Canonical repo-backed response.",
		RecentConversation: []tukushell.ConversationItem{
			{Role: "system", Body: "Repo-backed task created."},
		},
	}, &primaryRepoScratchBridge{
		RepoRoot: "/tmp/repo",
		Notes: []tukushell.ConversationItem{
			{Role: "user", Body: "Plan project structure"},
			{Role: "user", Body: "List initial requirements"},
		},
	})

	if snapshot.LatestCanonicalResponse != "Canonical repo-backed response." {
		t.Fatalf("expected canonical response to remain unchanged, got %q", snapshot.LatestCanonicalResponse)
	}
	if snapshot.LocalScratch == nil || len(snapshot.LocalScratch.Notes) != 2 {
		t.Fatalf("expected surfaced local scratch context, got %+v", snapshot.LocalScratch)
	}
	all := make([]string, 0, len(snapshot.RecentConversation))
	for _, msg := range snapshot.RecentConversation {
		all = append(all, msg.Body)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "have not been imported into canonical task state") {
		t.Fatalf("expected explicit local-only boundary, got %q", joined)
	}
	if !strings.Contains(joined, "Sending that pending message is the explicit adoption step") {
		t.Fatalf("expected explicit adoption step, got %q", joined)
	}
	if !strings.Contains(joined, "Shell commands: stage local scratch with `a`") {
		t.Fatalf("expected shell-local adoption command copy, got %q", joined)
	}
	if !strings.Contains(joined, "local scratch note: Plan project structure") {
		t.Fatalf("expected bridged scratch note, got %q", joined)
	}
}

func mustResolveShellTaskResponse(t *testing.T, taskID common.TaskID) ipc.Response {
	t.Helper()
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoResponse{
		TaskID:   taskID,
		RepoRoot: "/tmp/repo",
		Created:  false,
	})
	if err != nil {
		t.Fatalf("marshal resolve shell task response: %v", err)
	}
	return ipc.Response{OK: true, Payload: payload}
}

func daemonUnavailableErr() error {
	return &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}
}

type capturedStdout struct {
	previous *os.File
	reader   *os.File
	writer   *os.File
	buffer   bytes.Buffer
}

func captureCLIStdout(t *testing.T) *capturedStdout {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	captured := &capturedStdout{
		previous: os.Stdout,
		reader:   reader,
		writer:   writer,
	}
	os.Stdout = writer
	return captured
}

func (c *capturedStdout) restore() {
	if c == nil {
		return
	}
	if c.previous != nil {
		os.Stdout = c.previous
	}
	if c.writer != nil {
		_ = c.writer.Close()
	}
	if c.reader != nil {
		_, _ = c.buffer.ReadFrom(c.reader)
		_ = c.reader.Close()
	}
}
```

**internal/orchestrator/service_test.go**

```go
package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/response/canonical"
	"tuku/internal/storage"
	"tuku/internal/storage/sqlite"
)

func TestStartTaskCreatesCapsuleWithAnchorAndProof(t *testing.T) {
	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "abc123", WorkingTreeDirty: true, CapturedAt: time.Unix(1700000000, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	res, err := coord.StartTask(context.Background(), "Build milestone four", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	caps, err := store.Capsules().Get(res.TaskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.BranchName != "main" || caps.HeadSHA != "abc123" || !caps.WorkingTreeDirty {
		t.Fatalf("expected anchor persisted in capsule: %+v", caps)
	}
}

func TestMessageCreatesIntentAndBriefAndProof(t *testing.T) {
	store := newTestStore(t)
	provider := &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "head-1", WorkingTreeDirty: false, CapturedAt: time.Unix(1700001000, 0).UTC()}}
	coord := newTestCoordinator(t, store, provider, newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "Implement parser", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	msgRes, err := coord.MessageTask(context.Background(), string(start.TaskID), "continue and prepare implementation")
	if err != nil {
		t.Fatalf("message task: %v", err)
	}
	if msgRes.BriefID == "" || msgRes.BriefHash == "" {
		t.Fatal("expected brief id and hash")
	}

	events, err := store.Proofs().ListByTask(start.TaskID, 30)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventBriefCreated) {
		t.Fatal("expected brief created event")
	}
}

func TestStartTaskRollsBackOnProofAppendFailure(t *testing.T) {
	base := newTestStore(t)
	injected := &faultInjectedStore{base: base, failProofAppend: true}
	coord, err := NewCoordinator(Dependencies{
		Store:          injected,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
		IDGenerator: func(prefix string) string {
			return prefix + "_fixed"
		},
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	if _, err := coord.StartTask(context.Background(), "tx rollback start", "/tmp/repo"); err == nil {
		t.Fatal("expected start task failure")
	}

	if _, err := base.Capsules().Get(common.TaskID("tsk_fixed")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected no persisted capsule after rollback, got err=%v", err)
	}
	events, err := base.Proofs().ListByTask(common.TaskID("tsk_fixed"), 20)
	if err != nil {
		t.Fatalf("list proofs after rollback: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no proof events for rolled-back start, got %d", len(events))
	}
}

func TestMessageTaskRollsBackOnSynthesisFailure(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	start := setupTaskWithBrief(t, coord)

	capsBefore, err := store.Capsules().Get(start)
	if err != nil {
		t.Fatalf("get capsule before: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversations before: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(start, 200)
	if err != nil {
		t.Fatalf("list proofs before: %v", err)
	}

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}

	if _, err := failCoord.MessageTask(context.Background(), string(start), "this write should rollback"); err == nil {
		t.Fatal("expected message task failure")
	}

	capsAfter, err := store.Capsules().Get(start)
	if err != nil {
		t.Fatalf("get capsule after: %v", err)
	}
	if capsAfter.CurrentIntentID != capsBefore.CurrentIntentID {
		t.Fatalf("capsule intent pointer changed despite rollback: before=%s after=%s", capsBefore.CurrentIntentID, capsAfter.CurrentIntentID)
	}
	if capsAfter.CurrentBriefID != capsBefore.CurrentBriefID {
		t.Fatalf("capsule brief pointer changed despite rollback: before=%s after=%s", capsBefore.CurrentBriefID, capsAfter.CurrentBriefID)
	}

	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 100)
	if err != nil {
		t.Fatalf("list conversations after: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("conversation count changed despite rollback: before=%d after=%d", len(convBefore), len(convAfter))
	}
	eventsAfter, err := store.Proofs().ListByTask(start, 200)
	if err != nil {
		t.Fatalf("list proofs after: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("proof event count changed despite rollback: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
}

func TestRunRealSuccessCompletesAndRecordsEvidence(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if res.RunID == "" {
		t.Fatal("expected run id")
	}
	if res.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed status, got %s", res.RunStatus)
	}
	if res.Phase != phase.PhaseValidating {
		t.Fatalf("expected %s phase, got %s", phase.PhaseValidating, res.Phase)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "completed") {
		t.Fatalf("expected canonical completion response, got %q", res.CanonicalResponse)
	}

	runRec, err := store.Runs().Get(res.RunID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if runRec.Status != rundomain.StatusCompleted {
		t.Fatalf("expected run status completed, got %s", runRec.Status)
	}

	events, err := store.Proofs().ListByTask(taskID, 80)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventWorkerRunStarted) {
		t.Fatal("expected worker run started")
	}
	if !hasEvent(events, proof.EventWorkerOutputCaptured) {
		t.Fatal("expected worker output captured")
	}
	if !hasEvent(events, proof.EventFileChangeDetected) {
		t.Fatal("expected file change detected event")
	}
	if !hasEvent(events, proof.EventWorkerRunCompleted) {
		t.Fatal("expected worker run completed")
	}
	for _, e := range events {
		switch e.Type {
		case proof.EventWorkerRunStarted, proof.EventWorkerOutputCaptured, proof.EventFileChangeDetected, proof.EventWorkerRunCompleted, proof.EventWorkerRunFailed, proof.EventRunInterrupted:
			if e.RunID == nil {
				t.Fatalf("expected run_id for run-related event %s", e.Type)
			}
		}
	}
}

func TestRunRealFailureMarksBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real failure path: %v", err)
	}
	if res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed status, got %s", res.RunStatus)
	}
	if res.Phase != phase.PhaseBlocked {
		t.Fatalf("expected %s phase, got %s", phase.PhaseBlocked, res.Phase)
	}

	events, err := store.Proofs().ListByTask(taskID, 80)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventWorkerRunFailed) {
		t.Fatal("expected worker run failed")
	}
}

func TestRunRealAdapterErrorMarksBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterError(errors.New("codex missing")))
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real adapter error should map to canonical failure, got: %v", err)
	}
	if res.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed status, got %s", res.RunStatus)
	}
}

func TestRunRealPassesBoundedExecutionEnvelopeToAdapter(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if !adapter.called {
		t.Fatal("expected adapter execute to be called")
	}
	if adapter.lastReq.TaskID != taskID {
		t.Fatalf("expected adapter task id %s, got %s", taskID, adapter.lastReq.TaskID)
	}
	if adapter.lastReq.RunID != res.RunID {
		t.Fatalf("expected adapter run id %s, got %s", res.RunID, adapter.lastReq.RunID)
	}
	if adapter.lastReq.Brief.BriefID == "" {
		t.Fatal("expected adapter brief id to be populated")
	}
	if adapter.lastReq.Brief.NormalizedAction == "" {
		t.Fatal("expected adapter normalized action to be populated")
	}
	if adapter.lastReq.RepoAnchor.RepoRoot == "" {
		t.Fatal("expected adapter repo root to be populated")
	}
	if adapter.lastReq.ContextSummary == "" {
		t.Fatal("expected adapter context summary to be populated")
	}
	if adapter.lastReq.PolicyProfileID == "" {
		t.Fatal("expected adapter policy profile to be populated")
	}
}

func TestRunDurablyRunningBeforeWorkerExecute(t *testing.T) {
	store := newTestStore(t)
	adapter := newFakeAdapterSuccess()
	var observedRunStatus rundomain.Status
	var observedCapsulePhase phase.Phase
	adapter.onExecute = func(req adapter_contract.ExecutionRequest) {
		runRec, err := store.Runs().Get(req.RunID)
		if err != nil {
			t.Fatalf("expected run to exist before execute: %v", err)
		}
		observedRunStatus = runRec.Status

		caps, err := store.Capsules().Get(req.TaskID)
		if err != nil {
			t.Fatalf("expected capsule to exist before execute: %v", err)
		}
		observedCapsulePhase = caps.CurrentPhase
	}
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}
	if observedRunStatus != rundomain.StatusRunning {
		t.Fatalf("expected RUNNING before execute, got %s", observedRunStatus)
	}
	if observedCapsulePhase != phase.PhaseExecuting {
		t.Fatalf("expected EXECUTING before execute, got %s", observedCapsulePhase)
	}
	if res.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed final status, got %s", res.RunStatus)
	}
}

func TestCanonicalResponseNotRawWorkerText(t *testing.T) {
	store := newTestStore(t)
	adapter := &fakeWorkerAdapter{kind: adapter_contract.WorkerCodex, result: adapter_contract.ExecutionResult{
		ExitCode:  0,
		Stdout:    "RAW_WORKER_OUTPUT_TOKEN_12345",
		Stderr:    "",
		Summary:   "completed summary",
		StartedAt: time.Now().UTC(),
		EndedAt:   time.Now().UTC(),
	}}
	coord := newTestCoordinator(t, store, defaultAnchor(), adapter)
	taskID := setupTaskWithBrief(t, coord)

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if res.CanonicalResponse == adapter.result.Stdout {
		t.Fatal("canonical response must not equal raw worker stdout")
	}
	if strings.Contains(res.CanonicalResponse, "RAW_WORKER_OUTPUT_TOKEN_12345") {
		t.Fatal("canonical response leaked raw worker token")
	}
}

func TestRunNoBriefBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())

	start, err := coord.StartTask(context.Background(), "No brief case", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	res, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(start.TaskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run should return canonical blocked response, got error: %v", err)
	}
	if res.RunID != "" {
		t.Fatalf("expected empty run id when blocked, got %s", res.RunID)
	}
	if !strings.Contains(strings.ToLower(res.CanonicalResponse), "cannot start") {
		t.Fatalf("unexpected canonical response: %s", res.CanonicalResponse)
	}
}

func TestRunNoopModeManualLifecycleStillWorks(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop start: %v", err)
	}
	if startRes.RunStatus != rundomain.StatusRunning {
		t.Fatalf("expected running noop run, got %s", startRes.RunStatus)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after noop start: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseExecuting {
		t.Fatalf("running invariant broken: expected phase %s, got %s", phase.PhaseExecuting, caps.CurrentPhase)
	}
	completeRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "complete", RunID: startRes.RunID})
	if err != nil {
		t.Fatalf("noop complete: %v", err)
	}
	if completeRes.RunStatus != rundomain.StatusCompleted {
		t.Fatalf("expected completed noop run, got %s", completeRes.RunStatus)
	}
}

func TestRunInterruptSetsPausedInvariant(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("noop start: %v", err)
	}
	interruptRes, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startRes.RunID,
		InterruptionReason: "test interruption",
	})
	if err != nil {
		t.Fatalf("interrupt run: %v", err)
	}
	if interruptRes.RunStatus != rundomain.StatusInterrupted {
		t.Fatalf("expected interrupted status, got %s", interruptRes.RunStatus)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after interrupt: %v", err)
	}
	if caps.CurrentPhase != phase.PhasePaused {
		t.Fatalf("interrupt invariant broken: expected phase %s, got %s", phase.PhasePaused, caps.CurrentPhase)
	}
}

func TestStatusAndInspectExposeLatestRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.LatestRunID != runRes.RunID {
		t.Fatalf("status missing latest run id: %+v", status)
	}

	ins, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if ins.Run == nil || ins.Run.RunID != runRes.RunID {
		t.Fatalf("inspect missing latest run: %+v", ins)
	}
}

func TestBriefBuilderDeterministicHash(t *testing.T) {
	builder := NewBriefBuilderV1(func(_ string) string { return "brf_fixed" }, func() time.Time {
		return time.Unix(1700003000, 0).UTC()
	})

	input := brief.BuildInput{
		TaskID:           "tsk_1",
		IntentID:         "int_1",
		CapsuleVersion:   2,
		Goal:             "Implement feature X",
		NormalizedAction: "continue from current state",
		Constraints:      []string{"do not execute workers"},
		ScopeHints:       []string{"internal/orchestrator"},
		ScopeOutHints:    []string{"web"},
		DoneCriteria:     []string{"brief is generated"},
		Verbosity:        brief.VerbosityStandard,
		PolicyProfileID:  "default-safe-v1",
	}

	b1, err := builder.Build(input)
	if err != nil {
		t.Fatalf("build brief 1: %v", err)
	}
	b2, err := builder.Build(input)
	if err != nil {
		t.Fatalf("build brief 2: %v", err)
	}
	if b1.BriefHash != b2.BriefHash {
		t.Fatalf("expected deterministic hash, got %s vs %s", b1.BriefHash, b2.BriefHash)
	}
}

func TestRunTaskKeepsDurableRunningStateWhenFinalizationFails(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	capsBefore, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule before: %v", err)
	}
	convBefore, err := store.Conversations().ListRecent(capsBefore.ConversationID, 200)
	if err != nil {
		t.Fatalf("list conversations before: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proofs before: %v", err)
	}

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}

	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected run task failure")
	}

	runRec, err := store.Runs().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("expected persisted running run after stage-1 commit, got err=%v", err)
	}
	if runRec.Status != rundomain.StatusRunning {
		t.Fatalf("expected run to remain RUNNING when finalization fails, got %s", runRec.Status)
	}

	capsAfter, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule after: %v", err)
	}
	if capsAfter.CurrentPhase != phase.PhaseExecuting {
		t.Fatalf("expected capsule to remain EXECUTING when finalization fails, got %s", capsAfter.CurrentPhase)
	}
	convAfter, err := store.Conversations().ListRecent(capsAfter.ConversationID, 200)
	if err != nil {
		t.Fatalf("list conversations after: %v", err)
	}
	if len(convAfter) != len(convBefore) {
		t.Fatalf("conversation count changed despite rollback: before=%d after=%d", len(convBefore), len(convAfter))
	}

	eventsAfter, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proofs after: %v", err)
	}
	if len(eventsAfter) != len(eventsBefore)+2 {
		t.Fatalf("expected only stage-1 run start events to persist: before=%d after=%d", len(eventsBefore), len(eventsAfter))
	}
	if !hasEvent(eventsAfter, proof.EventWorkerRunStarted) {
		t.Fatal("expected durable worker run started event from stage-1 commit")
	}
	if hasEvent(eventsAfter, proof.EventWorkerOutputCaptured) {
		t.Fatal("worker output captured should rollback when finalization transaction fails")
	}
	if hasEvent(eventsAfter, proof.EventWorkerRunCompleted) || hasEvent(eventsAfter, proof.EventWorkerRunFailed) {
		t.Fatal("terminal run events should not persist when finalization transaction fails")
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after failed finalization: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerBeforeExecution {
		t.Fatalf("expected before-execution checkpoint from prepare stage, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.RunID != runRec.RunID {
		t.Fatalf("expected checkpoint run id %s, got %s", runRec.RunID, latestCheckpoint.RunID)
	}
}

func TestRunRealSuccessCreatesAfterExecutionCheckpoint(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	runRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start real: %v", err)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerAfterExecution {
		t.Fatalf("expected after-execution checkpoint, got %s", latestCheckpoint.Trigger)
	}
	if latestCheckpoint.RunID != runRes.RunID {
		t.Fatalf("expected checkpoint run id %s, got %s", runRes.RunID, latestCheckpoint.RunID)
	}
	if !latestCheckpoint.IsResumable {
		t.Fatal("expected checkpoint to be resumable")
	}
}

func TestCreateCheckpointManual(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	if out.Trigger != checkpoint.TriggerManual {
		t.Fatalf("expected manual trigger, got %s", out.Trigger)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected checkpoint id")
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.CheckpointID != out.CheckpointID {
		t.Fatalf("expected latest checkpoint %s, got %s", out.CheckpointID, latestCheckpoint.CheckpointID)
	}
	if !hasEventMust(t, store, taskID, proof.EventCheckpointCreated) {
		t.Fatal("expected checkpoint created proof event")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status after checkpoint: %v", err)
	}
	if status.LatestCheckpointID != out.CheckpointID {
		t.Fatalf("status missing latest checkpoint id: expected %s got %s", out.CheckpointID, status.LatestCheckpointID)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect after checkpoint: %v", err)
	}
	if inspectOut.Checkpoint == nil || inspectOut.Checkpoint.CheckpointID != out.CheckpointID {
		t.Fatalf("inspect missing checkpoint: %+v", inspectOut.Checkpoint)
	}
}

func TestContinueReconcilesStaleRunningRun(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave stale running state")
	}
	beforeCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint before continue reconciliation: %v", err)
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeStaleReconciled {
		t.Fatalf("expected stale reconciliation outcome, got %s", out.Outcome)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected reconciliation checkpoint id")
	}

	runRec, err := store.Runs().Get(out.RunID)
	if err != nil {
		t.Fatalf("get reconciled run: %v", err)
	}
	if runRec.Status != rundomain.StatusInterrupted {
		t.Fatalf("expected run interrupted after reconciliation, got %s", runRec.Status)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhasePaused {
		t.Fatalf("expected paused phase after stale reconciliation, got %s", caps.CurrentPhase)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after reconciliation: %v", err)
	}
	if latestCheckpoint.CheckpointID == beforeCheckpoint.CheckpointID {
		t.Fatalf("expected new checkpoint for reconciliation, got same id %s", latestCheckpoint.CheckpointID)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerInterruption {
		t.Fatalf("expected interruption checkpoint after stale reconciliation, got %s", latestCheckpoint.Trigger)
	}
}

func TestContinueBlockedOnMajorDrift(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	if _, err := coord.CreateCheckpoint(context.Background(), string(taskID)); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	driftAnchor := &staticAnchorProvider{
		snapshot: anchorgit.Snapshot{
			RepoRoot:         "/tmp/repo",
			Branch:           "feature/drift",
			HeadSHA:          "head-x",
			WorkingTreeDirty: false,
			CapturedAt:       time.Unix(1700005000, 0).UTC(),
		},
	}
	driftCoord := newTestCoordinator(t, store, driftAnchor, newFakeAdapterSuccess())
	out, err := driftCoord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with drift: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedDrift {
		t.Fatalf("expected blocked drift outcome, got %s", out.Outcome)
	}
	if out.DriftClass != checkpoint.DriftMajor {
		t.Fatalf("expected major drift class, got %s", out.DriftClass)
	}
	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseAwaitingDecision {
		t.Fatalf("expected awaiting decision phase, got %s", caps.CurrentPhase)
	}
}

func TestContinueSafeFromCheckpoint(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	eventsBefore, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("events before safe continue: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue safe: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe outcome, got %s", out.Outcome)
	}
	if out.CheckpointID == "" {
		t.Fatal("expected continuation checkpoint")
	}
	if out.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected safe continue to reuse checkpoint %s, got %s", seed.CheckpointID, out.CheckpointID)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "safe resume") {
		t.Fatalf("expected canonical safe resume response, got %q", out.CanonicalResponse)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint after safe continue: %v", err)
	}
	if latestCheckpoint.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected no new checkpoint to be created on safe continue")
	}
	eventsAfter, err := store.Proofs().ListByTask(taskID, 500)
	if err != nil {
		t.Fatalf("events after safe continue: %v", err)
	}
	if len(eventsAfter) <= len(eventsBefore) {
		t.Fatalf("expected durable proof records for no-op safe continue")
	}
	if !hasEvent(eventsAfter, proof.EventContinueAssessed) {
		t.Fatalf("expected continue-assessed proof event for no-op safe continue")
	}
}

func TestContinueInterruptedRunReportsRecoveryReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	startRes, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "noop"})
	if err != nil {
		t.Fatalf("start noop run: %v", err)
	}
	if _, err := coord.RunTask(context.Background(), RunTaskRequest{
		TaskID:             string(taskID),
		Action:             "interrupt",
		RunID:              startRes.RunID,
		InterruptionReason: "phase 2 interrupted recovery test",
	}); err != nil {
		t.Fatalf("interrupt run: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.RecoveryClass != RecoveryClassInterruptedRunRecoverable {
		t.Fatalf("expected interrupted recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionResumeInterrupted {
		t.Fatalf("expected resume interrupted action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected interrupted recovery to be ready for next run")
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "interrupted") {
		t.Fatalf("expected interrupted recovery canonical response, got %q", out.CanonicalResponse)
	}
}

func TestFailedRunRecoveryRequiresReviewNotNextRunReady(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	runOut, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
	if err != nil {
		t.Fatalf("run start: %v", err)
	}
	if runOut.RunStatus != rundomain.StatusFailed {
		t.Fatalf("expected failed run status, got %s", runOut.RunStatus)
	}

	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.IsResumable {
		t.Fatal("failed run checkpoint must not claim resumable recovery")
	}

	continueOut, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if continueOut.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed-run review recovery class, got %s", continueOut.RecoveryClass)
	}
	if continueOut.RecommendedAction != RecoveryActionInspectFailedRun {
		t.Fatalf("expected inspect failed run action, got %s", continueOut.RecommendedAction)
	}
	if continueOut.ReadyForNextRun {
		t.Fatal("failed run recovery must not be ready for next run")
	}
	if !strings.Contains(strings.ToLower(continueOut.CanonicalResponse), "not ready") {
		t.Fatalf("expected failed recovery canonical response to avoid ready claim, got %q", continueOut.CanonicalResponse)
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.CheckpointResumable {
		t.Fatal("status should report failed checkpoint as non-resumable")
	}
	if status.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected failed recovery class in status, got %s", status.RecoveryClass)
	}
	if status.ReadyForNextRun {
		t.Fatal("status must not claim ready-for-next-run after failed execution")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassFailedRunReviewRequired {
		t.Fatalf("expected inspect failed recovery class, got %s", inspectOut.Recovery.RecoveryClass)
	}
	if inspectOut.Recovery.ReadyForNextRun {
		t.Fatal("inspect recovery must not claim ready-for-next-run after failed execution")
	}
}

func TestRecordRecoveryActionFailedRunReviewedPromotesDecisionRequired(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
		Notes:  []string{"reviewed failure evidence"},
	})
	if err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}
	if out.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected decision-required recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionMakeResumeDecision {
		t.Fatalf("expected make-resume-decision action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("failed-run review should not make the task ready yet")
	}

	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	if status.LatestRecoveryAction == nil || status.LatestRecoveryAction.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("expected latest recovery action in status, got %+v", status.LatestRecoveryAction)
	}
	if status.RecoveryClass != RecoveryClassDecisionRequired {
		t.Fatalf("expected status decision-required class, got %s", status.RecoveryClass)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindFailedRunReviewed {
		t.Fatalf("expected latest inspect recovery action, got %+v", inspectOut.LatestRecoveryAction)
	}
	if len(inspectOut.RecentRecoveryActions) != 1 {
		t.Fatalf("expected one persisted recovery action, got %d", len(inspectOut.RecentRecoveryActions))
	}
	events, err := store.Proofs().ListByTask(taskID, 100)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	if !hasEvent(events, proof.EventRecoveryActionRecorded) {
		t.Fatal("expected recovery-action-recorded proof event")
	}
}

func TestRecordRecoveryActionDecisionContinueMakesTaskReadyNextRun(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("record decision continue: %v", err)
	}
	if out.RecoveryClass != RecoveryClassReadyNextRun {
		t.Fatalf("expected ready-next-run recovery class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionStartNextRun {
		t.Fatalf("expected start-next-run action, got %s", out.RecommendedAction)
	}
	if !out.ReadyForNextRun {
		t.Fatal("expected ready-for-next-run after continue decision")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBriefReady {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBriefReady, caps.CurrentPhase)
	}
}

func TestRecordRecoveryActionDecisionRegenerateBriefRequiresRebrief(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionRegenerateBrief,
	})
	if err != nil {
		t.Fatalf("record regenerate-brief decision: %v", err)
	}
	if out.RecoveryClass != RecoveryClassRebriefRequired {
		t.Fatalf("expected rebrief-required class, got %s", out.RecoveryClass)
	}
	if out.RecommendedAction != RecoveryActionRegenerateBrief {
		t.Fatalf("expected regenerate-brief action, got %s", out.RecommendedAction)
	}
	if out.ReadyForNextRun {
		t.Fatal("regenerate-brief decision must not claim next-run readiness")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	if caps.CurrentPhase != phase.PhaseBlocked {
		t.Fatalf("expected capsule phase %s, got %s", phase.PhaseBlocked, caps.CurrentPhase)
	}
}

func TestRecordRecoveryActionIdempotentReplayReusesLatestAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindFailedRunReviewed,
		Summary: "reviewed failed run",
		Notes:   []string{"same-note"},
	})
	if err != nil {
		t.Fatalf("first record recovery action: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindFailedRunReviewed,
		Summary: "reviewed failed run",
		Notes:   []string{"same-note"},
	})
	if err != nil {
		t.Fatalf("second record recovery action: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected idempotent recovery action replay, got %s and %s", first.Action.ActionID, second.Action.ActionID)
	}
	actions, err := store.RecoveryActions().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list recovery actions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one persisted recovery action, got %d", len(actions))
	}
}

func TestRecordRecoveryActionDecisionContinueReplayReusesLatestAction(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterExitFailure())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err != nil {
		t.Fatalf("run start: %v", err)
	}
	if _, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindFailedRunReviewed,
	}); err != nil {
		t.Fatalf("record failed-run review: %v", err)
	}

	first, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("first decision continue: %v", err)
	}
	second, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err != nil {
		t.Fatalf("second decision continue: %v", err)
	}
	if first.Action.ActionID != second.Action.ActionID {
		t.Fatalf("expected decision-continue replay to reuse latest action, got %s and %s", first.Action.ActionID, second.Action.ActionID)
	}
	if second.RecoveryClass != RecoveryClassReadyNextRun || !second.ReadyForNextRun {
		t.Fatalf("expected ready-next-run after decision continue replay, got %+v", second)
	}
	actions, err := store.RecoveryActions().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list recovery actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected exactly two persisted recovery actions (review + decision), got %d", len(actions))
	}
}

func TestRecordRecoveryActionRepairIntentPersistsWhileStillBlocked(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_broken_repair_intent",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff for repair intent test",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_repair_intent"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken repair handoff",
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create broken handoff: %v", err)
	}

	out, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID:  string(taskID),
		Kind:    recoveryaction.KindRepairIntentRecorded,
		Summary: "repair broken checkpoint reference",
	})
	if err != nil {
		t.Fatalf("record repair intent: %v", err)
	}
	if out.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required class, got %s", out.RecoveryClass)
	}
	if out.ReadyForNextRun {
		t.Fatal("repair intent must not claim next-run readiness")
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.LatestRecoveryAction == nil || inspectOut.LatestRecoveryAction.Kind != recoveryaction.KindRepairIntentRecorded {
		t.Fatalf("expected repair intent action in inspect output, got %+v", inspectOut.LatestRecoveryAction)
	}
	if inspectOut.Recovery == nil || !strings.Contains(strings.ToLower(inspectOut.Recovery.Reason), "repair intent recorded") {
		t.Fatalf("expected recovery reason to reflect repair intent, got %+v", inspectOut.Recovery)
	}
}

func TestRecordRecoveryActionRejectsInvalidPosture(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	_, err := coord.RecordRecoveryAction(context.Background(), RecordRecoveryActionRequest{
		TaskID: string(taskID),
		Kind:   recoveryaction.KindDecisionContinue,
	})
	if err == nil || !strings.Contains(err.Error(), string(RecoveryClassDecisionRequired)) {
		t.Fatalf("expected decision-required posture rejection, got %v", err)
	}
}

func TestInspectTaskSurfacesRecoveryIssuesForBrokenHandoffState(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_broken_inspect_recovery",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff for inspect recovery test",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_for_inspect_recovery"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken inspect handoff",
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create broken handoff packet: %v", err)
	}

	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	if inspectOut.Handoff == nil || inspectOut.Handoff.HandoffID != packet.HandoffID {
		t.Fatalf("expected inspect handoff %s, got %+v", packet.HandoffID, inspectOut.Handoff)
	}
	if inspectOut.Recovery == nil {
		t.Fatal("expected inspect recovery assessment")
	}
	if inspectOut.Recovery.RecoveryClass != RecoveryClassRepairRequired {
		t.Fatalf("expected repair-required recovery class, got %s", inspectOut.Recovery.RecoveryClass)
	}
	if len(inspectOut.Recovery.Issues) == 0 {
		t.Fatal("expected inspect recovery issues for broken handoff state")
	}
	foundCheckpointIssue := false
	for _, issue := range inspectOut.Recovery.Issues {
		if strings.Contains(strings.ToLower(issue.Message), "missing checkpoint") {
			foundCheckpointIssue = true
			break
		}
	}
	if !foundCheckpointIssue {
		t.Fatalf("expected missing-checkpoint issue, got %+v", inspectOut.Recovery.Issues)
	}
}

func TestContinueBlockedWhenBriefMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	start, err := coord.StartTask(context.Background(), "No brief continue", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(start.TaskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "inconsistent") {
		t.Fatalf("expected canonical inconsistent response, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenCheckpointBriefMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_missing_brief"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            common.BriefID("brf_missing_checkpoint"),
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with bad checkpoint brief: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "missing brief") {
		t.Fatalf("expected canonical missing-brief message, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenCheckpointRunMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_missing_run"),
		TaskID:             taskID,
		RunID:              common.RunID("run_missing_for_checkpoint"),
		CreatedAt:          time.Now().UTC().Add(5 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "bad checkpoint for missing run test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue with bad checkpoint run: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "missing run") {
		t.Fatalf("expected canonical missing-run message, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenRunningCheckpointLinkageBroken(t *testing.T) {
	store := newTestStore(t)
	taskID := setupTaskWithBrief(t, newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess()))

	failCoord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    &failingSynthesizer{err: errors.New("run synth failure")},
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new failing coordinator: %v", err)
	}
	if _, err := failCoord.RunTask(context.Background(), RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"}); err == nil {
		t.Fatal("expected staged finalization failure to leave RUNNING state")
	}

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	lastEvent, err := latestEventID(store, taskID)
	if err != nil {
		t.Fatalf("latest event id: %v", err)
	}
	bad := checkpoint.Checkpoint{
		Version:            1,
		CheckpointID:       common.CheckpointID("chk_bad_running_linkage"),
		TaskID:             taskID,
		RunID:              "",
		CreatedAt:          time.Now().UTC().Add(10 * time.Second),
		Trigger:            checkpoint.TriggerManual,
		CapsuleVersion:     caps.Version,
		Phase:              caps.CurrentPhase,
		Anchor:             anchorFromCapsule(caps),
		IntentID:           caps.CurrentIntentID,
		BriefID:            caps.CurrentBriefID,
		ContextPackID:      "",
		LastEventID:        lastEvent,
		PendingDecisionIDs: []common.DecisionID{},
		ResumeDescriptor:   "broken checkpoint linkage for test",
		IsResumable:        true,
	}
	if err := store.Checkpoints().Create(bad); err != nil {
		t.Fatalf("create bad checkpoint: %v", err)
	}

	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "inconsistent") {
		t.Fatalf("expected inconsistent canonical response, got %q", out.CanonicalResponse)
	}
}

func TestContinueBlockedWhenLatestHandoffCheckpointMissing(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	caps, err := store.Capsules().Get(taskID)
	if err != nil {
		t.Fatalf("get capsule: %v", err)
	}
	briefRec, err := store.Briefs().Get(caps.CurrentBriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}

	packet := handoff.Packet{
		Version:          1,
		HandoffID:        "hnd_missing_checkpoint_for_continue",
		TaskID:           taskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     rundomain.WorkerKindCodex,
		TargetWorker:     rundomain.WorkerKindClaude,
		HandoffMode:      handoff.ModeResume,
		Reason:           "broken handoff state",
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     common.CheckpointID("chk_missing_for_handoff"),
		BriefID:          briefRec.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       anchorFromCapsule(caps),
		IsResumable:      true,
		ResumeDescriptor: "broken handoff packet for continue validation",
		Goal:             caps.Goal,
		BriefObjective:   briefRec.Objective,
		NormalizedAction: briefRec.NormalizedAction,
		Constraints:      append([]string{}, briefRec.Constraints...),
		DoneCriteria:     append([]string{}, briefRec.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Handoffs().Create(packet); err != nil {
		t.Fatalf("create broken handoff packet: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeBlockedInconsistent {
		t.Fatalf("expected blocked inconsistent outcome, got %s", out.Outcome)
	}
	if !strings.Contains(strings.ToLower(out.CanonicalResponse), "handoff") {
		t.Fatalf("expected handoff-related inconsistency, got %q", out.CanonicalResponse)
	}
}

func TestContinueSafeAssessmentDoesNotRequireWriteTransaction(t *testing.T) {
	base := newTestStore(t)
	baseCoord := newTestCoordinator(t, base, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, baseCoord)
	seed, err := baseCoord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	counting := &txCountingStore{base: base}
	coord, err := NewCoordinator(Dependencies{
		Store:          counting,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  newFakeAdapterSuccess(),
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: defaultAnchor(),
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe outcome, got %s", out.Outcome)
	}
	if out.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected checkpoint reuse %s, got %s", seed.CheckpointID, out.CheckpointID)
	}
	if counting.withTxCount < 1 {
		t.Fatalf("expected lightweight durable write path for no-op safe continue")
	}
}

func TestContinueSafeReuseDoesNotCreateCheckpointChurn(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)
	seed, err := coord.CreateCheckpoint(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	first, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("first continue: %v", err)
	}
	second, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("second continue: %v", err)
	}
	if first.CheckpointID != seed.CheckpointID || second.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected checkpoint reuse across continues, got first=%s second=%s seed=%s", first.CheckpointID, second.CheckpointID, seed.CheckpointID)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.CheckpointID != seed.CheckpointID {
		t.Fatalf("expected no checkpoint churn, latest=%s seed=%s", latestCheckpoint.CheckpointID, seed.CheckpointID)
	}
}

func TestSafeContinueCreatesCheckpointWithContinueTrigger(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	out, err := coord.ContinueTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("continue task: %v", err)
	}
	if out.Outcome != ContinueOutcomeSafe {
		t.Fatalf("expected safe continue, got %s", out.Outcome)
	}
	latestCheckpoint, err := store.Checkpoints().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest checkpoint: %v", err)
	}
	if latestCheckpoint.Trigger != checkpoint.TriggerContinue {
		t.Fatalf("expected continue trigger, got %s", latestCheckpoint.Trigger)
	}
}

func setupTaskWithBrief(t *testing.T, coord *Coordinator) common.TaskID {
	t.Helper()
	start, err := coord.StartTask(context.Background(), "Run lifecycle test", "/tmp/repo")
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if _, err := coord.MessageTask(context.Background(), string(start.TaskID), "start implementation process"); err != nil {
		t.Fatalf("message task: %v", err)
	}
	return start.TaskID
}

func hasEvent(events []proof.Event, typ proof.EventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func countEvents(events []proof.Event, typ proof.EventType) int {
	count := 0
	for _, e := range events {
		if e.Type == typ {
			count++
		}
	}
	return count
}

func hasEventMust(t *testing.T, store storage.Store, taskID common.TaskID, typ proof.EventType) bool {
	t.Helper()
	events, err := store.Proofs().ListByTask(taskID, 200)
	if err != nil {
		t.Fatalf("list proof events: %v", err)
	}
	return hasEvent(events, typ)
}

func latestEventID(store storage.Store, taskID common.TaskID) (common.EventID, error) {
	events, err := store.Proofs().ListByTask(taskID, 1)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return "", nil
	}
	return events[len(events)-1].EventID, nil
}

func newTestCoordinator(t *testing.T, store *sqlite.Store, anchorProvider anchorgit.Provider, adapter adapter_contract.WorkerAdapter) *Coordinator {
	t.Helper()
	coord, err := NewCoordinator(Dependencies{
		Store:          store,
		IntentCompiler: NewIntentStubCompiler(),
		BriefBuilder:   NewBriefBuilderV1(nil, nil),
		WorkerAdapter:  adapter,
		Synthesizer:    canonical.NewSimpleSynthesizer(),
		AnchorProvider: anchorProvider,
		ShellSessions:  NewMemoryShellSessionRegistry(),
	})
	if err != nil {
		t.Fatalf("new coordinator: %v", err)
	}
	return coord
}

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tuku-test.db")
	store, err := sqlite.NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type staticAnchorProvider struct {
	snapshot anchorgit.Snapshot
}

func (p *staticAnchorProvider) Capture(_ context.Context, repoRoot string) anchorgit.Snapshot {
	out := p.snapshot
	if out.RepoRoot == "" {
		out.RepoRoot = repoRoot
	}
	if out.CapturedAt.IsZero() {
		out.CapturedAt = time.Now().UTC()
	}
	return out
}

func defaultAnchor() anchorgit.Provider {
	return &staticAnchorProvider{snapshot: anchorgit.Snapshot{RepoRoot: "/tmp/repo", Branch: "main", HeadSHA: "head-x", WorkingTreeDirty: false, CapturedAt: time.Unix(1700004000, 0).UTC()}}
}

type fakeWorkerAdapter struct {
	kind      adapter_contract.WorkerKind
	result    adapter_contract.ExecutionResult
	err       error
	called    bool
	lastReq   adapter_contract.ExecutionRequest
	onExecute func(req adapter_contract.ExecutionRequest)
}

func newFakeAdapterSuccess() *fakeWorkerAdapter {
	now := time.Now().UTC()
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode:          0,
			StartedAt:         now,
			EndedAt:           now.Add(200 * time.Millisecond),
			Stdout:            "implemented bounded step",
			Stderr:            "",
			ChangedFiles:      []string{"internal/orchestrator/service.go"},
			ValidationSignals: []string{"worker mentioned test activity"},
			Summary:           "bounded codex step complete",
		},
	}
}

func newFakeAdapterExitFailure() *fakeWorkerAdapter {
	now := time.Now().UTC()
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode:  1,
			StartedAt: now,
			EndedAt:   now.Add(100 * time.Millisecond),
			Stdout:    "attempted change",
			Stderr:    "test failed",
			Summary:   "run failed",
		},
	}
}

func newFakeAdapterError(err error) *fakeWorkerAdapter {
	return &fakeWorkerAdapter{
		kind: adapter_contract.WorkerCodex,
		result: adapter_contract.ExecutionResult{
			ExitCode: -1,
			Summary:  "adapter error",
		},
		err: err,
	}
}

func (f *fakeWorkerAdapter) Name() adapter_contract.WorkerKind {
	return f.kind
}

func (f *fakeWorkerAdapter) Execute(_ context.Context, req adapter_contract.ExecutionRequest, _ adapter_contract.WorkerEventSink) (adapter_contract.ExecutionResult, error) {
	f.called = true
	f.lastReq = req
	if f.onExecute != nil {
		f.onExecute(req)
	}
	out := f.result
	if out.WorkerRunID == "" {
		out.WorkerRunID = common.WorkerRunID("wrk_" + string(req.RunID))
	}
	if out.Command == "" {
		out.Command = "codex"
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now().UTC()
	}
	if out.EndedAt.IsZero() {
		out.EndedAt = out.StartedAt.Add(100 * time.Millisecond)
	}
	return out, f.err
}

var _ adapter_contract.WorkerAdapter = (*fakeWorkerAdapter)(nil)

type failingSynthesizer struct {
	err error
}

func (s *failingSynthesizer) Synthesize(_ context.Context, _ capsule.WorkCapsule, _ []proof.Event) (string, error) {
	return "", s.err
}

type faultInjectedStore struct {
	base            storage.Store
	failProofAppend bool
}

func (s *faultInjectedStore) Capsules() storage.CapsuleStore {
	return s.base.Capsules()
}

func (s *faultInjectedStore) Conversations() storage.ConversationStore {
	return s.base.Conversations()
}

func (s *faultInjectedStore) Intents() storage.IntentStore {
	return s.base.Intents()
}

func (s *faultInjectedStore) Briefs() storage.BriefStore {
	return s.base.Briefs()
}

func (s *faultInjectedStore) Proofs() storage.ProofStore {
	if !s.failProofAppend {
		return s.base.Proofs()
	}
	return &faultProofStore{base: s.base.Proofs()}
}

func (s *faultInjectedStore) Runs() storage.RunStore {
	return s.base.Runs()
}

func (s *faultInjectedStore) Checkpoints() storage.CheckpointStore {
	return s.base.Checkpoints()
}

func (s *faultInjectedStore) RecoveryActions() storage.RecoveryActionStore {
	return s.base.RecoveryActions()
}

func (s *faultInjectedStore) ContextPacks() storage.ContextPackStore {
	return s.base.ContextPacks()
}

func (s *faultInjectedStore) PolicyDecisions() storage.PolicyDecisionStore {
	return s.base.PolicyDecisions()
}

func (s *faultInjectedStore) WithTx(fn func(storage.Store) error) error {
	return s.base.WithTx(func(txStore storage.Store) error {
		wrapped := &faultInjectedStore{
			base:            txStore,
			failProofAppend: s.failProofAppend,
		}
		return fn(wrapped)
	})
}

type txCountingStore struct {
	base        storage.Store
	withTxCount int
}

func (s *txCountingStore) Capsules() storage.CapsuleStore {
	return s.base.Capsules()
}

func (s *txCountingStore) Conversations() storage.ConversationStore {
	return s.base.Conversations()
}

func (s *txCountingStore) Intents() storage.IntentStore {
	return s.base.Intents()
}

func (s *txCountingStore) Briefs() storage.BriefStore {
	return s.base.Briefs()
}

func (s *txCountingStore) Proofs() storage.ProofStore {
	return s.base.Proofs()
}

func (s *txCountingStore) Runs() storage.RunStore {
	return s.base.Runs()
}

func (s *txCountingStore) Checkpoints() storage.CheckpointStore {
	return s.base.Checkpoints()
}

func (s *txCountingStore) RecoveryActions() storage.RecoveryActionStore {
	return s.base.RecoveryActions()
}

func (s *txCountingStore) ContextPacks() storage.ContextPackStore {
	return s.base.ContextPacks()
}

func (s *txCountingStore) PolicyDecisions() storage.PolicyDecisionStore {
	return s.base.PolicyDecisions()
}

func (s *txCountingStore) WithTx(fn func(storage.Store) error) error {
	s.withTxCount++
	return s.base.WithTx(fn)
}

type faultProofStore struct {
	base storage.ProofStore
}

func (s *faultProofStore) Append(event proof.Event) error {
	return errors.New("forced proof append failure")
}

func (s *faultProofStore) ListByTask(taskID common.TaskID, limit int) ([]proof.Event, error) {
	return s.base.ListByTask(taskID, limit)
}
```
