package shell

import (
	"bytes"
	"testing"
)

func TestWriteFinalShellViewWritesRenderedFrame(t *testing.T) {
	var out bytes.Buffer
	err := writeFinalShellView(&out, "TUKU shell\n[ready]\n")
	if err != nil {
		t.Fatalf("write final shell view: %v", err)
	}
	if got := out.String(); got != "TUKU shell\n[ready]\n" {
		t.Fatalf("unexpected final shell view output: %q", got)
	}
}

func TestWriteFinalShellViewSkipsBlankContent(t *testing.T) {
	var out bytes.Buffer
	err := writeFinalShellView(&out, "\n   \n")
	if err != nil {
		t.Fatalf("write final shell view: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected blank final shell view to be skipped, got %q", out.String())
	}
}
