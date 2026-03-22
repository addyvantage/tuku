package shell

import (
	"strings"
	"testing"
	"time"
)

func TestSuccessfulPrimaryActionResultAcceptedHandoffLaunchShowsMeaningfulLaunchedDelta(t *testing.T) {
	before := Snapshot{
		ActiveBranch:      &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		LaunchControl:     &LaunchControlSummary{State: "NOT_REQUESTED", RetryDisposition: "ALLOWED"},
		HandoffContinuity: &HandoffContinuitySummary{State: "ACCEPTED_NOT_LAUNCHED"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF", Status: "REQUIRED_NEXT"},
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			RequiredNextAction: "LAUNCH_ACCEPTED_HANDOFF",
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
	}
	after := Snapshot{
		ActiveBranch:      &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		LaunchControl:     &LaunchControlSummary{State: "COMPLETED", RetryDisposition: "BLOCKED"},
		HandoffContinuity: &HandoffContinuitySummary{State: "LAUNCH_COMPLETED_ACK_CAPTURED"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF", Status: "ALLOWED"},
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.Outcome != "SUCCESS" || result.Summary != "executed launch accepted handoff" {
		t.Fatalf("unexpected launch result summary: %+v", result)
	}
	if result.NextStep != "allowed resolve active handoff" {
		t.Fatalf("expected next step to move forward truthfully after launch, got %+v", result)
	}
	joined := strings.Join(result.Deltas, "\n")
	if !strings.Contains(joined, "launch not requested | retry allowed -> completed (invocation only) | retry blocked") {
		t.Fatalf("expected launch-state delta, got %+v", result.Deltas)
	}
	if !strings.Contains(joined, "handoff accepted, not launched -> launch completed, acknowledgment captured, downstream unproven") {
		t.Fatalf("expected handoff continuity delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultInterruptedResumeShowsContinueFinalizationNext(t *testing.T) {
	before := Snapshot{
		LocalResume: &LocalResumeAuthoritySummary{
			State:        "ALLOWED",
			Mode:         "RESUME_INTERRUPTED_LINEAGE",
			CheckpointID: "chk_1",
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESUME_INTERRUPTED_LINEAGE", Status: "REQUIRED_NEXT"},
		},
	}
	after := Snapshot{
		LocalResume: &LocalResumeAuthoritySummary{
			State: "NOT_APPLICABLE",
			Mode:  "FINALIZE_CONTINUE_RECOVERY",
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "FINALIZE_CONTINUE_RECOVERY", Status: "REQUIRED_NEXT"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "RESUME_INTERRUPTED_LINEAGE"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.NextStep != "required finalize continue recovery" {
		t.Fatalf("expected continue-finalization next step after interrupted resume, got %+v", result)
	}
	if !strings.Contains(strings.Join(result.Deltas, "\n"), "local resume allowed via checkpoint chk_1 -> not applicable | finalize continue first") {
		t.Fatalf("expected local-resume delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultContinueRecoveryShowsReadyNextRun(t *testing.T) {
	before := Snapshot{
		OperatorDecision: &OperatorDecisionSummary{Headline: "Continue finalization required"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "FINALIZE_CONTINUE_RECOVERY", Status: "REQUIRED_NEXT"},
		},
	}
	after := Snapshot{
		OperatorDecision: &OperatorDecisionSummary{Headline: "Local fresh run ready"},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "START_LOCAL_RUN", Status: "REQUIRED_NEXT"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "FINALIZE_CONTINUE_RECOVERY"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.NextStep != "required start local run" {
		t.Fatalf("expected ready-next-run plan after continue recovery, got %+v", result)
	}
	joined := strings.Join(result.Deltas, "\n")
	if !strings.Contains(joined, "decision Continue finalization required -> Local fresh run ready") && !strings.Contains(joined, "decision continue finalization required -> local fresh run ready") {
		t.Fatalf("expected decision delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultActiveHandoffResolutionReturnsLocalOwnership(t *testing.T) {
	before := Snapshot{
		ActiveBranch: &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF", Status: "ALLOWED"},
		},
	}
	after := Snapshot{
		ActiveBranch: &ActiveBranchSummary{Class: "LOCAL", ActionabilityAnchor: "BRIEF", ActionabilityAnchorRef: "brf_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "ALLOWED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "START_LOCAL_RUN", Status: "REQUIRED_NEXT"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if result.NextStep != "required start local run" {
		t.Fatalf("expected local next step after handoff resolution, got %+v", result)
	}
	joined := strings.Join(result.Deltas, "\n")
	if !strings.Contains(joined, "branch Claude hnd_1 -> local") {
		t.Fatalf("expected branch-owner delta, got %+v", result.Deltas)
	}
	if !strings.Contains(joined, "local message blocked -> allowed") {
		t.Fatalf("expected local-mutation delta, got %+v", result.Deltas)
	}
}

func TestSuccessfulPrimaryActionResultStaysCompact(t *testing.T) {
	before := Snapshot{
		ActiveBranch:         &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		OperatorDecision:     &OperatorDecisionSummary{Headline: "Accepted handoff launch ready"},
		LaunchControl:        &LaunchControlSummary{State: "NOT_REQUESTED", RetryDisposition: "ALLOWED"},
		HandoffContinuity:    &HandoffContinuitySummary{State: "ACCEPTED_NOT_LAUNCHED"},
		LocalResume:          &LocalResumeAuthoritySummary{State: "BLOCKED", Mode: "RESUME_INTERRUPTED_LINEAGE", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_1"},
		LocalRunFinalization: &LocalRunFinalizationSummary{State: "STALE_RECONCILIATION_REQUIRED", RunID: "run_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF", Status: "REQUIRED_NEXT"},
		},
	}
	after := Snapshot{
		ActiveBranch:         &ActiveBranchSummary{Class: "HANDOFF_CLAUDE", BranchRef: "hnd_1"},
		OperatorDecision:     &OperatorDecisionSummary{Headline: "Active Claude handoff pending"},
		LaunchControl:        &LaunchControlSummary{State: "COMPLETED", RetryDisposition: "BLOCKED"},
		HandoffContinuity:    &HandoffContinuitySummary{State: "LAUNCH_COMPLETED_ACK_CAPTURED"},
		LocalResume:          &LocalResumeAuthoritySummary{State: "BLOCKED", Mode: "RESUME_INTERRUPTED_LINEAGE", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_1"},
		LocalRunFinalization: &LocalRunFinalizationSummary{State: "STALE_RECONCILIATION_REQUIRED", RunID: "run_1"},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "RESOLVE_ACTIVE_HANDOFF", Status: "ALLOWED"},
		},
	}

	result := successfulPrimaryActionResult(OperatorExecutionStep{Action: "LAUNCH_ACCEPTED_HANDOFF"}, before, after, OperatorStepReceiptSummary{}, time.Unix(1710000000, 0).UTC())
	if len(result.Deltas) == 0 || len(result.Deltas) > 4 {
		t.Fatalf("expected compact delta summary, got %+v", result.Deltas)
	}
}
