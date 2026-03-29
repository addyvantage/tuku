package shell

import (
	"strings"
	"testing"
	"time"

	"tuku/internal/ipc"
)

func TestBuildViewModelReflectsSnapshotState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:    "worker pane | codex live session",
		worker:   "codex live",
		lines:    []string{"codex> hello"},
		activity: []string{"12:00:00  worker host started"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
			Width:     80,
			Height:    20,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_1234567890",
		Goal:   "Implement shell",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Repo: RepoAnchor{
			RepoRoot:         "/Users/kagaya/Desktop/Tuku",
			Branch:           "main",
			HeadSHA:          "abc123",
			WorkingTreeDirty: true,
			CapturedAt:       now,
		},
		IntentSummary: "implement: build shell",
		Brief: &BriefSummary{
			ID:               "brf_1",
			Objective:        "Build worker-native shell",
			NormalizedAction: "build-shell",
			Constraints:      []string{"keep it narrow"},
			DoneCriteria:     []string{"full-screen shell"},
		},
		Run: &RunSummary{
			ID:               "run_1",
			WorkerKind:       "codex",
			Status:           "RUNNING",
			LastKnownSummary: "applying shell patch",
			StartedAt:        now,
		},
		Checkpoint: &CheckpointSummary{
			ID:               "chk_1",
			Trigger:          "CONTINUE",
			CreatedAt:        now,
			ResumeDescriptor: "resume from shell-ready checkpoint",
			IsResumable:      true,
		},
		Recovery: &RecoverySummary{
			Class:           "READY_NEXT_RUN",
			Action:          "START_NEXT_RUN",
			ReadyForNextRun: true,
			Reason:          "task is ready for the next bounded run with brief brf_1",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
			CreatedAt:    now,
		},
		Acknowledgment: &AcknowledgmentSummary{
			Status:    "CAPTURED",
			Summary:   "Claude acknowledged the handoff packet.",
			CreatedAt: now,
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:                        "LAUNCH_COMPLETED_ACK_CAPTURED",
			Reason:                       "Claude handoff launch completed and initial acknowledgment was captured; downstream continuation remains unproven",
			LaunchID:                     "hlc_1",
			AcknowledgmentStatus:         "CAPTURED",
			AcknowledgmentSummary:        "Claude acknowledged the handoff packet.",
			DownstreamContinuationProven: false,
		},
		RecentProofs: []ProofItem{
			{ID: "evt_1", Type: "BRIEF_CREATED", Summary: "Execution brief updated", Timestamp: now},
			{ID: "evt_2", Type: "HANDOFF_CREATED", Summary: "Handoff packet created", Timestamp: now},
		},
		RecentConversation: []ConversationItem{
			{Role: "user", Body: "Start implementation.", CreatedAt: now},
			{Role: "system", Body: "I prepared the shell state.", CreatedAt: now},
		},
		LatestCanonicalResponse: "I prepared the shell state.",
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
		Session: SessionState{
			SessionID: "shs_1234567890",
			StartedAt: now,
			Journal: []SessionEvent{
				{Type: SessionEventShellStarted, Summary: "Shell session shs_1234567890 started.", CreatedAt: now},
				{Type: SessionEventHostLive, Summary: "Live worker host is active.", CreatedAt: now},
			},
			PriorPersistedSummary: "Shell live host ended",
		},
		LastRefresh: now,
	}, host, 120, 32)

	if vm.Header.Worker != "codex live" {
		t.Fatalf("expected active worker label, got %q", vm.Header.Worker)
	}
	if vm.Header.Continuity != "ready" {
		t.Fatalf("expected ready continuity, got %q", vm.Header.Continuity)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	if vm.ProofStrip == nil {
		t.Fatal("expected proof strip")
	}
	if vm.Overlay != nil {
		t.Fatal("expected no overlay")
	}
	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if len(vm.ProofStrip.Lines) < 2 {
		t.Fatal("expected activity lines merged into proof strip")
	}
	if vm.Inspector.Sections[0].Title != "operator" {
		t.Fatalf("expected operator section first, got %q", vm.Inspector.Sections[0].Title)
	}
	foundOperatorNext := false
	for _, line := range vm.Inspector.Sections[0].Lines {
		if strings.Contains(line, "next start next run") {
			foundOperatorNext = true
		}
	}
	if !foundOperatorNext {
		t.Fatalf("expected operator section to include next action, got %#v", vm.Inspector.Sections[0].Lines)
	}
	if vm.Inspector.Sections[1].Title != "worker session" {
		t.Fatalf("expected worker session section second, got %q", vm.Inspector.Sections[1].Title)
	}
	foundSessionLine := false
	for _, line := range vm.Inspector.Sections[1].Lines {
		if strings.Contains(line, "new shell session shs_1234567890") {
			foundSessionLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected worker-session inspector to include session id, got %#v", vm.Inspector.Sections[1].Lines)
	}
	foundHandoffContinuity := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "continuity launch completed, acknowledgment captured, downstream unproven") {
				foundHandoffContinuity = true
				break
			}
		}
	}
	if !foundHandoffContinuity {
		t.Fatalf("expected handoff continuity line in inspector, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelSurfacesRebriefRequiredState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_rebrief",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "REBRIEF_REQUIRED",
			Action:          "REGENERATE_BRIEF",
			ReadyForNextRun: false,
			Reason:          "operator chose to regenerate the execution brief before another run",
		},
		Checkpoint: &CheckpointSummary{
			ID:          "chk_rebrief",
			IsResumable: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "rebrief" {
		t.Fatalf("expected rebrief continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery rebrief required") {
		t.Fatalf("expected rebrief operator state, got %q", status)
	}
	if !strings.Contains(status, "next regenerate brief") {
		t.Fatalf("expected regenerate-brief operator action, got %q", status)
	}
}

func TestBuildViewModelSurfacesContinueExecutionRequiredState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_continue_pending",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "CONTINUE_EXECUTION_REQUIRED",
			Action:          "EXECUTE_CONTINUE_RECOVERY",
			ReadyForNextRun: false,
			Reason:          "operator chose to continue with the current brief, but explicit continue finalization is still required",
		},
		Checkpoint: &CheckpointSummary{
			ID:          "chk_continue_pending",
			IsResumable: true,
		},
		LocalResume: &LocalResumeAuthoritySummary{
			State: "NOT_APPLICABLE",
			Mode:  "FINALIZE_CONTINUE_RECOVERY",
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			RequiredNextAction: "FINALIZE_CONTINUE_RECOVERY",
			Actions: []OperatorActionAuthority{
				{Action: "FINALIZE_CONTINUE_RECOVERY", State: "REQUIRED_NEXT", Reason: "operator chose to continue with the current brief, but explicit continue finalization is still required"},
				{Action: "RESUME_INTERRUPTED_LINEAGE", State: "NOT_APPLICABLE", Reason: "local interrupted-lineage resume is not applicable; explicit continue recovery must be executed before any new bounded run"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:      "FINALIZE_CONTINUE_RECOVERY",
				Status:      "REQUIRED_NEXT",
				Domain:      "LOCAL",
				CommandHint: "tuku recovery continue --task tsk_continue_pending",
			},
			MandatoryBeforeProgress: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "continue-pending" {
		t.Fatalf("expected continue-pending continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery continue confirmation required") {
		t.Fatalf("expected continue-confirmation operator state, got %q", status)
	}
	if !strings.Contains(status, "next finalize continue") {
		t.Fatalf("expected finalize-continue operator action, got %q", status)
	}
	if !strings.Contains(status, "authority required finalize continue") {
		t.Fatalf("expected required-next authority line, got %q", status)
	}
	if !strings.Contains(status, "plan required finalize continue recovery") {
		t.Fatalf("expected execution plan line, got %q", status)
	}
	if !strings.Contains(status, "command tuku recovery continue --task tsk_continue_pending") {
		t.Fatalf("expected execution command hint line, got %q", status)
	}
	if !strings.Contains(status, "local resume not applicable | finalize continue first") {
		t.Fatalf("expected explicit local-resume distinction, got %q", status)
	}
}

func TestBuildViewModelSurfacesCompiledIntentDigestAcrossStatusAndInspector(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_intent_overlay",
		Phase:  "INTERPRETING",
		Status: "ACTIVE",
		CompiledIntent: &CompiledIntentSummary{
			Class:                   "IMPLEMENT_CHANGE",
			Posture:                 "PLANNING",
			ExecutionReadiness:      "PLANNING_IN_PROGRESS",
			Objective:               "Prepare bounded intent rollout",
			RequiresClarification:   true,
			BoundedEvidenceMessages: 5,
			Digest:                  "planning intent posture in bounded recent evidence",
			Advisory:                "Intent remains planning-focused in bounded recent evidence.",
			ClarificationQuestions:  []string{"Which task slice should be executed first?"},
		},
	}, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "intent planning intent posture in bounded recent evidence") {
		t.Fatalf("expected intent digest in status overlay, got %q", status)
	}
	if !strings.Contains(status, "intent readiness clarification needed") {
		t.Fatalf("expected intent readiness in status overlay, got %q", status)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector view")
	}
	foundIntentSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "intent" {
			continue
		}
		foundIntentSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "planning intent posture in bounded recent evidence") {
			t.Fatalf("expected intent digest in inspector section, got %q", joined)
		}
		if !strings.Contains(joined, "posture planning | readiness planning in progress") {
			t.Fatalf("expected posture/readiness in inspector section, got %q", joined)
		}
		if !strings.Contains(joined, "clarification Which task slice should be executed first?") {
			t.Fatalf("expected clarification cue in inspector section, got %q", joined)
		}
	}
	if !foundIntentSection {
		t.Fatal("expected inspector intent section")
	}
}

