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
	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/provider"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
	anchorgit "tuku/internal/git/anchor"
	"tuku/internal/ipc"
	"tuku/internal/orchestrator"
	"tuku/internal/response/canonical"
	daemonruntime "tuku/internal/runtime/daemon"
	"tuku/internal/storage/sqlite"
	tukububble "tuku/internal/tui/bubble"
	tukushell "tuku/internal/tui/shell"
)

// CLIApplication is the top-level command host for the user-facing Tuku CLI.
type CLIApplication struct {
	openShellFn         func(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error
	openFallbackShellFn func(ctx context.Context, cwd string, preference tukushell.WorkerPreference) error
	chooseWorkerFn      func(ctx context.Context, selection primaryWorkerSelectionContext) (tukushell.WorkerPreference, error)
}

type primaryWorkerSelectionContext struct {
	Remembered     tukushell.WorkerPreference
	Preferred      tukushell.WorkerPreference
	Recommendation provider.Recommendation
	Notice         string
	Prerequisites  map[tukushell.WorkerPreference]tukushell.WorkerPrerequisite
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
	loadPrimaryWorkerPref  = loadPrimaryWorkerPreference
	savePrimaryWorkerPref  = savePrimaryWorkerPreference
	clearPrimaryLauncherFn = clearPrimaryLauncherSurface
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
	case "codex", "claude":
		return a.runPrimaryEntry(ctx, socketPath, []string{args[0]})
	case "chat":
		return a.runPrimaryEntry(ctx, socketPath, args[1:])
	case "ui":
		return a.runPrimaryBubbleEntry(ctx, socketPath, args[1:])
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
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskStatus, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskStatusResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskStatusHuman(os.Stdout, out)
		}
		return writeJSON(os.Stdout, out)

	case "shell":
		if len(args) > 1 && strings.EqualFold(strings.TrimSpace(args[1]), "transcript") {
			return runShellTranscriptCommand(ctx, socketPath, args[2:])
		}
		fs := flag.NewFlagSet("shell", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
		reattach := fs.String("reattach", "", "optional durable shell session id to reattach")
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
		return a.openShellWithReattach(ctx, socketPath, *task, preference, *reattach)

	case "shell-sessions":
		fs := flag.NewFlagSet("shell-sessions", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID(*task)})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskShellSessions, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskShellSessionsResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskShellSessionsHuman(ctx, socketPath, os.Stdout, out)
		}
		return writeJSON(os.Stdout, out)

	case "transition":
		return runTransitionHistoryCommand(ctx, socketPath, args[1:])
	case "incident":
		if len(args) > 1 && strings.EqualFold(strings.TrimSpace(args[1]), "triage") {
			if len(args) > 2 && strings.EqualFold(strings.TrimSpace(args[2]), "history") {
				return runContinuityIncidentTriageHistoryCommand(ctx, socketPath, args[3:])
			}
			return runContinuityIncidentTriageCommand(ctx, socketPath, args[2:])
		}
		if len(args) > 1 && strings.EqualFold(strings.TrimSpace(args[1]), "followup") {
			if len(args) > 2 && strings.EqualFold(strings.TrimSpace(args[2]), "history") {
				return runContinuityIncidentFollowUpHistoryCommand(ctx, socketPath, args[3:])
			}
			return runContinuityIncidentFollowUpCommand(ctx, socketPath, args[2:])
		}
		if len(args) > 1 && strings.EqualFold(strings.TrimSpace(args[1]), "closure") {
			return runContinuityIncidentClosureCommand(ctx, socketPath, args[2:])
		}
		if len(args) > 1 && strings.EqualFold(strings.TrimSpace(args[1]), "risk") {
			return runContinuityIncidentTaskRiskCommand(ctx, socketPath, args[2:])
		}
		return runContinuityIncidentCommand(ctx, socketPath, args[1:])

	case "run":
		fs := flag.NewFlagSet("run", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		action := fs.String("action", "start", "run action: start|complete|interrupt")
		mode := fs.String("mode", "real", "run mode: real|noop")
		runID := fs.String("run-id", "", "run id for complete/interrupt actions")
		shellSessionID := fs.String("shell-session", "", "optional shell session id to link execution evidence")
		simInterrupt := fs.Bool("simulate-interrupt", false, "start then immediately interrupt")
		reason := fs.String("reason", "", "interruption reason")
		human := fs.Bool("human", false, "render compact human-readable operator view")
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
			"shell_session_id":    *shellSessionID,
			"simulate_interrupt":  *simInterrupt,
			"interruption_reason": *reason,
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskRun, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskRunResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskActionHuman(
				ctx,
				socketPath,
				os.Stdout,
				"run "+strings.ToLower(strings.TrimSpace(*action)),
				out.TaskID,
				fmt.Sprintf("run %s status=%s phase=%s", nonEmpty(string(out.RunID), "n/a"), nonEmpty(string(out.RunStatus), "unknown"), nonEmpty(string(out.Phase), "unknown")),
				out.CanonicalResponse,
			)
		}
		return writeJSON(os.Stdout, out)

	case "next":
		fs := flag.NewFlagSet("next", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable operator result view")
		ackReviewGap := fs.Bool("ack-review-gap", false, "record explicit acknowledgment of stale/unreviewed retained transcript evidence before executing the primary operator step")
		ackSession := fs.String("ack-session", "", "optional shell session id for review-gap acknowledgment scope")
		ackKind := fs.String("ack-kind", "", "optional acknowledgment kind override: missing_review_marker|stale_review|source_scoped_only|source_scoped_stale")
		ackSummary := fs.String("ack-summary", "", "optional bounded acknowledgment note")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if trimmedKind := strings.TrimSpace(*ackKind); trimmedKind != "" {
			switch trimmedKind {
			case "missing_review_marker", "stale_review", "source_scoped_only", "source_scoped_stale":
			default:
				return fmt.Errorf("invalid --ack-kind: %q (expected missing_review_marker|stale_review|source_scoped_only|source_scoped_stale)", trimmedKind)
			}
		}
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{
			TaskID:                      common.TaskID(*task),
			AcknowledgeReviewGap:        *ackReviewGap,
			ReviewGapSessionID:          strings.TrimSpace(*ackSession),
			ReviewGapAcknowledgmentKind: strings.TrimSpace(*ackKind),
			ReviewGapSummary:            strings.TrimSpace(*ackSummary),
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecutePrimaryOperatorStep, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskExecutePrimaryOperatorStepResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskNextHuman(os.Stdout, out)
		}
		return writeJSON(os.Stdout, out)

	case "operator":
		if len(args) < 2 {
			return errors.New("usage: tuku operator acknowledge-review-gap --task <TASK_ID> [--session <SHELL_SESSION_ID>] [--kind <CLASS>] [--summary <TEXT>]")
		}
		switch strings.ToLower(strings.TrimSpace(args[1])) {
		case "acknowledge-review-gap":
			fs := flag.NewFlagSet("operator acknowledge-review-gap", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			session := fs.String("session", "", "optional shell session id")
			kind := fs.String("kind", "", "optional acknowledgment class: missing_review_marker|stale_review|source_scoped_only|source_scoped_stale")
			summary := fs.String("summary", "", "optional bounded acknowledgment note")
			actionContext := fs.String("action-context", "", "optional progression context label")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if strings.TrimSpace(*task) == "" {
				return errors.New("--task is required")
			}
			if trimmedKind := strings.TrimSpace(*kind); trimmedKind != "" {
				switch trimmedKind {
				case "missing_review_marker", "stale_review", "source_scoped_only", "source_scoped_stale":
				default:
					return fmt.Errorf("invalid --kind: %q (expected missing_review_marker|stale_review|source_scoped_only|source_scoped_stale)", trimmedKind)
				}
			}
			payload, err := json.Marshal(ipc.TaskOperatorAcknowledgeReviewGapRequest{
				TaskID:        common.TaskID(strings.TrimSpace(*task)),
				SessionID:     strings.TrimSpace(*session),
				Kind:          strings.TrimSpace(*kind),
				Summary:       strings.TrimSpace(*summary),
				ActionContext: strings.TrimSpace(*actionContext),
			})
			if err != nil {
				return err
			}
			resp, err := ipcCall(ctx, socketPath, ipc.Request{
				RequestID: requestID(),
				Method:    ipc.MethodOperatorAcknowledgeReviewGap,
				Payload:   payload,
			})
			if err != nil {
				return err
			}
			var out ipc.TaskOperatorAcknowledgeReviewGapResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			return writeOperatorReviewGapAcknowledgment(os.Stdout, out)
		default:
			return fmt.Errorf("unknown operator command %q", args[1])
		}

	case "continue":
		fs := flag.NewFlagSet("continue", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskContinueRequest{TaskID: common.TaskID(*task)})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodContinueTask, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskContinueResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeRecoveryActionHuman(ctx, socketPath, os.Stdout, "continue", out.TaskID, strings.TrimSpace(out.Outcome), out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
		}
		return writeJSON(os.Stdout, out)

	case "checkpoint":
		fs := flag.NewFlagSet("checkpoint", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskCheckpointRequest{TaskID: common.TaskID(*task)})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateCheckpoint, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskCheckpointResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			result := fmt.Sprintf(
				"checkpoint %s trigger=%s resumable=%t",
				nonEmpty(string(out.CheckpointID), "n/a"),
				nonEmpty(string(out.Trigger), "unknown"),
				out.IsResumable,
			)
			return writeTaskActionHuman(ctx, socketPath, os.Stdout, "checkpoint", out.TaskID, result, out.CanonicalResponse)
		}
		return writeJSON(os.Stdout, out)

	case "recovery":
		if len(args) < 2 {
			return errors.New("usage: tuku recovery <record|review-interrupted|resume-interrupted|rebrief|continue> ...")
		}
		switch args[1] {
		case "record":
			fs := flag.NewFlagSet("recovery record", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			action := fs.String("action", "", "recovery action kind")
			summary := fs.String("summary", "", "optional recovery action summary")
			note := fs.String("note", "", "optional recovery action note")
			human := fs.Bool("human", false, "render compact human-readable operator view")
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
			if *human {
				return writeRecoveryActionHuman(ctx, socketPath, os.Stdout, "recovery record", out.TaskID, out.Action.Summary, out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
			}
			return writeJSON(os.Stdout, out)
		case "review-interrupted":
			fs := flag.NewFlagSet("recovery review-interrupted", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			summary := fs.String("summary", "", "optional interrupted recovery summary")
			note := fs.String("note", "", "optional interrupted recovery note")
			human := fs.Bool("human", false, "render compact human-readable operator view")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskReviewInterruptedRunRequest{
				TaskID:  common.TaskID(*task),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodReviewInterruptedRun, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRecordRecoveryActionResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			if *human {
				return writeRecoveryActionHuman(ctx, socketPath, os.Stdout, "recovery review-interrupted", out.TaskID, out.Action.Summary, out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
			}
			return writeJSON(os.Stdout, out)
		case "continue":
			fs := flag.NewFlagSet("recovery continue", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			human := fs.Bool("human", false, "render compact human-readable operator view")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			payload, _ := json.Marshal(ipc.TaskContinueRecoveryRequest{TaskID: common.TaskID(*task)})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteContinueRecovery, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskContinueRecoveryResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			if *human {
				return writeRecoveryActionHuman(ctx, socketPath, os.Stdout, "recovery continue", out.TaskID, out.Action.Summary, out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
			}
			return writeJSON(os.Stdout, out)
		case "rebrief":
			fs := flag.NewFlagSet("recovery rebrief", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			human := fs.Bool("human", false, "render compact human-readable operator view")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			payload, _ := json.Marshal(ipc.TaskRebriefRequest{TaskID: common.TaskID(*task)})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteRebrief, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskRebriefResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			if *human {
				return writeRecoveryActionHuman(ctx, socketPath, os.Stdout, "recovery rebrief", out.TaskID, "", out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
			}
			return writeJSON(os.Stdout, out)
		case "resume-interrupted":
			fs := flag.NewFlagSet("recovery resume-interrupted", flag.ContinueOnError)
			task := fs.String("task", "", "task id")
			summary := fs.String("summary", "", "optional interrupted resume summary")
			note := fs.String("note", "", "optional interrupted resume note")
			human := fs.Bool("human", false, "render compact human-readable operator view")
			if err := fs.Parse(args[2:]); err != nil {
				return err
			}
			if *task == "" {
				return errors.New("--task is required")
			}
			notes := []string{}
			if trimmed := strings.TrimSpace(*note); trimmed != "" {
				notes = append(notes, trimmed)
			}
			payload, _ := json.Marshal(ipc.TaskInterruptedResumeRequest{
				TaskID:  common.TaskID(*task),
				Summary: strings.TrimSpace(*summary),
				Notes:   notes,
			})
			resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodExecuteInterruptedResume, Payload: payload})
			if err != nil {
				return err
			}
			var out ipc.TaskInterruptedResumeResponse
			if err := json.Unmarshal(resp.Payload, &out); err != nil {
				return err
			}
			if *human {
				return writeRecoveryActionHuman(ctx, socketPath, os.Stdout, "recovery resume-interrupted", out.TaskID, out.Action.Summary, out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
			}
			return writeJSON(os.Stdout, out)
		default:
			return fmt.Errorf("unknown recovery command: %s", args[1])
		}

	case "handoff-followthrough":
		fs := flag.NewFlagSet("handoff-followthrough", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		kindValue := fs.String("kind", "", "follow-through kind")
		summary := fs.String("summary", "", "optional follow-through summary")
		note := fs.String("note", "", "optional follow-through note")
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *kindValue == "" {
			return errors.New("--kind is required")
		}
		kind, err := parseHandoffFollowThroughKind(*kindValue)
		if err != nil {
			return err
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordRequest{
			TaskID:  common.TaskID(*task),
			Kind:    string(kind),
			Summary: strings.TrimSpace(*summary),
			Notes:   notes,
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordHandoffFollowThrough, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffFollowThroughRecordResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			result := ""
			if out.Record != nil {
				result = out.Record.Summary
			}
			return writeHandoffActionHuman(ctx, socketPath, os.Stdout, "handoff followthrough", out.TaskID, result, out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
		}
		return writeJSON(os.Stdout, out)

	case "handoff-create":
		fs := flag.NewFlagSet("handoff-create", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		target := fs.String("target", string(rundomain.WorkerKindClaude), "target worker (claude)")
		mode := fs.String("mode", string(handoff.ModeResume), "handoff mode: resume|review|takeover")
		reason := fs.String("reason", "", "handoff reason")
		note := fs.String("note", "", "optional handoff note")
		human := fs.Bool("human", false, "render compact human-readable operator view")
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
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodCreateHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffCreateResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeHandoffActionHuman(
				ctx,
				socketPath,
				os.Stdout,
				"handoff create",
				out.TaskID,
				fmt.Sprintf("handoff %s status=%s target=%s", nonEmpty(out.HandoffID, "n/a"), nonEmpty(out.Status, "unknown"), nonEmpty(string(out.TargetWorker), "unknown")),
				"",
				"",
				"",
				out.CanonicalResponse,
			)
		}
		return writeJSON(os.Stdout, out)

	case "handoff-accept":
		fs := flag.NewFlagSet("handoff-accept", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id")
		acceptedBy := fs.String("by", string(rundomain.WorkerKindClaude), "accepted-by worker")
		note := fs.String("note", "", "optional acceptance note")
		human := fs.Bool("human", false, "render compact human-readable operator view")
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
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodAcceptHandoff, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffAcceptResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeHandoffActionHuman(
				ctx,
				socketPath,
				os.Stdout,
				"handoff accept",
				out.TaskID,
				fmt.Sprintf("handoff %s status=%s", nonEmpty(out.HandoffID, "n/a"), nonEmpty(out.Status, "unknown")),
				"",
				"",
				"",
				out.CanonicalResponse,
			)
		}
		return writeJSON(os.Stdout, out)

	case "handoff-launch":
		fs := flag.NewFlagSet("handoff-launch", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "handoff packet id (optional; defaults to latest for task)")
		target := fs.String("target", "", "target worker override (optional; must match packet target if set)")
		human := fs.Bool("human", false, "render compact human-readable operator view")
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
		if *human {
			return writeHandoffActionHuman(ctx, socketPath, os.Stdout, "handoff launch", out.TaskID, fmt.Sprintf("launch status %s", nonEmpty(out.LaunchStatus, "unknown")), "", "", "", out.CanonicalResponse)
		}
		return writeJSON(os.Stdout, out)

	case "handoff-resolve":
		fs := flag.NewFlagSet("handoff-resolve", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		handoffID := fs.String("handoff", "", "optional handoff id")
		kindValue := fs.String("kind", "", "resolution kind")
		summary := fs.String("summary", "", "optional resolution summary")
		note := fs.String("note", "", "optional resolution note")
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		if *kindValue == "" {
			return errors.New("--kind is required")
		}
		kind, err := parseHandoffResolutionKind(*kindValue)
		if err != nil {
			return err
		}
		notes := []string{}
		if trimmed := strings.TrimSpace(*note); trimmed != "" {
			notes = append(notes, trimmed)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordRequest{
			TaskID:    common.TaskID(*task),
			HandoffID: strings.TrimSpace(*handoffID),
			Kind:      string(kind),
			Summary:   strings.TrimSpace(*summary),
			Notes:     notes,
		})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodRecordHandoffResolution, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskHandoffResolutionRecordResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			result := ""
			if out.Record != nil {
				result = out.Record.Summary
			}
			return writeHandoffActionHuman(ctx, socketPath, os.Stdout, "handoff resolve", out.TaskID, result, out.RecoveryClass, out.RecommendedAction, out.RecoveryReason, out.CanonicalResponse)
		}
		return writeJSON(os.Stdout, out)

	case "inspect":
		fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable operator view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *task == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(map[string]any{"task_id": *task})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskInspect, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskInspectResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskInspectHuman(os.Stdout, out)
		}
		return writeJSON(os.Stdout, out)
	case "intent":
		fs := flag.NewFlagSet("intent", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*task) == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskIntentRequest{TaskID: common.TaskID(strings.TrimSpace(*task))})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskIntent, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskIntentResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		return writeTaskIntent(os.Stdout, out)
	case "brief":
		fs := flag.NewFlagSet("brief", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable operator brief view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*task) == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskBriefRequest{TaskID: common.TaskID(strings.TrimSpace(*task))})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskBrief, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskBriefResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskBriefHuman(os.Stdout, out)
		}
		return writeJSON(os.Stdout, out)
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable pre-dispatch plan view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*task) == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskStatusRequest{TaskID: common.TaskID(strings.TrimSpace(*task))})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskStatus, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskStatusResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskPlanHuman(os.Stdout, out)
		}
		return writeJSON(os.Stdout, taskPlanViewFromStatus(out))
	case "benchmark":
		fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
		task := fs.String("task", "", "task id")
		human := fs.Bool("human", false, "render compact human-readable benchmark view")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if strings.TrimSpace(*task) == "" {
			return errors.New("--task is required")
		}
		payload, _ := json.Marshal(ipc.TaskBenchmarkRequest{TaskID: common.TaskID(strings.TrimSpace(*task))})
		resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskBenchmark, Payload: payload})
		if err != nil {
			return err
		}
		var out ipc.TaskBenchmarkResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			return err
		}
		if *human {
			return writeTaskBenchmarkHuman(os.Stdout, out)
		}
		return writeJSON(os.Stdout, out)

	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func cliUsage() string {
	return "usage: tuku [chat|codex|claude] | tuku <ui|start|message|shell|shell-sessions|transition|incident|run|next|operator|continue|checkpoint|recovery|handoff-create|handoff-accept|handoff-launch|handoff-followthrough|handoff-resolve|status|inspect|intent|brief|plan|benchmark|help> [flags]\n       tuku chat [codex|claude] [--worker auto|codex|claude]\n       tuku ui [--worker auto|codex|claude]\n       tuku status --task <TASK_ID> [--human]\n       tuku inspect --task <TASK_ID> [--human]\n       tuku intent --task <TASK_ID>\n       tuku brief --task <TASK_ID> [--human]\n       tuku plan --task <TASK_ID> [--human]\n       tuku benchmark --task <TASK_ID> [--human]\n       tuku next --task <TASK_ID> [--human] [--ack-review-gap] [--ack-session <SHELL_SESSION_ID>] [--ack-kind missing_review_marker|stale_review|source_scoped_only|source_scoped_stale] [--ack-summary TEXT]\n       tuku run --task <TASK_ID> [--action start|complete|interrupt] [--mode real|noop] [--run-id <RUN_ID>] [--shell-session <SHELL_SESSION_ID>] [--simulate-interrupt] [--reason TEXT] [--human]\n       tuku shell-sessions --task <TASK_ID> [--human]\n       tuku continue --task <TASK_ID> [--human]\n       tuku checkpoint --task <TASK_ID> [--human]\n       tuku handoff-create --task <TASK_ID> [--target claude] [--mode resume|review|takeover] [--reason TEXT] [--note TEXT] [--human]\n       tuku handoff-accept --task <TASK_ID> --handoff <HANDOFF_ID> [--by claude] [--note TEXT] [--human]\n       tuku handoff-launch --task <TASK_ID> [--handoff <HANDOFF_ID>] [--target claude] [--human]\n       tuku handoff-followthrough --task <TASK_ID> --kind proof-of-life-observed|continuation-confirmed|continuation-unknown [--summary TEXT] [--note TEXT] [--human]\n       tuku handoff-resolve --task <TASK_ID> --kind superseded-by-local|completed-downstream|abandoned [--handoff <HANDOFF_ID>] [--summary TEXT] [--note TEXT] [--human]\n       tuku recovery record --task <TASK_ID> --action <KIND> [--summary TEXT] [--note TEXT] [--human]\n       tuku recovery review-interrupted --task <TASK_ID> [--summary TEXT] [--note TEXT] [--human]\n       tuku recovery resume-interrupted --task <TASK_ID> [--summary TEXT] [--note TEXT] [--human]\n       tuku recovery continue --task <TASK_ID> [--human]\n       tuku recovery rebrief --task <TASK_ID> [--human]\n       tuku shell transcript --task <TASK_ID> --session <SHELL_SESSION_ID> [--limit N] [--before-seq SEQ] [--source worker_output|system_note|fallback_note]\n       tuku shell transcript review --task <TASK_ID> --session <SHELL_SESSION_ID> --up-to-seq <SEQ> [--source worker_output|system_note|fallback_note] [--summary TEXT]\n       tuku shell transcript history --task <TASK_ID> --session <SHELL_SESSION_ID> [--limit N] [--source worker_output|system_note|fallback_note]\n       tuku transition history --task <TASK_ID> [--limit N] [--before-receipt <RECEIPT_ID>] [--kind handoff_launch|handoff_resolution] [--handoff <HANDOFF_ID>]\n       tuku incident --task <TASK_ID> [--anchor-transition <RECEIPT_ID>] [--transitions N] [--runs N] [--recovery N] [--proofs N] [--acks N]\n       tuku incident triage --task <TASK_ID> [--anchor latest|receipt] [--receipt <RECEIPT_ID>] --posture triaged|needs_follow_up|deferred [--summary TEXT]\n       tuku incident triage history --task <TASK_ID> [--limit N] [--before-receipt <RECEIPT_ID>] [--anchor <TRANSITION_RECEIPT_ID>] [--posture triaged|needs_follow_up|deferred]\n       tuku incident followup --task <TASK_ID> [--anchor latest|receipt] [--receipt <TRANSITION_RECEIPT_ID>] [--triage-receipt <TRIAGE_RECEIPT_ID>] --action recorded_pending|progressed|closed|reopened [--summary TEXT]\n       tuku incident followup history --task <TASK_ID> [--limit N] [--before-receipt <FOLLOWUP_RECEIPT_ID>] [--anchor <TRANSITION_RECEIPT_ID>] [--triage-receipt <TRIAGE_RECEIPT_ID>] [--action recorded_pending|progressed|closed|reopened]\n       tuku incident closure --task <TASK_ID> [--limit N] [--before-receipt <FOLLOWUP_RECEIPT_ID>]\n       tuku incident risk --task <TASK_ID> [--limit N] [--before-receipt <FOLLOWUP_RECEIPT_ID>]\n       tuku operator acknowledge-review-gap --task <TASK_ID> [--session <SHELL_SESSION_ID>] [--kind missing_review_marker|stale_review|source_scoped_only|source_scoped_stale] [--summary TEXT]"
}

func parseRecoveryActionKind(value string) (recoveryaction.Kind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "failed-run-reviewed":
		return recoveryaction.KindFailedRunReviewed, nil
	case "interrupted-run-reviewed":
		return recoveryaction.KindInterruptedRunReviewed, nil
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

func parseHandoffFollowThroughKind(value string) (handoff.FollowThroughKind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "proof-of-life-observed":
		return handoff.FollowThroughProofOfLifeObserved, nil
	case "continuation-confirmed":
		return handoff.FollowThroughContinuationConfirmed, nil
	case "continuation-unknown":
		return handoff.FollowThroughContinuationUnknown, nil
	case "stalled-review-required":
		return handoff.FollowThroughStalledReviewRequired, nil
	default:
		return "", fmt.Errorf("unsupported handoff follow-through kind %q", value)
	}
}

func parseHandoffResolutionKind(value string) (handoff.ResolutionKind, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "abandoned":
		return handoff.ResolutionAbandoned, nil
	case "superseded-by-local":
		return handoff.ResolutionSupersededByLocal, nil
	case "closed-unproven":
		return handoff.ResolutionClosedUnproven, nil
	case "reviewed-stale":
		return handoff.ResolutionReviewedStale, nil
	default:
		return "", fmt.Errorf("unsupported handoff resolution kind %q", value)
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
	if configured := cleanPathFromEnv("TUKU_DATA_DIR"); configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Tuku"), nil
}

func defaultDBPath() (string, error) {
	if configured := cleanPathFromEnv("TUKU_DB_PATH"); configured != "" {
		return configured, nil
	}
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tuku.db"), nil
}

func defaultSocketPath() (string, error) {
	if configured := cleanPathFromEnv("TUKU_SOCKET_PATH"); configured != "" {
		return configured, nil
	}
	root, err := defaultRunRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "tukud.sock"), nil
}

func defaultRunRoot() (string, error) {
	if configured := cleanPathFromEnv("TUKU_RUN_DIR"); configured != "" {
		return configured, nil
	}
	if configuredDataRoot := cleanPathFromEnv("TUKU_DATA_DIR"); configuredDataRoot != "" {
		return filepath.Join(configuredDataRoot, "run"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(home))
	return filepath.Join(os.TempDir(), "tuku", fmt.Sprintf("%x", digest[:6])), nil
}

func defaultCacheRoot() (string, error) {
	if configured := cleanPathFromEnv("TUKU_CACHE_DIR"); configured != "" {
		return configured, nil
	}
	root, err := defaultDataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "cache"), nil
}

func cleanPathFromEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func requestID() string {
	return fmt.Sprintf("req_%d", time.Now().UTC().UnixNano())
}

func (a *CLIApplication) runPrimaryEntry(ctx context.Context, socketPath string, args []string) error {
	explicitWorkerArg := ""
	if len(args) > 0 && !strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		explicitWorkerArg = strings.TrimSpace(args[0])
		args = args[1:]
	}
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	worker := fs.String("worker", "auto", "worker preference: auto|codex|claude")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected primary entry argument %q", fs.Args()[0])
	}
	cwd, repoRoot, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return err
	}
	selection := primaryWorkerSelectionContext{}
	resolution := repoShellTaskResolution{}
	var source tukushell.SnapshotSource
	if repoDetected {
		remembered, loadErr := loadPrimaryWorkerPref()
		if loadErr == nil {
			selection.Remembered = remembered
		}
		if explicit, _, explicitErr := parsePrimaryEntryExplicitWorkerPreference(explicitWorkerArg, *worker); explicitErr != nil {
			return explicitErr
		} else if !explicit {
			resolution, err = resolveShellTaskForRepoWithDaemonBootstrap(ctx, socketPath, repoRoot, orchestrator.DefaultRepoContinueGoal)
			if err != nil {
				return err
			}
			source, err = newPrimaryRepoSnapshotSource(socketPath, repoRoot, resolution.Created)
			if err != nil {
				return err
			}
			if snapshot, loadErr := source.Load(string(resolution.TaskID)); loadErr == nil {
				selection.Recommendation = primaryEntryWorkerRecommendation(snapshot, selection.Remembered)
			}
		}
	}
	preference, explicit, launcherUsed, err := resolvePrimaryEntryWorkerPreference(ctx, a, explicitWorkerArg, *worker, repoDetected, selection)
	if err != nil {
		if errors.Is(err, errPrimaryWorkerSelectionCancelled) {
			return nil
		}
		return err
	}
	if !repoDetected {
		openFallback := a.openPrimaryFallbackShell
		if a.openFallbackShellFn != nil {
			openFallback = a.openFallbackShellFn
		}
		return openFallback(ctx, cwd, preference)
	}
	if explicit {
		_ = savePrimaryWorkerPref(preference)
	}
	if resolution.TaskID == "" {
		resolution, err = resolveShellTaskForRepoWithDaemonBootstrap(ctx, socketPath, repoRoot, orchestrator.DefaultRepoContinueGoal)
		if err != nil {
			return err
		}
	}
	if launcherUsed {
		if err := clearPrimaryLauncherFn(os.Stdout); err != nil {
			return err
		}
	}
	if a.openShellFn != nil {
		return a.openShellFn(ctx, socketPath, string(resolution.TaskID), preference)
	}
	if source == nil {
		source, err = newPrimaryRepoSnapshotSource(socketPath, repoRoot, resolution.Created)
		if err != nil {
			return err
		}
	}
	return a.openShellWithSource(ctx, string(resolution.TaskID), preference, "", source)
}

