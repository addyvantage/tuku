package ipc

import "encoding/json"

type Method string

const (
	MethodStartTask                             Method = "task.start"
	MethodResolveShellTaskForRepo               Method = "task.shell.resolve"
	MethodSendMessage                           Method = "task.message"
	MethodContinueTask                          Method = "task.continue"
	MethodRecordRecoveryAction                  Method = "task.recovery.record"
	MethodReviewInterruptedRun                  Method = "task.recovery.review_interrupted"
	MethodExecuteRebrief                        Method = "task.recovery.rebrief"
	MethodExecuteInterruptedResume              Method = "task.recovery.resume_interrupted"
	MethodExecuteContinueRecovery               Method = "task.recovery.continue"
	MethodExecutePrimaryOperatorStep            Method = "task.operator.next"
	MethodOperatorAcknowledgeReviewGap          Method = "task.operator.acknowledge_review_gap"
	MethodTaskRun                               Method = "task.run"
	MethodTaskStatus                            Method = "task.status"
	MethodTaskInspect                           Method = "task.inspect"
	MethodTaskIntent                            Method = "task.intent"
	MethodTaskBrief                             Method = "task.brief"
	MethodTaskShellSnapshot                     Method = "task.shell.snapshot"
	MethodTaskShellLifecycle                    Method = "task.shell.lifecycle"
	MethodTaskShellTranscriptAppend             Method = "task.shell.transcript.append"
	MethodTaskShellTranscriptRead               Method = "task.shell.transcript.read"
	MethodTaskShellTranscriptReview             Method = "task.shell.transcript.review"
	MethodTaskShellTranscriptHistory            Method = "task.shell.transcript.history"
	MethodTaskTransitionHistory                 Method = "task.transition.history"
	MethodTaskContinuityIncidentSlice           Method = "task.continuity.incident.slice"
	MethodTaskContinuityIncidentTriage          Method = "task.continuity.incident.triage"
	MethodTaskContinuityIncidentTriageHistory   Method = "task.continuity.incident.triage.history"
	MethodTaskContinuityIncidentFollowUp        Method = "task.continuity.incident.followup"
	MethodTaskContinuityIncidentFollowUpHistory Method = "task.continuity.incident.followup.history"
	MethodTaskContinuityIncidentClosure         Method = "task.continuity.incident.closure"
	MethodTaskContinuityIncidentRisk            Method = "task.continuity.incident.risk"
	MethodTaskShellSessionReport                Method = "task.shell.session.report"
	MethodTaskShellSessions                     Method = "task.shell.sessions"
	MethodCreateCheckpoint                      Method = "task.checkpoint"
	MethodCreateHandoff                         Method = "task.handoff.create"
	MethodAcceptHandoff                         Method = "task.handoff.accept"
	MethodLaunchHandoff                         Method = "task.handoff.launch"
	MethodRecordHandoffFollowThrough            Method = "task.handoff.followthrough.record"
	MethodRecordHandoffResolution               Method = "task.handoff.resolve"
	MethodApproveDecision                       Method = "task.approve"
	MethodRejectDecision                        Method = "task.reject"
)

type Request struct {
	RequestID string          `json:"request_id"`
	Method    Method          `json:"method"`
	Payload   json.RawMessage `json:"payload"`
}

type Response struct {
	RequestID string          `json:"request_id"`
	OK        bool            `json:"ok"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     *ErrorPayload   `json:"error,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
