package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

type PersistedLifecycleKind string

const (
	PersistedLifecycleHostStarted       PersistedLifecycleKind = "host_started"
	PersistedLifecycleHostExited        PersistedLifecycleKind = "host_exited"
	PersistedLifecycleFallback          PersistedLifecycleKind = "fallback_activated"
	PersistedLifecycleReattachRequested PersistedLifecycleKind = "reattach_requested"
	PersistedLifecycleReattachFailed    PersistedLifecycleKind = "reattach_failed"
)

type LifecycleSink interface {
	Record(taskID string, sessionID string, kind PersistedLifecycleKind, status HostStatus) error
}

type IPCLifecycleSink struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCLifecycleSink(socketPath string) *IPCLifecycleSink {
	return &IPCLifecycleSink{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (s *IPCLifecycleSink) Record(taskID string, sessionID string, kind PersistedLifecycleKind, status HostStatus) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskShellLifecycleRequest{
		TaskID:                common.TaskID(taskID),
		SessionID:             sessionID,
		Kind:                  string(kind),
		HostMode:              string(status.Mode),
		HostState:             string(status.State),
		WorkerSessionID:       status.WorkerSessionID,
		WorkerSessionIDSource: string(status.WorkerSessionIDSource),
		Note:                  status.Note,
		InputLive:             status.InputLive,
		ExitCode:              status.ExitCode,
		PaneWidth:             status.Width,
		PaneHeight:            status.Height,
	})
	if err != nil {
		return err
	}

	_, err = ipc.CallUnix(ctx, s.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_evt_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellLifecycle,
		Payload:   payload,
	})
	return err
}
