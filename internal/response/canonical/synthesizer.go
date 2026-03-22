package canonical

import (
	"context"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/proof"
)

// Synthesizer enforces the canonical response rule:
// worker output is ingested, interpreted, merged to state, grounded in evidence, then surfaced.
type Synthesizer interface {
	Synthesize(ctx context.Context, c capsule.WorkCapsule, evidence []proof.Event) (string, error)
}
