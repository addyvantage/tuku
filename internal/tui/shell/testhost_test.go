package shell

import "context"

type stubHost struct {
	title        string
	worker       string
	lines        []string
	activity     []string
	historyLines []string
	canInput     bool
	canInterrupt bool
	turnActive   bool
	interrupts   int
	status       HostStatus
	writes       [][]byte
	resizes      [][2]int
	historyCalls int
	snapshotSeen Snapshot
}

func (h *stubHost) Start(_ context.Context, snapshot Snapshot) error {
	h.snapshotSeen = snapshot
	return nil
}

func (h *stubHost) Stop() error {
	return nil
}

func (h *stubHost) UpdateSnapshot(snapshot Snapshot) {
	h.snapshotSeen = snapshot
}

func (h *stubHost) Resize(width int, height int) bool {
	h.resizes = append(h.resizes, [2]int{width, height})
	if h.status.Width == 0 {
		h.status.Width = width
	}
	if h.status.Height == 0 {
		h.status.Height = height
	}
	return true
}

func (h *stubHost) CanAcceptInput() bool {
	return h.canInput
}

func (h *stubHost) WriteInput(data []byte) bool {
	if !h.canInput {
		return false
	}
	cp := append([]byte{}, data...)
	h.writes = append(h.writes, cp)
	h.status.RenderVersion++
	return true
}

func (h *stubHost) CanInterrupt() bool {
	return h.canInterrupt
}

func (h *stubHost) Interrupt() bool {
	if !h.canInterrupt {
		return false
	}
	h.interrupts++
	return true
}

func (h *stubHost) Title() string {
	if h.title != "" {
		return h.title
	}
	return "stub host"
}

func (h *stubHost) Status() HostStatus {
	if h.status.Label == "" {
		h.status.Label = h.worker
	}
	h.status.InputLive = h.canInput
	return h.status
}

func (h *stubHost) WorkerLabel() string {
	return h.worker
}

func (h *stubHost) Lines(_ int, _ int) []string {
	if len(h.historyLines) > 0 {
		return append([]string{}, h.historyLines...)
	}
	return append([]string{}, h.lines...)
}

func (h *stubHost) HistoryLines(_ int) []string {
	h.historyCalls++
	if len(h.historyLines) > 0 {
		return append([]string{}, h.historyLines...)
	}
	return append([]string{}, h.lines...)
}

func (h *stubHost) ActivityLines(limit int) []string {
	if limit <= 0 || limit >= len(h.activity) {
		return append([]string{}, h.activity...)
	}
	return append([]string{}, h.activity[len(h.activity)-limit:]...)
}

type authoritativeStubHost struct {
	*stubHost
}

func (h *authoritativeStubHost) WorkerTurnActive() bool {
	return h.turnActive
}
