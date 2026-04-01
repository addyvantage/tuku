package shell

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
)

const (
	hostMaxLines                   = 500
	hostMaxActivity                = 40
	codexSafeReasoningEffortConfig = `model_reasoning_effort="high"`
)

type CodexPTYHost struct {
	binPath         string
	extraArgs       []string
	resumeSessionID string
	mode            string

	mu                sync.Mutex
	snapshot          Snapshot
	lines             []string
	activity          []string
	transcriptPending []TranscriptEvidenceChunk
	partial           string
	parserState       terminalParserState
	status            HostStatus
	inputBuffer       string
	execRunning       bool
	execThreadID      string
	execCancel        context.CancelFunc

	ptyFile *os.File
	cmd     *exec.Cmd
}

func NewDefaultCodexPTYHost() *CodexPTYHost {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv("TUKU_SHELL_CODEX_MODE")))
	if mode == "" {
		mode = "exec"
	}
	return &CodexPTYHost{
		binPath:   strings.TrimSpace(os.Getenv("TUKU_SHELL_CODEX_BIN")),
		extraArgs: strings.Fields(os.Getenv("TUKU_SHELL_CODEX_ARGS")),
		mode:      mode,
		status: HostStatus{
			Mode:                  HostModeCodexPTY,
			State:                 HostStateStarting,
			Label:                 "codex starting",
			WorkerSessionIDSource: WorkerSessionIDSourceNone,
			Width:                 120,
			Height:                24,
			StateChangedAt:        time.Now().UTC(),
		},
	}
}

func (h *CodexPTYHost) Start(ctx context.Context, snapshot Snapshot) error {
	h.UpdateSnapshot(snapshot)

	if strings.TrimSpace(snapshot.Repo.RepoRoot) == "" {
		h.setStatus(HostStateFailed, "repo root is required for codex PTY host", nil, false)
		return fmt.Errorf("repo root is required for codex PTY host")
	}

	prereq := DetectWorkerPrerequisite(WorkerPreferenceCodex)
	if !prereq.Ready {
		note := strings.TrimSpace(nonEmpty(prereq.Detail, prereq.Summary))
		h.setStatus(HostStateFailed, note, nil, false)
		return fmt.Errorf(note)
	}

	codexBin := h.binPath
	if codexBin == "" {
		path := strings.TrimSpace(prereq.BinaryPath)
		if path == "" {
			var err error
			path, err = exec.LookPath("codex")
			if err != nil {
				h.setStatus(HostStateFailed, "codex binary not found", nil, false)
				return fmt.Errorf("codex binary not found: %w", err)
			}
		}
		codexBin = path
	}
	if h.useExecMode() {
		return h.startExecMode(snapshot)
	}

	h.setStatus(HostStateStarting, "starting codex PTY host", nil, false)
	h.recordActivity("worker host starting: codex PTY session")

	args := []string{"--no-alt-screen", "-C", snapshot.Repo.RepoRoot}
	if sessionID := strings.TrimSpace(h.resumeSessionID); sessionID != "" {
		args = append(args, "resume", sessionID)
	}
	args = append(args, h.extraArgs...)
	args = ensureCodexReasoningEffortArgs(args)

	cmd := exec.CommandContext(ctx, codexBin, args...)
	cmd.Dir = snapshot.Repo.RepoRoot
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	size := &pty.Winsize{
		Cols: uint16(max(1, h.status.Width)),
		Rows: uint16(max(1, h.status.Height)),
	}

	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		h.setStatus(HostStateFailed, fmt.Sprintf("failed to start codex PTY host: %v", err), nil, false)
		return fmt.Errorf("start codex PTY host: %w", err)
	}

	h.mu.Lock()
	h.lines = nil
	h.activity = nil
	h.transcriptPending = nil
	h.partial = ""
	h.parserState = terminalParserState{}
	h.cmd = cmd
	h.ptyFile = ptmx
	h.status.Mode = HostModeCodexPTY
	h.status.State = HostStateLive
	h.status.Label = "codex live"
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
	h.recordActivity("worker host started: codex PTY session is live")

	go h.readStream(ptmx)
	go h.wait()

	return nil
}

