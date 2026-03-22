package shell

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func BuildViewModel(snapshot Snapshot, ui UIState, host WorkerHost, width int, height int) ViewModel {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 32
	}
	if host == nil {
		host = NewTranscriptHost()
		host.UpdateSnapshot(snapshot)
	}

	header := HeaderView{
		Title:      "Tuku",
		TaskLabel:  displayTaskLabel(snapshot.TaskID),
		Phase:      nonEmpty(snapshot.Phase, "UNKNOWN"),
		Worker:     effectiveWorkerLabel(snapshot, host),
		Repo:       repoLabel(snapshot.Repo),
		Continuity: continuityLabel(snapshot),
	}

	layout := computeShellLayout(width, height, ui)
	bodyHeight := layout.bodyHeight
	workerWidth := layout.workerWidth
	inspectorWidth := layout.inspectorWidth
	if !layout.showInspector && ui.Focus == FocusInspector {
		ui.Focus = FocusWorker
	}
	if !layout.showProof && ui.Focus == FocusActivity {
		ui.Focus = FocusWorker
	}

	workerPane := buildWorkerPane(snapshot, ui, host, bodyHeight-1, workerWidth)

	var inspector *InspectorView
	if layout.showInspector && inspectorWidth > 0 {
		inspector = &InspectorView{
			Title:   "inspector",
			Focused: ui.Focus == FocusInspector,
			Sections: []SectionView{
				{Title: "operator", Lines: inspectorOperator(snapshot, ui)},
				{Title: "worker session", Lines: inspectorWorkerSession(host, ui.Session)},
				{Title: "brief", Lines: inspectorBrief(snapshot)},
				{Title: "intent", Lines: inspectorIntent(snapshot)},
				{Title: "pending message", Lines: inspectorPendingMessage(snapshot, ui)},
				{Title: "checkpoint", Lines: inspectorCheckpoint(snapshot)},
				{Title: "handoff", Lines: inspectorHandoff(snapshot)},
				{Title: "launch", Lines: inspectorLaunch(snapshot)},
				{Title: "run", Lines: inspectorRun(snapshot)},
				{Title: "proof", Lines: inspectorProof(snapshot)},
			},
		}
	}

	var strip *StripView
	if layout.showProof {
		strip = &StripView{
			Title:   "activity",
			Focused: ui.Focus == FocusActivity,
			Lines:   buildActivityLines(snapshot, host, ui),
		}
	}

	vm := ViewModel{
		Header:     header,
		WorkerPane: workerPane,
		Inspector:  inspector,
		ProofStrip: strip,
		Footer:     footerText(snapshot, ui, host),
		Layout:     layout,
	}

	if ui.ShowHelp {
		vm.Overlay = &OverlayView{
			Title: "help",
			Lines: []string{
				"q quit shell",
				"i toggle inspector",
				"p toggle activity strip",
				"r refresh shell state",
				"n execute the current primary operator step when Tuku has a direct command path",
				"s toggle compact status card",
				"h toggle help",
				"tab cycle focus",
				"a stage a local draft from surfaced scratch",
				"e edit the staged local draft",
				"m send the current draft through Tuku",
				"x clear the local draft",
				"while editing: type in the worker pane",
				"ctrl-g s save and leave edit mode",
				"ctrl-g c cancel edits and restore the staged draft",
				"ctrl-g next-key when the live worker pane is focused",
				"",
				"Scratch stays local-only. The staged draft stays shell-local until you explicitly send it with m.",
			},
		}
	} else if ui.ShowStatus {
		lines := []string{
			fmt.Sprintf("task %s", displayTaskLabel(snapshot.TaskID)),
			fmt.Sprintf("new shell session %s", ui.Session.SessionID),
			fmt.Sprintf("phase %s", nonEmpty(snapshot.Phase, "UNKNOWN")),
			fmt.Sprintf("worker %s", effectiveWorkerLabel(snapshot, host)),
			fmt.Sprintf("host %s", hostStatusLine(snapshot, ui, host)),
			fmt.Sprintf("repo %s", repoLabel(snapshot.Repo)),
			fmt.Sprintf("continuity %s", continuityLabel(snapshot)),
			fmt.Sprintf("recovery %s", operatorStateLabel(snapshot)),
			fmt.Sprintf("next %s", operatorActionLabel(snapshot)),
			fmt.Sprintf("readiness %s", operatorReadinessLine(snapshot)),
			fmt.Sprintf("launch %s", launchControlLine(snapshot)),
			fmt.Sprintf("branch %s", activeBranchLine(snapshot)),
			fmt.Sprintf("local run %s", localRunFinalizationLine(snapshot)),
			fmt.Sprintf("local resume %s", localResumeLine(snapshot)),
			fmt.Sprintf("authority %s", operatorAuthorityLine(snapshot)),
			fmt.Sprintf("decision %s", operatorDecisionHeadline(snapshot)),
			fmt.Sprintf("plan %s", operatorExecutionPlanLine(snapshot)),
			fmt.Sprintf("command %s", operatorExecutionCommand(snapshot)),
			fmt.Sprintf("progress %s", primaryActionInFlightLine(ui)),
			fmt.Sprintf("guidance %s", operatorDecisionGuidance(snapshot)),
			fmt.Sprintf("caution %s", operatorDecisionIntegrity(snapshot)),
		}
		if result := operatorActionResultHeadline(ui); result != "n/a" {
			lines = append(lines, fmt.Sprintf("result %s", result))
			for _, delta := range operatorActionResultDeltas(ui, 3) {
				lines = append(lines, fmt.Sprintf("delta %s", delta))
			}
			if next := operatorActionResultNextStep(ui); next != "n/a" {
				lines = append(lines, fmt.Sprintf("new next %s", next))
			}
		}
		lines = append(lines,
			fmt.Sprintf("reason %s", strongestOperatorReason(snapshot)),
			fmt.Sprintf("registry %s", sessionRegistrySummary(ui.Session)),
			fmt.Sprintf("draft %s", pendingMessageSummary(snapshot, ui)),
			fmt.Sprintf("checkpoint %s", checkpointLine(snapshot)),
			fmt.Sprintf("handoff %s", handoffLine(snapshot)),
			sessionPriorLine(ui.Session),
			"",
			latestCanonicalLine(snapshot),
		)
		vm.Overlay = &OverlayView{
			Title: "status",
			Lines: lines,
		}
	}

	return vm
}

func buildWorkerPane(snapshot Snapshot, ui UIState, host WorkerHost, height int, width int) PaneView {
	if ui.PendingTaskMessageEditMode {
		return PaneView{
			Title:   "worker pane | pending message editor",
			Lines:   pendingTaskMessageEditorLines(ui, height, width),
			Focused: ui.Focus == FocusWorker,
		}
	}
	hostHeight := height
	lines := []string(nil)
	if summary := workerPaneSummaryLine(snapshot, ui, host); summary != "" && height >= 5 {
		hostHeight = max(1, height-1)
		lines = append(lines, summary)
	}
	lines = append(lines, host.Lines(hostHeight, width)...)
	return PaneView{
		Title:   host.Title(),
		Lines:   lines,
		Focused: ui.Focus == FocusWorker,
	}
}

func shortTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if len(taskID) <= 10 {
		return taskID
	}
	return taskID[:10]
}

