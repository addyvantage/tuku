package orchestrator

import (
	"testing"

	"tuku/internal/domain/common"
)

func TestCanonicalOperatorCommandSurfaceMapsMajorActions(t *testing.T) {
	taskID := common.TaskID("tsk_cmd")
	continuity := HandoffContinuity{HandoffID: "hnd_cmd"}

	tests := []struct {
		action      OperatorAction
		wantHint    string
		wantSurface OperatorCommandSurfaceType
	}{
		{action: OperatorActionStartLocalRun, wantHint: "tuku run --task tsk_cmd --action start", wantSurface: OperatorCommandSurfaceDedicated},
		{action: OperatorActionResumeInterruptedLineage, wantHint: "tuku recovery resume-interrupted --task tsk_cmd", wantSurface: OperatorCommandSurfaceDedicated},
		{action: OperatorActionFinalizeContinueRecovery, wantHint: "tuku recovery continue --task tsk_cmd", wantSurface: OperatorCommandSurfaceDedicated},
		{action: OperatorActionLaunchAcceptedHandoff, wantHint: "tuku handoff-launch --task tsk_cmd --handoff hnd_cmd", wantSurface: OperatorCommandSurfaceDedicated},
		{action: OperatorActionResolveActiveHandoff, wantHint: "tuku handoff-resolve --task tsk_cmd --handoff hnd_cmd --kind <abandoned|superseded-by-local|closed-unproven|reviewed-stale>", wantSurface: OperatorCommandSurfaceDedicated},
		{action: OperatorActionReconcileStaleRun, wantHint: "tuku continue --task tsk_cmd", wantSurface: OperatorCommandSurfaceDedicated},
		{action: OperatorActionReviewHandoffFollowUp, wantHint: "tuku inspect --task tsk_cmd", wantSurface: OperatorCommandSurfaceInspectFallback},
	}

	for _, tt := range tests {
		got := canonicalOperatorCommandSurface(taskID, continuity, tt.action)
		if got.CanonicalCLI != tt.wantHint || got.SurfaceType != tt.wantSurface {
			t.Fatalf("unexpected command surface for %s: %+v", tt.action, got)
		}
	}
}

func TestCanonicalOperatorCommandSurfaceDoesNotInventCommandForUnknownAction(t *testing.T) {
	got := canonicalOperatorCommandSurface(common.TaskID("tsk_none"), HandoffContinuity{}, OperatorActionNone)
	if got.CanonicalCLI != "" || got.SurfaceType != OperatorCommandSurfaceNone {
		t.Fatalf("expected no canonical command surface for NONE action, got %+v", got)
	}
}
