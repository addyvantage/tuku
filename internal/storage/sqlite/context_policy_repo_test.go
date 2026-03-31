package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	contextdomain "tuku/internal/domain/context"
	"tuku/internal/domain/policy"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/taskmemory"
)

func TestContextPackAndPolicyDecisionReposRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "context-policy.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	now := time.Unix(1713700000, 0).UTC()
	pack := contextdomain.Pack{
		ContextPackID:      common.ContextPackID("ctx_roundtrip"),
		TaskID:             common.TaskID("tsk_roundtrip"),
		Mode:               contextdomain.ModeStandard,
		TokenBudget:        2400,
		RepoAnchorHash:     "abc123",
		FreshnessState:     "current",
		IncludedFiles:      []string{"README.md", "internal/orchestrator/service.go"},
		IncludedSnippets:   []contextdomain.Snippet{{Path: "README.md", StartLine: 1, EndLine: 3, Content: "# Tuku\n"}},
		SelectionRationale: []string{"selected repo orientation and current execution file"},
		PackHash:           "hash_ctx",
		CreatedAt:          now,
	}
	if err := store.ContextPacks().Save(pack); err != nil {
		t.Fatalf("save context pack: %v", err)
	}

	gotPack, err := store.ContextPacks().Get(pack.ContextPackID)
	if err != nil {
		t.Fatalf("get context pack: %v", err)
	}
	if gotPack.ContextPackID != pack.ContextPackID || gotPack.PackHash != pack.PackHash || len(gotPack.IncludedSnippets) != 1 {
		t.Fatalf("unexpected context pack round-trip: %+v", gotPack)
	}

	resolvedAt := now.Add(30 * time.Second)
	decision := policy.Decision{
		DecisionID:      common.DecisionID("pdec_roundtrip"),
		TaskID:          pack.TaskID,
		OperationType:   "task.run.start",
		RiskLevel:       policy.RiskMedium,
		RequestedAt:     now,
		ResolvedAt:      &resolvedAt,
		ResolvedBy:      "tuku-policy-v1",
		Status:          policy.DecisionApproved,
		Reason:          "approved for bounded local execution",
		ScopeDescriptor: "local execution",
	}
	if err := store.PolicyDecisions().Save(decision); err != nil {
		t.Fatalf("save policy decision: %v", err)
	}

	gotDecision, err := store.PolicyDecisions().Get(decision.DecisionID)
	if err != nil {
		t.Fatalf("get policy decision: %v", err)
	}
	if gotDecision.DecisionID != decision.DecisionID || gotDecision.Status != decision.Status || gotDecision.ResolvedAt == nil {
		t.Fatalf("unexpected policy decision round-trip: %+v", gotDecision)
	}
}

func TestExecutionBriefPromptTriageRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "brief-prompt-triage.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	now := time.Unix(1713700300, 0).UTC()
	b := brief.ExecutionBrief{
		Version:          2,
		BriefID:          common.BriefID("brf_prompt_triage"),
		TaskID:           common.TaskID("tsk_prompt_triage"),
		IntentID:         common.IntentID("int_prompt_triage"),
		CapsuleVersion:   4,
		CreatedAt:        now,
		Posture:          brief.PostureExecutionReady,
		Objective:        "Repair the UI defect",
		NormalizedAction: "investigate and repair the issue in web/src/pages/Dashboard.tsx and web/src/components/ProfileCard.tsx",
		ScopeSummary:     "bounded repair scope inferred from repo-local triage: web/src/pages/Dashboard.tsx, web/src/components/ProfileCard.tsx",
		ScopeIn:          []string{"web/src/pages/Dashboard.tsx", "web/src/components/ProfileCard.tsx"},
		Constraints:      []string{"Keep the fix bounded."},
		DoneCriteria:     []string{"UI defect is fixed with bounded evidence."},
		WorkerFraming:    "Execution-ready brief with prompt triage guidance.",
		PromptTriage: brief.PromptTriage{
			Applied:                      true,
			Reason:                       "scope_not_explicit",
			Summary:                      "searched 8 repo-local file(s) and narrowed repair context to 2 ranked candidate(s)",
			SearchTerms:                  []string{"ui", "component", "page"},
			CandidateFiles:               []string{"web/src/pages/Dashboard.tsx", "web/src/components/ProfileCard.tsx"},
			FilesScanned:                 8,
			RawPromptTokenEstimate:       4,
			RewrittenPromptTokenEstimate: 38,
			SearchSpaceTokenEstimate:     520,
			SelectedContextTokenEstimate: 140,
			ContextTokenSavingsEstimate:  380,
		},
		ContextPackID: common.ContextPackID("ctx_prompt_triage"),
		PromptIR: promptir.Packet{
			Version:            1,
			NormalizedTaskType: "BUG_FIX",
			Objective:          "Repair the UI defect",
			Operation:          "investigate and repair",
			ScopeSummary:       "bounded repair scope inferred from repo-local triage",
			RankedTargets: []promptir.Target{
				{Path: "web/src/pages/Dashboard.tsx", Kind: promptir.TargetComponent, Score: 100},
				{Path: "web/src/components/ProfileCard.tsx", Kind: promptir.TargetComponent, Score: 95},
			},
			OperationPlan:         []string{"start from ranked targets", "run frontend validation"},
			ValidatorPlan:         promptir.ValidatorPlan{Summary: "run cheapest sufficient frontend checks", Commands: []string{"npm test -- Dashboard", "npm run lint -- Dashboard.tsx"}},
			Confidence:            promptir.ConfidenceScore{Value: 0.82, Level: "high", Reason: "repo triage and bounded scope aligned"},
			NaturalLanguageTokens: 116,
			StructuredTokens:      101,
			StructuredCheaper:     true,
			DefaultSerializer:     promptir.SerializerNaturalLanguage,
		},
		BenchmarkID:     common.BenchmarkID("bmk_prompt_triage"),
		Verbosity:       brief.VerbosityStandard,
		PolicyProfileID: "default-safe-v1",
		BriefHash:       "hash_prompt_triage",
	}
	if err := store.Briefs().Save(b); err != nil {
		t.Fatalf("save brief: %v", err)
	}

	got, err := store.Briefs().Get(b.BriefID)
	if err != nil {
		t.Fatalf("get brief: %v", err)
	}
	if !got.PromptTriage.Applied || got.PromptTriage.FilesScanned != 8 || got.PromptTriage.ContextTokenSavingsEstimate != 380 {
		t.Fatalf("unexpected prompt triage round-trip: %+v", got.PromptTriage)
	}
	if len(got.PromptTriage.CandidateFiles) != 2 {
		t.Fatalf("expected candidate files in prompt triage round-trip, got %+v", got.PromptTriage)
	}
	if got.BenchmarkID != b.BenchmarkID || got.PromptIR.Confidence.Level != "high" || len(got.PromptIR.RankedTargets) != 2 {
		t.Fatalf("unexpected prompt ir / benchmark round-trip: %+v", got)
	}
}

func TestBenchmarkRepoRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "benchmark.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	now := time.Unix(1713700400, 0).UTC()
	record := benchmark.Run{
		Version:                       1,
		BenchmarkID:                   common.BenchmarkID("bmk_roundtrip"),
		TaskID:                        common.TaskID("tsk_roundtrip"),
		BriefID:                       common.BriefID("brf_roundtrip"),
		RunID:                         common.RunID("run_roundtrip"),
		Source:                        "brief_compiled",
		RawPromptTokenEstimate:        6,
		DispatchPromptTokenEstimate:   114,
		StructuredPromptTokenEstimate: 96,
		SelectedContextTokenEstimate:  140,
		EstimatedTokenSavings:         380,
		FilesScanned:                  21,
		RankedTargetCount:             4,
		CandidateRecallAt3:            0.67,
		StructuredCheaper:             true,
		DefaultSerializer:             "natural_language",
		ConfidenceValue:               0.82,
		ConfidenceLevel:               "high",
		Summary:                       "ranked 4 targets and saved 380 tokens",
		ChangedFiles:                  []string{"web/src/pages/Dashboard.tsx"},
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}
	if err := store.Benchmarks().Save(record); err != nil {
		t.Fatalf("save benchmark: %v", err)
	}

	got, err := store.Benchmarks().Get(record.BenchmarkID)
	if err != nil {
		t.Fatalf("get benchmark: %v", err)
	}
	if got.BenchmarkID != record.BenchmarkID || got.EstimatedTokenSavings != record.EstimatedTokenSavings || len(got.ChangedFiles) != 1 {
		t.Fatalf("unexpected benchmark round-trip: %+v", got)
	}
	latest, err := store.Benchmarks().LatestByTask(record.TaskID)
	if err != nil {
		t.Fatalf("latest benchmark by task: %v", err)
	}
	if latest.BenchmarkID != record.BenchmarkID {
		t.Fatalf("expected latest benchmark, got %+v", latest)
	}
}

