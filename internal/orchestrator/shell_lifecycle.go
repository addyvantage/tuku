package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/proof"
	"tuku/internal/domain/shellsession"
)

type ShellLifecycleKind string

const (
	ShellLifecycleHostStarted       ShellLifecycleKind = "host_started"
	ShellLifecycleHostExited        ShellLifecycleKind = "host_exited"
	ShellLifecycleFallback          ShellLifecycleKind = "fallback_activated"
	ShellLifecycleReattachRequested ShellLifecycleKind = "reattach_requested"
	ShellLifecycleReattachFailed    ShellLifecycleKind = "reattach_failed"
)

type RecordShellLifecycleRequest struct {
	TaskID                string
	SessionID             string
	Kind                  ShellLifecycleKind
	HostMode              string
	HostState             string
	WorkerSessionID       string
	WorkerSessionIDSource shellsession.WorkerSessionIDSource
	AttachCapability      shellsession.AttachCapability
	Note                  string
	InputLive             bool
	ExitCode              *int
	PaneWidth             int
	PaneHeight            int
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
	if workerSessionID := strings.TrimSpace(req.WorkerSessionID); workerSessionID != "" {
		payload["worker_session_id"] = workerSessionID
	}
	if source := normalizeWorkerSessionIDSource(req.WorkerSessionIDSource, strings.TrimSpace(req.WorkerSessionID)); source != shellsession.WorkerSessionIDSourceNone {
		payload["worker_session_id_source"] = source
	}
	if capability := normalizeAttachCapability(req.AttachCapability); capability != shellsession.AttachCapabilityNone {
		payload["attach_capability"] = capability
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
	record := shellsession.Record{}
	if persisted, loadErr := c.loadShellSessionRecord(taskID, strings.TrimSpace(req.SessionID)); loadErr == nil {
		record = persisted
	}
	requestedWorkerSessionID := strings.TrimSpace(req.WorkerSessionID)
	workerSessionID := requestedWorkerSessionID
	if workerSessionID == "" {
		workerSessionID = record.WorkerSessionID
	}
	workerSessionIDSource := normalizeWorkerSessionIDSource(req.WorkerSessionIDSource, workerSessionID)
	if requestedWorkerSessionID == "" {
		workerSessionIDSource = normalizeWorkerSessionIDSource(record.WorkerSessionIDSource, workerSessionID)
	}
	attachCapability := normalizeAttachCapability(req.AttachCapability)
	if attachCapability == shellsession.AttachCapabilityNone {
		attachCapability = record.AttachCapability
	}
	exitCode := req.ExitCode
	if err := c.appendShellSessionEvent(shellsession.Event{
		TaskID:                taskID,
		SessionID:             strings.TrimSpace(req.SessionID),
		Kind:                  shellLifecycleEventKind(req.Kind),
		HostMode:              strings.TrimSpace(req.HostMode),
		HostState:             strings.TrimSpace(req.HostState),
		WorkerSessionID:       workerSessionID,
		WorkerSessionIDSource: workerSessionIDSource,
		AttachCapability:      attachCapability,
		Active:                record.Active,
		InputLive:             req.InputLive,
		ExitCode:              exitCode,
		PaneWidth:             req.PaneWidth,
		PaneHeight:            req.PaneHeight,
		Note:                  strings.TrimSpace(req.Note),
		CreatedAt:             c.clock(),
	}); err != nil {
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
	case ShellLifecycleReattachRequested:
		return proof.EventShellReattachRequested, nil
	case ShellLifecycleReattachFailed:
		return proof.EventShellReattachFailed, nil
	default:
		return "", fmt.Errorf("unsupported shell lifecycle kind: %s", kind)
	}
}

func shellLifecycleEventKind(kind ShellLifecycleKind) shellsession.EventKind {
	switch kind {
	case ShellLifecycleHostStarted:
		return shellsession.EventKindHostStarted
	case ShellLifecycleHostExited:
		return shellsession.EventKindHostExited
	case ShellLifecycleFallback:
		return shellsession.EventKindFallbackActivated
	case ShellLifecycleReattachRequested:
		return shellsession.EventKindReattachRequested
	case ShellLifecycleReattachFailed:
		return shellsession.EventKindReattachFailed
	default:
		return shellsession.EventKindSessionReported
	}
}
