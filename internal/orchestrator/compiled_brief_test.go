package orchestrator

import (
	"context"
	"strings"
	"testing"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	"tuku/internal/domain/intent"
)

func TestReadGeneratedBriefRejectsMissingTaskID(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	if _, err := coord.ReadGeneratedBrief(context.Background(), ReadGeneratedBriefRequest{}); err == nil {
		t.Fatal("expected missing task id rejection")
	}
}

func TestDeriveBriefPostureFromIntentReadinessMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   intent.State
		want brief.Posture
	}{
		{
			name: "clarification required",
			in: intent.State{
				RequiresClarification: true,
				ExecutionReadiness:    intent.ReadinessClarificationNeeded,
			},
			want: brief.PostureClarificationNeeded,
		},
		{
			name: "planning",
			in: intent.State{
				ExecutionReadiness: intent.ReadinessPlanningInProgress,
			},
			want: brief.PosturePlanningOriented,
		},
		{
			name: "validation",
			in: intent.State{
				ExecutionReadiness: intent.ReadinessValidationFocused,
			},
			want: brief.PostureValidationOriented,
		},
		{
			name: "repair",
			in: intent.State{
				ExecutionReadiness: intent.ReadinessRepairRecovery,
			},
			want: brief.PostureRepairOriented,
		},
		{
			name: "execution ready",
			in: intent.State{
				ExecutionReadiness: intent.ReadinessExecutionReady,
			},
			want: brief.PostureExecutionReady,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := deriveBriefPostureFromIntent(tc.in)
			if got != tc.want {
				t.Fatalf("expected posture %s, got %s", tc.want, got)
			}
		})
	}
}

func TestBuildBriefInputV2DerivesRepresentativeBriefPostures(t *testing.T) {
	t.Parallel()

	caps := capsule.WorkCapsule{
		TaskID: common.TaskID("tsk_brief_posture_matrix"),
		Goal:   "Generate bounded execution brief from compiled intent",
	}

	tests := []struct {
		name                      string
		in                        intent.State
		wantPosture               brief.Posture
		wantRequiresClarification bool
		wantDefaultDoneCriterion  string
	}{
		{
			name: "exploratory ambiguous maps to clarification needed",
			in: intent.State{
				IntentID:                common.IntentID("int_exploratory"),
				Posture:                 intent.PostureExploratoryAmbiguous,
				ExecutionReadiness:      intent.ReadinessClarificationNeeded,
				Objective:               "Maybe we should explore this area first",
				NormalizedAction:        "explore and clarify bounded next step",
				AmbiguityFlags:          []string{"exploratory_language"},
				BoundedEvidenceMessages: 3,
			},
			wantPosture:               brief.PostureClarificationNeeded,
			wantRequiresClarification: true,
			wantDefaultDoneCriterion:  "Clarification questions are captured before execution claims are made.",
		},
		{
			name: "planning maps to planning-oriented brief",
			in: intent.State{
				IntentID:                common.IntentID("int_planning"),
				ExecutionReadiness:      intent.ReadinessPlanningInProgress,
				Objective:               "Plan bounded changes for orchestrator projection updates",
				NormalizedAction:        "produce bounded implementation plan",
				BoundedEvidenceMessages: 4,
			},
			wantPosture:               brief.PosturePlanningOriented,
			wantRequiresClarification: false,
			wantDefaultDoneCriterion:  "Bounded execution plan is explicit and scoped for the immediate next step.",
		},
		{
			name: "execution ready maps to execution-ready brief",
			in: intent.State{
				IntentID:                common.IntentID("int_execution"),
				ExecutionReadiness:      intent.ReadinessExecutionReady,
				Objective:               "Implement compiled brief projection in status and inspect",
				NormalizedAction:        "implement bounded brief projection updates",
				BoundedEvidenceMessages: 5,
			},
			wantPosture:               brief.PostureExecutionReady,
			wantRequiresClarification: false,
			wantDefaultDoneCriterion:  "Execution step is bounded by explicit constraints and done criteria.",
		},
		{
			name: "validation focused maps to validation-oriented brief",
			in: intent.State{
				IntentID:                common.IntentID("int_validation"),
				ExecutionReadiness:      intent.ReadinessValidationFocused,
				Objective:               "Validate status and inspect output consistency",
				NormalizedAction:        "run bounded validation pass",
				BoundedEvidenceMessages: 6,
			},
			wantPosture:               brief.PostureValidationOriented,
			wantRequiresClarification: false,
			wantDefaultDoneCriterion:  "Validation findings are recorded with bounded evidence and no overclaiming.",
		},
		{
			name: "repair recovery maps to repair-oriented brief",
			in: intent.State{
				IntentID:                common.IntentID("int_repair"),
				ExecutionReadiness:      intent.ReadinessRepairRecovery,
				Objective:               "Repair brittle mapping in daemon payload projection",
				NormalizedAction:        "perform bounded repair on daemon mapping path",
				BoundedEvidenceMessages: 6,
			},
			wantPosture:               brief.PostureRepairOriented,
			wantRequiresClarification: false,
			wantDefaultDoneCriterion:  "Repair step and bounded evidence summary are produced for the targeted issue.",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			input := buildBriefInputV2(caps, tc.in, nil, 3)
			if input.Posture != tc.wantPosture {
				t.Fatalf("expected posture %s, got %s", tc.wantPosture, input.Posture)
			}
			if input.RequiresClarification != tc.wantRequiresClarification {
				t.Fatalf("expected requires clarification=%t, got %t", tc.wantRequiresClarification, input.RequiresClarification)
			}
			if len(input.DoneCriteria) == 0 || strings.TrimSpace(input.DoneCriteria[0]) != tc.wantDefaultDoneCriterion {
				t.Fatalf("expected default done criteria %q, got %+v", tc.wantDefaultDoneCriterion, input.DoneCriteria)
			}
			if strings.TrimSpace(input.WorkerFraming) == "" {
				t.Fatal("expected non-empty worker framing")
			}
			if tc.wantRequiresClarification && len(input.ClarificationQuestions) == 0 {
				t.Fatal("expected clarification question for clarification-needed posture")
			}
		})
	}
}

