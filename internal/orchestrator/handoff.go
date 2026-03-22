package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
)

type CreateHandoffRequest struct {
	TaskID       string
	TargetWorker rundomain.WorkerKind
	Reason       string
	Mode         handoff.Mode
	Notes        []string
}

type CreateHandoffResult struct {
	TaskID            common.TaskID
	HandoffID         string
	SourceWorker      rundomain.WorkerKind
	TargetWorker      rundomain.WorkerKind
	Status            handoff.Status
	CheckpointID      common.CheckpointID
	BriefID           common.BriefID
	CanonicalResponse string
	Packet            *handoff.Packet
}

type AcceptHandoffRequest struct {
	TaskID     string
	HandoffID  string
	AcceptedBy rundomain.WorkerKind
	Notes      []string
}

type AcceptHandoffResult struct {
	TaskID            common.TaskID
	HandoffID         string
	Status            handoff.Status
	CanonicalResponse string
}

func (c *Coordinator) CreateHandoff(ctx context.Context, req CreateHandoffRequest) (CreateHandoffResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return CreateHandoffResult{}, fmt.Errorf("task id is required")
	}

	targetWorker := req.TargetWorker
	if targetWorker == "" {
		targetWorker = rundomain.WorkerKindClaude
	}
	if targetWorker != rundomain.WorkerKindClaude {
		return CreateHandoffResult{}, fmt.Errorf("unsupported handoff target worker: %s", targetWorker)
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return CreateHandoffResult{}, err
	}
	if blockedReason, err := c.validateHandoffSafety(assessment); err != nil {
		return CreateHandoffResult{}, err
	} else if blockedReason != "" {
		return c.recordBlockedHandoffReason(ctx, taskID, blockedReason, targetWorker, req)
	}

	var result CreateHandoffResult
	err = c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		if caps.Version != assessment.Capsule.Version {
			return txc.recordBlockedHandoffTx(caps, "task state changed during handoff assessment", targetWorker, req, &result)
		}
		b, err := txc.store.Briefs().Get(caps.CurrentBriefID)
		if err != nil {
			if err == sql.ErrNoRows {
				return txc.recordBlockedHandoffTx(caps, fmt.Sprintf("brief %s not found", caps.CurrentBriefID), targetWorker, req, &result)
			}
			return err
		}

		if reused, ok, err := txc.tryReuseExistingHandoff(taskID, caps, assessment, targetWorker, req); err != nil {
			return err
		} else if ok {
			result = reused
			return nil
		}

		var cp checkpoint.Checkpoint
		if assessment.LatestCheckpoint != nil {
			latestCP, err := txc.store.Checkpoints().LatestByTask(taskID)
			if err != nil {
				return err
			}
			if txc.canReuseHandoffCheckpoint(caps, assessment, latestCP) {
				cp = latestCP
			}
		}
		if cp.CheckpointID == "" {
			runID := common.RunID("")
			if assessment.LatestRun != nil {
				runID = assessment.LatestRun.RunID
			}
			newCP, err := txc.createCheckpoint(caps, runID, checkpoint.TriggerHandoff, true, "Checkpoint created for cross-worker handoff packet generation.")
			if err != nil {
				return err
			}
			cp = newCP
		}

		var latestRun *rundomain.ExecutionRun
		if assessment.LatestRun != nil {
			runCopy := *assessment.LatestRun
			latestRun = &runCopy
		}
		packet := txc.buildHandoffPacket(caps, b, cp, latestRun, targetWorker, req)
		if err := txc.store.Handoffs().Create(packet); err != nil {
			return err
		}

		payload := map[string]any{
			"handoff_id":    packet.HandoffID,
			"source_worker": packet.SourceWorker,
			"target_worker": packet.TargetWorker,
			"checkpoint_id": packet.CheckpointID,
			"brief_id":      packet.BriefID,
			"mode":          packet.HandoffMode,
			"reason":        packet.Reason,
			"is_resumable":  packet.IsResumable,
		}
		runIDPtr := runIDPointer(packet.LatestRunID)
		if err := txc.appendProof(caps, proof.EventHandoffCreated, proof.ActorSystem, "tuku-daemon", payload, runIDPtr); err != nil {
			return err
		}

		canonical := fmt.Sprintf(
			"I created handoff packet %s for %s. It is anchored to checkpoint %s and brief %s on branch %s (head %s).",
			packet.HandoffID,
			packet.TargetWorker,
			packet.CheckpointID,
			packet.BriefID,
			packet.RepoAnchor.BranchName,
			packet.RepoAnchor.HeadSHA,
		)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runIDPtr); err != nil {
			return err
		}

		packetCopy := packet
		result = CreateHandoffResult{
			TaskID:            packet.TaskID,
			HandoffID:         packet.HandoffID,
			SourceWorker:      packet.SourceWorker,
			TargetWorker:      packet.TargetWorker,
			Status:            packet.Status,
			CheckpointID:      packet.CheckpointID,
			BriefID:           packet.BriefID,
			CanonicalResponse: canonical,
			Packet:            &packetCopy,
		}
		return nil
	})
	if err != nil {
		return CreateHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) AcceptHandoff(ctx context.Context, req AcceptHandoffRequest) (AcceptHandoffResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	handoffID := strings.TrimSpace(req.HandoffID)
	if taskID == "" {
		return AcceptHandoffResult{}, fmt.Errorf("task id is required")
	}
	if handoffID == "" {
		return AcceptHandoffResult{}, fmt.Errorf("handoff id is required")
	}

	var result AcceptHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		packet, err := txc.store.Handoffs().Get(handoffID)
		if err != nil {
			return err
		}
		if packet.TaskID != taskID {
			return fmt.Errorf("handoff task mismatch: packet task=%s request task=%s", packet.TaskID, taskID)
		}
		acceptedBy := req.AcceptedBy
		if acceptedBy == "" {
			acceptedBy = packet.TargetWorker
		}
		if packet.Status == handoff.StatusAccepted {
			if packet.AcceptedBy != "" && acceptedBy != "" && packet.AcceptedBy != acceptedBy {
				return fmt.Errorf("handoff %s is already accepted by %s", handoffID, packet.AcceptedBy)
			}
			result = AcceptHandoffResult{
				TaskID:            taskID,
				HandoffID:         handoffID,
				Status:            handoff.StatusAccepted,
				CanonicalResponse: fmt.Sprintf("Handoff %s was already accepted by %s. Reusing the durable acceptance anchored at checkpoint %s.", handoffID, packet.AcceptedBy, packet.CheckpointID),
			}
			return nil
		}
		if packet.Status != handoff.StatusCreated {
			return fmt.Errorf("handoff %s is not accept-ready in status %s", handoffID, packet.Status)
		}
		now := txc.clock()
		if err := txc.store.Handoffs().UpdateStatus(taskID, handoffID, handoff.StatusAccepted, acceptedBy, req.Notes, now); err != nil {
			return err
		}

		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"handoff_id":    handoffID,
			"accepted_by":   acceptedBy,
			"target_worker": packet.TargetWorker,
			"checkpoint_id": packet.CheckpointID,
			"brief_id":      packet.BriefID,
		}
		if err := txc.appendProof(caps, proof.EventHandoffAccepted, proof.ActorSystem, "tuku-daemon", payload, runIDPointer(packet.LatestRunID)); err != nil {
			return err
		}
		canonical := fmt.Sprintf("Handoff %s accepted by %s. Continuity remains anchored at checkpoint %s.", handoffID, acceptedBy, packet.CheckpointID)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runIDPointer(packet.LatestRunID)); err != nil {
			return err
		}
		result = AcceptHandoffResult{
			TaskID:            taskID,
			HandoffID:         handoffID,
			Status:            handoff.StatusAccepted,
			CanonicalResponse: canonical,
		}
		return nil
	})
	if err != nil {
		return AcceptHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) tryReuseExistingHandoff(taskID common.TaskID, caps capsule.WorkCapsule, assessment continueAssessment, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) (CreateHandoffResult, bool, error) {
	packet, err := c.store.Handoffs().LatestByTask(taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CreateHandoffResult{}, false, nil
		}
		return CreateHandoffResult{}, false, err
	}
	if !c.canReuseHandoffPacket(packet, caps, assessment, targetWorker, req) {
		return CreateHandoffResult{}, false, nil
	}

	packetCopy := packet
	return CreateHandoffResult{
		TaskID:            packet.TaskID,
		HandoffID:         packet.HandoffID,
		SourceWorker:      packet.SourceWorker,
		TargetWorker:      packet.TargetWorker,
		Status:            packet.Status,
		CheckpointID:      packet.CheckpointID,
		BriefID:           packet.BriefID,
		CanonicalResponse: fmt.Sprintf("Reused existing handoff packet %s for %s. Continuity remains anchored to checkpoint %s and brief %s.", packet.HandoffID, packet.TargetWorker, packet.CheckpointID, packet.BriefID),
		Packet:            &packetCopy,
	}, true, nil
}