func parsePrimaryEntryExplicitWorkerPreference(positional string, flagValue string) (bool, tukushell.WorkerPreference, error) {
	if raw := strings.TrimSpace(positional); raw != "" {
		preference, err := parseShellWorkerPreference(raw)
		if err != nil {
			return false, "", err
		}
		return preference != tukushell.WorkerPreferenceAuto, preference, nil
	}
	preference, err := parseShellWorkerPreference(flagValue)
	if err != nil {
		return false, "", err
	}
	return preference != tukushell.WorkerPreferenceAuto, preference, nil
}

func resolvePrimaryEntryWorkerPreference(ctx context.Context, app *CLIApplication, positional string, flagValue string, repoDetected bool, selection primaryWorkerSelectionContext) (tukushell.WorkerPreference, bool, bool, error) {
	explicit, preference, err := parsePrimaryEntryExplicitWorkerPreference(positional, flagValue)
	if err != nil {
		return "", false, false, err
	}
	if explicit {
		return preference, true, false, nil
	}
	if !repoDetected {
		return tukushell.WorkerPreferenceAuto, false, false, nil
	}
	if selection.Remembered == "" {
		selection.Remembered, err = loadPrimaryWorkerPref()
		if err != nil {
			selection.Remembered = tukushell.WorkerPreferenceAuto
		}
	}
	chooser := runPrimaryWorkerLauncher
	if app != nil && app.chooseWorkerFn != nil {
		chooser = app.chooseWorkerFn
	}
	chosen, err := chooser(ctx, selection)
	if err != nil {
		return "", false, true, err
	}
	if chosen == tukushell.WorkerPreferenceAuto {
		chosen = tukushell.WorkerPreferenceCodex
	}
	_ = savePrimaryWorkerPref(chosen)
	return chosen, true, true, nil
}

func primaryEntryWorkerRecommendation(snapshot tukushell.Snapshot, remembered tukushell.WorkerPreference) provider.Recommendation {
	briefPosture := ""
	requiresClarification := false
	validatorCount := 0
	rankedTargets := 0
	confidenceLevel := ""
	estimatedSavings := 0
	normalizedTaskType := ""
	latestRunStatus := ""
	handoffStatus := ""
	if snapshot.Brief != nil {
		briefPosture = snapshot.Brief.Posture
		requiresClarification = snapshot.Brief.RequiresClarification
		validatorCount = len(snapshot.Brief.ValidatorCommands)
		rankedTargets = len(snapshot.Brief.PromptTargets)
		confidenceLevel = snapshot.Brief.ConfidenceLevel
		estimatedSavings = snapshot.Brief.EstimatedTokenSavings
	}
	if snapshot.CompiledIntent != nil && strings.TrimSpace(snapshot.CompiledIntent.Class) != "" {
		normalizedTaskType = snapshot.CompiledIntent.Class
	}
	if snapshot.Run != nil {
		latestRunStatus = snapshot.Run.Status
	}
	if snapshot.Handoff != nil {
		handoffStatus = snapshot.Handoff.Status
	}
	return provider.Recommend(provider.Signals{
		NormalizedTaskType:    normalizedTaskType,
		BriefPosture:          briefPosture,
		RequiresClarification: requiresClarification,
		ValidatorCount:        validatorCount,
		RankedTargetCount:     rankedTargets,
		EstimatedTokenSavings: estimatedSavings,
		ConfidenceLevel:       confidenceLevel,
		LatestRunWorker:       provider.WorkerKind(strings.ToLower(strings.TrimSpace(snapshot.RunWorkerKind()))),
		LatestRunStatus:       strings.ToUpper(strings.TrimSpace(latestRunStatus)),
		HandoffTarget:         provider.WorkerKind(strings.ToLower(strings.TrimSpace(snapshot.HandoffTargetWorker()))),
		HandoffStatus:         strings.ToUpper(strings.TrimSpace(handoffStatus)),
		RememberedWorker:      provider.WorkerKind(strings.ToLower(strings.TrimSpace(string(remembered)))),
	})
}

func (a *CLIApplication) runPrimaryBubbleEntry(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	worker := fs.String("worker", "auto", "worker preference label: auto|codex|claude")
	if err := fs.Parse(args); err != nil {
		return err
	}
	preference, err := parseShellWorkerPreference(*worker)
	if err != nil {
		return err
	}

	_, repoRoot, repoDetected, err := resolvePrimaryEntryContext(ctx)
	if err != nil {
		return err
	}
	if !repoDetected {
		return errors.New("tuku ui requires a git repository; open a repo directory first")
	}

	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(ctx, socketPath, repoRoot, orchestrator.DefaultRepoContinueGoal)
	if err != nil {
		return err
	}

	workerLabel := strings.ToLower(strings.TrimSpace(*worker))
	if workerLabel == "" {
		workerLabel = strings.ToLower(strings.TrimSpace(string(preference)))
	}

	return tukububble.Run(ctx, tukububble.Config{
		Title:    "Tuku",
		TaskID:   string(resolution.TaskID),
		RepoRoot: repoRoot,
		Worker:   workerLabel,
		Send: func(ctx context.Context, prompt string) (tukububble.DispatchResult, error) {
			return dispatchPromptViaIPC(ctx, socketPath, resolution.TaskID, prompt)
		},
	})
}