func TestBuildBriefInputV2PreservesConstraintsDoneCriteriaAndAmbiguity(t *testing.T) {
	caps := capsule.WorkCapsule{
		TaskID:      common.TaskID("tsk_brief_v2"),
		Goal:        "Implement bounded brief generation",
		Constraints: []string{"Do not widen authority semantics."},
		TouchedFiles: []string{
			"internal/orchestrator/service.go",
		},
	}
	in := intent.State{
		IntentID:                common.IntentID("int_brief_v2"),
		ExecutionReadiness:      intent.ReadinessClarificationNeeded,
		Objective:               "Compile intent into bounded execution-ready brief",
		RequestedOutcome:        "Produce a conservative brief posture and worker framing",
		NormalizedAction:        "prepare bounded execution brief",
		ScopeSummary:            "bounded scope signals: internal/orchestrator/service.go",
		ExplicitConstraints:     []string{"Do not claim correctness."},
		DoneCriteria:            []string{"done when status/inspect/shell show aligned brief posture"},
		AmbiguityFlags:          []string{"scope_not_explicit"},
		ClarificationQuestions:  []string{"Which module should be changed first?"},
		RequiresClarification:   true,
		BoundedEvidenceMessages: 7,
	}

	input := buildBriefInputV2(caps, in, nil, 2)
	if input.Posture != brief.PostureClarificationNeeded {
		t.Fatalf("expected clarification-needed posture, got %s", input.Posture)
	}
	if !input.RequiresClarification {
		t.Fatal("expected requires clarification to remain true")
	}
	if len(input.Constraints) == 0 || !containsLine(input.Constraints, "Do not widen authority semantics.") || !containsLine(input.Constraints, "Do not claim correctness.") {
		t.Fatalf("expected merged constraints, got %+v", input.Constraints)
	}
	if len(input.DoneCriteria) == 0 || !containsLine(input.DoneCriteria, "done when status/inspect/shell show aligned brief posture") {
		t.Fatalf("expected done criteria carry-through, got %+v", input.DoneCriteria)
	}
	if len(input.AmbiguityFlags) == 0 || !containsLine(input.AmbiguityFlags, "scope_not_explicit") {
		t.Fatalf("expected ambiguity flags, got %+v", input.AmbiguityFlags)
	}
	if len(input.ClarificationQuestions) == 0 || !containsLine(input.ClarificationQuestions, "Which module should be changed first?") {
		t.Fatalf("expected clarification questions, got %+v", input.ClarificationQuestions)
	}
	if strings.TrimSpace(input.WorkerFraming) == "" {
		t.Fatal("expected non-empty worker framing")
	}
}