func (c *Coordinator) recordBlockedHandoffReason(_ context.Context, taskID common.TaskID, reason string, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) (CreateHandoffResult, error) {
	var result CreateHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		return txc.recordBlockedHandoffTx(caps, reason, targetWorker, req, &result)
	})
	if err != nil {
		return CreateHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) recordBlockedHandoffTx(caps capsule.WorkCapsule, reason string, targetWorker rundomain.WorkerKind, req CreateHandoffRequest, out *CreateHandoffResult) error {
	payload := map[string]any{
		"target_worker": targetWorker,
		"reason":        strings.TrimSpace(reason),
		"mode":          normalizeHandoffMode(req.Mode),
	}
	if err := c.appendProof(caps, proof.EventHandoffBlocked, proof.ActorSystem, "tuku-daemon", payload, nil); err != nil {
		return err
	}
	canonical := fmt.Sprintf("Handoff to %s is blocked: %s", targetWorker, strings.TrimSpace(reason))
	if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
		return err
	}
	*out = CreateHandoffResult{
		TaskID:            caps.TaskID,
		SourceWorker:      rundomain.WorkerKindUnknown,
		TargetWorker:      targetWorker,
		Status:            handoff.StatusBlocked,
		CanonicalResponse: canonical,
	}
	return nil
}