func TestTaskMemoryAndBriefMemoryCompressionRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "task-memory.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	now := time.Unix(1713700600, 0).UTC()
	snapshot := taskmemory.Snapshot{
		Version:                   1,
		MemoryID:                  common.MemoryID("mem_roundtrip"),
		TaskID:                    common.TaskID("tsk_memory_roundtrip"),
		BriefID:                   common.BriefID("brf_memory_roundtrip"),
		RunID:                     common.RunID("run_memory_roundtrip"),
		Source:                    "brief_compiled",
		Summary:                   "phase=planning; action=repair ui bug; files=web/src/App.tsx; validators=gofmt -l; next=run bounded validation",
		ConfirmedFacts:            []string{"goal: fix ui bug", "ranked candidate files: web/src/App.tsx"},
		RejectedHypotheses:        []string{"server-side router bug"},
		Unknowns:                  []string{"exact reproduction path not confirmed"},
		UserConstraints:           []string{"keep the fix bounded"},
		TouchedFiles:              []string{"web/src/App.tsx"},
		ValidatorsRun:             []string{"gofmt -l"},
		CandidateFiles:            []string{"web/src/App.tsx", "web/src/Button.tsx"},
		LastBlocker:               "exact reproduction path not confirmed",
		NextSuggestedStep:         "run bounded validation",
		FullHistoryTokenEstimate:  420,
		ResumePromptTokenEstimate: 120,
		MemoryCompactionRatio:     3.5,
		CreatedAt:                 now,
	}
	if err := store.TaskMemories().Save(snapshot); err != nil {
		t.Fatalf("save task memory snapshot: %v", err)
	}

	gotSnapshot, err := store.TaskMemories().Get(snapshot.MemoryID)
	if err != nil {
		t.Fatalf("get task memory snapshot: %v", err)
	}
	if gotSnapshot.MemoryID != snapshot.MemoryID || gotSnapshot.Summary != snapshot.Summary || gotSnapshot.MemoryCompactionRatio != snapshot.MemoryCompactionRatio {
		t.Fatalf("unexpected task memory round-trip: %+v", gotSnapshot)
	}
	if len(gotSnapshot.ConfirmedFacts) != 2 || len(gotSnapshot.CandidateFiles) != 2 || gotSnapshot.NextSuggestedStep != snapshot.NextSuggestedStep {
		t.Fatalf("unexpected task memory structured fields: %+v", gotSnapshot)
	}

	b := brief.ExecutionBrief{
		Version:          2,
		BriefID:          snapshot.BriefID,
		TaskID:           snapshot.TaskID,
		IntentID:         common.IntentID("int_memory_roundtrip"),
		CapsuleVersion:   5,
		CreatedAt:        now,
		Posture:          brief.PostureExecutionReady,
		Objective:        "Repair the UI defect",
		NormalizedAction: "repair the bug in web/src/App.tsx",
		ScopeSummary:     "bounded repair scope inferred from durable task memory",
		ScopeIn:          []string{"web/src/App.tsx"},
		Constraints:      []string{"Keep the fix bounded."},
		DoneCriteria:     []string{"UI defect is fixed with bounded evidence."},
		WorkerFraming:    "Execution-ready brief with durable task memory.",
		TaskMemoryID:     snapshot.MemoryID,
		MemoryCompression: brief.MemoryCompression{
			Applied:                   true,
			Summary:                   snapshot.Summary,
			FullHistoryTokenEstimate:  snapshot.FullHistoryTokenEstimate,
			ResumePromptTokenEstimate: snapshot.ResumePromptTokenEstimate,
			MemoryCompactionRatio:     snapshot.MemoryCompactionRatio,
			ConfirmedFactsCount:       len(snapshot.ConfirmedFacts),
			TouchedFilesCount:         len(snapshot.TouchedFiles),
			ValidatorsRunCount:        len(snapshot.ValidatorsRun),
			CandidateFilesCount:       len(snapshot.CandidateFiles),
			RejectedHypothesesCount:   len(snapshot.RejectedHypotheses),
			UnknownsCount:             len(snapshot.Unknowns),
		},
		ContextPackID:   common.ContextPackID("ctx_memory_roundtrip"),
		Verbosity:       brief.VerbosityStandard,
		PolicyProfileID: "default-safe-v1",
		BriefHash:       "hash_memory_roundtrip",
	}
	if err := store.Briefs().Save(b); err != nil {
		t.Fatalf("save brief with task memory: %v", err)
	}

	gotBrief, err := store.Briefs().Get(b.BriefID)
	if err != nil {
		t.Fatalf("get brief with task memory: %v", err)
	}
	if gotBrief.TaskMemoryID != snapshot.MemoryID {
		t.Fatalf("expected task memory id round-trip, got %+v", gotBrief)
	}
	if !gotBrief.MemoryCompression.Applied || gotBrief.MemoryCompression.ResumePromptTokenEstimate != 120 || gotBrief.MemoryCompression.CandidateFilesCount != 2 {
		t.Fatalf("unexpected memory compression round-trip: %+v", gotBrief.MemoryCompression)
	}

	latestSnapshot, err := store.TaskMemories().LatestByTask(snapshot.TaskID)
	if err != nil {
		t.Fatalf("latest task memory by task: %v", err)
	}
	if latestSnapshot.MemoryID != snapshot.MemoryID {
		t.Fatalf("expected latest task memory snapshot, got %+v", latestSnapshot)
	}
}
