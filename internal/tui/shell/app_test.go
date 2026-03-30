package shell

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type stubLifecycleSink struct {
	records []persistedLifecycleRecord
	err     error
}

type persistedLifecycleRecord struct {
	taskID    string
	sessionID string
	kind      PersistedLifecycleKind
	status    HostStatus
}

type stubTaskMessageSender struct {
	sent []sentTaskMessage
	err  error
}

type sentTaskMessage struct {
	taskID  string
	message string
}

type stubPrimaryActionExecutor struct {
	calls     []executedPrimaryAction
	err       error
	outcome   PrimaryActionExecutionOutcome
	startedCh chan struct{}
	releaseCh chan struct{}
}

type executedPrimaryAction struct {
	taskID   string
	snapshot Snapshot
}

type stubSnapshotSource struct {
	snapshot Snapshot
	err      error
	loads    []string
	next     []Snapshot
}

type stubTranscriptSink struct {
	appends []transcriptAppendCall
	err     error
}

type transcriptAppendCall struct {
	taskID    string
	sessionID string
	chunks    []TranscriptEvidenceChunk
}

type transcriptProviderStubHost struct {
	stubHost
	pending []TranscriptEvidenceChunk
}

func (s *stubSnapshotSource) Load(taskID string) (Snapshot, error) {
	s.loads = append(s.loads, taskID)
	if s.err != nil {
		return Snapshot{}, s.err
	}
	if len(s.next) > 0 {
		next := s.next[0]
		s.next = s.next[1:]
		s.snapshot = next
		return next, nil
	}
	return s.snapshot, nil
}

func (s *stubTranscriptSink) Append(taskID string, sessionID string, chunks []TranscriptEvidenceChunk) error {
	if s.err != nil {
		return s.err
	}
	s.appends = append(s.appends, transcriptAppendCall{
		taskID:    taskID,
		sessionID: sessionID,
		chunks:    append([]TranscriptEvidenceChunk{}, chunks...),
	})
	return nil
}

func (h *transcriptProviderStubHost) DrainTranscriptEvidence(limit int) []TranscriptEvidenceChunk {
	if len(h.pending) == 0 {
		return nil
	}
	if limit <= 0 || limit >= len(h.pending) {
		out := append([]TranscriptEvidenceChunk{}, h.pending...)
		h.pending = nil
		return out
	}
	out := append([]TranscriptEvidenceChunk{}, h.pending[:limit]...)
	h.pending = append([]TranscriptEvidenceChunk{}, h.pending[limit:]...)
	return out
}

func (s *stubLifecycleSink) Record(taskID string, sessionID string, kind PersistedLifecycleKind, status HostStatus) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, persistedLifecycleRecord{
		taskID:    taskID,
		sessionID: sessionID,
		kind:      kind,
		status:    status,
	})
	return nil
}

func (s *stubTaskMessageSender) Send(taskID string, message string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, sentTaskMessage{taskID: taskID, message: message})
	return nil
}

func (s *stubPrimaryActionExecutor) Execute(taskID string, snapshot Snapshot) (PrimaryActionExecutionOutcome, error) {
	if s.startedCh != nil {
		select {
		case s.startedCh <- struct{}{}:
		default:
		}
	}
	if s.releaseCh != nil {
		<-s.releaseCh
	}
	if s.err != nil {
		return PrimaryActionExecutionOutcome{}, s.err
	}
	s.calls = append(s.calls, executedPrimaryAction{taskID: taskID, snapshot: snapshot})
	outcome := s.outcome
	if strings.TrimSpace(outcome.Receipt.ActionHandle) == "" && snapshot.OperatorExecutionPlan != nil && snapshot.OperatorExecutionPlan.PrimaryStep != nil {
		outcome.Receipt.ActionHandle = snapshot.OperatorExecutionPlan.PrimaryStep.Action
	}
	if strings.TrimSpace(outcome.Receipt.ResultClass) == "" {
		outcome.Receipt.ResultClass = "SUCCEEDED"
	}
	if strings.TrimSpace(outcome.Receipt.Summary) == "" && snapshot.OperatorExecutionPlan != nil && snapshot.OperatorExecutionPlan.PrimaryStep != nil {
		outcome.Receipt.Summary = "executed " + strings.ToLower(snapshot.OperatorExecutionPlan.PrimaryStep.Action)
	}
	if outcome.Receipt.CreatedAt.IsZero() {
		outcome.Receipt.CreatedAt = time.Unix(1710000000, 0).UTC()
	}
	return outcome, nil
}