func (c *Coordinator) buildHandoffPacket(caps capsule.WorkCapsule, b brief.ExecutionBrief, cp checkpoint.Checkpoint, latestRun *rundomain.ExecutionRun, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) handoff.Packet {
	sourceWorker := rundomain.WorkerKindUnknown
	latestRunID := common.RunID("")
	latestRunStatus := rundomain.Status("")
	if latestRun != nil {
		sourceWorker = latestRun.WorkerKind
		latestRunID = latestRun.RunID
		latestRunStatus = latestRun.Status
	}

	notes := append([]string{}, req.Notes...)
	unknowns := buildHandoffUnknowns(caps, cp, latestRun)
	return handoff.Packet{
		Version:          1,
		HandoffID:        c.idGenerator("hnd"),
		TaskID:           caps.TaskID,
		Status:           handoff.StatusCreated,
		SourceWorker:     sourceWorker,
		TargetWorker:     targetWorker,
		HandoffMode:      normalizeHandoffMode(req.Mode),
		Reason:           strings.TrimSpace(req.Reason),
		CurrentPhase:     caps.CurrentPhase,
		CheckpointID:     cp.CheckpointID,
		BriefID:          b.BriefID,
		IntentID:         caps.CurrentIntentID,
		CapsuleVersion:   caps.Version,
		RepoAnchor:       cp.Anchor,
		IsResumable:      cp.IsResumable,
		ResumeDescriptor: cp.ResumeDescriptor,
		LatestRunID:      latestRunID,
		LatestRunStatus:  latestRunStatus,
		Goal:             caps.Goal,
		BriefObjective:   b.Objective,
		NormalizedAction: b.NormalizedAction,
		Constraints:      append([]string{}, b.Constraints...),
		DoneCriteria:     append([]string{}, b.DoneCriteria...),
		TouchedFiles:     append([]string{}, caps.TouchedFiles...),
		Blockers:         append([]string{}, caps.Blockers...),
		NextAction:       caps.NextAction,
		Unknowns:         unknowns,
		HandoffNotes:     notes,
		CreatedAt:        c.clock(),
	}
}

