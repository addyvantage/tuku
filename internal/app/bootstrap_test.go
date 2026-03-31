package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	"tuku/internal/domain/handoff"
	"tuku/internal/domain/promptir"
	"tuku/internal/domain/recoveryaction"
	"tuku/internal/domain/run"
	"tuku/internal/domain/taskmemory"
	"tuku/internal/ipc"
	tukushell "tuku/internal/tui/shell"
)

func TestParseShellWorkerPreference(t *testing.T) {
	preference, err := parseShellWorkerPreference("claude")
	if err != nil {
		t.Fatalf("parse claude worker preference: %v", err)
	}
	if preference != "claude" {
		t.Fatalf("expected claude preference, got %q", preference)
	}
}

func TestParseShellWorkerPreferenceRejectsInvalidWorker(t *testing.T) {
	if _, err := parseShellWorkerPreference("invalid-worker"); err == nil {
		t.Fatal("expected invalid worker error")
	}
}

func TestDefaultPathsRespectExplicitEnvironmentOverrides(t *testing.T) {
	t.Setenv("TUKU_DATA_DIR", "/tmp/tuku-data")
	t.Setenv("TUKU_RUN_DIR", "/tmp/tuku-run")
	t.Setenv("TUKU_CACHE_DIR", "/tmp/tuku-cache")
	t.Setenv("TUKU_DB_PATH", "/tmp/tuku-db/custom.db")
	t.Setenv("TUKU_SOCKET_PATH", "/tmp/tuku-run/custom.sock")

	dataRoot, err := defaultDataRoot()
	if err != nil {
		t.Fatalf("default data root: %v", err)
	}
	if dataRoot != "/tmp/tuku-data" {
		t.Fatalf("expected env data root, got %q", dataRoot)
	}

	runRoot, err := defaultRunRoot()
	if err != nil {
		t.Fatalf("default run root: %v", err)
	}
	if runRoot != "/tmp/tuku-run" {
		t.Fatalf("expected env run root, got %q", runRoot)
	}

	cacheRoot, err := defaultCacheRoot()
	if err != nil {
		t.Fatalf("default cache root: %v", err)
	}
	if cacheRoot != "/tmp/tuku-cache" {
		t.Fatalf("expected env cache root, got %q", cacheRoot)
	}

	dbPath, err := defaultDBPath()
	if err != nil {
		t.Fatalf("default db path: %v", err)
	}
	if dbPath != "/tmp/tuku-db/custom.db" {
		t.Fatalf("expected env db path, got %q", dbPath)
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		t.Fatalf("default socket path: %v", err)
	}
	if socketPath != "/tmp/tuku-run/custom.sock" {
		t.Fatalf("expected env socket path, got %q", socketPath)
	}

	scratchPath, err := defaultScratchSessionPath("/tmp/repo")
	if err != nil {
		t.Fatalf("default scratch path: %v", err)
	}
	if !strings.HasPrefix(scratchPath, filepath.Join("/tmp/tuku-cache", "scratch")+string(os.PathSeparator)) {
		t.Fatalf("expected scratch path under cache root, got %q", scratchPath)
	}
}

func TestDefaultRunAndScratchRootsFallBackToDataRoot(t *testing.T) {
	t.Setenv("TUKU_DATA_DIR", "/tmp/tuku-data")
	t.Setenv("TUKU_RUN_DIR", "")
	t.Setenv("TUKU_CACHE_DIR", "")
	t.Setenv("TUKU_DB_PATH", "")
	t.Setenv("TUKU_SOCKET_PATH", "")

	runRoot, err := defaultRunRoot()
	if err != nil {
		t.Fatalf("default run root: %v", err)
	}
	if runRoot != filepath.Join("/tmp/tuku-data", "run") {
		t.Fatalf("expected run root under data root, got %q", runRoot)
	}

	dbPath, err := defaultDBPath()
	if err != nil {
		t.Fatalf("default db path: %v", err)
	}
	if dbPath != filepath.Join("/tmp/tuku-data", "tuku.db") {
		t.Fatalf("expected db path under data root, got %q", dbPath)
	}

	socketPath, err := defaultSocketPath()
	if err != nil {
		t.Fatalf("default socket path: %v", err)
	}
	if socketPath != filepath.Join("/tmp/tuku-data", "run", "tukud.sock") {
		t.Fatalf("expected socket path under data/run, got %q", socketPath)
	}

	scratchPath, err := defaultScratchSessionPath("/tmp/repo")
	if err != nil {
		t.Fatalf("default scratch path: %v", err)
	}
	if !strings.HasPrefix(scratchPath, filepath.Join("/tmp/tuku-data", "scratch")+string(os.PathSeparator)) {
		t.Fatalf("expected scratch path under data root, got %q", scratchPath)
	}
}

func TestCLIShellTranscriptCommandRoutesReadRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskShellTranscriptReadResponse{
			TaskID:                 common.TaskID("tsk_123"),
			SessionID:              "shs_123",
			TranscriptState:        "transcript_only_bounded_partial",
			TranscriptOnly:         true,
			Bounded:                true,
			Partial:                true,
			RetentionLimit:         200,
			RetainedChunkCount:     200,
			DroppedChunkCount:      57,
			OldestRetainedSequence: 81,
			NewestRetainedSequence: 280,
			PageOldestSequence:     81,
			PageNewestSequence:     120,
			PageChunkCount:         40,
			HasMoreOlder:           true,
			NextBeforeSequence:     81,
			RequestedSource:        "worker_output",
			SourceSummary: []ipc.TaskShellTranscriptSourceCount{
				{Source: "fallback_note", Chunks: 12},
				{Source: "worker_output", Chunks: 188},
			},
			Chunks: []ipc.TaskShellTranscriptChunk{
				{ChunkID: "sst_81", TaskID: "tsk_123", SessionID: "shs_123", SequenceNo: 81, Source: "worker_output", Content: "line 81", CreatedAt: time.Unix(1710000000, 0).UTC()},
				{ChunkID: "sst_120", TaskID: "tsk_123", SessionID: "shs_123", SequenceNo: 120, Source: "worker_output", Content: "line 120", CreatedAt: time.Unix(1710000040, 0).UTC()},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"shell", "transcript",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--limit", "40",
		"--before-seq", "121",
		"--source", "worker_output",
	}); err != nil {
		t.Fatalf("run shell transcript command: %v", err)
	}
	if captured.Method != ipc.MethodTaskShellTranscriptRead {
		t.Fatalf("expected transcript read method, got %s", captured.Method)
	}
	var req ipc.TaskShellTranscriptReadRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal transcript read request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.SessionID != "shs_123" || req.Limit != 40 || req.BeforeSequence != 121 || req.Source != "worker_output" {
		t.Fatalf("unexpected transcript read request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "state transcript_only_bounded_partial") {
		t.Fatalf("expected transcript state line in cli output, got %q", output)
	}
	if !strings.Contains(output, "retention retained=200 dropped=57 limit=200") {
		t.Fatalf("expected retention truth in cli output, got %q", output)
	}
	if !strings.Contains(output, "older evidence available yes (use --before-seq 81)") {
		t.Fatalf("expected pagination guidance in cli output, got %q", output)
	}
	if !strings.Contains(output, "truth bounded transcript evidence is partial; older chunks were dropped") {
		t.Fatalf("expected partiality truth line in cli output, got %q", output)
	}
	if !strings.Contains(output, "does not imply live worker resumability") {
		t.Fatalf("expected resumability guard line in cli output, got %q", output)
	}
}

func TestCLIShellTranscriptCommandRejectsInvalidSource(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"shell", "transcript",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--source", "invalid_source",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid --source") {
		t.Fatalf("expected invalid source error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called when source filter is invalid")
	}
}

func TestCLIShellTranscriptReviewCommandRoutesRequestAndPrintsTruthfulResult(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskShellTranscriptReviewResponse{
			TaskID:                 common.TaskID("tsk_123"),
			SessionID:              "shs_123",
			TranscriptState:        "transcript_only_bounded_partial",
			RetentionLimit:         200,
			RetainedChunkCount:     200,
			DroppedChunkCount:      57,
			OldestRetainedSequence: 81,
			NewestRetainedSequence: 280,
			LatestReview: ipc.TaskShellTranscriptReviewMarker{
				ReviewID:             "srev_123",
				SourceFilter:         "worker_output",
				ReviewedUpToSequence: 180,
				Summary:              "reviewed most recent worker output window",
				CreatedAt:            time.Unix(1710000100, 0).UTC(),
				NewerRetainedCount:   100,
				StaleBehindLatest:    true,
			},
			HasUnreadNewerEvidence: true,
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"shell", "transcript", "review",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--up-to-seq", "180",
		"--source", "worker_output",
		"--summary", "reviewed most recent worker output window",
	}); err != nil {
		t.Fatalf("run shell transcript review command: %v", err)
	}
	if captured.Method != ipc.MethodTaskShellTranscriptReview {
		t.Fatalf("expected transcript review method, got %s", captured.Method)
	}
	var req ipc.TaskShellTranscriptReviewRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal transcript review request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.SessionID != "shs_123" || req.ReviewedUpToSeq != 180 || req.Source != "worker_output" {
		t.Fatalf("unexpected transcript review request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "reviewed up to sequence 180 (worker_output)") {
		t.Fatalf("expected review boundary line in cli output, got %q", output)
	}
	if !strings.Contains(output, "newer retained evidence exists beyond review boundary") {
		t.Fatalf("expected stale review guidance in cli output, got %q", output)
	}
	if !strings.Contains(output, "do not imply task completion, correctness, or worker resumability") {
		t.Fatalf("expected conservative truth guard in cli output, got %q", output)
	}
}

func TestCLIShellTranscriptReviewCommandRejectsNonPositiveSequence(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"shell", "transcript", "review",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--up-to-seq", "0",
	})
	if err == nil || !strings.Contains(err.Error(), "--up-to-seq") {
		t.Fatalf("expected --up-to-seq validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for invalid review sequence")
	}
}

func TestCLIShellTranscriptHistoryCommandRoutesRequestAndPrintsTruthfulResult(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskShellTranscriptHistoryResponse{
			TaskID:                 common.TaskID("tsk_123"),
			SessionID:              "shs_123",
			TranscriptState:        "transcript_only_bounded_partial",
			TranscriptOnly:         true,
			Bounded:                true,
			Partial:                true,
			RetentionLimit:         200,
			RetainedChunkCount:     200,
			DroppedChunkCount:      57,
			OldestRetainedSequence: 81,
			NewestRetainedSequence: 280,
			RequestedLimit:         5,
			RequestedSource:        "worker_output",
			Closure: ipc.TaskShellTranscriptReviewClosure{
				State:                    "source_scoped_review_stale_within_retained",
				Scope:                    "worker_output",
				HasReview:                true,
				HasUnreadNewerEvidence:   true,
				ReviewedUpToSequence:     184,
				OldestUnreviewedSequence: 185,
				NewestRetainedSequence:   280,
				UnreviewedRetainedCount:  96,
			},
			LatestReview: &ipc.TaskShellTranscriptReviewMarker{
				ReviewID:                 "srev_123",
				SourceFilter:             "worker_output",
				ReviewedUpToSequence:     184,
				CreatedAt:                time.Unix(1710000100, 0).UTC(),
				StaleBehindLatest:        true,
				NewerRetainedCount:       96,
				OldestUnreviewedSequence: 185,
			},
			Reviews: []ipc.TaskShellTranscriptReviewMarker{
				{
					ReviewID:             "srev_123",
					SourceFilter:         "worker_output",
					ReviewedUpToSequence: 184,
					Summary:              "reviewed worker output segment",
					CreatedAt:            time.Unix(1710000100, 0).UTC(),
					StaleBehindLatest:    true,
					NewerRetainedCount:   96,
				},
				{
					ReviewID:             "srev_120",
					SourceFilter:         "",
					ReviewedUpToSequence: 150,
					CreatedAt:            time.Unix(1710000000, 0).UTC(),
					StaleBehindLatest:    true,
					NewerRetainedCount:   130,
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"shell", "transcript", "history",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--limit", "5",
		"--source", "worker_output",
	}); err != nil {
		t.Fatalf("run shell transcript history command: %v", err)
	}
	if captured.Method != ipc.MethodTaskShellTranscriptHistory {
		t.Fatalf("expected transcript history method, got %s", captured.Method)
	}
	var req ipc.TaskShellTranscriptHistoryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal transcript history request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.SessionID != "shs_123" || req.Limit != 5 || req.Source != "worker_output" {
		t.Fatalf("unexpected transcript history request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "review closure source_scoped_review_stale_within_retained") {
		t.Fatalf("expected closure state line in history output, got %q", output)
	}
	if !strings.Contains(output, "newer retained evidence exists 185-280 (+96 seq)") {
		t.Fatalf("expected stale delta range in history output, got %q", output)
	}
	if !strings.Contains(output, "truth review history records bounded evidence review markers only") {
		t.Fatalf("expected conservative truth guard in history output, got %q", output)
	}
}

func TestCLIShellTranscriptHistoryCommandRejectsNonPositiveLimit(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"shell", "transcript", "history",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--limit", "0",
	})
	if err == nil || !strings.Contains(err.Error(), "--limit") {
		t.Fatalf("expected --limit validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for invalid history limit")
	}
}

func TestCLITransitionHistoryCommandRoutesRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskTransitionHistoryResponse{
			TaskID:                   common.TaskID("tsk_transition"),
			Bounded:                  true,
			RequestedLimit:           5,
			RequestedBeforeReceiptID: "ctr_120",
			RequestedTransitionKind:  "HANDOFF_LAUNCH",
			RequestedHandoffID:       "hnd_123",
			HasMoreOlder:             true,
			NextBeforeReceiptID:      "ctr_115",
			Latest: &ipc.TaskContinuityTransitionReceipt{
				ReceiptID:          "ctr_130",
				TransitionKind:     "HANDOFF_LAUNCH",
				HandoffStateBefore: "ACCEPTED_NOT_LAUNCHED",
				HandoffStateAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
				ReviewPosture:      "GLOBAL_REVIEW_STALE",
				CreatedAt:          time.Unix(1710002000, 0).UTC(),
			},
			Receipts: []ipc.TaskContinuityTransitionReceipt{
				{
					ReceiptID:          "ctr_130",
					TransitionKind:     "HANDOFF_LAUNCH",
					HandoffStateBefore: "ACCEPTED_NOT_LAUNCHED",
					HandoffStateAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
					BranchClassBefore:  "LOCAL",
					BranchClassAfter:   "HANDOFF_CLAUDE",
					ReviewPosture:      "GLOBAL_REVIEW_STALE",
					CreatedAt:          time.Unix(1710002000, 0).UTC(),
				},
				{
					ReceiptID:             "ctr_125",
					TransitionKind:        "HANDOFF_LAUNCH",
					HandoffStateBefore:    "ACCEPTED_NOT_LAUNCHED",
					HandoffStateAfter:     "LAUNCH_COMPLETED_ACK_CAPTURED",
					BranchClassBefore:     "LOCAL",
					BranchClassAfter:      "HANDOFF_CLAUDE",
					ReviewPosture:         "GLOBAL_REVIEW_STALE",
					AcknowledgmentPresent: true,
					CreatedAt:             time.Unix(1710001900, 0).UTC(),
				},
			},
			RiskSummary: ipc.TaskContinuityTransitionRiskSummary{
				WindowSize:                         2,
				ReviewGapTransitions:               2,
				AcknowledgedReviewGapTransitions:   1,
				UnacknowledgedReviewGapTransitions: 1,
				StaleReviewPostureTransitions:      2,
				IntoClaudeOwnershipTransitions:     2,
				OperationallyNotable:               true,
				Summary:                            "1 transition(s) recorded with unacknowledged transcript review gaps",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"transition", "history",
		"--task", "tsk_transition",
		"--limit", "5",
		"--before-receipt", "ctr_120",
		"--kind", "handoff_launch",
		"--handoff", "hnd_123",
	}); err != nil {
		t.Fatalf("run transition history command: %v", err)
	}
	if captured.Method != ipc.MethodTaskTransitionHistory {
		t.Fatalf("expected transition history method, got %s", captured.Method)
	}
	var req ipc.TaskTransitionHistoryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal transition history request: %v", err)
	}
	if req.TaskID != "tsk_transition" || req.Limit != 5 || req.BeforeReceiptID != "ctr_120" || req.TransitionKind != "HANDOFF_LAUNCH" || req.HandoffID != "hnd_123" {
		t.Fatalf("unexpected transition history request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "filter transition kind HANDOFF_LAUNCH") {
		t.Fatalf("expected transition kind filter line in output, got %q", output)
	}
	if !strings.Contains(output, "older receipts available yes (use --before-receipt ctr_115)") {
		t.Fatalf("expected pagination guidance in transition history output, got %q", output)
	}
	if !strings.Contains(output, "risk counts review-gap=2 acknowledged=1 unacknowledged=1") {
		t.Fatalf("expected compact risk count summary in transition history output, got %q", output)
	}
	if !strings.Contains(output, "truth transition receipts are audit evidence only") {
		t.Fatalf("expected conservative truth guard in transition history output, got %q", output)
	}
}

func TestCLITransitionHistoryCommandRejectsInvalidKind(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"transition", "history",
		"--task", "tsk_transition",
		"--kind", "invalid_kind",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid --kind") {
		t.Fatalf("expected invalid --kind validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called when transition kind filter is invalid")
	}
}

func TestCLIIncidentCommandRoutesRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentSliceResponse{
			TaskID:                             common.TaskID("tsk_incident"),
			Bounded:                            true,
			AnchorMode:                         "TRANSITION_RECEIPT_ID",
			RequestedAnchorTransitionReceiptID: "ctr_200",
			Anchor: ipc.TaskContinuityTransitionReceipt{
				ReceiptID:          "ctr_200",
				TransitionKind:     "HANDOFF_LAUNCH",
				HandoffStateBefore: "ACCEPTED_NOT_LAUNCHED",
				HandoffStateAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
				ReviewPosture:      "GLOBAL_REVIEW_STALE",
				CreatedAt:          time.Unix(1712000000, 0).UTC(),
			},
			TransitionNeighborLimit:          2,
			RunLimit:                         3,
			RecoveryLimit:                    3,
			ProofLimit:                       6,
			AckLimit:                         2,
			HasOlderTransitionsOutsideWindow: true,
			HasNewerTransitionsOutsideWindow: false,
			WindowStartAt:                    time.Unix(1711999900, 0).UTC(),
			WindowEndAt:                      time.Unix(1712000100, 0).UTC(),
			Transitions: []ipc.TaskContinuityTransitionReceipt{
				{
					ReceiptID:          "ctr_200",
					TransitionKind:     "HANDOFF_LAUNCH",
					HandoffStateBefore: "ACCEPTED_NOT_LAUNCHED",
					HandoffStateAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
					ReviewPosture:      "GLOBAL_REVIEW_STALE",
					CreatedAt:          time.Unix(1712000000, 0).UTC(),
				},
			},
			Runs: []ipc.TaskContinuityIncidentRun{
				{
					RunID:      common.RunID("run_200"),
					WorkerKind: run.WorkerKindCodex,
					Status:     run.StatusFailed,
					OccurredAt: time.Unix(1712000010, 0).UTC(),
					StartedAt:  time.Unix(1712000000, 0).UTC(),
					Summary:    "run failed with validation issues",
				},
			},
			RecoveryActions: []ipc.TaskRecoveryActionRecord{
				{
					ActionID:        "ract_200",
					TaskID:          common.TaskID("tsk_incident"),
					Kind:            "FAILED_RUN_REVIEWED",
					Summary:         "reviewed failed run evidence",
					CreatedAtUnixMs: time.Unix(1712000020, 0).UTC().UnixMilli(),
				},
			},
			ProofEvents: []ipc.TaskContinuityIncidentProof{
				{
					EventID:    "evt_200",
					Type:       "BRANCH_HANDOFF_TRANSITION_RECORDED",
					ActorType:  "SYSTEM",
					ActorID:    "tuku-daemon",
					Timestamp:  time.Unix(1712000005, 0).UTC(),
					Summary:    "Branch/handoff transition receipt recorded",
					SequenceNo: 200,
				},
			},
			LatestTranscriptReviewGapAck: &ipc.TaskTranscriptReviewGapAcknowledgment{
				AcknowledgmentID: "sack_200",
				Class:            "stale_review",
				ReviewState:      "global_review_stale",
			},
			RiskSummary: ipc.TaskContinuityIncidentRiskSummary{
				ReviewGapPresent:                true,
				AcknowledgmentPresent:           false,
				StaleOrUnreviewedReviewPosture:  true,
				UnresolvedContinuityAmbiguity:   true,
				NearbyFailedOrInterruptedRuns:   1,
				NearbyRecoveryActions:           1,
				RecentFailureOrRecoveryActivity: true,
				OperationallyNotable:            true,
				Summary:                         "anchor transition carried stale retained transcript posture with nearby failed run evidence",
			},
			Caveat: "bounded incident slice caveat",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"incident",
		"--task", "tsk_incident",
		"--anchor-transition", "ctr_200",
		"--transitions", "2",
		"--runs", "3",
		"--recovery", "3",
		"--proofs", "6",
		"--acks", "2",
	}); err != nil {
		t.Fatalf("run incident command: %v", err)
	}
	if captured.Method != ipc.MethodTaskContinuityIncidentSlice {
		t.Fatalf("expected continuity incident method, got %s", captured.Method)
	}
	var req ipc.TaskContinuityIncidentSliceRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal continuity incident request: %v", err)
	}
	if req.TaskID != "tsk_incident" || req.AnchorTransitionReceiptID != "ctr_200" || req.TransitionNeighborLimit != 2 || req.RunLimit != 3 || req.RecoveryLimit != 3 || req.ProofLimit != 6 || req.AckLimit != 2 {
		t.Fatalf("unexpected continuity incident request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "window bounds older-outside=true newer-outside=false") {
		t.Fatalf("expected incident boundedness line in output, got %q", output)
	}
	if !strings.Contains(output, "risk flags review-gap=true") {
		t.Fatalf("expected incident risk flags line in output, got %q", output)
	}
	if !strings.Contains(output, "truth bounded incident slice caveat") {
		t.Fatalf("expected conservative truth caveat in incident output, got %q", output)
	}
}

func TestCLIIncidentCommandRejectsInvalidLimits(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"incident",
		"--task", "tsk_incident",
		"--proofs", "0",
	})
	if err == nil || !strings.Contains(err.Error(), "--proofs") {
		t.Fatalf("expected --proofs validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for invalid incident command limits")
	}
}

func TestCLIIncidentTriageHistoryCommandRoutesRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentTriageHistoryResponse{
			TaskID:                             common.TaskID("tsk_triage_history"),
			Bounded:                            true,
			RequestedLimit:                     4,
			RequestedBeforeReceiptID:           "citr_590",
			RequestedAnchorTransitionReceiptID: "ctr_600",
			RequestedPosture:                   "NEEDS_FOLLOW_UP",
			HasMoreOlder:                       true,
			NextBeforeReceiptID:                "citr_605",
			LatestTransitionReceiptID:          "ctr_620",
			Latest: &ipc.TaskContinuityIncidentTriageReceipt{
				ReceiptID:                 "citr_610",
				AnchorTransitionReceiptID: "ctr_600",
				Posture:                   "NEEDS_FOLLOW_UP",
				FollowUpPosture:           "ADVISORY_OPEN",
				CreatedAt:                 time.Unix(1712000600, 0).UTC(),
			},
			Receipts: []ipc.TaskContinuityIncidentTriageReceipt{
				{
					ReceiptID:                 "citr_610",
					AnchorTransitionReceiptID: "ctr_600",
					Posture:                   "NEEDS_FOLLOW_UP",
					FollowUpPosture:           "ADVISORY_OPEN",
					ReviewGapPresent:          true,
					AcknowledgmentPresent:     false,
					CreatedAt:                 time.Unix(1712000600, 0).UTC(),
					Summary:                   "follow-up remains open",
				},
				{
					ReceiptID:                 "citr_605",
					AnchorTransitionReceiptID: "ctr_600",
					Posture:                   "NEEDS_FOLLOW_UP",
					FollowUpPosture:           "ADVISORY_OPEN",
					ReviewGapPresent:          true,
					AcknowledgmentPresent:     true,
					CreatedAt:                 time.Unix(1712000500, 0).UTC(),
				},
			},
			Rollup: ipc.TaskContinuityIncidentTriageHistoryRollup{
				WindowSize:                        2,
				BoundedWindow:                     true,
				DistinctAnchors:                   1,
				AnchorsNeedsFollowUp:              1,
				AnchorsWithOpenFollowUp:           1,
				AnchorsBehindLatestTransition:     1,
				AnchorsRepeatedWithoutProgression: 1,
				ReviewRiskReceipts:                2,
				AcknowledgedReviewGapReceipts:     1,
				OperationallyNotable:              true,
				Summary:                           "1 anchor(s) remain in open follow-up posture",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"incident", "triage", "history",
		"--task", "tsk_triage_history",
		"--limit", "4",
		"--before-receipt", "citr_590",
		"--anchor", "ctr_600",
		"--posture", "needs_follow_up",
	}); err != nil {
		t.Fatalf("run incident triage history command: %v", err)
	}
	if captured.Method != ipc.MethodTaskContinuityIncidentTriageHistory {
		t.Fatalf("expected continuity incident triage history method, got %s", captured.Method)
	}
	var req ipc.TaskContinuityIncidentTriageHistoryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal incident triage history request: %v", err)
	}
	if req.TaskID != "tsk_triage_history" || req.Limit != 4 || req.BeforeReceiptID != "citr_590" || req.AnchorTransitionReceiptID != "ctr_600" || req.Posture != "NEEDS_FOLLOW_UP" {
		t.Fatalf("unexpected incident triage history request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "filter anchor ctr_600") {
		t.Fatalf("expected anchor filter line in triage history output, got %q", output)
	}
	if !strings.Contains(output, "older triage receipts available yes (use --before-receipt citr_605)") {
		t.Fatalf("expected triage history pagination guidance in output, got %q", output)
	}
	if !strings.Contains(output, "rollup counts anchors=1 open-follow-up=1 needs=1 deferred=0 behind-latest=1 repeated=1 review-risk=2 acknowledged-review-gap=1") {
		t.Fatalf("expected compact triage-history rollup summary, got %q", output)
	}
	if !strings.Contains(output, "truth incident triage history is bounded audit evidence only") {
		t.Fatalf("expected conservative truth guard in triage-history output, got %q", output)
	}
}

func TestCLIIncidentTriageHistoryCommandRejectsInvalidPostureFilter(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"incident", "triage", "history",
		"--task", "tsk_triage_history",
		"--posture", "invalid_posture",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid --posture") {
		t.Fatalf("expected invalid --posture validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for invalid triage-history posture filter")
	}
}

func TestCLIIncidentFollowUpCommandRoutesRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentFollowUpResponse{
			TaskID:                    common.TaskID("tsk_followup"),
			AnchorMode:                "LATEST_TRANSITION",
			AnchorTransitionReceiptID: "ctr_700",
			TriageReceiptID:           "citr_705",
			ActionKind:                "PROGRESSED",
			Reused:                    false,
			Receipt: ipc.TaskContinuityIncidentFollowUpReceipt{
				ReceiptID:                 "cifr_710",
				AnchorTransitionReceiptID: "ctr_700",
				TriageReceiptID:           "citr_705",
				ActionKind:                "PROGRESSED",
				Summary:                   "follow-up advanced with review-risk awareness",
				ReviewGapPresent:          true,
				AcknowledgmentPresent:     true,
				CreatedAt:                 time.Unix(1712000710, 0).UTC(),
			},
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:                    "FOLLOW_UP_PROGRESSED",
				Digest:                   "follow-up open",
				WindowAdvisory:           "bounded window open=1",
				Advisory:                 "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
				FollowUpAdvised:          true,
				NeedsFollowUp:            true,
				LatestFollowUpReceiptID:  "cifr_710",
				LatestFollowUpActionKind: "PROGRESSED",
				FollowUpOpen:             true,
				FollowUpProgressed:       true,
			},
			ContinuityIncidentFollowUpHistoryRollup: &ipc.TaskContinuityIncidentFollowUpHistoryRollup{
				WindowSize:              1,
				DistinctAnchors:         1,
				ReceiptsProgressed:      1,
				AnchorsWithOpenFollowUp: 1,
				OperationallyNotable:    true,
				Summary:                 "1 anchor(s) have open follow-up receipts",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"incident", "followup",
		"--task", "tsk_followup",
		"--anchor", "latest",
		"--triage-receipt", "citr_705",
		"--action", "progressed",
		"--summary", "follow-up advanced with review-risk awareness",
	}); err != nil {
		t.Fatalf("run incident followup command: %v", err)
	}
	if captured.Method != ipc.MethodTaskContinuityIncidentFollowUp {
		t.Fatalf("expected continuity incident follow-up method, got %s", captured.Method)
	}
	var req ipc.TaskContinuityIncidentFollowUpRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal incident follow-up request: %v", err)
	}
	if req.TaskID != "tsk_followup" || req.AnchorMode != "latest" || req.TriageReceiptID != "citr_705" || req.ActionKind != "PROGRESSED" {
		t.Fatalf("unexpected incident follow-up request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "action PROGRESSED") {
		t.Fatalf("expected action line in follow-up output, got %q", output)
	}
	if !strings.Contains(output, "rollup counts anchors=1 open=1 closed=0 reopened=0") {
		t.Fatalf("expected compact follow-up rollup line, got %q", output)
	}
	if !strings.Contains(output, "follow-up digest follow-up open") {
		t.Fatalf("expected follow-up digest line in output, got %q", output)
	}
	if !strings.Contains(output, "follow-up window bounded window open=1") {
		t.Fatalf("expected follow-up bounded-window line in output, got %q", output)
	}
	if !strings.Contains(output, "truth incident follow-up receipts are bounded audit evidence only") {
		t.Fatalf("expected conservative truth guard in follow-up output, got %q", output)
	}
}

func TestCLIIncidentFollowUpHistoryCommandRoutesRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentFollowUpHistoryResponse{
			TaskID:                             common.TaskID("tsk_followup_history"),
			Bounded:                            true,
			RequestedLimit:                     3,
			RequestedBeforeReceiptID:           "cifr_690",
			RequestedAnchorTransitionReceiptID: "ctr_680",
			RequestedTriageReceiptID:           "citr_685",
			RequestedActionKind:                "CLOSED",
			HasMoreOlder:                       true,
			NextBeforeReceiptID:                "cifr_675",
			LatestTransitionReceiptID:          "ctr_700",
			Latest: &ipc.TaskContinuityIncidentFollowUpReceipt{
				ReceiptID:                 "cifr_695",
				AnchorTransitionReceiptID: "ctr_680",
				TriageReceiptID:           "citr_685",
				ActionKind:                "CLOSED",
				CreatedAt:                 time.Unix(1712000695, 0).UTC(),
			},
			Receipts: []ipc.TaskContinuityIncidentFollowUpReceipt{
				{
					ReceiptID:                 "cifr_695",
					AnchorTransitionReceiptID: "ctr_680",
					TriageReceiptID:           "citr_685",
					ActionKind:                "CLOSED",
					ReviewGapPresent:          true,
					AcknowledgmentPresent:     true,
					CreatedAt:                 time.Unix(1712000695, 0).UTC(),
				},
				{
					ReceiptID:                 "cifr_675",
					AnchorTransitionReceiptID: "ctr_680",
					TriageReceiptID:           "citr_685",
					ActionKind:                "PROGRESSED",
					ReviewGapPresent:          true,
					AcknowledgmentPresent:     true,
					CreatedAt:                 time.Unix(1712000675, 0).UTC(),
				},
			},
			Rollup: ipc.TaskContinuityIncidentFollowUpHistoryRollup{
				WindowSize:                        2,
				DistinctAnchors:                   1,
				ReceiptsProgressed:                1,
				ReceiptsClosed:                    1,
				AnchorsClosed:                     1,
				OpenAnchorsBehindLatestTransition: 1,
				OperationallyNotable:              true,
				Summary:                           "1 open anchor(s) are behind the latest transition anchor",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"incident", "followup", "history",
		"--task", "tsk_followup_history",
		"--limit", "3",
		"--before-receipt", "cifr_690",
		"--anchor", "ctr_680",
		"--triage-receipt", "citr_685",
		"--action", "closed",
	}); err != nil {
		t.Fatalf("run incident followup history command: %v", err)
	}
	if captured.Method != ipc.MethodTaskContinuityIncidentFollowUpHistory {
		t.Fatalf("expected continuity incident follow-up history method, got %s", captured.Method)
	}
	var req ipc.TaskContinuityIncidentFollowUpHistoryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal incident follow-up history request: %v", err)
	}
	if req.TaskID != "tsk_followup_history" || req.Limit != 3 || req.BeforeReceiptID != "cifr_690" || req.AnchorTransitionReceiptID != "ctr_680" || req.TriageReceiptID != "citr_685" || req.ActionKind != "CLOSED" {
		t.Fatalf("unexpected incident follow-up history request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "filter action CLOSED") {
		t.Fatalf("expected action filter line in follow-up history output, got %q", output)
	}
	if !strings.Contains(output, "older follow-up receipts available yes (use --before-receipt cifr_675)") {
		t.Fatalf("expected follow-up history pagination guidance in output, got %q", output)
	}
	if !strings.Contains(output, "rollup counts anchors=1 open=0 closed=1 reopened=0 pending=0 progressed=1") {
		t.Fatalf("expected compact follow-up-history rollup summary, got %q", output)
	}
	if !strings.Contains(output, "truth incident follow-up history is bounded audit evidence only") {
		t.Fatalf("expected conservative truth guard in follow-up-history output, got %q", output)
	}
}

func TestCLIIncidentFollowUpHistoryCommandRejectsInvalidActionFilter(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"incident", "followup", "history",
		"--task", "tsk_followup_history",
		"--action", "invalid_action",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid --action") {
		t.Fatalf("expected invalid --action validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for invalid follow-up-history action filter")
	}
}

