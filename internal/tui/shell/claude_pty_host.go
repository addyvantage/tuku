package shell

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type ClaudePTYHost struct {
	binPath         string
	extraArgs       []string
	resumeSessionID string

	mu                sync.Mutex
	snapshot          Snapshot
	lines             []string
	activity          []string
	transcriptPending []TranscriptEvidenceChunk
	partial           string
	parserState       terminalParserState
	status            HostStatus

	ptyFile *os.File
	cmd     *exec.Cmd
}

func NewDefaultClaudePTYHost() *ClaudePTYHost {
	binPath := strings.TrimSpace(os.Getenv("TUKU_SHELL_CLAUDE_BIN"))
	if binPath == "" {
		binPath = strings.TrimSpace(os.Getenv("TUKU_CLAUDE_BIN"))
	}
	args := strings.TrimSpace(os.Getenv("TUKU_SHELL_CLAUDE_ARGS"))
	if args == "" {
		args = strings.TrimSpace(os.Getenv("TUKU_CLAUDE_ARGS"))
	}
	return &ClaudePTYHost{
		binPath:   binPath,
		extraArgs: strings.Fields(args),
		status: HostStatus{
			Mode:                  HostModeClaudePTY,
			State:                 HostStateStarting,
			Label:                 "claude starting",
			WorkerSessionIDSource: WorkerSessionIDSourceNone,
			Width:                 120,
			Height:                24,
			StateChangedAt:        time.Now().UTC(),
		},
	}
}

func (h *ClaudePTYHost) Start(ctx context.Context, snapshot Snapshot) error {
	h.UpdateSnapshot(snapshot)

	if strings.TrimSpace(snapshot.Repo.RepoRoot) == "" {
		h.setStatus(HostStateFailed, "repo root is required for claude PTY host", nil, false)
		return fmt.Errorf("repo root is required for claude PTY host")
	}

	prereq := DetectWorkerPrerequisite(WorkerPreferenceClaude)
	if !prereq.Ready {
		note := strings.TrimSpace(nonEmpty(prereq.Detail, prereq.Summary))
		h.setStatus(HostStateFailed, note, nil, false)
		return fmt.Errorf(note)
	}

	claudeBin := h.binPath
	if claudeBin == "" {
		path := strings.TrimSpace(prereq.BinaryPath)
		if path == "" {
			var err error
			path, err = exec.LookPath("claude")
			if err != nil {
				h.setStatus(HostStateFailed, "claude binary not found", nil, false)
				return fmt.Errorf("claude binary not found: %w", err)
			}
		}
		claudeBin = path
	}

	h.setStatus(HostStateStarting, "starting claude PTY host", nil, false)
	h.recordActivity("worker host starting: claude PTY session")

	args := make([]string, 0, len(h.extraArgs)+3)
	if sessionID := strings.TrimSpace(h.resumeSessionID); sessionID != "" {
		args = append(args, "--resume", "--session-id", sessionID)
	}
	args = append(args, h.extraArgs...)
	cmd := exec.CommandContext(ctx, claudeBin, args...)
	cmd.Dir = snapshot.Repo.RepoRoot
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	size := &pty.Winsize{
		Cols: uint16(max(1, h.status.Width)),
		Rows: uint16(max(1, h.status.Height)),
	}

	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		h.setStatus(HostStateFailed, fmt.Sprintf("failed to start claude PTY host: %v", err), nil, false)
		return fmt.Errorf("start claude PTY host: %w", err)
	}

	h.mu.Lock()
	h.lines = nil
	h.activity = nil
	h.transcriptPending = nil
	h.partial = ""
	h.parserState = terminalParserState{}
	h.cmd = cmd
	h.ptyFile = ptmx
	h.status.Mode = HostModeClaudePTY
	h.status.State = HostStateLive
	h.status.Label = "claude live"
	h.status.Note = ""
	h.status.WorkerSessionID = strings.TrimSpace(h.resumeSessionID)
	if h.status.WorkerSessionID != "" {
		h.status.WorkerSessionIDSource = WorkerSessionIDSourceAuthoritative
	} else {
		h.status.WorkerSessionIDSource = WorkerSessionIDSourceNone
	}
	h.status.InputLive = true
	h.status.ExitCode = nil
	h.status.LastOutputAt = time.Time{}
	h.status.StateChangedAt = time.Now().UTC()
	h.mu.Unlock()
	h.recordActivity("worker host started: claude PTY session is live")

	go h.readStream(ptmx)
	go h.wait()

	return nil
}

func (h *ClaudePTYHost) Stop() error {
	h.mu.Lock()
	ptmx := h.ptyFile
	cmd := h.cmd
	live := h.status.State == HostStateLive || h.status.State == HostStateStarting
	h.status.InputLive = false
	h.mu.Unlock()

	if ptmx != nil {
		_ = ptmx.Close()
	}
	if cmd != nil && live && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
			return err
		}
	}
	return nil
}