func TestHandleKeyTogglesShellUI(t *testing.T) {
	ui := UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
	}

	if action := handleShellKey(&ui, 'i'); action != actionNone {
		t.Fatalf("expected no action on inspector toggle, got %v", action)
	}
	if ui.ShowInspector {
		t.Fatal("expected inspector hidden")
	}

	if action := handleShellKey(&ui, 'p'); action != actionNone {
		t.Fatalf("expected no action on proof toggle, got %v", action)
	}
	if ui.ShowProof {
		t.Fatal("expected proof hidden")
	}

	if action := handleShellKey(&ui, '/'); action != actionNone || !ui.ShowCommands {
		t.Fatal("expected command palette enabled")
	}
	if action := handleShellKey(&ui, '?'); action != actionNone || !ui.ShowHelp || ui.ShowCommands {
		t.Fatal("expected shortcut help enabled and commands cleared")
	}

	if action := handleShellKey(&ui, 'h'); action != actionNone || ui.ShowHelp {
		t.Fatal("expected h to toggle help overlay off when already enabled")
	}
	if action := handleShellKey(&ui, 's'); action != actionNone || !ui.ShowStatus || ui.ShowHelp {
		t.Fatal("expected status overlay enabled and help cleared")
	}
	if action := handleShellKey(&ui, 'r'); action != actionRefresh {
		t.Fatalf("expected refresh action, got %v", action)
	}
	if action := handleShellKey(&ui, 'n'); action != actionExecutePrimaryOperatorStep {
		t.Fatalf("expected execute-primary action, got %v", action)
	}
	if action := handleShellKey(&ui, 'q'); action != actionQuit {
		t.Fatalf("expected quit action, got %v", action)
	}
}

func TestRouteKeyOverlayConsumesWorkerInput(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{
		Focus:        FocusWorker,
		ShowCommands: true,
	}

	if action := routeKey(&ui, host, 'a'); action != actionStageScratchAdoption {
		t.Fatalf("expected overlay key handling to remain shell-local, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected overlay mode to suppress worker writes, got %#v", host.writes)
	}
	if action := routeKey(&ui, host, '/'); action != actionNone {
		t.Fatalf("expected overlay toggle to remain local, got %v", action)
	}
	if ui.ShowCommands {
		t.Fatal("expected slash to close command overlay")
	}
}