func TestBuildViewModelSurfacesBlockedLocalMutationAuthorityUnderClaudeOwnership(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_block",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:  "HANDOFF_LAUNCH_COMPLETED",
			Action: "MONITOR_LAUNCHED_HANDOFF",
		},
		ActionAuthority: &OperatorActionAuthoritySet{
			Actions: []OperatorActionAuthority{
				{Action: "LOCAL_MESSAGE_MUTATION", State: "BLOCKED", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_block", Reason: "Cannot send a local task message while launched Claude handoff hnd_block remains the active continuity branch."},
				{Action: "CREATE_CHECKPOINT", State: "BLOCKED", BlockingBranchClass: "HANDOFF_CLAUDE", BlockingBranchRef: "hnd_block", Reason: "Cannot create a local checkpoint while launched Claude handoff hnd_block remains the active continuity branch."},
				{Action: "RESOLVE_ACTIVE_HANDOFF", State: "ALLOWED", Reason: "active Claude handoff branch can be explicitly resolved without claiming downstream completion"},
			},
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:      "RESOLVE_ACTIVE_HANDOFF",
				Status:      "ALLOWED",
				Domain:      "HANDOFF_CLAUDE",
				CommandHint: "tuku handoff-resolve --task tsk_handoff_block --handoff hnd_block --kind <abandoned|superseded-by-local|closed-unproven|reviewed-stale>",
			},
			MandatoryBeforeProgress: true,
		},
	}, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "authority local mutation blocked by Claude handoff hnd_block") {
		t.Fatalf("expected blocked local mutation authority line, got %q", status)
	}
	if !strings.Contains(status, "plan allowed resolve active handoff") {
		t.Fatalf("expected execution plan line under Claude ownership, got %q", status)
	}
	if !strings.Contains(status, "command tuku handoff-resolve --task tsk_handoff_block --handoff hnd_block") {
		t.Fatalf("expected handoff resolution command hint, got %q", status)
	}
}

func TestBuildViewModelDifferentiatesLocalResumeModes(t *testing.T) {
	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "interrupted resume allowed",
			snapshot: Snapshot{
				TaskID: "tsk_interrupt",
				Recovery: &RecoverySummary{
					Class:  "INTERRUPTED_RUN_RECOVERABLE",
					Action: "RESUME_INTERRUPTED_RUN",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State:        "ALLOWED",
					Mode:         "RESUME_INTERRUPTED_LINEAGE",
					CheckpointID: "chk_interrupt",
				},
			},
			want: "local resume allowed via checkpoint chk_interr",
		},
		{
			name: "fresh next run",
			snapshot: Snapshot{
				TaskID: "tsk_ready",
				Recovery: &RecoverySummary{
					Class:           "READY_NEXT_RUN",
					Action:          "START_NEXT_RUN",
					ReadyForNextRun: true,
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "START_FRESH_NEXT_RUN",
				},
			},
			want: "local resume not applicable | start fresh next run",
		},
		{
			name: "blocked by Claude branch",
			snapshot: Snapshot{
				TaskID: "tsk_blocked",
				Recovery: &RecoverySummary{
					Class:  "ACCEPTED_HANDOFF_LAUNCH_READY",
					Action: "LAUNCH_ACCEPTED_HANDOFF",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State:               "BLOCKED",
					BlockingBranchClass: "HANDOFF_CLAUDE",
					BlockingBranchRef:   "hnd_block",
				},
			},
			want: "local resume blocked by Claude handoff hnd_block",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := BuildViewModel(tc.snapshot, UIState{ShowStatus: true}, NewTranscriptHost(), 120, 32)
			if vm.Overlay == nil {
				t.Fatal("expected status overlay")
			}
			status := strings.Join(vm.Overlay.Lines, "\n")
			if !strings.Contains(status, tc.want) {
				t.Fatalf("expected overlay to contain %q, got %q", tc.want, status)
			}
		})
	}
}

func TestBuildViewModelDifferentiatesLocalRunFinalizationFromResumeAuthority(t *testing.T) {
	cases := []struct {
		name       string
		snapshot   Snapshot
		wantRun    string
		wantResume string
	}{
		{
			name: "stale reconciliation",
			snapshot: Snapshot{
				TaskID: "tsk_stale",
				Recovery: &RecoverySummary{
					Class:  "STALE_RUN_RECONCILIATION_REQUIRED",
					Action: "RECONCILE_STALE_RUN",
				},
				LocalRunFinalization: &LocalRunFinalizationSummary{
					State: "STALE_RECONCILIATION_REQUIRED",
					RunID: "run_stale",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "NONE",
				},
			},
			wantRun:    "local run stale reconciliation required run_stale",
			wantResume: "local resume not applicable",
		},
		{
			name: "failed review required",
			snapshot: Snapshot{
				TaskID: "tsk_failed",
				Recovery: &RecoverySummary{
					Class:  "FAILED_RUN_REVIEW_REQUIRED",
					Action: "INSPECT_FAILED_RUN",
				},
				LocalRunFinalization: &LocalRunFinalizationSummary{
					State: "FAILED_REVIEW_REQUIRED",
					RunID: "run_failed",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "NONE",
				},
			},
			wantRun:    "local run failed review required run_failed",
			wantResume: "local resume not applicable",
		},
		{
			name: "fresh next run",
			snapshot: Snapshot{
				TaskID: "tsk_ready_again",
				Recovery: &RecoverySummary{
					Class:           "READY_NEXT_RUN",
					Action:          "START_NEXT_RUN",
					ReadyForNextRun: true,
				},
				LocalRunFinalization: &LocalRunFinalizationSummary{
					State: "FINALIZED",
					RunID: "run_done",
				},
				LocalResume: &LocalResumeAuthoritySummary{
					State: "NOT_APPLICABLE",
					Mode:  "START_FRESH_NEXT_RUN",
				},
			},
			wantRun:    "local run finalized run_done",
			wantResume: "local resume not applicable | start fresh next run",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := BuildViewModel(tc.snapshot, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)
			if vm.Overlay == nil {
				t.Fatal("expected status overlay")
			}
			status := strings.Join(vm.Overlay.Lines, "\n")
			if !strings.Contains(status, tc.wantRun) {
				t.Fatalf("expected overlay to contain %q, got %q", tc.wantRun, status)
			}
			if !strings.Contains(status, tc.wantResume) {
				t.Fatalf("expected overlay to contain %q, got %q", tc.wantResume, status)
			}
		})
	}
}

func TestBuildViewModelSurfacesResolvedClaudeHandoffContinuity(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_resolved_handoff",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "READY_NEXT_RUN",
			Action:          "START_NEXT_RUN",
			ReadyForNextRun: true,
			Reason:          "explicitly resolved Claude handoff no longer blocks local control",
		},
		Resolution: &ResolutionSummary{
			ResolutionID: "hrs_1",
			Kind:         "SUPERSEDED_BY_LOCAL",
			Summary:      "operator returned local control",
		},
		ActiveBranch: &ActiveBranchSummary{
			Class:                  "LOCAL",
			BranchRef:              "tsk_resolved_handoff",
			ActionabilityAnchor:    "BRIEF",
			ActionabilityAnchorRef: "brf_local",
			Reason:                 "local Tuku lineage currently controls canonical progression",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:  "NOT_APPLICABLE",
			Reason: "no Claude handoff continuity is active",
		},
	}, UIState{ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "ready" {
		t.Fatalf("expected ready continuity after explicit resolution, got %q", vm.Header.Continuity)
	}
	found := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "resolution superseded-by-local") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected historical resolution line in inspector, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelSurfacesClaudeActiveBranchOwnership(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_owner",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "ACCEPTED_HANDOFF_LAUNCH_READY",
			Action:                "LAUNCH_ACCEPTED_HANDOFF",
			ReadyForHandoffLaunch: true,
			Reason:                "accepted Claude handoff is ready to launch",
		},
		ActiveBranch: &ActiveBranchSummary{
			Class:                  "HANDOFF_CLAUDE",
			BranchRef:              "hnd_1",
			ActionabilityAnchor:    "HANDOFF",
			ActionabilityAnchorRef: "hnd_1",
			Reason:                 "accepted Claude handoff branch currently owns continuity",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:  "ACCEPTED_NOT_LAUNCHED",
			Reason: "accepted Claude handoff is ready to launch",
		},
	}, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "branch Claude handoff hnd_1") {
		t.Fatalf("expected explicit Claude branch ownership in status overlay, got %q", status)
	}
}