func normalizeHandoffMode(mode handoff.Mode) handoff.Mode {
	switch mode {
	case handoff.ModeResume, handoff.ModeReview, handoff.ModeTakeover:
		return mode
	default:
		return handoff.ModeResume
	}
}

func buildHandoffUnknowns(caps capsule.WorkCapsule, cp checkpoint.Checkpoint, latestRun *rundomain.ExecutionRun) []string {
	unknowns := []string{}

	if latestRun == nil {
		unknowns = append(unknowns, "No prior worker run is recorded for this task.")
	} else {
		switch latestRun.Status {
		case rundomain.StatusInterrupted:
			unknowns = append(unknowns, "Latest run is INTERRUPTED; completion status is unresolved.")
		case rundomain.StatusFailed:
			unknowns = append(unknowns, "Latest run FAILED; target worker should validate root cause before proceeding.")
		}
	}
	if caps.CurrentPhase != phase.PhaseCompleted {
		unknowns = append(unknowns, "End-to-end validation is not marked complete in continuity state.")
	}
	if isRepoAnchorDirty(cp.Anchor) || caps.WorkingTreeDirty {
		unknowns = append(unknowns, "Repository is currently dirty; handoff may include uncommitted state.")
	}
	if len(caps.Blockers) > 0 {
		unknowns = append(unknowns, "Task blockers are present and may require human decision.")
	}
	return unknowns
}

func (c *Coordinator) validateHandoffSafety(assessment continueAssessment) (string, error) {
	hasReusableCheckpoint := c.hasValidResumableHandoffCheckpoint(assessment.Capsule, assessment.LatestCheckpoint)

	switch assessment.Outcome {
	case ContinueOutcomeBlockedInconsistent:
		return fmt.Sprintf("handoff blocked by inconsistent continuity state: %s", assessment.Reason), nil
	case ContinueOutcomeStaleReconciled:
		return "handoff blocked because latest run is still unresolved and requires reconciliation", nil
	case ContinueOutcomeBlockedDrift:
		if !hasReusableCheckpoint {
			return "handoff blocked by major repository drift", nil
		}
	case ContinueOutcomeNeedsDecision:
		if !hasReusableCheckpoint {
			return "handoff blocked while task is in decision-gated continuity state", nil
		}
	case ContinueOutcomeSafe:
		// Continue with explicit handoff checks below.
	default:
		return fmt.Sprintf("handoff blocked by unsupported continuity outcome: %s", assessment.Outcome), nil
	}

	if assessment.DriftClass == checkpoint.DriftMajor {
		return "handoff blocked by major repository drift", nil
	}
	if assessment.LatestRun != nil && assessment.LatestRun.Status == rundomain.StatusRunning {
		return "handoff blocked because a RUNNING execution state is unresolved", nil
	}
	if assessment.Capsule.CurrentPhase == phase.PhaseExecuting {
		return "handoff blocked because task phase is EXECUTING", nil
	}
	if assessment.Capsule.CurrentBriefID == "" {
		return "handoff blocked because no current brief exists", nil
	}
	if _, err := c.store.Briefs().Get(assessment.Capsule.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Sprintf("handoff blocked because current brief %s is missing", assessment.Capsule.CurrentBriefID), nil
		}
		return "", err
	}

	if hasReusableCheckpoint {
		return "", nil
	}
	if c.canCreateHandoffCheckpoint(assessment) {
		return "", nil
	}

	return "handoff blocked because no reusable or safely creatable resumable checkpoint is available", nil
}

