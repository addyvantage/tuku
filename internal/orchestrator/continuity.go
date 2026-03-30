package orchestrator

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/checkpoint"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/recoveryaction"
	rundomain "tuku/internal/domain/run"
)

type continuityViolationCode string

const (
	continuityViolationCapsuleBriefMissing             continuityViolationCode = "CAPSULE_BRIEF_MISSING"
	continuityViolationCapsuleIntentMissing            continuityViolationCode = "CAPSULE_INTENT_MISSING"
	continuityViolationCheckpointTaskMismatch          continuityViolationCode = "CHECKPOINT_TASK_MISMATCH"
	continuityViolationCheckpointRunMissing            continuityViolationCode = "CHECKPOINT_RUN_MISSING"
	continuityViolationCheckpointBriefMissing          continuityViolationCode = "CHECKPOINT_BRIEF_MISSING"
	continuityViolationCheckpointResumablePhase        continuityViolationCode = "CHECKPOINT_RESUMABLE_PHASE_INVALID"
	continuityViolationRunTaskMismatch                 continuityViolationCode = "RUN_TASK_MISMATCH"
	continuityViolationRunBriefMissing                 continuityViolationCode = "RUN_BRIEF_MISSING"
	continuityViolationRunPhaseMismatch                continuityViolationCode = "RUN_PHASE_MISMATCH"
	continuityViolationRunningRunCheckpointMismatch    continuityViolationCode = "RUNNING_RUN_CHECKPOINT_MISMATCH"
	continuityViolationLatestHandoffTaskMismatch       continuityViolationCode = "LATEST_HANDOFF_TASK_MISMATCH"
	continuityViolationLatestHandoffBriefMissing       continuityViolationCode = "LATEST_HANDOFF_BRIEF_MISSING"
	continuityViolationLatestHandoffCheckpointMissing  continuityViolationCode = "LATEST_HANDOFF_CHECKPOINT_MISSING"
	continuityViolationLatestHandoffCheckpointMismatch continuityViolationCode = "LATEST_HANDOFF_CHECKPOINT_MISMATCH"
	continuityViolationLatestHandoffAcceptedInvalid    continuityViolationCode = "LATEST_HANDOFF_ACCEPTED_INVALID"
	continuityViolationLatestLaunchInvalid             continuityViolationCode = "LATEST_LAUNCH_INVALID"
	continuityViolationLatestAckInvalid                continuityViolationCode = "LATEST_ACK_INVALID"
	continuityViolationLatestFollowThroughInvalid      continuityViolationCode = "LATEST_FOLLOW_THROUGH_INVALID"
	continuityViolationLatestResolutionInvalid         continuityViolationCode = "LATEST_RESOLUTION_INVALID"
	continuityViolationLatestRecoveryActionInvalid     continuityViolationCode = "LATEST_RECOVERY_ACTION_INVALID"
)

type continuityViolation struct {
	Code    continuityViolationCode
	Message string
}

type continuitySnapshot struct {
	Capsule              capsule.WorkCapsule
	LatestRun            *rundomain.ExecutionRun
	LatestCheckpoint     *checkpoint.Checkpoint
	LatestHandoff        *handoff.Packet
	LatestLaunch         *handoff.Launch
	LatestAcknowledgment *handoff.Acknowledgment
	LatestFollowThrough  *handoff.FollowThrough
	ActiveHandoff        *handoff.Packet
	ActiveLaunch         *handoff.Launch
	ActiveAcknowledgment *handoff.Acknowledgment
	ActiveFollowThrough  *handoff.FollowThrough
	LatestResolution     *handoff.Resolution
	LatestRecoveryAction *recoveryaction.Record
}