func TestExecutePrimaryOperatorStepRefreshesSnapshotAfterSuccess(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	executor := &stubPrimaryActionExecutor{}
	source := &stubSnapshotSource{
		next: []Snapshot{{
			TaskID: "tsk_1",
			Phase:  "BRIEF_READY",
			OperatorExecutionPlan: &OperatorExecutionPlan{
				PrimaryStep: &OperatorExecutionStep{
					Action:         "START_LOCAL_RUN",
					CommandSurface: "DEDICATED",
					CommandHint:    "tuku run --task tsk_1 --action start",
				},
			},
		}},
	}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Session: newSessionState(now)}

	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("execute primary operator step: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].taskID != "tsk_1" {
		t.Fatalf("expected one primary-action execution, got %#v", executor.calls)
	}
	if len(source.loads) != 1 || source.loads[0] != "tsk_1" {
		t.Fatalf("expected one refresh load, got %#v", source.loads)
	}
	if snapshot.Phase != "BRIEF_READY" {
		t.Fatalf("expected snapshot to refresh after action, got %+v", snapshot)
	}
	if host.snapshotSeen.Phase != "BRIEF_READY" {
		t.Fatalf("expected host snapshot to refresh after action, got %+v", host.snapshotSeen)
	}
	if len(ui.Session.Journal) == 0 || ui.Session.Journal[len(ui.Session.Journal)-1].Type != SessionEventPrimaryOperatorActionExecuted {
		t.Fatalf("expected primary-action session event, got %#v", ui.Session.Journal)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "SUCCESS" {
		t.Fatalf("expected successful primary-action result summary, got %+v", ui.LastPrimaryActionResult)
	}
	if ui.LastPrimaryActionResult.NextStep == "" {
		t.Fatalf("expected next-step summary after successful action, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestExecutePrimaryOperatorStepSurfacesBackendRejection(t *testing.T) {
	executor := &stubPrimaryActionExecutor{err: context.DeadlineExceeded}
	source := &stubSnapshotSource{}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}

	err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui)
	if err == nil || !strings.Contains(err.Error(), "primary operator step start_local_run failed") {
		t.Fatalf("expected wrapped primary-action error, got %v", err)
	}
	if len(source.loads) != 0 {
		t.Fatalf("expected no refresh after failed execution, got %#v", source.loads)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "FAILED" {
		t.Fatalf("expected failed primary-action result summary, got %+v", ui.LastPrimaryActionResult)
	}
	if len(ui.LastPrimaryActionResult.Deltas) != 0 {
		t.Fatalf("expected no delta summary for failed action, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestExecutePrimaryOperatorStepRejectsGuidanceOnlyPrimaryStep(t *testing.T) {
	executor := &stubPrimaryActionExecutor{}
	source := &stubSnapshotSource{}
	host := &stubHost{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "REVIEW_HANDOFF_FOLLOW_THROUGH",
				CommandSurface: "INSPECT_FALLBACK",
				CommandHint:    "tuku inspect --task tsk_1",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}

	err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui)
	if err == nil || !strings.Contains(err.Error(), "guidance-only") {
		t.Fatalf("expected guidance-only rejection, got %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("expected no executor call for non-executable step, got %#v", executor.calls)
	}
	if ui.PrimaryActionInFlight != nil {
		t.Fatalf("expected non-executable step to never enter busy state, got %+v", ui.PrimaryActionInFlight)
	}
	if ui.LastPrimaryActionResult != nil {
		t.Fatalf("expected no execution summary for non-executable step, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestFlushTranscriptEvidenceAppendsDrainedChunks(t *testing.T) {
	host := &transcriptProviderStubHost{
		stubHost: stubHost{},
		pending: []TranscriptEvidenceChunk{
			{Source: "worker_output", Content: "line 1", CreatedAt: time.Unix(1710000000, 0).UTC()},
			{Source: "worker_output", Content: "line 2", CreatedAt: time.Unix(1710000001, 0).UTC()},
		},
	}
	sink := &stubTranscriptSink{}
	ui := &UIState{}

	flushTranscriptEvidence("tsk_1", "shs_1", host, sink, ui)

	if len(sink.appends) != 1 {
		t.Fatalf("expected one transcript append call, got %d", len(sink.appends))
	}
	if sink.appends[0].taskID != "tsk_1" || sink.appends[0].sessionID != "shs_1" {
		t.Fatalf("unexpected transcript append routing: %+v", sink.appends[0])
	}
	if len(sink.appends[0].chunks) != 2 {
		t.Fatalf("expected two appended transcript chunks, got %+v", sink.appends[0].chunks)
	}
	if len(host.pending) != 0 {
		t.Fatalf("expected transcript host pending buffer to drain, still have %d", len(host.pending))
	}
}

func TestFlushTranscriptEvidenceSurfacesSinkFailure(t *testing.T) {
	host := &transcriptProviderStubHost{
		stubHost: stubHost{},
		pending: []TranscriptEvidenceChunk{
			{Source: "worker_output", Content: "line 1", CreatedAt: time.Unix(1710000000, 0).UTC()},
		},
	}
	sink := &stubTranscriptSink{err: errors.New("append failed")}
	ui := &UIState{}

	flushTranscriptEvidence("tsk_1", "shs_1", host, sink, ui)

	if !strings.Contains(ui.LastError, "shell transcript evidence append failed") {
		t.Fatalf("expected transcript append failure surfaced in ui error, got %q", ui.LastError)
	}
}

func TestStartPrimaryOperatorStepExecutionRejectsDuplicateWhileInFlight(t *testing.T) {
	executor := &stubPrimaryActionExecutor{
		startedCh: make(chan struct{}, 1),
		releaseCh: make(chan struct{}),
	}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}
	done := make(chan primaryActionExecutionResult, 1)

	if err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done); err != nil {
		t.Fatalf("start primary operator step: %v", err)
	}
	<-executor.startedCh
	if ui.PrimaryActionInFlight == nil || ui.PrimaryActionInFlight.Action != "START_LOCAL_RUN" {
		t.Fatalf("expected in-flight primary action, got %+v", ui.PrimaryActionInFlight)
	}
	err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected duplicate in-flight rejection, got %v", err)
	}
	close(executor.releaseCh)
	result := <-done
	if err := completePrimaryOperatorStepExecution(&stubSnapshotSource{snapshot: snapshot}, "tsk_1", &stubHost{}, nil, &snapshot, &ui, result); err != nil {
		t.Fatalf("complete primary operator step: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("expected exactly one executor call, got %#v", executor.calls)
	}
}

func TestCompletePrimaryOperatorStepExecutionClearsBusyStateAfterFailure(t *testing.T) {
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{Action: "START_LOCAL_RUN", CommandSurface: "DEDICATED"},
		},
	}
	ui := UIState{
		Session:               newSessionState(time.Unix(1710000000, 0).UTC()),
		PrimaryActionInFlight: &PrimaryActionInFlightSummary{Action: "START_LOCAL_RUN", StartedAt: time.Unix(1710000000, 0).UTC()},
	}
	err := completePrimaryOperatorStepExecution(&stubSnapshotSource{snapshot: snapshot}, "tsk_1", &stubHost{}, nil, &snapshot, &ui, primaryActionExecutionResult{
		step:       OperatorExecutionStep{Action: "START_LOCAL_RUN"},
		before:     snapshot,
		err:        context.DeadlineExceeded,
		finishedAt: time.Unix(1710000001, 0).UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "primary operator step start_local_run failed") {
		t.Fatalf("expected wrapped failure, got %v", err)
	}
	if ui.PrimaryActionInFlight != nil {
		t.Fatalf("expected busy state to clear after failure, got %+v", ui.PrimaryActionInFlight)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.Outcome != "FAILED" {
		t.Fatalf("expected failure result summary, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestCompletePrimaryOperatorStepExecutionRefreshDuringInFlightPreservesFinalSummary(t *testing.T) {
	before := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "RESUME_INTERRUPTED_LINEAGE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery resume-interrupted --task tsk_1",
				Status:         "REQUIRED_NEXT",
			},
		},
		LocalResume: &LocalResumeAuthoritySummary{
			State:        "ALLOWED",
			Mode:         "RESUME_INTERRUPTED_LINEAGE",
			CheckpointID: "chk_1",
		},
	}
	manualRefreshSnapshot := Snapshot{
		TaskID: "tsk_1",
		Phase:  "PAUSED",
	}
	finalSnapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "FINALIZE_CONTINUE_RECOVERY",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery continue --task tsk_1",
				Status:         "REQUIRED_NEXT",
			},
		},
		LocalResume: &LocalResumeAuthoritySummary{
			State: "NOT_APPLICABLE",
			Mode:  "FINALIZE_CONTINUE_RECOVERY",
		},
	}
	source := &stubSnapshotSource{next: []Snapshot{manualRefreshSnapshot, finalSnapshot}}
	host := &stubHost{}
	ui := UIState{
		Session:               newSessionState(time.Unix(1710000000, 0).UTC()),
		PrimaryActionInFlight: &PrimaryActionInFlightSummary{Action: "RESUME_INTERRUPTED_LINEAGE", StartedAt: time.Unix(1710000000, 0).UTC()},
	}
	current := before
	if err := reloadShellSnapshot(source, "tsk_1", host, nil, &current, &ui, true); err != nil {
		t.Fatalf("manual refresh while busy: %v", err)
	}
	err := completePrimaryOperatorStepExecution(source, "tsk_1", host, nil, &current, &ui, primaryActionExecutionResult{
		step:       OperatorExecutionStep{Action: "RESUME_INTERRUPTED_LINEAGE"},
		before:     before,
		finishedAt: time.Unix(1710000001, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("complete primary operator step after refresh: %v", err)
	}
	if ui.LastPrimaryActionResult == nil || ui.LastPrimaryActionResult.NextStep != "required finalize continue recovery" {
		t.Fatalf("expected final summary to use post-action refreshed snapshot, got %+v", ui.LastPrimaryActionResult)
	}
}

func TestPrimaryOperatorStepCanRunAgainAfterPriorExecutionFinishes(t *testing.T) {
	source := &stubSnapshotSource{
		next: []Snapshot{
			{
				TaskID: "tsk_1",
				OperatorExecutionPlan: &OperatorExecutionPlan{
					PrimaryStep: &OperatorExecutionStep{
						Action:         "FINALIZE_CONTINUE_RECOVERY",
						CommandSurface: "DEDICATED",
						CommandHint:    "tuku recovery continue --task tsk_1",
					},
				},
			},
			{
				TaskID: "tsk_1",
				OperatorExecutionPlan: &OperatorExecutionPlan{
					PrimaryStep: &OperatorExecutionStep{
						Action:         "START_LOCAL_RUN",
						CommandSurface: "DEDICATED",
						CommandHint:    "tuku run --task tsk_1 --action start",
					},
				},
			},
		},
	}
	host := &stubHost{}
	executor := &stubPrimaryActionExecutor{}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "RESUME_INTERRUPTED_LINEAGE",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku recovery resume-interrupted --task tsk_1",
			},
		},
	}
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}

	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("first primary action: %v", err)
	}
	if ui.PrimaryActionInFlight != nil {
		t.Fatalf("expected busy state cleared after first action, got %+v", ui.PrimaryActionInFlight)
	}
	if err := executePrimaryOperatorStep(executor, source, "tsk_1", host, nil, &snapshot, &ui); err != nil {
		t.Fatalf("second primary action: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("expected two sequential executor calls, got %#v", executor.calls)
	}
}