func TestCLIIncidentClosureCommandRoutesRequestAndPrintsTruthfulSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentClosureResponse{
			TaskID:                    common.TaskID("tsk_closure"),
			Bounded:                   true,
			RequestedLimit:            4,
			RequestedBeforeReceiptID:  "cifr_860",
			HasMoreOlder:              true,
			NextBeforeReceiptID:       "cifr_855",
			LatestTransitionReceiptID: "ctr_900",
			Latest: &ipc.TaskContinuityIncidentFollowUpReceipt{
				ReceiptID:                 "cifr_865",
				AnchorTransitionReceiptID: "ctr_880",
				TriageReceiptID:           "citr_882",
				ActionKind:                "REOPENED",
				CreatedAt:                 time.Unix(1713000865, 0).UTC(),
			},
			Receipts: []ipc.TaskContinuityIncidentFollowUpReceipt{
				{
					ReceiptID:                 "cifr_865",
					AnchorTransitionReceiptID: "ctr_880",
					TriageReceiptID:           "citr_882",
					ActionKind:                "REOPENED",
					CreatedAt:                 time.Unix(1713000865, 0).UTC(),
				},
			},
			Rollup: ipc.TaskContinuityIncidentFollowUpHistoryRollup{
				WindowSize:                        1,
				BoundedWindow:                     true,
				DistinctAnchors:                   1,
				ReceiptsReopened:                  1,
				AnchorsWithOpenFollowUp:           1,
				AnchorsReopened:                   1,
				OpenAnchorsBehindLatestTransition: 1,
				OperationallyNotable:              true,
				Summary:                           "bounded evidence includes reopened follow-up posture",
			},
			FollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:          "FOLLOW_UP_REOPENED",
				Digest:         "follow-up reopened",
				WindowAdvisory: "bounded window open=1 reopened=1",
				Advisory:       "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
			},
			Closure: &ipc.TaskContinuityIncidentClosureSummary{
				Class:                   "WEAK_CLOSURE_REOPENED",
				Digest:                  "closure reopened after close",
				WindowAdvisory:          "bounded window anchors=1 open=1 closed=0 reopened=1 triaged-without-follow-up=0 repeated=0 behind-latest=1",
				Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
				OperationallyUnresolved: true,
				ClosureAppearsWeak:      true,
				ReopenedAfterClosure:    true,
				RecentAnchors: []ipc.TaskContinuityIncidentClosureAnchorItem{
					{
						AnchorTransitionReceiptID: "ctr_880",
						Class:                     "WEAK_CLOSURE_REOPENED",
						LatestFollowUpReceiptID:   "cifr_865",
						LatestFollowUpActionKind:  "REOPENED",
						LatestFollowUpAt:          time.Unix(1713000865, 0).UTC(),
						Explanation:               "reopened after closure in recent bounded evidence",
					},
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"incident", "closure",
		"--task", "tsk_closure",
		"--limit", "4",
		"--before-receipt", "cifr_860",
	}); err != nil {
		t.Fatalf("run incident closure command: %v", err)
	}
	if captured.Method != ipc.MethodTaskContinuityIncidentClosure {
		t.Fatalf("expected continuity incident closure method, got %s", captured.Method)
	}
	var req ipc.TaskContinuityIncidentClosureRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal incident closure request: %v", err)
	}
	if req.TaskID != "tsk_closure" || req.Limit != 4 || req.BeforeReceiptID != "cifr_860" {
		t.Fatalf("unexpected incident closure request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "closure class WEAK_CLOSURE_REOPENED") {
		t.Fatalf("expected closure class in output, got %q", output)
	}
	if !strings.Contains(output, "closure digest closure reopened after close") {
		t.Fatalf("expected closure digest in output, got %q", output)
	}
	if !strings.Contains(output, "closure signals unresolved=true weak=true reopened-after-close=true") {
		t.Fatalf("expected compact closure signals line in output, got %q", output)
	}
	if !strings.Contains(output, "ctr_880 class=WEAK_CLOSURE_REOPENED action=REOPENED follow-up=cifr_865") {
		t.Fatalf("expected closure per-anchor timeline line in output, got %q", output)
	}
	if !strings.Contains(output, "truth incident closure intelligence is bounded advisory evidence only") {
		t.Fatalf("expected conservative closure truth guard, got %q", output)
	}
}

func TestCLIIncidentClosureCommandRejectsInvalidLimit(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{
		"incident", "closure",
		"--task", "tsk_closure",
		"--limit", "0",
	})
	if err == nil || !strings.Contains(err.Error(), "--limit") {
		t.Fatalf("expected --limit validation error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for invalid closure command limit")
	}
}

func TestCLIIncidentRiskCommandRoutesRequestAndPrintsConservativeSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentTaskRiskResponse{
			TaskID:                   common.TaskID("tsk_risk"),
			Bounded:                  true,
			RequestedLimit:           6,
			RequestedBeforeReceiptID: "cifr_760",
			HasMoreOlder:             true,
			NextBeforeReceiptID:      "cifr_750",
			Summary: &ipc.TaskContinuityIncidentTaskRiskSummary{
				Class:                       "RECURRING_WEAK_CLOSURE",
				Digest:                      "recurring continuity weakness in recent bounded evidence",
				WindowAdvisory:              "bounded incident window anchors=3 open=1 reopened=2 triaged-without-follow-up=0 stagnant=0",
				Detail:                      "Recent bounded evidence suggests recurring weak closure posture across incidents.",
				RecurringWeakClosure:        true,
				RecurringUnresolved:         true,
				ReopenedAfterClosureAnchors: 2,
				RepeatedReopenLoopAnchors:   1,
				DistinctAnchors:             3,
				AnchorsWithOpenFollowUp:     1,
				AnchorsReopened:             2,
				RecentAnchorClasses: []string{
					"WEAK_CLOSURE_REOPENED",
					"WEAK_CLOSURE_LOOP",
				},
			},
			Closure: &ipc.TaskContinuityIncidentClosureSummary{
				Class:  "WEAK_CLOSURE_LOOP",
				Digest: "closure loop signals",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"incident", "risk",
		"--task", "tsk_risk",
		"--limit", "6",
		"--before-receipt", "cifr_760",
	}); err != nil {
		t.Fatalf("run incident risk command: %v", err)
	}
	if captured.Method != ipc.MethodTaskContinuityIncidentRisk {
		t.Fatalf("expected continuity incident task-risk method, got %s", captured.Method)
	}
	var req ipc.TaskContinuityIncidentTaskRiskRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal incident risk request: %v", err)
	}
	if req.TaskID != "tsk_risk" || req.Limit != 6 || req.BeforeReceiptID != "cifr_760" {
		t.Fatalf("unexpected incident risk request payload: %+v", req)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "task risk class recurring_weak_closure") {
		t.Fatalf("expected task-risk class line in output, got %q", output)
	}
	if !strings.Contains(output, "task risk digest recurring continuity weakness in recent bounded evidence") {
		t.Fatalf("expected task-risk digest in output, got %q", output)
	}
	if !strings.Contains(output, "recent anchor classes weak closure reopened, weak closure loop") {
		t.Fatalf("expected compact task-risk class cues in output, got %q", output)
	}
	if strings.Contains(output, "root cause solved") || strings.Contains(output, "safe") || strings.Contains(output, "completed") {
		t.Fatalf("task-risk output must remain conservative, got %q", output)
	}
}