func displayTaskLabel(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "no-task"
	}
	return shortTaskID(taskID)
}

func workerLabel(snapshot Snapshot) string {
	return snapshotWorkerLabel(snapshot)
}

func effectiveWorkerLabel(snapshot Snapshot, host WorkerHost) string {
	if isScratchIntakeSnapshot(snapshot) {
		return snapshotWorkerLabel(snapshot)
	}
	if host != nil {
		if label := strings.TrimSpace(host.WorkerLabel()); label != "" {
			return label
		}
		status := host.Status()
		if label := strings.TrimSpace(status.Label); label != "" {
			return label
		}
	}
	return snapshotWorkerLabel(snapshot)
}

func snapshotWorkerLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "scratch intake"
	}
	if snapshot.Run != nil {
		if snapshot.Run.Status == "RUNNING" {
			return fmt.Sprintf("%s active", nonEmpty(snapshot.Run.WorkerKind, "worker"))
		}
		return fmt.Sprintf("%s last", nonEmpty(snapshot.Run.WorkerKind, "worker"))
	}
	if snapshot.Handoff != nil && snapshot.Handoff.TargetWorker != "" {
		return fmt.Sprintf("%s handoff", snapshot.Handoff.TargetWorker)
	}
	return "none"
}

func repoLabel(anchor RepoAnchor) string {
	if strings.TrimSpace(anchor.RepoRoot) == "" {
		return "no-repo"
	}
	name := filepath.Base(anchor.RepoRoot)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = anchor.RepoRoot
	}
	branch := nonEmpty(anchor.Branch, "detached")
	dirty := ""
	if anchor.WorkingTreeDirty {
		dirty = " dirty"
	}
	return fmt.Sprintf("%s@%s%s", name, branch, dirty)
}

func continuityLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Recovery != nil {
		switch snapshot.Recovery.Class {
		case "READY_NEXT_RUN":
			if snapshot.Recovery.ReadyForNextRun {
				return "ready"
			}
		case "CONTINUE_EXECUTION_REQUIRED":
			return "continue-pending"
		case "INTERRUPTED_RUN_RECOVERABLE":
			return "recoverable"
		case "ACCEPTED_HANDOFF_LAUNCH_READY":
			if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "FAILED" && snapshot.LaunchControl.RetryDisposition == "ALLOWED" {
				return "launch-retry"
			}
			return "handoff-ready"
		case "HANDOFF_LAUNCH_PENDING_OUTCOME":
			return "launch-pending"
		case "HANDOFF_LAUNCH_COMPLETED":
			return "launched"
		case "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED":
			return "review"
		case "FAILED_RUN_REVIEW_REQUIRED", "VALIDATION_REVIEW_REQUIRED":
			return "review"
		case "DECISION_REQUIRED", "BLOCKED_DRIFT":
			return "decision"
		case "REBRIEF_REQUIRED":
			return "rebrief"
		case "REPAIR_REQUIRED":
			return "repair"
		case "COMPLETED_NO_ACTION":
			return "complete"
		}
	}
	if snapshot.Checkpoint != nil && snapshot.Checkpoint.IsResumable {
		return "resumable"
	}
	switch snapshot.Phase {
	case "BLOCKED", "FAILED":
		return "blocked"
	case "VALIDATING":
		return "validating"
	default:
		return strings.ToLower(nonEmpty(snapshot.Status, "active"))
	}
}

func inspectorBrief(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"No repo-backed brief exists in scratch intake mode.",
			"Use this session to frame the project, scope milestones, and prepare for repository setup.",
		}
	}
	if snapshot.Brief == nil {
		return []string{"No brief persisted yet."}
	}
	lines := []string{
		truncateWithEllipsis(snapshot.Brief.Objective, 48),
		fmt.Sprintf("action %s", nonEmpty(snapshot.Brief.NormalizedAction, "n/a")),
	}
	if len(snapshot.Brief.Constraints) > 0 {
		lines = append(lines, fmt.Sprintf("constraints %s", strings.Join(snapshot.Brief.Constraints, ", ")))
	}
	if len(snapshot.Brief.DoneCriteria) > 0 {
		lines = append(lines, fmt.Sprintf("done %s", strings.Join(snapshot.Brief.DoneCriteria, ", ")))
	}
	return lines
}

func inspectorIntent(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"Local scratch intake session.",
			"Plan the work here before cloning or initializing a repository.",
		}
	}
	if snapshot.IntentSummary == "" {
		return []string{"No intent summary."}
	}
	return []string{snapshot.IntentSummary}
}

func inspectorCheckpoint(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No checkpoint exists because this session is not repo-backed."}
	}
	if snapshot.Checkpoint == nil {
		return []string{"No checkpoint yet."}
	}
	lines := []string{
		fmt.Sprintf("%s | %s", shortTaskID(snapshot.Checkpoint.ID), strings.ToLower(snapshot.Checkpoint.Trigger)),
	}
	lines = append(lines, fmt.Sprintf("raw resumable %s", yesNo(snapshot.Checkpoint.IsResumable)))
	if snapshot.Checkpoint.ResumeDescriptor != "" {
		lines = append(lines, snapshot.Checkpoint.ResumeDescriptor)
	}
	return lines
}

func inspectorHandoff(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No handoff packet exists in local scratch intake mode."}
	}
	if snapshot.Handoff == nil {
		if snapshot.Resolution == nil {
			return []string{"No handoff packet."}
		}
		lines := []string{"No active handoff packet."}
		lines = append(lines, fmt.Sprintf("resolution %s", strings.ToLower(strings.ReplaceAll(snapshot.Resolution.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.Resolution.Summary, 48))
		if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
			lines = append(lines, "continuity "+continuity)
		}
		return lines
	}
	lines := []string{
		fmt.Sprintf("%s -> %s (%s)", nonEmpty(snapshot.Handoff.SourceWorker, "unknown"), nonEmpty(snapshot.Handoff.TargetWorker, "unknown"), nonEmpty(snapshot.Handoff.Status, "unknown")),
	}
	if snapshot.Handoff.Mode != "" {
		lines = append(lines, fmt.Sprintf("mode %s", snapshot.Handoff.Mode))
	}
	if snapshot.Handoff.Reason != "" {
		lines = append(lines, snapshot.Handoff.Reason)
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, "continuity "+continuity)
	}
	if snapshot.Acknowledgment != nil {
		lines = append(lines, fmt.Sprintf("ack %s", strings.ToLower(snapshot.Acknowledgment.Status)))
		lines = append(lines, truncateWithEllipsis(snapshot.Acknowledgment.Summary, 48))
	}
	if snapshot.FollowThrough != nil {
		lines = append(lines, fmt.Sprintf("follow-through %s", strings.ToLower(strings.ReplaceAll(snapshot.FollowThrough.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.FollowThrough.Summary, 48))
	}
	if snapshot.Resolution != nil {
		lines = append(lines, fmt.Sprintf("resolution %s", strings.ToLower(strings.ReplaceAll(snapshot.Resolution.Kind, "_", "-"))))
		lines = append(lines, truncateWithEllipsis(snapshot.Resolution.Summary, 48))
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State != "NOT_APPLICABLE" {
		lines = append(lines, "launch "+launchControlLine(snapshot))
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, "continuity "+continuity)
	}
	return lines
}

func inspectorLaunch(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No launch state exists in local scratch intake mode."}
	}
	if snapshot.Launch == nil && (snapshot.LaunchControl == nil || snapshot.LaunchControl.State == "NOT_APPLICABLE") {
		return []string{"No launch state."}
	}
	lines := []string{launchControlLine(snapshot)}
	if snapshot.Launch != nil {
		lines = append(lines, fmt.Sprintf("attempt %s | %s", shortTaskID(snapshot.Launch.AttemptID), strings.ToLower(nonEmpty(snapshot.Launch.Status, "unknown"))))
		if snapshot.Launch.LaunchID != "" {
			lines = append(lines, "launch id "+snapshot.Launch.LaunchID)
		}
		if snapshot.Launch.Summary != "" {
			lines = append(lines, truncateWithEllipsis(snapshot.Launch.Summary, 48))
		}
		if snapshot.Launch.ErrorMessage != "" {
			lines = append(lines, truncateWithEllipsis("error "+snapshot.Launch.ErrorMessage, 48))
		}
	}
	if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "COMPLETED" {
		lines = append(lines, "launcher invocation completed; downstream work not proven")
	}
	if continuity := handoffContinuityLine(snapshot); continuity != "n/a" {
		lines = append(lines, continuity)
	}
	if snapshot.HandoffContinuity != nil && snapshot.HandoffContinuity.State == "LAUNCH_COMPLETED_ACK_UNAVAILABLE" {
		lines = append(lines, "no usable acknowledgment captured; downstream work not proven")
	}
	return lines
}

