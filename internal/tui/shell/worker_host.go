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
}

func NewTranscriptHost() *TranscriptHost {
	return &TranscriptHost{
		status: HostStatus{
			Mode:           HostModeTranscript,
			State:          HostStateTranscriptOnly,
			Label:          "transcript",
			StateChangedAt: time.Now().UTC(),
		},
	}
}

func (h *TranscriptHost) Start(_ context.Context, snapshot Snapshot) error {
	h.snapshot = snapshot
	if h.status.State == "" {
		h.markTranscriptOnly("")
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

func (h *TranscriptHost) markFallback(note string) {
	h.status.Mode = HostModeTranscript
	h.status.State = HostStateFallback
	h.status.Label = "transcript fallback"
	h.status.InputLive = false
	h.status.Note = strings.TrimSpace(note)
	h.status.StateChangedAt = time.Now().UTC()
	if h.status.Note != "" {
		h.recordActivity("worker host degraded: " + h.status.Note)
	}
}

func (h *TranscriptHost) markTranscriptOnly(note string) {
	h.status.Mode = HostModeTranscript
	h.status.State = HostStateTranscriptOnly
	h.status.Label = "transcript"
	h.status.InputLive = false
	h.status.Note = strings.TrimSpace(note)
	h.status.StateChangedAt = time.Now().UTC()
}

func (h *TranscriptHost) recordActivity(message string) {
	stamped := fmt.Sprintf("%s  %s", time.Now().UTC().Format("15:04:05"), message)
	h.activity = append(h.activity, stamped)
	if len(h.activity) > hostMaxActivity {
		h.activity = h.activity[len(h.activity)-hostMaxActivity:]
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
