package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
)

type ShellLifecycleKind string

const (
	ShellLifecycleHostStarted ShellLifecycleKind = "host_started"
	ShellLifecycleHostExited  ShellLifecycleKind = "host_exited"
	ShellLifecycleFallback    ShellLifecycleKind = "fallback_activated"
)

type RecordShellLifecycleRequest struct {
	TaskID     string
	SessionID  string
	Kind       ShellLifecycleKind
	HostMode   string
	HostState  string
	Note       string
	InputLive  bool
	ExitCode   *int
	PaneWidth  int
	PaneHeight int
}

type RecordShellLifecycleResult struct {
	TaskID common.TaskID
}

func (c *Coordinator) RecordShellLifecycle(ctx context.Context, req RecordShellLifecycleRequest) (RecordShellLifecycleResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return RecordShellLifecycleResult{}, fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return RecordShellLifecycleResult{}, fmt.Errorf("session id is required")
	}
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return RecordShellLifecycleResult{}, err
	}

	eventType, err := shellLifecycleEventType(req.Kind)
	if err != nil {
		return RecordShellLifecycleResult{}, err
	}

	payload := map[string]any{
		"session_id": req.SessionID,
		"host_mode":  req.HostMode,
		"host_state": req.HostState,
		"input_live": req.InputLive,
	}
	if note := strings.TrimSpace(req.Note); note != "" {
		payload["note"] = note
	}
	if req.ExitCode != nil {
		payload["exit_code"] = *req.ExitCode
	}
	if req.PaneWidth > 0 {
		payload["pane_width"] = req.PaneWidth
	}
	if req.PaneHeight > 0 {
		payload["pane_height"] = req.PaneHeight
	}

	if err := c.appendProof(caps, eventType, proof.ActorSystem, "tuku-shell", payload, nil); err != nil {
		return RecordShellLifecycleResult{}, err
	}
	return RecordShellLifecycleResult{TaskID: taskID}, nil
}

func shellLifecycleEventType(kind ShellLifecycleKind) (proof.EventType, error) {
	switch kind {
	case ShellLifecycleHostStarted:
		return proof.EventShellHostStarted, nil
	case ShellLifecycleHostExited:
		return proof.EventShellHostExited, nil
	case ShellLifecycleFallback:
		return proof.EventShellFallbackActivated, nil
	default:
		return "", fmt.Errorf("unsupported shell lifecycle kind: %s", kind)
	}
}