func (c *Coordinator) loadContinuitySnapshot(taskID common.TaskID) (continuitySnapshot, error) {
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return continuitySnapshot{}, err
	}

	snapshot := continuitySnapshot{Capsule: caps}
	if latestRun, err := c.store.Runs().LatestByTask(taskID); err == nil {
		runCopy := latestRun
		snapshot.LatestRun = &runCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestCheckpoint, err := c.store.Checkpoints().LatestByTask(taskID); err == nil {
		cpCopy := latestCheckpoint
		snapshot.LatestCheckpoint = &cpCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestHandoff, err := c.store.Handoffs().LatestByTask(taskID); err == nil {
		packetCopy := latestHandoff
		snapshot.LatestHandoff = &packetCopy
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(latestHandoff.HandoffID); err == nil {
			launchCopy := latestLaunch
			snapshot.LatestLaunch = &launchCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(latestHandoff.HandoffID); err == nil {
			ackCopy := latestAck
			snapshot.LatestAcknowledgment = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(latestHandoff.HandoffID); err == nil {
			recordCopy := latestFollowThrough
			snapshot.LatestFollowThrough = &recordCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return continuitySnapshot{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	if latestResolution, err := c.store.Handoffs().LatestResolutionByTask(taskID); err == nil {
		recordCopy := latestResolution
		snapshot.LatestResolution = &recordCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}
	if latestHandoff, latestLaunch, latestAck, latestFollowThrough, err := c.loadActiveClaudeHandoffBranch(taskID); err != nil {
		return continuitySnapshot{}, err
	} else {
		snapshot.ActiveHandoff = latestHandoff
		snapshot.ActiveLaunch = latestLaunch
		snapshot.ActiveAcknowledgment = latestAck
		snapshot.ActiveFollowThrough = latestFollowThrough
	}

	if latestAction, err := c.store.RecoveryActions().LatestByTask(taskID); err == nil {
		actionCopy := latestAction
		snapshot.LatestRecoveryAction = &actionCopy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return continuitySnapshot{}, err
	}

	return snapshot, nil
}

func (c *Coordinator) loadActiveClaudeHandoffBranch(taskID common.TaskID) (*handoff.Packet, *handoff.Launch, *handoff.Acknowledgment, *handoff.FollowThrough, error) {
	packets, err := c.store.Handoffs().ListByTask(taskID, 12)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, nil, nil
		}
		return nil, nil, nil, nil, err
	}
	for i := range packets {
		packet := packets[i]
		if packet.TargetWorker != rundomain.WorkerKindClaude || packet.Status != handoff.StatusAccepted {
			continue
		}
		if _, err := c.store.Handoffs().LatestResolution(packet.HandoffID); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, nil, err
		}

		packetCopy := packet
		var launchPtr *handoff.Launch
		if latestLaunch, err := c.store.Handoffs().LatestLaunchByHandoff(packet.HandoffID); err == nil {
			launchCopy := latestLaunch
			launchPtr = &launchCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, nil, err
		}
		var ackPtr *handoff.Acknowledgment
		if latestAck, err := c.store.Handoffs().LatestAcknowledgment(packet.HandoffID); err == nil {
			ackCopy := latestAck
			ackPtr = &ackCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, nil, err
		}
		var followThroughPtr *handoff.FollowThrough
		if latestFollowThrough, err := c.store.Handoffs().LatestFollowThrough(packet.HandoffID); err == nil {
			recordCopy := latestFollowThrough
			followThroughPtr = &recordCopy
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil, nil, err
		}
		return &packetCopy, launchPtr, ackPtr, followThroughPtr, nil
	}
	return nil, nil, nil, nil, nil
}

func (c *Coordinator) validateContinuitySnapshot(snapshot continuitySnapshot) ([]continuityViolation, error) {
	violations := make([]continuityViolation, 0, 8)
	caps := snapshot.Capsule

	if caps.CurrentBriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCapsuleBriefMissing,
			Message: "capsule has no current brief reference",
		})
	} else if _, err := c.store.Briefs().Get(caps.CurrentBriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCapsuleBriefMissing,
				Message: fmt.Sprintf("capsule references missing brief %s", caps.CurrentBriefID),
			})
		} else {
			return nil, err
		}
	}

	if caps.CurrentIntentID != "" {
		latestIntent, err := c.store.Intents().LatestByTask(caps.TaskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCapsuleIntentMissing,
					Message: fmt.Sprintf("capsule references missing intent %s", caps.CurrentIntentID),
				})
			} else {
				return nil, err
			}
		} else if latestIntent.IntentID != caps.CurrentIntentID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCapsuleIntentMissing,
				Message: fmt.Sprintf("capsule current intent %s does not match latest intent %s", caps.CurrentIntentID, latestIntent.IntentID),
			})
		}
	}

	if snapshot.LatestCheckpoint != nil {
		cpViolations, err := c.validateCheckpointContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, cpViolations...)
	}

	if snapshot.LatestRun != nil {
		runViolations, err := c.validateRunContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, runViolations...)
	} else if caps.CurrentPhase == phase.PhaseExecuting {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunPhaseMismatch,
			Message: "capsule phase is EXECUTING but no run exists",
		})
	}

	if snapshot.LatestHandoff != nil {
		handoffViolations, err := c.validateHandoffContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, handoffViolations...)
	}
	if snapshot.LatestRecoveryAction != nil {
		actionViolations, err := c.validateRecoveryActionContinuity(snapshot)
		if err != nil {
			return nil, err
		}
		violations = append(violations, actionViolations...)
	}

	return dedupeContinuityViolations(violations), nil
}

