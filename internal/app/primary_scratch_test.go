package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tukushell "tuku/internal/tui/shell"
)

func TestPrimaryScratchIntakeIsReadableAndStoresNotes(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	path := t.TempDir() + "/scratch.json"
	input := bytes.NewBufferString("plan repo milestones\n/quit\n")
	var output bytes.Buffer

	app := newPrimaryScratchIntake("/tmp/no-repo", path)
	app.in = input
	app.out = &output
	app.now = func() time.Time { return now }

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run primary scratch intake: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Tuku Scratch Intake") {
		t.Fatalf("expected readable scratch title, got %q", rendered)
	}
	if !strings.Contains(rendered, "Commands: /help  /list  /quit") {
		t.Fatalf("expected visible commands, got %q", rendered)
	}
	if strings.Contains(rendered, "\x1b[?1049h") || strings.Contains(rendered, "\x1b[2J") {
		t.Fatalf("expected no fullscreen terminal control sequences, got %q", rendered)
	}
	notes, err := tukushell.LoadLocalScratchNotes(path)
	if err != nil {
		t.Fatalf("load saved notes: %v", err)
	}
	if len(notes) != 1 || notes[0].Body != "plan repo milestones" {
		t.Fatalf("expected persisted scratch note, got %#v", notes)
	}
}

func TestPrimaryScratchIntakeListsExistingNotes(t *testing.T) {
	now := time.Unix(1710000000, 0).UTC()
	path := t.TempDir() + "/scratch.json"
	if _, err := tukushell.AppendLocalScratchNote(path, "/tmp/no-repo", "draft milestone list", now); err != nil {
		t.Fatalf("seed scratch note: %v", err)
	}

	input := bytes.NewBufferString("/list\n/quit\n")
	var output bytes.Buffer

	app := newPrimaryScratchIntake("/tmp/no-repo", path)
	app.in = input
	app.out = &output
	app.now = func() time.Time { return now }

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run primary scratch intake: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Saved notes: 1 local note(s) already stored here.") {
		t.Fatalf("expected saved note count, got %q", rendered)
	}
	if !strings.Contains(rendered, "Local scratch notes:") || !strings.Contains(rendered, "draft milestone list") {
		t.Fatalf("expected readable note list, got %q", rendered)
	}
}

func TestPrimaryScratchIntakeHandlesBlankAndUnknownCommandsCalmly(t *testing.T) {
	path := t.TempDir() + "/scratch.json"
	input := bytes.NewBufferString("\n/nope\n/help\n/quit\n")
	var output bytes.Buffer

	app := newPrimaryScratchIntake("/tmp/no-repo", path)
	app.in = input
	app.out = &output

	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("run primary scratch intake: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Enter a note to save it locally, or use /help.") {
		t.Fatalf("expected blank-line guidance, got %q", rendered)
	}
	if !strings.Contains(rendered, "Unknown command. Use /help.") {
		t.Fatalf("expected calm unknown-command guidance, got %q", rendered)
	}
	if !strings.Contains(rendered, "Any other line is saved as a local scratch note.") {
		t.Fatalf("expected help text, got %q", rendered)
	}
}