func TestInitialUIStateStartsWithCalmDefaultChrome(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := initialUIState(now, WorkerPreferenceClaude)

	if ui.ShowInspector {
		t.Fatal("expected inspector to be hidden by default")
	}
	if ui.ShowProof {
		t.Fatal("expected activity strip to be hidden by default")
	}
	if ui.Focus != FocusWorker {
		t.Fatalf("expected worker focus, got %v", ui.Focus)
	}
	if ui.Session.WorkerPreference != WorkerPreferenceClaude {
		t.Fatalf("expected worker preference to be preserved, got %q", ui.Session.WorkerPreference)
	}
	if !ui.ObservedAt.Equal(now) {
		t.Fatalf("expected observed time to initialize, got %v want %v", ui.ObservedAt, now)
	}
}

func TestNextFocusCyclesVisiblePanes(t *testing.T) {
	ui := UIState{
		ShowInspector: true,
		ShowProof:     true,
		Focus:         FocusWorker,
	}
	if got := nextFocus(ui); got != FocusInspector {
		t.Fatalf("expected inspector focus, got %v", got)
	}
	ui.Focus = FocusInspector
	if got := nextFocus(ui); got != FocusActivity {
		t.Fatalf("expected activity focus, got %v", got)
	}
	ui.ShowInspector = false
	ui.Focus = FocusWorker
	if got := nextFocus(ui); got != FocusActivity {
		t.Fatalf("expected activity focus when inspector hidden, got %v", got)
	}
}

