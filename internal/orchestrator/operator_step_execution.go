package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/operatorstep"
	"tuku/internal/domain/proof"
)

type ExecutePrimaryOperatorStepRequest struct {
	TaskID string
}

type ExecutePrimaryOperatorStepResult struct {
	TaskID                     common.TaskID
	Receipt                    operatorstep.Receipt
	ActiveBranch               ActiveBranchProvenance
	OperatorDecision           OperatorDecisionSummary
	OperatorExecutionPlan      OperatorExecutionPlan
	RecoveryClass              RecoveryClass
	RecommendedAction          RecoveryAction
	ReadyForNextRun            bool
	ReadyForHandoffLaunch      bool
	RecoveryReason             string
	CanonicalResponse          string
	RecentOperatorStepReceipts []operatorstep.Receipt
}

type operatorStepExecutionDispatch struct {
	attempted         bool
	resultClass       operatorstep.ResultClass
	summary           string
	reason            string
	canonicalResponse string
	runID             common.RunID
	checkpointID      common.CheckpointID
	briefID           common.BriefID
	handoffID         string
	launchAttemptID   string
	launchID          string
}

func (c *Coordinator) ExecutePrimaryOperatorStep(ctx context.Context, req ExecutePrimaryOperatorStepRequest) (ExecutePrimaryOperatorStepResult, error) {
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return ExecutePrimaryOperatorStepResult{}, fmt.Errorf("task id is required")
	}

	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	_, _, _, _, plan, continuity, _, _ := c.operatorTruthForAssessment(assessment)
	if plan.PrimaryStep == nil {
		receipt, recErr := c.recordOperatorStepReceipt(ctx, taskID, plan, continuity, operatorStepExecutionDispatch{
			attempted:   false,
			resultClass: operatorstep.ResultRejected,
			summary:     "primary operator step is unavailable",
			reason:      "no primary operator step is currently available",
		}, nil)
		if recErr != nil {
			return ExecutePrimaryOperatorStepResult{}, recErr
		}
		fresh, err := c.buildPrimaryOperatorStepExecutionResult(ctx, taskID, receipt, "")
		if err != nil {
			return ExecutePrimaryOperatorStepResult{}, err
		}
		return fresh, nil
	}
	step := *plan.PrimaryStep
	if step.CommandSurface != OperatorCommandSurfaceDedicated {
		reason := fmt.Sprintf("primary operator step %s is guidance-only and cannot be executed directly", step.Action)
		receipt, recErr := c.recordOperatorStepReceipt(ctx, taskID, plan, continuity, operatorStepExecutionDispatch{
			attempted:   false,
			resultClass: operatorstep.ResultRejected,
			summary:     fmt.Sprintf("rejected %s", stepExecutionLabel(step.Action)),
			reason:      reason,
		}, &step)
		if recErr != nil {
			return ExecutePrimaryOperatorStepResult{}, recErr
		}
		fresh, err := c.buildPrimaryOperatorStepExecutionResult(ctx, taskID, receipt, "")
		if err != nil {
			return ExecutePrimaryOperatorStepResult{}, err
		}
		return fresh, nil
	}

	dispatch := c.dispatchPrimaryOperatorStep(ctx, taskID, step, continuity)
	receipt, err := c.recordOperatorStepReceipt(ctx, taskID, plan, continuity, dispatch, &step)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	return c.buildPrimaryOperatorStepExecutionResult(ctx, taskID, receipt, dispatch.canonicalResponse)
}