func inspectorRun(snapshot Snapshot) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{"No execution run exists because this session has no task-backed orchestration state."}
	}
	if snapshot.Run == nil {
		return []string{"No run recorded."}
	}
	lines := []string{
		fmt.Sprintf("%s | %s", nonEmpty(snapshot.Run.WorkerKind, "worker"), snapshot.Run.Status),
	}
	if snapshot.Run.LastKnownSummary != "" {
		lines = append(lines, truncateWithEllipsis(snapshot.Run.LastKnownSummary, 48))
	}
	if snapshot.Run.InterruptionReason != "" {
		lines = append(lines, fmt.Sprintf("interrupt %s", snapshot.Run.InterruptionReason))
	}
	return lines
}

func inspectorWorkerSession(host WorkerHost, session SessionState) []string {
	if host == nil {
		return []string{"No worker host."}
	}
	status := host.Status()
	lines := []string{
		fmt.Sprintf("new shell session %s", session.SessionID),
		sessionRegistrySummary(session),
		fmt.Sprintf("preferred %s", nonEmpty(string(session.WorkerPreference), "auto")),
		fmt.Sprintf("resolved %s", nonEmpty(string(session.ResolvedWorker), "unknown")),
		fmt.Sprintf("worker session %s", nonEmpty(session.WorkerSessionID, "none")),
		fmt.Sprintf("attach %s", nonEmpty(string(session.AttachCapability), "none")),
		fmt.Sprintf("mode %s", nonEmpty(string(status.Mode), "unknown")),
		fmt.Sprintf("state %s", nonEmpty(string(status.State), "unknown")),
	}
	if !session.StartedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("started %s", session.StartedAt.Format("15:04:05")))
	}
	if status.InputLive {
		lines = append(lines, "input live")
	} else {
		lines = append(lines, "input disabled")
	}
	if status.Width > 0 && status.Height > 0 {
		lines = append(lines, fmt.Sprintf("pane %dx%d", status.Width, status.Height))
	}
	if status.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("exit code %d", *status.ExitCode))
	}
	if note := strings.TrimSpace(status.Note); note != "" {
		lines = append(lines, truncateWithEllipsis(note, 64))
	}
	if session.PriorPersistedSummary != "" {
		lines = append(lines, truncateWithEllipsis("previous persisted shell outcome "+session.PriorPersistedSummary, 64))
	}
	for _, evt := range recentSessionEvents(session, 2) {
		lines = append(lines, fmt.Sprintf("%s %s", evt.CreatedAt.Format("15:04"), truncateWithEllipsis(evt.Summary, 48)))
	}
	return lines
}

func inspectorProof(snapshot Snapshot) []string {
	if len(snapshot.RecentProofs) == 0 {
		return []string{"No proof events yet."}
	}
	lines := make([]string, 0, min(4, len(snapshot.RecentProofs)))
	limit := min(4, len(snapshot.RecentProofs))
	for _, evt := range snapshot.RecentProofs[:limit] {
		lines = append(lines, fmt.Sprintf("%s %s", evt.Timestamp.Format("15:04"), evt.Summary))
	}
	return lines
}

func inspectorOperator(snapshot Snapshot, ui UIState) []string {
	if isScratchIntakeSnapshot(snapshot) {
		return []string{
			"Local-only scratch intake session.",
			"No task-backed recovery or launch-control state exists here.",
		}
	}
	lines := []string{
		"state " + operatorStateLabel(snapshot),
		"next " + operatorActionLabel(snapshot),
		"readiness " + operatorReadinessLine(snapshot),
	}
	if branch := activeBranchLine(snapshot); branch != "n/a" {
		lines = append(lines, "branch "+branch)
	}
	if localRun := localRunFinalizationLine(snapshot); localRun != "n/a" {
		lines = append(lines, "local run "+localRun)
	}
	if localResume := localResumeLine(snapshot); localResume != "n/a" {
		lines = append(lines, "local resume "+localResume)
	}
	if authority := operatorAuthorityLine(snapshot); authority != "n/a" {
		lines = append(lines, "authority "+authority)
	}
	if decision := operatorDecisionHeadline(snapshot); decision != "n/a" {
		lines = append(lines, "decision "+decision)
	}
	if plan := operatorExecutionPlanLine(snapshot); plan != "n/a" {
		lines = append(lines, "plan "+plan)
	}
	if command := operatorExecutionCommand(snapshot); command != "n/a" {
		lines = append(lines, "command "+truncateWithEllipsis(command, 64))
	}
	if progress := primaryActionInFlightLine(ui); progress != "n/a" {
		lines = append(lines, "progress "+truncateWithEllipsis(progress, 64))
	}
	if guidance := operatorDecisionGuidance(snapshot); guidance != "n/a" {
		lines = append(lines, "guidance "+truncateWithEllipsis(guidance, 64))
	}
	if caution := operatorDecisionIntegrity(snapshot); caution != "n/a" {
		lines = append(lines, "caution "+truncateWithEllipsis(caution, 64))
	}
	if result := operatorActionResultHeadline(ui); result != "n/a" {
		lines = append(lines, "result "+truncateWithEllipsis(result, 64))
	}
	if receipt := latestOperatorReceiptLine(snapshot); receipt != "n/a" {
		lines = append(lines, "receipt "+truncateWithEllipsis(receipt, 64))
	}
	for _, delta := range operatorActionResultDeltas(ui, 3) {
		lines = append(lines, "delta "+truncateWithEllipsis(delta, 64))
	}
	if next := operatorActionResultNextStep(ui); next != "n/a" {
		lines = append(lines, "new next "+truncateWithEllipsis(next, 64))
	}
	if launch := launchControlLine(snapshot); launch != "n/a" {
		lines = append(lines, "launch "+launch)
	}
	if reason := strongestOperatorReason(snapshot); reason != "none" {
		lines = append(lines, "reason "+truncateWithEllipsis(reason, 64))
	}
	return lines
}