func TestRouteKeyForwardsInputToLiveWorkerHost(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 'a'); action != actionNone {
		t.Fatalf("expected no shell action for worker input, got %v", action)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "a" {
		t.Fatalf("expected worker input to be forwarded, got %#v", host.writes)
	}

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if !ui.EscapePrefix {
		t.Fatal("expected escape prefix to be armed")
	}

	if action := routeKey(&ui, host, 'q'); action != actionQuit {
		t.Fatalf("expected prefixed q to quit shell, got %v", action)
	}
}

func TestRouteKeyUsesPrefixedScratchAdoptionCommand(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'a'); action != actionStageScratchAdoption {
		t.Fatalf("expected staged-scratch action, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected prefixed adoption command to stay shell-local, got %#v", host.writes)
	}
}

func TestRouteKeyUsesPrefixedPendingDraftEditCommand(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "draft",
	}

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'e'); action != actionEnterPendingTaskMessageEdit {
		t.Fatalf("expected edit-draft action, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected prefixed edit command to stay shell-local, got %#v", host.writes)
	}
}

func TestRouteKeyUsesPrefixedPrimaryOperatorExecutionCommand(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 'n'); action != actionNone {
		t.Fatalf("expected raw n to pass through to live worker input, got %v", action)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "n" {
		t.Fatalf("expected raw n to reach worker input, got %#v", host.writes)
	}

	host.writes = nil
	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'n'); action != actionExecutePrimaryOperatorStep {
		t.Fatalf("expected prefixed execute-primary action, got %v", action)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected prefixed execute-primary command to stay shell-local, got %#v", host.writes)
	}
}

func TestPrefixedPrimaryOperatorExecutionDoesNotDoubleExecuteWhileBusy(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	executor := &stubPrimaryActionExecutor{
		startedCh: make(chan struct{}, 1),
		releaseCh: make(chan struct{}),
	}
	snapshot := Snapshot{
		TaskID: "tsk_1",
		OperatorExecutionPlan: &OperatorExecutionPlan{
			PrimaryStep: &OperatorExecutionStep{
				Action:         "START_LOCAL_RUN",
				CommandSurface: "DEDICATED",
				CommandHint:    "tuku run --task tsk_1 --action start",
			},
		},
	}
	ui := UIState{Focus: FocusWorker, Session: newSessionState(time.Unix(1710000000, 0).UTC())}
	done := make(chan primaryActionExecutionResult, 1)

	if err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done); err != nil {
		t.Fatalf("start primary operator step: %v", err)
	}
	<-executor.startedCh

	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 'n'); action != actionExecutePrimaryOperatorStep {
		t.Fatalf("expected prefixed execute-primary action, got %v", action)
	}
	err := startPrimaryOperatorStepExecution(executor, "tsk_1", snapshot, &ui, done)
	if err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("expected duplicate in-flight rejection, got %v", err)
	}

	close(executor.releaseCh)
	result := <-done
	if err := completePrimaryOperatorStepExecution(&stubSnapshotSource{snapshot: snapshot}, "tsk_1", host, nil, &snapshot, &ui, result); err != nil {
		t.Fatalf("complete primary operator step: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("expected exactly one executor call, got %#v", executor.calls)
	}
}

