package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

var primaryActionIPCCall = ipc.CallUnix

type PrimaryActionExecutionOutcome struct {
	Receipt OperatorStepReceiptSummary
}

type PrimaryActionExecutor interface {
	Execute(taskID string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error)
}

type IPCPrimaryActionExecutor struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCPrimaryActionExecutor(socketPath string) *IPCPrimaryActionExecutor {
	return &IPCPrimaryActionExecutor{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (e *IPCPrimaryActionExecutor) Execute(taskID string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error) {
	step, err := executablePrimaryStep(snapshot)
	if err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return executePrimaryStepIPC(ctx, e.SocketPath, taskID, *step)
}

func executablePrimaryStep(snapshot Snapshot) (*OperatorExecutionStep, error) {
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return nil, fmt.Errorf("no primary operator step is currently available")
	}
	step := snapshot.OperatorExecutionPlan.PrimaryStep
	if step.CommandSurface != "DEDICATED" {
		return nil, fmt.Errorf("primary operator step %s is guidance-only and cannot be executed directly from the shell", primaryStepActionLabel(step.Action))
	}
	return step, nil
}

func executePrimaryStepIPC(ctx context.Context, socketPath string, taskID string, step OperatorExecutionStep) (PrimaryActionExecutionOutcome, error) {
	raw, err := json.Marshal(ipc.TaskExecutePrimaryOperatorStepRequest{TaskID: common.TaskID(taskID)})
	if err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	resp, err := primaryActionIPCCall(ctx, socketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_primary_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodExecutePrimaryOperatorStep,
		Payload:   raw,
	})
	if err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	var out ipc.TaskExecutePrimaryOperatorStepResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		return PrimaryActionExecutionOutcome{}, err
	}
	if out.Receipt.ActionHandle == "" {
		return PrimaryActionExecutionOutcome{}, fmt.Errorf("primary operator step execution returned no durable receipt")
	}
	if out.Receipt.ActionHandle != step.Action {
		return PrimaryActionExecutionOutcome{}, fmt.Errorf("primary operator step changed during execution from %s to %s", primaryStepActionLabel(step.Action), primaryStepActionLabel(out.Receipt.ActionHandle))
	}
	return PrimaryActionExecutionOutcome{
		Receipt: operatorStepReceiptFromIPC(out.Receipt),
	}, nil
}

func operatorStepReceiptFromIPC(raw ipc.TaskOperatorStepReceipt) OperatorStepReceiptSummary {
	return OperatorStepReceiptSummary{
		ReceiptID:          raw.ReceiptID,
		TaskID:             string(raw.TaskID),
		ActionHandle:       raw.ActionHandle,
		ExecutionDomain:    raw.ExecutionDomain,
		CommandSurfaceKind: raw.CommandSurfaceKind,
		ExecutionAttempted: raw.ExecutionAttempted,
		ResultClass:        raw.ResultClass,
		Summary:            raw.Summary,
		Reason:             raw.Reason,
		RunID:              string(raw.RunID),
		CheckpointID:       string(raw.CheckpointID),
		BriefID:            string(raw.BriefID),
		HandoffID:          raw.HandoffID,
		LaunchAttemptID:    raw.LaunchAttemptID,
		LaunchID:           raw.LaunchID,
		CreatedAt:          raw.CreatedAt,
		CompletedAt:        raw.CompletedAt,
	}
}

func primaryStepActionLabel(action string) string {
	if action == "" {
		return "action"
	}
	return action
}
