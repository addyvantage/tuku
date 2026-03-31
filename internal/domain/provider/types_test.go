package provider

import "testing"

func TestRecommendPrefersCodexForExecutionReadyImplementation(t *testing.T) {
	rec := Recommend(Signals{
		NormalizedTaskType:    "BUG_FIX",
		BriefPosture:          "EXECUTION_READY",
		ValidatorCount:        2,
		RankedTargetCount:     3,
		EstimatedTokenSavings: 320,
		ConfidenceLevel:       "high",
	})
	if rec.Worker != WorkerCodex {
		t.Fatalf("expected Codex recommendation, got %+v", rec)
	}
	if rec.Confidence == "" || rec.Reason == "" {
		t.Fatalf("expected explainable codex recommendation, got %+v", rec)
	}
}

func TestRecommendPrefersClaudeForPlanningAndClarification(t *testing.T) {
	rec := Recommend(Signals{
		NormalizedTaskType:    "PLAN",
		BriefPosture:          "PLANNING_ORIENTED",
		RequiresClarification: true,
		ConfidenceLevel:       "low",
	})
	if rec.Worker != WorkerClaude {
		t.Fatalf("expected Claude recommendation, got %+v", rec)
	}
}

func TestRecommendPreservesActiveHandoffContinuity(t *testing.T) {
	rec := Recommend(Signals{
		NormalizedTaskType: "BUG_FIX",
		BriefPosture:       "EXECUTION_READY",
		HandoffTarget:      WorkerClaude,
		HandoffStatus:      "ACCEPTED",
	})
	if rec.Worker != WorkerClaude {
		t.Fatalf("expected Claude recommendation from active handoff continuity, got %+v", rec)
	}
}
