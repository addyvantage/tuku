package orchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/proof"
	rundomain "tuku/internal/domain/run"
	"tuku/internal/domain/transition"
)

type HandoffLaunchStatus string

const (
	// HandoffLaunchStatusBlocked means Tuku intentionally refused launch before adapter invocation.
	HandoffLaunchStatusBlocked HandoffLaunchStatus = "BLOCKED"
	// HandoffLaunchStatusCompleted means launch invocation completed, not downstream coding completion.
	HandoffLaunchStatusCompleted HandoffLaunchStatus = "COMPLETED"
	// HandoffLaunchStatusFailed means adapter invocation was attempted but failed.
	HandoffLaunchStatusFailed HandoffLaunchStatus = "FAILED"
)

type LaunchHandoffRequest struct {
	TaskID       string
	HandoffID    string
	TargetWorker rundomain.WorkerKind
}

type LaunchHandoffResult struct {
	TaskID              common.TaskID
	HandoffID           string
	TargetWorker        rundomain.WorkerKind
	LaunchStatus        HandoffLaunchStatus
	LaunchID            string
	TransitionReceiptID common.EventID
	CanonicalResponse   string
	Payload             *handoff.LaunchPayload
}

type preparedHandoffLaunch struct {
	TaskID  common.TaskID
	Packet  handoff.Packet
	Payload handoff.LaunchPayload
	Launch  handoff.Launch
}

func (c *Coordinator) LaunchHandoff(ctx context.Context, req LaunchHandoffRequest) (LaunchHandoffResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return LaunchHandoffResult{}, fmt.Errorf("task id is required")
	}
	if c.handoffLauncher == nil {
		return c.recordHandoffLaunchBlockedWithoutAdapter(taskID, req)
	}

	prepared, blocked, err := c.prepareHandoffLaunch(ctx, taskID, req)
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	if blocked != nil {
		return *blocked, nil
	}

	launchReq := adapter_contract.HandoffLaunchRequest{
		TaskID:       prepared.TaskID,
		HandoffID:    prepared.Packet.HandoffID,
		SourceWorker: adapterWorkerKind(prepared.Packet.SourceWorker),
		TargetWorker: adapterWorkerKind(prepared.Packet.TargetWorker),
		Payload:      prepared.Payload,
	}

	var launchOut adapter_contract.HandoffLaunchResult
	var launchErr error
	launchOut, launchErr = c.handoffLauncher.LaunchHandoff(ctx, launchReq)

	result, err := c.finalizeHandoffLaunch(ctx, prepared, launchOut, launchErr)
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) prepareHandoffLaunch(ctx context.Context, taskID common.TaskID, req LaunchHandoffRequest) (*preparedHandoffLaunch, *LaunchHandoffResult, error) {
	var prepared preparedHandoffLaunch
	var blocked *LaunchHandoffResult

	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}

		packet, err := txc.resolveLaunchPacket(taskID, strings.TrimSpace(req.HandoffID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, "", "handoff packet not found for this task")
				if blockErr != nil {
					return blockErr
				}
				blocked = &out
				return nil
			}
			return err
		}
		if packet.TaskID != taskID {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("handoff task mismatch: packet task=%s request task=%s", packet.TaskID, taskID))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}
		if req.TargetWorker != "" && req.TargetWorker != packet.TargetWorker {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("requested target worker %s does not match packet target %s", req.TargetWorker, packet.TargetWorker))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}
		if packet.TargetWorker != rundomain.WorkerKindClaude {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("unsupported handoff launch target: %s", packet.TargetWorker))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}
		switch packet.Status {
		case handoff.StatusCreated, handoff.StatusAccepted:
		default:
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("handoff %s is not launchable in status %s", packet.HandoffID, packet.Status))
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}

		if prior, ok, err := txc.tryReusePriorLaunchOutcome(taskID, packet); err != nil {
			return err
		} else if ok {
			blocked = &prior
			return nil
		}

		assessment, err := txc.assessContinue(ctx, taskID)
		if err != nil {
			return err
		}
		if reason, err := txc.validateLaunchSafety(packet, assessment); err != nil {
			return err
		} else if reason != "" {
			out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, reason)
			if blockErr != nil {
				return blockErr
			}
			blocked = &out
			return nil
		}

		briefRec, err := txc.store.Briefs().Get(packet.BriefID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				out, blockErr := txc.recordHandoffLaunchBlocked(caps, req, packet.TargetWorker, fmt.Sprintf("handoff brief %s is missing", packet.BriefID))
				if blockErr != nil {
					return blockErr
				}
				blocked = &out
				return nil
			}
			return err
		}

		runSummary, runStatus, runID := txc.resolveLaunchRunSummary(packet, taskID)
		payload := txc.materializeLaunchPayload(packet, briefRec, runSummary, runStatus, runID)
		payloadHash := hashLaunchPayload(payload)
		launch := txc.buildRequestedLaunch(taskID, packet, payloadHash)
		if err := txc.store.Handoffs().CreateLaunch(launch); err != nil {
			return err
		}

		launchRunID := runIDPointer(payload.LatestRunID)
		proofPayload := map[string]any{
			"handoff_id":          packet.HandoffID,
			"target_worker":       packet.TargetWorker,
			"source_worker":       packet.SourceWorker,
			"checkpoint_id":       packet.CheckpointID,
			"brief_id":            packet.BriefID,
			"launch_attempt_id":   launch.AttemptID,
			"launch_payload_hash": payloadHash,
		}
		if err := txc.appendProof(caps, proof.EventHandoffLaunchRequested, proof.ActorSystem, "tuku-daemon", proofPayload, launchRunID); err != nil {
			return err
		}
		canonical := fmt.Sprintf(
			"I prepared Claude handoff launch for packet %s. The launch payload is anchored to checkpoint %s and brief %s.",
			packet.HandoffID,
			packet.CheckpointID,
			packet.BriefID,
		)
		if err := txc.emitCanonicalConversation(caps, canonical, proofPayload, launchRunID); err != nil {
			return err
		}

		prepared = preparedHandoffLaunch{
			TaskID:  taskID,
			Packet:  packet,
			Payload: payload,
			Launch:  launch,
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if blocked != nil {
		return nil, blocked, nil
	}
	return &prepared, nil, nil
}