func (c *Coordinator) canReuseHandoffCheckpoint(caps capsule.WorkCapsule, assessment continueAssessment, latestCP checkpoint.Checkpoint) bool {
	if assessment.LatestCheckpoint == nil {
		return false
	}
	if latestCP.CheckpointID != assessment.LatestCheckpoint.CheckpointID {
		return false
	}
	return c.hasValidResumableHandoffCheckpoint(caps, &latestCP)
}

func (c *Coordinator) canReuseHandoffPacket(packet handoff.Packet, caps capsule.WorkCapsule, assessment continueAssessment, targetWorker rundomain.WorkerKind, req CreateHandoffRequest) bool {
	if packet.Status != handoff.StatusCreated && packet.Status != handoff.StatusAccepted {
		return false
	}
	if assessment.LatestResolution != nil && assessment.LatestResolution.HandoffID == packet.HandoffID {
		return false
	}
	if packet.TaskID != caps.TaskID || packet.TargetWorker != targetWorker {
		return false
	}
	if packet.HandoffMode != normalizeHandoffMode(req.Mode) {
		return false
	}
	if strings.TrimSpace(packet.Reason) != strings.TrimSpace(req.Reason) {
		return false
	}
	if !stringSlicesEqual(packet.HandoffNotes, req.Notes) {
		return false
	}
	if packet.BriefID != caps.CurrentBriefID || packet.IntentID != caps.CurrentIntentID {
		return false
	}
	if packet.CurrentPhase != caps.CurrentPhase || packet.CapsuleVersion != caps.Version {
		return false
	}
	if assessment.LatestRun != nil {
		if packet.LatestRunID != assessment.LatestRun.RunID || packet.LatestRunStatus != assessment.LatestRun.Status {
			return false
		}
	} else if packet.LatestRunID != "" || packet.LatestRunStatus != "" {
		return false
	}
	cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
	if err != nil {
		return false
	}
	if !c.hasValidResumableHandoffCheckpoint(caps, &cp) {
		return false
	}
	if !repoAnchorsEqual(packet.RepoAnchor, cp.Anchor) {
		return false
	}
	return true
}

func (c *Coordinator) hasValidResumableHandoffCheckpoint(caps capsule.WorkCapsule, cp *checkpoint.Checkpoint) bool {
	if cp == nil {
		return false
	}
	if !cp.IsResumable {
		return false
	}
	if cp.TaskID != caps.TaskID {
		return false
	}
	if cp.BriefID == "" || cp.BriefID != caps.CurrentBriefID {
		return false
	}
	if cp.IntentID != "" && caps.CurrentIntentID != "" && cp.IntentID != caps.CurrentIntentID {
		return false
	}
	if cp.Phase != caps.CurrentPhase {
		return false
	}
	return true
}

func (c *Coordinator) canCreateHandoffCheckpoint(assessment continueAssessment) bool {
	if assessment.Outcome != ContinueOutcomeSafe {
		return false
	}
	if assessment.DriftClass == checkpoint.DriftMajor {
		return false
	}
	if assessment.Capsule.CurrentBriefID == "" {
		return false
	}
	if assessment.Capsule.CurrentPhase == phase.PhaseExecuting || assessment.Capsule.CurrentPhase == phase.PhaseAwaitingDecision || assessment.Capsule.CurrentPhase == phase.PhaseBlocked {
		return false
	}
	if assessment.LatestRun != nil && assessment.LatestRun.Status == rundomain.StatusRunning {
		return false
	}
	return true
}

func isRepoAnchorDirty(anchor checkpoint.RepoAnchor) bool {
	dirty := strings.TrimSpace(strings.ToLower(anchor.DirtyHash))
	return dirty == "true" || dirty == "1" || dirty == "yes"
}