func dispatchPromptViaIPC(ctx context.Context, socketPath string, taskID common.TaskID, prompt string) (tukububble.DispatchResult, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return tukububble.DispatchResult{}, errors.New("prompt is required")
	}

	msgPayload, _ := json.Marshal(ipc.TaskMessageRequest{
		TaskID:  taskID,
		Message: prompt,
	})
	msgResp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodSendMessage,
		Payload:   msgPayload,
	})
	if err != nil {
		return tukububble.DispatchResult{}, err
	}
	var msgOut ipc.TaskMessageResponse
	if err := json.Unmarshal(msgResp.Payload, &msgOut); err != nil {
		return tukububble.DispatchResult{}, err
	}

	runPayload, _ := json.Marshal(ipc.TaskRunRequest{
		TaskID: taskID,
		Action: "start",
		Mode:   "real",
	})
	runResp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskRun,
		Payload:   runPayload,
	})
	if err != nil {
		return tukububble.DispatchResult{}, err
	}
	var runOut ipc.TaskRunResponse
	if err := json.Unmarshal(runResp.Payload, &runOut); err != nil {
		return tukububble.DispatchResult{}, err
	}

	canonical := strings.TrimSpace(runOut.CanonicalResponse)
	if canonical == "" {
		canonical = strings.TrimSpace(msgOut.CanonicalResponse)
	}

	return tukububble.DispatchResult{
		CanonicalResponse: canonical,
		Phase:             nonEmpty(string(runOut.Phase), string(msgOut.Phase)),
		RunID:             string(runOut.RunID),
		RunStatus:         string(runOut.RunStatus),
	}, nil
}

func (a *CLIApplication) openPrimaryFallbackShell(ctx context.Context, cwd string, _ tukushell.WorkerPreference) error {
	scratchPath, err := resolveScratchPath(cwd)
	if err != nil {
		return err
	}
	return newPrimaryScratchIntake(cwd, scratchPath).Run(ctx)
}

func (a *CLIApplication) openShell(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference) error {
	return a.openShellWithReattach(ctx, socketPath, taskID, preference, "")
}

func (a *CLIApplication) openShellWithReattach(ctx context.Context, socketPath string, taskID string, preference tukushell.WorkerPreference, reattachSessionID string) error {
	return a.openShellWithSource(ctx, taskID, preference, strings.TrimSpace(reattachSessionID), tukushell.NewIPCSnapshotSource(socketPath))
}

func (a *CLIApplication) openShellWithSource(ctx context.Context, taskID string, preference tukushell.WorkerPreference, reattachSessionID string, source tukushell.SnapshotSource) error {
	shellApp := tukushell.NewApp(taskID, source)
	shellApp.WorkerPreference = preference
	shellApp.ReattachSessionID = strings.TrimSpace(reattachSessionID)
	if socketPath := snapshotSourceSocketPath(source); socketPath != "" {
		shellApp.MessageSender = tukushell.NewIPCTaskMessageSender(socketPath)
		shellApp.ActionExecutor = tukushell.NewIPCPrimaryActionExecutor(socketPath)
		shellApp.LifecycleSink = tukushell.NewIPCLifecycleSink(socketPath)
		shellApp.RegistrySink = tukushell.NewIPCSessionRegistryClient(socketPath)
		shellApp.RegistrySource = tukushell.NewIPCSessionRegistryClient(socketPath)
		shellApp.TranscriptSink = tukushell.NewIPCTranscriptSink(socketPath)
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
	if configured := cleanPathFromEnv("TUKU_CACHE_DIR"); configured != "" {
		root = configured
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

func parseTranscriptSourceFilter(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	switch raw {
	case "worker_output", "system_note", "fallback_note":
		return raw, nil
	default:
		return "", fmt.Errorf("invalid --source: %q (expected worker_output|system_note|fallback_note)", raw)
	}
}

func parseTransitionKindFilter(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	switch normalized {
	case "handoff-launch":
		return "HANDOFF_LAUNCH", nil
	case "handoff-resolution":
		return "HANDOFF_RESOLUTION", nil
	default:
		return "", fmt.Errorf("invalid --kind: %q (expected handoff_launch|handoff_resolution)", raw)
	}
}

func parseIncidentTriagePosture(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("--posture is required")
	}
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "triaged":
		return "TRIAGED", nil
	case "needs_follow_up":
		return "NEEDS_FOLLOW_UP", nil
	case "deferred":
		return "DEFERRED", nil
	default:
		return "", fmt.Errorf("invalid --posture: %q (expected triaged|needs_follow_up|deferred)", raw)
	}
}

func parseIncidentFollowUpAction(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("--action is required")
	}
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "recorded_pending", "recorded", "pending":
		return "RECORDED_PENDING", nil
	case "progressed":
		return "PROGRESSED", nil
	case "closed":
		return "CLOSED", nil
	case "reopened":
		return "REOPENED", nil
	default:
		return "", fmt.Errorf("invalid --action: %q (expected recorded_pending|progressed|closed|reopened)", raw)
	}
}

