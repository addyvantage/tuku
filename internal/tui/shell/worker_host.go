package shell

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type TranscriptHost struct {
	snapshot Snapshot
	status   HostStatus
	activity []string
	transcriptPending []TranscriptEvidenceChunk
}

func NewTranscriptHost() *TranscriptHost {
	return &TranscriptHost{
		status: HostStatus{
			Mode:                  HostModeTranscript,
			State:                 HostStateTranscriptOnly,
			Label:                 "transcript",
			WorkerSessionIDSource: WorkerSessionIDSourceNone,
			StateChangedAt:        time.Now().UTC(),
		},
	}
}

func (h *TranscriptHost) Start(_ context.Context, snapshot Snapshot) error {
	h.snapshot = snapshot
	if h.status.State == "" {
		h.markTranscriptOnly("")
	} else {
		h.enqueueTranscriptNote("transcript pane active; no live worker is attached")
	}
	return nil
}

func (h *TranscriptHost) Stop() error {
	return nil
}

func (h *TranscriptHost) UpdateSnapshot(snapshot Snapshot) {
	h.snapshot = snapshot
}

func (h *TranscriptHost) Resize(width int, height int) bool {
	h.status.Width = width
	h.status.Height = height
	return false
}

func (h *TranscriptHost) CanAcceptInput() bool {
	return false
}

func (h *TranscriptHost) WriteInput(_ []byte) bool {
	return false
}

func (h *TranscriptHost) Status() HostStatus {
	return h.status
}

func (h *TranscriptHost) Title() string {
	if isScratchIntakeSnapshot(h.snapshot) {
		return "worker pane | local scratch intake"
	}
	switch h.status.State {
	case HostStateFallback:
		return "worker pane | transcript fallback | read-only"
	case HostStateTranscriptOnly:
		if h.snapshot.Run != nil && h.snapshot.Run.WorkerKind != "" {
			return fmt.Sprintf("worker pane | %s transcript | read-only", h.snapshot.Run.WorkerKind)
		}
		if h.snapshot.Handoff != nil && h.snapshot.Handoff.TargetWorker != "" {
			return fmt.Sprintf("worker pane | %s handoff context | read-only", h.snapshot.Handoff.TargetWorker)
		}
	}
	if h.snapshot.Run != nil && h.snapshot.Run.WorkerKind != "" {
		return fmt.Sprintf("worker pane | %s transcript | read-only", h.snapshot.Run.WorkerKind)
	}
	if h.snapshot.Handoff != nil && h.snapshot.Handoff.TargetWorker != "" {
		return fmt.Sprintf("worker pane | %s handoff context | read-only", h.snapshot.Handoff.TargetWorker)
	}
	return "worker pane | tuku transcript | read-only"
}

func (h *TranscriptHost) WorkerLabel() string {
	return ""
}

func (h *TranscriptHost) Lines(height int, width int) []string {
	if height < 1 {
		return nil
	}
	lines := make([]string, 0, len(h.snapshot.RecentConversation)*2+6)
	lines = append(lines, transcriptBannerLines(h.status, width)...)
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	if len(h.snapshot.RecentShellTranscript) > 0 {
		lines = append(lines, wrapText("Recent durable transcript evidence (bounded retention):", width)...)
		if summary := transcriptEvidenceSummaryForSnapshot(h.snapshot); summary != "" {
			lines = append(lines, wrapText(summary, width)...)
		}
		for _, chunk := range h.snapshot.RecentShellTranscript {
			prefix := "evidence> "
			if strings.TrimSpace(chunk.Source) == "worker_output" {
				prefix = "worker> "
			}
			lines = append(lines, wrapPrefixedOutput(prefix, chunk.Content, width)...)
		}
		lines = append(lines, "")
	}
	if len(h.snapshot.RecentConversation) == 0 {
		lines = append(lines, wrapText("No transcript is attached yet.", width)...)
		return fitBottom(lines, height)
	}
	for _, msg := range h.snapshot.RecentConversation {
		prefix := transcriptPrefix(msg.Role)
		body := strings.TrimSpace(msg.Body)
		if body == "" {
			continue
		}
		lines = append(lines, wrapPrefixedOutput(prefix, body, width)...)
		lines = append(lines, "")
	}
	return fitBottom(lines, height)
}

func (h *TranscriptHost) ActivityLines(limit int) []string {
	if limit <= 0 || limit >= len(h.activity) {
		return append([]string{}, h.activity...)
	}
	return append([]string{}, h.activity[len(h.activity)-limit:]...)
}

