package canonical

import (
	"context"
	"encoding/json"
	"fmt"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/proof"
)

type SimpleSynthesizer struct{}

func NewSimpleSynthesizer() *SimpleSynthesizer {
	return &SimpleSynthesizer{}
}

func (s *SimpleSynthesizer) Synthesize(_ context.Context, c capsule.WorkCapsule, evidence []proof.Event) (string, error) {
	intentSummary := "I interpreted your latest request and updated task state."
	briefSummary := "Execution brief has not been generated yet."
	runSummary := "No active execution run."
	evidenceSummary := "No worker execution evidence has been captured yet."
	unknownsSummary := "Unknowns remain until validation is performed."
	intentSet := false
	briefSet := false
	runSet := false
	evidenceSet := false
	for i := len(evidence) - 1; i >= 0; i-- {
		e := evidence[i]
		switch e.Type {
		case proof.EventIntentCompiled:
			if intentSet {
				continue
			}
			var payload struct {
				Class            string  `json:"class"`
				NormalizedAction string  `json:"normalized_action"`
				Confidence       float64 `json:"confidence"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				intentSummary = fmt.Sprintf("I interpreted your intent as %q (%s, confidence %.2f).", payload.NormalizedAction, payload.Class, payload.Confidence)
				intentSet = true
			}
		case proof.EventBriefCreated:
			if briefSet {
				continue
			}
			var payload struct {
				BriefID   string `json:"brief_id"`
				BriefHash string `json:"brief_hash"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				briefSummary = fmt.Sprintf("Execution brief %s is ready (hash %s).", payload.BriefID, payload.BriefHash)
				briefSet = true
			}
		case proof.EventWorkerRunStarted:
			if runSet {
				continue
			}
			var payload struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				runSummary = fmt.Sprintf("Execution run %s is started.", payload.RunID)
				runSet = true
			}
		case proof.EventRunInterrupted:
			if runSet {
				continue
			}
			var payload struct {
				RunID  string `json:"run_id"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				runSummary = fmt.Sprintf("Execution run %s was interrupted (%s).", payload.RunID, payload.Reason)
				runSet = true
			}
		case proof.EventWorkerRunCompleted:
			if runSet {
				continue
			}
			var payload struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				runSummary = fmt.Sprintf("Execution run %s completed.", payload.RunID)
				runSet = true
			}
		case proof.EventWorkerRunFailed:
			if runSet {
				continue
			}
			var payload struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				runSummary = fmt.Sprintf("Execution run %s failed.", payload.RunID)
				runSet = true
			}
		case proof.EventWorkerOutputCaptured:
			if evidenceSet {
				continue
			}
			var payload struct {
				ExitCode              int      `json:"exit_code"`
				Summary               string   `json:"summary"`
				ChangedFiles          []string `json:"changed_files"`
				ChangedFilesSemantics string   `json:"changed_files_semantics"`
			}
			if err := json.Unmarshal([]byte(e.PayloadJSON), &payload); err == nil {
				if payload.Summary != "" {
					evidenceSummary = fmt.Sprintf("Worker evidence captured (exit code %d). Summary: %s.", payload.ExitCode, payload.Summary)
				} else {
					evidenceSummary = fmt.Sprintf("Worker evidence captured (exit code %d).", payload.ExitCode)
				}
				if len(payload.ChangedFiles) > 0 {
					if payload.ChangedFilesSemantics != "" {
						unknownsSummary = fmt.Sprintf("Worker reported %d changed-file hint(s) (%s); explicit validation remains unknown until validation logic runs.", len(payload.ChangedFiles), payload.ChangedFilesSemantics)
					} else {
						unknownsSummary = fmt.Sprintf("Worker reported %d changed file(s); explicit validation remains unknown until validation logic runs.", len(payload.ChangedFiles))
					}
				} else {
					if payload.ChangedFilesSemantics != "" {
						unknownsSummary = fmt.Sprintf("No changed-file hints were detected (%s); validation status remains unknown.", payload.ChangedFilesSemantics)
					} else {
						unknownsSummary = "No changed files were detected in captured evidence; validation status remains unknown."
					}
				}
				evidenceSet = true
			}
		}
	}

	return fmt.Sprintf(
		"Tuku state updated. %s %s %s %s %s Current phase: %s. Next action: %s.",
		intentSummary,
		briefSummary,
		runSummary,
		evidenceSummary,
		unknownsSummary,
		c.CurrentPhase,
		c.NextAction,
	), nil
}