func (c *Coordinator) validateCheckpointContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	cp := snapshot.LatestCheckpoint
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 4)

	if cp.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCheckpointTaskMismatch,
			Message: fmt.Sprintf("latest checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, caps.TaskID),
		})
	}
	if cp.BriefID != "" {
		if _, err := c.store.Briefs().Get(cp.BriefID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCheckpointBriefMissing,
					Message: fmt.Sprintf("latest checkpoint references missing brief %s", cp.BriefID),
				})
			} else {
				return nil, err
			}
		}
	}
	if cp.RunID != "" {
		runRec, err := c.store.Runs().Get(cp.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationCheckpointRunMissing,
					Message: fmt.Sprintf("latest checkpoint references missing run %s", cp.RunID),
				})
			} else {
				return nil, err
			}
		} else if runRec.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationCheckpointTaskMismatch,
				Message: fmt.Sprintf("latest checkpoint run task mismatch: run task=%s capsule task=%s", runRec.TaskID, caps.TaskID),
			})
		}
	}
	if cp.IsResumable && (cp.Phase == phase.PhaseAwaitingDecision || cp.Phase == phase.PhaseBlocked) {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationCheckpointResumablePhase,
			Message: fmt.Sprintf("latest checkpoint %s is marked resumable in incompatible phase %s", cp.CheckpointID, cp.Phase),
		})
	}

	return violations, nil
}

func (c *Coordinator) validateRunContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	runRec := snapshot.LatestRun
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 4)

	if runRec.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunTaskMismatch,
			Message: fmt.Sprintf("latest run task mismatch: run task=%s capsule task=%s", runRec.TaskID, caps.TaskID),
		})
	}
	if runRec.BriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunBriefMissing,
			Message: "latest run has empty brief reference",
		})
	} else if _, err := c.store.Briefs().Get(runRec.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunBriefMissing,
				Message: fmt.Sprintf("latest run references missing brief %s", runRec.BriefID),
			})
		} else {
			return nil, err
		}
	}

	if runRec.Status == rundomain.StatusRunning {
		if caps.CurrentPhase != phase.PhaseExecuting {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunPhaseMismatch,
				Message: fmt.Sprintf("latest run %s is RUNNING but capsule phase is %s", runRec.RunID, caps.CurrentPhase),
			})
		}
		if caps.CurrentBriefID != "" && caps.CurrentBriefID != runRec.BriefID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationRunPhaseMismatch,
				Message: fmt.Sprintf("RUNNING run brief %s does not match capsule brief %s", runRec.BriefID, caps.CurrentBriefID),
			})
		}
		if snapshot.LatestCheckpoint != nil {
			if snapshot.LatestCheckpoint.RunID == "" {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationRunningRunCheckpointMismatch,
					Message: fmt.Sprintf("RUNNING run %s has checkpoint linkage without run_id", runRec.RunID),
				})
			} else if snapshot.LatestCheckpoint.RunID != runRec.RunID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationRunningRunCheckpointMismatch,
					Message: fmt.Sprintf("RUNNING run %s does not match checkpoint run linkage %s", runRec.RunID, snapshot.LatestCheckpoint.RunID),
				})
			}
		}
	} else if caps.CurrentPhase == phase.PhaseExecuting {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationRunPhaseMismatch,
			Message: fmt.Sprintf("capsule phase EXECUTING is inconsistent with latest run terminal status %s", runRec.Status),
		})
	}

	return violations, nil
}