func TestBuildViewModelSurfacesStalledClaudeFollowThroughState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_handoff_stalled",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED",
			Action:          "REVIEW_HANDOFF_FOLLOW_THROUGH",
			ReadyForNextRun: false,
			Reason:          "Claude handoff follow-through appears stalled and needs review",
		},
		HandoffContinuity: &HandoffContinuitySummary{
			State:                "FOLLOW_THROUGH_STALLED",
			Reason:               "Claude handoff launch appears stalled and needs review",
			FollowThroughKind:    "STALLED_REVIEW_REQUIRED",
			FollowThroughSummary: "Claude follow-through appears stalled",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
		},
		FollowThrough: &FollowThroughSummary{
			RecordID: "hft_1",
			Kind:     "STALLED_REVIEW_REQUIRED",
			Summary:  "Claude follow-through appears stalled",
		},
	}, UIState{ShowStatus: true, ShowInspector: true}, NewTranscriptHost(), 120, 32)

	if vm.Header.Continuity != "review" {
		t.Fatalf("expected review continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "recovery handoff follow-through review required") {
		t.Fatalf("expected stalled handoff operator state, got %q", status)
	}
	if !strings.Contains(status, "next review handoff follow-through") {
		t.Fatalf("expected stalled handoff operator action, got %q", status)
	}
	found := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "handoff" {
			continue
		}
		for _, line := range section.Lines {
			if strings.Contains(line, "continuity follow-through stalled, review required") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected stalled follow-through continuity in inspector, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelAddsLiveWorkerPaneRecencySummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | codex live | input to worker",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_live_summary",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(18 * time.Second),
		Session: SessionState{
			SessionID: "shs_live_summary",
		},
	}, host, 120, 20)

	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "codex live | newest output at bottom") {
		t.Fatalf("expected live recency summary, got %#v", vm.WorkerPane.Lines)
	}
	if strings.Contains(vm.WorkerPane.Lines[0], "quiet for 18s") {
		t.Fatalf("expected pane summary to stay concise, got %#v", vm.WorkerPane.Lines)
	}
}

func TestSnapshotFromIPCMapsShellState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	raw := ipc.TaskShellSnapshotResponse{
		TaskID:        "tsk_1",
		Goal:          "Goal",
		Phase:         "BRIEF_READY",
		Status:        "ACTIVE",
		IntentClass:   "implement",
		IntentSummary: "implement: wire shell",
		RepoAnchor: ipc.RepoAnchor{
			RepoRoot:         "/tmp/repo",
			Branch:           "main",
			HeadSHA:          "sha",
			WorkingTreeDirty: true,
			CapturedAt:       now,
		},
		Brief: &ipc.TaskShellBrief{
			BriefID:          "brf_1",
			Objective:        "Objective",
			NormalizedAction: "act",
			Constraints:      []string{"c1"},
			DoneCriteria:     []string{"d1"},
		},
		Run: &ipc.TaskShellRun{
			RunID:            "run_1",
			WorkerKind:       "codex",
			Status:           "COMPLETED",
			LastKnownSummary: "done",
			StartedAt:        now,
		},
		Launch: &ipc.TaskShellLaunch{
			AttemptID:   "hlc_1",
			LaunchID:    "launch_1",
			Status:      "FAILED",
			RequestedAt: now,
			EndedAt:     now.Add(2 * time.Second),
			Summary:     "launcher failed",
		},
		LaunchControl: &ipc.TaskShellLaunchControl{
			State:            "FAILED",
			RetryDisposition: "ALLOWED",
			Reason:           "durable failure may be retried",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			LaunchID:         "launch_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
			FailedAt:         now.Add(2 * time.Second),
		},
		Recovery: &ipc.TaskShellRecovery{
			ContinuityOutcome:     "SAFE_RESUME_AVAILABLE",
			RecoveryClass:         "FAILED_RUN_REVIEW_REQUIRED",
			RecommendedAction:     "INSPECT_FAILED_RUN",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "latest run failed",
			Issues: []ipc.TaskShellRecoveryIssue{
				{Code: "RUN_BRIEF_MISSING", Message: "run references missing brief"},
			},
		},
		ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
			State:          "FOLLOW_UP_REOPENED",
			Digest:         "follow-up reopened",
			WindowAdvisory: "bounded window open=1 reopened=1",
			Advisory:       "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
			ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
				Class:                   "WEAK_CLOSURE_REOPENED",
				Digest:                  "closure reopened after close",
				WindowAdvisory:          "bounded window anchors=1 open=1 closed=0 reopened=1 triaged-without-follow-up=0 repeated=0 behind-latest=0",
				Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
				OperationallyUnresolved: true,
				ClosureAppearsWeak:      true,
				ReopenedAfterClosure:    true,
				RecentAnchors: []ipc.TaskContinuityIncidentClosureAnchorItem{
					{
						AnchorTransitionReceiptID: "ctr_1",
						Class:                     "WEAK_CLOSURE_REOPENED",
						Digest:                    "closure reopened after close",
						Explanation:               "reopened after closure in recent bounded evidence",
						LatestFollowUpReceiptID:   "cifr_1",
						LatestFollowUpActionKind:  "REOPENED",
						LatestFollowUpAt:          now.Add(3 * time.Second),
					},
				},
			},
		},
		RecentProofs: []ipc.TaskShellProof{
			{EventID: "evt_1", Type: "CHECKPOINT_CREATED", Summary: "Checkpoint created", Timestamp: now},
		},
		RecentConversation: []ipc.TaskShellConversation{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
		LatestCanonicalResponse: "Canonical response.",
	}

	snapshot := snapshotFromIPC(raw)
	if snapshot.TaskID != "tsk_1" {
		t.Fatalf("expected task id tsk_1, got %q", snapshot.TaskID)
	}
	if snapshot.Brief == nil || snapshot.Brief.ID != "brf_1" {
		t.Fatal("expected brief mapping")
	}
	if snapshot.Run == nil || snapshot.Run.WorkerKind != "codex" {
		t.Fatal("expected run mapping")
	}
	if snapshot.Launch == nil || snapshot.Launch.AttemptID != "hlc_1" {
		t.Fatalf("expected launch mapping, got %+v", snapshot.Launch)
	}
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.RetryDisposition != "ALLOWED" {
		t.Fatalf("expected launch control mapping, got %+v", snapshot.LaunchControl)
	}
	if snapshot.Recovery == nil || snapshot.Recovery.Class != "FAILED_RUN_REVIEW_REQUIRED" {
		t.Fatalf("expected recovery mapping, got %+v", snapshot.Recovery)
	}
	if snapshot.ContinuityIncidentFollowUp == nil || snapshot.ContinuityIncidentFollowUp.ClosureIntelligence == nil {
		t.Fatalf("expected continuity incident closure intelligence mapping, got %+v", snapshot.ContinuityIncidentFollowUp)
	}
	if len(snapshot.ContinuityIncidentFollowUp.ClosureIntelligence.RecentAnchors) != 1 || snapshot.ContinuityIncidentFollowUp.ClosureIntelligence.RecentAnchors[0].Class != "WEAK_CLOSURE_REOPENED" {
		t.Fatalf("expected closure anchor timeline mapping, got %+v", snapshot.ContinuityIncidentFollowUp.ClosureIntelligence.RecentAnchors)
	}
	if len(snapshot.RecentProofs) != 1 || snapshot.RecentProofs[0].Summary != "Checkpoint created" {
		t.Fatal("expected proof mapping")
	}
}

func TestContinuityLabelUsesRecoveryTruthOverRawCheckpointResumability(t *testing.T) {
	snapshot := Snapshot{
		Status: "ACTIVE",
		Checkpoint: &CheckpointSummary{
			ID:          "chk_1",
			IsResumable: true,
		},
		Recovery: &RecoverySummary{
			Class:           "FAILED_RUN_REVIEW_REQUIRED",
			ReadyForNextRun: false,
		},
	}

	if got := continuityLabel(snapshot); got != "review" {
		t.Fatalf("expected recovery-driven continuity label, got %q", got)
	}
}

