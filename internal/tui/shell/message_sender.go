package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

type TaskMessageSender interface {
	Send(taskID string, message string) error
}

type IPCTaskMessageSender struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCTaskMessageSender(socketPath string) *IPCTaskMessageSender {
	return &IPCTaskMessageSender{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (s *IPCTaskMessageSender) Send(taskID string, message string) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskMessageRequest{
		TaskID:  common.TaskID(taskID),
		Message: message,
	})
	if err != nil {
		return err
	}

	_, err = ipc.CallUnix(ctx, s.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("task_message_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodSendMessage,
		Payload:   payload,
	})
	return err
}