func TestRouteKeyExplainsUnavailableWorkerInput(t *testing.T) {
	host := &stubHost{
		canInput: false,
		status:   HostStatus{Mode: HostModeTranscript, State: HostStateFallback, Label: "transcript fallback"},
	}
	ui := UIState{Focus: FocusWorker}

	if action := routeKey(&ui, host, 'z'); action != actionNone {
		t.Fatalf("expected no shell action for unavailable worker input, got %v", action)
	}
	if ui.LastError == "" {
		t.Fatal("expected unavailable input message")
	}
}

func TestStagePendingTaskMessageFromLocalScratch(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{Session: newSessionState(now)}
	err := stagePendingTaskMessageFromLocalScratch(&ui, Snapshot{
		TaskID: "tsk_1",
		LocalScratch: &LocalScratchContext{
			RepoRoot: "/tmp/repo",
			Notes: []ConversationItem{
				{Role: "user", Body: "Plan project structure", CreatedAt: now},
				{Role: "user", Body: "List initial requirements", CreatedAt: now},
			},
		},
	})
	if err != nil {
		t.Fatalf("stage pending task message: %v", err)
	}
	if ui.PendingTaskMessageSource != "local_scratch_adoption" {
		t.Fatalf("expected local scratch adoption source, got %q", ui.PendingTaskMessageSource)
	}
	if ui.PendingTaskMessage == "" || !strings.Contains(ui.PendingTaskMessage, "Plan project structure") {
		t.Fatalf("expected staged draft to include scratch notes, got %q", ui.PendingTaskMessage)
	}
	if len(ui.Session.Journal) == 0 || ui.Session.Journal[len(ui.Session.Journal)-1].Type != SessionEventPendingMessageStaged {
		t.Fatalf("expected staged journal event, got %#v", ui.Session.Journal)
	}
}

func TestEnterPendingTaskMessageEditModeRequiresPendingDraft(t *testing.T) {
	ui := UIState{Session: newSessionState(time.Unix(1710000000, 0).UTC())}
	if err := enterPendingTaskMessageEditMode(&ui); err == nil {
		t.Fatal("expected missing pending draft error")
	}
}

func TestPendingTaskMessageEditInputStaysLocalUntilSaved(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "Draft",
		Session:            newSessionState(now),
	}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	if action := routeKey(&ui, host, '!'); action != actionNone {
		t.Fatalf("expected local edit input, got %v", action)
	}
	if action := routeKey(&ui, host, '\n'); action != actionNone {
		t.Fatalf("expected newline edit input, got %v", action)
	}
	if action := routeKey(&ui, host, 'X'); action != actionNone {
		t.Fatalf("expected local edit input, got %v", action)
	}
	if ui.PendingTaskMessage != "Draft" {
		t.Fatalf("expected saved draft to stay unchanged during edit, got %q", ui.PendingTaskMessage)
	}
	if ui.PendingTaskMessageEditBuffer != "Draft!\nX" {
		t.Fatalf("expected edit buffer to change locally, got %q", ui.PendingTaskMessageEditBuffer)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected edit-mode input to stay local, got %#v", host.writes)
	}
}

func TestPendingTaskMessageEditBackspaceRemovesLastRuneSafely(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "界",
		Session:            newSessionState(now),
	}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	if action := routeKey(&ui, host, 0x7f); action != actionNone {
		t.Fatalf("expected local backspace handling, got %v", action)
	}
	if ui.PendingTaskMessageEditBuffer != "" {
		t.Fatalf("expected multibyte rune to be removed cleanly, got %q", ui.PendingTaskMessageEditBuffer)
	}
	if len(host.writes) != 0 {
		t.Fatalf("expected backspace to stay local in edit mode, got %#v", host.writes)
	}
}