func runTransitionHistoryCommand(ctx context.Context, socketPath string, args []string) error {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "history") {
		args = args[1:]
	}
	fs := flag.NewFlagSet("transition history", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	limit := fs.Int("limit", 10, "max transition receipts to return in this page")
	beforeReceipt := fs.String("before-receipt", "", "optional exclusive receipt anchor for older-page reads")
	kind := fs.String("kind", "", "optional transition kind filter: handoff_launch|handoff_resolution")
	handoff := fs.String("handoff", "", "optional handoff id filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return errors.New("--task is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	kindFilter, err := parseTransitionKindFilter(*kind)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ipc.TaskTransitionHistoryRequest{
		TaskID:          common.TaskID(strings.TrimSpace(*task)),
		Limit:           *limit,
		BeforeReceiptID: common.EventID(strings.TrimSpace(*beforeReceipt)),
		TransitionKind:  kindFilter,
		HandoffID:       strings.TrimSpace(*handoff),
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskTransitionHistory,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskTransitionHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeTransitionHistory(os.Stdout, out)
}

func runContinuityIncidentCommand(ctx context.Context, socketPath string, args []string) error {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "slice") {
		args = args[1:]
	}
	fs := flag.NewFlagSet("incident", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	anchorTransition := fs.String("anchor-transition", "", "optional transition receipt id anchor")
	transitionLimit := fs.Int("transitions", 2, "neighbor transitions to include on each side of the anchor")
	runLimit := fs.Int("runs", 3, "max nearby runs to correlate in bounded incident window")
	recoveryLimit := fs.Int("recovery", 3, "max nearby recovery actions to correlate in bounded incident window")
	proofLimit := fs.Int("proofs", 8, "max nearby proof events to correlate in bounded incident window")
	ackLimit := fs.Int("acks", 3, "max transcript review-gap acknowledgments to include")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return errors.New("--task is required")
	}
	if *transitionLimit <= 0 {
		return errors.New("--transitions must be greater than zero")
	}
	if *runLimit <= 0 {
		return errors.New("--runs must be greater than zero")
	}
	if *recoveryLimit <= 0 {
		return errors.New("--recovery must be greater than zero")
	}
	if *proofLimit <= 0 {
		return errors.New("--proofs must be greater than zero")
	}
	if *ackLimit <= 0 {
		return errors.New("--acks must be greater than zero")
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentSliceRequest{
		TaskID:                    common.TaskID(strings.TrimSpace(*task)),
		AnchorTransitionReceiptID: common.EventID(strings.TrimSpace(*anchorTransition)),
		TransitionNeighborLimit:   *transitionLimit,
		RunLimit:                  *runLimit,
		RecoveryLimit:             *recoveryLimit,
		ProofLimit:                *proofLimit,
		AckLimit:                  *ackLimit,
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentSlice,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentSliceResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentSlice(os.Stdout, out)
}

func runContinuityIncidentTriageCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("incident triage", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	anchor := fs.String("anchor", "latest", "incident anchor mode: latest|receipt")
	receipt := fs.String("receipt", "", "explicit transition receipt id when --anchor receipt is used")
	posture := fs.String("posture", "", "triage posture: triaged|needs_follow_up|deferred")
	summary := fs.String("summary", "", "optional bounded triage note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID := strings.TrimSpace(*task)
	if taskID == "" {
		return errors.New("--task is required")
	}
	anchorMode := strings.ToLower(strings.TrimSpace(*anchor))
	switch anchorMode {
	case "", "latest":
		anchorMode = "latest"
	case "receipt":
	default:
		return fmt.Errorf("invalid --anchor: %q (expected latest|receipt)", *anchor)
	}
	receiptID := strings.TrimSpace(*receipt)
	if anchorMode == "receipt" && receiptID == "" {
		return errors.New("--receipt is required when --anchor receipt is used")
	}
	if anchorMode == "latest" && receiptID != "" {
		return errors.New("--receipt cannot be combined with --anchor latest")
	}
	postureValue, err := parseIncidentTriagePosture(*posture)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentTriageRequest{
		TaskID:                    common.TaskID(taskID),
		AnchorMode:                anchorMode,
		AnchorTransitionReceiptID: common.EventID(receiptID),
		Posture:                   postureValue,
		Summary:                   strings.TrimSpace(*summary),
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentTriage,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentTriageResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentTriage(os.Stdout, out)
}

func runContinuityIncidentTriageHistoryCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("incident triage history", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	limit := fs.Int("limit", 10, "max incident triage receipts to return in this page")
	beforeReceipt := fs.String("before-receipt", "", "optional exclusive triage receipt anchor for older-page reads")
	anchor := fs.String("anchor", "", "optional transition receipt id anchor filter")
	posture := fs.String("posture", "", "optional posture filter: triaged|needs_follow_up|deferred")
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID := strings.TrimSpace(*task)
	if taskID == "" {
		return errors.New("--task is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	postureFilter := ""
	if strings.TrimSpace(*posture) != "" {
		parsed, err := parseIncidentTriagePosture(*posture)
		if err != nil {
			return err
		}
		postureFilter = parsed
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentTriageHistoryRequest{
		TaskID:                    common.TaskID(taskID),
		Limit:                     *limit,
		BeforeReceiptID:           common.EventID(strings.TrimSpace(*beforeReceipt)),
		AnchorTransitionReceiptID: common.EventID(strings.TrimSpace(*anchor)),
		Posture:                   postureFilter,
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentTriageHistory,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentTriageHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentTriageHistory(os.Stdout, out)
}

func runContinuityIncidentFollowUpCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("incident followup", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	anchor := fs.String("anchor", "latest", "incident anchor mode: latest|receipt")
	receipt := fs.String("receipt", "", "explicit transition receipt id when --anchor receipt is used")
	triageReceipt := fs.String("triage-receipt", "", "optional explicit triage receipt id for the anchor")
	action := fs.String("action", "", "follow-up action: recorded_pending|progressed|closed|reopened")
	summary := fs.String("summary", "", "optional bounded follow-up note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID := strings.TrimSpace(*task)
	if taskID == "" {
		return errors.New("--task is required")
	}
	anchorMode := strings.ToLower(strings.TrimSpace(*anchor))
	switch anchorMode {
	case "", "latest":
		anchorMode = "latest"
	case "receipt":
	default:
		return fmt.Errorf("invalid --anchor: %q (expected latest|receipt)", *anchor)
	}
	receiptID := strings.TrimSpace(*receipt)
	if anchorMode == "receipt" && receiptID == "" {
		return errors.New("--receipt is required when --anchor receipt is used")
	}
	if anchorMode == "latest" && receiptID != "" {
		return errors.New("--receipt cannot be combined with --anchor latest")
	}
	actionKind, err := parseIncidentFollowUpAction(*action)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentFollowUpRequest{
		TaskID:                    common.TaskID(taskID),
		AnchorMode:                anchorMode,
		AnchorTransitionReceiptID: common.EventID(receiptID),
		TriageReceiptID:           common.EventID(strings.TrimSpace(*triageReceipt)),
		ActionKind:                actionKind,
		Summary:                   strings.TrimSpace(*summary),
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentFollowUp,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentFollowUpResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentFollowUp(os.Stdout, out)
}

func runContinuityIncidentFollowUpHistoryCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("incident followup history", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	limit := fs.Int("limit", 10, "max incident follow-up receipts to return in this page")
	beforeReceipt := fs.String("before-receipt", "", "optional exclusive follow-up receipt anchor for older-page reads")
	anchor := fs.String("anchor", "", "optional transition receipt id anchor filter")
	triageReceipt := fs.String("triage-receipt", "", "optional triage receipt filter")
	action := fs.String("action", "", "optional action filter: recorded_pending|progressed|closed|reopened")
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID := strings.TrimSpace(*task)
	if taskID == "" {
		return errors.New("--task is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	actionFilter := ""
	if strings.TrimSpace(*action) != "" {
		parsed, err := parseIncidentFollowUpAction(*action)
		if err != nil {
			return err
		}
		actionFilter = parsed
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentFollowUpHistoryRequest{
		TaskID:                    common.TaskID(taskID),
		Limit:                     *limit,
		BeforeReceiptID:           common.EventID(strings.TrimSpace(*beforeReceipt)),
		AnchorTransitionReceiptID: common.EventID(strings.TrimSpace(*anchor)),
		TriageReceiptID:           common.EventID(strings.TrimSpace(*triageReceipt)),
		ActionKind:                actionFilter,
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentFollowUpHistory,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentFollowUpHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentFollowUpHistory(os.Stdout, out)
}

func runContinuityIncidentClosureCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("incident closure", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	limit := fs.Int("limit", 10, "max follow-up receipts to inspect for closure intelligence")
	beforeReceipt := fs.String("before-receipt", "", "optional exclusive follow-up receipt anchor for older-page reads")
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID := strings.TrimSpace(*task)
	if taskID == "" {
		return errors.New("--task is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentClosureRequest{
		TaskID:          common.TaskID(taskID),
		Limit:           *limit,
		BeforeReceiptID: common.EventID(strings.TrimSpace(*beforeReceipt)),
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentClosure,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentClosureResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentClosure(os.Stdout, out)
}

func runContinuityIncidentTaskRiskCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("incident risk", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	limit := fs.Int("limit", 20, "max follow-up receipts to inspect for bounded task-level incident-risk intelligence")
	beforeReceipt := fs.String("before-receipt", "", "optional exclusive follow-up receipt anchor for older-page reads")
	if err := fs.Parse(args); err != nil {
		return err
	}
	taskID := strings.TrimSpace(*task)
	if taskID == "" {
		return errors.New("--task is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	payload, err := json.Marshal(ipc.TaskContinuityIncidentTaskRiskRequest{
		TaskID:          common.TaskID(taskID),
		Limit:           *limit,
		BeforeReceiptID: common.EventID(strings.TrimSpace(*beforeReceipt)),
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskContinuityIncidentRisk,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskContinuityIncidentTaskRiskResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeContinuityIncidentTaskRisk(os.Stdout, out)
}

func runShellTranscriptCommand(ctx context.Context, socketPath string, args []string) error {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "history") {
		return runShellTranscriptHistoryCommand(ctx, socketPath, args[1:])
	}
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "review") {
		return runShellTranscriptReviewCommand(ctx, socketPath, args[1:])
	}
	fs := flag.NewFlagSet("shell transcript", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	session := fs.String("session", "", "shell session id")
	limit := fs.Int("limit", 40, "max transcript chunks to return in this page")
	beforeSeq := fs.Int64("before-seq", 0, "optional exclusive sequence anchor for older-page reads")
	source := fs.String("source", "", "optional transcript source filter: worker_output|system_note|fallback_note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return errors.New("--task is required")
	}
	if strings.TrimSpace(*session) == "" {
		return errors.New("--session is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	if *beforeSeq < 0 {
		return errors.New("--before-seq must be zero or greater")
	}
	sourceFilter, err := parseTranscriptSourceFilter(*source)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ipc.TaskShellTranscriptReadRequest{
		TaskID:         common.TaskID(strings.TrimSpace(*task)),
		SessionID:      strings.TrimSpace(*session),
		Limit:          *limit,
		BeforeSequence: *beforeSeq,
		Source:         sourceFilter,
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskShellTranscriptRead,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskShellTranscriptReadResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeShellTranscriptRead(os.Stdout, out)
}

func runShellTranscriptHistoryCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("shell transcript history", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	session := fs.String("session", "", "shell session id")
	limit := fs.Int("limit", 10, "max transcript review markers to return")
	source := fs.String("source", "", "optional transcript source scope: worker_output|system_note|fallback_note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return errors.New("--task is required")
	}
	if strings.TrimSpace(*session) == "" {
		return errors.New("--session is required")
	}
	if *limit <= 0 {
		return errors.New("--limit must be greater than zero")
	}
	sourceFilter, err := parseTranscriptSourceFilter(*source)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ipc.TaskShellTranscriptHistoryRequest{
		TaskID:    common.TaskID(strings.TrimSpace(*task)),
		SessionID: strings.TrimSpace(*session),
		Source:    sourceFilter,
		Limit:     *limit,
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskShellTranscriptHistory,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskShellTranscriptHistoryResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeShellTranscriptHistory(os.Stdout, out)
}

func runShellTranscriptReviewCommand(ctx context.Context, socketPath string, args []string) error {
	fs := flag.NewFlagSet("shell transcript review", flag.ContinueOnError)
	task := fs.String("task", "", "task id")
	session := fs.String("session", "", "shell session id")
	upToSeq := fs.Int64("up-to-seq", 0, "retained transcript sequence boundary reviewed by the operator")
	source := fs.String("source", "", "optional transcript source scope: worker_output|system_note|fallback_note")
	summary := fs.String("summary", "", "optional bounded review note")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) == "" {
		return errors.New("--task is required")
	}
	if strings.TrimSpace(*session) == "" {
		return errors.New("--session is required")
	}
	if *upToSeq <= 0 {
		return errors.New("--up-to-seq must be greater than zero")
	}
	sourceFilter, err := parseTranscriptSourceFilter(*source)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(ipc.TaskShellTranscriptReviewRequest{
		TaskID:          common.TaskID(strings.TrimSpace(*task)),
		SessionID:       strings.TrimSpace(*session),
		ReviewedUpToSeq: *upToSeq,
		Source:          sourceFilter,
		Summary:         strings.TrimSpace(*summary),
	})
	if err != nil {
		return err
	}
	resp, err := ipcCall(ctx, socketPath, ipc.Request{
		RequestID: requestID(),
		Method:    ipc.MethodTaskShellTranscriptReview,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	var out ipc.TaskShellTranscriptReviewResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return err
	}
	return writeShellTranscriptReview(os.Stdout, out)
}

func writeTransitionHistory(out *os.File, response ipc.TaskTransitionHistoryResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("window limit=%d receipts=%d", response.RequestedLimit, len(response.Receipts)),
	}
	if response.RequestedTransitionKind != "" {
		lines = append(lines, fmt.Sprintf("filter transition kind %s", response.RequestedTransitionKind))
	}
	if response.RequestedHandoffID != "" {
		lines = append(lines, fmt.Sprintf("filter handoff %s", response.RequestedHandoffID))
	}
	if response.RequestedBeforeReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter before receipt %s", response.RequestedBeforeReceiptID))
	}
	if response.HasMoreOlder {
		lines = append(lines, fmt.Sprintf("older receipts available yes (use --before-receipt %s)", response.NextBeforeReceiptID))
	} else {
		lines = append(lines, "older receipts available no within bounded history window")
	}
	if latest := response.Latest; latest != nil {
		lines = append(lines, fmt.Sprintf(
			"latest %s %s -> %s (review posture %s, ack=%t)",
			nonEmpty(latest.TransitionKind, "transition"),
			nonEmpty(latest.HandoffStateBefore, "n/a"),
			nonEmpty(latest.HandoffStateAfter, "n/a"),
			nonEmpty(latest.ReviewPosture, "none"),
			latest.AcknowledgmentPresent,
		))
	}
	risk := response.RiskSummary
	if summary := strings.TrimSpace(risk.Summary); summary != "" {
		lines = append(lines, "risk "+summary)
	}
	lines = append(lines, fmt.Sprintf(
		"risk counts review-gap=%d acknowledged=%d unacknowledged=%d stale=%d source-scoped=%d into-claude=%d back-to-local=%d",
		risk.ReviewGapTransitions,
		risk.AcknowledgedReviewGapTransitions,
		risk.UnacknowledgedReviewGapTransitions,
		risk.StaleReviewPostureTransitions,
		risk.SourceScopedReviewPostureTransitions,
		risk.IntoClaudeOwnershipTransitions,
		risk.BackToLocalOwnershipTransitions,
	))
	lines = append(lines, fmt.Sprintf("risk notable %t", risk.OperationallyNotable))
	lines = append(lines, "")
	if len(response.Receipts) == 0 {
		lines = append(lines, "No continuity transition receipts in this window.")
	} else {
		for _, item := range response.Receipts {
			timestamp := ""
			if !item.CreatedAt.IsZero() {
				timestamp = item.CreatedAt.UTC().Format(time.RFC3339)
			}
			lines = append(lines, fmt.Sprintf(
				"%s %s %s->%s branch %s->%s review=%s ack=%t id=%s",
				timestamp,
				nonEmpty(item.TransitionKind, "transition"),
				nonEmpty(item.HandoffStateBefore, "n/a"),
				nonEmpty(item.HandoffStateAfter, "n/a"),
				nonEmpty(item.BranchClassBefore, "n/a"),
				nonEmpty(item.BranchClassAfter, "n/a"),
				nonEmpty(item.ReviewPosture, "none"),
				item.AcknowledgmentPresent,
				item.ReceiptID,
			))
			if note := strings.TrimSpace(item.Summary); note != "" {
				lines = append(lines, "  summary "+note)
			}
		}
	}
	lines = append(lines, "", "truth transition receipts are audit evidence only; they do not imply completion, correctness, downstream worker completion, or resumability")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentSlice(out *os.File, response ipc.TaskContinuityIncidentSliceResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("anchor %s %s %s", nonEmpty(response.AnchorMode, "LATEST_TRANSITION"), response.Anchor.ReceiptID, nonEmpty(response.Anchor.TransitionKind, "transition")),
		fmt.Sprintf("window transitions=%d runs=%d recovery=%d proofs=%d acks=%d", response.TransitionNeighborLimit, response.RunLimit, response.RecoveryLimit, response.ProofLimit, response.AckLimit),
	}
	if response.RequestedAnchorTransitionReceiptID != "" {
		lines = append(lines, fmt.Sprintf("requested anchor %s", response.RequestedAnchorTransitionReceiptID))
	}
	if !response.WindowStartAt.IsZero() && !response.WindowEndAt.IsZero() {
		lines = append(lines, fmt.Sprintf("window time %s -> %s", response.WindowStartAt.UTC().Format(time.RFC3339), response.WindowEndAt.UTC().Format(time.RFC3339)))
	}
	lines = append(lines, fmt.Sprintf(
		"window bounds older-outside=%t newer-outside=%t",
		response.HasOlderTransitionsOutsideWindow,
		response.HasNewerTransitionsOutsideWindow,
	))

	risk := response.RiskSummary
	if summary := strings.TrimSpace(risk.Summary); summary != "" {
		lines = append(lines, "risk "+summary)
	}
	lines = append(lines, fmt.Sprintf(
		"risk flags review-gap=%t ack=%t stale-or-unreviewed=%t source-scoped=%t unresolved=%t failed-runs=%d recovery=%d notable=%t",
		risk.ReviewGapPresent,
		risk.AcknowledgmentPresent,
		risk.StaleOrUnreviewedReviewPosture,
		risk.SourceScopedReviewPosture,
		risk.UnresolvedContinuityAmbiguity,
		risk.NearbyFailedOrInterruptedRuns,
		risk.NearbyRecoveryActions,
		risk.OperationallyNotable,
	))
	lines = append(lines, "")

	lines = append(lines, "transitions")
	if len(response.Transitions) == 0 {
		lines = append(lines, "  none in bounded incident window")
	} else {
		for _, item := range response.Transitions {
			marker := " "
			if item.ReceiptID == response.Anchor.ReceiptID {
				marker = "*"
			}
			lines = append(lines, fmt.Sprintf(
				"%s %s %s %s->%s review=%s ack=%t id=%s",
				marker,
				item.CreatedAt.UTC().Format(time.RFC3339),
				nonEmpty(item.TransitionKind, "transition"),
				nonEmpty(item.HandoffStateBefore, "n/a"),
				nonEmpty(item.HandoffStateAfter, "n/a"),
				nonEmpty(item.ReviewPosture, "none"),
				item.AcknowledgmentPresent,
				item.ReceiptID,
			))
		}
	}

	lines = append(lines, "", "runs")
	if len(response.Runs) == 0 {
		lines = append(lines, "  none in bounded incident window")
	} else {
		for _, item := range response.Runs {
			exit := "n/a"
			if item.ExitCode != nil {
				exit = fmt.Sprintf("%d", *item.ExitCode)
			}
			lines = append(lines, fmt.Sprintf(
				"  %s %s %s worker=%s exit=%s id=%s",
				item.OccurredAt.UTC().Format(time.RFC3339),
				item.Status,
				nonEmpty(item.Summary, "run"),
				nonEmpty(string(item.WorkerKind), "unknown"),
				exit,
				item.RunID,
			))
		}
	}

	lines = append(lines, "", "recovery")
	if len(response.RecoveryActions) == 0 {
		lines = append(lines, "  none in bounded incident window")
	} else {
		for _, item := range response.RecoveryActions {
			lines = append(lines, fmt.Sprintf(
				"  %s %s %s id=%s",
				time.UnixMilli(item.CreatedAtUnixMs).UTC().Format(time.RFC3339),
				nonEmpty(item.Kind, "RECOVERY_ACTION"),
				nonEmpty(item.Summary, "recovery action"),
				item.ActionID,
			))
		}
	}

	lines = append(lines, "", "proof")
	if len(response.ProofEvents) == 0 {
		lines = append(lines, "  none in bounded incident window")
	} else {
		for _, item := range response.ProofEvents {
			lines = append(lines, fmt.Sprintf(
				"  %s #%d %s %s",
				item.Timestamp.UTC().Format(time.RFC3339),
				item.SequenceNo,
				nonEmpty(item.Type, "PROOF_EVENT"),
				nonEmpty(item.Summary, "proof recorded"),
			))
		}
	}

	lines = append(lines, "", "transcript review")
	if response.LatestTranscriptReview != nil {
		scope := nonEmpty(response.LatestTranscriptReview.SourceFilter, "all-sources")
		lines = append(lines, fmt.Sprintf(
			"  latest review %s up_to=%d scope=%s state=%s",
			response.LatestTranscriptReview.ReviewID,
			response.LatestTranscriptReview.ReviewedUpToSequence,
			scope,
			nonEmpty(response.LatestTranscriptReview.ClosureState, "none"),
		))
	} else {
		lines = append(lines, "  latest review unavailable in bounded incident slice")
	}
	if response.LatestTranscriptReviewGapAck != nil {
		lines = append(lines, fmt.Sprintf(
			"  latest review-gap ack %s class=%s state=%s",
			response.LatestTranscriptReviewGapAck.AcknowledgmentID,
			nonEmpty(response.LatestTranscriptReviewGapAck.Class, "unknown"),
			nonEmpty(response.LatestTranscriptReviewGapAck.ReviewState, "unknown"),
		))
	} else {
		lines = append(lines, "  no review-gap acknowledgment in bounded incident slice")
	}
	if len(response.RecentTranscriptReviewGapAcks) > 1 {
		lines = append(lines, fmt.Sprintf("  recent ack history %d record(s)", len(response.RecentTranscriptReviewGapAcks)))
	}

	if note := strings.TrimSpace(response.Caveat); note != "" {
		lines = append(lines, "", "truth "+note)
	} else {
		lines = append(lines, "", "truth continuity incident slices are bounded audit correlations only; they do not imply causality, completion, correctness, resumability, or full transcript completeness")
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentTriage(out *os.File, response ipc.TaskContinuityIncidentTriageResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("anchor %s %s", nonEmpty(response.AnchorMode, "latest"), response.AnchorTransitionReceiptID),
		fmt.Sprintf("posture %s", nonEmpty(response.Posture, "TRIAGED")),
	}
	if response.Reused {
		lines = append(lines, "receipt reused existing identical triage for this anchor")
	}
	lines = append(lines, fmt.Sprintf("receipt %s", response.Receipt.ReceiptID))
	if !response.Receipt.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("recorded at %s", response.Receipt.CreatedAt.UTC().Format(time.RFC3339)))
	}
	if note := strings.TrimSpace(response.Receipt.Summary); note != "" {
		lines = append(lines, "summary "+note)
	}
	if risk := strings.TrimSpace(response.Receipt.RiskSummary.Summary); risk != "" {
		lines = append(lines, "risk snapshot "+risk)
	}
	if response.ContinuityIncidentFollowUp != nil {
		lines = append(lines, fmt.Sprintf("follow-up %s", nonEmpty(response.ContinuityIncidentFollowUp.State, "none")))
		if digest := strings.TrimSpace(response.ContinuityIncidentFollowUp.Digest); digest != "" {
			lines = append(lines, "follow-up digest "+digest)
		}
		if window := strings.TrimSpace(response.ContinuityIncidentFollowUp.WindowAdvisory); window != "" {
			lines = append(lines, "follow-up window "+window)
		}
		if advisory := strings.TrimSpace(response.ContinuityIncidentFollowUp.Advisory); advisory != "" {
			lines = append(lines, "advisory "+advisory)
		}
	}
	if len(response.RecentContinuityIncidentTriages) > 1 {
		lines = append(lines, fmt.Sprintf("recent triage history %d record(s)", len(response.RecentContinuityIncidentTriages)))
	}
	lines = append(lines, "truth incident triage receipts record operator audit posture only; they do not imply correctness, completion, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentTriageHistory(out *os.File, response ipc.TaskContinuityIncidentTriageHistoryResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("window limit=%d triage-receipts=%d", response.RequestedLimit, len(response.Receipts)),
	}
	if response.RequestedAnchorTransitionReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter anchor %s", response.RequestedAnchorTransitionReceiptID))
	}
	if response.RequestedPosture != "" {
		lines = append(lines, fmt.Sprintf("filter posture %s", response.RequestedPosture))
	}
	if response.RequestedBeforeReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter before receipt %s", response.RequestedBeforeReceiptID))
	}
	if response.HasMoreOlder {
		lines = append(lines, fmt.Sprintf("older triage receipts available yes (use --before-receipt %s)", response.NextBeforeReceiptID))
	} else {
		lines = append(lines, "older triage receipts available no within bounded history window")
	}
	if response.LatestTransitionReceiptID != "" {
		lines = append(lines, fmt.Sprintf("latest transition anchor %s", response.LatestTransitionReceiptID))
	}
	if latest := response.Latest; latest != nil {
		lines = append(lines, fmt.Sprintf(
			"latest posture=%s anchor=%s follow-up=%s receipt=%s",
			nonEmpty(latest.Posture, "TRIAGED"),
			nonEmpty(string(latest.AnchorTransitionReceiptID), "n/a"),
			nonEmpty(latest.FollowUpPosture, "NONE"),
			latest.ReceiptID,
		))
	}
	rollup := response.Rollup
	if summary := strings.TrimSpace(rollup.Summary); summary != "" {
		lines = append(lines, "rollup "+summary)
	}
	lines = append(lines, fmt.Sprintf(
		"rollup counts anchors=%d open-follow-up=%d needs=%d deferred=%d behind-latest=%d repeated=%d review-risk=%d acknowledged-review-gap=%d",
		rollup.DistinctAnchors,
		rollup.AnchorsWithOpenFollowUp,
		rollup.AnchorsNeedsFollowUp,
		rollup.AnchorsDeferred,
		rollup.AnchorsBehindLatestTransition,
		rollup.AnchorsRepeatedWithoutProgression,
		rollup.ReviewRiskReceipts,
		rollup.AcknowledgedReviewGapReceipts,
	))
	lines = append(lines, fmt.Sprintf("rollup notable %t", rollup.OperationallyNotable))
	lines = append(lines, "")
	if len(response.Receipts) == 0 {
		lines = append(lines, "No continuity incident triage receipts in this window.")
	} else {
		for _, item := range response.Receipts {
			timestamp := ""
			if !item.CreatedAt.IsZero() {
				timestamp = item.CreatedAt.UTC().Format(time.RFC3339)
			}
			behindLatest := response.LatestTransitionReceiptID != "" && item.AnchorTransitionReceiptID != "" && item.AnchorTransitionReceiptID != response.LatestTransitionReceiptID
			lines = append(lines, fmt.Sprintf(
				"%s posture=%s anchor=%s follow-up=%s behind-latest=%t review-gap=%t ack=%t id=%s",
				timestamp,
				nonEmpty(item.Posture, "TRIAGED"),
				nonEmpty(string(item.AnchorTransitionReceiptID), "n/a"),
				nonEmpty(item.FollowUpPosture, "NONE"),
				behindLatest,
				item.ReviewGapPresent,
				item.AcknowledgmentPresent,
				item.ReceiptID,
			))
			if note := strings.TrimSpace(item.Summary); note != "" {
				lines = append(lines, "  summary "+note)
			}
		}
	}
	lines = append(lines, "", "truth incident triage history is bounded audit evidence only; it does not imply closure, correctness, completion, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentFollowUp(out *os.File, response ipc.TaskContinuityIncidentFollowUpResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("anchor %s %s", nonEmpty(response.AnchorMode, "latest"), response.AnchorTransitionReceiptID),
		fmt.Sprintf("triage receipt %s", nonEmpty(string(response.TriageReceiptID), "latest-for-anchor")),
		fmt.Sprintf("action %s", nonEmpty(response.ActionKind, "RECORDED_PENDING")),
	}
	if response.Reused {
		lines = append(lines, "receipt reused existing identical follow-up action for this anchor")
	}
	lines = append(lines, fmt.Sprintf("receipt %s", response.Receipt.ReceiptID))
	if !response.Receipt.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("recorded at %s", response.Receipt.CreatedAt.UTC().Format(time.RFC3339)))
	}
	if note := strings.TrimSpace(response.Receipt.Summary); note != "" {
		lines = append(lines, "summary "+note)
	}
	if response.ContinuityIncidentFollowUpHistoryRollup != nil {
		rollup := response.ContinuityIncidentFollowUpHistoryRollup
		if summary := strings.TrimSpace(rollup.Summary); summary != "" {
			lines = append(lines, "rollup "+summary)
		}
		lines = append(lines, fmt.Sprintf(
			"rollup counts anchors=%d open=%d closed=%d reopened=%d triaged-without-followup=%d repeated=%d",
			rollup.DistinctAnchors,
			rollup.AnchorsWithOpenFollowUp,
			rollup.AnchorsClosed,
			rollup.AnchorsReopened,
			rollup.AnchorsTriagedWithoutFollowUp,
			rollup.AnchorsRepeatedWithoutProgression,
		))
	}
	if response.ContinuityIncidentFollowUp != nil {
		lines = append(lines, fmt.Sprintf("follow-up state %s", nonEmpty(response.ContinuityIncidentFollowUp.State, "none")))
		if digest := strings.TrimSpace(response.ContinuityIncidentFollowUp.Digest); digest != "" {
			lines = append(lines, "follow-up digest "+digest)
		}
		if window := strings.TrimSpace(response.ContinuityIncidentFollowUp.WindowAdvisory); window != "" {
			lines = append(lines, "follow-up window "+window)
		}
		if advisory := strings.TrimSpace(response.ContinuityIncidentFollowUp.Advisory); advisory != "" {
			lines = append(lines, "advisory "+advisory)
		}
	}
	lines = append(lines, "truth incident follow-up receipts are bounded audit evidence only; they do not imply closure correctness, task completion, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentFollowUpHistory(out *os.File, response ipc.TaskContinuityIncidentFollowUpHistoryResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("window limit=%d follow-up-receipts=%d", response.RequestedLimit, len(response.Receipts)),
	}
	if response.RequestedAnchorTransitionReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter anchor %s", response.RequestedAnchorTransitionReceiptID))
	}
	if response.RequestedTriageReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter triage receipt %s", response.RequestedTriageReceiptID))
	}
	if response.RequestedActionKind != "" {
		lines = append(lines, fmt.Sprintf("filter action %s", response.RequestedActionKind))
	}
	if response.RequestedBeforeReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter before receipt %s", response.RequestedBeforeReceiptID))
	}
	if response.HasMoreOlder {
		lines = append(lines, fmt.Sprintf("older follow-up receipts available yes (use --before-receipt %s)", response.NextBeforeReceiptID))
	} else {
		lines = append(lines, "older follow-up receipts available no within bounded history window")
	}
	if response.LatestTransitionReceiptID != "" {
		lines = append(lines, fmt.Sprintf("latest transition anchor %s", response.LatestTransitionReceiptID))
	}
	if latest := response.Latest; latest != nil {
		lines = append(lines, fmt.Sprintf(
			"latest action=%s anchor=%s triage=%s receipt=%s",
			nonEmpty(latest.ActionKind, "RECORDED_PENDING"),
			nonEmpty(string(latest.AnchorTransitionReceiptID), "n/a"),
			nonEmpty(string(latest.TriageReceiptID), "n/a"),
			latest.ReceiptID,
		))
	}
	rollup := response.Rollup
	if summary := strings.TrimSpace(rollup.Summary); summary != "" {
		lines = append(lines, "rollup "+summary)
	}
	lines = append(lines, fmt.Sprintf(
		"rollup counts anchors=%d open=%d closed=%d reopened=%d pending=%d progressed=%d repeated=%d triaged-without-followup=%d behind-latest=%d",
		rollup.DistinctAnchors,
		rollup.AnchorsWithOpenFollowUp,
		rollup.AnchorsClosed,
		rollup.AnchorsReopened,
		rollup.ReceiptsRecordedPending,
		rollup.ReceiptsProgressed,
		rollup.AnchorsRepeatedWithoutProgression,
		rollup.AnchorsTriagedWithoutFollowUp,
		rollup.OpenAnchorsBehindLatestTransition,
	))
	lines = append(lines, fmt.Sprintf("rollup notable %t", rollup.OperationallyNotable))
	lines = append(lines, "")
	if len(response.Receipts) == 0 {
		lines = append(lines, "No continuity incident follow-up receipts in this window.")
	} else {
		for _, item := range response.Receipts {
			timestamp := ""
			if !item.CreatedAt.IsZero() {
				timestamp = item.CreatedAt.UTC().Format(time.RFC3339)
			}
			behindLatest := response.LatestTransitionReceiptID != "" && item.AnchorTransitionReceiptID != "" && item.AnchorTransitionReceiptID != response.LatestTransitionReceiptID
			lines = append(lines, fmt.Sprintf(
				"%s action=%s anchor=%s triage=%s behind-latest=%t review-gap=%t ack=%t id=%s",
				timestamp,
				nonEmpty(item.ActionKind, "RECORDED_PENDING"),
				nonEmpty(string(item.AnchorTransitionReceiptID), "n/a"),
				nonEmpty(string(item.TriageReceiptID), "n/a"),
				behindLatest,
				item.ReviewGapPresent,
				item.AcknowledgmentPresent,
				item.ReceiptID,
			))
			if note := strings.TrimSpace(item.Summary); note != "" {
				lines = append(lines, "  summary "+note)
			}
		}
	}
	lines = append(lines, "", "truth incident follow-up history is bounded audit evidence only; it does not imply task completion, closure correctness, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentClosure(out *os.File, response ipc.TaskContinuityIncidentClosureResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("window limit=%d follow-up-receipts=%d", response.RequestedLimit, len(response.Receipts)),
	}
	if response.RequestedBeforeReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter before receipt %s", response.RequestedBeforeReceiptID))
	}
	if response.HasMoreOlder {
		lines = append(lines, fmt.Sprintf("older follow-up receipts available yes (use --before-receipt %s)", response.NextBeforeReceiptID))
	} else {
		lines = append(lines, "older follow-up receipts available no within bounded history window")
	}
	if response.LatestTransitionReceiptID != "" {
		lines = append(lines, fmt.Sprintf("latest transition anchor %s", response.LatestTransitionReceiptID))
	}
	if response.Latest != nil {
		lines = append(lines, fmt.Sprintf(
			"latest action=%s anchor=%s triage=%s receipt=%s",
			nonEmpty(response.Latest.ActionKind, "RECORDED_PENDING"),
			nonEmpty(string(response.Latest.AnchorTransitionReceiptID), "n/a"),
			nonEmpty(string(response.Latest.TriageReceiptID), "n/a"),
			response.Latest.ReceiptID,
		))
	}
	if rollupSummary := strings.TrimSpace(response.Rollup.Summary); rollupSummary != "" {
		lines = append(lines, "rollup "+rollupSummary)
	}
	lines = append(lines, fmt.Sprintf(
		"rollup counts anchors=%d open=%d closed=%d reopened=%d triaged-without-followup=%d repeated=%d behind-latest=%d",
		response.Rollup.DistinctAnchors,
		response.Rollup.AnchorsWithOpenFollowUp,
		response.Rollup.AnchorsClosed,
		response.Rollup.AnchorsReopened,
		response.Rollup.AnchorsTriagedWithoutFollowUp,
		response.Rollup.AnchorsRepeatedWithoutProgression,
		response.Rollup.OpenAnchorsBehindLatestTransition,
	))
	if response.FollowUp != nil {
		lines = append(lines, fmt.Sprintf("follow-up state %s", nonEmpty(response.FollowUp.State, "none")))
		if digest := strings.TrimSpace(response.FollowUp.Digest); digest != "" {
			lines = append(lines, "follow-up digest "+digest)
		}
		if window := strings.TrimSpace(response.FollowUp.WindowAdvisory); window != "" {
			lines = append(lines, "follow-up window "+window)
		}
		if advisory := strings.TrimSpace(response.FollowUp.Advisory); advisory != "" {
			lines = append(lines, "advisory "+advisory)
		}
	}
	if response.Closure != nil {
		lines = append(lines, fmt.Sprintf("closure class %s", nonEmpty(response.Closure.Class, "NONE")))
		if digest := strings.TrimSpace(response.Closure.Digest); digest != "" {
			lines = append(lines, "closure digest "+digest)
		}
		if window := strings.TrimSpace(response.Closure.WindowAdvisory); window != "" {
			lines = append(lines, "closure window "+window)
		}
		if detail := strings.TrimSpace(response.Closure.Detail); detail != "" {
			lines = append(lines, "closure detail "+detail)
		}
		lines = append(lines, fmt.Sprintf(
			"closure signals unresolved=%t weak=%t reopened-after-close=%t reopen-loop=%t stagnant=%t triaged-without-followup=%t",
			response.Closure.OperationallyUnresolved,
			response.Closure.ClosureAppearsWeak,
			response.Closure.ReopenedAfterClosure,
			response.Closure.RepeatedReopenLoop,
			response.Closure.StagnantProgression,
			response.Closure.TriagedWithoutFollowUp,
		))
		if len(response.Closure.RecentAnchors) > 0 {
			lines = append(lines, "closure anchors")
			for _, anchor := range response.Closure.RecentAnchors {
				lines = append(lines, fmt.Sprintf(
					"  %s class=%s action=%s follow-up=%s",
					nonEmpty(string(anchor.AnchorTransitionReceiptID), "n/a"),
					nonEmpty(anchor.Class, "NONE"),
					nonEmpty(anchor.LatestFollowUpActionKind, "none"),
					nonEmpty(string(anchor.LatestFollowUpReceiptID), "n/a"),
				))
				if note := strings.TrimSpace(anchor.Explanation); note != "" {
					lines = append(lines, "    "+note)
				}
			}
		}
	}
	lines = append(lines, "", "truth incident closure intelligence is bounded advisory evidence only; it does not imply causality, root-cause resolution, correctness, completion, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeContinuityIncidentTaskRisk(out *os.File, response ipc.TaskContinuityIncidentTaskRiskResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("window limit=%d", response.RequestedLimit),
	}
	if response.RequestedBeforeReceiptID != "" {
		lines = append(lines, fmt.Sprintf("filter before receipt %s", response.RequestedBeforeReceiptID))
	}
	if response.HasMoreOlder {
		lines = append(lines, fmt.Sprintf("older evidence available yes (use --before-receipt %s)", response.NextBeforeReceiptID))
	} else {
		lines = append(lines, "older evidence available no within bounded history window")
	}
	if response.Summary != nil {
		lines = append(lines, fmt.Sprintf("task risk class %s", nonEmpty(response.Summary.Class, "NONE")))
		if digest := strings.TrimSpace(response.Summary.Digest); digest != "" {
			lines = append(lines, "task risk digest "+digest)
		}
		if window := strings.TrimSpace(response.Summary.WindowAdvisory); window != "" {
			lines = append(lines, "task risk window "+window)
		}
		if detail := strings.TrimSpace(response.Summary.Detail); detail != "" {
			lines = append(lines, "task risk detail "+detail)
		}
		lines = append(lines, fmt.Sprintf(
			"task risk signals recurring-weak=%t recurring-unresolved=%t recurring-stagnant=%t recurring-triaged-without-follow-up=%t anchors=%d open=%d reopened=%d triaged-without-follow-up=%d",
			response.Summary.RecurringWeakClosure,
			response.Summary.RecurringUnresolved,
			response.Summary.RecurringStagnantFollowUp,
			response.Summary.RecurringTriagedWithoutFollowUp,
			response.Summary.DistinctAnchors,
			response.Summary.AnchorsWithOpenFollowUp,
			response.Summary.AnchorsReopened,
			response.Summary.AnchorsTriagedWithoutFollowUp,
		))
		if len(response.Summary.RecentAnchorClasses) > 0 {
			classes := make([]string, 0, len(response.Summary.RecentAnchorClasses))
			for _, item := range response.Summary.RecentAnchorClasses {
				classes = append(classes, strings.ToLower(strings.ReplaceAll(strings.TrimSpace(item), "_", " ")))
			}
			lines = append(lines, "recent anchor classes "+strings.Join(classes, ", "))
		}
	}
	if response.Closure != nil {
		lines = append(lines, fmt.Sprintf("latest anchor closure class %s", nonEmpty(response.Closure.Class, "NONE")))
		if digest := strings.TrimSpace(response.Closure.Digest); digest != "" {
			lines = append(lines, "latest anchor closure digest "+digest)
		}
	}
	lines = append(lines, "", "truth task-level incident risk is bounded advisory evidence only; it does not imply causality, root-cause resolution, correctness, completion, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeShellTranscriptRead(out *os.File, response ipc.TaskShellTranscriptReadResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("session %s", response.SessionID),
		fmt.Sprintf("state %s", nonEmpty(response.TranscriptState, "none")),
		fmt.Sprintf("retention retained=%d dropped=%d limit=%d", response.RetainedChunkCount, response.DroppedChunkCount, response.RetentionLimit),
	}
	if response.OldestRetainedSequence > 0 && response.NewestRetainedSequence > 0 {
		lines = append(lines, fmt.Sprintf("retained sequence window %d-%d", response.OldestRetainedSequence, response.NewestRetainedSequence))
	}
	if response.PageChunkCount > 0 {
		lines = append(lines, fmt.Sprintf("page sequence window %d-%d (%d chunks)", response.PageOldestSequence, response.PageNewestSequence, response.PageChunkCount))
	} else {
		lines = append(lines, "page sequence window n/a (no chunks)")
	}
	if response.HasMoreOlder {
		lines = append(lines, fmt.Sprintf("older evidence available yes (use --before-seq %d)", response.NextBeforeSequence))
	} else {
		lines = append(lines, "older evidence available no within retained window")
	}
	if response.RequestedSource != "" {
		lines = append(lines, fmt.Sprintf("source filter %s", response.RequestedSource))
	}
	if len(response.SourceSummary) > 0 {
		pairs := make([]string, 0, len(response.SourceSummary))
		for _, source := range response.SourceSummary {
			pairs = append(pairs, fmt.Sprintf("%s=%d", source.Source, source.Chunks))
		}
		lines = append(lines, "sources "+strings.Join(pairs, ", "))
	}
	if response.Partial {
		lines = append(lines, "truth bounded transcript evidence is partial; older chunks were dropped")
	} else if response.RetainedChunkCount > 0 {
		lines = append(lines, "truth bounded transcript evidence is available within the current retention window")
	} else {
		lines = append(lines, "truth no durable transcript evidence is retained for this session")
	}
	if response.TranscriptOnly {
		lines = append(lines, "context transcript-only evidence; this does not imply live worker resumability")
	}
	if response.LatestReview != nil {
		scope := nonEmpty(response.LatestReview.SourceFilter, "all-sources")
		lines = append(lines, fmt.Sprintf("latest review id=%s up_to_seq=%d scope=%s", response.LatestReview.ReviewID, response.LatestReview.ReviewedUpToSequence, scope))
		if !response.LatestReview.CreatedAt.IsZero() {
			lines = append(lines, fmt.Sprintf("latest review at %s", response.LatestReview.CreatedAt.UTC().Format(time.RFC3339)))
		}
		if response.HasUnreadNewerEvidence {
			lines = append(lines, fmt.Sprintf("review boundary behind latest retained evidence (+%d seq)", max(1, response.LatestReview.NewerRetainedCount)))
		} else {
			lines = append(lines, "review boundary reaches latest retained evidence within bounded window")
		}
		if response.PageFullyReviewed {
			lines = append(lines, "page review coverage fully reviewed")
		} else if response.PageCrossesReview {
			lines = append(lines, "page review coverage crosses beyond reviewed boundary")
		} else if response.PageHasUnreviewed {
			lines = append(lines, "page review coverage includes unreviewed evidence")
		}
	}
	if state := strings.TrimSpace(response.Closure.State); state != "" && state != "none" {
		lines = append(lines, fmt.Sprintf("review closure %s", state))
	}
	if response.Closure.HasUnreadNewerEvidence {
		lines = append(lines, fmt.Sprintf(
			"unreviewed retained range %d-%d (+%d seq)",
			response.Closure.OldestUnreviewedSequence,
			response.Closure.NewestRetainedSequence,
			max(1, response.Closure.UnreviewedRetainedCount),
		))
	}
	lines = append(lines, "")
	if len(response.Chunks) == 0 {
		lines = append(lines, "No transcript chunks in this page.")
	} else {
		for _, chunk := range response.Chunks {
			timestamp := ""
			if !chunk.CreatedAt.IsZero() {
				timestamp = chunk.CreatedAt.UTC().Format(time.RFC3339)
			}
			body := strings.TrimSpace(chunk.Content)
			lines = append(lines, fmt.Sprintf("[%d] %s %s | %s", chunk.SequenceNo, timestamp, nonEmpty(chunk.Source, "worker_output"), body))
		}
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeShellTranscriptReview(out *os.File, response ipc.TaskShellTranscriptReviewResponse) error {
	scope := nonEmpty(response.LatestReview.SourceFilter, "all-sources")
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("session %s", response.SessionID),
		fmt.Sprintf("review %s", response.LatestReview.ReviewID),
		fmt.Sprintf("reviewed up to sequence %d (%s)", response.LatestReview.ReviewedUpToSequence, scope),
		fmt.Sprintf("retention retained=%d dropped=%d limit=%d", response.RetainedChunkCount, response.DroppedChunkCount, response.RetentionLimit),
		fmt.Sprintf("retained sequence window %d-%d", response.OldestRetainedSequence, response.NewestRetainedSequence),
	}
	if !response.LatestReview.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("review created %s", response.LatestReview.CreatedAt.UTC().Format(time.RFC3339)))
	}
	if note := strings.TrimSpace(response.LatestReview.Summary); note != "" {
		lines = append(lines, "review note "+note)
	}
	if response.HasUnreadNewerEvidence {
		lines = append(lines, fmt.Sprintf("newer retained evidence exists beyond review boundary (+%d seq)", max(1, response.LatestReview.NewerRetainedCount)))
	} else {
		lines = append(lines, "no newer retained evidence exists beyond the current review boundary")
	}
	if state := strings.TrimSpace(response.Closure.State); state != "" && state != "none" {
		lines = append(lines, "review closure "+state)
	}
	if response.Closure.HasUnreadNewerEvidence {
		lines = append(lines, fmt.Sprintf(
			"unreviewed retained range %d-%d (+%d seq)",
			response.Closure.OldestUnreviewedSequence,
			response.Closure.NewestRetainedSequence,
			max(1, response.Closure.UnreviewedRetainedCount),
		))
	}
	lines = append(lines, "truth review markers attest bounded transcript evidence only; they do not imply task completion, correctness, or worker resumability")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeShellTranscriptHistory(out *os.File, response ipc.TaskShellTranscriptHistoryResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("session %s", response.SessionID),
		fmt.Sprintf("state %s", nonEmpty(response.TranscriptState, "none")),
		fmt.Sprintf("retention retained=%d dropped=%d limit=%d", response.RetainedChunkCount, response.DroppedChunkCount, response.RetentionLimit),
	}
	if response.OldestRetainedSequence > 0 && response.NewestRetainedSequence > 0 {
		lines = append(lines, fmt.Sprintf("retained sequence window %d-%d", response.OldestRetainedSequence, response.NewestRetainedSequence))
	}
	if response.RequestedSource != "" {
		lines = append(lines, fmt.Sprintf("source scope %s", response.RequestedSource))
	}
	if state := strings.TrimSpace(response.Closure.State); state != "" {
		lines = append(lines, fmt.Sprintf("review closure %s", state))
	}
	if response.Closure.HasReview {
		scope := nonEmpty(response.Closure.Scope, "all-sources")
		lines = append(lines, fmt.Sprintf("latest review boundary seq %d (%s)", response.Closure.ReviewedUpToSequence, scope))
	}
	if response.Closure.HasUnreadNewerEvidence {
		lines = append(lines, fmt.Sprintf(
			"newer retained evidence exists %d-%d (+%d seq)",
			response.Closure.OldestUnreviewedSequence,
			response.Closure.NewestRetainedSequence,
			max(1, response.Closure.UnreviewedRetainedCount),
		))
	} else if response.Closure.HasReview {
		lines = append(lines, "no newer retained evidence exists beyond the latest review boundary")
	} else {
		lines = append(lines, "no transcript review markers recorded for this scope")
	}
	if response.Partial {
		lines = append(lines, "truth bounded transcript evidence is partial; older chunks were dropped")
	}
	if response.TranscriptOnly {
		lines = append(lines, "context transcript-only evidence; this does not imply worker resumability")
	}
	lines = append(lines, "")
	if len(response.Reviews) == 0 {
		lines = append(lines, "No transcript review markers in this window.")
	} else {
		for _, review := range response.Reviews {
			scope := nonEmpty(review.SourceFilter, "all-sources")
			ts := ""
			if !review.CreatedAt.IsZero() {
				ts = review.CreatedAt.UTC().Format(time.RFC3339)
			}
			lines = append(lines, fmt.Sprintf(
				"%s seq<=%d scope=%s stale=%t newer=+%d id=%s",
				ts,
				review.ReviewedUpToSequence,
				scope,
				review.StaleBehindLatest,
				max(0, review.NewerRetainedCount),
				review.ReviewID,
			))
			if note := strings.TrimSpace(review.Summary); note != "" {
				lines = append(lines, "  note "+note)
			}
		}
	}
	lines = append(lines, "", "truth review history records bounded evidence review markers only; it does not imply task completion, correctness, or resumability")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeOperatorReviewGapAcknowledgment(out *os.File, response ipc.TaskOperatorAcknowledgeReviewGapResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("session %s", response.SessionID),
		fmt.Sprintf("acknowledgment %s", response.Acknowledgment.AcknowledgmentID),
		fmt.Sprintf("class %s", nonEmpty(response.ReviewGapClass, "unknown")),
		fmt.Sprintf("review posture %s", nonEmpty(response.ReviewGapState, "unknown")),
	}
	if response.ReviewScope != "" {
		lines = append(lines, fmt.Sprintf("scope %s", response.ReviewScope))
	}
	if response.ReviewedUpToSequence > 0 {
		lines = append(lines, fmt.Sprintf("review boundary up to seq %d", response.ReviewedUpToSequence))
	}
	if response.OldestUnreviewedSeq > 0 && response.NewestRetainedSequence >= response.OldestUnreviewedSeq {
		lines = append(lines, fmt.Sprintf(
			"retained unreviewed range %d-%d (+%d seq)",
			response.OldestUnreviewedSeq,
			response.NewestRetainedSequence,
			max(1, response.UnreviewedRetained),
		))
	}
	if !response.Acknowledgment.CreatedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("recorded at %s", response.Acknowledgment.CreatedAt.UTC().Format(time.RFC3339)))
	}
	if action := strings.TrimSpace(response.Acknowledgment.ActionContext); action != "" {
		lines = append(lines, fmt.Sprintf("action context %s", action))
	}
	if note := strings.TrimSpace(response.Acknowledgment.Summary); note != "" {
		lines = append(lines, "note "+note)
	}
	if advisory := strings.TrimSpace(response.Advisory); advisory != "" {
		lines = append(lines, "advisory "+advisory)
	}
	if response.Acknowledgment.StaleBehindCurrent {
		lines = append(lines, fmt.Sprintf("newer retained evidence exists since this acknowledgment (+%d seq)", max(1, response.Acknowledgment.NewerRetainedCount)))
	}
	lines = append(lines, "truth this acknowledgment records operator awareness of transcript review gaps only; it does not imply completion, correctness, resumability, or full transcript coverage")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskStatusHuman(out *os.File, response ipc.TaskStatusResponse) error {
	var benchmarkView *benchmark.Run
	if strings.TrimSpace(string(response.CurrentBenchmarkID)) != "" {
		benchmarkView = &benchmark.Run{
			BenchmarkID:                   response.CurrentBenchmarkID,
			TaskID:                        response.TaskID,
			Source:                        response.CurrentBenchmarkSource,
			RawPromptTokenEstimate:        response.CurrentBenchmarkRawPromptTokens,
			DispatchPromptTokenEstimate:   response.CurrentBenchmarkDispatchPromptTokens,
			StructuredPromptTokenEstimate: response.CurrentBenchmarkStructuredPromptTokens,
			SelectedContextTokenEstimate:  response.CurrentBenchmarkSelectedContextTokens,
			EstimatedTokenSavings:         response.CurrentBenchmarkEstimatedTokenSavings,
			FilesScanned:                  response.CurrentBenchmarkFilesScanned,
			RankedTargetCount:             response.CurrentBenchmarkRankedTargetCount,
			CandidateRecallAt3:            response.CurrentBenchmarkCandidateRecallAt3,
			StructuredCheaper:             response.CurrentBenchmarkStructuredCheaper,
			DefaultSerializer:             response.CurrentBenchmarkDefaultSerializer,
			ConfidenceValue:               response.CurrentBenchmarkConfidenceValue,
			ConfidenceLevel:               response.CurrentBenchmarkConfidenceLevel,
			Summary:                       response.CurrentBenchmarkSummary,
		}
	}
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf("phase %s | status %s", nonEmpty(string(response.Phase), "UNKNOWN"), nonEmpty(response.Status, "UNKNOWN")),
		fmt.Sprintf("required-next %s", nonEmpty(response.RequiredNextOperatorAction, "none")),
	}
	lines = append(lines, providerHumanLines(recommendationFromCompiledBrief(response.CompiledBrief, benchmarkView, "", string(response.LatestRunStatus), "", ""))...)
	if response.CurrentContextPackID != "" {
		lines = append(lines, fmt.Sprintf(
			"context-pack %s | mode %s | files %d",
			response.CurrentContextPackID,
			nonEmpty(response.CurrentContextPackMode, "unknown"),
			response.CurrentContextPackFileCount,
		))
		if response.CurrentContextPackHash != "" {
			lines = append(lines, "context-pack hash "+response.CurrentContextPackHash)
		}
	}
	if response.LatestPolicyDecisionID != "" {
		lines = append(lines, fmt.Sprintf(
			"policy %s | status %s | risk %s",
			response.LatestPolicyDecisionID,
			nonEmpty(response.LatestPolicyDecisionStatus, "unknown"),
			nonEmpty(response.LatestPolicyDecisionRiskLevel, "unknown"),
		))
		if response.LatestPolicyDecisionReason != "" {
			lines = append(lines, "policy reason "+response.LatestPolicyDecisionReason)
		}
	}
	if response.LatestRunID != "" {
		lines = append(lines, fmt.Sprintf(
			"run %s | status %s | changed %d",
			response.LatestRunID,
			nonEmpty(string(response.LatestRunStatus), "unknown"),
			len(response.LatestRunChangedFiles),
		))
		if response.LatestRunChangedFilesSemantics != "" {
			lines = append(lines, "run changes "+response.LatestRunChangedFilesSemantics)
		}
		if response.LatestRunRepoDiffSummary != "" {
			lines = append(lines, "repo diff "+response.LatestRunRepoDiffSummary)
		}
		if response.LatestRunWorktreeSummary != "" {
			lines = append(lines, "worktree "+response.LatestRunWorktreeSummary)
		}
		if len(response.LatestRunValidationSignals) > 0 {
			lines = append(lines, "validation "+strings.Join(response.LatestRunValidationSignals, " | "))
		}
		if response.LatestRunOutputArtifactRef != "" {
			lines = append(lines, "artifact "+response.LatestRunOutputArtifactRef)
		}
	}
	if response.OperatorDecision != nil && strings.TrimSpace(response.OperatorDecision.Headline) != "" {
		lines = append(lines, "decision "+response.OperatorDecision.Headline)
	}
	lines = append(lines, taskMemoryHumanLines(
		string(response.CurrentTaskMemoryID),
		response.CurrentTaskMemorySource,
		response.CurrentTaskMemorySummary,
		response.CurrentTaskMemoryFullHistoryTokens,
		response.CurrentTaskMemoryResumePromptTokens,
		response.CurrentTaskMemoryCompactionRatio,
	)...)
	lines = append(lines, benchmarkHumanLines(
		string(response.CurrentBenchmarkID),
		response.CurrentBenchmarkSource,
		response.CurrentBenchmarkSummary,
		response.CurrentBenchmarkRawPromptTokens,
		response.CurrentBenchmarkDispatchPromptTokens,
		response.CurrentBenchmarkStructuredPromptTokens,
		response.CurrentBenchmarkSelectedContextTokens,
		response.CurrentBenchmarkEstimatedTokenSavings,
		response.CurrentBenchmarkFilesScanned,
		response.CurrentBenchmarkRankedTargetCount,
		response.CurrentBenchmarkCandidateRecallAt3,
		response.CurrentBenchmarkDefaultSerializer,
		response.CurrentBenchmarkStructuredCheaper,
		response.CurrentBenchmarkConfidenceLevel,
		response.CurrentBenchmarkConfidenceValue,
	)...)
	lines = append(lines, intentHumanLines(response.CompiledIntent, false)...)
	lines = append(lines, briefHumanLines(response.CompiledBrief, false)...)
	lines = append(lines, followUpHumanLines(response.ContinuityIncidentFollowUp, response.ContinuityIncidentFollowUpHistoryRollup, false)...)
	lines = append(lines, taskRiskHumanLines(response.ContinuityIncidentTaskRisk, false)...)
	lines = append(lines, "truth human mode summarizes bounded audit advisories only; it does not imply completion, correctness, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskInspectHuman(out *os.File, response ipc.TaskInspectResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		fmt.Sprintf(
			"repo %s | branch %s | dirty=%t",
			nonEmpty(response.RepoAnchor.RepoRoot, "n/a"),
			nonEmpty(response.RepoAnchor.Branch, "n/a"),
			response.RepoAnchor.WorkingTreeDirty,
		),
	}
	lines = append(lines, providerHumanLines(recommendationFromCompiledBrief(response.CompiledBrief, response.Benchmark, "", "", "", ""))...)
	if response.OperatorDecision != nil && strings.TrimSpace(response.OperatorDecision.Headline) != "" {
		lines = append(lines, "decision "+response.OperatorDecision.Headline)
	}
	if response.OperatorExecutionPlan != nil && response.OperatorExecutionPlan.PrimaryStep != nil {
		lines = append(lines, fmt.Sprintf(
			"plan %s (%s)",
			nonEmpty(response.OperatorExecutionPlan.PrimaryStep.Action, "none"),
			nonEmpty(response.OperatorExecutionPlan.PrimaryStep.Status, "unknown"),
		))
	}
	if response.TaskMemory != nil {
		lines = append(lines, taskMemoryHumanLines(
			string(response.TaskMemory.MemoryID),
			response.TaskMemory.Source,
			response.TaskMemory.Summary,
			response.TaskMemory.FullHistoryTokenEstimate,
			response.TaskMemory.ResumePromptTokenEstimate,
			response.TaskMemory.MemoryCompactionRatio,
		)...)
	}
	if response.Benchmark != nil {
		lines = append(lines, benchmarkHumanLines(
			string(response.Benchmark.BenchmarkID),
			response.Benchmark.Source,
			response.Benchmark.Summary,
			response.Benchmark.RawPromptTokenEstimate,
			response.Benchmark.DispatchPromptTokenEstimate,
			response.Benchmark.StructuredPromptTokenEstimate,
			response.Benchmark.SelectedContextTokenEstimate,
			response.Benchmark.EstimatedTokenSavings,
			response.Benchmark.FilesScanned,
			response.Benchmark.RankedTargetCount,
			response.Benchmark.CandidateRecallAt3,
			response.Benchmark.DefaultSerializer,
			response.Benchmark.StructuredCheaper,
			response.Benchmark.ConfidenceLevel,
			response.Benchmark.ConfidenceValue,
		)...)
	}
	lines = append(lines, intentHumanLines(response.CompiledIntent, true)...)
	lines = append(lines, briefHumanLines(response.CompiledBrief, true)...)
	lines = append(lines, followUpHumanLines(response.ContinuityIncidentFollowUp, response.ContinuityIncidentFollowUpHistoryRollup, true)...)
	lines = append(lines, taskRiskHumanLines(response.ContinuityIncidentTaskRisk, true)...)
	if response.LatestContinuityIncidentFollowUpReceipt != nil {
		latest := response.LatestContinuityIncidentFollowUpReceipt
		lines = append(lines, fmt.Sprintf(
			"latest follow-up receipt %s action=%s anchor=%s",
			latest.ReceiptID,
			nonEmpty(latest.ActionKind, "unknown"),
			nonEmpty(string(latest.AnchorTransitionReceiptID), "n/a"),
		))
	}
	lines = append(lines, "truth human mode summarizes bounded audit advisories only; it does not imply completion, correctness, resumability, transcript completeness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskIntent(out *os.File, response ipc.TaskIntentResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
	}
	if response.CurrentIntentID != "" {
		lines = append(lines, fmt.Sprintf("current intent %s", response.CurrentIntentID))
	}
	if response.Bounded {
		lines = append(lines, "bounded read yes")
	}
	if response.CompiledIntent == nil {
		lines = append(lines, "No compiled intent is available yet. Send a task message to compile bounded intent evidence.")
		lines = append(lines, "truth compiled intent is bounded advisory interpretation only; it does not imply correctness, completion, or execution success")
		_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
		return err
	}
	lines = append(lines, fmt.Sprintf("class %s", nonEmpty(response.CompiledIntent.Class, "UNKNOWN")))
	lines = append(lines, fmt.Sprintf("posture %s", nonEmpty(response.CompiledIntent.Posture, "UNKNOWN")))
	lines = append(lines, fmt.Sprintf("readiness %s", nonEmpty(response.CompiledIntent.ExecutionReadiness, "UNKNOWN")))
	if objective := strings.TrimSpace(response.CompiledIntent.Objective); objective != "" {
		lines = append(lines, "objective "+objective)
	}
	if outcome := strings.TrimSpace(response.CompiledIntent.RequestedOutcome); outcome != "" {
		lines = append(lines, "requested outcome "+outcome)
	}
	if action := strings.TrimSpace(response.CompiledIntent.NormalizedAction); action != "" {
		lines = append(lines, "normalized action "+action)
	}
	if scope := strings.TrimSpace(response.CompiledIntent.ScopeSummary); scope != "" {
		lines = append(lines, "scope "+scope)
	}
	if len(response.CompiledIntent.ExplicitConstraints) > 0 {
		lines = append(lines, "constraints "+strings.Join(response.CompiledIntent.ExplicitConstraints, " | "))
	}
	if len(response.CompiledIntent.DoneCriteria) > 0 {
		lines = append(lines, "done criteria "+strings.Join(response.CompiledIntent.DoneCriteria, " | "))
	}
	if len(response.CompiledIntent.AmbiguityFlags) > 0 {
		lines = append(lines, "ambiguity "+strings.Join(response.CompiledIntent.AmbiguityFlags, ", "))
	}
	if len(response.CompiledIntent.ClarificationQuestions) > 0 {
		lines = append(lines, "clarification "+strings.Join(response.CompiledIntent.ClarificationQuestions, " | "))
	}
	lines = append(lines, fmt.Sprintf("requires clarification %t", response.CompiledIntent.RequiresClarification))
	if response.CompiledIntent.BoundedEvidenceMessages > 0 {
		lines = append(lines, fmt.Sprintf("bounded evidence messages %d", response.CompiledIntent.BoundedEvidenceMessages))
	}
	if reason := strings.TrimSpace(response.CompiledIntent.ReadinessReason); reason != "" {
		lines = append(lines, "readiness reason "+reason)
	}
	if digest := strings.TrimSpace(response.CompiledIntent.Digest); digest != "" {
		lines = append(lines, "digest "+digest)
	}
	if advisory := strings.TrimSpace(response.CompiledIntent.Advisory); advisory != "" {
		lines = append(lines, "advisory "+advisory)
	}
	lines = append(lines, "truth compiled intent is bounded advisory interpretation only; it does not imply correctness, completion, or execution success")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskBriefHuman(out *os.File, response ipc.TaskBriefResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
	}
	if response.CurrentBriefID != "" {
		lines = append(lines, fmt.Sprintf("current brief %s", response.CurrentBriefID))
	}
	if response.Bounded {
		lines = append(lines, "bounded read yes")
	}
	lines = append(lines, providerHumanLines(recommendationFromCompiledBrief(response.CompiledBrief, nil, "", "", "", ""))...)
	lines = append(lines, briefHumanLines(response.CompiledBrief, true)...)
	if response.Brief != nil && strings.TrimSpace(response.Brief.WorkerFraming) != "" {
		lines = append(lines, "worker framing "+response.Brief.WorkerFraming)
	}
	if response.CompiledBrief == nil && response.Brief == nil {
		lines = append(lines, "No generated brief is available yet. Send a task message to compile intent and generate a bounded brief.")
	}
	lines = append(lines, "truth generated briefs are bounded advisory execution framing only; they do not imply completion, correctness, or downstream worker completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

type taskPlanView struct {
	TaskID         common.TaskID                 `json:"task_id"`
	Phase          string                        `json:"phase,omitempty"`
	Status         string                        `json:"status,omitempty"`
	Recommendation provider.Recommendation       `json:"recommendation"`
	CompiledBrief  *ipc.TaskCompiledBriefSummary `json:"compiled_brief,omitempty"`
	Benchmark      *benchmark.Run                `json:"benchmark,omitempty"`
}

func taskPlanViewFromStatus(response ipc.TaskStatusResponse) taskPlanView {
	var benchmarkView *benchmark.Run
	if strings.TrimSpace(string(response.CurrentBenchmarkID)) != "" {
		benchmarkView = &benchmark.Run{
			BenchmarkID:                   response.CurrentBenchmarkID,
			TaskID:                        response.TaskID,
			Source:                        response.CurrentBenchmarkSource,
			RawPromptTokenEstimate:        response.CurrentBenchmarkRawPromptTokens,
			DispatchPromptTokenEstimate:   response.CurrentBenchmarkDispatchPromptTokens,
			StructuredPromptTokenEstimate: response.CurrentBenchmarkStructuredPromptTokens,
			SelectedContextTokenEstimate:  response.CurrentBenchmarkSelectedContextTokens,
			EstimatedTokenSavings:         response.CurrentBenchmarkEstimatedTokenSavings,
			FilesScanned:                  response.CurrentBenchmarkFilesScanned,
			RankedTargetCount:             response.CurrentBenchmarkRankedTargetCount,
			CandidateRecallAt3:            response.CurrentBenchmarkCandidateRecallAt3,
			StructuredCheaper:             response.CurrentBenchmarkStructuredCheaper,
			DefaultSerializer:             response.CurrentBenchmarkDefaultSerializer,
			ConfidenceValue:               response.CurrentBenchmarkConfidenceValue,
			ConfidenceLevel:               response.CurrentBenchmarkConfidenceLevel,
			Summary:                       response.CurrentBenchmarkSummary,
		}
	}
	return taskPlanView{
		TaskID:         response.TaskID,
		Phase:          string(response.Phase),
		Status:         response.Status,
		Recommendation: recommendationFromCompiledBrief(response.CompiledBrief, benchmarkView, "", string(response.LatestRunStatus), "", ""),
		CompiledBrief:  response.CompiledBrief,
		Benchmark:      benchmarkView,
	}
}

func writeTaskPlanHuman(out *os.File, response ipc.TaskStatusResponse) error {
	plan := taskPlanViewFromStatus(response)
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		"plan advisory",
		fmt.Sprintf("phase %s | status %s", nonEmpty(plan.Phase, "UNKNOWN"), nonEmpty(plan.Status, "UNKNOWN")),
	}
	lines = append(lines, providerHumanLines(plan.Recommendation)...)
	lines = append(lines, briefHumanLines(plan.CompiledBrief, true)...)
	if plan.Benchmark != nil {
		lines = append(lines, benchmarkHumanLines(
			string(plan.Benchmark.BenchmarkID),
			plan.Benchmark.Source,
			plan.Benchmark.Summary,
			plan.Benchmark.RawPromptTokenEstimate,
			plan.Benchmark.DispatchPromptTokenEstimate,
			plan.Benchmark.StructuredPromptTokenEstimate,
			plan.Benchmark.SelectedContextTokenEstimate,
			plan.Benchmark.EstimatedTokenSavings,
			plan.Benchmark.FilesScanned,
			plan.Benchmark.RankedTargetCount,
			plan.Benchmark.CandidateRecallAt3,
			plan.Benchmark.DefaultSerializer,
			plan.Benchmark.StructuredCheaper,
			plan.Benchmark.ConfidenceLevel,
			plan.Benchmark.ConfidenceValue,
		)...)
	}
	lines = append(lines, "truth plan output is bounded Tuku orchestration guidance only; it recommends routing and scoped execution framing rather than claiming downstream completion")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskBenchmarkHuman(out *os.File, response ipc.TaskBenchmarkResponse) error {
	lines := []string{
		fmt.Sprintf("task %s", response.TaskID),
		"benchmark advisory",
	}
	lines = append(lines, providerHumanLines(recommendationFromCompiledBrief(response.CompiledBrief, response.Benchmark, "", "", "", ""))...)
	if response.Benchmark == nil {
		lines = append(lines, "  digest none")
	} else {
		lines = append(lines,
			fmt.Sprintf("  id %s | source %s", response.Benchmark.BenchmarkID, nonEmpty(response.Benchmark.Source, "unknown")),
			fmt.Sprintf("  summary %s", nonEmpty(strings.TrimSpace(response.Benchmark.Summary), "benchmark summary unavailable")),
			fmt.Sprintf("  tokens raw=%d dispatch=%d structured=%d selected-context=%d saved=%d",
				response.Benchmark.RawPromptTokenEstimate,
				response.Benchmark.DispatchPromptTokenEstimate,
				response.Benchmark.StructuredPromptTokenEstimate,
				response.Benchmark.SelectedContextTokenEstimate,
				response.Benchmark.EstimatedTokenSavings,
			),
			fmt.Sprintf("  targeting files-scanned=%d ranked-targets=%d recall@3=%.2f",
				response.Benchmark.FilesScanned,
				response.Benchmark.RankedTargetCount,
				response.Benchmark.CandidateRecallAt3,
			),
			fmt.Sprintf("  serializer %s | structured-cheaper=%t | confidence %s %.2f",
				nonEmpty(response.Benchmark.DefaultSerializer, "natural_language"),
				response.Benchmark.StructuredCheaper,
				nonEmpty(response.Benchmark.ConfidenceLevel, "unknown"),
				response.Benchmark.ConfidenceValue,
			),
		)
		if len(response.Benchmark.ChangedFiles) > 0 {
			lines = append(lines, "  changed "+strings.Join(response.Benchmark.ChangedFiles, ", "))
		}
	}
	lines = append(lines, briefHumanLines(response.CompiledBrief, true)...)
	lines = append(lines, "truth benchmark output is bounded Tuku telemetry only; it estimates prompt and context savings rather than provider-billed token counts")
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func recommendationFromCompiledBrief(
	compiled *ipc.TaskCompiledBriefSummary,
	benchmarkView *benchmark.Run,
	latestRunWorker string,
	latestRunStatus string,
	handoffTarget string,
	handoffStatus string,
) provider.Recommendation {
	signals := provider.Signals{
		LatestRunWorker: provider.WorkerKind(strings.ToLower(strings.TrimSpace(latestRunWorker))),
		LatestRunStatus: strings.ToUpper(strings.TrimSpace(latestRunStatus)),
		HandoffTarget:   provider.WorkerKind(strings.ToLower(strings.TrimSpace(handoffTarget))),
		HandoffStatus:   strings.ToUpper(strings.TrimSpace(handoffStatus)),
	}
	if compiled != nil {
		signals.BriefPosture = compiled.Posture
		signals.RequiresClarification = compiled.RequiresClarification
		if promptIR := compiled.PromptIR; promptIR != nil {
			signals.NormalizedTaskType = promptIR.NormalizedTaskType
			signals.ValidatorCount = len(promptIR.ValidatorPlan.Commands)
			signals.RankedTargetCount = len(promptIR.RankedTargets)
			signals.ConfidenceLevel = promptIR.Confidence.Level
		}
	}
	if benchmarkView != nil {
		signals.EstimatedTokenSavings = benchmarkView.EstimatedTokenSavings
		if signals.ConfidenceLevel == "" {
			signals.ConfidenceLevel = benchmarkView.ConfidenceLevel
		}
		if signals.RankedTargetCount == 0 {
			signals.RankedTargetCount = benchmarkView.RankedTargetCount
		}
	}
	return provider.Recommend(signals)
}

func providerHumanLines(recommendation provider.Recommendation) []string {
	lines := []string{"worker routing"}
	if recommendation.Worker == "" || recommendation.Worker == provider.WorkerUnknown {
		return append(lines, "  digest none")
	}
	lines = append(lines, fmt.Sprintf(
		"  recommended %s | confidence %s",
		provider.Label(recommendation.Worker),
		nonEmpty(recommendation.Confidence, "unknown"),
	))
	if reason := strings.TrimSpace(recommendation.Reason); reason != "" {
		lines = append(lines, "  reason "+reason)
	}
	for _, why := range recommendation.Why {
		if strings.EqualFold(strings.TrimSpace(why), strings.TrimSpace(recommendation.Reason)) {
			continue
		}
		lines = append(lines, "  why "+why)
	}
	return lines
}

func writeTaskNextHuman(out *os.File, response ipc.TaskExecutePrimaryOperatorStepResponse) error {
	extra := []string{}
	if summary := strings.TrimSpace(response.Receipt.Summary); summary != "" {
		extra = append(extra, "result "+summary)
	}
	if response.OperatorDecision != nil && strings.TrimSpace(response.OperatorDecision.Headline) != "" {
		extra = append(extra, "decision "+response.OperatorDecision.Headline)
	}
	if response.OperatorExecutionPlan != nil && response.OperatorExecutionPlan.PrimaryStep != nil {
		step := response.OperatorExecutionPlan.PrimaryStep
		extra = append(extra, fmt.Sprintf(
			"next step %s (%s)",
			nonEmpty(step.Action, "none"),
			nonEmpty(step.Status, "unknown"),
		))
	}
	lines := composeHumanActionLines(
		response.TaskID,
		fmt.Sprintf(
			"%s (%s)",
			nonEmpty(response.Receipt.ActionHandle, "OPERATOR_STEP"),
			nonEmpty(response.Receipt.ResultClass, "UNKNOWN"),
		),
		"",
		"",
		response.ContinuityIncidentFollowUp,
		response.ContinuityIncidentFollowUpHistoryRollup,
		extra,
		"truth human mode summarizes bounded audit advisories only; it does not imply completion, correctness, resumability, transcript completeness, or downstream worker completion",
	)
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskActionHuman(
	ctx context.Context,
	socketPath string,
	out *os.File,
	actionLabel string,
	taskID common.TaskID,
	resultSummary string,
	canonicalResponse string,
) error {
	followUp, rollup, err := fetchTaskFollowUpSummary(ctx, socketPath, taskID)
	if err != nil {
		return err
	}
	lines := composeHumanActionLines(
		taskID,
		nonEmpty(actionLabel, "task action"),
		resultSummary,
		canonicalResponse,
		followUp,
		rollup,
		[]string{},
		"truth human mode summarizes bounded audit advisories only; it does not imply completion, correctness, resumability, transcript completeness, or downstream worker completion",
	)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeRecoveryActionHuman(
	ctx context.Context,
	socketPath string,
	out *os.File,
	actionLabel string,
	taskID common.TaskID,
	resultSummary string,
	recoveryClass string,
	recommendedAction string,
	recoveryReason string,
	canonicalResponse string,
) error {
	followUp, rollup, err := fetchTaskFollowUpSummary(ctx, socketPath, taskID)
	if err != nil {
		return err
	}
	extra := []string{}
	if strings.TrimSpace(recoveryClass) != "" || strings.TrimSpace(recommendedAction) != "" {
		extra = append(extra, fmt.Sprintf("recovery class %s | recommended %s", nonEmpty(recoveryClass, "UNKNOWN"), nonEmpty(recommendedAction, "none")))
	}
	if reason := strings.TrimSpace(recoveryReason); reason != "" {
		extra = append(extra, "recovery reason "+reason)
	}
	lines := composeHumanActionLines(
		taskID,
		nonEmpty(actionLabel, "recovery action"),
		resultSummary,
		canonicalResponse,
		followUp,
		rollup,
		extra,
		"truth human mode summarizes bounded audit advisories only; it does not imply completion, correctness, resumability, transcript completeness, or downstream worker completion",
	)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeHandoffActionHuman(
	ctx context.Context,
	socketPath string,
	out *os.File,
	actionLabel string,
	taskID common.TaskID,
	resultSummary string,
	recoveryClass string,
	recommendedAction string,
	recoveryReason string,
	canonicalResponse string,
) error {
	followUp, rollup, err := fetchTaskFollowUpSummary(ctx, socketPath, taskID)
	if err != nil {
		return err
	}
	extra := []string{}
	if strings.TrimSpace(recoveryClass) != "" || strings.TrimSpace(recommendedAction) != "" {
		extra = append(extra, fmt.Sprintf("recovery class %s | recommended %s", nonEmpty(recoveryClass, "UNKNOWN"), nonEmpty(recommendedAction, "none")))
	}
	if reason := strings.TrimSpace(recoveryReason); reason != "" {
		extra = append(extra, "recovery reason "+reason)
	}
	lines := composeHumanActionLines(
		taskID,
		nonEmpty(actionLabel, "handoff action"),
		resultSummary,
		canonicalResponse,
		followUp,
		rollup,
		extra,
		"truth handoff action results remain bounded continuity evidence only; launch, acknowledgment, follow-through, and resolution do not imply downstream completion or task correctness",
	)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeTaskShellSessionsHuman(
	ctx context.Context,
	socketPath string,
	out *os.File,
	response ipc.TaskShellSessionsResponse,
) error {
	followUp, rollup, err := fetchTaskFollowUpSummary(ctx, socketPath, response.TaskID)
	if err != nil {
		return err
	}
	extra := []string{fmt.Sprintf("sessions %d", len(response.Sessions))}
	for _, session := range response.Sessions {
		summary := strings.TrimSpace(session.OperatorSummary)
		if summary == "" {
			summary = strings.TrimSpace(session.ReattachGuidance)
		}
		if summary == "" {
			summary = strings.TrimSpace(session.SessionClassReason)
		}
		line := fmt.Sprintf(
			"session %s class=%s attach=%s worker=%s active=%t",
			nonEmpty(session.SessionID, "n/a"),
			nonEmpty(strings.ToLower(strings.ReplaceAll(session.SessionClass, "_", "-")), "unknown"),
			nonEmpty(strings.ToLower(strings.ReplaceAll(session.AttachCapability, "_", "-")), "unknown"),
			nonEmpty(session.ResolvedWorker, "unknown"),
			session.Active,
		)
		if summary != "" {
			line += " note=" + summary
		}
		extra = append(extra, line)
	}
	lines := composeHumanActionLines(
		response.TaskID,
		"shell sessions report",
		"",
		"",
		followUp,
		rollup,
		extra,
		"truth shell session reports are bounded continuity evidence only; attachability and worker-session markers do not imply live resumability, full transcript completeness, or process resurrection",
	)
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func composeHumanActionLines(
	taskID common.TaskID,
	actionLabel string,
	resultSummary string,
	canonicalResponse string,
	followUp *ipc.TaskContinuityIncidentFollowUpSummary,
	rollup *ipc.TaskContinuityIncidentFollowUpHistoryRollup,
	extra []string,
	truthLine string,
) []string {
	lines := []string{
		fmt.Sprintf("task %s", taskID),
		fmt.Sprintf("executed %s", actionLabel),
	}
	lines = append(lines, followUpHumanLines(followUp, rollup, false)...)
	if summary := strings.TrimSpace(resultSummary); summary != "" {
		lines = append(lines, "result "+summary)
	}
	if canonical := strings.TrimSpace(canonicalResponse); canonical != "" {
		lines = append(lines, "canonical "+canonical)
	}
	lines = append(lines, extra...)
	if line := strings.TrimSpace(truthLine); line != "" {
		lines = append(lines, line)
	}
	return lines
}

func fetchTaskFollowUpSummary(
	ctx context.Context,
	socketPath string,
	taskID common.TaskID,
) (*ipc.TaskContinuityIncidentFollowUpSummary, *ipc.TaskContinuityIncidentFollowUpHistoryRollup, error) {
	payload, _ := json.Marshal(ipc.TaskStatusRequest{TaskID: taskID})
	resp, err := ipcCall(ctx, socketPath, ipc.Request{RequestID: requestID(), Method: ipc.MethodTaskStatus, Payload: payload})
	if err != nil {
		return nil, nil, err
	}
	var status ipc.TaskStatusResponse
	if err := json.Unmarshal(resp.Payload, &status); err != nil {
		return nil, nil, err
	}
	return status.ContinuityIncidentFollowUp, status.ContinuityIncidentFollowUpHistoryRollup, nil
}

func intentHumanLines(compiled *ipc.TaskCompiledIntentSummary, includeDetails bool) []string {
	lines := []string{"intent advisory"}
	if compiled == nil {
		return append(lines, "  digest none")
	}
	digest := strings.TrimSpace(compiled.Digest)
	if digest == "" {
		digest = strings.ToLower(strings.ReplaceAll(nonEmpty(compiled.ExecutionReadiness, "unknown"), "_", " "))
	}
	lines = append(lines, "  digest "+nonEmpty(digest, "none"))
	if compiled.BoundedEvidenceMessages > 0 {
		lines = append(lines, fmt.Sprintf("  window bounded recent messages=%d", compiled.BoundedEvidenceMessages))
	}
	detail := strings.TrimSpace(compiled.Advisory)
	if detail == "" {
		detail = strings.TrimSpace(compiled.ReadinessReason)
	}
	if detail != "" {
		lines = append(lines, "  detail "+detail)
	}
	if includeDetails {
		if objective := strings.TrimSpace(compiled.Objective); objective != "" {
			lines = append(lines, "  objective "+objective)
		}
		if outcome := strings.TrimSpace(compiled.RequestedOutcome); outcome != "" {
			lines = append(lines, "  outcome "+outcome)
		}
		lines = append(lines, fmt.Sprintf("  posture %s | readiness %s", nonEmpty(compiled.Posture, "UNKNOWN"), nonEmpty(compiled.ExecutionReadiness, "UNKNOWN")))
		if scope := strings.TrimSpace(compiled.ScopeSummary); scope != "" {
			lines = append(lines, "  scope "+scope)
		}
		if len(compiled.ExplicitConstraints) > 0 {
			lines = append(lines, "  constraints "+strings.Join(compiled.ExplicitConstraints, " | "))
		}
		if len(compiled.DoneCriteria) > 0 {
			lines = append(lines, "  done "+strings.Join(compiled.DoneCriteria, " | "))
		}
		if len(compiled.AmbiguityFlags) > 0 {
			lines = append(lines, "  ambiguity "+strings.Join(compiled.AmbiguityFlags, ", "))
		}
		if len(compiled.ClarificationQuestions) > 0 {
			lines = append(lines, "  clarification "+strings.Join(compiled.ClarificationQuestions, " | "))
		}
	}
	return lines
}

func briefHumanLines(compiled *ipc.TaskCompiledBriefSummary, includeDetails bool) []string {
	lines := []string{"brief advisory"}
	if compiled == nil {
		return append(lines, "  digest none")
	}
	digest := strings.TrimSpace(compiled.Digest)
	if digest == "" {
		digest = strings.ToLower(strings.ReplaceAll(nonEmpty(compiled.Posture, "unknown"), "_", " "))
	}
	lines = append(lines, "  digest "+nonEmpty(digest, "none"))
	if compiled.BoundedEvidenceMessages > 0 {
		lines = append(lines, fmt.Sprintf("  window bounded recent messages=%d", compiled.BoundedEvidenceMessages))
	}
	if triage := compiled.PromptTriage; triage != nil && triage.Applied {
		lines = append(lines, fmt.Sprintf("  prompt sharpened yes | files scanned %d | candidates %d | saved context tokens %d", triage.FilesScanned, len(triage.CandidateFiles), triage.ContextTokenSavingsEstimate))
	}
	if memory := compiled.MemoryCompression; memory != nil && memory.Applied {
		lines = append(lines, fmt.Sprintf("  task memory yes | history tokens %d | resume tokens %d | compaction %.2fx", memory.FullHistoryTokenEstimate, memory.ResumePromptTokenEstimate, memory.MemoryCompactionRatio))
	}
	if promptIR := compiled.PromptIR; promptIR != nil && (len(promptIR.RankedTargets) > 0 || len(promptIR.ValidatorPlan.Commands) > 0) {
		headline := fmt.Sprintf(
			"  prompt ir yes | targets %d | validators %d | confidence %s %.2f",
			len(promptIR.RankedTargets),
			len(promptIR.ValidatorPlan.Commands),
			nonEmpty(promptIR.Confidence.Level, "unknown"),
			promptIR.Confidence.Value,
		)
		if summary := strings.TrimSpace(promptIR.RepoIndexSummary); summary != "" {
			headline += " | repo index " + summary
		}
		lines = append(lines, headline)
	}
	detail := strings.TrimSpace(compiled.Advisory)
	if detail != "" {
		lines = append(lines, "  detail "+detail)
	}
	if includeDetails {
		if objective := strings.TrimSpace(compiled.Objective); objective != "" {
			lines = append(lines, "  objective "+objective)
		}
		if outcome := strings.TrimSpace(compiled.RequestedOutcome); outcome != "" {
			lines = append(lines, "  outcome "+outcome)
		}
		if action := strings.TrimSpace(compiled.NormalizedAction); action != "" {
			lines = append(lines, "  action "+action)
		}
		if scope := strings.TrimSpace(compiled.ScopeSummary); scope != "" {
			lines = append(lines, "  scope "+scope)
		}
		lines = append(lines, fmt.Sprintf("  posture %s | requires clarification %t", nonEmpty(compiled.Posture, "UNKNOWN"), compiled.RequiresClarification))
		if len(compiled.Constraints) > 0 {
			lines = append(lines, "  constraints "+strings.Join(compiled.Constraints, " | "))
		}
		if len(compiled.DoneCriteria) > 0 {
			lines = append(lines, "  done "+strings.Join(compiled.DoneCriteria, " | "))
		}
		if len(compiled.AmbiguityFlags) > 0 {
			lines = append(lines, "  ambiguity "+strings.Join(compiled.AmbiguityFlags, ", "))
		}
		if len(compiled.ClarificationQuestions) > 0 {
			lines = append(lines, "  clarification "+strings.Join(compiled.ClarificationQuestions, " | "))
		}
		if framing := strings.TrimSpace(compiled.WorkerFraming); framing != "" {
			lines = append(lines, "  framing "+framing)
		}
		if triage := compiled.PromptTriage; triage != nil && triage.Applied {
			if summary := strings.TrimSpace(triage.Summary); summary != "" {
				lines = append(lines, "  triage "+summary)
			}
			if len(triage.SearchTerms) > 0 {
				lines = append(lines, "  search "+strings.Join(triage.SearchTerms, ", "))
			}
			if len(triage.CandidateFiles) > 0 {
				lines = append(lines, "  candidates "+strings.Join(triage.CandidateFiles, ", "))
			}
			lines = append(lines, fmt.Sprintf("  token estimate raw=%d rewritten=%d search-space=%d selected-context=%d saved=%d",
				triage.RawPromptTokenEstimate,
				triage.RewrittenPromptTokenEstimate,
				triage.SearchSpaceTokenEstimate,
				triage.SelectedContextTokenEstimate,
				triage.ContextTokenSavingsEstimate,
			))
		}
		if memory := compiled.MemoryCompression; memory != nil && memory.Applied {
			if summary := strings.TrimSpace(memory.Summary); summary != "" {
				lines = append(lines, "  memory "+summary)
			}
			lines = append(lines, fmt.Sprintf("  memory tokens history=%d resume=%d compaction=%.2fx facts=%d touched=%d validators=%d candidates=%d rejected=%d unknowns=%d",
				memory.FullHistoryTokenEstimate,
				memory.ResumePromptTokenEstimate,
				memory.MemoryCompactionRatio,
				memory.ConfirmedFactsCount,
				memory.TouchedFilesCount,
				memory.ValidatorsRunCount,
				memory.CandidateFilesCount,
				memory.RejectedHypothesesCount,
				memory.UnknownsCount,
			))
		}
		if promptIR := compiled.PromptIR; promptIR != nil {
			if taskType := strings.TrimSpace(promptIR.NormalizedTaskType); taskType != "" {
				lines = append(lines, "  prompt ir type "+taskType)
			}
			if repoIndexID := strings.TrimSpace(string(promptIR.RepoIndexID)); repoIndexID != "" || strings.TrimSpace(promptIR.RepoIndexSummary) != "" {
				repoIndexLine := "  prompt ir repo index " + nonEmpty(repoIndexID, "unknown")
				if summary := strings.TrimSpace(promptIR.RepoIndexSummary); summary != "" {
					repoIndexLine += " | " + summary
				}
				lines = append(lines, repoIndexLine)
			}
			if len(promptIR.RankedTargets) > 0 {
				targets := make([]string, 0, min(len(promptIR.RankedTargets), 5))
				for _, target := range promptIR.RankedTargets {
					label := strings.TrimSpace(target.Path)
					if strings.TrimSpace(target.Name) != "" {
						if label != "" {
							label = label + "#" + strings.TrimSpace(target.Name)
						} else {
							label = strings.TrimSpace(target.Name)
						}
					}
					if label == "" {
						continue
					}
					targets = append(targets, label)
					if len(targets) >= 5 {
						break
					}
				}
				if len(targets) > 0 {
					lines = append(lines, "  prompt ir targets "+strings.Join(targets, ", "))
				}
			}
			if len(promptIR.OperationPlan) > 0 {
				lines = append(lines, "  prompt ir plan "+strings.Join(promptIR.OperationPlan, " | "))
			}
			if len(promptIR.ValidatorPlan.Commands) > 0 {
				lines = append(lines, "  validators "+strings.Join(promptIR.ValidatorPlan.Commands, " | "))
			}
			lines = append(lines, fmt.Sprintf(
				"  serializer default=%s natural=%d structured=%d structured-cheaper=%t",
				nonEmpty(string(promptIR.DefaultSerializer), "natural_language"),
				promptIR.NaturalLanguageTokens,
				promptIR.StructuredTokens,
				promptIR.StructuredCheaper,
			))
			if reason := strings.TrimSpace(promptIR.Confidence.Reason); reason != "" {
				lines = append(lines, "  confidence "+nonEmpty(promptIR.Confidence.Level, "unknown")+" "+reason)
			}
		}
	}
	return lines
}

func taskMemoryHumanLines(memoryID string, source string, summary string, fullHistoryTokens int, resumeTokens int, compactionRatio float64) []string {
	lines := []string{"task memory"}
	if strings.TrimSpace(memoryID) == "" {
		return append(lines, "  digest none")
	}
	lines = append(lines, fmt.Sprintf("  id %s | source %s", memoryID, nonEmpty(source, "unknown")))
	lines = append(lines, fmt.Sprintf("  history tokens %d | resume tokens %d | compaction %.2fx", fullHistoryTokens, resumeTokens, compactionRatio))
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, "  summary "+summary)
	}
	return lines
}

func benchmarkHumanLines(
	benchmarkID string,
	source string,
	summary string,
	rawTokens int,
	dispatchTokens int,
	structuredTokens int,
	selectedContextTokens int,
	estimatedSavings int,
	filesScanned int,
	rankedTargets int,
	recallAt3 float64,
	defaultSerializer string,
	structuredCheaper bool,
	confidenceLevel string,
	confidenceValue float64,
) []string {
	lines := []string{"benchmark"}
	if strings.TrimSpace(benchmarkID) == "" {
		return append(lines, "  digest none")
	}
	lines = append(lines, fmt.Sprintf("  id %s | source %s", benchmarkID, nonEmpty(source, "unknown")))
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, "  summary "+summary)
	}
	lines = append(lines, fmt.Sprintf(
		"  tokens raw=%d dispatch=%d structured=%d selected-context=%d saved=%d",
		rawTokens,
		dispatchTokens,
		structuredTokens,
		selectedContextTokens,
		estimatedSavings,
	))
	lines = append(lines, fmt.Sprintf(
		"  targeting files-scanned=%d ranked-targets=%d recall@3=%.2f",
		filesScanned,
		rankedTargets,
		recallAt3,
	))
	lines = append(lines, fmt.Sprintf(
		"  serializer %s | structured-cheaper=%t | confidence %s %.2f",
		nonEmpty(defaultSerializer, "natural_language"),
		structuredCheaper,
		nonEmpty(confidenceLevel, "unknown"),
		confidenceValue,
	))
	return lines
}

func followUpHumanLines(followUp *ipc.TaskContinuityIncidentFollowUpSummary, rollup *ipc.TaskContinuityIncidentFollowUpHistoryRollup, includeClosureAnchors bool) []string {
	lines := []string{"follow-up advisory"}
	if followUp == nil {
		lines = append(lines, "  digest none")
		if rollup != nil && rollup.WindowSize > 0 {
			lines = append(lines, fmt.Sprintf("  bounded window anchors=%d open=%d reopened=%d triaged-without-follow-up=%d repeated=%d",
				rollup.DistinctAnchors,
				rollup.AnchorsWithOpenFollowUp,
				rollup.AnchorsReopened,
				rollup.AnchorsTriagedWithoutFollowUp,
				rollup.AnchorsRepeatedWithoutProgression,
			))
		}
		return lines
	}
	lines = append(lines, "  digest "+nonEmpty(strings.TrimSpace(followUp.Digest), strings.ToLower(strings.ReplaceAll(nonEmpty(followUp.State, "none"), "_", " "))))
	if window := strings.TrimSpace(followUp.WindowAdvisory); window != "" {
		lines = append(lines, "  window "+window)
	} else if rollup != nil && rollup.WindowSize > 0 {
		lines = append(lines, fmt.Sprintf("  bounded window anchors=%d open=%d reopened=%d triaged-without-follow-up=%d repeated=%d",
			rollup.DistinctAnchors,
			rollup.AnchorsWithOpenFollowUp,
			rollup.AnchorsReopened,
			rollup.AnchorsTriagedWithoutFollowUp,
			rollup.AnchorsRepeatedWithoutProgression,
		))
	}
	if advisory := strings.TrimSpace(followUp.Advisory); advisory != "" {
		lines = append(lines, "  detail "+advisory)
	}
	lines = append(lines, closureHumanLines(followUp.ClosureIntelligence, includeClosureAnchors)...)
	return lines
}

func taskRiskHumanLines(risk *ipc.TaskContinuityIncidentTaskRiskSummary, includeRecentClasses bool) []string {
	if risk == nil {
		return nil
	}
	lines := []string{"task incident risk advisory"}
	digest := strings.TrimSpace(risk.Digest)
	if digest == "" {
		digest = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(nonEmpty(risk.Class, "NONE")), "_", " "))
	}
	lines = append(lines, "  digest "+nonEmpty(digest, "none"))
	if window := strings.TrimSpace(risk.WindowAdvisory); window != "" {
		lines = append(lines, "  window "+window)
	}
	if detail := strings.TrimSpace(risk.Detail); detail != "" {
		lines = append(lines, "  detail "+detail)
	}
	if includeRecentClasses && len(risk.RecentAnchorClasses) > 0 {
		classes := make([]string, 0, len(risk.RecentAnchorClasses))
		for _, item := range risk.RecentAnchorClasses {
			classes = append(classes, strings.ToLower(strings.ReplaceAll(strings.TrimSpace(nonEmpty(item, "NONE")), "_", " ")))
		}
		lines = append(lines, "  classes "+strings.Join(classes, ", "))
	}
	return lines
}

func closureHumanLines(closure *ipc.TaskContinuityIncidentClosureSummary, includeAnchors bool) []string {
	if closure == nil {
		return nil
	}
	lines := []string{"closure advisory"}
	digest := strings.TrimSpace(closure.Digest)
	if digest == "" {
		digest = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(nonEmpty(closure.Class, "NONE")), "_", " "))
	}
	lines = append(lines, "  digest "+nonEmpty(digest, "none"))
	if window := strings.TrimSpace(closure.WindowAdvisory); window != "" {
		lines = append(lines, "  window "+window)
	}
	if detail := strings.TrimSpace(closure.Detail); detail != "" {
		lines = append(lines, "  detail "+detail)
	}
	if includeAnchors && len(closure.RecentAnchors) > 0 {
		lines = append(lines, "  recent anchors")
		for _, anchor := range closure.RecentAnchors {
			lines = append(lines, fmt.Sprintf(
				"    anchor=%s class=%s action=%s",
				shortHumanID(anchor.AnchorTransitionReceiptID),
				strings.ToLower(strings.ReplaceAll(nonEmpty(anchor.Class, "NONE"), "_", " ")),
				strings.ToLower(strings.ReplaceAll(nonEmpty(anchor.LatestFollowUpActionKind, "NONE"), "_", " ")),
			))
			if note := strings.TrimSpace(anchor.Explanation); note != "" {
				lines = append(lines, "      "+note)
			}
		}
	}
	return lines
}

func shortHumanID(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 10 {
		return trimmed
	}
	return trimmed[:10]
}

func writeJSON(out *os.File, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
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
