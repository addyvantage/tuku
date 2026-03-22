package shell

import (
	"context"
	"encoding/json"
	"testing"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

func TestExecutePrimaryStepIPCUsesUnifiedBackendNextRoute(t *testing.T) {
	original := primaryActionIPCCall
	defer func() { primaryActionIPCCall = original }()

	called := false
	primaryActionIPCCall = func(_ context.Context, socketPath string, req ipc.Request) (ipc.Response, error) {
		called = true
		if socketPath != "/tmp/tuku.sock" {
			t.Fatalf("unexpected socket path: %s", socketPath)
		}
		if req.Method != ipc.MethodExecutePrimaryOperatorStep {
			t.Fatalf("expected unified primary-step method, got %s", req.Method)
		}
		var payload ipc.TaskExecutePrimaryOperatorStepRequest
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.TaskID != common.TaskID("tsk_123") {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		body, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_123"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_123",
				TaskID:       common.TaskID("tsk_123"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_123",
			},
		})
		return ipc.Response{OK: true, Payload: body}, nil
	}

	out, err := executePrimaryStepIPC(context.Background(), "/tmp/tuku.sock", "tsk_123", OperatorExecutionStep{Action: "START_LOCAL_RUN", CommandSurface: "DEDICATED"})
	if err != nil {
		t.Fatalf("executePrimaryStepIPC: %v", err)
	}
	if !called {
		t.Fatal("expected unified IPC route to be called")
	}
	if out.Receipt.ReceiptID != "orec_123" || out.Receipt.ActionHandle != "START_LOCAL_RUN" || out.Receipt.ResultClass != "SUCCEEDED" {
		t.Fatalf("unexpected execution outcome: %+v", out)
	}
}

func TestExecutablePrimaryStepRejectsInspectFallback(t *testing.T) {
	_, err := executablePrimaryStep(Snapshot{
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "REVIEW_HANDOFF_FOLLOW_THROUGH",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_123",
			},
		},
	})
	if err == nil {
		t.Fatal("expected inspect-fallback primary step to be non-executable")
	}
}

func TestExecutablePrimaryStepRequiresDedicatedCommandSurface(t *testing.T) {
	_, err := executablePrimaryStep(Snapshot{
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_123",
			},
		},
	})
	if err == nil {
		t.Fatal("expected non-dedicated command surface to block direct execution")
	}
}