func inspectorPendingMessage(snapshot Snapshot, ui UIState) []string {
	if ui.PendingTaskMessageEditMode {
		lines := []string{
			"Editing the staged local draft.",
			pendingMessageSummary(snapshot, ui),
			"Typing changes only the shell-local draft. Nothing here is canonical until you explicitly send it with m.",
		}
		for _, line := range wrapText(truncateWithEllipsis(currentPendingTaskMessage(ui), 160), 48) {
			lines = append(lines, line)
		}
		lines = append(lines, "save with ctrl-g then s", "cancel with ctrl-g then c", "send with ctrl-g then m")
		return lines
	}
	if strings.TrimSpace(ui.PendingTaskMessage) != "" {
		lines := []string{
			"Local draft is staged and ready for review.",
			pendingMessageSummary(snapshot, ui),
			"Editing and clearing stay shell-local. Sending with m is the explicit step that makes this canonical.",
		}
		for _, line := range wrapText(truncateWithEllipsis(ui.PendingTaskMessage, 160), 48) {
			lines = append(lines, line)
		}
		lines = append(lines, "edit with e", "send with m", "clear with x")
		return lines
	}
	if snapshot.HasLocalScratchAdoption() {
		return []string{
			"Local scratch is available for explicit adoption.",
			"Stage a shell-local draft with a.",
			"Nothing becomes canonical until you explicitly send that draft with m.",
		}
	}
	return []string{"No pending task message."}
}

func buildActivityLines(snapshot Snapshot, host WorkerHost, ui UIState) []string {
	lines := []string{latestCanonicalLine(snapshot)}
	if progress := primaryActionInFlightLine(ui); progress != "n/a" {
		lines = append(lines, "progress "+truncateWithEllipsis(progress, 96))
	}
	if result := operatorActionResultHeadline(ui); result != "n/a" {
		lines = append(lines, "result  "+truncateWithEllipsis(result, 96))
		for _, delta := range operatorActionResultDeltas(ui, 2) {
			lines = append(lines, "delta   "+truncateWithEllipsis(delta, 96))
		}
		if next := operatorActionResultNextStep(ui); next != "n/a" {
			lines = append(lines, "next    "+truncateWithEllipsis(next, 96))
		}
	}
	for _, receipt := range recentOperatorReceiptLines(snapshot, 2) {
		lines = append(lines, receipt)
	}
	if host != nil {
		for _, line := range host.ActivityLines(3) {
			lines = append(lines, line)
		}
	}
	for _, evt := range recentSessionEvents(ui.Session, 3) {
		lines = append(lines, fmt.Sprintf("%s  %s", evt.CreatedAt.Format("15:04:05"), evt.Summary))
	}
	if len(snapshot.RecentProofs) > 0 {
		lines = append(lines, "")
		limit := min(3, len(snapshot.RecentProofs))
		for _, evt := range snapshot.RecentProofs[:limit] {
			lines = append(lines, fmt.Sprintf("%s  %s", evt.Timestamp.Format("15:04:05"), evt.Summary))
		}
	}
	return lines
}

func checkpointLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Checkpoint == nil {
		return "none"
	}
	label := shortTaskID(snapshot.Checkpoint.ID)
	if snapshot.Checkpoint.IsResumable {
		return label + " raw-resumable"
	}
	return label + " raw-not-resumable"
}

func handoffLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Handoff == nil {
		if snapshot.Resolution != nil {
			return "resolved history only"
		}
		return "none"
	}
	return fmt.Sprintf("%s %s->%s", snapshot.Handoff.Status, nonEmpty(snapshot.Handoff.SourceWorker, "unknown"), nonEmpty(snapshot.Handoff.TargetWorker, "unknown"))
}

func latestCanonicalLine(snapshot Snapshot) string {
	if strings.TrimSpace(snapshot.LatestCanonicalResponse) == "" {
		return "No canonical Tuku response persisted yet."
	}
	return truncateWithEllipsis(snapshot.LatestCanonicalResponse, 160)
}

func operatorStateLabel(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.Recovery == nil || strings.TrimSpace(snapshot.Recovery.Class) == "" {
		return continuityLabel(snapshot)
	}
	switch snapshot.Recovery.Class {
	case "READY_NEXT_RUN":
		return "ready next run"
	case "INTERRUPTED_RUN_RECOVERABLE":
		return "interrupted recoverable"
	case "ACCEPTED_HANDOFF_LAUNCH_READY":
		if snapshot.LaunchControl != nil && snapshot.LaunchControl.State == "FAILED" && snapshot.LaunchControl.RetryDisposition == "ALLOWED" {
			return "launch retry ready"
		}
		return "accepted handoff launch ready"
	case "HANDOFF_LAUNCH_PENDING_OUTCOME":
		return "launch pending"
	case "HANDOFF_LAUNCH_COMPLETED":
		return "launch completed"
	case "HANDOFF_FOLLOW_THROUGH_REVIEW_REQUIRED":
		return "handoff follow-through review required"
	case "FAILED_RUN_REVIEW_REQUIRED":
		return "failed run review required"
	case "VALIDATION_REVIEW_REQUIRED":
		return "validation review required"
	case "STALE_RUN_RECONCILIATION_REQUIRED":
		return "stale run reconciliation required"
	case "DECISION_REQUIRED":
		return "decision required"
	case "CONTINUE_EXECUTION_REQUIRED":
		return "continue confirmation required"
	case "BLOCKED_DRIFT":
		return "drift blocked"
	case "REBRIEF_REQUIRED":
		return "rebrief required"
	case "REPAIR_REQUIRED":
		return "repair required"
	case "COMPLETED_NO_ACTION":
		return "completed"
	default:
		return humanizeConstant(snapshot.Recovery.Class)
	}
}

func operatorActionLabel(snapshot Snapshot) string {
	if action := requiredNextOperatorAction(snapshot); action != "" && action != "NONE" {
		switch action {
		case "START_LOCAL_RUN":
			return "start next run"
		case "RECONCILE_STALE_RUN":
			return "reconcile stale run"
		case "INSPECT_FAILED_RUN":
			return "inspect failed run"
		case "REVIEW_VALIDATION_STATE":
			return "review validation state"
		case "MAKE_RESUME_DECISION":
			return "make resume decision"
		case "RESUME_INTERRUPTED_LINEAGE":
			return "resume interrupted run"
		case "FINALIZE_CONTINUE_RECOVERY":
			return "finalize continue"
		case "EXECUTE_REBRIEF":
			return "regenerate brief"
		case "LAUNCH_ACCEPTED_HANDOFF":
			return "launch accepted handoff"
		case "REVIEW_HANDOFF_FOLLOW_THROUGH":
			return "review handoff follow-through"
		case "RESOLVE_ACTIVE_HANDOFF":
			return "resolve active handoff"
		case "REPAIR_CONTINUITY":
			return "repair continuity"
		default:
			return humanizeConstant(action)
		}
	}
	if snapshot.Recovery == nil || strings.TrimSpace(snapshot.Recovery.Action) == "" {
		return "none"
	}
	switch snapshot.Recovery.Action {
	case "START_NEXT_RUN":
		return "start next run"
	case "RESUME_INTERRUPTED_RUN":
		return "resume interrupted run"
	case "LAUNCH_ACCEPTED_HANDOFF":
		return "launch accepted handoff"
	case "WAIT_FOR_LAUNCH_OUTCOME":
		return "wait for launch outcome"
	case "MONITOR_LAUNCHED_HANDOFF":
		return "monitor launched handoff"
	case "REVIEW_HANDOFF_FOLLOW_THROUGH":
		return "review handoff follow-through"
	case "INSPECT_FAILED_RUN":
		return "inspect failed run"
	case "REVIEW_VALIDATION_STATE":
		return "review validation state"
	case "RECONCILE_STALE_RUN":
		return "reconcile stale run"
	case "MAKE_RESUME_DECISION":
		return "make resume decision"
	case "EXECUTE_CONTINUE_RECOVERY":
		return "finalize continue"
	case "REPAIR_CONTINUITY":
		return "repair continuity"
	case "REGENERATE_BRIEF":
		return "regenerate brief"
	case "NONE":
		return "none"
	default:
		return humanizeConstant(snapshot.Recovery.Action)
	}
}