func TestContinuityLabelDistinguishesReadyRecoverableAndLaunchStates(t *testing.T) {
	cases := []struct {
		name     string
		snapshot Snapshot
		want     string
	}{
		{
			name: "ready next run",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "READY_NEXT_RUN", ReadyForNextRun: true},
			},
			want: "ready",
		},
		{
			name: "interrupted recoverable",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "INTERRUPTED_RUN_RECOVERABLE", ReadyForNextRun: true},
			},
			want: "recoverable",
		},
		{
			name: "launch retry",
			snapshot: Snapshot{
				Recovery:      &RecoverySummary{Class: "ACCEPTED_HANDOFF_LAUNCH_READY", ReadyForHandoffLaunch: true},
				LaunchControl: &LaunchControlSummary{State: "FAILED", RetryDisposition: "ALLOWED"},
			},
			want: "launch-retry",
		},
		{
			name: "completed",
			snapshot: Snapshot{
				Recovery: &RecoverySummary{Class: "COMPLETED_NO_ACTION"},
			},
			want: "complete",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := continuityLabel(tc.snapshot); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildViewModelStatusOverlayReflectsHostState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		status: HostStatus{
			Mode:           HostModeTranscript,
			State:          HostStateFallback,
			Label:          "transcript fallback",
			InputLive:      false,
			Note:           "live worker exited; switched to transcript fallback",
			StateChangedAt: now,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_overlay",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "FAILED_RUN_REVIEW_REQUIRED",
			Action:          "INSPECT_FAILED_RUN",
			ReadyForNextRun: false,
			Reason:          "latest run run_1 failed; inspect failure evidence before retrying or regenerating the brief",
		},
		LatestCanonicalResponse: "Tuku is ready to continue from transcript mode.",
	}, UIState{
		ShowStatus: true,
		ObservedAt: now.Add(6 * time.Second),
		Session: SessionState{
			SessionID:             "shs_overlay",
			PriorPersistedSummary: "Shell transcript fallback activated",
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundHostLine := false
	foundVerboseFallbackTiming := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "host transcript / fallback / input off") {
			foundHostLine = true
		}
		if strings.Contains(line, "fallback activated 6s ago") {
			foundVerboseFallbackTiming = true
		}
	}
	if !foundHostLine {
		t.Fatalf("expected host status line in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundVerboseFallbackTiming {
		t.Fatalf("expected overlay to retain verbose fallback timing, got %#v", vm.Overlay.Lines)
	}
	foundSessionLine := false
	foundPriorLine := false
	foundRecoveryLine := false
	foundNextLine := false
	foundReasonLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "new shell session shs_overlay") {
			foundSessionLine = true
		}
		if strings.Contains(line, "previous shell outcome Shell transcript fallback activated") {
			foundPriorLine = true
		}
		if strings.Contains(line, "recovery failed run review required") {
			foundRecoveryLine = true
		}
		if strings.Contains(line, "next inspect failed run") {
			foundNextLine = true
		}
		if strings.Contains(line, "reason latest run run_1 failed") {
			foundReasonLine = true
		}
	}
	if !foundSessionLine {
		t.Fatalf("expected session id in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundPriorLine {
		t.Fatalf("expected previous shell outcome in overlay, got %#v", vm.Overlay.Lines)
	}
	if !foundRecoveryLine || !foundNextLine || !foundReasonLine {
		t.Fatalf("expected operator truth lines in overlay, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "read-only") {
		t.Fatalf("expected footer to clarify read-only fallback, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "fallback active") {
		t.Fatalf("expected footer to include short fallback cue, got %q", vm.Footer)
	}
	if !strings.Contains(vm.Footer, "next inspect failed run") {
		t.Fatalf("expected footer to include operator next-action cue, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "fallback activated 6s ago") {
		t.Fatalf("expected footer to avoid duplicating verbose fallback timing, got %q", vm.Footer)
	}
}

func TestBuildViewModelAddsFallbackWorkerPaneSummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.status.StateChangedAt = now
	host.UpdateSnapshot(Snapshot{
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	})

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_fallback_summary",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:           "HANDOFF_LAUNCH_PENDING_OUTCOME",
			Action:          "WAIT_FOR_LAUNCH_OUTCOME",
			ReadyForNextRun: false,
		},
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(6 * time.Second),
		Session: SessionState{
			SessionID: "shs_fallback_summary",
		},
	}, host, 120, 20)

	if len(vm.WorkerPane.Lines) == 0 {
		t.Fatal("expected worker pane lines")
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "launch pending | next wait for launch outcome | transcript fallback | historical transcript below | fallback active") {
		t.Fatalf("expected fallback summary line, got %#v", vm.WorkerPane.Lines)
	}
}

func TestBuildViewModelSurfacesTranscriptReviewBoundaryTruth(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	snapshot := Snapshot{
		TaskID: "tsk_review_truth",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
		ShellSessions: []KnownShellSession{
			{
				SessionID:                        "shs_review_truth",
				TaskID:                           "tsk_review_truth",
				TranscriptState:                  "transcript_only_bounded_partial",
				TranscriptRetainedChunks:         200,
				TranscriptDroppedChunks:          57,
				TranscriptRetentionLimit:         200,
				TranscriptReviewedUpTo:           184,
				TranscriptReviewSource:           "worker_output",
				TranscriptReviewSummary:          "reviewed most recent worker output window",
				TranscriptReviewAt:               now.Add(-10 * time.Second),
				TranscriptReviewStale:            true,
				TranscriptReviewNewer:            37,
				TranscriptReviewClosureState:     "source_scoped_review_stale_within_retained",
				TranscriptReviewOldestUnreviewed: 185,
				TranscriptNewestSequence:         221,
				TranscriptRecentReviews: []TranscriptReviewMarker{
					{ReviewID: "srev_184", SourceFilter: "worker_output", ReviewedUpToSequence: 184, StaleBehindLatest: true},
					{ReviewID: "srev_150", SourceFilter: "", ReviewedUpToSequence: 150, StaleBehindLatest: true},
				},
				LastUpdatedAt: now,
				Active:        true,
			},
		},
	}
	vm := BuildViewModel(snapshot, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session: SessionState{
			SessionID: "shs_review_truth",
			KnownSessions: []KnownShellSession{
				snapshot.ShellSessions[0],
			},
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector view")
	}
	foundReviewBoundary := false
	foundNewerEvidence := false
	foundUnreviewedRange := false
	foundRecentHistory := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "worker session" {
			continue
		}
		joined := strings.Join(section.Lines, "\n")
		if strings.Contains(joined, "review up to seq 184 (worker_output)") {
			foundReviewBoundary = true
		}
		if strings.Contains(joined, "newer retained evidence exists (+37 seq)") {
			foundNewerEvidence = true
		}
		if strings.Contains(joined, "unreviewed retained range 185-221") {
			foundUnreviewedRange = true
		}
		if strings.Contains(joined, "recent review markers:") {
			foundRecentHistory = true
		}
	}
	if !foundReviewBoundary || !foundNewerEvidence || !foundUnreviewedRange || !foundRecentHistory {
		t.Fatalf("expected transcript review truth in inspector, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	overlay := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(overlay, "transcript review seq 184 (worker_output), unreviewed retained range 185-221 (+37)") {
		t.Fatalf("expected transcript review status line in overlay, got %q", overlay)
	}
	if strings.Contains(strings.ToLower(overlay), "verified") || strings.Contains(strings.ToLower(overlay), "resume from") {
		t.Fatalf("overlay must stay conservative, got %q", overlay)
	}
}

func TestBuildViewModelSurfacesReviewAwareOperatorGuidance(t *testing.T) {
	now := time.Unix(1710005000, 0).UTC()
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_review_gate",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "READY_NEXT_RUN",
			Action:                "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
		},
		OperatorDecision: &OperatorDecisionSummary{
			Headline:           "Local fresh run ready",
			RequiredNextAction: "START_LOCAL_RUN",
			Guidance:           "Start the next bounded local run. Transcript review is stale for shell session shs_review_gate; newer retained evidence starts at sequence 42.",
			IntegrityNote:      "Transcript review is behind retained evidence (oldest unreviewed sequence 42).",
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:      "START_LOCAL_RUN",
				Status:      "REQUIRED_NEXT",
				CommandHint: "tuku run --task tsk_review_gate --action start",
				Reason:      "Newer retained transcript evidence exists starting at sequence 42; review awareness is recommended while progressing.",
			},
		},
		RecentConversation: []ConversationItem{
			{Role: "system", Body: "Canonical response.", CreatedAt: now},
		},
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_review_gate",
		},
	}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	overlay := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(overlay, "guidance Start the next bounded local run. Transcript review is stale for shell session shs_review_gate") {
		t.Fatalf("expected review-aware guidance in overlay, got %q", overlay)
	}
	if !strings.Contains(overlay, "caution Transcript review is behind retained evidence (oldest unreviewed sequence 42).") {
		t.Fatalf("expected review-aware integrity note in overlay, got %q", overlay)
	}
}

func TestBuildViewModelShowsLongQuietLiveInferenceCarefully(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | codex live | input to worker",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:         HostModeCodexPTY,
			State:        HostStateLive,
			Label:        "codex live",
			InputLive:    true,
			LastOutputAt: now,
		},
	}

	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_live_quiet",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		Focus:      FocusWorker,
		ObservedAt: now.Add(2 * time.Minute),
		Session: SessionState{
			SessionID: "shs_live_quiet",
		},
	}, host, 120, 20)

	if !strings.Contains(vm.WorkerPane.Lines[0], "quiet a while") {
		t.Fatalf("expected concise long-quiet pane cue, got %#v", vm.WorkerPane.Lines)
	}
	if !strings.Contains(vm.Footer, "quiet a while") {
		t.Fatalf("expected footer to carry a short quiet-state cue, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "possibly waiting for input or stalled") {
		t.Fatalf("expected footer to avoid duplicating verbose quiet inference, got %q", vm.Footer)
	}
}

