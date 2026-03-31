package canonical

import (
	"context"
	"strings"
	"testing"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/phase"
	"tuku/internal/domain/proof"
)

func TestSimpleSynthesizerIncludesValidationSummary(t *testing.T) {
	s := NewSimpleSynthesizer()
	caps := capsule.WorkCapsule{
		CurrentPhase: phase.PhaseValidating,
		NextAction:   "review validation results",
	}
	evidence := []proof.Event{
		{
			Type:        proof.EventValidationResult,
			PayloadJSON: `{"passed":true,"signals":["validation: go test passed"],"output_artifact_ref":"/tmp/validation.txt"}`,
		},
	}

	out, err := s.Synthesize(context.Background(), caps, evidence)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if !strings.Contains(out, "Validation passed on the current bounded evidence") {
		t.Fatalf("expected validation summary in output, got %q", out)
	}
	if !strings.Contains(out, "/tmp/validation.txt") {
		t.Fatalf("expected artifact ref in output, got %q", out)
	}
}

func TestSimpleSynthesizerKeepsValidationTruthWhenWorkerOutputAlsoExists(t *testing.T) {
	s := NewSimpleSynthesizer()
	caps := capsule.WorkCapsule{
		CurrentPhase: phase.PhaseValidating,
		NextAction:   "review validation results",
	}
	evidence := []proof.Event{
		{
			Type:        proof.EventWorkerOutputCaptured,
			PayloadJSON: `{"exit_code":0,"summary":"completed","changed_files":[],"changed_files_semantics":"hint: no new dirty paths compared with pre-run dirty baseline"}`,
		},
		{
			Type:        proof.EventValidationResult,
			PayloadJSON: `{"passed":true,"signals":["validation: git diff --check reported no diff hygiene issues"]}`,
		},
	}

	out, err := s.Synthesize(context.Background(), caps, evidence)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if strings.Contains(out, "validation status remains unknown") {
		t.Fatalf("expected validation truth to win over worker-output unknowns, got %q", out)
	}
	if !strings.Contains(out, "Validation passed on the current bounded evidence") {
		t.Fatalf("expected validation summary in output, got %q", out)
	}
}