func TestSendPendingTaskMessageMakesDraftCanonicalOnlyOnExplicitSend(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "Explicitly adopt these notes",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err != nil {
		t.Fatalf("send pending task message: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one sent message, got %#v", sender.sent)
	}
	if sender.sent[0].taskID != "tsk_1" || sender.sent[0].message != "Explicitly adopt these notes" {
		t.Fatalf("unexpected sent payload: %#v", sender.sent[0])
	}
	if ui.PendingTaskMessage != "" || ui.PendingTaskMessageSource != "" {
		t.Fatalf("expected pending draft to clear after explicit send, got %+v", ui)
	}
	if len(ui.Session.Journal) == 0 || ui.Session.Journal[len(ui.Session.Journal)-1].Type != SessionEventPendingMessageSent {
		t.Fatalf("expected sent journal event, got %#v", ui.Session.Journal)
	}
}

func TestSendPendingTaskMessageUsesEditedDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "Original draft",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	ui.PendingTaskMessageEditBuffer = "Edited draft"
	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err != nil {
		t.Fatalf("send edited draft: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].message != "Edited draft" {
		t.Fatalf("expected edited draft to be sent, got %#v", sender.sent)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected edit mode to end after explicit send")
	}
}

func TestSendPendingTaskMessagePreservesMultilineDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "line 1\n\nline 2\n",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err != nil {
		t.Fatalf("send multiline draft: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0].message != "line 1\n\nline 2\n" {
		t.Fatalf("expected multiline draft to be preserved, got %#v", sender.sent)
	}
}

func TestSendPendingTaskMessageRejectsEffectivelyEmptyDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage: " \n\t ",
		Session:            newSessionState(now),
	}
	sender := &stubTaskMessageSender{}

	if err := sendPendingTaskMessage(sender, "tsk_1", &ui); err == nil {
		t.Fatal("expected effectively empty draft to be rejected")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected no send for empty draft, got %#v", sender.sent)
	}
}

func TestSavePendingTaskMessageEditRestoresWorkerRouting(t *testing.T) {
	host := &stubHost{
		canInput: true,
		status:   HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"},
	}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		Focus:              FocusWorker,
		PendingTaskMessage: "Draft",
		Session:            newSessionState(now),
	}

	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	if action := routeKey(&ui, host, '1'); action != actionNone {
		t.Fatalf("expected edit-mode local input, got %v", action)
	}
	if action := routeKey(&ui, host, 0x07); action != actionNone {
		t.Fatalf("expected prefix arm only, got %v", action)
	}
	if action := routeKey(&ui, host, 's'); action != actionSavePendingTaskMessageEdit {
		t.Fatalf("expected save-edit action, got %v", action)
	}
	if err := savePendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("save edit mode: %v", err)
	}
	if ui.PendingTaskMessage != "Draft1" {
		t.Fatalf("expected saved draft to include edit, got %q", ui.PendingTaskMessage)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected edit mode to end after save")
	}
	if action := routeKey(&ui, host, 'z'); action != actionNone {
		t.Fatalf("expected worker input after save, got %v", action)
	}
	if len(host.writes) != 1 || string(host.writes[0]) != "z" {
		t.Fatalf("expected normal worker routing after save, got %#v", host.writes)
	}
}

func TestCancelPendingTaskMessageEditRestoresSavedDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage: "Saved draft",
		Session:            newSessionState(now),
	}
	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	ui.PendingTaskMessageEditBuffer = "Edited draft"
	if err := cancelPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("cancel edit mode: %v", err)
	}
	if ui.PendingTaskMessage != "Saved draft" {
		t.Fatalf("expected saved draft to be restored, got %q", ui.PendingTaskMessage)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected edit mode to be inactive after cancel")
	}
}

func TestClearPendingTaskMessageClearsStagedAndEditState(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{
		PendingTaskMessage:       "Saved draft",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}
	if err := enterPendingTaskMessageEditMode(&ui); err != nil {
		t.Fatalf("enter edit mode: %v", err)
	}
	ui.PendingTaskMessageEditBuffer = "Edited draft"

	clearPendingTaskMessage(&ui)

	if ui.PendingTaskMessage != "" || ui.PendingTaskMessageSource != "" {
		t.Fatalf("expected staged draft to be cleared, got %+v", ui)
	}
	if ui.PendingTaskMessageEditMode || ui.PendingTaskMessageEditBuffer != "" || ui.PendingTaskMessageEditOriginal != "" {
		t.Fatalf("expected edit state to be cleared, got %+v", ui)
	}
}