func TestBuildViewModelReflectsClaudeHostState(t *testing.T) {
	host := &stubHost{
		title:    "worker pane | claude live session",
		worker:   "claude live",
		lines:    []string{"claude> hello"},
		canInput: true,
		status: HostStatus{
			Mode:  HostModeClaudePTY,
			State: HostStateLive,
			Label: "claude live",
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_claude",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_claude",
		},
	}, host, 120, 32)

	if vm.Header.Worker != "claude live" {
		t.Fatalf("expected claude worker label, got %q", vm.Header.Worker)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundHostLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "host claude-pty / live / input live") {
			foundHostLine = true
		}
	}
	if !foundHostLine {
		t.Fatalf("expected claude host line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelSurfacesInterruptedRecoverableState(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex transcript",
		worker: "codex last",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "codex transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_interrupt",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "INTERRUPTED_RUN_RECOVERABLE",
			Action:                "RESUME_INTERRUPTED_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			Reason:                "latest run run_1 was interrupted and can be resumed safely",
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "RESUME_INTERRUPTED_LINEAGE",
				Status:         "REQUIRED_NEXT",
				Domain:         "LOCAL",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery resume-interrupted --task tsk_interrupt",
			},
			MandatoryBeforeProgress: true,
		},
	}, UIState{
		ShowInspector: true,
		Session:       SessionState{SessionID: "shs_interrupt"},
	}, host, 120, 32)

	if vm.Header.Continuity != "recoverable" {
		t.Fatalf("expected recoverable continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "interrupted recoverable | next resume interrupted run") {
		t.Fatalf("expected interrupted operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	joined := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(joined, "readiness next-run yes | handoff-launch no") || !strings.Contains(joined, "reason latest run run_1 was interrupted") {
		t.Fatalf("expected interrupted recovery truth in operator section, got %q", joined)
	}
	if !strings.Contains(joined, "plan required resume interrupted lineage") || !strings.Contains(joined, "command tuku recovery resume-interrupted --task tsk_interrupt") {
		t.Fatalf("expected interrupted execution plan in operator section, got %q", joined)
	}
	if !strings.Contains(vm.Footer, "n execute next step") {
		t.Fatalf("expected direct-execution footer cue, got %q", vm.Footer)
	}
}

func TestBuildViewModelSurfacesAcceptedHandoffLaunchReadyState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | transcript",
		worker: "claude handoff",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch_ready",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "ACCEPTED_HANDOFF_LAUNCH_READY",
			Action:                "LAUNCH_ACCEPTED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: true,
			Reason:                "accepted handoff hnd_1 is ready to launch for claude",
		},
		LaunchControl: &LaunchControlSummary{
			State:            "NOT_REQUESTED",
			RetryDisposition: "ALLOWED",
			Reason:           "accepted handoff hnd_1 is ready to launch for claude",
			HandoffID:        "hnd_1",
			TargetWorker:     "claude",
		},
		Handoff: &HandoffSummary{
			ID:           "hnd_1",
			Status:       "ACCEPTED",
			SourceWorker: "codex",
			TargetWorker: "claude",
			Mode:         "resume",
			CreatedAt:    now,
		},
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "LAUNCH_ACCEPTED_HANDOFF",
				Status:         "REQUIRED_NEXT",
				Domain:         "HANDOFF_CLAUDE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku handoff-launch --task tsk_launch_ready --handoff hnd_1",
			},
			MandatoryBeforeProgress: true,
		},
	}, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_launch_ready"},
	}, host, 120, 32)

	if vm.Header.Continuity != "handoff-ready" {
		t.Fatalf("expected handoff-ready continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "accepted handoff launch ready | next launch accepted handoff") {
		t.Fatalf("expected worker pane operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundReadiness := false
	foundLaunch := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "readiness next-run no | handoff-launch yes") {
			foundReadiness = true
		}
		if strings.Contains(line, "launch not requested | retry allowed") {
			foundLaunch = true
		}
	}
	if !foundReadiness || !foundLaunch {
		t.Fatalf("expected launch-ready operator lines, got %#v", vm.Overlay.Lines)
	}
	statusOverlay := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(statusOverlay, "plan required launch accepted handoff") || !strings.Contains(statusOverlay, "command tuku handoff-launch --task tsk_launch_ready --handoff hnd_1") {
		t.Fatalf("expected launch-ready execution plan lines, got %q", statusOverlay)
	}
	if !strings.Contains(vm.Footer, "n execute next step") {
		t.Fatalf("expected launch-ready direct-execution footer cue, got %q", vm.Footer)
	}
}

func TestBuildViewModelDoesNotShowDirectExecuteCueForInspectFallbackPrimaryStep(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_review_only",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "REVIEW_HANDOFF_FOLLOW_THROUGH",
				Status:         "REQUIRED_NEXT",
				Domain:         "REVIEW",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_review_only",
			},
		},
	}, UIState{Session: SessionState{SessionID: "shs_review_only"}}, NewTranscriptHost(), 120, 32)

	if strings.Contains(vm.Footer, "execute next step") {
		t.Fatalf("expected no direct-execution footer cue for inspect fallback, got %q", vm.Footer)
	}
}

func TestBuildViewModelSurfacesPrimaryActionResultSummary(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_result",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "FINALIZE_CONTINUE_RECOVERY",
				Status:         "REQUIRED_NEXT",
				Domain:         "LOCAL",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery continue --task tsk_result",
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_result"},
		LastPrimaryActionResult: &PrimaryActionResultSummary{
			Action:    "RESUME_INTERRUPTED_LINEAGE",
			Outcome:   "SUCCESS",
			Summary:   "executed resume interrupted lineage",
			Deltas:    []string{"next required resume interrupted lineage -> required finalize continue recovery"},
			NextStep:  "required finalize continue recovery",
			CreatedAt: now,
		},
	}, NewTranscriptHost(), 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	joined := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(joined, "result success | executed resume interrupted lineage") {
		t.Fatalf("expected result headline in status overlay, got %q", joined)
	}
	if !strings.Contains(joined, "delta next required resume interrupted lineage -> required finalize continue recovery") {
		t.Fatalf("expected delta in status overlay, got %q", joined)
	}
	if !strings.Contains(joined, "new next required finalize continue recovery") {
		t.Fatalf("expected new next-step line in status overlay, got %q", joined)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	operator := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(operator, "result success | executed resume interrupted lineage") {
		t.Fatalf("expected result headline in inspector, got %q", operator)
	}
	if vm.ProofStrip == nil || !strings.Contains(strings.Join(vm.ProofStrip.Lines, "\n"), "next    required finalize continue recovery") {
		t.Fatalf("expected activity strip to surface refreshed next step, got %+v", vm.ProofStrip)
	}
}

func TestBuildViewModelSurfacesPrimaryActionInFlightState(t *testing.T) {
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_busy",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "LAUNCH_ACCEPTED_HANDOFF",
				Status:         "REQUIRED_NEXT",
				Domain:         "HANDOFF_CLAUDE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku handoff-launch --task tsk_busy --handoff hnd_1",
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		ShowStatus:    true,
		Session:       SessionState{SessionID: "shs_busy"},
		PrimaryActionInFlight: &PrimaryActionInFlightSummary{
			Action: "LAUNCH_ACCEPTED_HANDOFF",
		},
	}, NewTranscriptHost(), 120, 32)

	if !strings.Contains(vm.Footer, "executing launch accepted handoff...") {
		t.Fatalf("expected busy cue in footer, got %q", vm.Footer)
	}
	if strings.Contains(vm.Footer, "execute next step") {
		t.Fatalf("expected direct execute cue to hide while busy, got %q", vm.Footer)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	status := strings.Join(vm.Overlay.Lines, "\n")
	if !strings.Contains(status, "progress executing launch accepted handoff...") {
		t.Fatalf("expected busy progress in status overlay, got %q", status)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector pane")
	}
	operator := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(operator, "progress executing launch accepted handoff...") {
		t.Fatalf("expected busy progress in operator inspector, got %q", operator)
	}
	if vm.ProofStrip == nil || !strings.Contains(strings.Join(vm.ProofStrip.Lines, "\n"), "progress executing launch accepted handoff...") {
		t.Fatalf("expected busy progress in activity strip, got %+v", vm.ProofStrip)
	}
}

func TestBuildViewModelSurfacesRepairRequiredReason(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_repair",
		Phase:  "BLOCKED",
		Status: "ACTIVE",
		Checkpoint: &CheckpointSummary{
			ID:          "chk_repair",
			IsResumable: true,
		},
		Recovery: &RecoverySummary{
			Class:                 "REPAIR_REQUIRED",
			Action:                "REPAIR_CONTINUITY",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "capsule references missing brief brf_missing",
			Issues:                []RecoveryIssue{{Code: "MISSING_BRIEF", Message: "capsule references missing brief brf_missing"}},
		},
	}, UIState{
		ShowInspector: true,
		Session:       SessionState{SessionID: "shs_repair"},
	}, host, 120, 32)

	if vm.Header.Continuity != "repair" {
		t.Fatalf("expected repair continuity, got %q", vm.Header.Continuity)
	}
	if !strings.Contains(vm.WorkerPane.Lines[0], "repair required | next repair continuity") {
		t.Fatalf("expected repair operator cue, got %#v", vm.WorkerPane.Lines)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	if vm.Inspector.Sections[0].Title != "operator" {
		t.Fatalf("expected operator section first, got %q", vm.Inspector.Sections[0].Title)
	}
	joined := strings.Join(vm.Inspector.Sections[0].Lines, "\n")
	if !strings.Contains(joined, "state repair required") || !strings.Contains(joined, "reason capsule references missing brief brf_missing") {
		t.Fatalf("expected repair reason in operator section, got %q", joined)
	}
	checkpointJoined := ""
	for _, section := range vm.Inspector.Sections {
		if section.Title == "checkpoint" {
			checkpointJoined = strings.Join(section.Lines, "\n")
			break
		}
	}
	if !strings.Contains(checkpointJoined, "raw resumable yes") {
		t.Fatalf("expected checkpoint section to preserve raw resumable truth, got %q", checkpointJoined)
	}
}

