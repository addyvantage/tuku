package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

const shellSessionHeartbeatInterval = 30 * time.Second

type SessionRegistryReport struct {
	TaskID                string
	SessionID             string
	WorkerPreference      WorkerPreference
	ResolvedWorker        WorkerPreference
	WorkerSessionID       string
	WorkerSessionIDSource WorkerSessionIDSource
	AttachCapability      WorkerAttachCapability
	StartedAt             time.Time
	HostMode              HostMode
	HostState             HostState
	Active                bool
	Note                  string
}

type SessionRegistrySink interface {
	Report(report SessionRegistryReport) error
}

type SessionRegistrySource interface {
	List(taskID string) ([]KnownShellSession, error)
}

type IPCSessionRegistryClient struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCSessionRegistryClient(socketPath string) *IPCSessionRegistryClient {
	return &IPCSessionRegistryClient{SocketPath: socketPath, Timeout: 5 * time.Second}
}

func (c *IPCSessionRegistryClient) Report(report SessionRegistryReport) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskShellSessionReportRequest{
		TaskID:                common.TaskID(report.TaskID),
		SessionID:             report.SessionID,
		WorkerPreference:      string(report.WorkerPreference),
		ResolvedWorker:        string(report.ResolvedWorker),
		WorkerSessionID:       report.WorkerSessionID,
		WorkerSessionIDSource: string(report.WorkerSessionIDSource),
		AttachCapability:      string(report.AttachCapability),
		HostMode:              string(report.HostMode),
		HostState:             string(report.HostState),
		StartedAt:             report.StartedAt,
		Active:                report.Active,
		Note:                  strings.TrimSpace(report.Note),
	})
	if err != nil {
		return err
	}

	_, err = ipc.CallUnix(ctx, c.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_session_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellSessionReport,
		Payload:   payload,
	})
	return err
}