func TestBuildBriefInputV2UsesPreviousBriefFallbacksConservatively(t *testing.T) {
	t.Parallel()

	caps := capsule.WorkCapsule{
		TaskID: common.TaskID("tsk_previous_fallback"),
		Goal:   "",
	}
	previous := brief.ExecutionBrief{
		BriefID:          common.BriefID("brf_prev"),
		Objective:        "Previous bounded objective",
		RequestedOutcome: "Previous bounded outcome",
		NormalizedAction: "previous normalized action",
		ScopeSummary:     "previous bounded scope",
		ScopeIn:          []string{"internal/orchestrator/service.go"},
		ScopeOut:         []string{"internal/runtime/daemon"},
		Constraints:      []string{"Do not widen authority semantics."},
		DoneCriteria:     []string{"done when bounded projections remain aligned"},
		ContextPackID:    common.ContextPackID("ctx_prev"),
		Verbosity:        brief.VerbosityVerbose,
		PolicyProfileID:  "default-safe-v1",
	}
	in := intent.State{
		IntentID:                common.IntentID("int_fallback"),
		ExecutionReadiness:      intent.ReadinessExecutionReady,
		BoundedEvidenceMessages: 2,
	}

	input := buildBriefInputV2(caps, in, &previous, 9)
	if input.Goal != "Previous bounded objective" {
		t.Fatalf("expected previous objective fallback, got %q", input.Goal)
	}
	if input.RequestedOutcome != "Previous bounded outcome" {
		t.Fatalf("expected previous requested outcome fallback, got %q", input.RequestedOutcome)
	}
	if input.NormalizedAction != "previous normalized action" {
		t.Fatalf("expected previous normalized action fallback, got %q", input.NormalizedAction)
	}
	if input.ScopeSummary != "previous bounded scope" {
		t.Fatalf("expected previous scope summary fallback, got %q", input.ScopeSummary)
	}
	if len(input.ScopeHints) != 1 || input.ScopeHints[0] != "internal/orchestrator/service.go" {
		t.Fatalf("expected previous scope-in hints fallback, got %+v", input.ScopeHints)
	}
	if len(input.ScopeOutHints) != 1 || input.ScopeOutHints[0] != "internal/runtime/daemon" {
		t.Fatalf("expected previous scope-out hints fallback, got %+v", input.ScopeOutHints)
	}
	if len(input.Constraints) == 0 || !containsLine(input.Constraints, "Do not widen authority semantics.") {
		t.Fatalf("expected previous constraints carry-through, got %+v", input.Constraints)
	}
	if len(input.DoneCriteria) == 0 || !containsLine(input.DoneCriteria, "done when bounded projections remain aligned") {
		t.Fatalf("expected previous done criteria carry-through, got %+v", input.DoneCriteria)
	}
	if input.ContextPackID != "ctx_prev" || input.Verbosity != brief.VerbosityVerbose {
		t.Fatalf("expected previous context/verbosity carry-through, got context=%q verbosity=%q", input.ContextPackID, input.Verbosity)
	}
}