func TestBuildViewModelSurfacesPendingLaunchBlockedRetry(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := NewTranscriptHost()
	host.markFallback("live worker exited; switched to transcript fallback")
	host.status.StateChangedAt = now
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch_pending",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "HANDOFF_LAUNCH_PENDING_OUTCOME",
			Action:                "WAIT_FOR_LAUNCH_OUTCOME",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "handoff launch hlc_1 is still pending durable outcome",
		},
		Launch: &LaunchSummary{
			AttemptID:   "hlc_1",
			Status:      "REQUESTED",
			RequestedAt: now,
			Summary:     "launch requested",
		},
		LaunchControl: &LaunchControlSummary{
			State:            "REQUESTED_OUTCOME_UNKNOWN",
			RetryDisposition: "BLOCKED",
			Reason:           "handoff launch hlc_1 is still pending durable outcome",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
		},
	}, UIState{
		ShowStatus: true,
		ObservedAt: now.Add(6 * time.Second),
		Session:    SessionState{SessionID: "shs_launch_pending"},
	}, host, 120, 32)

	if vm.Header.Continuity != "launch-pending" {
		t.Fatalf("expected launch-pending continuity, got %q", vm.Header.Continuity)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRecovery := false
	foundNext := false
	foundLaunch := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "recovery launch pending") {
			foundRecovery = true
		}
		if strings.Contains(line, "next wait for launch outcome") {
			foundNext = true
		}
		if strings.Contains(line, "launch pending outcome unknown | retry blocked") {
			foundLaunch = true
		}
	}
	if !foundRecovery || !foundNext || !foundLaunch {
		t.Fatalf("expected pending-launch operator truth in overlay, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelInspectorLaunchSectionUsesLaunchControlTruth(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	host := &stubHost{
		title:  "worker pane | transcript",
		worker: "claude handoff",
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_launch",
		Phase:  "PAUSED",
		Status: "ACTIVE",
		Recovery: &RecoverySummary{
			Class:                 "HANDOFF_LAUNCH_COMPLETED",
			Action:                "MONITOR_LAUNCHED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			Reason:                "handoff hnd_1 already has durable completed launch launch_1",
		},
		Launch: &LaunchSummary{
			AttemptID:   "hlc_1",
			LaunchID:    "launch_1",
			Status:      "COMPLETED",
			RequestedAt: now,
			EndedAt:     now.Add(2 * time.Second),
		},
		LaunchControl: &LaunchControlSummary{
			State:            "COMPLETED",
			RetryDisposition: "BLOCKED",
			Reason:           "handoff hnd_1 already has durable completed launch launch_1",
			HandoffID:        "hnd_1",
			AttemptID:        "hlc_1",
			LaunchID:         "launch_1",
			TargetWorker:     "claude",
			RequestedAt:      now,
			CompletedAt:      now.Add(2 * time.Second),
		},
	}, UIState{
		ShowInspector: true,
	}, host, 120, 32)

	if vm.Header.Continuity != "launched" {
		t.Fatalf("expected launched continuity label, got %q", vm.Header.Continuity)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundLaunchSection := false
	foundInvocationOnly := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "launch" {
			continue
		}
		foundLaunchSection = true
		for _, line := range section.Lines {
			if strings.Contains(line, "completed (invocation only) | retry blocked") {
				foundInvocationOnly = true
			}
		}
	}
	if !foundLaunchSection || !foundInvocationOnly {
		t.Fatalf("expected launch inspector section with invocation-only truth, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelShowsAnotherKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_known",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_other", SessionClass: KnownShellSessionClassAttachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "attachable session known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsStaleKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_stale",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_stale", SessionClass: KnownShellSessionClassStale, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateLive, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "stale session known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected stale registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsActiveUnattachableKnownSession(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_unattachable",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_current",
			KnownSessions: []KnownShellSession{
				{SessionID: "shs_current", SessionClass: KnownShellSessionClassAttachable, Active: true},
				{SessionID: "shs_other", SessionClass: KnownShellSessionClassActiveUnattachable, Active: true, ResolvedWorker: WorkerPreferenceClaude, HostState: HostStateFallback, LastUpdatedAt: time.Unix(1710000001, 0).UTC()},
			},
		},
	}, host, 120, 32)

	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundRegistryLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "active non-attachable session known") {
			foundRegistryLine = true
		}
	}
	if !foundRegistryLine {
		t.Fatalf("expected active-unattachable registry summary line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsNoRepoLabels(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateTranscriptOnly,
			Label:     "transcript",
			InputLive: false,
		},
	}
	vm := BuildViewModel(Snapshot{
		Phase:                   "SCRATCH_INTAKE",
		Status:                  "LOCAL_ONLY",
		IntentClass:             "scratch",
		IntentSummary:           "Use this local scratch session to plan work before cloning or initializing a repository.",
		LatestCanonicalResponse: "No git repository was detected. Tuku opened a local scratch and intake session instead of repo-backed continuity.",
	}, UIState{
		ShowStatus: true,
		Session: SessionState{
			SessionID: "shs_no_repo",
		},
	}, host, 120, 32)

	if vm.Header.TaskLabel != "no-task" {
		t.Fatalf("expected no-task header label, got %q", vm.Header.TaskLabel)
	}
	if vm.Header.Repo != "no-repo" {
		t.Fatalf("expected no-repo header label, got %q", vm.Header.Repo)
	}
	if vm.Header.Continuity != "local-only" {
		t.Fatalf("expected local-only continuity, got %q", vm.Header.Continuity)
	}
	if vm.Header.Worker != "scratch intake" {
		t.Fatalf("expected scratch intake worker label, got %q", vm.Header.Worker)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundTaskLine := false
	foundRepoLine := false
	foundContinuityLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "task no-task") {
			foundTaskLine = true
		}
		if strings.Contains(line, "repo no-repo") {
			foundRepoLine = true
		}
		if strings.Contains(line, "continuity local-only") {
			foundContinuityLine = true
		}
	}
	if !foundTaskLine || !foundRepoLine || !foundContinuityLine {
		t.Fatalf("expected no-repo overlay lines, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsPendingScratchAdoptionDraft(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_pending",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector:            true,
		ShowStatus:               true,
		PendingTaskMessage:       "Explicitly adopt these local scratch intake notes into this repo-backed Tuku task:\n\n- Plan project structure",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session: SessionState{
			SessionID: "shs_pending",
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Local draft is staged and ready for review.") {
			t.Fatalf("expected staged draft guidance, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft staged draft from local scratch") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected pending message overlay line, got %#v", vm.Overlay.Lines)
	}
}

func TestBuildViewModelShowsPendingDraftEditMode(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex live session",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_editing",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector:                true,
		ShowStatus:                   true,
		Focus:                        FocusWorker,
		PendingTaskMessage:           "Saved draft",
		PendingTaskMessageSource:     "local_scratch_adoption",
		PendingTaskMessageEditMode:   true,
		PendingTaskMessageEditBuffer: "Edited draft",
		Session: SessionState{
			SessionID: "shs_editing",
		},
	}, host, 120, 32)

	if vm.WorkerPane.Title != "worker pane | pending message editor" {
		t.Fatalf("expected worker pane edit title, got %q", vm.WorkerPane.Title)
	}
	joinedPane := strings.Join(vm.WorkerPane.Lines, "\n")
	if !strings.Contains(joinedPane, "Edited draft") {
		t.Fatalf("expected editor lines to show edited draft, got %q", joinedPane)
	}
	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Editing the staged local draft.") {
			t.Fatalf("expected edit-mode copy, got %q", joined)
		}
		if !strings.Contains(joined, "Nothing here is canonical until you explicitly send it with m.") {
			t.Fatalf("expected explicit local-only boundary, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft editing staged draft from local scratch") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected edit-mode overlay line, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "editing staged draft") {
		t.Fatalf("expected footer edit-mode hint, got %q", vm.Footer)
	}
}

func TestBuildViewModelShowsLocalScratchAvailableState(t *testing.T) {
	host := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_local_scratch",
		Phase:  "INTAKE",
		Status: "ACTIVE",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure"},
			},
		},
	}, UIState{
		ShowInspector: true,
		ShowStatus:    true,
		Session: SessionState{
			SessionID: "shs_local_scratch",
		},
	}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatal("expected inspector")
	}
	foundPendingSection := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "pending message" {
			continue
		}
		foundPendingSection = true
		joined := strings.Join(section.Lines, "\n")
		if !strings.Contains(joined, "Local scratch is available for explicit adoption.") {
			t.Fatalf("expected local scratch available copy, got %q", joined)
		}
	}
	if !foundPendingSection {
		t.Fatalf("expected pending message section, got %#v", vm.Inspector.Sections)
	}
	if vm.Overlay == nil {
		t.Fatal("expected status overlay")
	}
	foundPendingOverlayLine := false
	for _, line := range vm.Overlay.Lines {
		if strings.Contains(line, "draft local scratch available") {
			foundPendingOverlayLine = true
		}
	}
	if !foundPendingOverlayLine {
		t.Fatalf("expected local scratch available overlay line, got %#v", vm.Overlay.Lines)
	}
	if !strings.Contains(vm.Footer, "local scratch available") {
		t.Fatalf("expected footer local scratch hint, got %q", vm.Footer)
	}
}

