package shell

import (
	"fmt"
	"strings"
	"time"
)

func successfulPrimaryActionResult(step OperatorExecutionStep, before Snapshot, after Snapshot, receipt OperatorStepReceiptSummary, now time.Time) *PrimaryActionResultSummary {
	result := &PrimaryActionResultSummary{
		Action:      strings.TrimSpace(step.Action),
		Outcome:     "SUCCESS",
		Summary:     nonEmpty(strings.TrimSpace(receipt.Summary), fmt.Sprintf("executed %s", operatorActionDisplayName(step.Action))),
		Deltas:      meaningfulPrimaryActionDeltas(before, after),
		NextStep:    compactNextStep(after),
		ReceiptID:   strings.TrimSpace(receipt.ReceiptID),
		ResultClass: strings.TrimSpace(receipt.ResultClass),
		CreatedAt:   now.UTC(),
	}
	if len(result.Deltas) == 0 {
		result.Deltas = []string{"no operator-visible control-plane delta"}
	}
	return result
}

func failedPrimaryActionResult(step OperatorExecutionStep, err error, now time.Time) *PrimaryActionResultSummary {
	text := ""
	if err != nil {
		text = strings.TrimSpace(err.Error())
	}
	return &PrimaryActionResultSummary{
		Action:      strings.TrimSpace(step.Action),
		Outcome:     "FAILED",
		Summary:     fmt.Sprintf("failed %s", operatorActionDisplayName(step.Action)),
		ErrorText:   text,
		ResultClass: "FAILED",
		CreatedAt:   now.UTC(),
	}
}

func failedPrimaryActionResultWithReceipt(step OperatorExecutionStep, receipt OperatorStepReceiptSummary, now time.Time) *PrimaryActionResultSummary {
	return &PrimaryActionResultSummary{
		Action:      strings.TrimSpace(step.Action),
		Outcome:     "FAILED",
		Summary:     nonEmpty(strings.TrimSpace(receipt.Summary), fmt.Sprintf("failed %s", operatorActionDisplayName(step.Action))),
		ErrorText:   nonEmpty(strings.TrimSpace(receipt.Reason), strings.TrimSpace(receipt.Summary)),
		ReceiptID:   strings.TrimSpace(receipt.ReceiptID),
		ResultClass: strings.TrimSpace(receipt.ResultClass),
		CreatedAt:   now.UTC(),
	}
}

func receiptIsFailure(receipt OperatorStepReceiptSummary) bool {
	switch strings.TrimSpace(receipt.ResultClass) {
	case "FAILED", "REJECTED":
		return true
	default:
		return false
	}
}

func meaningfulPrimaryActionDeltas(before Snapshot, after Snapshot) []string {
	type candidate struct {
		label string
		from  string
		to    string
	}
	candidates := []candidate{
		{label: "branch", from: compactActiveBranchOwner(before), to: compactActiveBranchOwner(after)},
		{label: "decision", from: operatorDecisionHeadline(before), to: operatorDecisionHeadline(after)},
		{label: "next", from: compactNextStep(before), to: compactNextStep(after)},
		{label: "launch", from: launchControlLine(before), to: launchControlLine(after)},
		{label: "handoff", from: handoffContinuityLine(before), to: handoffContinuityLine(after)},
		{label: "local message", from: compactActionAuthorityState(before, "LOCAL_MESSAGE_MUTATION"), to: compactActionAuthorityState(after, "LOCAL_MESSAGE_MUTATION")},
		{label: "local resume", from: localResumeLine(before), to: localResumeLine(after)},
		{label: "local run", from: localRunFinalizationLine(before), to: localRunFinalizationLine(after)},
	}

	out := make([]string, 0, 4)
	for _, item := range candidates {
		from := compactResultValue(item.from)
		to := compactResultValue(item.to)
		if from == to {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s -> %s", item.label, from, to))
		if len(out) == 4 {
			break
		}
	}
	return out
}

func compactResultValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "n/a"
	}
	return value
}

func compactNextStep(snapshot Snapshot) string {
	if plan := operatorExecutionPlanLine(snapshot); plan != "n/a" && strings.TrimSpace(plan) != "" {
		return plan
	}
	if action := operatorActionLabel(snapshot); action != "none" && action != "n/a" && strings.TrimSpace(action) != "" {
		return action
	}
	return "none"
}

func compactActionAuthorityState(snapshot Snapshot, action string) string {
	authority := authorityFor(snapshot, action)
	if authority == nil {
		return "n/a"
	}
	switch authority.State {
	case "ALLOWED":
		return "allowed"
	case "BLOCKED":
		return "blocked"
	case "REQUIRED_NEXT":
		return "required"
	case "NOT_APPLICABLE":
		return "not applicable"
	default:
		return humanizeConstant(authority.State)
	}
}

func compactActiveBranchOwner(snapshot Snapshot) string {
	if snapshot.ActiveBranch == nil || strings.TrimSpace(snapshot.ActiveBranch.Class) == "" {
		return "n/a"
	}
	switch snapshot.ActiveBranch.Class {
	case "LOCAL":
		return "local"
	case "HANDOFF_CLAUDE":
		if strings.TrimSpace(snapshot.ActiveBranch.BranchRef) != "" {
			return "Claude " + shortTaskID(snapshot.ActiveBranch.BranchRef)
		}
		return "Claude"
	default:
		return humanizeConstant(snapshot.ActiveBranch.Class)
	}
}