func (c *Coordinator) finalizeHandoffLaunch(ctx context.Context, prepared *preparedHandoffLaunch, launchOut adapter_contract.HandoffLaunchResult, launchErr error) (LaunchHandoffResult, error) {
	var result LaunchHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(prepared.TaskID)
		if err != nil {
			return err
		}
		beforeSnapshot, err := txc.captureContinuityTransitionSnapshot(ctx, prepared.TaskID)
		if err != nil {
			return err
		}
		launchRec, err := txc.store.Handoffs().GetLaunch(prepared.Launch.AttemptID)
		if err != nil {
			return err
		}
		if launchRec.Status != handoff.LaunchStatusRequested {
			return fmt.Errorf("launch attempt %s is not REQUESTED during finalization (status=%s)", launchRec.AttemptID, launchRec.Status)
		}
		runID := runIDPointer(prepared.Payload.LatestRunID)
		target := prepared.Packet.TargetWorker
		if target == "" {
			target = prepared.Payload.TargetWorker
		}

		if launchErr != nil {
			launchRec.Status = handoff.LaunchStatusFailed
			launchRec.LaunchID = strings.TrimSpace(launchOut.LaunchID)
			launchRec.StartedAt = launchOut.StartedAt
			launchRec.EndedAt = launchOut.EndedAt
			launchRec.Command = launchOut.Command
			launchRec.Args = append([]string{}, launchOut.Args...)
			launchRec.Summary = launchOut.Summary
			launchRec.ErrorMessage = launchErr.Error()
			launchRec.OutputArtifactRef = launchOut.OutputArtifactRef
			launchRec.UpdatedAt = txc.clock()
			if launchOut.ExitCode != 0 {
				exitCode := launchOut.ExitCode
				launchRec.ExitCode = &exitCode
			}
			if err := txc.store.Handoffs().UpdateLaunch(launchRec); err != nil {
				return err
			}
			payload := map[string]any{
				"handoff_id":           prepared.Packet.HandoffID,
				"target_worker":        target,
				"launch_attempt_id":    launchRec.AttemptID,
				"launch_id":            launchOut.LaunchID,
				"error":                launchErr.Error(),
				"started_at":           launchOut.StartedAt,
				"ended_at":             launchOut.EndedAt,
				"command":              launchOut.Command,
				"args":                 launchOut.Args,
				"exit_code":            launchOut.ExitCode,
				"stdout_excerpt":       truncate(launchOut.Stdout, 1200),
				"stderr_excerpt":       truncate(launchOut.Stderr, 1200),
				"summary":              launchOut.Summary,
				"launch_scope":         "launcher invocation only",
				"completion_semantics": "invocation attempted; downstream coding completion not observed by Tuku",
			}
			if err := txc.appendProof(caps, proof.EventHandoffLaunchFailed, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
				return err
			}
			afterSnapshot, err := txc.captureContinuityTransitionSnapshot(ctx, prepared.TaskID)
			if err != nil {
				return err
			}
			transitionReceipt, created, err := txc.recordContinuityTransitionReceipt(caps, continuityTransitionRecordInput{
				TaskID:           prepared.TaskID,
				TransitionKind:   transition.KindHandoffLaunch,
				TransitionHandle: launchRec.AttemptID,
				TriggerAction:    "handoff.launch",
				TriggerSource:    "user",
				HandoffID:        prepared.Packet.HandoffID,
				LaunchAttemptID:  launchRec.AttemptID,
				LaunchID:         launchRec.LaunchID,
				Summary: fmt.Sprintf(
					"handoff launch attempt %s failed (%s -> %s) under review posture %s",
					launchRec.AttemptID,
					beforeSnapshot.HandoffContinuity.State,
					afterSnapshot.HandoffContinuity.State,
					transitionReviewPostureFromProgression(beforeSnapshot.Review),
				),
				Before: beforeSnapshot,
				After:  afterSnapshot,
			})
			if err != nil {
				return err
			}
			if created {
				payload["transition_receipt_id"] = transitionReceipt.ReceiptID
			}
			canonical := fmt.Sprintf(
				"Claude handoff launch failed for packet %s: %s. No execution worker state was mutated; review the launch evidence and retry.",
				prepared.Packet.HandoffID,
				launchErr.Error(),
			)
			if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
				return err
			}
			result = LaunchHandoffResult{
				TaskID:              prepared.TaskID,
				HandoffID:           prepared.Packet.HandoffID,
				TargetWorker:        target,
				LaunchStatus:        HandoffLaunchStatusFailed,
				LaunchID:            launchRec.LaunchID,
				TransitionReceiptID: transitionReceipt.ReceiptID,
				CanonicalResponse:   canonical,
				Payload:             &prepared.Payload,
			}
			return nil
		}

		launchRec.Status = handoff.LaunchStatusCompleted
		launchRec.LaunchID = strings.TrimSpace(launchOut.LaunchID)
		launchRec.StartedAt = launchOut.StartedAt
		launchRec.EndedAt = launchOut.EndedAt
		launchRec.Command = launchOut.Command
		launchRec.Args = append([]string{}, launchOut.Args...)
		launchRec.Summary = launchOut.Summary
		launchRec.ErrorMessage = ""
		launchRec.OutputArtifactRef = launchOut.OutputArtifactRef
		launchRec.UpdatedAt = txc.clock()
		if launchOut.ExitCode != 0 {
			exitCode := launchOut.ExitCode
			launchRec.ExitCode = &exitCode
		} else {
			launchRec.ExitCode = nil
		}
		if err := txc.store.Handoffs().UpdateLaunch(launchRec); err != nil {
			return err
		}

		payload := map[string]any{
			"handoff_id":           prepared.Packet.HandoffID,
			"target_worker":        target,
			"launch_attempt_id":    launchRec.AttemptID,
			"launch_id":            launchRec.LaunchID,
			"started_at":           launchOut.StartedAt,
			"ended_at":             launchOut.EndedAt,
			"command":              launchOut.Command,
			"args":                 launchOut.Args,
			"exit_code":            launchOut.ExitCode,
			"summary":              launchOut.Summary,
			"output_artifact_ref":  launchOut.OutputArtifactRef,
			"launch_scope":         "launcher invocation only",
			"completion_semantics": "launch request submitted; downstream coding completion not observed by Tuku",
		}

		ack := txc.buildLaunchAcknowledgment(prepared.TaskID, prepared.Packet.HandoffID, target, launchOut)
		if err := txc.store.Handoffs().SaveAcknowledgment(ack); err != nil {
			return err
		}
		ackPayload := map[string]any{
			"ack_id":        ack.AckID,
			"handoff_id":    ack.HandoffID,
			"launch_id":     ack.LaunchID,
			"target_worker": ack.TargetWorker,
			"status":        ack.Status,
			"summary":       ack.Summary,
			"unknowns":      append([]string{}, ack.Unknowns...),
			"timestamp":     ack.CreatedAt,
		}
		ackEvent := proof.EventHandoffAcknowledgmentCaptured
		if ack.Status == handoff.AcknowledgmentUnavailable {
			ackEvent = proof.EventHandoffAcknowledgmentUnavailable
		}
		if err := txc.appendProof(caps, ackEvent, proof.ActorSystem, "tuku-daemon", ackPayload, runID); err != nil {
			return err
		}
		payload["ack_id"] = ack.AckID
		payload["ack_status"] = ack.Status
		payload["ack_summary"] = ack.Summary
		payload["ack_unknowns"] = append([]string{}, ack.Unknowns...)

		if err := txc.appendProof(caps, proof.EventHandoffLaunchCompleted, proof.ActorSystem, "tuku-daemon", payload, runID); err != nil {
			return err
		}
		afterSnapshot, err := txc.captureContinuityTransitionSnapshot(ctx, prepared.TaskID)
		if err != nil {
			return err
		}
		transitionReceipt, created, err := txc.recordContinuityTransitionReceipt(caps, continuityTransitionRecordInput{
			TaskID:           prepared.TaskID,
			TransitionKind:   transition.KindHandoffLaunch,
			TransitionHandle: launchRec.AttemptID,
			TriggerAction:    "handoff.launch",
			TriggerSource:    "user",
			HandoffID:        prepared.Packet.HandoffID,
			LaunchAttemptID:  launchRec.AttemptID,
			LaunchID:         launchRec.LaunchID,
			Summary: fmt.Sprintf(
				"handoff launch attempt %s completed (%s -> %s) under review posture %s",
				launchRec.AttemptID,
				beforeSnapshot.HandoffContinuity.State,
				afterSnapshot.HandoffContinuity.State,
				transitionReviewPostureFromProgression(beforeSnapshot.Review),
			),
			Before: beforeSnapshot,
			After:  afterSnapshot,
		})
		if err != nil {
			return err
		}
		if created {
			payload["transition_receipt_id"] = transitionReceipt.ReceiptID
		}
		canonical := txc.buildLaunchCanonicalSuccess(prepared.Packet.HandoffID, launchOut.LaunchID, ack)
		if err := txc.emitCanonicalConversation(caps, canonical, payload, runID); err != nil {
			return err
		}
		result = LaunchHandoffResult{
			TaskID:              prepared.TaskID,
			HandoffID:           prepared.Packet.HandoffID,
			TargetWorker:        target,
			LaunchStatus:        HandoffLaunchStatusCompleted,
			LaunchID:            launchRec.LaunchID,
			TransitionReceiptID: transitionReceipt.ReceiptID,
			CanonicalResponse:   canonical,
			Payload:             &prepared.Payload,
		}
		return nil
	})
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	return result, nil
}