func TestBuildViewModelCollapsesSecondaryChromeInNarrowTerminal(t *testing.T) {
	host := &stubHost{
		title:  "worker pane | codex live session",
		worker: "codex live",
		lines:  []string{"codex> hello"},
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateLive,
			Label:     "codex live",
			InputLive: true,
		},
	}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_narrow",
		Phase:  "EXECUTING",
		Status: "ACTIVE",
	}, UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
		Session: SessionState{
			SessionID: "shs_narrow",
		},
	}, host, 100, 16)

	if vm.Inspector != nil {
		t.Fatalf("expected inspector to auto-collapse in narrow terminal, got %#v", vm.Inspector)
	}
	if vm.ProofStrip != nil {
		t.Fatalf("expected activity strip to auto-collapse in narrow terminal, got %#v", vm.ProofStrip)
	}
	if vm.Layout.showInspector || vm.Layout.showProof {
		t.Fatalf("expected collapsed layout flags, got %+v", vm.Layout)
	}
}

func TestBuildViewModelSurfacesDurableOperatorReceiptHistory(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID:                    "tsk_receipt",
		Phase:                     "BRIEF_READY",
		Status:                    "ACTIVE",
		LatestOperatorStepReceipt: &OperatorStepReceiptSummary{ReceiptID: "orec_latest", ActionHandle: "FINALIZE_CONTINUE_RECOVERY", ResultClass: "SUCCEEDED", Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
		RecentOperatorStepReceipts: []OperatorStepReceiptSummary{
			{ReceiptID: "orec_latest", ActionHandle: "FINALIZE_CONTINUE_RECOVERY", ResultClass: "SUCCEEDED", Summary: "finalized continue recovery", CreatedAt: time.Unix(1710000100, 0).UTC()},
			{ReceiptID: "orec_prev", ActionHandle: "RESUME_INTERRUPTED_LINEAGE", ResultClass: "SUCCEEDED", Summary: "resumed interrupted lineage", CreatedAt: time.Unix(1710000000, 0).UTC()},
		},
	}, UIState{ShowInspector: true, ShowProof: true, Session: SessionState{SessionID: "shs_receipt"}}, host, 120, 32)

	if vm.Inspector == nil || vm.ProofStrip == nil {
		t.Fatalf("expected inspector and activity strip, got %+v %+v", vm.Inspector, vm.ProofStrip)
	}
	foundReceipt := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "operator" {
			continue
		}
		joined := strings.Join(section.Lines, "\n")
		if strings.Contains(joined, "receipt succeeded | finalized continue recovery") {
			foundReceipt = true
		}
	}
	if !foundReceipt {
		t.Fatalf("expected inspector receipt line, got %#v", vm.Inspector.Sections)
	}
	joinedActivity := strings.Join(vm.ProofStrip.Lines, "\n")
	if !strings.Contains(joinedActivity, "operator succeeded finalized continue recovery") || !strings.Contains(joinedActivity, "operator succeeded resumed interrupted lineage") {
		t.Fatalf("expected operator receipt history in activity strip, got %q", joinedActivity)
	}
}

func TestBuildViewModelSurfacesTranscriptReviewGapAcknowledgmentTruth(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_ack_vm",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		LatestOperatorStepReceipt: &OperatorStepReceiptSummary{
			ReceiptID:             "orec_ack",
			ActionHandle:          "START_LOCAL_RUN",
			ResultClass:           "SUCCEEDED",
			Summary:               "started local run run_123",
			ReviewGapPresent:      true,
			ReviewGapAcknowledged: true,
			CreatedAt:             time.Unix(1710000200, 0).UTC(),
		},
		LatestTranscriptReviewGapAcknowledgment: &TranscriptReviewGapAcknowledgment{
			AcknowledgmentID:         "sack_123",
			SessionID:                "shs_123",
			Class:                    "stale_review",
			OldestUnreviewedSequence: 121,
			NewestRetainedSequence:   160,
			NewerRetainedCount:       6,
			StaleBehindCurrent:       true,
			Summary:                  "proceed with explicit awareness",
			CreatedAt:                time.Unix(1710000210, 0).UTC(),
		},
	}, UIState{ShowInspector: true, ShowProof: true, Session: SessionState{SessionID: "shs_ack_vm"}}, host, 120, 32)

	if vm.Inspector == nil {
		t.Fatalf("expected inspector view to be present")
	}
	foundAck := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "operator" {
			continue
		}
		joined := strings.Join(section.Lines, "\n")
		if strings.Contains(joined, "review ack stale_review") && strings.Contains(joined, "newer +6") {
			foundAck = true
		}
	}
	if !foundAck {
		t.Fatalf("expected operator inspector to surface review-gap acknowledgment truth, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelSurfacesContinuityTransitionAuditTruth(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_transition_vm",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		LatestOperatorStepReceipt: &OperatorStepReceiptSummary{
			ReceiptID:             "orec_transition",
			ActionHandle:          "LAUNCH_ACCEPTED_HANDOFF",
			ResultClass:           "SUCCEEDED",
			Summary:               "launched accepted handoff hnd_123",
			TransitionReceiptID:   "ctr_latest",
			TransitionKind:        "HANDOFF_LAUNCH",
			ReviewGapPresent:      true,
			ReviewGapAcknowledged: false,
			CreatedAt:             time.Unix(1710000310, 0).UTC(),
		},
		LatestContinuityTransitionReceipt: &ContinuityTransitionReceiptSummary{
			ReceiptID:             "ctr_latest",
			TransitionKind:        "HANDOFF_LAUNCH",
			HandoffStateBefore:    "ACCEPTED_NOT_LAUNCHED",
			HandoffStateAfter:     "LAUNCH_COMPLETED_ACK_CAPTURED",
			ReviewGapPresent:      true,
			AcknowledgmentPresent: false,
			Summary:               "handoff launch recorded while retained transcript review was stale",
			CreatedAt:             time.Unix(1710000320, 0).UTC(),
		},
		RecentContinuityTransitionReceipts: []ContinuityTransitionReceiptSummary{
			{
				ReceiptID:             "ctr_latest",
				TransitionKind:        "HANDOFF_LAUNCH",
				HandoffStateBefore:    "ACCEPTED_NOT_LAUNCHED",
				HandoffStateAfter:     "LAUNCH_COMPLETED_ACK_CAPTURED",
				ReviewGapPresent:      true,
				AcknowledgmentPresent: false,
				Summary:               "handoff launch recorded while retained transcript review was stale",
				CreatedAt:             time.Unix(1710000320, 0).UTC(),
			},
			{
				ReceiptID:             "ctr_prev",
				TransitionKind:        "HANDOFF_RESOLUTION",
				HandoffStateBefore:    "FOLLOW_THROUGH_STALLED",
				HandoffStateAfter:     "RESOLVED",
				ReviewGapPresent:      true,
				AcknowledgmentPresent: true,
				Summary:               "handoff resolution recorded after explicit review-gap acknowledgment",
				CreatedAt:             time.Unix(1710000300, 0).UTC(),
			},
		},
		ContinuityIncidentSummary: &ContinuityIncidentRiskSummary{
			ReviewGapPresent:                true,
			AcknowledgmentPresent:           false,
			StaleOrUnreviewedReviewPosture:  true,
			UnresolvedContinuityAmbiguity:   true,
			NearbyFailedOrInterruptedRuns:   1,
			NearbyRecoveryActions:           1,
			RecentFailureOrRecoveryActivity: true,
			OperationallyNotable:            true,
			Summary:                         "anchor transition carried stale retained transcript posture with nearby failed run evidence",
		},
		ContinuityIncidentTriageHistoryRollup: &ContinuityIncidentTriageHistoryRollupSummary{
			WindowSize:                        2,
			BoundedWindow:                     true,
			DistinctAnchors:                   1,
			AnchorsNeedsFollowUp:              1,
			AnchorsWithOpenFollowUp:           1,
			AnchorsBehindLatestTransition:     1,
			AnchorsRepeatedWithoutProgression: 1,
			ReviewRiskReceipts:                2,
			AcknowledgedReviewGapReceipts:     1,
			OperationallyNotable:              true,
			Summary:                           "1 anchor(s) remain in open follow-up posture",
		},
	}, UIState{ShowInspector: true, ShowProof: true, Session: SessionState{SessionID: "shs_transition_vm"}}, host, 120, 32)

	if vm.Inspector == nil || vm.ProofStrip == nil {
		t.Fatalf("expected inspector and activity strip, got %+v %+v", vm.Inspector, vm.ProofStrip)
	}
	foundTransition := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "operator" {
			continue
		}
		joined := strings.Join(section.Lines, "\n")
		if strings.Contains(joined, "transition handoff launch") {
			foundTransition = true
		}
		if !strings.Contains(joined, "incident anchor transition carried stale retained transcript posture") {
			t.Fatalf("expected operator inspector to include continuity incident summary, got %q", joined)
		}
		if !strings.Contains(joined, "incident triage history 1 anchor(s) remain in open follow-up posture") {
			t.Fatalf("expected operator inspector to include continuity incident triage-history summary, got %q", joined)
		}
		if strings.Contains(joined, "verified") || strings.Contains(joined, "resumable") {
			t.Fatalf("transition audit wording must remain conservative, got %q", joined)
		}
	}
	if !foundTransition {
		t.Fatalf("expected operator inspector to show continuity transition audit truth, got %#v", vm.Inspector.Sections)
	}
	joinedActivity := strings.Join(vm.ProofStrip.Lines, "\n")
	if !strings.Contains(joinedActivity, "handoff launch ACCEPTED_NOT_LAUNCHED -> LAUNCH_COMPLETED_ACK_CAPTURED (review-gap unacknowledged)") {
		t.Fatalf("expected activity strip to include unacknowledged transition history line, got %q", joinedActivity)
	}
	if !strings.Contains(joinedActivity, "handoff resolution FOLLOW_THROUGH_STALLED -> RESOLVED (review-gap acknowledged)") {
		t.Fatalf("expected activity strip to include acknowledged transition history line, got %q", joinedActivity)
	}
	if !strings.Contains(joinedActivity, "risk    incident review-gap=true stale=true unresolved=true failed=1 recovery=1") {
		t.Fatalf("expected activity strip to include continuity incident risk line, got %q", joinedActivity)
	}
	if !strings.Contains(joinedActivity, "risk    triage-history window=2 anchors=1 open=1 behind-latest=1 repeated=1") {
		t.Fatalf("expected activity strip to include continuity incident triage-history line, got %q", joinedActivity)
	}
}

