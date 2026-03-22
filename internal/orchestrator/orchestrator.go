package orchestrator

import "context"

type Service interface {
	StartTask(ctx context.Context, goal string, repoRoot string) (StartTaskResult, error)
	ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (ResolveShellTaskResult, error)
	MessageTask(ctx context.Context, taskID string, message string) (MessageTaskResult, error)
	RunTask(ctx context.Context, req RunTaskRequest) (RunTaskResult, error)
	ContinueTask(ctx context.Context, taskID string) (ContinueTaskResult, error)
	CreateCheckpoint(ctx context.Context, taskID string) (CreateCheckpointResult, error)
	CreateHandoff(ctx context.Context, req CreateHandoffRequest) (CreateHandoffResult, error)
	AcceptHandoff(ctx context.Context, req AcceptHandoffRequest) (AcceptHandoffResult, error)
	LaunchHandoff(ctx context.Context, req LaunchHandoffRequest) (LaunchHandoffResult, error)
	RecordHandoffFollowThrough(ctx context.Context, req RecordHandoffFollowThroughRequest) (RecordHandoffFollowThroughResult, error)
	RecordHandoffResolution(ctx context.Context, req RecordHandoffResolutionRequest) (RecordHandoffResolutionResult, error)
	RecordRecoveryAction(ctx context.Context, req RecordRecoveryActionRequest) (RecordRecoveryActionResult, error)
	ExecuteRebrief(ctx context.Context, req ExecuteRebriefRequest) (ExecuteRebriefResult, error)
	ExecuteInterruptedResume(ctx context.Context, req ExecuteInterruptedResumeRequest) (ExecuteInterruptedResumeResult, error)
	ExecuteContinueRecovery(ctx context.Context, req ExecuteContinueRecoveryRequest) (ExecuteContinueRecoveryResult, error)
	ExecutePrimaryOperatorStep(ctx context.Context, req ExecutePrimaryOperatorStepRequest) (ExecutePrimaryOperatorStepResult, error)
	StatusTask(ctx context.Context, taskID string) (StatusTaskResult, error)
	InspectTask(ctx context.Context, taskID string) (InspectTaskResult, error)
	ShellSnapshotTask(ctx context.Context, taskID string) (ShellSnapshotResult, error)
	RecordShellLifecycle(ctx context.Context, req RecordShellLifecycleRequest) (RecordShellLifecycleResult, error)
	ReportShellSession(ctx context.Context, req ReportShellSessionRequest) (ReportShellSessionResult, error)
	ListShellSessions(ctx context.Context, taskID string) (ListShellSessionsResult, error)
}