func (h *CodexPTYHost) Stop() error {
	if h.useExecMode() {
		h.mu.Lock()
		cancel := h.execCancel
		cmd := h.cmd
		h.execCancel = nil
		h.execRunning = false
		h.status.InputLive = false
		h.status.State = HostStateExited
		h.status.Label = "codex exited"
		h.status.Note = "codex host stopped"
		h.status.StateChangedAt = time.Now().UTC()
		h.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil
	}

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

func (h *CodexPTYHost) UpdateSnapshot(snapshot Snapshot) {
	h.mu.Lock()
	h.snapshot = snapshot
	h.mu.Unlock()
}

func (h *CodexPTYHost) SetResumeSessionID(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resumeSessionID = strings.TrimSpace(sessionID)
}

func (h *CodexPTYHost) Resize(width int, height int) bool {
	if width < 1 || height < 1 {
		return false
	}

	if h.useExecMode() {
		h.mu.Lock()
		h.status.Width = width
		h.status.Height = height
		h.mu.Unlock()
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

func (h *CodexPTYHost) CanAcceptInput() bool {
	if h.useExecMode() {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.status.State == HostStateLive && h.status.InputLive && !h.execRunning
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status.State == HostStateLive && h.ptyFile != nil && h.status.InputLive
}

func (h *CodexPTYHost) CanInterrupt() bool {
	if h.useExecMode() {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.status.State == HostStateLive && h.execRunning && h.execCancel != nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status.State == HostStateLive && h.status.InputLive && h.ptyFile != nil
}

func (h *CodexPTYHost) Interrupt() bool {
	if h.useExecMode() {
		h.mu.Lock()
		cancel := h.execCancel
		running := h.status.State == HostStateLive && h.execRunning
		if running {
			h.status.Note = "interrupt requested for codex prompt"
			h.status.StateChangedAt = time.Now().UTC()
		}
		h.mu.Unlock()
		if !running || cancel == nil {
			return false
		}
		cancel()
		h.recordActivity("worker interrupt requested")
		return true
	}

	h.mu.Lock()
	ptmx := h.ptyFile
	live := h.status.State == HostStateLive && h.status.InputLive
	if live {
		h.status.Note = "interrupt signal sent to codex"
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

func (h *CodexPTYHost) WorkerTurnActive() bool {
	if !h.useExecMode() {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status.State == HostStateLive && h.execRunning
}

func (h *CodexPTYHost) WriteInput(data []byte) bool {
	if h.useExecMode() {
		return h.writeInputExec(data)
	}

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

func (h *CodexPTYHost) Status() HostStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	status := h.status
	if status.ExitCode != nil {
		code := *status.ExitCode
		status.ExitCode = &code
	}
	return status
}

func (h *CodexPTYHost) Title() string {
	status := h.Status()
	switch status.State {
	case HostStateLive:
		return "worker pane | codex live | input to worker"
	case HostStateStarting:
		return "worker pane | codex starting"
	case HostStateExited:
		if status.ExitCode != nil {
			return fmt.Sprintf("worker pane | codex exited (%d) | read-only", *status.ExitCode)
		}
		return "worker pane | codex exited | read-only"
	case HostStateFailed:
		return "worker pane | codex failed | read-only"
	default:
		return "worker pane | codex PTY host"
	}
}

func (h *CodexPTYHost) WorkerLabel() string {
	return h.Status().Label
}

func (h *CodexPTYHost) Lines(height int, width int) []string {
	h.mu.Lock()
	lines := append([]string{}, h.lines...)
	partial := h.partial
	state := h.status.State
	note := h.status.Note
	lastOutputAt := h.status.LastOutputAt
	stateChangedAt := h.status.StateChangedAt
	h.mu.Unlock()

	status := HostStatus{
		Mode:           HostModeCodexPTY,
		State:          state,
		Label:          "codex live",
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
			return []string{"Launching Codex PTY session."}
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

func (h *CodexPTYHost) ActivityLines(limit int) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit >= len(h.activity) {
		return append([]string{}, h.activity...)
	}
	return append([]string{}, h.activity[len(h.activity)-limit:]...)
}

func (h *CodexPTYHost) DrainTranscriptEvidence(limit int) []TranscriptEvidenceChunk {
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

func (h *CodexPTYHost) readStream(file *os.File) {
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

func (h *CodexPTYHost) wait() {
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
		h.status.Label = "codex failed"
		h.status.Note = fmt.Sprintf("codex exited with code %d", exitCode)
		h.mu.Unlock()
		h.recordActivity(fmt.Sprintf("worker host exited with code %d", exitCode))
		return
	}
	h.status.State = HostStateExited
	h.status.Label = "codex exited"
	h.status.Note = fmt.Sprintf("codex exited cleanly with code %d", exitCode)
	h.mu.Unlock()
	h.recordActivity(fmt.Sprintf("worker host exited cleanly with code %d", exitCode))
}

func (h *CodexPTYHost) useExecMode() bool {
	mode := strings.TrimSpace(strings.ToLower(h.mode))
	return mode != "pty"
}

func (h *CodexPTYHost) startExecMode(snapshot Snapshot) error {
	h.mu.Lock()
	h.lines = nil
	h.activity = nil
	h.transcriptPending = nil
	h.partial = ""
	h.parserState = terminalParserState{}
	h.inputBuffer = ""
	h.execRunning = false
	h.execCancel = nil
	h.cmd = nil
	h.ptyFile = nil
	h.status.Mode = HostModeCodexPTY
	h.status.State = HostStateLive
	h.status.Label = "codex live"
	h.status.Note = "codex exec mode"
	h.status.WorkerSessionID = strings.TrimSpace(h.resumeSessionID)
	if h.status.WorkerSessionID != "" {
		h.status.WorkerSessionIDSource = WorkerSessionIDSourceAuthoritative
		h.execThreadID = h.status.WorkerSessionID
	} else {
		h.status.WorkerSessionIDSource = WorkerSessionIDSourceNone
		h.execThreadID = ""
	}
	h.status.InputLive = true
	h.status.ExitCode = nil
	h.status.LastOutputAt = time.Time{}
	h.status.StateChangedAt = time.Now().UTC()
	h.mu.Unlock()

	h.recordActivity("worker host started: codex exec session is live")
	return nil
}

func (h *CodexPTYHost) writeInputExec(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	var prompt string

	h.mu.Lock()
	live := h.status.State == HostStateLive && h.status.InputLive && !h.execRunning
	if !live {
		h.mu.Unlock()
		return false
	}
	for _, b := range data {
		switch b {
		case '\r', '\n':
			prompt = strings.TrimSpace(h.inputBuffer)
			h.inputBuffer = ""
			if prompt != "" {
				h.execRunning = true
				h.status.InputLive = true
				h.status.Note = "running codex prompt"
				h.status.StateChangedAt = time.Now().UTC()
			}
		case 0x7f, 0x08:
			if h.inputBuffer != "" {
				_, size := utf8.DecodeLastRuneInString(h.inputBuffer)
				if size <= 0 {
					h.inputBuffer = ""
				} else {
					h.inputBuffer = h.inputBuffer[:len(h.inputBuffer)-size]
				}
			}
		default:
			if b >= 32 && b < 127 {
				h.inputBuffer += string(b)
			}
		}
		if prompt != "" {
			break
		}
	}
	h.mu.Unlock()

	if prompt == "" {
		return true
	}

	h.recordActivity("codex prompt submitted")
	go h.runExecPrompt(prompt)
	return true
}

type codexExecEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage"`
}

func (h *CodexPTYHost) runExecPrompt(prompt string) {
	h.mu.Lock()
	snapshot := h.snapshot
	h.mu.Unlock()
	repoRoot := strings.TrimSpace(snapshot.Repo.RepoRoot)
	if repoRoot == "" {
		h.finishExecPrompt(fmt.Errorf("repo root is required"), false)
		return
	}

	codexBin := h.binPath
	if codexBin == "" {
		path, err := exec.LookPath("codex")
		if err != nil {
			h.finishExecPrompt(fmt.Errorf("codex binary not found: %w", err), false)
			return
		}
		codexBin = path
	}

	args := h.execArgsForPrompt(prompt)
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, codexBin, args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		h.finishExecPrompt(fmt.Errorf("codex stdout pipe: %w", err), false)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		h.finishExecPrompt(fmt.Errorf("codex stderr pipe: %w", err), false)
		return
	}
	if err := cmd.Start(); err != nil {
		cancel()
		h.finishExecPrompt(fmt.Errorf("codex exec start: %w", err), false)
		return
	}

	h.mu.Lock()
	h.execCancel = cancel
	h.cmd = cmd
	h.mu.Unlock()

	var (
		wg           sync.WaitGroup
		outputSeenMu sync.Mutex
		outputSeen   bool
	)
	markOutput := func() {
		outputSeenMu.Lock()
		outputSeen = true
		outputSeenMu.Unlock()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if h.handleExecJSONLine(line) {
				markOutput()
				continue
			}
			h.recordActivity("codex stdout: " + truncateWithEllipsis(line, 140))
		}
		if err := scanner.Err(); err != nil {
			h.recordActivity("codex stdout read error")
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			h.recordActivity("codex stderr: " + truncateWithEllipsis(line, 140))
		}
		if err := scanner.Err(); err != nil {
			h.recordActivity("codex stderr read error")
		}
	}()

	err = cmd.Wait()
	wg.Wait()

	outputSeenMu.Lock()
	sawOutput := outputSeen
	outputSeenMu.Unlock()
	if !sawOutput {
		h.appendExecOutputLine("The worker completed without a visible assistant message.")
	}
	h.finishExecPrompt(err, sawOutput)
}

func (h *CodexPTYHost) execArgsForPrompt(prompt string) []string {
	args := make([]string, 0, 8)
	h.mu.Lock()
	threadID := strings.TrimSpace(h.execThreadID)
	resumeSessionID := strings.TrimSpace(h.resumeSessionID)
	h.mu.Unlock()
	if threadID == "" {
		threadID = resumeSessionID
	}
	if threadID != "" {
		args = append(args, "exec", "resume", threadID, "--json", prompt)
		return ensureCodexReasoningEffortArgs(args)
	}
	args = append(args, "exec", "--json", prompt)
	return ensureCodexReasoningEffortArgs(args)
}

func ensureCodexReasoningEffortArgs(args []string) []string {
	if hasCodexReasoningEffortOverride(args) {
		return append([]string{}, args...)
	}
	return append([]string{"-c", codexSafeReasoningEffortConfig}, args...)
}

func hasCodexReasoningEffortOverride(args []string) bool {
	for idx, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		if strings.Contains(lower, "model_reasoning_effort") {
			return true
		}
		if (lower == "-c" || lower == "--config") && idx+1 < len(args) {
			if strings.Contains(strings.ToLower(strings.TrimSpace(args[idx+1])), "model_reasoning_effort") {
				return true
			}
		}
	}
	return false
}

func (h *CodexPTYHost) handleExecJSONLine(line string) bool {
	var event codexExecEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return false
	}

	switch strings.TrimSpace(event.Type) {
	case "thread.started":
		if threadID := strings.TrimSpace(event.ThreadID); threadID != "" {
			h.mu.Lock()
			h.execThreadID = threadID
			h.status.WorkerSessionID = threadID
			h.status.WorkerSessionIDSource = WorkerSessionIDSourceAuthoritative
			h.mu.Unlock()
		}
		return false
	case "item.completed":
		if strings.EqualFold(strings.TrimSpace(event.Item.Type), "agent_message") {
			text := strings.TrimSpace(event.Item.Text)
			if text == "" {
				return false
			}
			for _, part := range strings.Split(text, "\n") {
				if strings.TrimSpace(part) == "" {
					continue
				}
				h.appendExecOutputLine(part)
			}
			return true
		}
		return false
	case "turn.completed":
		h.recordActivity(fmt.Sprintf("codex turn completed (%d out tokens)", event.Usage.OutputTokens))
		return false
	default:
		return false
	}
}

func (h *CodexPTYHost) appendExecOutputLine(line string) {
	h.mu.Lock()
	visible := h.appendLineLocked(line)
	if visible {
		h.status.LastOutputAt = time.Now().UTC()
	}
	h.mu.Unlock()
}

func (h *CodexPTYHost) finishExecPrompt(execErr error, outputSeen bool) {
	h.mu.Lock()
	h.execRunning = false
	h.execCancel = nil
	h.cmd = nil
	h.status.State = HostStateLive
	h.status.Label = "codex live"
	h.status.InputLive = true
	h.status.ExitCode = nil
	if execErr != nil {
		if execErr == context.Canceled {
			h.status.Note = "codex prompt interrupted"
		} else {
			h.status.Note = "last codex prompt failed"
		}
	} else if outputSeen {
		h.status.Note = "codex response received"
	} else {
		h.status.Note = "codex prompt completed"
	}
	h.status.StateChangedAt = time.Now().UTC()
	h.mu.Unlock()

	if execErr != nil {
		if execErr == context.Canceled {
			h.recordActivity("codex prompt interrupted")
		} else {
			h.recordActivity("codex prompt failed")
		}
	} else {
		h.recordActivity("codex prompt completed")
	}
}

func (h *CodexPTYHost) appendOutput(chunk []byte) {
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

func (h *CodexPTYHost) appendLineLocked(line string) bool {
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

func (h *CodexPTYHost) recordActivity(message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	stamped := fmt.Sprintf("%s  %s", time.Now().UTC().Format("15:04:05"), message)
	h.activity = append(h.activity, stamped)
	if len(h.activity) > hostMaxActivity {
		h.activity = h.activity[len(h.activity)-hostMaxActivity:]
	}
}

func (h *CodexPTYHost) setStatus(state HostState, note string, exitCode *int, inputLive bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.Mode = HostModeCodexPTY
	h.status.State = state
	h.status.Note = strings.TrimSpace(note)
	h.status.InputLive = inputLive
	h.status.ExitCode = exitCode
	h.status.StateChangedAt = time.Now().UTC()
	switch state {
	case HostStateStarting:
		h.status.Label = "codex starting"
		h.status.LastOutputAt = time.Time{}
	case HostStateLive:
		h.status.Label = "codex live"
	case HostStateExited:
		h.status.Label = "codex exited"
	case HostStateFailed:
		h.status.Label = "codex failed"
	}
}