func TestLatestTranscriptTimestampUsesDurableTranscriptEvidence(t *testing.T) {
	conversationAt := time.Unix(1710000000, 0).UTC()
	transcriptAt := conversationAt.Add(2 * time.Minute)
	got := latestTranscriptTimestamp(Snapshot{
		RecentConversation: []ConversationItem{
			{Role: "worker", Body: "older conversation entry", CreatedAt: conversationAt},
		},
		RecentShellTranscript: []ShellTranscriptChunkSummary{
			{Source: "worker_output", Content: "new durable transcript chunk", CreatedAt: transcriptAt},
		},
	})
	if !got.Equal(transcriptAt) {
		t.Fatalf("expected latest transcript timestamp %v from durable transcript evidence, got %v", transcriptAt, got)
	}
}

func TestBuildViewModelSurfacesTriagedWithoutFollowUpCue(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_followup_cue",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		ContinuityIncidentFollowUp: &ContinuityIncidentFollowUpSummary{
			State:                     "TRIAGED_CURRENT",
			Digest:                    "triaged without follow-up",
			Advisory:                  "Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
			FollowUpAdvised:           true,
			LatestTransitionReceiptID: "ctr_cue_latest",
			LatestTriageReceiptID:     "citr_cue_latest",
			TriageAnchorReceiptID:     "ctr_cue_latest",
			FollowUpReceiptPresent:    false,
		},
	}, UIState{ShowInspector: true}, host, 120, 30)

	if vm.Inspector == nil {
		t.Fatalf("expected inspector section for follow-up cue test")
	}
	found := false
	for _, section := range vm.Inspector.Sections {
		if section.Title != "operator" {
			continue
		}
		joined := strings.Join(section.Lines, "\n")
		if strings.Contains(joined, "triaged without follow-up") {
			found = true
		}
		if strings.Contains(strings.ToLower(joined), "problem solved") || strings.Contains(strings.ToLower(joined), "incident resolved") {
			t.Fatalf("follow-up cue wording must stay conservative, got %q", joined)
		}
	}
	if !found {
		t.Fatalf("expected operator inspector to include triaged-without-follow-up cue, got %#v", vm.Inspector.Sections)
	}
}

func TestBuildViewModelSurfacesClosureAnchorTimelineCues(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_closure_timeline",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		ContinuityIncidentFollowUp: &ContinuityIncidentFollowUpSummary{
			State:          "FOLLOW_UP_REOPENED",
			Digest:         "follow-up reopened",
			WindowAdvisory: "bounded window open=1 reopened=1",
			Advisory:       "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
			ClosureIntelligence: &ContinuityIncidentClosureSummary{
				Class:                   "WEAK_CLOSURE_REOPENED",
				Digest:                  "closure reopened after close",
				WindowAdvisory:          "bounded window anchors=2 open=1 closed=1 reopened=1 triaged-without-follow-up=0 repeated=0 behind-latest=0",
				Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
				OperationallyUnresolved: true,
				ClosureAppearsWeak:      true,
				ReopenedAfterClosure:    true,
				RecentAnchors: []ContinuityIncidentClosureAnchorItem{
					{
						AnchorTransitionReceiptID: "ctr_close_reopen",
						Class:                     "WEAK_CLOSURE_REOPENED",
						Digest:                    "closure reopened after close",
						Explanation:               "reopened after closure in recent bounded evidence",
						LatestFollowUpReceiptID:   "cifr_close_reopen",
						LatestFollowUpActionKind:  "REOPENED",
						LatestFollowUpAt:          time.Unix(1713000910, 0).UTC(),
					},
					{
						AnchorTransitionReceiptID: "ctr_stable",
						Class:                     "STABLE_BOUNDED",
						Digest:                    "stable within bounded evidence",
						Explanation:               "stable bounded closure progression",
						LatestFollowUpReceiptID:   "cifr_stable",
						LatestFollowUpActionKind:  "CLOSED",
						LatestFollowUpAt:          time.Unix(1713000810, 0).UTC(),
					},
				},
			},
		},
	}, UIState{ShowInspector: true, ShowProof: true}, host, 120, 30)

	if vm.Inspector == nil || vm.ProofStrip == nil {
		t.Fatalf("expected inspector and activity strip, got %+v %+v", vm.Inspector, vm.ProofStrip)
	}
	joinedInspector := ""
	for _, section := range vm.Inspector.Sections {
		if section.Title == "operator" {
			joinedInspector = strings.Join(section.Lines, "\n")
			break
		}
	}
	if !strings.Contains(joinedInspector, "incident closure closure reopened after close") {
		t.Fatalf("expected closure digest line in inspector, got %q", joinedInspector)
	}
	joinedActivity := strings.Join(vm.ProofStrip.Lines, "\n")
	if !strings.Contains(joinedActivity, "risk    incident-closure closure reopened after close") {
		t.Fatalf("expected closure risk activity line, got %q", joinedActivity)
	}
	if !strings.Contains(joinedActivity, "closure weak closure reopened anchor=ctr_close_") {
		t.Fatalf("expected bounded closure anchor timeline line, got %q", joinedActivity)
	}
	if !strings.Contains(joinedActivity, "reopened after closure in recent bounded evidence") {
		t.Fatalf("expected conservative bounded explanation line, got %q", joinedActivity)
	}
	if strings.Contains(strings.ToLower(joinedActivity), "root cause solved") || strings.Contains(strings.ToLower(joinedActivity), "safe to continue") {
		t.Fatalf("closure timeline wording must stay conservative, got %q", joinedActivity)
	}
}

func TestBuildViewModelSurfacesTaskIncidentRiskCues(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript", InputLive: false}}
	vm := BuildViewModel(Snapshot{
		TaskID: "tsk_task_risk",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		ContinuityIncidentTaskRisk: &ContinuityIncidentTaskRiskSummary{
			Class:                               "RECURRING_WEAK_CLOSURE",
			Digest:                              "recurring continuity weakness in recent bounded evidence",
			WindowAdvisory:                      "bounded incident window anchors=3 open=1 reopened=2 triaged-without-follow-up=0 stagnant=0",
			Detail:                              "Recent bounded evidence suggests recurring weak closure posture across incidents.",
			DistinctAnchors:                     3,
			RecurringWeakClosure:                true,
			RecurringUnresolved:                 true,
			ReopenedAfterClosureAnchors:         2,
			RepeatedReopenLoopAnchors:           1,
			OperationallyUnresolvedAnchorSignal: 3,
			RecentAnchorClasses: []string{
				"WEAK_CLOSURE_REOPENED",
				"WEAK_CLOSURE_LOOP",
			},
		},
	}, UIState{ShowInspector: true, ShowProof: true}, host, 120, 30)

	if vm.Inspector == nil || vm.ProofStrip == nil {
		t.Fatalf("expected inspector and activity strip, got %+v %+v", vm.Inspector, vm.ProofStrip)
	}
	joinedInspector := ""
	for _, section := range vm.Inspector.Sections {
		if section.Title == "operator" {
			joinedInspector = strings.Join(section.Lines, "\n")
			break
		}
	}
	if !strings.Contains(joinedInspector, "incident task risk recurring continuity weakness in recent bounded evidence") {
		t.Fatalf("expected task-level risk digest in inspector, got %q", joinedInspector)
	}
	joinedActivity := strings.Join(vm.ProofStrip.Lines, "\n")
	if !strings.Contains(joinedActivity, "risk    task-incident recurring continuity weakness in recent bounded evidence | weak=true unresolved=true stagnant=false triaged-without-follow-up=false anchors=3") {
		t.Fatalf("expected task-level risk activity line, got %q", joinedActivity)
	}
	if strings.Contains(strings.ToLower(joinedActivity), "root cause solved") || strings.Contains(strings.ToLower(joinedActivity), "safe to continue") {
		t.Fatalf("task-level risk wording must stay conservative, got %q", joinedActivity)
	}
}
