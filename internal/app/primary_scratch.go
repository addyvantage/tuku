package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tukushell "tuku/internal/tui/shell"
)

type primaryScratchIntake struct {
	cwd        string
	path       string
	in         io.Reader
	out        io.Writer
	now        func() time.Time
	loadNotes  func(path string) ([]tukushell.ConversationItem, error)
	appendNote func(path string, cwd string, body string, createdAt time.Time) ([]tukushell.ConversationItem, error)
}

func newPrimaryScratchIntake(cwd string, path string) *primaryScratchIntake {
	return &primaryScratchIntake{
		cwd:        cwd,
		path:       path,
		in:         os.Stdin,
		out:        os.Stdout,
		now:        func() time.Time { return time.Now().UTC() },
		loadNotes:  tukushell.LoadLocalScratchNotes,
		appendNote: tukushell.AppendLocalScratchNote,
	}
}

func (s *primaryScratchIntake) Run(ctx context.Context) error {
	if s.in == nil {
		s.in = os.Stdin
	}
	if s.out == nil {
		s.out = os.Stdout
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.loadNotes == nil {
		s.loadNotes = tukushell.LoadLocalScratchNotes
	}
	if s.appendNote == nil {
		s.appendNote = tukushell.AppendLocalScratchNote
	}

	notes, err := s.loadNotes(s.path)
	if err != nil {
		return fmt.Errorf("load local scratch notes: %w", err)
	}

	printPrimaryScratchIntro(s.out, s.cwd, notes)
	reader := bufio.NewReader(s.in)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := fmt.Fprint(s.out, "scratch> "); err != nil {
			return err
		}
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		line = strings.TrimRight(line, "\r\n")
		nextNotes, shouldExit, err := s.handleLine(line, notes)
		if err != nil {
			return err
		}
		notes = nextNotes
		if shouldExit {
			return nil
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

func (s *primaryScratchIntake) handleLine(line string, notes []tukushell.ConversationItem) ([]tukushell.ConversationItem, bool, error) {
	trimmed := strings.TrimSpace(line)
	switch trimmed {
	case "":
		if _, err := fmt.Fprintln(s.out, "Enter a note to save it locally, or use /help."); err != nil {
			return notes, false, err
		}
		return notes, false, nil
	case "/q", "/quit", "/exit":
		if _, err := fmt.Fprintln(s.out, "Leaving local scratch intake."); err != nil {
			return notes, true, err
		}
		return notes, true, nil
	case "/h", "/help":
		if err := printPrimaryScratchHelp(s.out); err != nil {
			return notes, false, err
		}
		return notes, false, nil
	case "/l", "/list":
		if err := printPrimaryScratchNotes(s.out, notes); err != nil {
			return notes, false, err
		}
		return notes, false, nil
	default:
		if strings.HasPrefix(trimmed, "/") {
			if _, err := fmt.Fprintln(s.out, "Unknown command. Use /help."); err != nil {
				return notes, false, err
			}
			return notes, false, nil
		}
	}

	updated, err := s.appendNote(s.path, s.cwd, trimmed, s.now())
	if err != nil {
		return notes, false, fmt.Errorf("save local scratch note: %w", err)
	}
	if _, err := fmt.Fprintf(s.out, "Saved local scratch note locally. %d note(s) stored on this machine.\n", len(updated)); err != nil {
		return notes, false, err
	}
	return updated, false, nil
}

func printPrimaryScratchIntro(out io.Writer, cwd string, notes []tukushell.ConversationItem) {
	_, _ = fmt.Fprintln(out, "Tuku Scratch Intake")
	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintln(out, "No git repository was detected in the current directory.")
	_, _ = fmt.Fprintln(out, "This mode is local-only. Tuku is not starting the daemon, not creating a task, and not claiming repo-backed continuity.")
	_, _ = fmt.Fprintln(out, "Type one line and press Enter to save a local scratch note on this machine.")
	_, _ = fmt.Fprintln(out, "Commands: /help  /list  /quit")
	_, _ = fmt.Fprintf(out, "Current directory: %s\n", cwd)
	if len(notes) == 0 {
		_, _ = fmt.Fprintln(out, "Saved notes: none yet.")
		_, _ = fmt.Fprintln(out, "")
		return
	}
	_, _ = fmt.Fprintf(out, "Saved notes: %d local note(s) already stored here.\n", len(notes))
	_, _ = fmt.Fprintln(out, "")
	_ = printPrimaryScratchNotes(out, notes)
}

func printPrimaryScratchHelp(out io.Writer) error {
	_, err := fmt.Fprintln(out, "Commands:")
	if err != nil {
		return err
	}
	lines := []string{
		"  /help  show this help",
		"  /list  show saved local scratch notes",
		"  /quit  leave scratch intake",
		"Any other line is saved as a local scratch note.",
		"These notes stay local-only until a later repo-backed task explicitly adopts them.",
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

func printPrimaryScratchNotes(out io.Writer, notes []tukushell.ConversationItem) error {
	if len(notes) == 0 {
		_, err := fmt.Fprintln(out, "No local scratch notes saved yet.")
		return err
	}
	if _, err := fmt.Fprintln(out, "Local scratch notes:"); err != nil {
		return err
	}
	for _, note := range notes {
		body := strings.TrimSpace(note.Body)
		if body == "" {
			continue
		}
		if _, err := fmt.Fprintf(out, "  - %s\n", body); err != nil {
			return err
		}
	}
	return nil
}