func (c *Coordinator) validateHandoffContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	packet := snapshot.LatestHandoff
	caps := snapshot.Capsule
	violations := make([]continuityViolation, 0, 8)

	if packet.TaskID != caps.TaskID {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffTaskMismatch,
			Message: fmt.Sprintf("latest handoff task mismatch: handoff task=%s capsule task=%s", packet.TaskID, caps.TaskID),
		})
	}
	if packet.BriefID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffBriefMissing,
			Message: fmt.Sprintf("latest handoff %s has empty brief reference", packet.HandoffID),
		})
	} else if _, err := c.store.Briefs().Get(packet.BriefID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffBriefMissing,
				Message: fmt.Sprintf("latest handoff references missing brief %s", packet.BriefID),
			})
		} else {
			return nil, err
		}
	}
	if packet.CheckpointID == "" {
		violations = append(violations, continuityViolation{
			Code:    continuityViolationLatestHandoffCheckpointMissing,
			Message: fmt.Sprintf("latest handoff %s has empty checkpoint reference", packet.HandoffID),
		})
	} else {
		cp, err := c.store.Checkpoints().Get(packet.CheckpointID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMissing,
					Message: fmt.Sprintf("latest handoff references missing checkpoint %s", packet.CheckpointID),
				})
			} else {
				return nil, err
			}
		} else {
			if cp.TaskID != caps.TaskID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffTaskMismatch,
					Message: fmt.Sprintf("latest handoff checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, caps.TaskID),
				})
			}
			if packet.IsResumable && !cp.IsResumable {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff %s claims resumable continuity but checkpoint %s is not resumable", packet.HandoffID, packet.CheckpointID),
				})
			}
			if packet.BriefID != "" && cp.BriefID != "" && packet.BriefID != cp.BriefID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff brief %s does not match checkpoint brief %s", packet.BriefID, cp.BriefID),
				})
			}
			if packet.IntentID != "" && cp.IntentID != "" && packet.IntentID != cp.IntentID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestHandoffCheckpointMismatch,
					Message: fmt.Sprintf("latest handoff intent %s does not match checkpoint intent %s", packet.IntentID, cp.IntentID),
				})
			}
		}
	}

	switch packet.Status {
	case handoff.StatusAccepted:
		if packet.AcceptedBy == "" || packet.AcceptedAt == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is ACCEPTED without accepted_by and accepted_at", packet.HandoffID),
			})
		}
	case handoff.StatusCreated:
		if packet.AcceptedBy != "" || packet.AcceptedAt != nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is CREATED but carries acceptance fields", packet.HandoffID),
			})
		}
	case handoff.StatusBlocked:
		if packet.AcceptedBy != "" || packet.AcceptedAt != nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestHandoffAcceptedInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but carries acceptance fields", packet.HandoffID),
			})
		}
	}

	if snapshot.LatestLaunch != nil {
		launch := snapshot.LatestLaunch
		if launch.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch task mismatch: launch task=%s capsule task=%s", launch.TaskID, caps.TaskID),
			})
		}
		if launch.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch handoff mismatch: launch handoff=%s latest handoff=%s", launch.HandoffID, packet.HandoffID),
			})
		}
		if launch.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch target %s does not match latest handoff target %s", launch.TargetWorker, packet.TargetWorker),
			})
		}
		if packet.Status == handoff.StatusBlocked {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but has a persisted launch attempt", packet.HandoffID),
			})
		}
		switch launch.Status {
		case handoff.LaunchStatusRequested:
		case handoff.LaunchStatusCompleted:
			if launch.EndedAt.IsZero() {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestLaunchInvalid,
					Message: fmt.Sprintf("latest launch %s is COMPLETED without ended_at", launch.AttemptID),
				})
			}
		case handoff.LaunchStatusFailed:
			if launch.EndedAt.IsZero() {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestLaunchInvalid,
					Message: fmt.Sprintf("latest launch %s is FAILED without ended_at", launch.AttemptID),
				})
			}
		default:
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestLaunchInvalid,
				Message: fmt.Sprintf("latest launch %s has unsupported status %s", launch.AttemptID, launch.Status),
			})
		}
		if launch.Status == handoff.LaunchStatusCompleted && snapshot.LatestAcknowledgment == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest completed launch %s for handoff %s has no persisted acknowledgment", launch.AttemptID, packet.HandoffID),
			})
		}
	}

	if snapshot.LatestAcknowledgment != nil {
		ack := snapshot.LatestAcknowledgment
		if ack.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment task mismatch: ack task=%s capsule task=%s", ack.TaskID, caps.TaskID),
			})
		}
		if ack.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment handoff mismatch: ack handoff=%s latest handoff=%s", ack.HandoffID, packet.HandoffID),
			})
		}
		if ack.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment target %s does not match latest handoff target %s", ack.TargetWorker, packet.TargetWorker),
			})
		}
		if packet.Status == handoff.StatusBlocked {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest handoff %s is BLOCKED but has a persisted launch acknowledgment", packet.HandoffID),
			})
		}
		if strings.TrimSpace(ack.LaunchID) == "" {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestAckInvalid,
				Message: fmt.Sprintf("latest acknowledgment for handoff %s has empty launch id", packet.HandoffID),
			})
		}
		if snapshot.LatestLaunch != nil {
			launch := snapshot.LatestLaunch
			switch launch.Status {
			case handoff.LaunchStatusCompleted:
				if strings.TrimSpace(launch.LaunchID) != "" && ack.LaunchID != launch.LaunchID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestAckInvalid,
						Message: fmt.Sprintf("latest acknowledgment launch %s does not match latest completed launch %s", ack.LaunchID, launch.LaunchID),
					})
				}
			case handoff.LaunchStatusFailed:
				if strings.TrimSpace(ack.LaunchID) != "" && (ack.LaunchID == launch.LaunchID || launch.LaunchID == "") {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestAckInvalid,
						Message: fmt.Sprintf("latest failed launch %s should not have acknowledgment state for the same attempt", launch.AttemptID),
					})
				}
			}
		}
	}

	if snapshot.LatestFollowThrough != nil {
		record := snapshot.LatestFollowThrough
		if record.TaskID != caps.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through task mismatch: record task=%s capsule task=%s", record.TaskID, caps.TaskID),
			})
		}
		if record.HandoffID != packet.HandoffID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through handoff mismatch: record handoff=%s latest handoff=%s", record.HandoffID, packet.HandoffID),
			})
		}
		if record.TargetWorker != packet.TargetWorker {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through target %s does not match latest handoff target %s", record.TargetWorker, packet.TargetWorker),
			})
		}
		if snapshot.LatestLaunch == nil {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestFollowThroughInvalid,
				Message: fmt.Sprintf("latest follow-through for handoff %s exists without a persisted launch attempt", packet.HandoffID),
			})
		} else {
			launch := snapshot.LatestLaunch
			if launch.Status != handoff.LaunchStatusCompleted {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through for handoff %s exists but latest launch %s is not COMPLETED", packet.HandoffID, launch.AttemptID),
				})
			}
			if strings.TrimSpace(record.LaunchAttemptID) == "" {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through for handoff %s has empty launch attempt id", packet.HandoffID),
				})
			} else if record.LaunchAttemptID != launch.AttemptID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through launch attempt %s does not match latest launch %s", record.LaunchAttemptID, launch.AttemptID),
				})
			}
			if strings.TrimSpace(record.LaunchID) != "" && strings.TrimSpace(launch.LaunchID) != "" && record.LaunchID != launch.LaunchID {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestFollowThroughInvalid,
					Message: fmt.Sprintf("latest follow-through launch id %s does not match latest launch id %s", record.LaunchID, launch.LaunchID),
				})
			}
		}
	}

	if snapshot.LatestResolution != nil {
		record := snapshot.LatestResolution
		if record.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{
				Code:    continuityViolationLatestResolutionInvalid,
				Message: fmt.Sprintf("latest handoff resolution task %s does not match capsule task %s", record.TaskID, snapshot.Capsule.TaskID),
			})
		} else {
			packet, err := c.store.Handoffs().Get(record.HandoffID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution references missing handoff %s", record.HandoffID),
					})
				} else {
					return nil, err
				}
			} else {
				if packet.TaskID != record.TaskID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution task %s does not match handoff task %s", record.TaskID, packet.TaskID),
					})
				}
				if record.TargetWorker != packet.TargetWorker {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution target %s does not match handoff target %s", record.TargetWorker, packet.TargetWorker),
					})
				}
			}
		}
		if record.LaunchAttemptID == "" {
			if record.LaunchID != "" {
				violations = append(violations, continuityViolation{
					Code:    continuityViolationLatestResolutionInvalid,
					Message: "latest handoff resolution has a launch id without a launch attempt id",
				})
			}
		} else {
			launch, err := c.store.Handoffs().GetLaunch(record.LaunchAttemptID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution references missing launch attempt %s", record.LaunchAttemptID),
					})
				} else {
					return nil, err
				}
			} else {
				if launch.HandoffID != record.HandoffID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution launch attempt %s belongs to handoff %s, not %s", record.LaunchAttemptID, launch.HandoffID, record.HandoffID),
					})
				}
				if launch.TaskID != record.TaskID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution launch attempt %s belongs to task %s, not %s", record.LaunchAttemptID, launch.TaskID, record.TaskID),
					})
				}
				if record.LaunchID != "" && launch.LaunchID != "" && record.LaunchID != launch.LaunchID {
					violations = append(violations, continuityViolation{
						Code:    continuityViolationLatestResolutionInvalid,
						Message: fmt.Sprintf("latest handoff resolution launch %s does not match launch attempt launch id %s", record.LaunchID, launch.LaunchID),
					})
				}
			}
		}
	}

	return violations, nil
}