func (h *ClaudePTYHost) UpdateSnapshot(snapshot Snapshot) {
	h.mu.Lock()
	h.snapshot = snapshot
	h.mu.Unlock()
}

func (h *ClaudePTYHost) SetResumeSessionID(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resumeSessionID = strings.TrimSpace(sessionID)
}

func (h *ClaudePTYHost) Resize(width int, height int) bool {
	if width < 1 || height < 1 {
		return false
	}

	h.mu.Lock()
	h.status.Width = width
	h.status.Height = height
	ptmx := h.ptyFile
	state := h.status.State
	h.mu.Unlock()

	if ptmx == nil || state != HostStateLive {
		return false
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)}); err != nil {
		h.mu.Lock()
		if h.status.Note == "" {
			h.status.Note = fmt.Sprintf("resize update failed: %v", err)
		}
		h.mu.Unlock()
		return false
	}
	return true
}

func (h *ClaudePTYHost) CanAcceptInput() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status.State == HostStateLive && h.ptyFile != nil && h.status.InputLive
}

func (h *ClaudePTYHost) CanInterrupt() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status.State == HostStateLive && h.status.InputLive && h.ptyFile != nil
}

func (h *ClaudePTYHost) Interrupt() bool {
	h.mu.Lock()
	ptmx := h.ptyFile
	live := h.status.State == HostStateLive && h.status.InputLive
	if live {
		h.status.Note = "interrupt signal sent to claude"
		h.status.StateChangedAt = time.Now().UTC()
	}
	h.mu.Unlock()
	if !live || ptmx == nil {
		return false
	}
	if _, err := ptmx.Write([]byte{3}); err != nil {
		h.recordActivity("worker interrupt failed")
		return false
	}
	h.recordActivity("worker interrupt signal sent")
	return true
}

func (h *ClaudePTYHost) WriteInput(data []byte) bool {
	h.mu.Lock()
	ptmx := h.ptyFile
	live := h.status.State == HostStateLive && h.status.InputLive
	h.mu.Unlock()
	if !live || ptmx == nil || len(data) == 0 {
		return false
	}
	_, err := ptmx.Write(data)
	return err == nil
}

func (h *ClaudePTYHost) Status() HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	status := h.status
	if status.ExitCode != nil {
		code := *status.ExitCode
		status.ExitCode = &code
	}
	return status
}

func (h *ClaudePTYHost) Title() string {
	status := h.Status()
	switch status.State {
	case HostStateLive:
		return "worker pane | claude live | input to worker"
	case HostStateStarting:
		return "worker pane | claude starting"
	case HostStateExited:
		if status.ExitCode != nil {
			return fmt.Sprintf("worker pane | claude exited (%d) | read-only", *status.ExitCode)
		}
		return "worker pane | claude exited | read-only"
	case HostStateFailed:
		return "worker pane | claude failed | read-only"
	default:
		return "worker pane | claude PTY host"
	}
}

func (h *ClaudePTYHost) WorkerLabel() string {
	return h.Status().Label
}

func (h *ClaudePTYHost) Lines(height int, width int) []string {
	h.mu.Lock()
	lines := append([]string{}, h.lines...)
	partial := h.partial
	state := h.status.State
	note := h.status.Note
	lastOutputAt := h.status.LastOutputAt
	stateChangedAt := h.status.StateChangedAt
	h.mu.Unlock()

	status := HostStatus{
		Mode:           HostModeClaudePTY,
		State:          state,
		Label:          "claude live",
		Note:           note,
		LastOutputAt:   lastOutputAt,
		StateChangedAt: stateChangedAt,
	}

	_ = partial
	partialLine := sanitizeRenderedLine(partial)
	if len(lines) > 0 && partialLine != "" && !isLikelyCursorNoiseLine(partialLine) && !isLikelyFrameNoiseLine(partialLine) {
		lines = append(lines, partialLine)
	}
	if len(lines) == 0 {
		switch state {
		case HostStateStarting:
			return []string{"Launching Claude PTY session."}
		case HostStateExited, HostStateFailed:
			if note != "" {
				return wrapText(note, width)
			}
			return wrapText(describeInactiveBody(status), width)
		default:
			return []string{"Input goes directly to the worker."}
		}
	}

	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, wrapOutputLine(line, width)...)
	}
	return fitBottom(wrapped, height)
}

func (h *ClaudePTYHost) ActivityLines(limit int) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit >= len(h.activity) {
		return append([]string{}, h.activity...)
	}
	return append([]string{}, h.activity[len(h.activity)-limit:]...)
}