func (c *Coordinator) resolveLaunchPacket(taskID common.TaskID, handoffID string) (handoff.Packet, error) {
	if handoffID == "" {
		packet, _, _, _, err := c.loadActiveClaudeHandoffBranch(taskID)
		if err != nil {
			return handoff.Packet{}, err
		}
		if packet == nil {
			return handoff.Packet{}, sql.ErrNoRows
		}
		return *packet, nil
	}
	return c.store.Handoffs().Get(handoffID)
}

func (c *Coordinator) tryReusePriorLaunchOutcome(taskID common.TaskID, packet handoff.Packet) (LaunchHandoffResult, bool, error) {
	launch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LaunchHandoffResult{}, false, nil
		}
		return LaunchHandoffResult{}, false, err
	}

	control := assessLaunchControl(taskID, &packet, &launch)
	switch control.State {
	case LaunchControlStateNotRequested, LaunchControlStateFailed:
		return LaunchHandoffResult{}, false, nil
	case LaunchControlStateCompleted:
		ack, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return LaunchHandoffResult{
					TaskID:            taskID,
					HandoffID:         packet.HandoffID,
					TargetWorker:      packet.TargetWorker,
					LaunchStatus:      HandoffLaunchStatusBlocked,
					CanonicalResponse: fmt.Sprintf("Launch for handoff %s is inconsistent: Tuku has a durable completion record for launch attempt %s but no persisted acknowledgment. Automatic retry is blocked until continuity is repaired.", packet.HandoffID, launch.AttemptID),
				}, true, nil
			}
			return LaunchHandoffResult{}, false, err
		}
		return LaunchHandoffResult{
			TaskID:            taskID,
			HandoffID:         packet.HandoffID,
			TargetWorker:      packet.TargetWorker,
			LaunchStatus:      HandoffLaunchStatusCompleted,
			LaunchID:          launch.LaunchID,
			CanonicalResponse: c.buildLaunchCanonicalSuccess(packet.HandoffID, launch.LaunchID, ack),
		}, true, nil
	case LaunchControlStateRequestedOutcomeUnknown:
		result := buildReplayBlockedLaunchResponse(packet)
		return result, true, nil
	}
	return LaunchHandoffResult{}, false, nil
}