func operatorReadinessLine(snapshot Snapshot) string {
	nextRun := false
	handoffLaunch := false
	if snapshot.Recovery != nil {
		nextRun = snapshot.Recovery.ReadyForNextRun
		handoffLaunch = snapshot.Recovery.ReadyForHandoffLaunch
	}
	return fmt.Sprintf("next-run %s | handoff-launch %s", yesNo(nextRun), yesNo(handoffLaunch))
}

func strongestOperatorReason(snapshot Snapshot) string {
	if snapshot.OperatorDecision != nil {
		if reason := strings.TrimSpace(snapshot.OperatorDecision.PrimaryReason); reason != "" {
			return reason
		}
	}
	if snapshot.ActionAuthority != nil {
		if action := authorityFor(snapshot, snapshot.ActionAuthority.RequiredNextAction); action != nil {
			if reason := strings.TrimSpace(action.Reason); reason != "" {
				return reason
			}
		}
		for _, candidate := range []string{"LOCAL_MESSAGE_MUTATION", "CREATE_CHECKPOINT", "START_LOCAL_RUN"} {
			if action := authorityFor(snapshot, candidate); action != nil && action.State == "BLOCKED" {
				if reason := strings.TrimSpace(action.Reason); reason != "" {
					return reason
				}
			}
		}
	}
	if snapshot.ActiveBranch != nil {
		if reason := strings.TrimSpace(snapshot.ActiveBranch.Reason); reason != "" {
			return reason
		}
	}
	if snapshot.Recovery != nil {
		if reason := strings.TrimSpace(snapshot.Recovery.Reason); reason != "" {
			return reason
		}
		if len(snapshot.Recovery.Issues) > 0 {
			if msg := strings.TrimSpace(snapshot.Recovery.Issues[0].Message); msg != "" {
				return msg
			}
		}
	}
	if snapshot.LaunchControl != nil {
		if reason := strings.TrimSpace(snapshot.LaunchControl.Reason); reason != "" {
			return reason
		}
	}
	return "none"
}

func operatorDecisionHeadline(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.OperatorDecision == nil || strings.TrimSpace(snapshot.OperatorDecision.Headline) == "" {
		return "n/a"
	}
	return snapshot.OperatorDecision.Headline
}

func operatorDecisionGuidance(snapshot Snapshot) string {
	if snapshot.OperatorDecision == nil || strings.TrimSpace(snapshot.OperatorDecision.Guidance) == "" {
		return "n/a"
	}
	return snapshot.OperatorDecision.Guidance
}

func operatorDecisionIntegrity(snapshot Snapshot) string {
	if snapshot.OperatorDecision == nil || strings.TrimSpace(snapshot.OperatorDecision.IntegrityNote) == "" {
		return "n/a"
	}
	return snapshot.OperatorDecision.IntegrityNote
}

func operatorExecutionPlanLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return "n/a"
	}
	step := snapshot.OperatorExecutionPlan.PrimaryStep
	label := operatorActionDisplayName(step.Action)
	if label == "" {
		return "n/a"
	}
	prefix := operatorExecutionStatusLabel(step.Status)
	if prefix == "" {
		return label
	}
	return prefix + " " + label
}

func operatorExecutionCommand(snapshot Snapshot) string {
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return "n/a"
	}
	if command := strings.TrimSpace(snapshot.OperatorExecutionPlan.PrimaryStep.CommandHint); command != "" {
		return command
	}
	if action := strings.TrimSpace(snapshot.OperatorExecutionPlan.PrimaryStep.Action); action != "" {
		return "handle " + action
	}
	return "n/a"
}

func operatorActionResultHeadline(ui UIState) string {
	if ui.LastPrimaryActionResult == nil || strings.TrimSpace(ui.LastPrimaryActionResult.Summary) == "" {
		return "n/a"
	}
	result := strings.ToLower(strings.TrimSpace(ui.LastPrimaryActionResult.Outcome))
	if result == "" {
		result = "unknown"
	}
	line := result + " | " + ui.LastPrimaryActionResult.Summary
	if receipt := strings.TrimSpace(ui.LastPrimaryActionResult.ReceiptID); receipt != "" {
		line += " | " + shortTaskID(receipt)
	}
	return line
}

func operatorActionResultDeltas(ui UIState, limit int) []string {
	if ui.LastPrimaryActionResult == nil || len(ui.LastPrimaryActionResult.Deltas) == 0 {
		return nil
	}
	if limit <= 0 || limit >= len(ui.LastPrimaryActionResult.Deltas) {
		return append([]string{}, ui.LastPrimaryActionResult.Deltas...)
	}
	return append([]string{}, ui.LastPrimaryActionResult.Deltas[:limit]...)
}

func operatorActionResultDeltaLine(ui UIState) string {
	deltas := operatorActionResultDeltas(ui, 1)
	if len(deltas) == 0 {
		return "n/a"
	}
	return deltas[0]
}

func operatorActionResultNextStep(ui UIState) string {
	if ui.LastPrimaryActionResult == nil || strings.TrimSpace(ui.LastPrimaryActionResult.NextStep) == "" || strings.TrimSpace(ui.LastPrimaryActionResult.NextStep) == "none" {
		return "n/a"
	}
	return ui.LastPrimaryActionResult.NextStep
}

func latestOperatorReceiptLine(snapshot Snapshot) string {
	if snapshot.LatestOperatorStepReceipt == nil || strings.TrimSpace(snapshot.LatestOperatorStepReceipt.Summary) == "" {
		return "n/a"
	}
	line := strings.ToLower(strings.TrimSpace(snapshot.LatestOperatorStepReceipt.ResultClass))
	if line == "" {
		line = "recorded"
	}
	line += " | " + strings.TrimSpace(snapshot.LatestOperatorStepReceipt.Summary)
	if snapshot.LatestOperatorStepReceipt.ReceiptID != "" {
		line += " | " + shortTaskID(snapshot.LatestOperatorStepReceipt.ReceiptID)
	}
	return line
}