func TestCompiledBriefSummaryClarificationDigestRemainsConservative(t *testing.T) {
	t.Parallel()

	summary := compiledBriefSummaryFromBrief(brief.ExecutionBrief{
		BriefID:                common.BriefID("brf_clar"),
		IntentID:               common.IntentID("int_clar"),
		Posture:                brief.PostureExecutionReady,
		RequiresClarification:  true,
		ClarificationQuestions: []string{"Which subsystem is in scope?"},
		WorkerFraming:          "Clarification-focused brief",
	})
	if summary == nil {
		t.Fatal("expected compiled brief summary")
	}
	if summary.Posture != brief.PostureClarificationNeeded {
		t.Fatalf("expected clarification-needed normalized posture, got %s", summary.Posture)
	}
	if !strings.Contains(strings.ToLower(summary.Digest), "clarification-needed") || !strings.Contains(strings.ToLower(summary.Digest), "bounded") {
		t.Fatalf("expected conservative clarification digest, got %q", summary.Digest)
	}
	if !strings.Contains(strings.ToLower(summary.Advisory), "bounded") || !strings.Contains(strings.ToLower(summary.Advisory), "clarification") {
		t.Fatalf("expected conservative clarification advisory, got %q", summary.Advisory)
	}
	for _, forbidden := range []string{"root cause", "fixed", "safe to continue", "task complete"} {
		if strings.Contains(strings.ToLower(summary.Advisory), forbidden) {
			t.Fatalf("unexpected overclaiming advisory term %q in %q", forbidden, summary.Advisory)
		}
	}
}

func TestReadGeneratedBriefProjectionConsistencyAcrossStatusInspectShell(t *testing.T) {
	store := newTestStore(t)
	coord := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	taskID := setupTaskWithBrief(t, coord)

	briefRead, err := coord.ReadGeneratedBrief(context.Background(), ReadGeneratedBriefRequest{TaskID: string(taskID)})
	if err != nil {
		t.Fatalf("read generated brief: %v", err)
	}
	status, err := coord.StatusTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("status task: %v", err)
	}
	inspect, err := coord.InspectTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("inspect task: %v", err)
	}
	shell, err := coord.ShellSnapshotTask(context.Background(), string(taskID))
	if err != nil {
		t.Fatalf("shell snapshot task: %v", err)
	}

	if briefRead.CompiledBrief == nil || status.CompiledBrief == nil || inspect.CompiledBrief == nil || shell.Brief == nil {
		t.Fatalf("expected compiled brief projections across surfaces, got read=%+v status=%+v inspect=%+v shell=%+v", briefRead.CompiledBrief, status.CompiledBrief, inspect.CompiledBrief, shell.Brief)
	}
	if briefRead.CompiledBrief.BriefID != status.CompiledBrief.BriefID || briefRead.CompiledBrief.BriefID != inspect.CompiledBrief.BriefID {
		t.Fatalf("expected same brief id across read/status/inspect, got %s / %s / %s", briefRead.CompiledBrief.BriefID, status.CompiledBrief.BriefID, inspect.CompiledBrief.BriefID)
	}
	if briefRead.CompiledBrief.Posture != status.CompiledBrief.Posture || briefRead.CompiledBrief.Posture != inspect.CompiledBrief.Posture || briefRead.CompiledBrief.Posture != shell.Brief.Posture {
		t.Fatalf("expected same brief posture across read/status/inspect/shell, got %s / %s / %s / %s", briefRead.CompiledBrief.Posture, status.CompiledBrief.Posture, inspect.CompiledBrief.Posture, shell.Brief.Posture)
	}
	if strings.TrimSpace(status.CompiledBrief.Digest) == "" || strings.TrimSpace(inspect.CompiledBrief.Digest) == "" {
		t.Fatalf("expected digest on status/inspect compiled brief, got status=%q inspect=%q", status.CompiledBrief.Digest, inspect.CompiledBrief.Digest)
	}
	if status.RequiredNextOperatorAction == "" {
		t.Fatal("required-next operator action unexpectedly empty")
	}
}

func containsLine(lines []string, wanted string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == strings.TrimSpace(wanted) {
			return true
		}
	}
	return false
}