func (h *TranscriptHost) DrainTranscriptEvidence(limit int) []TranscriptEvidenceChunk {
	if len(h.transcriptPending) == 0 {
		return nil
	}
	if limit <= 0 || limit >= len(h.transcriptPending) {
		out := append([]TranscriptEvidenceChunk{}, h.transcriptPending...)
		h.transcriptPending = nil
		return out
	}
	out := append([]TranscriptEvidenceChunk{}, h.transcriptPending[:limit]...)
	h.transcriptPending = append([]TranscriptEvidenceChunk{}, h.transcriptPending[limit:]...)
	return out
}

func (h *TranscriptHost) markFallback(note string) {
	h.status.Mode = HostModeTranscript
	h.status.State = HostStateFallback
	h.status.Label = "transcript fallback"
	h.status.InputLive = false
	h.status.Note = strings.TrimSpace(note)
	h.status.StateChangedAt = time.Now().UTC()
	if h.status.Note != "" {
		h.recordActivity("worker host degraded: " + h.status.Note)
		h.enqueueTranscriptNote(h.status.Note)
	} else {
		h.enqueueTranscriptNote("transcript fallback active")
	}
}

func (h *TranscriptHost) markTranscriptOnly(note string) {
	h.status.Mode = HostModeTranscript
	h.status.State = HostStateTranscriptOnly
	h.status.Label = "transcript"
	h.status.InputLive = false
	h.status.Note = strings.TrimSpace(note)
	h.status.StateChangedAt = time.Now().UTC()
	if h.status.Note != "" {
		h.enqueueTranscriptNote(h.status.Note)
	} else {
		h.enqueueTranscriptNote("transcript-only mode active")
	}
}

func (h *TranscriptHost) recordActivity(message string) {
	stamped := fmt.Sprintf("%s  %s", time.Now().UTC().Format("15:04:05"), message)
	h.activity = append(h.activity, stamped)
	if len(h.activity) > hostMaxActivity {
		h.activity = h.activity[len(h.activity)-hostMaxActivity:]
	}
}

func (h *TranscriptHost) enqueueTranscriptNote(note string) {
	note = strings.TrimSpace(note)
	if note == "" {
		return
	}
	h.transcriptPending = append(h.transcriptPending, TranscriptEvidenceChunk{
		Source:    "fallback_note",
		Content:   note,
		CreatedAt: time.Now().UTC(),
	})
	if len(h.transcriptPending) > hostMaxLines {
		h.transcriptPending = h.transcriptPending[len(h.transcriptPending)-hostMaxLines:]
	}
}

func transcriptPrefix(role string) string {
	switch role {
	case "user":
		return "you> "
	case "worker":
		return "worker> "
	default:
		return "tuku> "
	}
}

func transcriptBannerLines(status HostStatus, width int) []string {
	switch status.State {
	case HostStateFallback:
		lines := wrapText("Live input is unavailable in this pane.", width)
		if note := strings.TrimSpace(status.Note); note != "" {
			lines = append(lines, wrapText(note, width)...)
		}
		return lines
	case HostStateTranscriptOnly:
		lines := wrapText("No live worker is attached to this pane.", width)
		if note := strings.TrimSpace(status.Note); note != "" {
			lines = append(lines, wrapText(note, width)...)
		}
		return lines
	default:
		return nil
	}
}

func transcriptEvidenceSummaryForSnapshot(snapshot Snapshot) string {
	session, ok := latestTranscriptSession(snapshot)
	if !ok {
		return ""
	}
	switch strings.TrimSpace(session.TranscriptState) {
	case "bounded_available":
		return fmt.Sprintf("retained %d chunks within the bounded transcript window", session.TranscriptRetainedChunks)
	case "bounded_partial":
		return fmt.Sprintf("partial retained transcript: %d retained, %d dropped by bounded retention", session.TranscriptRetainedChunks, session.TranscriptDroppedChunks)
	case "transcript_only_bounded_available":
		return fmt.Sprintf("transcript-only fallback evidence retained: %d chunks", session.TranscriptRetainedChunks)
	case "transcript_only_bounded_partial":
		return fmt.Sprintf("transcript-only partial evidence: %d retained, %d dropped by bounded retention", session.TranscriptRetainedChunks, session.TranscriptDroppedChunks)
	default:
		return ""
	}
}

func latestTranscriptSession(snapshot Snapshot) (KnownShellSession, bool) {
	var (
		latest KnownShellSession
		found  bool
	)
	for _, session := range snapshot.ShellSessions {
		if strings.TrimSpace(session.TranscriptState) == "" || strings.TrimSpace(session.TranscriptState) == "none" {
			continue
		}
		if !found || session.LastUpdatedAt.After(latest.LastUpdatedAt) {
			latest = session
			found = true
		}
	}
	return latest, found
}
