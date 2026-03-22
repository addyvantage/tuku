package orchestrator

import (
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/phase"
)

// IntentStubCompiler is intentionally simple for early milestones.
// It provides deterministic placeholder intent classification without full NLP logic.
type IntentStubCompiler struct{}

func NewIntentStubCompiler() *IntentStubCompiler {
	return &IntentStubCompiler{}
}

func (c *IntentStubCompiler) Compile(input intent.CompileInput) (intent.State, error) {
	msg := strings.TrimSpace(strings.ToLower(input.LatestMessage))
	intentClass := intent.ClassImplement
	normalized := "start implementation preparation"
	proposedPhase := phase.PhaseInterpreting
	confidence := 0.72

	switch {
	case strings.Contains(msg, "continue"):
		intentClass = intent.ClassContinueTask
		normalized = "continue from current task state"
		confidence = 0.88
	case strings.Contains(msg, "status") || strings.Contains(msg, "explain"):
		intentClass = intent.ClassExplain
		normalized = "summarize current task status"
		confidence = 0.90
	case strings.Contains(msg, "test") || strings.Contains(msg, "validate"):
		intentClass = intent.ClassValidate
		normalized = "prepare validation run"
		confidence = 0.82
	case strings.Contains(msg, "fix") || strings.Contains(msg, "bug"):
		intentClass = intent.ClassDebug
		normalized = "debug and patch issue"
		confidence = 0.80
	case strings.Contains(msg, "pause") || strings.Contains(msg, "stop"):
		intentClass = intent.ClassPause
		normalized = "pause active work"
		proposedPhase = phase.PhasePaused
		confidence = 0.85
	}

	return intent.State{
		Version:               1,
		IntentID:              common.IntentID(newID("int")),
		TaskID:                input.TaskID,
		Class:                 intentClass,
		NormalizedAction:      normalized,
		Confidence:            confidence,
		AmbiguityFlags:        []string{},
		RequiresClarification: false,
		SourceMessageIDs:      []common.MessageID{},
		ProposedPhase:         proposedPhase,
		CreatedAt:             time.Now().UTC(),
	}, nil
}