func recentOperatorReceiptLines(snapshot Snapshot, limit int) []string {
	if len(snapshot.RecentOperatorStepReceipts) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(snapshot.RecentOperatorStepReceipts) {
		limit = len(snapshot.RecentOperatorStepReceipts)
	}
	out := make([]string, 0, limit)
	for _, item := range snapshot.RecentOperatorStepReceipts[:limit] {
		summary := nonEmpty(strings.TrimSpace(item.Summary), operatorActionDisplayName(item.ActionHandle))
		out = append(out, fmt.Sprintf("%s  operator %s %s", item.CreatedAt.Format("15:04:05"), strings.ToLower(nonEmpty(item.ResultClass, "recorded")), truncateWithEllipsis(summary, 72)))
	}
	return out
}

func primaryActionInFlightLine(ui UIState) string {
	if ui.PrimaryActionInFlight == nil || strings.TrimSpace(ui.PrimaryActionInFlight.Action) == "" {
		return "n/a"
	}
	return "executing " + operatorActionDisplayName(ui.PrimaryActionInFlight.Action) + "..."
}

func primaryOperatorStepDirectlyExecutable(snapshot Snapshot) bool {
	if snapshot.OperatorExecutionPlan == nil || snapshot.OperatorExecutionPlan.PrimaryStep == nil {
		return false
	}
	return strings.TrimSpace(snapshot.OperatorExecutionPlan.PrimaryStep.CommandSurface) == "DEDICATED"
}

func requiredNextOperatorAction(snapshot Snapshot) string {
	if snapshot.ActionAuthority == nil {
		return ""
	}
	return strings.TrimSpace(snapshot.ActionAuthority.RequiredNextAction)
}

func authorityFor(snapshot Snapshot, action string) *OperatorActionAuthority {
	if snapshot.ActionAuthority == nil {
		return nil
	}
	action = strings.TrimSpace(action)
	for i := range snapshot.ActionAuthority.Actions {
		if snapshot.ActionAuthority.Actions[i].Action == action {
			return &snapshot.ActionAuthority.Actions[i]
		}
	}
	return nil
}

func operatorActionDisplayName(action string) string {
	switch strings.TrimSpace(action) {
	case "LOCAL_MESSAGE_MUTATION":
		return "send local message"
	case "CREATE_CHECKPOINT":
		return "create checkpoint"
	case "START_LOCAL_RUN":
		return "start local run"
	case "RECONCILE_STALE_RUN":
		return "reconcile stale run"
	case "INSPECT_FAILED_RUN":
		return "inspect failed run"
	case "REVIEW_VALIDATION_STATE":
		return "review validation"
	case "MAKE_RESUME_DECISION":
		return "make resume decision"
	case "RESUME_INTERRUPTED_LINEAGE":
		return "resume interrupted lineage"
	case "FINALIZE_CONTINUE_RECOVERY":
		return "finalize continue recovery"
	case "EXECUTE_REBRIEF":
		return "regenerate brief"
	case "LAUNCH_ACCEPTED_HANDOFF":
		return "launch accepted handoff"
	case "REVIEW_HANDOFF_FOLLOW_THROUGH":
		return "review handoff follow-through"
	case "RESOLVE_ACTIVE_HANDOFF":
		return "resolve active handoff"
	case "REPAIR_CONTINUITY":
		return "repair continuity"
	default:
		return humanizeConstant(action)
	}
}

func operatorExecutionStatusLabel(status string) string {
	switch strings.TrimSpace(status) {
	case "REQUIRED_NEXT":
		return "required"
	case "ALLOWED":
		return "allowed"
	case "BLOCKED":
		return "blocked"
	case "NOT_APPLICABLE":
		return "not applicable"
	default:
		return humanizeConstant(status)
	}
}

func operatorAuthorityLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if action := requiredNextOperatorAction(snapshot); action != "" && action != "NONE" {
		return "required " + operatorActionLabel(snapshot)
	}
	if blocked := authorityFor(snapshot, "LOCAL_MESSAGE_MUTATION"); blocked != nil && blocked.State == "BLOCKED" {
		if blocked.BlockingBranchClass == "HANDOFF_CLAUDE" && blocked.BlockingBranchRef != "" {
			return fmt.Sprintf("local mutation blocked by Claude handoff %s", shortTaskID(blocked.BlockingBranchRef))
		}
		return "local mutation blocked"
	}
	if blocked := authorityFor(snapshot, "START_LOCAL_RUN"); blocked != nil && blocked.State == "BLOCKED" {
		return "fresh run blocked"
	}
	return "n/a"
}

func activeBranchLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.ActiveBranch == nil || strings.TrimSpace(snapshot.ActiveBranch.Class) == "" {
		return "n/a"
	}
	switch snapshot.ActiveBranch.Class {
	case "LOCAL":
		switch snapshot.ActiveBranch.ActionabilityAnchor {
		case "BRIEF":
			return fmt.Sprintf("local via brief %s", shortTaskID(snapshot.ActiveBranch.ActionabilityAnchorRef))
		case "CHECKPOINT":
			return fmt.Sprintf("local via checkpoint %s", shortTaskID(snapshot.ActiveBranch.ActionabilityAnchorRef))
		default:
			return "local"
		}
	case "HANDOFF_CLAUDE":
		return fmt.Sprintf("Claude handoff %s", shortTaskID(snapshot.ActiveBranch.BranchRef))
	default:
		return humanizeConstant(snapshot.ActiveBranch.Class)
	}
}

func localResumeLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.LocalResume == nil || strings.TrimSpace(snapshot.LocalResume.State) == "" {
		return "n/a"
	}
	switch snapshot.LocalResume.State {
	case "ALLOWED":
		switch snapshot.LocalResume.Mode {
		case "RESUME_INTERRUPTED_LINEAGE":
			if snapshot.LocalResume.CheckpointID != "" {
				return fmt.Sprintf("allowed via checkpoint %s", shortTaskID(snapshot.LocalResume.CheckpointID))
			}
			return "allowed for interrupted lineage"
		default:
			return "allowed"
		}
	case "BLOCKED":
		if snapshot.LocalResume.BlockingBranchClass == "HANDOFF_CLAUDE" && snapshot.LocalResume.BlockingBranchRef != "" {
			return fmt.Sprintf("blocked by Claude handoff %s", shortTaskID(snapshot.LocalResume.BlockingBranchRef))
		}
		return "blocked"
	default:
		switch snapshot.LocalResume.Mode {
		case "FINALIZE_CONTINUE_RECOVERY":
			return "not applicable | finalize continue first"
		case "START_FRESH_NEXT_RUN":
			return "not applicable | start fresh next run"
		case "RESUME_INTERRUPTED_LINEAGE":
			return "not applicable"
		default:
			return "not applicable"
		}
	}
}