func (c *Coordinator) validateLaunchSafety(packet handoff.Packet, assessment continueAssessment) (string, error) {
	if _, err := c.store.Handoffs().LatestResolution(packet.HandoffID); err == nil {
		return fmt.Sprintf("handoff launch blocked because Claude handoff branch %s was explicitly resolved and is no longer the active continuity branch", packet.HandoffID), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	switch assessment.Outcome {
	case ContinueOutcomeSafe:
	case ContinueOutcomeStaleReconciled:
		return "handoff launch blocked because a stale RUNNING execution state requires reconciliation first", nil
	case ContinueOutcomeBlockedInconsistent:
		return fmt.Sprintf("handoff launch blocked by inconsistent continuity state: %s", assessment.Reason), nil
	case ContinueOutcomeBlockedDrift:
		return "handoff launch blocked by major repository drift", nil
	case ContinueOutcomeNeedsDecision:
		return "handoff launch blocked while task is in decision-gated continuity state", nil
	default:
		return fmt.Sprintf("handoff launch blocked by unsupported continuity outcome: %s", assessment.Outcome), nil
	}

	if packet.BriefID == "" {
		return "handoff launch blocked because packet brief reference is empty", nil
	}
	if packet.CheckpointID == "" {
		return "handoff launch blocked because packet checkpoint reference is empty", nil
	}
	if !packet.IsResumable {
		return "handoff launch blocked because packet checkpoint is not resumable", nil
	}
	cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Sprintf("handoff launch blocked because checkpoint %s is missing", packet.CheckpointID), nil
		}
		return "", err
	}
	if cp.TaskID != assessment.TaskID {
		return fmt.Sprintf("handoff launch blocked because checkpoint %s belongs to task %s", packet.CheckpointID, cp.TaskID), nil
	}
	if !cp.IsResumable {
		return fmt.Sprintf("handoff launch blocked because checkpoint %s is not resumable", packet.CheckpointID), nil
	}
	if packet.BriefID != "" && cp.BriefID != "" && packet.BriefID != cp.BriefID {
		return fmt.Sprintf("handoff launch blocked because packet brief %s does not match checkpoint brief %s", packet.BriefID, cp.BriefID), nil
	}
	if assessment.LatestRun != nil && assessment.LatestRun.Status == rundomain.StatusRunning {
		return "handoff launch blocked because an execution run is still marked RUNNING", nil
	}
	if _, err := c.store.Briefs().Get(packet.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Sprintf("handoff launch blocked because brief %s is missing", packet.BriefID), nil
		}
		return "", err
	}
	return "", nil
}

