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