func (c *Coordinator) dispatchPrimaryOperatorStep(ctx context.Context, taskID common.TaskID, step OperatorExecutionStep, continuity HandoffContinuity) operatorStepExecutionDispatch {
	switch step.Action {
	case OperatorActionStartLocalRun:
		out, err := c.RunTask(ctx, RunTaskRequest{TaskID: string(taskID), Action: "start", Mode: "real"})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("started local run %s", out.RunID),
			canonicalResponse: out.CanonicalResponse,
			runID:             out.RunID,
		}
	case OperatorActionResumeInterruptedLineage:
		out, err := c.ExecuteInterruptedResume(ctx, ExecuteInterruptedResumeRequest{TaskID: string(taskID)})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("resumed interrupted lineage for brief %s", out.BriefID),
			canonicalResponse: out.CanonicalResponse,
			runID:             out.Action.RunID,
			checkpointID:      out.Action.CheckpointID,
			briefID:           out.BriefID,
		}
	case OperatorActionFinalizeContinueRecovery:
		out, err := c.ExecuteContinueRecovery(ctx, ExecuteContinueRecoveryRequest{TaskID: string(taskID)})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("finalized continue recovery for brief %s", out.BriefID),
			canonicalResponse: out.CanonicalResponse,
			runID:             out.Action.RunID,
			checkpointID:      out.Action.CheckpointID,
			briefID:           out.BriefID,
		}
	case OperatorActionExecuteRebrief:
		out, err := c.ExecuteRebrief(ctx, ExecuteRebriefRequest{TaskID: string(taskID)})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("regenerated brief %s", out.BriefID),
			canonicalResponse: out.CanonicalResponse,
			briefID:           out.BriefID,
		}
	case OperatorActionLaunchAcceptedHandoff:
		out, err := c.LaunchHandoff(ctx, LaunchHandoffRequest{TaskID: string(taskID), HandoffID: continuity.HandoffID})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		dispatch := launchDispatchFromResult(continuity, out)
		dispatch.attempted = true
		return dispatch
	case OperatorActionResolveActiveHandoff:
		out, err := c.RecordHandoffResolution(ctx, RecordHandoffResolutionRequest{
			TaskID:    string(taskID),
			HandoffID: continuity.HandoffID,
			Kind:      handoff.ResolutionSupersededByLocal,
			Summary:   "operator-next returned canonical local control",
		})
		if err != nil {
			return classifyOperatorStepError(step, err)
		}
		return operatorStepExecutionDispatch{
			attempted:         true,
			resultClass:       operatorstep.ResultSucceeded,
			summary:           fmt.Sprintf("resolved active handoff %s as %s", out.Record.HandoffID, out.Record.Kind),
			canonicalResponse: out.CanonicalResponse,
			handoffID:         out.Record.HandoffID,
			launchAttemptID:   out.Record.LaunchAttemptID,
			launchID:          out.Record.LaunchID,
		}
	default:
		return operatorStepExecutionDispatch{
			attempted:   false,
			resultClass: operatorstep.ResultRejected,
			summary:     fmt.Sprintf("rejected %s", stepExecutionLabel(step.Action)),
			reason:      fmt.Sprintf("primary operator step %s does not have a dedicated unified backend execution path", step.Action),
		}
	}
}