func (c *Coordinator) resolveLaunchRunSummary(packet handoff.Packet, taskID common.TaskID) (string, rundomain.Status, common.RunID) {
	if packet.LatestRunID != "" {
		if runRec, err := c.store.Runs().Get(packet.LatestRunID); err == nil {
			return runRec.LastKnownSummary, runRec.Status, runRec.RunID
		}
	}
	if latest, err := c.store.Runs().LatestByTask(taskID); err == nil {
		return latest.LastKnownSummary, latest.Status, latest.RunID
	}
	return "", packet.LatestRunStatus, packet.LatestRunID
}

func (c *Coordinator) materializeLaunchPayload(packet handoff.Packet, b brief.ExecutionBrief, runSummary string, runStatus rundomain.Status, runID common.RunID) handoff.LaunchPayload {
	return handoff.LaunchPayload{
		Version:          1,
		TaskID:           packet.TaskID,
		HandoffID:        packet.HandoffID,
		SourceWorker:     packet.SourceWorker,
		TargetWorker:     packet.TargetWorker,
		HandoffMode:      packet.HandoffMode,
		CurrentPhase:     packet.CurrentPhase,
		CheckpointID:     packet.CheckpointID,
		BriefID:          b.BriefID,
		IntentID:         packet.IntentID,
		CapsuleVersion:   packet.CapsuleVersion,
		RepoAnchor:       packet.RepoAnchor,
		IsResumable:      packet.IsResumable,
		ResumeDescriptor: packet.ResumeDescriptor,
		LatestRunID:      runID,
		LatestRunStatus:  runStatus,
		LatestRunSummary: strings.TrimSpace(runSummary),
		Goal:             packet.Goal,
		BriefObjective:   b.Objective,
		NormalizedAction: b.NormalizedAction,
		Constraints:      append([]string{}, b.Constraints...),
		DoneCriteria:     append([]string{}, b.DoneCriteria...),
		TouchedFiles:     append([]string{}, packet.TouchedFiles...),
		Blockers:         append([]string{}, packet.Blockers...),
		NextAction:       packet.NextAction,
		Unknowns:         append([]string{}, packet.Unknowns...),
		HandoffNotes:     append([]string{}, packet.HandoffNotes...),
		GeneratedAt:      c.clock(),
	}
}

