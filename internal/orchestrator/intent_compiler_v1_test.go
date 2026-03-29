package orchestrator

import (
	"context"
	"strings"
	"testing"

	"tuku/internal/domain/intent"
)

func TestIntentCompilerV1DerivesRepresentativePostures(t *testing.T) {
	compiler := NewIntentCompilerV1()

	tests := []struct {
		name          string
		message       string
		wantClass     intent.Class
		wantPosture   intent.Posture
		wantReadiness intent.Readiness
		wantClar      bool
	}{
		{
			name:          "exploratory ambiguous",
			message:       "Maybe we should explore ideas for this?",
			wantClass:     intent.ClassImplement,
			wantPosture:   intent.PostureExploratoryAmbiguous,
			wantReadiness: intent.ReadinessClarificationNeeded,
			wantClar:      true,
		},
		{
			name:          "planning focused",
			message:       "Implement a plan for internal/orchestrator/service.go milestones.",
			wantClass:     intent.ClassImplement,
			wantPosture:   intent.PosturePlanning,
			wantReadiness: intent.ReadinessPlanningInProgress,
			wantClar:      false,
		},
		{
			name:          "execution ready",
			message:       "Implement internal/orchestrator/compiled_intent.go and add deterministic assertions.",
			wantClass:     intent.ClassImplement,
			wantPosture:   intent.PostureExecutionReady,
			wantReadiness: intent.ReadinessExecutionReady,
			wantClar:      false,
		},
		{
			name:          "validation focused",
			message:       "Validate internal/orchestrator/service.go by running go test ./...",
			wantClass:     intent.ClassValidate,
			wantPosture:   intent.PostureValidationFocused,
			wantReadiness: intent.ReadinessValidationFocused,
			wantClar:      false,
		},
		{
			name:          "repair recovery focused",
			message:       "Fix bug in internal/orchestrator/service.go by patching recovery logic.",
			wantClass:     intent.ClassDebug,
			wantPosture:   intent.PostureRepairRecovery,
			wantReadiness: intent.ReadinessRepairRecovery,
			wantClar:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := compiler.Compile(intent.CompileInput{
				TaskID:        "tsk_intent_compiler_v1",
				LatestMessage: tc.message,
				RecentMessages: []string{
					tc.message,
				},
				CurrentGoal: "Compile intent conservatively",
			})
			if err != nil {
				t.Fatalf("compile intent: %v", err)
			}
			if st.Class != tc.wantClass {
				t.Fatalf("expected class %s, got %s", tc.wantClass, st.Class)
			}
			if st.Posture != tc.wantPosture {
				t.Fatalf("expected posture %s, got %s", tc.wantPosture, st.Posture)
			}
			if st.ExecutionReadiness != tc.wantReadiness {
				t.Fatalf("expected readiness %s, got %s", tc.wantReadiness, st.ExecutionReadiness)
			}
			if st.RequiresClarification != tc.wantClar {
				t.Fatalf("expected requires clarification=%t, got %t", tc.wantClar, st.RequiresClarification)
			}
		})
	}
}

func TestIntentCompilerV1ExtractsConstraintsDoneCriteriaAndClarifications(t *testing.T) {
	compiler := NewIntentCompilerV1()
	message := strings.Join([]string{
		"Implement internal/orchestrator/service.go intent projection updates.",
		"- Do not redesign Tuku.",
		"- done when go test ./... passes.",
		"- success when status and inspect surface compiled intent.",
		"- must include task.intent route wiring.",
	}, "\n")

	st, err := compiler.Compile(intent.CompileInput{
		TaskID:        "tsk_extract",
		LatestMessage: message,
		RecentMessages: []string{
			message,
		},
		CurrentGoal: "Intent compiler extraction",
	})
	if err != nil {
		t.Fatalf("compile intent: %v", err)
	}
	if len(st.ExplicitConstraints) == 0 || !strings.Contains(strings.ToLower(strings.Join(st.ExplicitConstraints, " ")), "do not redesign tuku") {
		t.Fatalf("expected explicit constraints extraction, got %+v", st.ExplicitConstraints)
	}
	if len(st.DoneCriteria) == 0 {
		t.Fatalf("expected done criteria extraction, got %+v", st.DoneCriteria)
	}
	joinedDone := strings.ToLower(strings.Join(st.DoneCriteria, " | "))
	if !strings.Contains(joinedDone, "done when") || !strings.Contains(joinedDone, "success when") || !strings.Contains(joinedDone, "must include") {
		t.Fatalf("expected done criteria markers, got %+v", st.DoneCriteria)
	}
	if st.BoundedEvidenceMessages <= 0 {
		t.Fatalf("expected bounded evidence message count, got %d", st.BoundedEvidenceMessages)
	}
}

func TestReadCompiledIntentProjectionConsistencyAcrossStatusInspectShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	if _, err := coord.MessageTask(context.Background(), string(taskID), strings.Join([]string{
		"Implement internal/orchestrator/compiled_intent.go updates.",
		"Do not widen authority semantics.",
		"done when go test ./... passes.",
	}, "\n")); err != nil {
		t.Fatalf("message task: %v", err)
	}

	readOut, err := coord.ReadCompiledIntent(context.Background(), ReadCompiledIntentRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("read compiled intent: %v", err)
	}
	if readOut.CompiledIntent == nil {
		t.Fatal("expected compiled intent summary from dedicated read")
	}

	statusOut, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	inspectOut, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	shellOut, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}

	for _, item := range []struct {
		label   string
		summary *CompiledIntentSummary
	}{
		{label: "status", summary: statusOut.CompiledIntent},
		{label: "inspect", summary: inspectOut.CompiledIntent},
		{label: "shell", summary: shellOut.CompiledIntent},
	} {
		if item.summary == nil {
			t.Fatalf("%s: expected compiled intent projection", item.label)
		}
		if item.summary.IntentID != readOut.CompiledIntent.IntentID {
			t.Fatalf("%s: expected intent id %s, got %+v", item.label, readOut.CompiledIntent.IntentID, item.summary)
		}
		if item.summary.Posture != readOut.CompiledIntent.Posture || item.summary.ExecutionReadiness != readOut.CompiledIntent.ExecutionReadiness {
			t.Fatalf("%s: expected posture/readiness %s/%s, got %+v", item.label, readOut.CompiledIntent.Posture, readOut.CompiledIntent.ExecutionReadiness, item.summary)
		}
		if strings.TrimSpace(item.summary.Digest) == "" || !strings.Contains(strings.ToLower(item.summary.Advisory), "bounded") {
			t.Fatalf("%s: expected bounded digest/advisory cues, got %+v", item.label, item.summary)
		}
	}
}

func TestReadCompiledIntentRejectsInvalidRequest(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())

	if _, err := coord.ReadCompiledIntent(context.Background(), ReadCompiledIntentRequest{TaskID: ""}); err == nil || !strings.Contains(err.Error(), "task id is required") {
		t.Fatalf("expected task id validation error, got %v", err)
	}
}