func (c *IPCSessionRegistryClient) List(taskID string) ([]KnownShellSession, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload, err := json.Marshal(ipc.TaskShellSessionsRequest{TaskID: common.TaskID(taskID)})
	if err != nil {
		return nil, err
	}
	resp, err := ipc.CallUnix(ctx, c.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_sessions_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellSessions,
		Payload:   payload,
	})
	if err != nil {
		return nil, err
	}
	var raw ipc.TaskShellSessionsResponse
	if err := json.Unmarshal(resp.Payload, &raw); err != nil {
		return nil, err
	}
	out := make([]KnownShellSession, 0, len(raw.Sessions))
	for _, session := range raw.Sessions {
		sessionClass := KnownShellSessionClass(session.SessionClass)
		attachCapability := WorkerAttachCapability(session.AttachCapability)
		workerSessionIDSource := WorkerSessionIDSource(session.WorkerSessionIDSource)
		workerSessionIDSource = normalizeWorkerSessionIDSource(workerSessionIDSource, session.WorkerSessionID)
		if attachCapability == "" {
			attachCapability = WorkerAttachCapabilityNone
		}
		if sessionClass == "" {
			if session.Active {
				if session.WorkerSessionID != "" && workerSessionIDSource == WorkerSessionIDSourceAuthoritative && attachCapability == WorkerAttachCapabilityAttachable {
					sessionClass = KnownShellSessionClassAttachable
				} else {
					sessionClass = KnownShellSessionClassActiveUnattachable
				}
			} else {
				sessionClass = KnownShellSessionClassEnded
			}
		}
		mapped := KnownShellSession{
			SessionID:                        session.SessionID,
			TaskID:                           string(session.TaskID),
			WorkerPreference:                 WorkerPreference(session.WorkerPreference),
			ResolvedWorker:                   WorkerPreference(session.ResolvedWorker),
			WorkerSessionID:                  session.WorkerSessionID,
			WorkerSessionIDSource:            workerSessionIDSource,
			AttachCapability:                 attachCapability,
			HostMode:                         HostMode(session.HostMode),
			HostState:                        HostState(session.HostState),
			SessionClass:                     sessionClass,
			SessionClassReason:               session.SessionClassReason,
			ReattachGuidance:                 session.ReattachGuidance,
			OperatorSummary:                  session.OperatorSummary,
			TranscriptState:                  session.TranscriptState,
			TranscriptRetainedChunks:         session.TranscriptRetainedChunks,
			TranscriptDroppedChunks:          session.TranscriptDroppedChunks,
			TranscriptRetentionLimit:         session.TranscriptRetentionLimit,
			TranscriptOldestSequence:         session.TranscriptOldestSequence,
			TranscriptNewestSequence:         session.TranscriptNewestSequence,
			TranscriptLastChunkAt:            session.TranscriptLastChunkAt,
			TranscriptReviewID:               string(session.TranscriptReviewID),
			TranscriptReviewSource:           session.TranscriptReviewSource,
			TranscriptReviewedUpTo:           session.TranscriptReviewedUpTo,
			TranscriptReviewSummary:          session.TranscriptReviewSummary,
			TranscriptReviewAt:               session.TranscriptReviewAt,
			TranscriptReviewStale:            session.TranscriptReviewStale,
			TranscriptReviewNewer:            session.TranscriptReviewNewer,
			TranscriptReviewClosureState:     session.TranscriptReviewClosureState,
			TranscriptReviewOldestUnreviewed: session.TranscriptReviewOldestUnreviewed,
			StartedAt:                        session.StartedAt,
			LastUpdatedAt:                    session.LastUpdatedAt,
			Active:                           session.Active,
			Note:                             session.Note,
			LatestEventID:                    string(session.LatestEventID),
			LatestEventKind:                  session.LatestEventKind,
			LatestEventAt:                    session.LatestEventAt,
			LatestEventNote:                  session.LatestEventNote,
		}
		if len(session.TranscriptRecentReviews) > 0 {
			mapped.TranscriptRecentReviews = make([]TranscriptReviewMarker, 0, len(session.TranscriptRecentReviews))
			for _, review := range session.TranscriptRecentReviews {
				mapped.TranscriptRecentReviews = append(mapped.TranscriptRecentReviews, TranscriptReviewMarker{
					ReviewID:                 string(review.ReviewID),
					SourceFilter:             review.SourceFilter,
					ReviewedUpToSequence:     review.ReviewedUpToSequence,
					Summary:                  review.Summary,
					CreatedAt:                review.CreatedAt,
					TranscriptState:          review.TranscriptState,
					RetentionLimit:           review.RetentionLimit,
					RetainedChunks:           review.RetainedChunks,
					DroppedChunks:            review.DroppedChunks,
					OldestRetainedSequence:   review.OldestRetainedSequence,
					NewestRetainedSequence:   review.NewestRetainedSequence,
					StaleBehindLatest:        review.StaleBehindLatest,
					NewerRetainedCount:       review.NewerRetainedCount,
					OldestUnreviewedSequence: review.OldestUnreviewedSequence,
					ClosureState:             review.ClosureState,
				})
			}
		}
		out = append(out, mapped)
	}
	return out, nil
}

func reportShellSession(sink SessionRegistrySink, taskID string, session *SessionState, status HostStatus, active bool, ui *UIState) {
	if sink == nil || session == nil {
		return
	}
	refreshWorkerSessionAnchor(session, status)
	note := strings.TrimSpace(status.Note)
	if !active && note == "" {
		note = "shell session ended"
	}
	if err := sink.Report(SessionRegistryReport{
		TaskID:                taskID,
		SessionID:             session.SessionID,
		WorkerPreference:      session.WorkerPreference,
		ResolvedWorker:        session.ResolvedWorker,
		WorkerSessionID:       session.WorkerSessionID,
		WorkerSessionIDSource: session.WorkerSessionIDSource,
		AttachCapability:      session.AttachCapability,
		StartedAt:             session.StartedAt,
		HostMode:              status.Mode,
		HostState:             status.State,
		Active:                active,
		Note:                  note,
	}); err != nil && ui != nil {
		ui.LastError = "shell session registry update failed: " + err.Error()
	}
}

func loadKnownShellSessions(source SessionRegistrySource, taskID string, session *SessionState) error {
	if source == nil || session == nil {
		return nil
	}
	sessions, err := source.List(taskID)
	if err != nil {
		return err
	}
	session.KnownSessions = sessions
	return nil
}