func hashLaunchPayload(payload handoff.LaunchPayload) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hexString(sum[:8])
}

func (c *Coordinator) recordHandoffLaunchBlocked(caps capsule.WorkCapsule, req LaunchHandoffRequest, packetTarget rundomain.WorkerKind, reason string) (LaunchHandoffResult, error) {
	target := resolveBlockedLaunchTarget(req.TargetWorker, packetTarget)
	payload := map[string]any{
		"handoff_id":    strings.TrimSpace(req.HandoffID),
		"target_worker": target,
		"reason":        strings.TrimSpace(reason),
	}
	if err := c.appendProof(caps, proof.EventHandoffLaunchBlocked, proof.ActorSystem, "tuku-daemon", payload, nil); err != nil {
		return LaunchHandoffResult{}, err
	}
	canonical := fmt.Sprintf("Handoff launch is blocked: %s", strings.TrimSpace(reason))
	if err := c.emitCanonicalConversation(caps, canonical, payload, nil); err != nil {
		return LaunchHandoffResult{}, err
	}
	return LaunchHandoffResult{
		TaskID:            caps.TaskID,
		HandoffID:         strings.TrimSpace(req.HandoffID),
		TargetWorker:      target,
		LaunchStatus:      HandoffLaunchStatusBlocked,
		CanonicalResponse: canonical,
	}, nil
}