func dedupeContinuityViolations(values []continuityViolation) []continuityViolation {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]continuityViolation, 0, len(values))
	for _, value := range values {
		key := string(value.Code) + "|" + value.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstContinuityViolationMessage(values []continuityViolation) string {
	if len(values) == 0 {
		return ""
	}
	return values[0].Message
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func repoAnchorsEqual(a checkpoint.RepoAnchor, b checkpoint.RepoAnchor) bool {
	return a.RepoRoot == b.RepoRoot &&
		a.WorktreePath == b.WorktreePath &&
		a.BranchName == b.BranchName &&
		a.HeadSHA == b.HeadSHA &&
		a.DirtyHash == b.DirtyHash &&
		a.UntrackedHash == b.UntrackedHash
}

func buildReplayBlockedLaunchResponse(packet handoff.Packet) LaunchHandoffResult {
	canonical := fmt.Sprintf(
		"Launch for handoff %s was previously requested, but Tuku does not have a durable completion or failure record for that request. The outcome is unknown, so automatic retry is blocked to avoid duplicate worker launch.",
		packet.HandoffID,
	)
	return LaunchHandoffResult{
		TaskID:            packet.TaskID,
		HandoffID:         packet.HandoffID,
		TargetWorker:      packet.TargetWorker,
		LaunchStatus:      HandoffLaunchStatusBlocked,
		CanonicalResponse: canonical,
	}
}

func (c *Coordinator) validateRecoveryActionContinuity(snapshot continuitySnapshot) ([]continuityViolation, error) {
	action := snapshot.LatestRecoveryAction
	violations := make([]continuityViolation, 0, 4)
	if action == nil {
		return violations, nil
	}
	if action.TaskID != snapshot.Capsule.TaskID {
		violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action task mismatch: action task=%s capsule task=%s", action.TaskID, snapshot.Capsule.TaskID)})
	}
	if action.RunID != "" {
		runRec, err := c.store.Runs().Get(action.RunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing run %s", action.RunID)})
			} else {
				return nil, err
			}
		} else if runRec.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action run task mismatch: run task=%s capsule task=%s", runRec.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.CheckpointID != "" {
		cp, err := c.store.Checkpoints().Get(action.CheckpointID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing checkpoint %s", action.CheckpointID)})
			} else {
				return nil, err
			}
		} else if cp.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action checkpoint task mismatch: checkpoint task=%s capsule task=%s", cp.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.HandoffID != "" {
		packet, err := c.store.Handoffs().Get(action.HandoffID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing handoff %s", action.HandoffID)})
			} else {
				return nil, err
			}
		} else if packet.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action handoff task mismatch: handoff task=%s capsule task=%s", packet.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	if action.LaunchAttemptID != "" {
		launch, err := c.store.Handoffs().GetLaunch(action.LaunchAttemptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action references missing launch attempt %s", action.LaunchAttemptID)})
			} else {
				return nil, err
			}
		} else if launch.TaskID != snapshot.Capsule.TaskID {
			violations = append(violations, continuityViolation{Code: continuityViolationLatestRecoveryActionInvalid, Message: fmt.Sprintf("latest recovery action launch task mismatch: launch task=%s capsule task=%s", launch.TaskID, snapshot.Capsule.TaskID)})
		}
	}
	return violations, nil
}

func latestHandoffID(packet *handoff.Packet) string {
	if packet == nil {
		return "none"
	}
	return packet.HandoffID
}