func TestReloadShellSnapshotDoesNotRebuildClearedDraft(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	source := &stubSnapshotSource{
		snapshot: Snapshot{
			TaskID: "tsk_1",
			LocalScratch: &LocalScratchContext{
				RepoRoot: "/tmp/repo",
				Notes: []ConversationItem{
					{Role: "user", Body: "Plan project structure", CreatedAt: now},
				},
			},
		},
	}
	host := &stubHost{}
	snapshot := Snapshot{}
	ui := UIState{
		PendingTaskMessage:       "Staged draft",
		PendingTaskMessageSource: "local_scratch_adoption",
		Session:                  newSessionState(now),
	}

	clearPendingTaskMessage(&ui)

	if err := reloadShellSnapshot(source, "tsk_1", host, nil, &snapshot, &ui, true); err != nil {
		t.Fatalf("reload shell snapshot: %v", err)
	}
	if ui.PendingTaskMessage != "" || ui.PendingTaskMessageSource != "" {
		t.Fatalf("expected refresh to leave cleared draft empty, got %+v", ui)
	}
	if ui.PendingTaskMessageEditMode {
		t.Fatal("expected refresh to leave edit mode inactive")
	}
	if snapshot.TaskID != "tsk_1" {
		t.Fatalf("expected refreshed snapshot to load, got %+v", snapshot)
	}
}

func TestApplyHostResizeUsesWorkerPaneDimensions(t *testing.T) {
	host := &stubHost{status: HostStatus{Mode: HostModeCodexPTY, State: HostStateLive, Label: "codex live"}}
	ui := UIState{ShowInspector: true, ShowProof: true}

	if !applyHostResize(host, 120, 32, ui) {
		t.Fatal("expected resize to be propagated")
	}
	if len(host.resizes) != 1 {
		t.Fatalf("expected one resize call, got %d", len(host.resizes))
	}
	if host.resizes[0][0] <= 0 || host.resizes[0][1] <= 0 {
		t.Fatalf("expected positive resize dimensions, got %#v", host.resizes[0])
	}
}

func TestCaptureHostLifecycleRecordsJournalAndPersistsMilestones(t *testing.T) {
	sink := &stubLifecycleSink{}
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{Session: newSessionState(now)}

	live := HostStatus{
		Mode:      HostModeCodexPTY,
		State:     HostStateLive,
		Label:     "codex live",
		InputLive: true,
		Width:     80,
		Height:    24,
	}
	captureHostLifecycle(context.Background(), sink, "tsk_1", ui.Session.SessionID, &ui, HostStatus{}, live)

	if len(ui.Session.Journal) != 1 || ui.Session.Journal[0].Type != SessionEventHostLive {
		t.Fatalf("expected live host journal entry, got %#v", ui.Session.Journal)
	}
	if len(sink.records) != 1 || sink.records[0].kind != PersistedLifecycleHostStarted {
		t.Fatalf("expected persisted host-start record, got %#v", sink.records)
	}

	exitCode := 9
	exited := HostStatus{
		Mode:      HostModeCodexPTY,
		State:     HostStateExited,
		Label:     "codex exited",
		InputLive: false,
		ExitCode:  &exitCode,
	}
	captureHostLifecycle(context.Background(), sink, "tsk_1", ui.Session.SessionID, &ui, live, exited)

	if len(ui.Session.Journal) != 2 || ui.Session.Journal[1].Type != SessionEventHostExited {
		t.Fatalf("expected host-exited journal entry, got %#v", ui.Session.Journal)
	}
	if len(sink.records) != 2 || sink.records[1].kind != PersistedLifecycleHostExited {
		t.Fatalf("expected persisted host-exit record, got %#v", sink.records)
	}
}

func TestFallbackTransitionPreservesSessionIdentity(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	ui := UIState{Session: newSessionState(now)}
	sessionID := ui.Session.SessionID
	exitCode := 7
	current := &stubHost{
		status: HostStatus{
			Mode:      HostModeCodexPTY,
			State:     HostStateExited,
			Label:     "codex exited",
			ExitCode:  &exitCode,
			InputLive: false,
		},
	}
	fallback := NewTranscriptHost()

	nextHost, _, changed := transitionExitedHost(context.Background(), current, fallback, Snapshot{TaskID: "tsk_2"})
	if !changed {
		t.Fatal("expected fallback transition")
	}

	captureHostLifecycle(context.Background(), nil, "tsk_2", ui.Session.SessionID, &ui, current.Status(), nextHost.Status())

	if ui.Session.SessionID != sessionID {
		t.Fatalf("expected session identity to survive fallback, got %q want %q", ui.Session.SessionID, sessionID)
	}
	if len(ui.Session.Journal) != 1 || ui.Session.Journal[0].Type != SessionEventFallbackActivated {
		t.Fatalf("expected fallback journal entry, got %#v", ui.Session.Journal)
	}
}
