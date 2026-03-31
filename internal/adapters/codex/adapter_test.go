package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/taskmemory"
	"tuku/internal/runtime/process"
)

type fakeRunner struct {
	spec   process.Spec
	result process.Result
	err    error
	called bool
}

func (r *fakeRunner) Run(_ context.Context, spec process.Spec) (process.Result, error) {
	r.called = true
	r.spec = spec
	return r.result, r.err
}

type sideEffectRunner struct {
	onRun  func(spec process.Spec) error
	result process.Result
}

func (r *sideEffectRunner) Run(_ context.Context, spec process.Spec) (process.Result, error) {
	if r.onRun != nil {
		if err := r.onRun(spec); err != nil {
			return process.Result{}, err
		}
	}
	return r.result, nil
}

func TestExecuteMissingBinaryFailsCleanly(t *testing.T) {
	runner := &fakeRunner{}
	adapter := NewAdapterWithConfig(Config{
		Binary:  "__tuku_codex_missing_binary__",
		Timeout: 5 * time.Second,
		Runner:  runner,
	})

	res, err := adapter.Execute(context.Background(), testExecutionRequest(t), nil)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("expected not found error, got: %v", err)
	}
	if res.ErrorMessage == "" {
		t.Fatal("expected error message in execution result")
	}
	if runner.called {
		t.Fatal("runner should not be called when binary is missing")
	}
}

func TestExecuteCapturesRunnerResultAndPrompt(t *testing.T) {
	runner := &fakeRunner{result: process.Result{
		ExitCode: 0,
		Stdout:   "TUKU_SUMMARY_JSON:{\"result\":\"ok\"}\nworker said tests passed",
		Stderr:   "",
	}}
	adapter := NewAdapterWithConfig(Config{
		Binary:  "sh",
		Args:    []string{"-lc", "cat >/dev/null"},
		Timeout: 5 * time.Second,
		Runner:  runner,
	})

	req := testExecutionRequest(t)
	res, err := adapter.Execute(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	if !runner.called {
		t.Fatal("expected runner to be called")
	}
	if runner.spec.WorkingDir != req.RepoAnchor.WorktreePath {
		t.Fatalf("expected working dir %q, got %q", req.RepoAnchor.WorktreePath, runner.spec.WorkingDir)
	}
	if got := strings.Join(runner.spec.Args, " "); got != "-c model_reasoning_effort=\"high\" -lc cat >/dev/null exec --color never -" {
		t.Fatalf("expected codex exec args to be appended for non-interactive runs, got %q", got)
	}
	if !strings.Contains(runner.spec.Stdin, "Task ID: tsk_test") {
		t.Fatalf("expected bounded prompt to include task id, got: %q", runner.spec.Stdin)
	}
	if !strings.Contains(runner.spec.Stdin, "Objective: Implement bounded change") {
		t.Fatalf("expected bounded prompt to include objective, got: %q", runner.spec.Stdin)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.ExitCode)
	}
	if res.Command != "sh" {
		t.Fatalf("expected command sh, got %q", res.Command)
	}
	if got := strings.Join(res.Args, " "); got != "-c model_reasoning_effort=\"high\" -lc cat >/dev/null exec --color never -" {
		t.Fatalf("expected execution result args to reflect codex exec invocation, got %q", got)
	}
	if res.Summary == "" {
		t.Fatal("expected summary in execution result")
	}
	if res.StructuredSummary == "" {
		t.Fatal("expected structured summary from stdout marker")
	}
	if res.ChangedFilesSemantics == "" {
		t.Fatal("expected changed-file semantics to be populated")
	}
}