func TestCLIStatusCommandHumanUsesFollowUpDigestVocabulary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID:                     common.TaskID("tsk_status_human"),
			Phase:                      "BRIEF_READY",
			Status:                     "ACTIVE",
			RequiredNextOperatorAction: "START_LOCAL_RUN",
			OperatorDecision: &ipc.TaskOperatorDecisionSummary{
				Headline: "Local fresh run ready",
			},
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:           "TRIAGED_CURRENT",
				Digest:          "triaged without follow-up",
				WindowAdvisory:  "bounded window triaged-without-follow-up=1 open=1",
				Advisory:        "Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
				FollowUpAdvised: true,
				ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
					Class:                  "OPERATIONALLY_UNRESOLVED",
					Digest:                 "operationally unresolved",
					WindowAdvisory:         "bounded window anchors=1 open=1 closed=0 reopened=0 triaged-without-follow-up=1 repeated=0 behind-latest=0",
					Detail:                 "Recent bounded evidence suggests incident closure remains operationally unresolved.",
					BoundedWindow:          true,
					WindowSize:             1,
					DistinctAnchors:        1,
					TriagedWithoutFollowUp: true,
				},
			},
			ContinuityIncidentTaskRisk: &ipc.TaskContinuityIncidentTaskRiskSummary{
				Class:                           "RECURRING_TRIAGED_WITHOUT_FOLLOW_UP",
				Digest:                          "repeated triaged-without-follow-up posture in recent bounded evidence",
				WindowAdvisory:                  "bounded incident window anchors=2 open=1 reopened=0 triaged-without-follow-up=2 stagnant=0",
				Detail:                          "Recent bounded evidence suggests repeated triaged incidents still lack follow-up receipts.",
				RecurringTriagedWithoutFollowUp: true,
				DistinctAnchors:                 2,
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"status", "--task", "tsk_status_human", "--human"}); err != nil {
		t.Fatalf("run status --human command: %v", err)
	}
	if captured.Method != ipc.MethodTaskStatus {
		t.Fatalf("expected task.status method, got %s", captured.Method)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "follow-up advisory") || !strings.Contains(output, "digest triaged without follow-up") {
		t.Fatalf("expected follow-up digest vocabulary in status human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window triaged-without-follow-up=1 open=1") {
		t.Fatalf("expected bounded-window cue in status human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest operationally unresolved") {
		t.Fatalf("expected closure advisory digest in status human output, got %q", output)
	}
	if !strings.Contains(output, "detail Recent bounded evidence suggests incident closure remains operationally unresolved.") {
		t.Fatalf("expected bounded closure detail in status human output, got %q", output)
	}
	if !strings.Contains(output, "task incident risk advisory") || !strings.Contains(output, "digest repeated triaged-without-follow-up posture in recent bounded evidence") {
		t.Fatalf("expected task-level incident risk advisory in status human output, got %q", output)
	}
	followDetailIdx := strings.Index(output, "detail Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.")
	closureIdx := strings.Index(output, "closure advisory")
	taskRiskIdx := strings.Index(output, "task incident risk advisory")
	if followDetailIdx == -1 || closureIdx == -1 || taskRiskIdx == -1 || followDetailIdx > closureIdx || closureIdx > taskRiskIdx {
		t.Fatalf("expected digest/window/detail hierarchy before closure lines, got %q", output)
	}
}

func TestCLIInspectCommandHumanUsesReopenedDigestAndConservativeClosureLanguage(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskInspectResponse{
			TaskID: common.TaskID("tsk_inspect_human"),
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         "/repo",
				Branch:           "main",
				WorkingTreeDirty: false,
			},
			OperatorDecision: &ipc.TaskOperatorDecisionSummary{
				Headline: "Local fresh run ready",
			},
			OperatorExecutionPlan: &ipc.TaskOperatorExecutionPlan{
				PrimaryStep: &ipc.TaskOperatorExecutionStep{
					Action: "START_LOCAL_RUN",
					Status: "REQUIRED_NEXT",
				},
			},
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:          "FOLLOW_UP_REOPENED",
				Digest:         "follow-up reopened",
				WindowAdvisory: "bounded window open=1 reopened=1",
				Advisory:       "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
				ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
					Class:                   "WEAK_CLOSURE_REOPENED",
					Digest:                  "closure reopened after close",
					WindowAdvisory:          "bounded window anchors=2 open=1 closed=1 reopened=1 triaged-without-follow-up=0 repeated=0 behind-latest=0",
					Detail:                  "Recent bounded evidence suggests incident closure remains operationally unresolved.",
					OperationallyUnresolved: true,
					ClosureAppearsWeak:      true,
					ReopenedAfterClosure:    true,
					RecentAnchors: []ipc.TaskContinuityIncidentClosureAnchorItem{
						{
							AnchorTransitionReceiptID: "ctr_close_reopen",
							Class:                     "WEAK_CLOSURE_REOPENED",
							LatestFollowUpReceiptID:   "cifr_close_reopen",
							LatestFollowUpActionKind:  "REOPENED",
							LatestFollowUpAt:          time.Unix(1713000910, 0).UTC(),
							Explanation:               "reopened after closure in recent bounded evidence",
						},
					},
				},
			},
			LatestContinuityIncidentFollowUpReceipt: &ipc.TaskContinuityIncidentFollowUpReceipt{
				ReceiptID:                 "cifr_latest",
				ActionKind:                "REOPENED",
				AnchorTransitionReceiptID: "ctr_latest",
			},
			ContinuityIncidentTaskRisk: &ipc.TaskContinuityIncidentTaskRiskSummary{
				Class:                       "RECURRING_WEAK_CLOSURE",
				Digest:                      "recurring continuity weakness in recent bounded evidence",
				WindowAdvisory:              "bounded incident window anchors=3 open=1 reopened=2 triaged-without-follow-up=0 stagnant=0",
				Detail:                      "Recent bounded evidence suggests recurring weak closure posture across incidents.",
				RecurringWeakClosure:        true,
				ReopenedAfterClosureAnchors: 2,
				RecentAnchorClasses: []string{
					"WEAK_CLOSURE_REOPENED",
					"WEAK_CLOSURE_LOOP",
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"inspect", "--task", "tsk_inspect_human", "--human"}); err != nil {
		t.Fatalf("run inspect --human command: %v", err)
	}
	if captured.Method != ipc.MethodTaskInspect {
		t.Fatalf("expected task.inspect method, got %s", captured.Method)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "digest follow-up reopened") || !strings.Contains(output, "window bounded window open=1 reopened=1") {
		t.Fatalf("expected reopened digest/window vocabulary in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest closure reopened after close") {
		t.Fatalf("expected closure digest parity in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "recent anchors") || !strings.Contains(output, "anchor=ctr_close_ class=weak closure reopened action=reopened") {
		t.Fatalf("expected compact recent closure anchor line in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "task incident risk advisory") || !strings.Contains(output, "digest recurring continuity weakness in recent bounded evidence") {
		t.Fatalf("expected task-level incident risk advisory in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "classes weak closure reopened, weak closure loop") {
		t.Fatalf("expected compact recent task-level class cues in inspect human output, got %q", output)
	}
	if strings.Contains(strings.ToLower(output), "root cause solved") || strings.Contains(strings.ToLower(output), "safe to continue") || strings.Contains(strings.ToLower(output), "incident resolved") {
		t.Fatalf("inspect human follow-up wording must remain conservative, got %q", output)
	}
}

func TestCLIIntentCommandRoutesRequestAndPrintsCompiledIntentSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskIntentResponse{
			TaskID:          common.TaskID("tsk_intent_cli"),
			CurrentIntentID: common.IntentID("int_cli_1"),
			Bounded:         true,
			CompiledIntent: &ipc.TaskCompiledIntentSummary{
				IntentID:                common.IntentID("int_cli_1"),
				Class:                   "IMPLEMENT_CHANGE",
				Posture:                 "PLANNING",
				ExecutionReadiness:      "PLANNING_IN_PROGRESS",
				Objective:               "Prepare intent compiler milestone execution",
				RequestedOutcome:        "produce bounded intent projections across status/inspect/shell",
				ScopeSummary:            "bounded scope signals: internal/orchestrator/service.go, internal/runtime/daemon/service.go",
				ExplicitConstraints:     []string{"Do not redesign Tuku."},
				DoneCriteria:            []string{"done when go test ./... passes"},
				AmbiguityFlags:          []string{"scope_not_explicit"},
				ClarificationQuestions:  []string{"Which file should be implemented first?"},
				RequiresClarification:   true,
				BoundedEvidenceMessages: 7,
				ReadinessReason:         "request remains in planning posture within bounded recent operator input",
				Digest:                  "planning intent posture in bounded recent evidence",
				Advisory:                "Intent remains planning-focused in bounded recent evidence.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"intent", "--task", "tsk_intent_cli"}); err != nil {
		t.Fatalf("run intent command: %v", err)
	}
	if captured.Method != ipc.MethodTaskIntent {
		t.Fatalf("expected task.intent method, got %s", captured.Method)
	}
	var req ipc.TaskIntentRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal task intent request: %v", err)
	}
	if req.TaskID != "tsk_intent_cli" {
		t.Fatalf("unexpected task intent request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "class IMPLEMENT_CHANGE") || !strings.Contains(output, "posture PLANNING") || !strings.Contains(output, "readiness PLANNING_IN_PROGRESS") {
		t.Fatalf("expected intent class/posture/readiness in output, got %q", output)
	}
	if !strings.Contains(output, "digest planning intent posture in bounded recent evidence") || !strings.Contains(output, "advisory Intent remains planning-focused in bounded recent evidence.") {
		t.Fatalf("expected bounded digest/advisory in output, got %q", output)
	}
	if !strings.Contains(output, "requires clarification true") || !strings.Contains(output, "clarification Which file should be implemented first?") {
		t.Fatalf("expected clarification cues in output, got %q", output)
	}
	if strings.Contains(strings.ToLower(output), "task completed") || strings.Contains(strings.ToLower(output), "root cause solved") {
		t.Fatalf("intent command output must remain conservative, got %q", output)
	}
}

func TestCLIBriefCommandRoutesRequestAndPrintsCompiledBriefSummary(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskBriefResponse{
			TaskID:         common.TaskID("tsk_brief_cli"),
			CurrentBriefID: common.BriefID("brf_cli_1"),
			Bounded:        true,
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				BriefID:                 common.BriefID("brf_cli_1"),
				IntentID:                common.IntentID("int_cli_1"),
				Posture:                 "CLARIFICATION_NEEDED",
				Objective:               "Clarify and bound next execution step",
				RequestedOutcome:        "produce execution-ready brief",
				NormalizedAction:        "prepare bounded execution brief",
				ScopeSummary:            "bounded scope not explicitly provided; operator clarification may improve execution targeting",
				Constraints:             []string{"Do not widen authority semantics."},
				DoneCriteria:            []string{"Clarification questions remain explicit."},
				AmbiguityFlags:          []string{"scope_not_explicit"},
				ClarificationQuestions:  []string{"Which files or subsystems are explicitly in scope?"},
				RequiresClarification:   true,
				WorkerFraming:           "Clarification-focused brief: do not fabricate missing requirements; surface unresolved questions before bounded execution.",
				BoundedEvidenceMessages: 5,
				Digest:                  "clarification-needed brief posture in bounded recent evidence",
				Advisory:                "Brief remains clarification-needed in bounded recent evidence; unresolved clarification questions remain explicit.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"brief", "--task", "tsk_brief_cli", "--human"}); err != nil {
		t.Fatalf("run brief command: %v", err)
	}
	if captured.Method != ipc.MethodTaskBrief {
		t.Fatalf("expected task.brief method, got %s", captured.Method)
	}
	var req ipc.TaskBriefRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal task brief request: %v", err)
	}
	if req.TaskID != "tsk_brief_cli" {
		t.Fatalf("unexpected task brief request payload: %+v", req)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	assertSubstringsInOrder(t, output,
		"brief advisory",
		"digest clarification-needed brief posture in bounded recent evidence",
		"window bounded recent messages=5",
		"detail brief remains clarification-needed in bounded recent evidence; unresolved clarification questions remain explicit.",
		"posture clarification_needed | requires clarification true",
	)
	if strings.Contains(output, "task completed") || strings.Contains(output, "fixed") || strings.Contains(output, "safe to continue") {
		t.Fatalf("brief command output must remain conservative, got %q", output)
	}
}

func TestCLIBriefCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskBrief {
			t.Fatalf("expected task.brief method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskBriefResponse{
			TaskID:         common.TaskID("tsk_brief_json"),
			CurrentBriefID: common.BriefID("brf_json_1"),
			Bounded:        true,
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				Posture: "EXECUTION_READY",
				Digest:  "execution-ready brief posture in bounded recent evidence",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"brief", "--task", "tsk_brief_json"}); err != nil {
		t.Fatalf("run brief JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_brief_json" {
		t.Fatalf("unexpected brief JSON task_id: %+v", out)
	}
}

func TestCLIStatusCommandHumanIncludesCompiledIntentAdvisory(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskStatus {
			t.Fatalf("expected task.status method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID:                     common.TaskID("tsk_status_intent_human"),
			Phase:                      "INTERPRETING",
			Status:                     "ACTIVE",
			RequiredNextOperatorAction: "START_LOCAL_RUN",
			CompiledIntent: &ipc.TaskCompiledIntentSummary{
				Class:                   "IMPLEMENT_CHANGE",
				Posture:                 "PLANNING",
				ExecutionReadiness:      "PLANNING_IN_PROGRESS",
				BoundedEvidenceMessages: 4,
				Digest:                  "planning intent posture in bounded recent evidence",
				Advisory:                "Intent remains planning-focused in bounded recent evidence.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"status", "--task", "tsk_status_intent_human", "--human"}); err != nil {
		t.Fatalf("run status --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "intent advisory") || !strings.Contains(output, "digest planning intent posture in bounded recent evidence") {
		t.Fatalf("expected intent digest in status human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded recent messages=4") || !strings.Contains(output, "detail Intent remains planning-focused in bounded recent evidence.") {
		t.Fatalf("expected bounded intent window/detail in status human output, got %q", output)
	}
	if strings.Contains(strings.ToLower(output), "intent is certain") || strings.Contains(strings.ToLower(output), "task completed") {
		t.Fatalf("status human intent cues must remain conservative, got %q", output)
	}
}

func TestCLIStatusCommandHumanIncludesCompiledBriefAdvisory(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskStatus {
			t.Fatalf("expected task.status method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID:                     common.TaskID("tsk_status_brief_human"),
			Phase:                      "INTERPRETING",
			Status:                     "ACTIVE",
			RequiredNextOperatorAction: "START_LOCAL_RUN",
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				Posture:                 "PLANNING_ORIENTED",
				BoundedEvidenceMessages: 4,
				Digest:                  "planning-oriented brief posture in bounded recent evidence",
				Advisory:                "Brief remains planning-oriented in bounded recent evidence.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"status", "--task", "tsk_status_brief_human", "--human"}); err != nil {
		t.Fatalf("run status --human command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "brief advisory") || !strings.Contains(output, "digest planning-oriented brief posture in bounded recent evidence") {
		t.Fatalf("expected brief digest in status human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded recent messages=4") || !strings.Contains(output, "detail brief remains planning-oriented in bounded recent evidence.") {
		t.Fatalf("expected bounded brief window/detail in status human output, got %q", output)
	}
}

func TestCLIBriefCommandHumanIncludesPromptTriageMetrics(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskBrief {
			t.Fatalf("expected task.brief method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskBriefResponse{
			TaskID:         common.TaskID("tsk_brief_triage"),
			CurrentBriefID: common.BriefID("brf_triage"),
			Bounded:        true,
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				Posture:                 "EXECUTION_READY",
				BoundedEvidenceMessages: 3,
				Digest:                  "execution-ready brief posture in bounded recent evidence",
				Advisory:                "Brief is execution-ready within bounded recent evidence.",
				PromptTriage: &brief.PromptTriage{
					Applied:                      true,
					Summary:                      "searched 21 repo-local file(s) and narrowed repair context to 3 ranked candidate(s)",
					SearchTerms:                  []string{"ui", "component", "page"},
					CandidateFiles:               []string{"web/src/pages/Dashboard.tsx", "web/src/components/ProfileCard.tsx"},
					FilesScanned:                 21,
					RawPromptTokenEstimate:       4,
					RewrittenPromptTokenEstimate: 31,
					SearchSpaceTokenEstimate:     620,
					SelectedContextTokenEstimate: 180,
					ContextTokenSavingsEstimate:  440,
				},
				PromptIR: &promptir.Packet{
					NormalizedTaskType: "BUG_FIX",
					RankedTargets: []promptir.Target{
						{Path: "web/src/pages/Dashboard.tsx"},
						{Path: "web/src/components/ProfileCard.tsx"},
					},
					ValidatorPlan:         promptir.ValidatorPlan{Commands: []string{"npm test -- Dashboard"}},
					Confidence:            promptir.ConfidenceScore{Level: "high", Value: 0.82},
					DefaultSerializer:     promptir.SerializerNaturalLanguage,
					NaturalLanguageTokens: 112,
					StructuredTokens:      97,
					StructuredCheaper:     true,
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"brief", "--task", "tsk_brief_triage", "--human"}); err != nil {
		t.Fatalf("run brief --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "prompt sharpened yes | files scanned 21 | candidates 2 | saved context tokens 440") {
		t.Fatalf("expected prompt triage headline in human output, got %q", output)
	}
	if !strings.Contains(output, "search ui, component, page") || !strings.Contains(output, "token estimate raw=4 rewritten=31 search-space=620 selected-context=180 saved=440") {
		t.Fatalf("expected prompt triage details in human output, got %q", output)
	}
	if !strings.Contains(output, "prompt ir yes | targets 2 | validators 1 | confidence high 0.82") {
		t.Fatalf("expected prompt ir headline in human output, got %q", output)
	}
	if !strings.Contains(output, "serializer default=natural_language natural=112 structured=97 structured-cheaper=true") {
		t.Fatalf("expected prompt ir serializer metrics in human output, got %q", output)
	}
	if !strings.Contains(output, "worker routing") || !strings.Contains(output, "recommended Codex") {
		t.Fatalf("expected worker routing guidance in brief human output, got %q", output)
	}
}

func TestCLIPlanCommandHumanIncludesRoutingAndBenchmark(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskStatus {
			t.Fatalf("expected task.status method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID:          common.TaskID("tsk_plan"),
			Phase:           "BRIEF_READY",
			Status:          "ACTIVE",
			LatestRunStatus: run.Status("RUNNING"),
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				Posture:               "EXECUTION_READY",
				RequiresClarification: false,
				PromptIR: &promptir.Packet{
					NormalizedTaskType: "BUG_FIX",
					RankedTargets: []promptir.Target{
						{Path: "web/src/pages/Landing.tsx"},
						{Path: "web/src/components/HeroButton.tsx"},
					},
					ValidatorPlan:         promptir.ValidatorPlan{Commands: []string{"npm test -- landing-page"}},
					Confidence:            promptir.ConfidenceScore{Level: "high", Value: 0.88, Reason: "targets=2 validators=1 ambiguity=0"},
					DefaultSerializer:     promptir.SerializerNaturalLanguage,
					NaturalLanguageTokens: 124,
					StructuredTokens:      101,
					StructuredCheaper:     true,
				},
			},
			CurrentBenchmarkID:                     common.BenchmarkID("bmk_plan"),
			CurrentBenchmarkSource:                 "brief_compiled",
			CurrentBenchmarkSummary:                "ranked 2 targets, planned 1 validator, estimated pre-dispatch savings 260 token(s)",
			CurrentBenchmarkDispatchPromptTokens:   124,
			CurrentBenchmarkStructuredPromptTokens: 101,
			CurrentBenchmarkEstimatedTokenSavings:  260,
			CurrentBenchmarkFilesScanned:           14,
			CurrentBenchmarkRankedTargetCount:      2,
			CurrentBenchmarkConfidenceLevel:        "high",
			CurrentBenchmarkConfidenceValue:        0.88,
			CurrentBenchmarkDefaultSerializer:      "natural_language",
			CurrentBenchmarkStructuredCheaper:      true,
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"plan", "--task", "tsk_plan", "--human"}); err != nil {
		t.Fatalf("run plan --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "plan advisory") || !strings.Contains(output, "recommended Codex") {
		t.Fatalf("expected routing section in plan human output, got %q", output)
	}
	if !strings.Contains(output, "benchmark") || !strings.Contains(output, "saved=260") {
		t.Fatalf("expected benchmark savings in plan human output, got %q", output)
	}
}

func TestCLIBenchmarkCommandHumanIncludesSavingsAndConfidence(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskBenchmark {
			t.Fatalf("expected task.benchmark method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskBenchmarkResponse{
			TaskID: common.TaskID("tsk_benchmark"),
			Benchmark: &benchmark.Run{
				BenchmarkID:                   common.BenchmarkID("bmk_123"),
				Source:                        "brief_compiled",
				Summary:                       "ranked 4 targets and saved 380 tokens",
				RawPromptTokenEstimate:        5,
				DispatchPromptTokenEstimate:   118,
				StructuredPromptTokenEstimate: 99,
				SelectedContextTokenEstimate:  144,
				EstimatedTokenSavings:         380,
				FilesScanned:                  21,
				RankedTargetCount:             4,
				CandidateRecallAt3:            0.67,
				DefaultSerializer:             "natural_language",
				StructuredCheaper:             true,
				ConfidenceLevel:               "high",
				ConfidenceValue:               0.82,
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"benchmark", "--task", "tsk_benchmark", "--human"}); err != nil {
		t.Fatalf("run benchmark --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "tokens raw=5 dispatch=118 structured=99 selected-context=144 saved=380") {
		t.Fatalf("expected benchmark token metrics in human output, got %q", output)
	}
	if !strings.Contains(output, "targeting files-scanned=21 ranked-targets=4 recall@3=0.67") {
		t.Fatalf("expected benchmark targeting metrics in human output, got %q", output)
	}
	if !strings.Contains(output, "serializer natural_language | structured-cheaper=true | confidence high 0.82") {
		t.Fatalf("expected benchmark serializer/confidence metrics in human output, got %q", output)
	}
}

func TestCLIBriefCommandHumanIncludesTaskMemoryMetrics(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskBrief {
			t.Fatalf("expected task.brief method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskBriefResponse{
			TaskID:         common.TaskID("tsk_brief_memory"),
			CurrentBriefID: common.BriefID("brf_memory"),
			Bounded:        true,
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				Posture:                 "EXECUTION_READY",
				BoundedEvidenceMessages: 4,
				Digest:                  "execution-ready brief posture in bounded recent evidence",
				Advisory:                "Brief is execution-ready within bounded recent evidence.",
				MemoryCompression: &brief.MemoryCompression{
					Applied:                   true,
					Summary:                   "phase=planning; action=repair ui bug; files=web/src/App.tsx; next=run validation",
					FullHistoryTokenEstimate:  420,
					ResumePromptTokenEstimate: 120,
					MemoryCompactionRatio:     3.5,
					ConfirmedFactsCount:       4,
					TouchedFilesCount:         1,
					ValidatorsRunCount:        1,
					CandidateFilesCount:       2,
					RejectedHypothesesCount:   1,
					UnknownsCount:             1,
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"brief", "--task", "tsk_brief_memory", "--human"}); err != nil {
		t.Fatalf("run brief --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "task memory yes | history tokens 420 | resume tokens 120 | compaction 3.50x") {
		t.Fatalf("expected task memory headline in human output, got %q", output)
	}
	if !strings.Contains(output, "memory phase=planning; action=repair ui bug; files=web/src/App.tsx; next=run validation") || !strings.Contains(output, "memory tokens history=420 resume=120 compaction=3.50x facts=4 touched=1 validators=1 candidates=2 rejected=1 unknowns=1") {
		t.Fatalf("expected task memory details in human output, got %q", output)
	}
}

func TestCLIStatusCommandHumanIncludesContextPolicyAndValidationDetails(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskStatus {
			t.Fatalf("expected task.status method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID:                         common.TaskID("tsk_status_rich_human"),
			Phase:                          "VALIDATING",
			Status:                         "ACTIVE",
			RequiredNextOperatorAction:     "REVIEW_VALIDATION_STATE",
			CurrentContextPackID:           common.ContextPackID("ctx_123"),
			CurrentContextPackMode:         "standard",
			CurrentContextPackFileCount:    4,
			CurrentContextPackHash:         "hash_ctx",
			LatestPolicyDecisionID:         common.DecisionID("pdec_123"),
			LatestPolicyDecisionStatus:     "APPROVED",
			LatestPolicyDecisionRiskLevel:  "MEDIUM",
			LatestPolicyDecisionReason:     "approved against a dirty worktree because Tuku captured repo anchor and bounded context before execution",
			LatestRunID:                    common.RunID("run_123"),
			LatestRunStatus:                "COMPLETED",
			LatestRunChangedFiles:          []string{"internal/orchestrator/service.go"},
			LatestRunChangedFilesSemantics: "hint: paths became newly dirty compared with pre-run dirty baseline",
			LatestRunRepoDiffSummary:       "git diff relative to HEAD: 1 file(s), +10/-2",
			LatestRunWorktreeSummary:       "worktree dirty: 1 path(s) [modified=1 added=0 deleted=0 renamed=0 untracked=0]",
			LatestRunValidationSignals:     []string{"validation: gofmt reported no formatting drift", "validation: go test passed"},
			LatestRunOutputArtifactRef:     "/tmp/validation.txt",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"status", "--task", "tsk_status_rich_human", "--human"}); err != nil {
		t.Fatalf("run status --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "context-pack ctx_123 | mode standard | files 4") {
		t.Fatalf("expected context pack line in status human output, got %q", output)
	}
	if !strings.Contains(output, "policy pdec_123 | status APPROVED | risk MEDIUM") {
		t.Fatalf("expected policy line in status human output, got %q", output)
	}
	if !strings.Contains(output, "repo diff git diff relative to HEAD: 1 file(s), +10/-2") {
		t.Fatalf("expected repo diff line in status human output, got %q", output)
	}
	if !strings.Contains(output, "artifact /tmp/validation.txt") {
		t.Fatalf("expected validation artifact line in status human output, got %q", output)
	}
}

func TestCLIInspectCommandHumanIncludesCompiledIntentDetails(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskInspect {
			t.Fatalf("expected task.inspect method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskInspectResponse{
			TaskID: common.TaskID("tsk_inspect_intent_human"),
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         "/repo",
				Branch:           "main",
				WorkingTreeDirty: false,
			},
			CompiledIntent: &ipc.TaskCompiledIntentSummary{
				Class:                   "RUN_VALIDATION",
				Posture:                 "VALIDATION_FOCUSED",
				ExecutionReadiness:      "VALIDATION_FOCUSED",
				Objective:               "Validate intent compiler read surface",
				RequestedOutcome:        "confirm bounded projections across status and inspect",
				ScopeSummary:            "bounded scope signals: internal/app/bootstrap.go",
				ExplicitConstraints:     []string{"Do not widen authority semantics."},
				DoneCriteria:            []string{"done when go test ./... passes"},
				ClarificationQuestions:  []string{"Should validation include shell snapshot output too?"},
				RequiresClarification:   true,
				BoundedEvidenceMessages: 3,
				Digest:                  "validation-focused intent posture in bounded recent evidence",
				Advisory:                "Intent is validation-focused in bounded recent evidence.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"inspect", "--task", "tsk_inspect_intent_human", "--human"}); err != nil {
		t.Fatalf("run inspect --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "intent advisory") || !strings.Contains(output, "digest validation-focused intent posture in bounded recent evidence") {
		t.Fatalf("expected intent digest in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "objective Validate intent compiler read surface") || !strings.Contains(output, "scope bounded scope signals: internal/app/bootstrap.go") {
		t.Fatalf("expected intent objective/scope detail in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "clarification Should validation include shell snapshot output too?") {
		t.Fatalf("expected intent clarification question in inspect human output, got %q", output)
	}
}

func TestCLIInspectCommandHumanIncludesCompiledBriefDetails(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskInspect {
			t.Fatalf("expected task.inspect method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskInspectResponse{
			TaskID: common.TaskID("tsk_inspect_brief_human"),
			RepoAnchor: ipc.RepoAnchor{
				RepoRoot:         "/repo",
				Branch:           "main",
				WorkingTreeDirty: false,
			},
			CompiledBrief: &ipc.TaskCompiledBriefSummary{
				Posture:                 "CLARIFICATION_NEEDED",
				Objective:               "Clarify scope before execution",
				RequestedOutcome:        "produce execution-ready brief",
				NormalizedAction:        "prepare bounded execution brief",
				ScopeSummary:            "bounded scope not explicitly provided; operator clarification may improve execution targeting",
				Constraints:             []string{"Do not widen authority semantics."},
				DoneCriteria:            []string{"Clarification questions remain explicit."},
				AmbiguityFlags:          []string{"scope_not_explicit"},
				ClarificationQuestions:  []string{"Which files or subsystems are explicitly in scope?"},
				RequiresClarification:   true,
				WorkerFraming:           "Clarification-focused brief: do not fabricate missing requirements; surface unresolved questions before bounded execution.",
				BoundedEvidenceMessages: 6,
				Digest:                  "clarification-needed brief posture in bounded recent evidence",
				Advisory:                "Brief remains clarification-needed in bounded recent evidence; unresolved clarification questions remain explicit.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"inspect", "--task", "tsk_inspect_brief_human", "--human"}); err != nil {
		t.Fatalf("run inspect --human command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "brief advisory") || !strings.Contains(output, "digest clarification-needed brief posture in bounded recent evidence") {
		t.Fatalf("expected brief digest in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "clarification which files or subsystems are explicitly in scope?") || !strings.Contains(output, "scope bounded scope not explicitly provided; operator clarification may improve execution targeting") {
		t.Fatalf("expected brief scope/clarification detail in inspect human output, got %q", output)
	}
	if strings.Contains(output, "task completed") || strings.Contains(output, "safe to continue") {
		t.Fatalf("inspect human brief cues must remain conservative, got %q", output)
	}
}

func TestCLIInspectCommandHumanIncludesTaskMemoryDetails(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskInspect {
			t.Fatalf("expected task.inspect method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskInspectResponse{
			TaskID: common.TaskID("tsk_inspect_memory"),
			TaskMemory: &taskmemory.Snapshot{
				MemoryID:                  common.MemoryID("mem_123"),
				Source:                    "run_completed",
				Summary:                   "phase=validation; action=repair ui bug; files=web/src/App.tsx; validators=go test; next=review validation",
				FullHistoryTokenEstimate:  510,
				ResumePromptTokenEstimate: 150,
				MemoryCompactionRatio:     3.4,
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"inspect", "--task", "tsk_inspect_memory", "--human"}); err != nil {
		t.Fatalf("run inspect --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "task memory") || !strings.Contains(output, "id mem_123 | source run_completed") {
		t.Fatalf("expected task memory identity lines in inspect human output, got %q", output)
	}
	if !strings.Contains(output, "history tokens 510 | resume tokens 150 | compaction 3.40x") || !strings.Contains(output, "summary phase=validation; action=repair ui bug; files=web/src/App.tsx; validators=go test; next=review validation") {
		t.Fatalf("expected task memory details in inspect human output, got %q", output)
	}
}

func TestCLIStatusCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskStatus {
			t.Fatalf("expected task.status method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID: common.TaskID("tsk_status_json"),
			Phase:  "BRIEF_READY",
			Status: "ACTIVE",
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:  "FOLLOW_UP_CLOSED",
				Digest: "follow-up closed (audit only)",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"status", "--task", "tsk_status_json"}); err != nil {
		t.Fatalf("run status JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_status_json" {
		t.Fatalf("unexpected status JSON task_id: %+v", out)
	}
}

func TestCLIStatusCommandHumanClosedWordingRemainsAuditOnly(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskStatus {
			t.Fatalf("expected task.status method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskStatusResponse{
			TaskID: common.TaskID("tsk_status_closed"),
			Phase:  "BRIEF_READY",
			Status: "ACTIVE",
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:    "FOLLOW_UP_CLOSED",
				Digest:   "follow-up closed (audit only)",
				Advisory: "Follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"status", "--task", "tsk_status_closed", "--human"}); err != nil {
		t.Fatalf("run status --human closed command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "digest follow-up closed (audit only)") {
		t.Fatalf("expected closed audit-only digest in human status output, got %q", output)
	}
	if strings.Contains(output, "resolved") || strings.Contains(output, "safe to continue") || strings.Contains(output, "problem solved") {
		t.Fatalf("closed follow-up human wording must remain conservative, got %q", output)
	}
}

func TestCLIUsageMentionsChat(t *testing.T) {
	if !strings.Contains(cliUsage(), "chat") {
		t.Fatalf("expected cli usage to mention chat, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsTransitionHistory(t *testing.T) {
	if !strings.Contains(cliUsage(), "transition history") {
		t.Fatalf("expected cli usage to mention transition history, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsIncidentCommand(t *testing.T) {
	if !strings.Contains(cliUsage(), "incident --task") {
		t.Fatalf("expected cli usage to mention incident command, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsIncidentTriageHistoryCommand(t *testing.T) {
	if !strings.Contains(cliUsage(), "incident triage history") {
		t.Fatalf("expected cli usage to mention incident triage history command, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsIncidentFollowUpHistoryCommand(t *testing.T) {
	if !strings.Contains(cliUsage(), "incident followup history") {
		t.Fatalf("expected cli usage to mention incident followup history command, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsIncidentClosureCommand(t *testing.T) {
	if !strings.Contains(cliUsage(), "incident closure --task") {
		t.Fatalf("expected cli usage to mention incident closure command, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsIncidentRiskCommand(t *testing.T) {
	if !strings.Contains(cliUsage(), "incident risk --task") {
		t.Fatalf("expected cli usage to mention incident risk command, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsStatusAndInspectHumanModes(t *testing.T) {
	if !strings.Contains(cliUsage(), "tuku status --task <TASK_ID> [--human]") {
		t.Fatalf("expected cli usage to mention status human mode, got %q", cliUsage())
	}
	if !strings.Contains(cliUsage(), "tuku inspect --task <TASK_ID> [--human]") {
		t.Fatalf("expected cli usage to mention inspect human mode, got %q", cliUsage())
	}
	if !strings.Contains(cliUsage(), "tuku next --task <TASK_ID> [--human]") {
		t.Fatalf("expected cli usage to mention next human mode, got %q", cliUsage())
	}
}

func TestCLIUsageMentionsRecovery(t *testing.T) {
	if !strings.Contains(cliUsage(), "recovery") {
		t.Fatalf("expected cli usage to mention recovery, got %q", cliUsage())
	}
}

func TestParseRecoveryActionKind(t *testing.T) {
	kind, err := parseRecoveryActionKind("decision-regenerate-brief")
	if err != nil {
		t.Fatalf("parse recovery action kind: %v", err)
	}
	if kind != "DECISION_REGENERATE_BRIEF" {
		t.Fatalf("expected DECISION_REGENERATE_BRIEF, got %s", kind)
	}
}

func TestParseHandoffFollowThroughKind(t *testing.T) {
	kind, err := parseHandoffFollowThroughKind("proof-of-life-observed")
	if err != nil {
		t.Fatalf("parse handoff follow-through kind: %v", err)
	}
	if kind != handoff.FollowThroughProofOfLifeObserved {
		t.Fatalf("expected %s, got %s", handoff.FollowThroughProofOfLifeObserved, kind)
	}
}

func TestParseHandoffResolutionKind(t *testing.T) {
	kind, err := parseHandoffResolutionKind("superseded-by-local")
	if err != nil {
		t.Fatalf("parse handoff resolution kind: %v", err)
	}
	if kind != handoff.ResolutionSupersededByLocal {
		t.Fatalf("expected %s, got %s", handoff.ResolutionSupersededByLocal, kind)
	}
}

func TestCLIRecoveryRecordCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_123"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_123", Kind: "FAILED_RUN_REVIEWED"},
			RecoveryClass:         "DECISION_REQUIRED",
			RecommendedAction:     "MAKE_RESUME_DECISION",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "failed run reviewed; choose next step",
			CanonicalResponse:     "recovery action recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "record",
		"--task", "tsk_123",
		"--action", "failed-run-reviewed",
		"--summary", "reviewed failed run",
		"--note", "operator reviewed logs",
	}); err != nil {
		t.Fatalf("run recovery command: %v", err)
	}
	if captured.Method != ipc.MethodRecordRecoveryAction {
		t.Fatalf("expected recovery record method, got %s", captured.Method)
	}
	var req ipc.TaskRecordRecoveryActionRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal recovery record request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.Kind != "FAILED_RUN_REVIEWED" {
		t.Fatalf("unexpected recovery record request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator reviewed logs" {
		t.Fatalf("unexpected recovery record notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryRecordCommandRejectsUnsupportedAction(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "not-a-real-action"})
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery action") {
		t.Fatalf("expected unsupported recovery action error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported recovery action")
	}
}

func TestCLIRecoveryRecordCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [RECOVERY_ACTION_FAILED]: continue decision can only be recorded while recovery class is DECISION_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "record", "--task", "tsk_123", "--action", "decision-continue"})
	if err == nil || !strings.Contains(err.Error(), "DECISION_REQUIRED") {
		t.Fatalf("expected daemon rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryRebriefCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRebriefResponse{
			TaskID:                common.TaskID("tsk_456"),
			PreviousBriefID:       common.BriefID("brf_old"),
			BriefID:               common.BriefID("brf_new"),
			BriefHash:             "hash_new",
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "execution brief was regenerated after operator decision",
			CanonicalResponse:     "rebrief executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"recovery", "rebrief", "--task", "tsk_456"}); err != nil {
		t.Fatalf("run recovery rebrief command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteRebrief {
		t.Fatalf("expected rebrief method, got %s", captured.Method)
	}
	var req ipc.TaskRebriefRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal rebrief request: %v", err)
	}
	if req.TaskID != "tsk_456" {
		t.Fatalf("unexpected rebrief request: %+v", req)
	}
}

func TestCLIRecoveryRebriefCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [REBRIEF_FAILED]: rebrief can only be executed while recovery class is REBRIEF_REQUIRED")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "rebrief", "--task", "tsk_456"})
	if err == nil || !strings.Contains(err.Error(), "REBRIEF_REQUIRED") {
		t.Fatalf("expected daemon rebrief rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryResumeInterruptedCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskInterruptedResumeResponse{
			TaskID:                common.TaskID("tsk_resume_interrupt"),
			BriefID:               common.BriefID("brf_current"),
			BriefHash:             "hash_current",
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_interrupt_resume_1", Kind: string(recoveryaction.KindInterruptedResumeExecuted), Summary: "operator resumed interrupted lineage"},
			RecoveryClass:         "CONTINUE_EXECUTION_REQUIRED",
			RecommendedAction:     "EXECUTE_CONTINUE_RECOVERY",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "operator explicitly resumed interrupted execution lineage; continue recovery still must be executed before the next bounded run",
			CanonicalResponse:     "interrupted resume executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "resume-interrupted",
		"--task", "tsk_resume_interrupt",
		"--summary", "operator resumed interrupted lineage",
		"--note", "maintain interrupted lineage semantics",
	}); err != nil {
		t.Fatalf("run recovery resume-interrupted command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteInterruptedResume {
		t.Fatalf("expected interrupted-resume method, got %s", captured.Method)
	}
	var req ipc.TaskInterruptedResumeRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal interrupted resume request: %v", err)
	}
	if req.TaskID != "tsk_resume_interrupt" || req.Summary != "operator resumed interrupted lineage" {
		t.Fatalf("unexpected interrupted resume request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "maintain interrupted lineage semantics" {
		t.Fatalf("unexpected interrupted resume notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryResumeInterruptedCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [INTERRUPTED_RESUME_FAILED]: interrupted resume can only be executed while recovery class is INTERRUPTED_RUN_RECOVERABLE and recommended action is RESUME_INTERRUPTED_RUN")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "resume-interrupted", "--task", "tsk_resume_interrupt"})
	if err == nil || !strings.Contains(err.Error(), "INTERRUPTED_RUN_RECOVERABLE") {
		t.Fatalf("expected daemon interrupted-resume rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryContinueCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskContinueRecoveryResponse{
			TaskID:                common.TaskID("tsk_789"),
			BriefID:               common.BriefID("brf_current"),
			BriefHash:             "hash_current",
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_continue_1", TaskID: common.TaskID("tsk_789"), Kind: string(recoveryaction.KindContinueExecuted), Summary: "operator confirmed current brief"},
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
			CanonicalResponse:     "continue recovery executed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_789"}); err != nil {
		t.Fatalf("run recovery continue command: %v", err)
	}
	if captured.Method != ipc.MethodExecuteContinueRecovery {
		t.Fatalf("expected continue-recovery method, got %s", captured.Method)
	}
	var req ipc.TaskContinueRecoveryRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal continue recovery request: %v", err)
	}
	if req.TaskID != "tsk_789" {
		t.Fatalf("unexpected continue recovery request: %+v", req)
	}
}

func TestCLIRecoveryContinueCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodExecuteContinueRecovery:
			payload, _ := json.Marshal(ipc.TaskContinueRecoveryResponse{
				TaskID:                common.TaskID("tsk_continue_human"),
				BriefID:               common.BriefID("brf_current"),
				BriefHash:             "hash_current",
				Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_continue_human", TaskID: common.TaskID("tsk_continue_human"), Kind: string(recoveryaction.KindContinueExecuted), Summary: "operator confirmed current brief"},
				RecoveryClass:         "READY_NEXT_RUN",
				RecommendedAction:     "START_NEXT_RUN",
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "operator explicitly confirmed the current brief for the next bounded run",
				CanonicalResponse:     "continue recovery executed",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_continue_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "FOLLOW_UP_OPEN",
					Digest:         "follow-up open",
					WindowAdvisory: "bounded window open=1 reopened=0 triaged-without-follow-up=0",
					Advisory:       "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "OPERATIONALLY_UNRESOLVED",
						Digest:         "operationally unresolved",
						WindowAdvisory: "bounded closure window anchors=1 open=1",
						Detail:         "follow-up progression remains stagnant in recent bounded evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_continue_human", "--human"}); err != nil {
		t.Fatalf("run recovery continue --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "executed recovery continue") {
		t.Fatalf("expected executed line in recovery human output, got %q", output)
	}
	if !strings.Contains(output, "digest follow-up open") {
		t.Fatalf("expected digest line in recovery human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window open=1 reopened=0 triaged-without-follow-up=0") {
		t.Fatalf("expected bounded-window cue in recovery human output, got %q", output)
	}
	if !strings.Contains(output, "detail Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.") {
		t.Fatalf("expected detail advisory in recovery human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest operationally unresolved") {
		t.Fatalf("expected closure advisory digest in recovery human output, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest follow-up open",
		"window bounded window open=1 reopened=0 triaged-without-follow-up=0",
		"detail Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
		"closure advisory",
		"digest operationally unresolved",
		"window bounded closure window anchors=1 open=1",
		"detail follow-up progression remains stagnant in recent bounded evidence.",
		"result operator confirmed current brief",
	)
}

func TestCLIHandoffFollowThroughCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskHandoffFollowThroughRecordResponse{
			TaskID: common.TaskID("tsk_follow"),
			Record: &handoff.FollowThrough{
				Version:         1,
				RecordID:        "hft_1",
				HandoffID:       "hnd_1",
				LaunchAttemptID: "hlc_1",
				LaunchID:        "launch_1",
				TaskID:          common.TaskID("tsk_follow"),
				TargetWorker:    "claude",
				Kind:            handoff.FollowThroughProofOfLifeObserved,
				Summary:         "later Claude proof of life observed",
				CreatedAt:       time.Unix(1710000200, 0).UTC(),
			},
			RecoveryClass:         "HANDOFF_LAUNCH_COMPLETED",
			RecommendedAction:     "MONITOR_LAUNCHED_HANDOFF",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "launched handoff remains monitor-only",
			CanonicalResponse:     "follow-through recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"handoff-followthrough",
		"--task", "tsk_follow",
		"--kind", "proof-of-life-observed",
		"--summary", "later Claude proof of life observed",
		"--note", "operator confirmed downstream ping",
	}); err != nil {
		t.Fatalf("run handoff-followthrough command: %v", err)
	}
	if captured.Method != ipc.MethodRecordHandoffFollowThrough {
		t.Fatalf("expected handoff follow-through method, got %s", captured.Method)
	}
	var req ipc.TaskHandoffFollowThroughRecordRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal handoff follow-through request: %v", err)
	}
	if req.TaskID != "tsk_follow" || req.Kind != string(handoff.FollowThroughProofOfLifeObserved) {
		t.Fatalf("unexpected handoff follow-through request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "operator confirmed downstream ping" {
		t.Fatalf("unexpected handoff follow-through notes: %+v", req.Notes)
	}
}

func TestCLIHandoffFollowThroughCommandRejectsUnsupportedKind(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-followthrough", "--task", "tsk_follow", "--kind", "not-a-real-kind"})
	if err == nil || !strings.Contains(err.Error(), "unsupported handoff follow-through kind") {
		t.Fatalf("expected unsupported follow-through kind error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported handoff follow-through kind")
	}
}

func TestCLIHandoffFollowThroughCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [HANDOFF_FOLLOW_THROUGH_FAILED]: handoff follow-through kind PROOF_OF_LIFE_OBSERVED can only be recorded while handoff continuity state is a launched Claude follow-through posture")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-followthrough", "--task", "tsk_follow", "--kind", "proof-of-life-observed"})
	if err == nil || !strings.Contains(err.Error(), "launched Claude follow-through posture") {
		t.Fatalf("expected daemon follow-through rejection to surface, got %v", err)
	}
}

func TestCLIHandoffResolveCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordResponse{
			TaskID: common.TaskID("tsk_resolve"),
			Record: &handoff.Resolution{
				Version:         1,
				ResolutionID:    "hrs_1",
				HandoffID:       "hnd_1",
				LaunchAttemptID: "hlc_1",
				LaunchID:        "launch_1",
				TaskID:          common.TaskID("tsk_resolve"),
				TargetWorker:    "claude",
				Kind:            handoff.ResolutionSupersededByLocal,
				Summary:         "operator returned local control",
				CreatedAt:       time.Unix(1710000600, 0).UTC(),
			},
			RecoveryClass:         "READY_NEXT_RUN",
			RecommendedAction:     "START_NEXT_RUN",
			ReadyForNextRun:       true,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "resolved handoff no longer blocks local mutation",
			CanonicalResponse:     "handoff resolution recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"handoff-resolve",
		"--task", "tsk_resolve",
		"--kind", "superseded-by-local",
		"--summary", "operator returned local control",
		"--note", "close Claude branch",
	}); err != nil {
		t.Fatalf("run handoff-resolve command: %v", err)
	}
	if captured.Method != ipc.MethodRecordHandoffResolution {
		t.Fatalf("expected handoff resolution method, got %s", captured.Method)
	}
	var req ipc.TaskHandoffResolutionRecordRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal handoff resolution request: %v", err)
	}
	if req.TaskID != "tsk_resolve" || req.Kind != string(handoff.ResolutionSupersededByLocal) {
		t.Fatalf("unexpected handoff resolution request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "close Claude branch" {
		t.Fatalf("unexpected handoff resolution notes: %+v", req.Notes)
	}
}

func TestCLIHandoffResolveCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodRecordHandoffResolution:
			payload, _ := json.Marshal(ipc.TaskHandoffResolutionRecordResponse{
				TaskID: common.TaskID("tsk_resolve_human"),
				Record: &handoff.Resolution{
					Version:         1,
					ResolutionID:    "hrs_human",
					HandoffID:       "hnd_human",
					LaunchAttemptID: "hlc_human",
					LaunchID:        "launch_human",
					TaskID:          common.TaskID("tsk_resolve_human"),
					TargetWorker:    "claude",
					Kind:            handoff.ResolutionSupersededByLocal,
					Summary:         "operator returned local control",
					CreatedAt:       time.Unix(1710000600, 0).UTC(),
				},
				RecoveryClass:         "READY_NEXT_RUN",
				RecommendedAction:     "START_NEXT_RUN",
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "resolved handoff no longer blocks local mutation",
				CanonicalResponse:     "handoff resolution recorded",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_resolve_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "FOLLOW_UP_CLOSED",
					Digest:         "follow-up closed (audit only)",
					WindowAdvisory: "bounded window open=0 reopened=0 triaged-without-follow-up=0",
					Advisory:       "Follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "STABLE_BOUNDED",
						Digest:         "stable bounded closure progression",
						WindowAdvisory: "bounded closure window anchors=1 closed=1",
						Detail:         "stable within bounded recent evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"handoff-resolve", "--task", "tsk_resolve_human", "--kind", "superseded-by-local", "--human"}); err != nil {
		t.Fatalf("run handoff-resolve --human command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "executed handoff resolve") {
		t.Fatalf("expected executed line in handoff-resolve human output, got %q", output)
	}
	if !strings.Contains(output, "digest follow-up closed (audit only)") {
		t.Fatalf("expected audit-only digest line in handoff-resolve human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest stable bounded closure progression") {
		t.Fatalf("expected closure advisory digest in handoff-resolve human output, got %q", output)
	}
	if strings.Contains(output, "safe to continue") || strings.Contains(output, "completed successfully") || strings.Contains(output, "problem solved") {
		t.Fatalf("handoff-resolve human wording must remain conservative, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest follow-up closed (audit only)",
		"window bounded window open=0 reopened=0 triaged-without-follow-up=0",
		"detail follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
		"closure advisory",
		"digest stable bounded closure progression",
		"window bounded closure window anchors=1 closed=1",
		"detail stable within bounded recent evidence.",
		"result operator returned local control",
	)
}

func TestCLIHandoffResolveCommandRejectsUnsupportedKind(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	called := false
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		called = true
		return ipc.Response{}, nil
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-resolve", "--task", "tsk_resolve", "--kind", "not-a-real-kind"})
	if err == nil || !strings.Contains(err.Error(), "unsupported handoff resolution kind") {
		t.Fatalf("expected unsupported handoff resolution kind error, got %v", err)
	}
	if called {
		t.Fatal("ipc should not be called for unsupported handoff resolution kind")
	}
}

func TestCLIHandoffResolveCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [HANDOFF_RESOLUTION_FAILED]: no active Claude handoff branch exists")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"handoff-resolve", "--task", "tsk_resolve", "--kind", "abandoned"})
	if err == nil || !strings.Contains(err.Error(), "no active Claude handoff branch") {
		t.Fatalf("expected daemon handoff resolution rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryContinueCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [CONTINUE_RECOVERY_FAILED]: continue recovery can only be executed while recovery class is CONTINUE_EXECUTION_REQUIRED and latest action is DECISION_CONTINUE")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "continue", "--task", "tsk_789"})
	if err == nil || !strings.Contains(err.Error(), "CONTINUE_EXECUTION_REQUIRED") {
		t.Fatalf("expected daemon continue-recovery rejection to surface, got %v", err)
	}
}

func TestCLIRecoveryReviewInterruptedCommandRoutesRequest(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskRecordRecoveryActionResponse{
			TaskID:                common.TaskID("tsk_interrupt"),
			Action:                ipc.TaskRecoveryActionRecord{ActionID: "ract_interrupt_1", Kind: string(recoveryaction.KindInterruptedRunReviewed)},
			RecoveryClass:         "INTERRUPTED_RUN_RECOVERABLE",
			RecommendedAction:     "RESUME_INTERRUPTED_RUN",
			ReadyForNextRun:       false,
			ReadyForHandoffLaunch: false,
			RecoveryReason:        "interrupted execution lineage was reviewed and remains recoverable",
			CanonicalResponse:     "interrupted run reviewed",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"recovery", "review-interrupted",
		"--task", "tsk_interrupt",
		"--summary", "interrupted lineage reviewed",
		"--note", "preserve interrupted lineage",
	}); err != nil {
		t.Fatalf("run recovery review-interrupted command: %v", err)
	}
	if captured.Method != ipc.MethodReviewInterruptedRun {
		t.Fatalf("expected review-interrupted method, got %s", captured.Method)
	}
	var req ipc.TaskReviewInterruptedRunRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal review-interrupted request: %v", err)
	}
	if req.TaskID != "tsk_interrupt" || req.Summary != "interrupted lineage reviewed" {
		t.Fatalf("unexpected review-interrupted request: %+v", req)
	}
	if len(req.Notes) != 1 || req.Notes[0] != "preserve interrupted lineage" {
		t.Fatalf("unexpected review-interrupted notes: %+v", req.Notes)
	}
}

func TestCLIRecoveryReviewInterruptedCommandReturnsDaemonError(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [INTERRUPTED_REVIEW_FAILED]: interrupted-run review can only be recorded while recovery class is INTERRUPTED_RUN_RECOVERABLE")
	}

	app := NewCLIApplication()
	err := app.Run(context.Background(), []string{"recovery", "review-interrupted", "--task", "tsk_interrupt"})
	if err == nil || !strings.Contains(err.Error(), "INTERRUPTED_RUN_RECOVERABLE") {
		t.Fatalf("expected daemon interrupted-review rejection to surface, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapStartsDaemonOnUnavailable(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	var calls int
	var launched int
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		calls++
		if calls == 1 || calls == 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		return mustResolveShellTaskResponse(t, "tsk_bootstrap"), nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		launched++
		ch := make(chan error)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	resolution, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err != nil {
		t.Fatalf("resolve shell task with bootstrap: %v", err)
	}
	if resolution.TaskID != common.TaskID("tsk_bootstrap") {
		t.Fatalf("expected task id tsk_bootstrap, got %s", resolution.TaskID)
	}
	if launched != 1 {
		t.Fatalf("expected daemon to be launched once, got %d", launched)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapDoesNotStartDaemonOnUnexpectedError(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, errors.New("daemon error [BAD_PAYLOAD]: broken request")
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be launched for unexpected IPC errors")
		return nil, nil
	}

	if _, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository"); err == nil {
		t.Fatal("expected unexpected IPC error to be returned")
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsStartupFailure(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		return nil, errors.New("launch failed")
	}

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "could not start the local Tuku daemon automatically") {
		t.Fatalf("expected daemon startup failure, got %v", err)
	}
}

func TestResolveShellTaskForRepoWithDaemonBootstrapReturnsProcessExit(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
	}()

	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		return ipc.Response{}, daemonUnavailableErr()
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error, 1)
		ch <- errors.New("exit status 1")
		close(ch)
		return ch, nil
	}
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0

	_, err := resolveShellTaskForRepoWithDaemonBootstrap(context.Background(), "/tmp/tukud.sock", "/tmp/repo", "Continue work in this repository")
	if err == nil || !strings.Contains(err.Error(), "local Tuku daemon failed to start") {
		t.Fatalf("expected daemon process exit failure, got %v", err)
	}
}

func TestRunPrimaryEntryStartsDaemonAndOpensShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	origTimeout := daemonReadyTimeout
	origInterval := daemonRetryInterval
	origLoadWorker := loadPrimaryWorkerPref
	origSaveWorker := savePrimaryWorkerPref
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
		daemonReadyTimeout = origTimeout
		daemonRetryInterval = origInterval
		loadPrimaryWorkerPref = origLoadWorker
		savePrimaryWorkerPref = origSaveWorker
	}()

	getWorkingDir = func() (string, error) { return "/tmp/repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, dir string) (string, error) { return dir, nil }
	daemonReadyTimeout = 50 * time.Millisecond
	daemonRetryInterval = 0
	loadPrimaryWorkerPref = func() (tukushell.WorkerPreference, error) { return tukushell.WorkerPreferenceAuto, nil }
	savePrimaryWorkerPref = func(preference tukushell.WorkerPreference) error { return nil }

	var calls int
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		calls++
		if calls <= 2 {
			return ipc.Response{}, daemonUnavailableErr()
		}
		switch req.Method {
		case ipc.MethodResolveShellTaskForRepo:
			return mustResolveShellTaskResponse(t, "tsk_primary"), nil
		case ipc.MethodTaskShellSnapshot:
			return mustShellSnapshotResponse(t, "tsk_primary"), nil
		default:
			t.Fatalf("expected resolve/shell snapshot request, got %s", req.Method)
			return ipc.Response{}, nil
		}
	}
	startLocalDaemon = func() (<-chan error, error) {
		ch := make(chan error)
		return ch, nil
	}

	var openedTaskID string
	app := &CLIApplication{
		chooseWorkerFn: func(_ context.Context, _ primaryWorkerSelectionContext) (tukushell.WorkerPreference, error) {
			return tukushell.WorkerPreferenceCodex, nil
		},
		openShellFn: func(_ context.Context, _ string, taskID string, _ tukushell.WorkerPreference) error {
			openedTaskID = taskID
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry: %v", err)
	}
	if openedTaskID != "tsk_primary" {
		t.Fatalf("expected shell to open task tsk_primary, got %q", openedTaskID)
	}
}

func TestRunPrimaryEntryOutsideRepoOpensFallbackShell(t *testing.T) {
	origCall := ipcCall
	origStart := startLocalDaemon
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		ipcCall = origCall
		startLocalDaemon = origStart
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/no-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}
	ipcCall = func(_ context.Context, _ string, _ ipc.Request) (ipc.Response, error) {
		t.Fatal("daemon IPC should not be used outside repo fallback mode")
		return ipc.Response{}, nil
	}
	startLocalDaemon = func() (<-chan error, error) {
		t.Fatal("daemon should not be auto-started outside repo fallback mode")
		return nil, nil
	}

	var fallbackCWD string
	app := &CLIApplication{
		openFallbackShellFn: func(_ context.Context, cwd string, _ tukushell.WorkerPreference) error {
			fallbackCWD = cwd
			return nil
		},
		openShellFn: func(_ context.Context, _ string, _ string, _ tukushell.WorkerPreference) error {
			t.Fatal("task-backed shell should not open outside repo fallback mode")
			return nil
		},
	}
	if err := app.runPrimaryEntry(context.Background(), "/tmp/tukud.sock", nil); err != nil {
		t.Fatalf("run primary entry outside repo: %v", err)
	}
	if fallbackCWD != "/tmp/no-repo" {
		t.Fatalf("expected fallback cwd /tmp/no-repo, got %q", fallbackCWD)
	}
}

func TestResolveCurrentRepoRootReturnsPrimaryEntryMessage(t *testing.T) {
	origGetwd := getWorkingDir
	origResolveRepo := resolveRepoRootFromDir
	defer func() {
		getWorkingDir = origGetwd
		resolveRepoRootFromDir = origResolveRepo
	}()

	getWorkingDir = func() (string, error) { return "/tmp/not-repo", nil }
	resolveRepoRootFromDir = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("git repo root not found")
	}

	_, err := resolveCurrentRepoRoot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "tuku needs a git repository for the primary entry path") {
		t.Fatalf("expected primary-entry repo error, got %v", err)
	}
}

func TestPrimaryEntryScratchSnapshotExplainsNoRepoMode(t *testing.T) {
	snapshot := primaryEntryScratchSnapshot("/tmp/no-repo")
	if snapshot.Status != "LOCAL_ONLY" || snapshot.Phase != "SCRATCH_INTAKE" {
		t.Fatalf("expected scratch intake snapshot, got %+v", snapshot)
	}
	if snapshot.Repo.RepoRoot != "" {
		t.Fatalf("expected no repo anchor in scratch mode, got %+v", snapshot.Repo)
	}
	if snapshot.IntentClass != "scratch" {
		t.Fatalf("expected scratch intent class, got %q", snapshot.IntentClass)
	}
	if !strings.Contains(snapshot.LatestCanonicalResponse, "local scratch and intake session") {
		t.Fatalf("expected scratch explanation, got %q", snapshot.LatestCanonicalResponse)
	}
	if !strings.Contains(snapshot.IntentSummary, "/tmp/no-repo") {
		t.Fatalf("expected cwd in scratch intent summary, got %q", snapshot.IntentSummary)
	}
	if len(snapshot.RecentConversation) < 3 {
		t.Fatal("expected scratch intake guidance conversation")
	}
}

func TestLoadPrimaryRepoScratchBridgeLoadsExactRepoScratchNotes(t *testing.T) {
	origResolveScratchPath := resolveScratchPath
	defer func() {
		resolveScratchPath = origResolveScratchPath
	}()

	path := filepath.Join(t.TempDir(), "scratch.json")
	resolveScratchPath = func(string) (string, error) {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "kind": "local_scratch_intake",
  "cwd": "/tmp/repo",
  "created_at": "2026-03-19T00:00:00Z",
  "updated_at": "2026-03-19T00:00:00Z",
  "notes": [
    {"role": "user", "body": "Draft the first milestone list", "created_at": "2026-03-19T00:00:00Z"}
  ]
}`), 0o644); err != nil {
		t.Fatalf("write scratch file: %v", err)
	}

	bridge, err := loadPrimaryRepoScratchBridge("/tmp/repo")
	if err != nil {
		t.Fatalf("load primary repo scratch bridge: %v", err)
	}
	if bridge == nil || len(bridge.Notes) != 1 {
		t.Fatalf("expected one bridged scratch note, got %+v", bridge)
	}
	if bridge.Notes[0].Body != "Draft the first milestone list" {
		t.Fatalf("expected bridged note body, got %+v", bridge.Notes[0])
	}
}

func TestApplyPrimaryRepoScratchBridgeAppendsExplicitLocalOnlyMessages(t *testing.T) {
	snapshot := applyPrimaryRepoScratchBridge(tukushell.Snapshot{
		TaskID:                  "tsk_repo",
		Phase:                   "INTAKE",
		Status:                  "ACTIVE",
		LatestCanonicalResponse: "Canonical repo-backed response.",
		RecentConversation: []tukushell.ConversationItem{
			{Role: "system", Body: "Repo-backed task created."},
		},
	}, &primaryRepoScratchBridge{
		RepoRoot: "/tmp/repo",
		Notes: []tukushell.ConversationItem{
			{Role: "user", Body: "Plan project structure"},
			{Role: "user", Body: "List initial requirements"},
		},
	})

	if snapshot.LatestCanonicalResponse != "Canonical repo-backed response." {
		t.Fatalf("expected canonical response to remain unchanged, got %q", snapshot.LatestCanonicalResponse)
	}
	if snapshot.LocalScratch == nil || len(snapshot.LocalScratch.Notes) != 2 {
		t.Fatalf("expected surfaced local scratch context, got %+v", snapshot.LocalScratch)
	}
	all := make([]string, 0, len(snapshot.RecentConversation))
	for _, msg := range snapshot.RecentConversation {
		all = append(all, msg.Body)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "have not been imported into canonical task state") {
		t.Fatalf("expected explicit local-only boundary, got %q", joined)
	}
	if !strings.Contains(joined, "Sending that pending message is the explicit adoption step") {
		t.Fatalf("expected explicit adoption step, got %q", joined)
	}
	if !strings.Contains(joined, "Shell commands: stage local scratch with `a`") {
		t.Fatalf("expected shell-local adoption command copy, got %q", joined)
	}
	if !strings.Contains(joined, "local scratch note: Plan project structure") {
		t.Fatalf("expected bridged scratch note, got %q", joined)
	}
}

func mustResolveShellTaskResponse(t *testing.T, taskID common.TaskID) ipc.Response {
	t.Helper()
	payload, err := json.Marshal(ipc.ResolveShellTaskForRepoResponse{
		TaskID:   taskID,
		RepoRoot: "/tmp/repo",
		Created:  false,
	})
	if err != nil {
		t.Fatalf("marshal resolve shell task response: %v", err)
	}
	return ipc.Response{OK: true, Payload: payload}
}

func mustShellSnapshotResponse(t *testing.T, taskID common.TaskID) ipc.Response {
	t.Helper()
	payload, err := json.Marshal(ipc.TaskShellSnapshotResponse{
		TaskID: taskID,
		Goal:   "fix the ui bug",
		Phase:  "BRIEF_READY",
		Status: "ACTIVE",
		Brief: &ipc.TaskShellBrief{
			Posture:               "EXECUTION_READY",
			RequiresClarification: false,
			PromptTargets:         []string{"web/src/App.tsx", "web/src/components/Button.tsx"},
			ValidatorCommands:     []string{"npm test -- landing-page"},
			ConfidenceLevel:       "high",
			EstimatedTokenSavings: 220,
		},
		CompiledIntent: &ipc.TaskCompiledIntentSummary{
			Class: "BUG_FIX",
		},
	})
	if err != nil {
		t.Fatalf("marshal shell snapshot response: %v", err)
	}
	return ipc.Response{OK: true, Payload: payload}
}

func daemonUnavailableErr() error {
	return &net.OpError{Op: "dial", Net: "unix", Err: syscall.ECONNREFUSED}
}

type capturedStdout struct {
	previous *os.File
	reader   *os.File
	writer   *os.File
	buffer   bytes.Buffer
}

func captureCLIStdout(t *testing.T) *capturedStdout {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	captured := &capturedStdout{
		previous: os.Stdout,
		reader:   reader,
		writer:   writer,
	}
	os.Stdout = writer
	return captured
}

func (c *capturedStdout) restore() {
	if c == nil {
		return
	}
	if c.previous != nil {
		os.Stdout = c.previous
	}
	if c.writer != nil {
		_ = c.writer.Close()
	}
	if c.reader != nil {
		_, _ = c.buffer.ReadFrom(c.reader)
		_ = c.reader.Close()
	}
}

func assertSubstringsInOrder(t *testing.T, body string, parts ...string) {
	t.Helper()
	pos := 0
	for _, part := range parts {
		idx := strings.Index(body[pos:], part)
		if idx < 0 {
			t.Fatalf("expected substring %q after offset %d in output %q", part, pos, body)
		}
		pos += idx + len(part)
	}
}

func TestCLINextCommandRoutesUnifiedPrimaryExecution(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_123"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_123",
				TaskID:       common.TaskID("tsk_123"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_123",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{"next", "--task", "tsk_123"}); err != nil {
		t.Fatalf("run next command: %v", err)
	}
	if captured.Method != ipc.MethodExecutePrimaryOperatorStep {
		t.Fatalf("expected unified primary-step method, got %s", captured.Method)
	}
	var req ipc.TaskExecutePrimaryOperatorStepRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal next request: %v", err)
	}
	if req.TaskID != "tsk_123" {
		t.Fatalf("unexpected next request: %+v", req)
	}
}

func TestCLINextCommandRoutesReviewGapAcknowledgmentFlags(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_123"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_456",
				TaskID:       common.TaskID("tsk_123"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_456",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	defer stdout.restore()
	if err := app.Run(context.Background(), []string{
		"next",
		"--task", "tsk_123",
		"--ack-review-gap",
		"--ack-session", "shs_123",
		"--ack-kind", "stale_review",
		"--ack-summary", "proceed with explicit stale-evidence awareness",
	}); err != nil {
		t.Fatalf("run next command with review-gap flags: %v", err)
	}
	if captured.Method != ipc.MethodExecutePrimaryOperatorStep {
		t.Fatalf("expected unified primary-step method, got %s", captured.Method)
	}
	var req ipc.TaskExecutePrimaryOperatorStepRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal next request: %v", err)
	}
	if req.TaskID != "tsk_123" || !req.AcknowledgeReviewGap || req.ReviewGapSessionID != "shs_123" || req.ReviewGapAcknowledgmentKind != "stale_review" {
		t.Fatalf("unexpected next request with acknowledgment flags: %+v", req)
	}
}

func TestCLINextCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodExecutePrimaryOperatorStep {
			t.Fatalf("expected unified primary-step method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_next_human"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_next_human",
				TaskID:       common.TaskID("tsk_next_human"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_next_human",
			},
			OperatorDecision: &ipc.TaskOperatorDecisionSummary{
				Headline: "Local fresh run ready",
			},
			OperatorExecutionPlan: &ipc.TaskOperatorExecutionPlan{
				PrimaryStep: &ipc.TaskOperatorExecutionStep{
					Action: "START_LOCAL_RUN",
					Status: "REQUIRED_NEXT",
				},
			},
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:          "TRIAGED_CURRENT",
				Digest:         "triaged without follow-up",
				WindowAdvisory: "bounded window triaged-without-follow-up=1 open=1",
				Advisory:       "Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
				ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
					Class:          "WEAK_CLOSURE_REOPENED",
					Digest:         "weak closure reopened",
					WindowAdvisory: "bounded closure window anchors=1 reopened=1",
					Detail:         "reopened after closure in recent bounded evidence.",
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"next", "--task", "tsk_next_human", "--human"}); err != nil {
		t.Fatalf("run next --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "executed START_LOCAL_RUN (SUCCEEDED)") {
		t.Fatalf("expected executed line in next human output, got %q", output)
	}
	if !strings.Contains(output, "digest triaged without follow-up") {
		t.Fatalf("expected digest line in next human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window triaged-without-follow-up=1 open=1") {
		t.Fatalf("expected bounded-window cue in next human output, got %q", output)
	}
	if !strings.Contains(output, "detail Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.") {
		t.Fatalf("expected detail advisory line in next human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest weak closure reopened") {
		t.Fatalf("expected closure advisory digest in next human output, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest triaged without follow-up",
		"window bounded window triaged-without-follow-up=1 open=1",
		"detail Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
		"closure advisory",
		"digest weak closure reopened",
		"window bounded closure window anchors=1 reopened=1",
		"detail reopened after closure in recent bounded evidence.",
		"result started local run run_next_human",
	)
}

func TestCLINextCommandHumanClosedWordingRemainsAuditOnly(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodExecutePrimaryOperatorStep {
			t.Fatalf("expected unified primary-step method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_next_closed"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_next_closed",
				TaskID:       common.TaskID("tsk_next_closed"),
				ActionHandle: "REVIEW_HANDOFF_FOLLOW_THROUGH",
				ResultClass:  "SUCCEEDED",
				Summary:      "reviewed handoff follow-through posture",
			},
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:    "FOLLOW_UP_CLOSED",
				Digest:   "follow-up closed (audit only)",
				Advisory: "Follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"next", "--task", "tsk_next_closed", "--human"}); err != nil {
		t.Fatalf("run next --human closed command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "digest follow-up closed (audit only)") {
		t.Fatalf("expected audit-only closure digest in next human output, got %q", output)
	}
	if strings.Contains(output, "resolved") || strings.Contains(output, "safe to continue") || strings.Contains(output, "problem solved") {
		t.Fatalf("next human closure wording must remain conservative, got %q", output)
	}
}

func TestCLINextCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodExecutePrimaryOperatorStep {
			t.Fatalf("expected unified primary-step method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskExecutePrimaryOperatorStepResponse{
			TaskID: common.TaskID("tsk_next_json"),
			Receipt: ipc.TaskOperatorStepReceipt{
				ReceiptID:    "orec_next_json",
				TaskID:       common.TaskID("tsk_next_json"),
				ActionHandle: "START_LOCAL_RUN",
				ResultClass:  "SUCCEEDED",
				Summary:      "started local run run_next_json",
			},
			ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
				State:  "FOLLOW_UP_OPEN",
				Digest: "follow-up open",
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"next", "--task", "tsk_next_json"}); err != nil {
		t.Fatalf("run next JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_next_json" {
		t.Fatalf("unexpected next JSON task_id: %+v", out)
	}
}

func TestCLIRunCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodTaskRun:
			payload, _ := json.Marshal(ipc.TaskRunResponse{
				TaskID:            common.TaskID("tsk_run_human"),
				RunID:             common.RunID("run_human"),
				RunStatus:         run.StatusCompleted,
				Phase:             "EXECUTION",
				CanonicalResponse: "bounded run action recorded",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_run_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "FOLLOW_UP_OPEN",
					Digest:         "follow-up open",
					WindowAdvisory: "bounded window open=1 reopened=0 triaged-without-follow-up=0",
					Advisory:       "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "OPERATIONALLY_UNRESOLVED",
						Digest:         "operationally unresolved",
						WindowAdvisory: "bounded closure window anchors=1 open=1",
						Detail:         "follow-up progression remains stagnant in recent bounded evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"run", "--task", "tsk_run_human", "--human"}); err != nil {
		t.Fatalf("run run --human command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "executed run start") || !strings.Contains(output, "digest follow-up open") {
		t.Fatalf("expected digest hierarchy in run human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window open=1 reopened=0 triaged-without-follow-up=0") {
		t.Fatalf("expected bounded-window cue in run human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest operationally unresolved") {
		t.Fatalf("expected closure advisory digest in run human output, got %q", output)
	}
	if strings.Contains(output, "task completed") || strings.Contains(output, "safe to continue") || strings.Contains(output, "problem solved") {
		t.Fatalf("run human wording must remain conservative, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest follow-up open",
		"window bounded window open=1 reopened=0 triaged-without-follow-up=0",
		"detail latest incident follow-up receipt is progressed; explicit closure remains open.",
		"closure advisory",
		"digest operationally unresolved",
		"window bounded closure window anchors=1 open=1",
		"detail follow-up progression remains stagnant in recent bounded evidence.",
		"result run run_human status=completed phase=execution",
	)
}

func TestCLIRunCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskRun {
			t.Fatalf("expected run method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskRunResponse{
			TaskID:            common.TaskID("tsk_run_json"),
			RunID:             common.RunID("run_json"),
			RunStatus:         run.StatusRunning,
			Phase:             "EXECUTION",
			CanonicalResponse: "run started",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"run", "--task", "tsk_run_json"}); err != nil {
		t.Fatalf("run run JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_run_json" {
		t.Fatalf("unexpected run JSON task_id: %+v", out)
	}
}

func TestCLIShellSessionsCommandHumanRemainsConservative(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodTaskShellSessions:
			payload, _ := json.Marshal(ipc.TaskShellSessionsResponse{
				TaskID: common.TaskID("tsk_sessions_human"),
				Sessions: []ipc.TaskShellSessionRecord{
					{
						SessionID:             "shs_live",
						TaskID:                common.TaskID("tsk_sessions_human"),
						ResolvedWorker:        "codex",
						AttachCapability:      "ATTACHABLE",
						SessionClass:          "ACTIVE_ATTACHABLE",
						OperatorSummary:       "active codex session can be attached",
						Active:                true,
						WorkerSessionID:       "wrk_1",
						WorkerSessionIDSource: "authoritative",
					},
					{
						SessionID:             "shs_fallback",
						TaskID:                common.TaskID("tsk_sessions_human"),
						ResolvedWorker:        "claude",
						AttachCapability:      "UNSUPPORTED",
						SessionClass:          "ENDED_TRANSCRIPT_ONLY",
						OperatorSummary:       "only bounded transcript evidence remains",
						Active:                false,
						WorkerSessionIDSource: "heuristic",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_sessions_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "TRIAGED_WITHOUT_FOLLOW_UP",
					Digest:         "triaged without follow-up",
					WindowAdvisory: "bounded window triaged-without-follow-up=1 open=0",
					Advisory:       "Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "OPERATIONALLY_UNRESOLVED",
						Digest:         "operationally unresolved",
						WindowAdvisory: "bounded closure window anchors=1 triaged-without-follow-up=1",
						Detail:         "triaged without follow-up remains operationally unresolved in recent bounded evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"shell-sessions", "--task", "tsk_sessions_human", "--human"}); err != nil {
		t.Fatalf("run shell-sessions --human command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "digest triaged without follow-up") || !strings.Contains(output, "sessions 2") {
		t.Fatalf("expected digest and session summary in shell-sessions human output, got %q", output)
	}
	if !strings.Contains(output, "truth shell session reports are bounded continuity evidence only") {
		t.Fatalf("expected conservative truth guard in shell-sessions human output, got %q", output)
	}
	if strings.Contains(output, "guaranteed attach") || strings.Contains(output, "fully resumable") || strings.Contains(output, "live session restored") {
		t.Fatalf("shell-sessions human wording must remain conservative, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest triaged without follow-up",
		"window bounded window triaged-without-follow-up=1 open=0",
		"detail latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
		"sessions 2",
	)
}

func TestComposeHumanActionLinesLocksDigestWindowDetailOrder(t *testing.T) {
	lines := composeHumanActionLines(
		common.TaskID("tsk_golden"),
		"run start",
		"run run_1 status=RUNNING",
		"run started",
		&ipc.TaskContinuityIncidentFollowUpSummary{
			State:          "FOLLOW_UP_OPEN",
			Digest:         "follow-up open",
			WindowAdvisory: "bounded window open=1 reopened=0 triaged-without-follow-up=0",
			Advisory:       "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
			ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
				Class:          "WEAK_CLOSURE_LOOP",
				Digest:         "weak closure loop",
				WindowAdvisory: "bounded closure window anchors=1 reopen-loops=1",
				Detail:         "repeated reopen pattern in recent bounded evidence.",
			},
		},
		nil,
		[]string{"detail line"},
		"truth line",
	)
	output := strings.Join(lines, "\n")
	assertSubstringsInOrder(t, output,
		"executed run start",
		"follow-up advisory",
		"digest follow-up open",
		"window bounded window open=1 reopened=0 triaged-without-follow-up=0",
		"detail Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
		"closure advisory",
		"digest weak closure loop",
		"window bounded closure window anchors=1 reopen-loops=1",
		"detail repeated reopen pattern in recent bounded evidence.",
		"result run run_1 status=RUNNING",
		"canonical run started",
		"detail line",
		"truth line",
	)
}

func TestClosureHumanLinesContractLocksVocabularyAndOrdering(t *testing.T) {
	lines := closureHumanLines(&ipc.TaskContinuityIncidentClosureSummary{
		Class:          "WEAK_CLOSURE_REOPENED",
		Digest:         "weak closure reopened",
		WindowAdvisory: "bounded closure window anchors=2 reopened=1",
		Detail:         "reopened after closure in recent bounded evidence.",
		RecentAnchors: []ipc.TaskContinuityIncidentClosureAnchorItem{
			{
				AnchorTransitionReceiptID: "ctr_anchor_1234567890",
				Class:                     "WEAK_CLOSURE_REOPENED",
				Explanation:               "reopened after closure in recent bounded evidence.",
				LatestFollowUpActionKind:  "REOPENED",
			},
		},
	}, true)
	output := strings.ToLower(strings.Join(lines, "\n"))
	assertSubstringsInOrder(t, output,
		"closure advisory",
		"digest weak closure reopened",
		"window bounded closure window anchors=2 reopened=1",
		"detail reopened after closure in recent bounded evidence.",
		"recent anchors",
		"anchor=ctr_anchor",
		"class=weak closure reopened",
		"action=reopened",
	)
	if strings.Contains(output, "resolved") || strings.Contains(output, "fixed") || strings.Contains(output, "safe") || strings.Contains(output, "complete") {
		t.Fatalf("closure human vocabulary drifted into overclaiming wording: %q", output)
	}
}

func TestCLIShellSessionsCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskShellSessions {
			t.Fatalf("expected shell sessions method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskShellSessionsResponse{
			TaskID: common.TaskID("tsk_sessions_json"),
			Sessions: []ipc.TaskShellSessionRecord{
				{SessionID: "shs_1", TaskID: common.TaskID("tsk_sessions_json")},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"shell-sessions", "--task", "tsk_sessions_json"}); err != nil {
		t.Fatalf("run shell-sessions JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_sessions_json" {
		t.Fatalf("unexpected shell-sessions JSON task_id: %+v", out)
	}
}

func TestCLIContinueCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodContinueTask:
			payload, _ := json.Marshal(ipc.TaskContinueResponse{
				TaskID:                common.TaskID("tsk_continue_human"),
				Outcome:               "continue assessment refreshed",
				RecoveryClass:         "READY_NEXT_RUN",
				RecommendedAction:     "START_NEXT_RUN",
				ReadyForNextRun:       true,
				ReadyForHandoffLaunch: false,
				RecoveryReason:        "latest continuity is ready for bounded execution",
				CanonicalResponse:     "continue assessment recorded",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_continue_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "TRIAGED_WITHOUT_FOLLOW_UP",
					Digest:         "triaged without follow-up",
					WindowAdvisory: "bounded window triaged-without-follow-up=1 open=0",
					Advisory:       "Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "OPERATIONALLY_UNRESOLVED",
						Digest:         "operationally unresolved",
						WindowAdvisory: "bounded closure window anchors=1 triaged-without-follow-up=1",
						Detail:         "triaged without follow-up remains operationally unresolved in recent bounded evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"continue", "--task", "tsk_continue_human", "--human"}); err != nil {
		t.Fatalf("run continue --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "executed continue") || !strings.Contains(output, "digest triaged without follow-up") {
		t.Fatalf("expected digest hierarchy in continue human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window triaged-without-follow-up=1 open=0") || !strings.Contains(output, "detail Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.") {
		t.Fatalf("expected window/detail lines in continue human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest operationally unresolved") {
		t.Fatalf("expected closure advisory digest in continue human output, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest triaged without follow-up",
		"window bounded window triaged-without-follow-up=1 open=0",
		"detail Latest continuity incident anchor is triaged, but no follow-up receipt is recorded yet.",
		"closure advisory",
		"digest operationally unresolved",
		"window bounded closure window anchors=1 triaged-without-follow-up=1",
		"detail triaged without follow-up remains operationally unresolved in recent bounded evidence.",
		"result continue assessment refreshed",
	)
}

func TestCLIContinueCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodContinueTask {
			t.Fatalf("expected continue method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskContinueResponse{
			TaskID:            common.TaskID("tsk_continue_json"),
			Outcome:           "continue assessment refreshed",
			CanonicalResponse: "continue assessment recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"continue", "--task", "tsk_continue_json"}); err != nil {
		t.Fatalf("run continue JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_continue_json" {
		t.Fatalf("unexpected continue JSON task_id: %+v", out)
	}
}

func TestCLICheckpointCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodCreateCheckpoint:
			payload, _ := json.Marshal(ipc.TaskCheckpointResponse{
				TaskID:            common.TaskID("tsk_checkpoint_human"),
				CheckpointID:      common.CheckpointID("chk_human"),
				Trigger:           "manual",
				IsResumable:       true,
				CanonicalResponse: "checkpoint recorded",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_checkpoint_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "FOLLOW_UP_OPEN",
					Digest:         "follow-up open",
					WindowAdvisory: "bounded window open=1 reopened=0 triaged-without-follow-up=0",
					Advisory:       "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "WEAK_CLOSURE_STAGNANT",
						Digest:         "weak closure stagnant",
						WindowAdvisory: "bounded closure window anchors=1 stagnant=1",
						Detail:         "stagnant follow-up progression in recent bounded evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"checkpoint", "--task", "tsk_checkpoint_human", "--human"}); err != nil {
		t.Fatalf("run checkpoint --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "executed checkpoint") || !strings.Contains(output, "digest follow-up open") {
		t.Fatalf("expected digest hierarchy in checkpoint human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window open=1 reopened=0 triaged-without-follow-up=0") || !strings.Contains(output, "detail Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.") {
		t.Fatalf("expected window/detail lines in checkpoint human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest weak closure stagnant") {
		t.Fatalf("expected closure advisory digest in checkpoint human output, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest follow-up open",
		"window bounded window open=1 reopened=0 triaged-without-follow-up=0",
		"detail Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
		"closure advisory",
		"digest weak closure stagnant",
		"window bounded closure window anchors=1 stagnant=1",
		"detail stagnant follow-up progression in recent bounded evidence.",
		"result checkpoint chk_human",
	)
}

func TestCLICheckpointCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodCreateCheckpoint {
			t.Fatalf("expected checkpoint method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskCheckpointResponse{
			TaskID:            common.TaskID("tsk_checkpoint_json"),
			CheckpointID:      common.CheckpointID("chk_json"),
			CanonicalResponse: "checkpoint recorded",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"checkpoint", "--task", "tsk_checkpoint_json"}); err != nil {
		t.Fatalf("run checkpoint JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_checkpoint_json" {
		t.Fatalf("unexpected checkpoint JSON task_id: %+v", out)
	}
}

func TestCLIHandoffCreateCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodCreateHandoff:
			payload, _ := json.Marshal(ipc.TaskHandoffCreateResponse{
				TaskID:            common.TaskID("tsk_handoff_create_human"),
				HandoffID:         "hnd_human",
				SourceWorker:      run.WorkerKindCodex,
				TargetWorker:      run.WorkerKindClaude,
				Status:            "ACCEPTED_NOT_LAUNCHED",
				CanonicalResponse: "handoff created",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_handoff_create_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "FOLLOW_UP_REOPENED",
					Digest:         "follow-up reopened",
					WindowAdvisory: "bounded window open=1 reopened=1 triaged-without-follow-up=0",
					Advisory:       "Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "WEAK_CLOSURE_REOPENED",
						Digest:         "weak closure reopened",
						WindowAdvisory: "bounded closure window anchors=1 reopened=1",
						Detail:         "reopened after closure in recent bounded evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"handoff-create", "--task", "tsk_handoff_create_human", "--human"}); err != nil {
		t.Fatalf("run handoff-create --human command: %v", err)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "executed handoff create") || !strings.Contains(output, "digest follow-up reopened") {
		t.Fatalf("expected digest hierarchy in handoff-create human output, got %q", output)
	}
	if !strings.Contains(output, "window bounded window open=1 reopened=1 triaged-without-follow-up=0") || !strings.Contains(output, "detail Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.") {
		t.Fatalf("expected window/detail lines in handoff-create human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest weak closure reopened") {
		t.Fatalf("expected closure advisory digest in handoff-create human output, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest follow-up reopened",
		"window bounded window open=1 reopened=1 triaged-without-follow-up=0",
		"detail Latest incident follow-up receipt is REOPENED; follow-up remains explicitly open.",
		"closure advisory",
		"digest weak closure reopened",
		"window bounded closure window anchors=1 reopened=1",
		"detail reopened after closure in recent bounded evidence.",
		"result handoff hnd_human status=ACCEPTED_NOT_LAUNCHED target=claude",
	)
}

func TestCLIHandoffCreateCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodCreateHandoff {
			t.Fatalf("expected handoff-create method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffCreateResponse{
			TaskID:            common.TaskID("tsk_handoff_create_json"),
			HandoffID:         "hnd_json",
			Status:            "ACCEPTED_NOT_LAUNCHED",
			CanonicalResponse: "handoff created",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"handoff-create", "--task", "tsk_handoff_create_json"}); err != nil {
		t.Fatalf("run handoff-create JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_handoff_create_json" {
		t.Fatalf("unexpected handoff-create JSON task_id: %+v", out)
	}
}

func TestCLIHandoffAcceptCommandHumanUsesDigestWindowDetailHierarchy(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		switch req.Method {
		case ipc.MethodAcceptHandoff:
			payload, _ := json.Marshal(ipc.TaskHandoffAcceptResponse{
				TaskID:            common.TaskID("tsk_handoff_accept_human"),
				HandoffID:         "hnd_accept_human",
				Status:            "ACCEPTED_NOT_LAUNCHED",
				CanonicalResponse: "handoff accepted",
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		case ipc.MethodTaskStatus:
			payload, _ := json.Marshal(ipc.TaskStatusResponse{
				TaskID: common.TaskID("tsk_handoff_accept_human"),
				ContinuityIncidentFollowUp: &ipc.TaskContinuityIncidentFollowUpSummary{
					State:          "FOLLOW_UP_CLOSED",
					Digest:         "follow-up closed (audit only)",
					WindowAdvisory: "bounded window open=0 reopened=0 triaged-without-follow-up=0",
					Advisory:       "Follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
					ClosureIntelligence: &ipc.TaskContinuityIncidentClosureSummary{
						Class:          "STABLE_BOUNDED",
						Digest:         "stable bounded closure progression",
						WindowAdvisory: "bounded closure window anchors=1 closed=1",
						Detail:         "stable within bounded recent evidence.",
					},
				},
			})
			return ipc.Response{OK: true, Payload: payload}, nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return ipc.Response{}, nil
		}
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"handoff-accept", "--task", "tsk_handoff_accept_human", "--handoff", "hnd_accept_human", "--human"}); err != nil {
		t.Fatalf("run handoff-accept --human command: %v", err)
	}
	stdout.restore()
	output := strings.ToLower(stdout.buffer.String())
	if !strings.Contains(output, "executed handoff accept") || !strings.Contains(output, "digest follow-up closed (audit only)") {
		t.Fatalf("expected digest hierarchy in handoff-accept human output, got %q", output)
	}
	if !strings.Contains(output, "closure advisory") || !strings.Contains(output, "digest stable bounded closure progression") {
		t.Fatalf("expected closure advisory digest in handoff-accept human output, got %q", output)
	}
	if strings.Contains(output, "safe") || strings.Contains(output, "solved") || strings.Contains(output, "completed successfully") {
		t.Fatalf("handoff-accept human wording must remain conservative, got %q", output)
	}
	assertSubstringsInOrder(t, output,
		"follow-up advisory",
		"digest follow-up closed (audit only)",
		"window bounded window open=0 reopened=0 triaged-without-follow-up=0",
		"detail follow-up closure is an audit marker only and does not certify correctness, completion, or resumability.",
		"closure advisory",
		"digest stable bounded closure progression",
		"window bounded closure window anchors=1 closed=1",
		"detail stable within bounded recent evidence.",
		"result handoff hnd_accept_human status=accepted_not_launched",
	)
}

func TestCLIHandoffAcceptCommandDefaultsToStableJSONOutput(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodAcceptHandoff {
			t.Fatalf("expected handoff-accept method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskHandoffAcceptResponse{
			TaskID:            common.TaskID("tsk_handoff_accept_json"),
			HandoffID:         "hnd_accept_json",
			Status:            "ACCEPTED_NOT_LAUNCHED",
			CanonicalResponse: "handoff accepted",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"handoff-accept", "--task", "tsk_handoff_accept_json", "--handoff", "hnd_accept_json"}); err != nil {
		t.Fatalf("run handoff-accept JSON mode command: %v", err)
	}
	stdout.restore()
	raw := strings.TrimSpace(stdout.buffer.String())
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("expected JSON output by default, got %q (%v)", raw, err)
	}
	if out["task_id"] != "tsk_handoff_accept_json" {
		t.Fatalf("unexpected handoff-accept JSON task_id: %+v", out)
	}
}

func TestCLIOperatorAcknowledgeReviewGapCommandRoutesAndPrintsTruth(t *testing.T) {
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	var captured ipc.Request
	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		captured = req
		payload, _ := json.Marshal(ipc.TaskOperatorAcknowledgeReviewGapResponse{
			TaskID:    common.TaskID("tsk_123"),
			SessionID: "shs_123",
			Acknowledgment: ipc.TaskTranscriptReviewGapAcknowledgment{
				AcknowledgmentID:         "sack_123",
				TaskID:                   common.TaskID("tsk_123"),
				SessionID:                "shs_123",
				Class:                    "stale_review",
				ReviewState:              "global_review_stale",
				ReviewedUpToSequence:     120,
				OldestUnreviewedSequence: 121,
				NewestRetainedSequence:   160,
				UnreviewedRetainedCount:  40,
				Summary:                  "proceed with explicit awareness",
				CreatedAt:                time.Unix(1710001000, 0).UTC(),
			},
			ReviewGapState:         "global_review_stale",
			ReviewGapClass:         "stale_review",
			ReviewedUpToSequence:   120,
			OldestUnreviewedSeq:    121,
			NewestRetainedSequence: 160,
			UnreviewedRetained:     40,
			Advisory:               "review awareness recommended while progressing",
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{
		"operator", "acknowledge-review-gap",
		"--task", "tsk_123",
		"--session", "shs_123",
		"--kind", "stale_review",
		"--summary", "proceed with explicit awareness",
	}); err != nil {
		t.Fatalf("run operator acknowledge-review-gap command: %v", err)
	}
	if captured.Method != ipc.MethodOperatorAcknowledgeReviewGap {
		t.Fatalf("expected operator review-gap acknowledgment method, got %s", captured.Method)
	}
	var req ipc.TaskOperatorAcknowledgeReviewGapRequest
	if err := json.Unmarshal(captured.Payload, &req); err != nil {
		t.Fatalf("unmarshal operator review-gap acknowledgment request: %v", err)
	}
	if req.TaskID != "tsk_123" || req.SessionID != "shs_123" || req.Kind != "stale_review" {
		t.Fatalf("unexpected operator review-gap acknowledgment request payload: %+v", req)
	}
	stdout.restore()
	output := stdout.buffer.String()
	if !strings.Contains(output, "class stale_review") {
		t.Fatalf("expected acknowledgment class in output, got %q", output)
	}
	if !strings.Contains(output, "truth this acknowledgment records operator awareness of transcript review gaps only") {
		t.Fatalf("expected conservative truth guard in output, got %q", output)
	}
}

func TestCrossSurfaceClosureVocabularyParityContract(t *testing.T) {
	scenarios := []struct {
		name   string
		class  string
		digest string
		detail string
		action string
	}{
		{
			name:   "weak-reopened",
			class:  "WEAK_CLOSURE_REOPENED",
			digest: "weak closure reopened",
			detail: "reopened after closure in recent bounded evidence.",
			action: "REOPENED",
		},
		{
			name:   "weak-loop",
			class:  "WEAK_CLOSURE_LOOP",
			digest: "weak closure loop",
			detail: "repeated reopen pattern in recent bounded evidence.",
			action: "REOPENED",
		},
		{
			name:   "weak-stagnant",
			class:  "WEAK_CLOSURE_STAGNANT",
			digest: "weak closure stagnant",
			detail: "stagnant follow-up progression in recent bounded evidence.",
			action: "PROGRESSED",
		},
		{
			name:   "operationally-unresolved",
			class:  "OPERATIONALLY_UNRESOLVED",
			digest: "operationally unresolved",
			detail: "triaged without follow-up remains operationally unresolved in recent bounded evidence.",
			action: "RECORDED_PENDING",
		},
		{
			name:   "stable-bounded",
			class:  "STABLE_BOUNDED",
			digest: "stable bounded closure progression",
			detail: "stable within bounded recent evidence.",
			action: "CLOSED",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			closure := &ipc.TaskContinuityIncidentClosureSummary{
				Class:          scenario.class,
				Digest:         scenario.digest,
				WindowAdvisory: "bounded closure window anchors=1",
				Detail:         scenario.detail,
				RecentAnchors: []ipc.TaskContinuityIncidentClosureAnchorItem{
					{
						AnchorTransitionReceiptID: "ctr_cross_surface_123",
						Class:                     scenario.class,
						Digest:                    scenario.digest,
						Explanation:               scenario.detail,
						LatestFollowUpActionKind:  scenario.action,
					},
				},
			}
			followUp := &ipc.TaskContinuityIncidentFollowUpSummary{
				State:               "FOLLOW_UP_OPEN",
				Digest:              "follow-up open",
				WindowAdvisory:      "bounded window open=1 reopened=0 triaged-without-follow-up=0",
				Advisory:            "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
				ClosureIntelligence: closure,
			}
			cliRendered := strings.ToLower(strings.Join(followUpHumanLines(followUp, nil, true), "\n"))
			shellRendered := renderShellClosureContractView(scenario.class, scenario.digest, scenario.detail, scenario.action)
			closureCommandRendered := renderIncidentClosureContractView(t, scenario.class, scenario.digest, scenario.detail, scenario.action)

			assertCrossSurfaceClosureContract(t, cliRendered, shellRendered, closureCommandRendered, scenario.digest, scenario.detail)
		})
	}
}

func renderShellClosureContractView(class string, digest string, detail string, action string) string {
	vm := tukushell.BuildViewModel(
		tukushell.Snapshot{
			TaskID: "tsk_cross_surface",
			Phase:  "BRIEF_READY",
			Status: "ACTIVE",
			ContinuityIncidentFollowUp: &tukushell.ContinuityIncidentFollowUpSummary{
				State:          "FOLLOW_UP_OPEN",
				Digest:         "follow-up open",
				WindowAdvisory: "bounded window open=1 reopened=0 triaged-without-follow-up=0",
				Advisory:       "Latest incident follow-up receipt is PROGRESSED; explicit closure remains open.",
				ClosureIntelligence: &tukushell.ContinuityIncidentClosureSummary{
					Class:          class,
					Digest:         digest,
					WindowAdvisory: "bounded closure window anchors=1",
					Detail:         detail,
					RecentAnchors: []tukushell.ContinuityIncidentClosureAnchorItem{
						{
							AnchorTransitionReceiptID: "ctr_cross_surface_123",
							Class:                     class,
							Digest:                    digest,
							Explanation:               detail,
							LatestFollowUpActionKind:  action,
						},
					},
				},
			},
		},
		tukushell.UIState{ShowInspector: true, ShowProof: true},
		crossSurfaceContractHost{},
		120,
		30,
	)
	sections := []string{}
	if vm.Inspector != nil {
		for _, section := range vm.Inspector.Sections {
			sections = append(sections, strings.Join(section.Lines, "\n"))
		}
	}
	if vm.ProofStrip != nil {
		sections = append(sections, strings.Join(vm.ProofStrip.Lines, "\n"))
	}
	return strings.ToLower(strings.Join(sections, "\n"))
}

func renderIncidentClosureContractView(t *testing.T, class string, digest string, detail string, action string) string {
	t.Helper()
	origCall := ipcCall
	defer func() { ipcCall = origCall }()

	ipcCall = func(_ context.Context, _ string, req ipc.Request) (ipc.Response, error) {
		if req.Method != ipc.MethodTaskContinuityIncidentClosure {
			t.Fatalf("expected continuity incident closure method, got %s", req.Method)
		}
		payload, _ := json.Marshal(ipc.TaskContinuityIncidentClosureResponse{
			TaskID:                    common.TaskID("tsk_cross_surface"),
			Bounded:                   true,
			RequestedLimit:            2,
			HasMoreOlder:              false,
			LatestTransitionReceiptID: "ctr_cross_surface_123",
			Latest: &ipc.TaskContinuityIncidentFollowUpReceipt{
				ReceiptID:                 "cifr_cross_surface_123",
				AnchorTransitionReceiptID: "ctr_cross_surface_123",
				ActionKind:                action,
			},
			Receipts: []ipc.TaskContinuityIncidentFollowUpReceipt{
				{
					ReceiptID:                 "cifr_cross_surface_123",
					AnchorTransitionReceiptID: "ctr_cross_surface_123",
					ActionKind:                action,
				},
			},
			Rollup: ipc.TaskContinuityIncidentFollowUpHistoryRollup{
				WindowSize:              1,
				BoundedWindow:           true,
				DistinctAnchors:         1,
				OperationallyNotable:    true,
				Summary:                 "bounded evidence includes closure posture",
				AnchorsWithOpenFollowUp: 1,
			},
			Closure: &ipc.TaskContinuityIncidentClosureSummary{
				Class:          class,
				Digest:         digest,
				WindowAdvisory: "bounded closure window anchors=1",
				Detail:         detail,
				RecentAnchors: []ipc.TaskContinuityIncidentClosureAnchorItem{
					{
						AnchorTransitionReceiptID: "ctr_cross_surface_123",
						Class:                     class,
						Digest:                    digest,
						Explanation:               detail,
						LatestFollowUpActionKind:  action,
						LatestFollowUpReceiptID:   "cifr_cross_surface_123",
					},
				},
			},
		})
		return ipc.Response{OK: true, Payload: payload}, nil
	}

	app := NewCLIApplication()
	stdout := captureCLIStdout(t)
	if err := app.Run(context.Background(), []string{"incident", "closure", "--task", "tsk_cross_surface", "--limit", "2"}); err != nil {
		t.Fatalf("run incident closure command for cross-surface contract: %v", err)
	}
	stdout.restore()
	return strings.ToLower(stdout.buffer.String())
}

func assertCrossSurfaceClosureContract(t *testing.T, cliRendered string, shellRendered string, closureCommandRendered string, digest string, detail string) {
	t.Helper()
	normalizedDigest := strings.ToLower(strings.TrimSpace(digest))
	normalizedDetail := strings.ToLower(strings.TrimSpace(detail))
	if normalizedDigest == "" || normalizedDetail == "" {
		t.Fatalf("invalid closure contract fixture digest/detail: %q / %q", digest, detail)
	}

	for _, rendered := range []struct {
		channel string
		body    string
	}{
		{channel: "cli", body: cliRendered},
		{channel: "shell", body: shellRendered},
		{channel: "incident-closure-command", body: closureCommandRendered},
	} {
		if !strings.Contains(rendered.body, normalizedDigest) {
			t.Fatalf("%s output missing closure digest %q: %q", rendered.channel, normalizedDigest, rendered.body)
		}
		if !strings.Contains(rendered.body, normalizedDetail) {
			t.Fatalf("%s output missing closure detail %q: %q", rendered.channel, normalizedDetail, rendered.body)
		}
		if strings.Contains(rendered.body, "root cause solved") ||
			strings.Contains(rendered.body, "incident resolved") ||
			strings.Contains(rendered.body, "problem solved") ||
			strings.Contains(rendered.body, "safe to continue") ||
			strings.Contains(rendered.body, "task completed") ||
			strings.Contains(rendered.body, "claude completed the work") {
			t.Fatalf("%s output drifted into overclaiming closure language: %q", rendered.channel, rendered.body)
		}
	}
}

type crossSurfaceContractHost struct{}

func (crossSurfaceContractHost) Start(context.Context, tukushell.Snapshot) error { return nil }
func (crossSurfaceContractHost) Stop() error                                     { return nil }
func (crossSurfaceContractHost) UpdateSnapshot(tukushell.Snapshot)               {}
func (crossSurfaceContractHost) Resize(int, int) bool                            { return false }
func (crossSurfaceContractHost) CanAcceptInput() bool                            { return false }
func (crossSurfaceContractHost) WriteInput([]byte) bool                          { return false }
func (crossSurfaceContractHost) Status() tukushell.HostStatus {
	return tukushell.HostStatus{Mode: tukushell.HostModeTranscript, State: tukushell.HostStateFallback, Label: "transcript", InputLive: false}
}
func (crossSurfaceContractHost) Title() string              { return "cross-surface-contract" }
func (crossSurfaceContractHost) WorkerLabel() string        { return "none" }
func (crossSurfaceContractHost) Lines(int, int) []string    { return nil }
func (crossSurfaceContractHost) ActivityLines(int) []string { return nil }
