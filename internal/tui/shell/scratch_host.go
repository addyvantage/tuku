package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type LocalScratchHost struct {
	path        string
	cwd         string
	clock       func() time.Time
	snapshot    Snapshot
	savedNotes  []ConversationItem
	draft       []byte
	status      HostStatus
	activity    []string
	lastSaveErr string
}

type persistedScratchSession struct {
	Version   int                `json:"version"`
	Kind      string             `json:"kind"`
	CWD       string             `json:"cwd"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
	Notes     []ConversationItem `json:"notes"`
}

func NewLocalScratchHost(path string, cwd string) *LocalScratchHost {
	return &LocalScratchHost{
		path:  path,
		cwd:   cwd,
		clock: func() time.Time { return time.Now().UTC() },
		status: HostStatus{
			Mode:      HostModeTranscript,
			State:     HostStateLive,
			Label:     "scratch intake",
			InputLive: true,
			Note:      "local scratch notes persist only on this machine",
		},
	}
}

func (h *LocalScratchHost) Start(_ context.Context, snapshot Snapshot) error {
	h.applySnapshot(snapshot)
	return nil
}

func (h *LocalScratchHost) Stop() error {
	return nil
}

func (h *LocalScratchHost) UpdateSnapshot(snapshot Snapshot) {
	h.applySnapshot(snapshot)
}

func (h *LocalScratchHost) Resize(width int, height int) bool {
	h.status.Width = width
	h.status.Height = height
	return false
}

func (h *LocalScratchHost) CanAcceptInput() bool {
	return true
}

func (h *LocalScratchHost) WriteInput(data []byte) bool {
	for _, b := range data {
		switch b {
		case '\r', '\n':
			if err := h.commitDraft(); err != nil {
				h.lastSaveErr = err.Error()
				h.status.Note = "local scratch save failed: " + truncateWithEllipsis(err.Error(), 64)
				h.recordActivity("local scratch save failed")
			}
		case 0x7f, 0x08:
			if len(h.draft) > 0 {
				h.draft = h.draft[:len(h.draft)-1]
			}
		case '\t':
			h.draft = append(h.draft, ' ', ' ')
		default:
			if b >= 32 {
				h.draft = append(h.draft, b)
			}
		}
	}
	return true
}

func (h *LocalScratchHost) Status() HostStatus {
	return h.status
}

func (h *LocalScratchHost) Title() string {
	return "worker pane | local scratch intake"
}

func (h *LocalScratchHost) WorkerLabel() string {
	return "scratch intake"
}

func (h *LocalScratchHost) Lines(height int, width int) []string {
	if height < 1 {
		return nil
	}
	lines := make([]string, 0, len(h.snapshot.RecentConversation)*2+4)
	for _, msg := range h.snapshot.RecentConversation {
		prefix := transcriptPrefix(msg.Role)
		body := strings.TrimSpace(msg.Body)
		if body == "" {
			continue
		}
		lines = append(lines, wrapText(prefix+body, width)...)
		lines = append(lines, "")
	}
	draft := strings.TrimSpace(string(h.draft))
	if draft == "" {
		lines = append(lines, wrapText("you> type here and press Enter to save a local scratch note", width)...)
	} else {
		lines = append(lines, wrapText("you> "+string(h.draft), width)...)
	}
	return fitBottom(lines, height)
}

func (h *LocalScratchHost) ActivityLines(limit int) []string {
	if limit <= 0 || limit >= len(h.activity) {
		return append([]string{}, h.activity...)
	}
	return append([]string{}, h.activity[len(h.activity)-limit:]...)
}

func (h *LocalScratchHost) applySnapshot(base Snapshot) {
	notes, err := loadScratchNotes(h.path)
	h.savedNotes = notes
	h.snapshot = mergeScratchSnapshot(base, notes)
	if err != nil {
		h.lastSaveErr = err.Error()
		h.status.Note = "local scratch history could not be read; starting with a clean intake view"
		h.recordActivity("local scratch history read failed")
		return
	}
	if len(notes) > 0 {
		h.status.Note = fmt.Sprintf("loaded %d local scratch note(s) from this machine", len(notes))
		return
	}
	h.status.Note = "local scratch notes persist only on this machine"
}

func (h *LocalScratchHost) commitDraft() error {
	body := strings.TrimSpace(string(h.draft))
	if body == "" {
		return nil
	}
	note := ConversationItem{
		Role:      "user",
		Body:      body,
		CreatedAt: h.clock(),
	}
	notes := append(append([]ConversationItem{}, h.savedNotes...), note)
	if err := saveScratchNotes(h.path, h.cwd, notes, h.clock); err != nil {
		return err
	}
	h.savedNotes = notes
	h.snapshot = mergeScratchSnapshot(h.snapshotBase(), h.savedNotes)
	h.draft = h.draft[:0]
	h.lastSaveErr = ""
	h.status.Note = fmt.Sprintf("saved %d local scratch note(s) on this machine", len(h.savedNotes))
	h.recordActivity("saved local scratch note")
	return nil
}

func (h *LocalScratchHost) snapshotBase() Snapshot {
	base := h.snapshot
	if len(h.savedNotes) == 0 {
		return base
	}
	if len(base.RecentConversation) >= len(h.savedNotes) {
		base.RecentConversation = append([]ConversationItem{}, base.RecentConversation[:len(base.RecentConversation)-len(h.savedNotes)]...)
	}
	return base
}

func (h *LocalScratchHost) recordActivity(message string) {
	stamped := fmt.Sprintf("%s  %s", h.clock().Format("15:04:05"), message)
	h.activity = append(h.activity, stamped)
	if len(h.activity) > hostMaxActivity {
		h.activity = h.activity[len(h.activity)-hostMaxActivity:]
	}
}

func mergeScratchSnapshot(base Snapshot, notes []ConversationItem) Snapshot {
	out := base
	if len(notes) == 0 {
		out.RecentConversation = append([]ConversationItem{}, base.RecentConversation...)
		return out
	}
	out.RecentConversation = append(append([]ConversationItem{}, base.RecentConversation...), notes...)
	return out
}

func loadScratchNotes(path string) ([]ConversationItem, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var session persistedScratchSession
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, err
	}
	return normalizeScratchNotes(session.Notes), nil
}

func LoadLocalScratchNotes(path string) ([]ConversationItem, error) {
	return loadScratchNotes(path)
}

func AppendLocalScratchNote(path string, cwd string, body string, createdAt time.Time) ([]ConversationItem, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return loadScratchNotes(path)
	}
	notes, err := loadScratchNotes(path)
	if err != nil {
		return nil, err
	}
	notes = append(notes, ConversationItem{
		Role:      "user",
		Body:      trimmed,
		CreatedAt: createdAt,
	})
	if err := saveScratchNotes(path, cwd, notes, func() time.Time { return createdAt }); err != nil {
		return nil, err
	}
	return append([]ConversationItem{}, notes...), nil
}

func saveScratchNotes(path string, cwd string, notes []ConversationItem, clock func() time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	createdAt := clock()
	if existing, err := os.ReadFile(path); err == nil {
		var prior persistedScratchSession
		if json.Unmarshal(existing, &prior) == nil && !prior.CreatedAt.IsZero() {
			createdAt = prior.CreatedAt
		}
	}
	session := persistedScratchSession{
		Version:   1,
		Kind:      "local_scratch_intake",
		CWD:       filepath.Clean(strings.TrimSpace(cwd)),
		CreatedAt: createdAt,
		UpdatedAt: clock(),
		Notes:     normalizeScratchNotes(notes),
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func normalizeScratchNotes(notes []ConversationItem) []ConversationItem {
	out := make([]ConversationItem, 0, len(notes))
	for _, note := range notes {
		body := strings.TrimSpace(note.Body)
		if !strings.EqualFold(strings.TrimSpace(note.Role), "user") || body == "" {
			continue
		}
		out = append(out, ConversationItem{
			Role:      "user",
			Body:      body,
			CreatedAt: note.CreatedAt,
		})
	}
	return out
}