func TestBuildPromptIncludesPromptTriageEvidence(t *testing.T) {
	req := testExecutionRequest(t)
	req.Brief.PromptTriage = brief.PromptTriage{
		Applied:                      true,
		Summary:                      "searched 18 repo-local file(s) and narrowed repair context to 2 ranked candidate(s)",
		SearchTerms:                  []string{"ui", "component", "page"},
		CandidateFiles:               []string{"web/src/pages/Dashboard.tsx", "web/src/components/ProfileCard.tsx"},
		RawPromptTokenEstimate:       4,
		RewrittenPromptTokenEstimate: 29,
		SearchSpaceTokenEstimate:     540,
		SelectedContextTokenEstimate: 160,
		ContextTokenSavingsEstimate:  380,
	}

	prompt := buildPrompt(req)
	if !strings.Contains(prompt, "Prompt triage: searched 18 repo-local file(s) and narrowed repair context to 2 ranked candidate(s)") {
		t.Fatalf("expected prompt triage summary in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Prompt triage candidates: web/src/pages/Dashboard.tsx, web/src/components/ProfileCard.tsx") {
		t.Fatalf("expected prompt triage candidates in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Prompt triage token estimates: raw=4 rewritten=29 search_space=540 selected_context=160 savings=380") {
		t.Fatalf("expected prompt triage token estimates in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Task memory compaction: full_history_tokens=320 resume_prompt_tokens=96 ratio=3.33") {
		t.Fatalf("expected task memory compaction metrics in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Task memory facts: brief posture: EXECUTION_READY | normalized action: apply brief") {
		t.Fatalf("expected task memory facts in prompt, got %q", prompt)
	}
}

func TestBuildPromptIncludesPromptIREvidence(t *testing.T) {
	req := testExecutionRequest(t)
	req.Brief.PromptIR = promptir.Packet{
		Version:            1,
		NormalizedTaskType: "BUG_FIX",
		Objective:          "Implement bounded change",
		Operation:          "apply brief",
		RankedTargets: []promptir.Target{
			{Path: "internal/orchestrator/service.go", Kind: promptir.TargetFile, Score: 100},
			{Path: "internal/orchestrator/compiled_brief.go", Kind: promptir.TargetFile, Score: 95},
		},
		OperationPlan:         []string{"start from ranked targets", "avoid unrelated refactors"},
		ValidatorPlan:         promptir.ValidatorPlan{Commands: []string{"go test ./internal/orchestrator", "gofmt -l internal/orchestrator/service.go"}},
		Confidence:            promptir.ConfidenceScore{Level: "high", Value: 0.84, Reason: "repo triage and task memory agree"},
		DefaultSerializer:     promptir.SerializerNaturalLanguage,
		NaturalLanguageTokens: 110,
		StructuredTokens:      98,
		StructuredCheaper:     true,
	}

	prompt := buildPrompt(req)
	if !strings.Contains(prompt, "Prompt IR task type: BUG_FIX") {
		t.Fatalf("expected prompt ir task type in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Prompt IR targets: internal/orchestrator/service.go, internal/orchestrator/compiled_brief.go") {
		t.Fatalf("expected prompt ir targets in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Prompt IR validators: go test ./internal/orchestrator | gofmt -l internal/orchestrator/service.go") {
		t.Fatalf("expected prompt ir validators in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Prompt IR serializer benchmark: default=natural_language natural=110 structured=98 structured_cheaper=true") {
		t.Fatalf("expected prompt ir serializer benchmark in prompt, got %q", prompt)
	}
}

func TestExecutePreservesExplicitSubcommandArgs(t *testing.T) {
	runner := &fakeRunner{result: process.Result{ExitCode: 0, Stdout: "ok"}}
	adapter := NewAdapterWithConfig(Config{
		Binary:  "sh",
		Args:    []string{"exec", "--json", "-"},
		Timeout: 5 * time.Second,
		Runner:  runner,
	})

	if _, err := adapter.Execute(context.Background(), testExecutionRequest(t), nil); err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	if got := strings.Join(runner.spec.Args, " "); got != "-c model_reasoning_effort=\"high\" exec --json -" {
		t.Fatalf("expected explicit subcommand args to remain unchanged, got %q", got)
	}
}

func TestCommandArgsDoesNotDuplicateReasoningEffortOverride(t *testing.T) {
	adapter := NewAdapterWithConfig(Config{
		Binary: "sh",
		Args:   []string{"-c", `model_reasoning_effort="medium"`, "exec", "--json", "-"},
		Runner: &fakeRunner{},
	})

	got := strings.Join(adapter.commandArgs(), " ")
	if strings.Count(got, "model_reasoning_effort") != 1 {
		t.Fatalf("expected a single reasoning override, got %q", got)
	}
	if !strings.Contains(got, `model_reasoning_effort="medium"`) {
		t.Fatalf("expected explicit reasoning override to survive, got %q", got)
	}
}

func TestExecuteRunnerErrorPropagates(t *testing.T) {
	runner := &fakeRunner{
		result: process.Result{ExitCode: 2, Stdout: "", Stderr: "runner failure"},
		err:    errors.New("runner boom"),
	}
	adapter := NewAdapterWithConfig(Config{
		Binary:  "sh",
		Timeout: 5 * time.Second,
		Runner:  runner,
	})

	res, err := adapter.Execute(context.Background(), testExecutionRequest(t), nil)
	if err == nil {
		t.Fatal("expected execute error")
	}
	if !strings.Contains(err.Error(), "runner boom") {
		t.Fatalf("expected runner boom error, got %v", err)
	}
	if res.ErrorMessage == "" {
		t.Fatal("expected execution result to retain error message")
	}
}

func TestExecuteChangedFilesHintUsesPrePostDelta(t *testing.T) {
	repo := t.TempDir()
	mustRun(t, repo, "git", "init")
	if err := os.WriteFile(filepath.Join(repo, "pre_existing.txt"), []byte("pre"), 0o644); err != nil {
		t.Fatalf("write pre-existing file: %v", err)
	}

	runner := &sideEffectRunner{
		onRun: func(spec process.Spec) error {
			return os.WriteFile(filepath.Join(spec.WorkingDir, "new_by_run.txt"), []byte("new"), 0o644)
		},
		result: process.Result{ExitCode: 0, Stdout: "ok", Stderr: ""},
	}
	adapter := NewAdapterWithConfig(Config{
		Binary:  "sh",
		Timeout: 5 * time.Second,
		Runner:  runner,
	})

	req := testExecutionRequestWithRepo(t, repo)
	res, err := adapter.Execute(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("execute with changed-file side effect: %v", err)
	}
	if len(res.ChangedFiles) != 1 || res.ChangedFiles[0] != "new_by_run.txt" {
		t.Fatalf("expected only new file hint, got %#v", res.ChangedFiles)
	}
	if !strings.Contains(res.ChangedFilesSemantics, "newly dirty") {
		t.Fatalf("unexpected changed-file semantics: %s", res.ChangedFilesSemantics)
	}
}

func testExecutionRequest(t *testing.T) adapter_contract.ExecutionRequest {
	t.Helper()
	return testExecutionRequestWithRepo(t, t.TempDir())
}

func testExecutionRequestWithRepo(t *testing.T, repo string) adapter_contract.ExecutionRequest {
	t.Helper()
	now := time.Now().UTC()
	return adapter_contract.ExecutionRequest{
		RunID:  common.RunID("run_test"),
		TaskID: common.TaskID("tsk_test"),
		Worker: adapter_contract.WorkerCodex,
		Brief: brief.ExecutionBrief{
			Version:          1,
			BriefID:          common.BriefID("brf_test"),
			TaskID:           common.TaskID("tsk_test"),
			IntentID:         common.IntentID("int_test"),
			CapsuleVersion:   1,
			CreatedAt:        now,
			Objective:        "Implement bounded change",
			NormalizedAction: "apply brief",
			ScopeIn:          []string{"internal/orchestrator/service.go"},
			ScopeOut:         []string{"web"},
			Constraints:      []string{"keep changes minimal"},
			DoneCriteria:     []string{"code compiles"},
			ContextPackID:    common.ContextPackID("ctx_test"),
			Verbosity:        brief.VerbosityStandard,
			PolicyProfileID:  "default-safe-v1",
			BriefHash:        "hash_test",
		},
		ContextPack: contextdomain.Pack{
			ContextPackID:      common.ContextPackID("ctx_test"),
			TaskID:             common.TaskID("tsk_test"),
			Mode:               contextdomain.ModeCompact,
			TokenBudget:        1200,
			RepoAnchorHash:     "head123",
			FreshnessState:     "current",
			IncludedFiles:      []string{"internal/orchestrator/service.go"},
			IncludedSnippets:   []contextdomain.Snippet{},
			SelectionRationale: []string{"test rationale"},
			PackHash:           "pack_hash",
			CreatedAt:          now,
		},
		TaskMemory: taskmemory.Snapshot{
			Version:                   1,
			MemoryID:                  common.MemoryID("mem_test"),
			TaskID:                    common.TaskID("tsk_test"),
			BriefID:                   common.BriefID("brf_test"),
			Source:                    "brief_compiled",
			Summary:                   "phase=BRIEF_READY; action=apply brief; files=internal/orchestrator/service.go; next=run bounded implementation",
			ConfirmedFacts:            []string{"brief posture: EXECUTION_READY", "normalized action: apply brief"},
			Unknowns:                  []string{"runtime verification remains pending"},
			CandidateFiles:            []string{"internal/orchestrator/service.go"},
			NextSuggestedStep:         "run bounded implementation",
			FullHistoryTokenEstimate:  320,
			ResumePromptTokenEstimate: 96,
			MemoryCompactionRatio:     3.33,
			CreatedAt:                 now,
		},
		RepoAnchor: checkpoint.RepoAnchor{
			RepoRoot:      repo,
			WorktreePath:  repo,
			BranchName:    "main",
			HeadSHA:       "head123",
			DirtyHash:     "clean",
			UntrackedHash: "",
		},
		PolicyProfileID:    "default-safe-v1",
		AgentsChecksum:     "agents123",
		AgentsInstructions: "Tuku owns canonical response",
		ContextSummary:     "context ready",
	}
}

func mustRun(t *testing.T, dir string, cmd string, args ...string) {
	t.Helper()
	c := exec.Command(cmd, args...)
	if dir != "" {
		c.Dir = dir
	}
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("command %s %v failed: %v output=%s", cmd, args, err, string(out))
	}
}
