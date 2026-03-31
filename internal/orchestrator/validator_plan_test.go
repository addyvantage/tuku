package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/repoindex"
)

func TestPlannedValidatorPlanIncludesRepoCheckAndTypeScriptValidator(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "tsconfig.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules/.bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "node_modules", ".bin", "tsc"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tsc shim: %v", err)
	}

	plan := plannedValidatorPlan(repoRoot, brief.PostureExecutionReady, repoindex.Snapshot{}, []promptir.Target{
		{Path: "web/src/pages/Landing.tsx", Kind: promptir.TargetComponent},
	})

	if !containsFold(plan.Commands, "git diff --check") {
		t.Fatalf("expected git diff validator in %v", plan.Commands)
	}
	if !containsSubstring(plan.Commands, "tsc --noemit --pretty false") {
		t.Fatalf("expected typescript validator in %v", plan.Commands)
	}
}

func TestPlannedValidatorPlanUsesRelatedFrontendTestsFromRepoIndex(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "vitest.config.ts"), []byte("export default {}\n"), 0o644); err != nil {
		t.Fatalf("write vitest config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules/.bin: %v", err)
	}
	localVitest := filepath.Join(repoRoot, "node_modules", ".bin", "vitest")
	if err := os.WriteFile(localVitest, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write vitest shim: %v", err)
	}

	index := repoindex.Snapshot{
		Files: []repoindex.File{
			{Path: "web/src/pages/Landing.tsx", Kinds: []string{"file", "component"}},
			{Path: "web/src/pages/Landing.test.tsx", Kinds: []string{"file", "test"}},
		},
	}
	deriveRepoIndexCounts(&index)

	plan := plannedValidatorPlan(repoRoot, brief.PostureExecutionReady, index, []promptir.Target{
		{Path: "web/src/pages/Landing.tsx", Kind: promptir.TargetComponent},
	})

	if !containsFold(plan.Commands, "git diff --check") {
		t.Fatalf("expected git diff validator in %v", plan.Commands)
	}
	if !containsSubstring(plan.Commands, "vitest run web/src/pages/landing.test.tsx") {
		t.Fatalf("expected related frontend test validator in %v", plan.Commands)
	}
}

func TestValidationCommandsForRunFallsBackToPromptIRTargets(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "tsconfig.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules/.bin: %v", err)
	}
	localTSC := filepath.Join(repoRoot, "node_modules", ".bin", "tsc")
	if err := os.WriteFile(localTSC, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tsc shim: %v", err)
	}

	prepared := &preparedRealRun{
		Brief: brief.ExecutionBrief{
			Posture: brief.PostureExecutionReady,
			PromptIR: promptir.Packet{
				RankedTargets: []promptir.Target{
					{Path: "web/src/pages/Landing.tsx", Kind: promptir.TargetComponent},
				},
			},
		},
		Capsule: capsule.WorkCapsule{RepoRoot: repoRoot},
	}

	store := newTestStore(t)
	c := newTestCoordinator(t, store, defaultAnchor(), newFakeAdapterSuccess())
	specs := c.validationCommandsForRun(prepared, adapter_contract.ExecutionResult{})
	joined := make([]string, 0, len(specs))
	for _, spec := range specs {
		joined = append(joined, strings.Join(append([]string{spec.Command}, spec.Args...), " "))
	}

	if !containsFold(joined, "git diff --check") {
		t.Fatalf("expected git diff validator in %v", joined)
	}
	if !containsSubstring(joined, localTSC+" --noemit --pretty false") {
		t.Fatalf("expected prompt-ir fallback validator in %v", joined)
	}
}

func TestValidationPassedTreatsWorkerFailSignalAsFailure(t *testing.T) {
	if validationPassed([]string{"worker reported fail signal", "validation: git diff --check reported no diff hygiene issues"}) {
		t.Fatal("expected worker fail signal to make validation fail")
	}
}

func containsSubstring(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), needle) {
			return true
		}
	}
	return false
}