func localRunFinalizationLine(snapshot Snapshot) string {
	if isScratchIntakeSnapshot(snapshot) {
		return "local-only"
	}
	if snapshot.LocalRunFinalization == nil || strings.TrimSpace(snapshot.LocalRunFinalization.State) == "" {
		return "n/a"
	}
	switch snapshot.LocalRunFinalization.State {
	case "NO_RELEVANT_RUN":
		return "none"
	case "FINALIZED":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("finalized %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "finalized"
	case "INTERRUPTED_RECOVERABLE":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("interrupted recoverable %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "interrupted recoverable"
	case "INTERRUPTED_NEEDS_REPAIR":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("interrupted needs repair %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "interrupted needs repair"
	case "FAILED_REVIEW_REQUIRED":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("failed review required %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "failed review required"
	case "STALE_RECONCILIATION_REQUIRED":
		if snapshot.LocalRunFinalization.RunID != "" {
			return fmt.Sprintf("stale reconciliation required %s", shortTaskID(snapshot.LocalRunFinalization.RunID))
		}
		return "stale reconciliation required"
	default:
		return humanizeConstant(snapshot.LocalRunFinalization.State)
	}
}

func launchControlLine(snapshot Snapshot) string {
	if snapshot.LaunchControl == nil || snapshot.LaunchControl.State == "" || snapshot.LaunchControl.State == "NOT_APPLICABLE" {
		return "n/a"
	}
	state := ""
	switch snapshot.LaunchControl.State {
	case "NOT_REQUESTED":
		state = "not requested"
	case "REQUESTED_OUTCOME_UNKNOWN":
		state = "pending outcome unknown"
	case "COMPLETED":
		state = "completed (invocation only)"
	case "FAILED":
		state = "failed"
	default:
		state = humanizeConstant(snapshot.LaunchControl.State)
	}
	retry := "retry " + strings.ToLower(nonEmpty(snapshot.LaunchControl.RetryDisposition, "unknown"))
	return state + " | " + retry
}

func handoffContinuityLine(snapshot Snapshot) string {
	if snapshot.HandoffContinuity == nil || snapshot.HandoffContinuity.State == "" || snapshot.HandoffContinuity.State == "NOT_APPLICABLE" {
		return "n/a"
	}
	switch snapshot.HandoffContinuity.State {
	case "ACCEPTED_NOT_LAUNCHED":
		return "accepted, not launched"
	case "LAUNCH_PENDING_OUTCOME":
		return "launch pending, downstream outcome unknown"
	case "LAUNCH_FAILED_RETRYABLE":
		return "launch failed, retry allowed"
	case "LAUNCH_COMPLETED_ACK_CAPTURED":
		return "launch completed, acknowledgment captured, downstream unproven"
	case "LAUNCH_COMPLETED_ACK_UNAVAILABLE":
		return "launch completed, acknowledgment unavailable, downstream unproven"
	case "LAUNCH_COMPLETED_ACK_MISSING":
		return "launch completed, acknowledgment missing, continuity inconsistent"
	case "FOLLOW_THROUGH_PROOF_OF_LIFE":
		return "proof of life observed, completion unproven"
	case "FOLLOW_THROUGH_CONFIRMED":
		return "continuation confirmed, completion unproven"
	case "FOLLOW_THROUGH_UNKNOWN":
		return "follow-through still unknown"
	case "FOLLOW_THROUGH_STALLED":
		return "follow-through stalled, review required"
	case "RESOLVED":
		return "explicitly resolved, no downstream completion claim"
	default:
		return humanizeConstant(snapshot.HandoffContinuity.State)
	}
}

func operatorPaneCue(snapshot Snapshot) string {
	state := operatorStateLabel(snapshot)
	action := operatorActionLabel(snapshot)
	if state == "" || state == "local-only" {
		return state
	}
	if action == "" || action == "none" {
		return state
	}
	return state + " | next " + action
}

func pendingMessageSummary(snapshot Snapshot, ui UIState) string {
	if ui.PendingTaskMessageEditMode {
		switch ui.PendingTaskMessageSource {
		case "local_scratch_adoption":
			return "editing staged draft from local scratch"
		default:
			return "editing staged local draft"
		}
	}
	if strings.TrimSpace(ui.PendingTaskMessage) != "" {
		switch ui.PendingTaskMessageSource {
		case "local_scratch_adoption":
			return "staged draft from local scratch"
		default:
			return "staged local draft"
		}
	}
	if snapshot.HasLocalScratchAdoption() {
		return "local scratch available"
	}
	return "none"
}

func isScratchIntakeSnapshot(snapshot Snapshot) bool {
	return strings.TrimSpace(snapshot.TaskID) == "" &&
		strings.EqualFold(strings.TrimSpace(snapshot.Phase), "SCRATCH_INTAKE")
}

func footerText(snapshot Snapshot, ui UIState, host WorkerHost) string {
	parts := make([]string, 0, 12)
	if ui.Session.SessionID != "" {
		parts = append(parts, "session "+shortTaskID(ui.Session.SessionID))
	}
	if host != nil {
		status := host.Status()
		if status.InputLive {
			parts = append(parts, "worker live input")
		} else {
			parts = append(parts, "worker read-only")
		}
		if cue := footerHostCue(snapshot, ui, status); cue != "" {
			parts = append(parts, cue)
		}
	}
	if operator := footerOperatorCue(snapshot); operator != "" {
		parts = append(parts, operator)
	}
	if progress := primaryActionInFlightLine(ui); progress != "n/a" {
		parts = append(parts, progress)
	}
	parts = append(parts, "q quit", "h help", "i inspector", "p activity", "r refresh", "s status")
	if cue := footerExecutePrimaryCue(snapshot, ui, host); cue != "" {
		parts = append(parts, cue)
	}
	if host != nil && ui.Focus == FocusWorker && host.CanAcceptInput() {
		parts = append(parts, "ctrl-g shell commands")
	}
	if ui.EscapePrefix {
		parts = append(parts, "shell command armed")
	}
	if ui.PendingTaskMessageEditMode {
		parts = append(parts, "editing staged draft")
	} else if pending := strings.TrimSpace(ui.PendingTaskMessage); pending != "" {
		parts = append(parts, "staged local draft")
	} else if snapshot.HasLocalScratchAdoption() {
		parts = append(parts, "local scratch available")
	}
	if !ui.LastRefresh.IsZero() {
		parts = append(parts, "refreshed "+ui.LastRefresh.Format("15:04:05"))
	}
	if ui.LastError != "" {
		parts = append(parts, truncateWithEllipsis(ui.LastError, 80))
	} else if host != nil {
		if note := strings.TrimSpace(host.Status().Note); note != "" {
			parts = append(parts, truncateWithEllipsis(note, 80))
		}
	}
	return strings.Join(parts, " | ")
}

func footerExecutePrimaryCue(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if ui.PrimaryActionInFlight != nil {
		return ""
	}
	if !primaryOperatorStepDirectlyExecutable(snapshot) {
		return ""
	}
	if host != nil && ui.Focus == FocusWorker && host.CanAcceptInput() {
		return "ctrl-g n execute next step"
	}
	return "n execute next step"
}

func hostStatusLine(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if host == nil {
		return "none"
	}
	status := host.Status()
	line := fmt.Sprintf("%s / %s", nonEmpty(string(status.Mode), "unknown"), nonEmpty(string(status.State), "unknown"))
	if status.InputLive {
		line += " / input live"
	} else {
		line += " / input off"
	}
	if status.ExitCode != nil {
		line += fmt.Sprintf(" / exit %d", *status.ExitCode)
	}
	if temporal := hostTemporalStatus(snapshot, ui, status); temporal != "" {
		line += " / " + temporal
	}
	if note := strings.TrimSpace(status.Note); note != "" {
		line += " / " + truncateWithEllipsis(note, 48)
	}
	return line
}

func workerPaneSummaryLine(snapshot Snapshot, ui UIState, host WorkerHost) string {
	if host == nil {
		return ""
	}
	status := host.Status()
	label := nonEmpty(strings.TrimSpace(status.Label), strings.TrimSpace(string(status.Mode)))
	now := observedAt(ui)
	cue := workerPanePrimaryCue(snapshot, status, now)
	operatorCue := operatorPaneCue(snapshot)
	if operatorCue == "" {
		if cue == "" {
			return label
		}
		return label + " | " + cue
	}
	if cue == "" {
		return operatorCue + " | " + label
	}
	return operatorCue + " | " + label + " | " + cue
}

func workerPanePrimaryCue(snapshot Snapshot, status HostStatus, now time.Time) string {
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return "awaiting visible output"
		}
		return livePaneCue(status, now)
	case HostStateStarting:
		return "starting up"
	case HostStateExited, HostStateFailed:
		return inactivePaneCue(status)
	case HostStateFallback:
		return "historical transcript below | fallback active"
	case HostStateTranscriptOnly:
		if savedAt := latestTranscriptTimestamp(snapshot); !savedAt.IsZero() {
			return "historical transcript below | saved transcript " + savedAt.Format("15:04:05")
		}
		return "historical transcript below"
	}
	return ""
}

func livePaneCue(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.LastOutputAt)
	switch {
	case since <= 0:
		return "newest output at bottom"
	case since < 60*time.Second:
		return "newest output at bottom"
	case since < 2*time.Minute:
		return "newest output at bottom | quiet"
	default:
		return "newest output at bottom | quiet a while"
	}
}

func inactivePaneCue(status HostStatus) string {
	switch status.State {
	case HostStateFailed:
		return "newest captured output at bottom | worker failed"
	default:
		return "newest captured output at bottom | worker exited"
	}
}

func footerHostCue(snapshot Snapshot, ui UIState, status HostStatus) string {
	now := observedAt(ui)
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return "awaiting output"
		}
		since := elapsedSince(now, status.LastOutputAt)
		switch {
		case since <= 0:
			return "recent output"
		case since < 60*time.Second:
			return "recent output"
		case since < 2*time.Minute:
			return "quiet"
		default:
			return "quiet a while"
		}
	case HostStateStarting:
		return "starting"
	case HostStateExited:
		if elapsedSince(now, status.StateChangedAt) < 30*time.Second {
			return "recent exit"
		}
		return "exited"
	case HostStateFailed:
		if elapsedSince(now, status.StateChangedAt) < 30*time.Second {
			return "recent failure"
		}
		return "failed"
	case HostStateFallback:
		return "fallback active"
	case HostStateTranscriptOnly:
		if !latestTranscriptTimestamp(snapshot).IsZero() {
			return "historical transcript"
		}
		return "read-only transcript"
	}
	return ""
}