func (h *ClaudePTYHost) DrainTranscriptEvidence(limit int) []TranscriptEvidenceChunk {
	h.mu.Lock()
	defer h.mu.Unlock()
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

func (h *ClaudePTYHost) readStream(file *os.File) {
	reader := bufio.NewReader(file)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			h.appendOutput(buf[:n])
		}
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "file already closed") {
				h.recordActivity("worker host stream read error")
				h.mu.Lock()
				if h.status.State == HostStateLive {
					h.status.Note = "worker host stream ended unexpectedly"
				}
				h.mu.Unlock()
			}
			return
		}
	}
}

func (h *ClaudePTYHost) wait() {
	h.mu.Lock()
	cmd := h.cmd
	h.mu.Unlock()
	if cmd == nil {
		return
	}

	err := cmd.Wait()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	h.mu.Lock()
	if h.ptyFile != nil {
		_ = h.ptyFile.Close()
		h.ptyFile = nil
	}
	h.status.InputLive = false
	h.status.ExitCode = &exitCode
	if err != nil {
		h.status.State = HostStateFailed
		h.status.Label = "claude failed"
		h.status.Note = fmt.Sprintf("claude exited with code %d", exitCode)
		h.mu.Unlock()
		h.recordActivity(fmt.Sprintf("worker host exited with code %d", exitCode))
		return
	}
	h.status.State = HostStateExited
	h.status.Label = "claude exited"
	h.status.Note = fmt.Sprintf("claude exited cleanly with code %d", exitCode)
	h.mu.Unlock()
	h.recordActivity(fmt.Sprintf("worker host exited cleanly with code %d", exitCode))
}

func (h *ClaudePTYHost) appendOutput(chunk []byte) {
	h.mu.Lock()
	prevPartial := h.partial
	result := normalizeTerminalChunkWithState(h.partial, h.parserState, chunk)
	h.partial = result.partial
	h.parserState = result.state
	visibleOutput := false
	for _, line := range result.lines {
		if detected, source := detectWorkerSessionIDWithSource(line); detected != "" && strings.TrimSpace(h.status.WorkerSessionID) == "" {
			h.status.WorkerSessionID = detected
			h.status.WorkerSessionIDSource = source
		}
		if h.appendLineLocked(line) {
			visibleOutput = true
		}
	}
	if detected, source := detectWorkerSessionIDWithSource(result.partial); detected != "" && strings.TrimSpace(h.status.WorkerSessionID) == "" {
		h.status.WorkerSessionID = detected
		h.status.WorkerSessionIDSource = source
	}
	if !visibleOutput {
		currentPartial := sanitizeRenderedLine(result.partial)
		if currentPartial != "" && !isLikelyCursorNoiseLine(currentPartial) && !isLikelyFrameNoiseLine(currentPartial) && currentPartial != sanitizeRenderedLine(prevPartial) {
			visibleOutput = true
		}
	}
	if visibleOutput {
		h.status.LastOutputAt = time.Now().UTC()
	}
	h.mu.Unlock()
}

func (h *ClaudePTYHost) appendLineLocked(line string) bool {
	line = sanitizeRenderedLine(line)
	if line == "" {
		return false
	}
	if isLikelyCursorNoiseLine(line) {
		return false
	}
	if isLikelyFrameNoiseLine(line) {
		return false
	}
	h.lines = append(h.lines, line)
	if strings.TrimSpace(line) != "" {
		h.transcriptPending = append(h.transcriptPending, TranscriptEvidenceChunk{
			Source:    "worker_output",
			Content:   line,
			CreatedAt: time.Now().UTC(),
		})
	}
	if len(h.lines) > hostMaxLines {
		h.lines = h.lines[len(h.lines)-hostMaxLines:]
	}
	if len(h.transcriptPending) > hostMaxLines {
		h.transcriptPending = h.transcriptPending[len(h.transcriptPending)-hostMaxLines:]
	}
	return true
}

func (h *ClaudePTYHost) recordActivity(message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	stamped := fmt.Sprintf("%s  %s", time.Now().UTC().Format("15:04:05"), message)
	h.activity = append(h.activity, stamped)
	if len(h.activity) > hostMaxActivity {
		h.activity = h.activity[len(h.activity)-hostMaxActivity:]
	}
}

func (h *ClaudePTYHost) setStatus(state HostState, note string, exitCode *int, inputLive bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.Mode = HostModeClaudePTY
	h.status.State = state
	h.status.Note = strings.TrimSpace(note)
	h.status.InputLive = inputLive
	h.status.ExitCode = exitCode
	h.status.StateChangedAt = time.Now().UTC()
	switch state {
	case HostStateStarting:
		h.status.Label = "claude starting"
		h.status.LastOutputAt = time.Time{}
	case HostStateLive:
		h.status.Label = "claude live"
	case HostStateExited:
		h.status.Label = "claude exited"
	case HostStateFailed:
		h.status.Label = "claude failed"
	}
}