func launchDispatchFromResult(continuity HandoffContinuity, out LaunchHandoffResult) operatorStepExecutionDispatch {
	resultClass := operatorstep.ResultSucceeded
	summary := fmt.Sprintf("launched accepted handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
	reason := ""
	switch out.LaunchStatus {
	case HandoffLaunchStatusBlocked:
		resultClass = operatorstep.ResultRejected
		summary = fmt.Sprintf("rejected launch of accepted handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
		reason = strings.TrimSpace(out.CanonicalResponse)
	case HandoffLaunchStatusFailed:
		resultClass = operatorstep.ResultFailed
		summary = fmt.Sprintf("failed launch of accepted handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
		reason = strings.TrimSpace(out.CanonicalResponse)
	case HandoffLaunchStatusCompleted:
		if continuity.LaunchID != "" && continuity.LaunchID == out.LaunchID {
			resultClass = operatorstep.ResultNoopReused
			summary = fmt.Sprintf("reused durable launch result for handoff %s", nonEmpty(out.HandoffID, continuity.HandoffID))
		}
	}
	return operatorStepExecutionDispatch{
		resultClass:       resultClass,
		summary:           summary,
		reason:            reason,
		canonicalResponse: out.CanonicalResponse,
		handoffID:         nonEmpty(out.HandoffID, continuity.HandoffID),
		launchID:          out.LaunchID,
	}
}

func classifyOperatorStepError(step OperatorExecutionStep, err error) operatorStepExecutionDispatch {
	reason := strings.TrimSpace(err.Error())
	class := operatorstep.ResultFailed
	lower := strings.ToLower(reason)
	for _, token := range []string{"already", "blocked", "cannot", "requires", "not ", "missing", "unsupported", "mismatch", "guidance-only", "no active", "no primary", "only be executed", "rejected"} {
		if strings.Contains(lower, token) {
			class = operatorstep.ResultRejected
			break
		}
	}
	summary := fmt.Sprintf("failed %s", stepExecutionLabel(step.Action))
	if class == operatorstep.ResultRejected {
		summary = fmt.Sprintf("rejected %s", stepExecutionLabel(step.Action))
	}
	return operatorStepExecutionDispatch{
		attempted:   true,
		resultClass: class,
		summary:     summary,
		reason:      reason,
	}
}

func stepExecutionLabel(action OperatorAction) string {
	return strings.ToLower(strings.TrimSpace(string(action)))
}

func (c *Coordinator) recordOperatorStepReceipt(_ context.Context, taskID common.TaskID, plan OperatorExecutionPlan, continuity HandoffContinuity, dispatch operatorStepExecutionDispatch, step *OperatorExecutionStep) (operatorstep.Receipt, error) {
	now := c.clock()
	receipt := operatorstep.Receipt{
		Version:            1,
		ReceiptID:          c.idGenerator("orec"),
		TaskID:             taskID,
		ExecutionAttempted: dispatch.attempted,
		ResultClass:        dispatch.resultClass,
		Summary:            strings.TrimSpace(dispatch.summary),
		Reason:             strings.TrimSpace(dispatch.reason),
		RunID:              dispatch.runID,
		CheckpointID:       dispatch.checkpointID,
		BriefID:            dispatch.briefID,
		HandoffID:          dispatch.handoffID,
		LaunchAttemptID:    dispatch.launchAttemptID,
		LaunchID:           dispatch.launchID,
		CreatedAt:          now,
	}
	if step != nil {
		receipt.ActionHandle = string(step.Action)
		receipt.ExecutionDomain = string(step.Domain)
		receipt.CommandSurfaceKind = string(step.CommandSurface)
	} else if plan.PrimaryStep != nil {
		receipt.ActionHandle = string(plan.PrimaryStep.Action)
		receipt.ExecutionDomain = string(plan.PrimaryStep.Domain)
		receipt.CommandSurfaceKind = string(plan.PrimaryStep.CommandSurface)
	}
	completedAt := now
	receipt.CompletedAt = &completedAt
	if receipt.HandoffID == "" {
		receipt.HandoffID = continuity.HandoffID
	}
	if receipt.LaunchAttemptID == "" {
		receipt.LaunchAttemptID = continuity.LaunchAttemptID
	}
	if receipt.LaunchID == "" {
		receipt.LaunchID = continuity.LaunchID
	}

	err := c.withTx(func(txc *Coordinator) error {
		if err := txc.store.OperatorStepReceipts().Create(receipt); err != nil {
			return err
		}
		caps, err := txc.store.Capsules().Get(taskID)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"receipt_id":           receipt.ReceiptID,
			"action_handle":        receipt.ActionHandle,
			"execution_domain":     receipt.ExecutionDomain,
			"command_surface_kind": receipt.CommandSurfaceKind,
			"execution_attempted":  receipt.ExecutionAttempted,
			"result_class":         receipt.ResultClass,
			"summary":              receipt.Summary,
			"reason":               receipt.Reason,
			"handoff_id":           receipt.HandoffID,
			"launch_attempt_id":    receipt.LaunchAttemptID,
			"launch_id":            receipt.LaunchID,
			"brief_id":             receipt.BriefID,
			"checkpoint_id":        receipt.CheckpointID,
			"run_id":               receipt.RunID,
		}
		return txc.appendProof(caps, proof.EventOperatorStepExecutionRecorded, proof.ActorUser, "user", payload, runIDPointer(receipt.RunID))
	})
	if err != nil {
		return operatorstep.Receipt{}, err
	}
	return receipt, nil
}

func (c *Coordinator) buildPrimaryOperatorStepExecutionResult(ctx context.Context, taskID common.TaskID, receipt operatorstep.Receipt, canonicalResponse string) (ExecutePrimaryOperatorStepResult, error) {
	assessment, err := c.assessContinue(ctx, taskID)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	recovery, branch, _, decision, plan, _, _, _ := c.operatorTruthForAssessment(assessment)
	recent, err := c.store.OperatorStepReceipts().ListByTask(taskID, 5)
	if err != nil {
		return ExecutePrimaryOperatorStepResult{}, err
	}
	return ExecutePrimaryOperatorStepResult{
		TaskID:                     taskID,
		Receipt:                    receipt,
		ActiveBranch:               branch,
		OperatorDecision:           decision,
		OperatorExecutionPlan:      plan,
		RecoveryClass:              recovery.RecoveryClass,
		RecommendedAction:          recovery.RecommendedAction,
		ReadyForNextRun:            recovery.ReadyForNextRun,
		ReadyForHandoffLaunch:      recovery.ReadyForHandoffLaunch,
		RecoveryReason:             recovery.Reason,
		CanonicalResponse:          canonicalResponse,
		RecentOperatorStepReceipts: append([]operatorstep.Receipt{}, recent...),
	}, nil
}

func (c *Coordinator) operatorTruthForAssessment(assessment continueAssessment) (RecoveryAssessment, ActiveBranchProvenance, OperatorActionAuthoritySet, OperatorDecisionSummary, OperatorExecutionPlan, HandoffContinuity, LocalRunFinalization, LocalResumeAuthority) {
	recovery := c.recoveryFromContinueAssessment(assessment)
	branch := deriveActiveBranchProvenanceFromAssessment(assessment, recovery)
	runFinalization := deriveLocalRunFinalization(assessment, recovery)
	localResume := deriveLocalResumeAuthority(assessment, recovery)
	actions := deriveOperatorActionAuthoritySet(assessment, recovery, branch, runFinalization, localResume)
	decision := deriveOperatorDecisionSummary(assessment, recovery, branch, runFinalization, localResume, actions)
	plan := deriveOperatorExecutionPlan(assessment, branch, actions, decision)
	continuity := assessHandoffContinuity(assessment.TaskID, assessment.LatestHandoff, assessment.LatestLaunch, assessment.LatestAck, assessment.LatestFollowThrough, assessment.LatestResolution)
	return recovery, branch, actions, decision, plan, continuity, runFinalization, localResume
}