func hostTemporalStatus(snapshot Snapshot, ui UIState, status HostStatus) string {
	now := observedAt(ui)
	switch status.State {
	case HostStateLive:
		if status.LastOutputAt.IsZero() {
			return describeAwaitingVisibleOutput(status, now)
		}
		return describeLiveOutputAssessment(status, now)
	case HostStateStarting:
		return describeAwaitingVisibleOutput(status, now)
	case HostStateExited, HostStateFailed:
		return describeInactiveState(status, now)
	case HostStateFallback:
		return describeFallbackState(status, now)
	case HostStateTranscriptOnly:
		if savedAt := latestTranscriptTimestamp(snapshot); !savedAt.IsZero() {
			return "latest transcript " + savedAt.Format("15:04:05")
		}
	}
	return ""
}

func latestTranscriptTimestamp(snapshot Snapshot) time.Time {
	var latest time.Time
	for _, item := range snapshot.RecentConversation {
		if item.CreatedAt.After(latest) {
			latest = item.CreatedAt
		}
	}
	return latest
}

func observedAt(ui UIState) time.Time {
	if !ui.ObservedAt.IsZero() {
		return ui.ObservedAt
	}
	if !ui.LastRefresh.IsZero() {
		return ui.LastRefresh
	}
	return time.Now().UTC()
}

func describeAwaitingVisibleOutput(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.StateChangedAt)
	if since <= 0 {
		return "awaiting first visible output"
	}
	return "awaiting first visible output for " + formatElapsed(since)
}

func describeLiveOutputAssessment(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.LastOutputAt)
	if since <= 0 {
		return "quiet with recent visible output"
	}
	if since >= 60*time.Second {
		return "quiet for " + formatElapsed(since) + "; possibly waiting for input or stalled"
	}
	return "quiet for " + formatElapsed(since)
}

func describeInactiveState(status HostStatus, now time.Time) string {
	sinceChange := elapsedSince(now, status.StateChangedAt)
	switch status.State {
	case HostStateFailed:
		if sinceChange > 0 && sinceChange < 30*time.Second {
			return "recently failed " + formatElapsed(sinceChange) + " ago"
		}
		if sinceChange > 0 {
			return "failed " + formatElapsed(sinceChange) + " ago"
		}
		return "worker failed"
	default:
		if sinceChange > 0 && sinceChange < 30*time.Second {
			return "recently exited " + formatElapsed(sinceChange) + " ago"
		}
		if sinceChange > 0 {
			return "exited " + formatElapsed(sinceChange) + " ago"
		}
		return "worker exited"
	}
}

func describeFallbackState(status HostStatus, now time.Time) string {
	since := elapsedSince(now, status.StateChangedAt)
	if since <= 0 {
		return "fallback active"
	}
	return "fallback activated " + formatElapsed(since) + " ago"
}

func describeInactiveBody(status HostStatus) string {
	if status.LastOutputAt.IsZero() {
		return "The session ended before any visible output arrived."
	}
	return "No newer worker output arrived after the session ended."
}

func elapsedSince(now time.Time, then time.Time) time.Duration {
	if now.IsZero() || then.IsZero() {
		return 0
	}
	if then.After(now) {
		return 0
	}
	return now.Sub(then)
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	}
	if d < 10*time.Minute {
		seconds := int(d.Round(time.Second) / time.Second)
		minutes := seconds / 60
		remain := seconds % 60
		if remain == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, remain)
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	return fmt.Sprintf("%dm", minutes)
}

func sessionPriorLine(session SessionState) string {
	if strings.TrimSpace(session.PriorPersistedSummary) == "" {
		return "previous shell outcome none"
	}
	return "previous shell outcome " + truncateWithEllipsis(session.PriorPersistedSummary, 48)
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func humanizeConstant(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, "_", " ")
}

func footerOperatorCue(snapshot Snapshot) string {
	if snapshot.Recovery == nil || isScratchIntakeSnapshot(snapshot) {
		return ""
	}
	action := operatorActionLabel(snapshot)
	if action == "" || action == "none" {
		return ""
	}
	return "next " + action
}

func truncateWithEllipsis(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func pendingTaskMessageEditorLines(ui UIState, height int, width int) []string {
	if height < 1 {
		return nil
	}
	lines := []string{
		"editing staged local draft",
		"this draft stays shell-local until you explicitly send it",
		"ctrl-g s save edit | ctrl-g c cancel edit | ctrl-g m send | ctrl-g x clear",
		"",
	}
	buffer := currentPendingTaskMessage(ui)
	editorLines := strings.Split(buffer, "\n")
	if len(editorLines) == 0 {
		editorLines = []string{""}
	}
	for idx, line := range editorLines {
		prefix := "draft> "
		if idx > 0 {
			prefix = "       "
		}
		lines = append(lines, wrapText(prefix+line, width)...)
	}
	return fitBottom(lines, height)
}