func otherKnownShellSessions(session SessionState) []KnownShellSession {
	out := make([]KnownShellSession, 0, len(session.KnownSessions))
	for _, known := range session.KnownSessions {
		if known.SessionID == session.SessionID {
			continue
		}
		out = append(out, known)
	}
	return out
}

func latestKnownShellSessionByClass(session SessionState, class KnownShellSessionClass) (KnownShellSession, bool) {
	var latest KnownShellSession
	found := false
	for _, known := range otherKnownShellSessions(session) {
		if known.SessionClass != class {
			continue
		}
		if !found || known.LastUpdatedAt.After(latest.LastUpdatedAt) {
			latest = known
			found = true
		}
	}
	return latest, found
}

func sessionRegistrySummary(session SessionState) string {
	if attachable, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassAttachable); ok {
		reason := strings.TrimSpace(attachable.SessionClassReason)
		if reason == "" {
			reason = "ready for reattach"
		}
		return fmt.Sprintf("attachable session known: %s %s %s | %s", shortTaskID(attachable.SessionID), sessionWorkerLabel(attachable), string(attachable.HostState), reason)
	}
	if active, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassActiveUnattachable); ok {
		reason := strings.TrimSpace(active.SessionClassReason)
		if reason == "" {
			reason = "active but non-attachable"
		}
		return fmt.Sprintf("active non-attachable session known: %s %s %s | %s", shortTaskID(active.SessionID), sessionWorkerLabel(active), string(active.HostState), reason)
	}
	if stale, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassStale); ok {
		return fmt.Sprintf("stale session known: %s %s last update %s", shortTaskID(stale.SessionID), sessionWorkerLabel(stale), stale.LastUpdatedAt.Format("15:04:05"))
	}
	if ended, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassEnded); ok {
		return fmt.Sprintf("prior ended session known: %s %s %s", shortTaskID(ended.SessionID), sessionWorkerLabel(ended), string(ended.HostState))
	}
	return "fresh shell session; no other known shell session"
}

func sessionRegistryFooterLabel(session SessionState) string {
	if _, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassAttachable); ok {
		return "attachable-known"
	}
	if _, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassActiveUnattachable); ok {
		return "active-unattachable-known"
	}
	if _, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassStale); ok {
		return "stale-known"
	}
	if _, ok := latestKnownShellSessionByClass(session, KnownShellSessionClassEnded); ok {
		return "prior-ended-known"
	}
	return "fresh"
}

func sessionWorkerLabel(session KnownShellSession) string {
	if strings.TrimSpace(string(session.ResolvedWorker)) != "" {
		return string(session.ResolvedWorker)
	}
	if strings.TrimSpace(string(session.WorkerPreference)) != "" {
		return string(session.WorkerPreference)
	}
	return "worker"
}

func refreshWorkerSessionAnchor(session *SessionState, status HostStatus) {
	if session == nil {
		return
	}
	if workerSessionID := strings.TrimSpace(status.WorkerSessionID); workerSessionID != "" {
		session.WorkerSessionID = workerSessionID
		session.WorkerSessionIDSource = normalizeWorkerSessionIDSource(status.WorkerSessionIDSource, workerSessionID)
	}
	if isPTYHostMode(status.Mode) && status.State == HostStateLive {
		if strings.TrimSpace(session.WorkerSessionID) != "" && session.WorkerSessionIDSource == WorkerSessionIDSourceAuthoritative {
			session.AttachCapability = WorkerAttachCapabilityAttachable
		} else {
			session.AttachCapability = WorkerAttachCapabilityNone
		}
		return
	}
	session.AttachCapability = WorkerAttachCapabilityNone
}

func isPTYHostMode(mode HostMode) bool {
	return mode == HostModeCodexPTY || mode == HostModeClaudePTY
}

func normalizeWorkerSessionIDSource(source WorkerSessionIDSource, workerSessionID string) WorkerSessionIDSource {
	if strings.TrimSpace(workerSessionID) == "" {
		return WorkerSessionIDSourceNone
	}
	switch source {
	case WorkerSessionIDSourceAuthoritative, WorkerSessionIDSourceHeuristic, WorkerSessionIDSourceUnknown:
		return source
	default:
		return WorkerSessionIDSourceUnknown
	}
}