func (c *Coordinator) recordHandoffLaunchBlockedWithoutAdapter(taskID common.TaskID, req LaunchHandoffRequest) (LaunchHandoffResult, error) {
	var out LaunchHandoffResult
	err := c.withTx(func(txc *Coordinator) error {
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		blocked, err := txc.recordHandoffLaunchBlocked(caps, req, "", "Claude handoff launcher is not configured")
		if err != nil {
			return err
		}
		out = blocked
		return nil
	})
	if err != nil {
		return LaunchHandoffResult{}, err
	}
	return out, nil
}

func adapterWorkerKind(kind rundomain.WorkerKind) adapter_contract.WorkerKind {
	switch kind {
	case rundomain.WorkerKindClaude:
		return adapter_contract.WorkerClaude
	case rundomain.WorkerKindCodex:
		return adapter_contract.WorkerCodex
	default:
		return adapter_contract.WorkerUnknown
	}
}

func resolveBlockedLaunchTarget(reqTarget rundomain.WorkerKind, packetTarget rundomain.WorkerKind) rundomain.WorkerKind {
	if reqTarget != "" {
		return reqTarget
	}
	if packetTarget != "" {
		return packetTarget
	}
	return rundomain.WorkerKindClaude
}

func (c *Coordinator) buildLaunchAcknowledgment(taskID common.TaskID, handoffID string, target rundomain.WorkerKind, launchOut adapter_contract.HandoffLaunchResult) handoff.Acknowledgment {
	status := handoff.AcknowledgmentCaptured
	unknowns := []string{}
	summary := summarizeAcknowledgment(launchOut)
	if summary == "" {
		status = handoff.AcknowledgmentUnavailable
		summary = "No usable initial acknowledgment text was returned by the target worker."
		unknowns = append(unknowns, "Initial target-worker acknowledgment text was empty or unusable.")
	}
	unknowns = append(unknowns, "Acknowledgment alone does not prove downstream coding execution completed.")

	return handoff.Acknowledgment{
		Version:      1,
		AckID:        c.idGenerator("hak"),
		HandoffID:    handoffID,
		LaunchID:     strings.TrimSpace(launchOut.LaunchID),
		TaskID:       taskID,
		TargetWorker: target,
		Status:       status,
		Summary:      summary,
		Unknowns:     unknowns,
		CreatedAt:    c.clock(),
	}
}

func (c *Coordinator) buildRequestedLaunch(taskID common.TaskID, packet handoff.Packet, payloadHash string) handoff.Launch {
	now := c.clock()
	return handoff.Launch{
		Version:      1,
		AttemptID:    c.idGenerator("hlc"),
		HandoffID:    packet.HandoffID,
		TaskID:       taskID,
		TargetWorker: packet.TargetWorker,
		Status:       handoff.LaunchStatusRequested,
		PayloadHash:  payloadHash,
		RequestedAt:  now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func summarizeAcknowledgment(launchOut adapter_contract.HandoffLaunchResult) string {
	if summary := strings.TrimSpace(launchOut.Summary); summary != "" {
		return truncate(summary, 280)
	}
	stdout := strings.TrimSpace(launchOut.Stdout)
	if stdout == "" {
		return ""
	}
	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return truncate(line, 280)
	}
	return ""
}

func (c *Coordinator) buildLaunchCanonicalSuccess(handoffID, launchID string, ack handoff.Acknowledgment) string {
	if ack.Status == handoff.AcknowledgmentCaptured {
		return fmt.Sprintf(
			"Claude handoff launch invocation succeeded for packet %s (launch %s). I captured an initial acknowledgment: %s. This proves launcher invocation and initial Claude acknowledgment only; downstream coding execution is not yet proven complete.",
			handoffID,
			strings.TrimSpace(launchID),
			ack.Summary,
		)
	}
	return fmt.Sprintf(
		"Claude handoff launch invocation succeeded for packet %s (launch %s), but no usable initial acknowledgment was captured. Downstream coding execution is not yet proven complete.",
		handoffID,
		strings.TrimSpace(launchID),
	)
}
