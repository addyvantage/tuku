package orchestrator

import (
	"fmt"
	"strings"

	"tuku/internal/domain/common"
)

type OperatorCommandSurfaceType string

const (
	OperatorCommandSurfaceNone            OperatorCommandSurfaceType = "NONE"
	OperatorCommandSurfaceDedicated       OperatorCommandSurfaceType = "DEDICATED"
	OperatorCommandSurfaceInspectFallback OperatorCommandSurfaceType = "INSPECT_FALLBACK"
)

type OperatorCommandSurface struct {
	Action       OperatorAction
	CanonicalCLI string
	SurfaceType  OperatorCommandSurfaceType
	Aliases      []string
}

func canonicalOperatorCommandSurface(taskID common.TaskID, continuity HandoffContinuity, action OperatorAction) OperatorCommandSurface {
	switch action {
	case OperatorActionLocalMessageMutation:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku message --task %s --text \"<message>\"", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionCreateCheckpoint:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku checkpoint --task %s", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionStartLocalRun:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku run --task %s --action start", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionReconcileStaleRun:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku continue --task %s", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionInspectFailedRun, OperatorActionReviewValidationState, OperatorActionReviewHandoffFollowUp, OperatorActionRepairContinuity:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku inspect --task %s", taskID),
			SurfaceType:  OperatorCommandSurfaceInspectFallback,
		}
	case OperatorActionMakeResumeDecision:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku recovery record --task %s --action <decision-continue|decision-regenerate-brief>", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionResumeInterruptedLineage:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku recovery resume-interrupted --task %s", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionFinalizeContinueRecovery:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku recovery continue --task %s", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionExecuteRebrief:
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: fmt.Sprintf("tuku recovery rebrief --task %s", taskID),
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionLaunchAcceptedHandoff:
		hint := fmt.Sprintf("tuku handoff-launch --task %s", taskID)
		if strings.TrimSpace(continuity.HandoffID) != "" {
			hint = fmt.Sprintf("%s --handoff %s", hint, continuity.HandoffID)
		}
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: hint,
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	case OperatorActionResolveActiveHandoff:
		hint := fmt.Sprintf("tuku handoff-resolve --task %s", taskID)
		if strings.TrimSpace(continuity.HandoffID) != "" {
			hint = fmt.Sprintf("%s --handoff %s", hint, continuity.HandoffID)
		}
		hint += " --kind <abandoned|superseded-by-local|closed-unproven|reviewed-stale>"
		return OperatorCommandSurface{
			Action:       action,
			CanonicalCLI: hint,
			SurfaceType:  OperatorCommandSurfaceDedicated,
		}
	default:
		return OperatorCommandSurface{Action: action, SurfaceType: OperatorCommandSurfaceNone}
	}
}
