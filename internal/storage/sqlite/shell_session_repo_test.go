package sqlite

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/shellsession"
	"tuku/internal/domain/transition"
)

func TestShellSessionTranscriptPageReadDeterministicOrdering(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shell-transcript-page.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	repo := store.ShellSessions()
	taskID := common.TaskID("tsk_page")
	sessionID := "shs_page"
	base := time.Unix(1710000000, 0).UTC()

	chunks := make([]shellsession.TranscriptChunk, 0, 5)
	for i := 0; i < 5; i++ {
		chunks = append(chunks, shellsession.TranscriptChunk{
			TaskID:    taskID,
			SessionID: sessionID,
			Source:    shellsession.TranscriptSourceWorkerOutput,
			Content:   fmt.Sprintf("line %d", i+1),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	if _, err := repo.AppendTranscript(taskID, sessionID, chunks, 200); err != nil {
		t.Fatalf("append transcript: %v", err)
	}

	page1, more1, err := repo.ListTranscriptPage(taskID, sessionID, 0, 2, "")
	if err != nil {
		t.Fatalf("read first page: %v", err)
	}
	if !more1 || len(page1) != 2 || page1[0].SequenceNo != 4 || page1[1].SequenceNo != 5 {
		t.Fatalf("unexpected first page payload more=%v chunks=%+v", more1, page1)
	}

	page2, more2, err := repo.ListTranscriptPage(taskID, sessionID, 4, 2, "")
	if err != nil {
		t.Fatalf("read second page: %v", err)
	}
	if !more2 || len(page2) != 2 || page2[0].SequenceNo != 2 || page2[1].SequenceNo != 3 {
		t.Fatalf("unexpected second page payload more=%v chunks=%+v", more2, page2)
	}

	page3, more3, err := repo.ListTranscriptPage(taskID, sessionID, 2, 2, "")
	if err != nil {
		t.Fatalf("read third page: %v", err)
	}
	if more3 || len(page3) != 1 || page3[0].SequenceNo != 1 {
		t.Fatalf("unexpected third page payload more=%v chunks=%+v", more3, page3)
	}
}

func TestShellSessionTranscriptSummaryRetentionAndReopenDurability(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shell-transcript-retention.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.ShellSessions()
	taskID := common.TaskID("tsk_retention")
	sessionID := "shs_retention"
	base := time.Unix(1710000000, 0).UTC()
	chunks := make([]shellsession.TranscriptChunk, 0, 210)
	for i := 0; i < 210; i++ {
		chunks = append(chunks, shellsession.TranscriptChunk{
			TaskID:    taskID,
			SessionID: sessionID,
			Source:    shellsession.TranscriptSourceWorkerOutput,
			Content:   fmt.Sprintf("line %03d", i+1),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	if _, err := repo.AppendTranscript(taskID, sessionID, chunks, shellsession.DefaultTranscriptRetentionChunks); err != nil {
		t.Fatalf("append transcript: %v", err)
	}

	summary, err := repo.TranscriptSummary(taskID, sessionID, shellsession.DefaultTranscriptRetentionChunks)
	if err != nil {
		t.Fatalf("transcript summary: %v", err)
	}
	if summary.RetainedChunks != shellsession.DefaultTranscriptRetentionChunks || summary.DroppedChunks != 10 {
		t.Fatalf("unexpected retention counters: %+v", summary)
	}
	if summary.OldestSequenceNo != 11 || summary.NewestSequenceNo != 210 || summary.LastSequenceNo != 210 {
		t.Fatalf("unexpected sequence summary: %+v", summary)
	}
	if len(summary.SourceCounts) != 1 || summary.SourceCounts[0].Source != shellsession.TranscriptSourceWorkerOutput || summary.SourceCounts[0].Chunks != 200 {
		t.Fatalf("unexpected source summary: %+v", summary.SourceCounts)
	}
	page, more, err := repo.ListTranscriptPage(taskID, sessionID, 0, 40, "")
	if err != nil {
		t.Fatalf("read transcript page: %v", err)
	}
	if !more || len(page) != 40 || page[0].SequenceNo != 171 || page[len(page)-1].SequenceNo != 210 {
		t.Fatalf("unexpected page after retention: more=%v first=%d last=%d len=%d", more, page[0].SequenceNo, page[len(page)-1].SequenceNo, len(page))
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	reopenedSummary, err := reopened.ShellSessions().TranscriptSummary(taskID, sessionID, shellsession.DefaultTranscriptRetentionChunks)
	if err != nil {
		t.Fatalf("summary after reopen: %v", err)
	}
	if reopenedSummary.RetainedChunks != 200 || reopenedSummary.DroppedChunks != 10 || reopenedSummary.OldestSequenceNo != 11 || reopenedSummary.NewestSequenceNo != 210 {
		t.Fatalf("unexpected durable summary after reopen: %+v", reopenedSummary)
	}
}

func TestShellSessionTranscriptReviewPersistenceAndLatestByScope(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shell-transcript-review.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.ShellSessions()
	taskID := common.TaskID("tsk_review")
	sessionID := "shs_review"
	first := time.Unix(1710000000, 0).UTC()
	second := first.Add(30 * time.Second)

	firstReview, err := repo.AppendTranscriptReview(shellsession.TranscriptReview{
		ReviewID:               "srev_1",
		TaskID:                 taskID,
		SessionID:              sessionID,
		SourceFilter:           "",
		ReviewedUpToSequence:   120,
		Summary:                "reviewed bounded transcript through seq 120",
		TranscriptState:        shellsession.TranscriptStateTranscriptOnlyPartial,
		RetentionLimit:         200,
		RetainedChunks:         200,
		DroppedChunks:          57,
		OldestRetainedSequence: 81,
		NewestRetainedSequence: 280,
		CreatedAt:              first,
	})
	if err != nil {
		t.Fatalf("append first transcript review: %v", err)
	}
	if firstReview.ReviewID != "srev_1" {
		t.Fatalf("unexpected first review response: %+v", firstReview)
	}
	if _, err := repo.AppendTranscriptReview(shellsession.TranscriptReview{
		ReviewID:               "srev_2",
		TaskID:                 taskID,
		SessionID:              sessionID,
		SourceFilter:           shellsession.TranscriptSourceWorkerOutput,
		ReviewedUpToSequence:   180,
		Summary:                "reviewed worker output through seq 180",
		TranscriptState:        shellsession.TranscriptStateTranscriptOnlyPartial,
		RetentionLimit:         200,
		RetainedChunks:         200,
		DroppedChunks:          57,
		OldestRetainedSequence: 81,
		NewestRetainedSequence: 280,
		CreatedAt:              second,
	}); err != nil {
		t.Fatalf("append second transcript review: %v", err)
	}

	latestAll, err := repo.LatestTranscriptReview(taskID, sessionID, "")
	if err != nil {
		t.Fatalf("latest all-source transcript review: %v", err)
	}
	if latestAll == nil || latestAll.ReviewID != "srev_1" {
		t.Fatalf("unexpected all-source latest review: %+v", latestAll)
	}
	latestWorker, err := repo.LatestTranscriptReview(taskID, sessionID, shellsession.TranscriptSourceWorkerOutput)
	if err != nil {
		t.Fatalf("latest worker-source transcript review: %v", err)
	}
	if latestWorker == nil || latestWorker.ReviewID != "srev_2" || latestWorker.ReviewedUpToSequence != 180 {
		t.Fatalf("unexpected worker-source latest review: %+v", latestWorker)
	}
	history, err := repo.ListTranscriptReviews(taskID, sessionID, shellsession.TranscriptSourceWorkerOutput, 10)
	if err != nil {
		t.Fatalf("list worker-source transcript reviews: %v", err)
	}
	if len(history) != 1 || history[0].ReviewID != "srev_2" {
		t.Fatalf("unexpected review history payload: %+v", history)
	}
	latestAny, err := repo.LatestTranscriptReviewAnyScope(taskID, sessionID)
	if err != nil {
		t.Fatalf("latest any-scope transcript review: %v", err)
	}
	if latestAny == nil || latestAny.ReviewID != "srev_2" {
		t.Fatalf("unexpected latest any-scope review payload: %+v", latestAny)
	}
	anyHistory, err := repo.ListTranscriptReviewsAnyScope(taskID, sessionID, 10)
	if err != nil {
		t.Fatalf("list any-scope transcript reviews: %v", err)
	}
	if len(anyHistory) != 2 || anyHistory[0].ReviewID != "srev_2" || anyHistory[1].ReviewID != "srev_1" {
		t.Fatalf("unexpected any-scope review history payload: %+v", anyHistory)
	}
	limitedAnyHistory, err := repo.ListTranscriptReviewsAnyScope(taskID, sessionID, 1)
	if err != nil {
		t.Fatalf("list limited any-scope transcript reviews: %v", err)
	}
	if len(limitedAnyHistory) != 1 || limitedAnyHistory[0].ReviewID != "srev_2" {
		t.Fatalf("unexpected limited any-scope review history payload: %+v", limitedAnyHistory)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	durableReview, err := reopened.ShellSessions().LatestTranscriptReview(taskID, sessionID, shellsession.TranscriptSourceWorkerOutput)
	if err != nil {
		t.Fatalf("latest review after reopen: %v", err)
	}
	if durableReview == nil || durableReview.ReviewID != "srev_2" || durableReview.CreatedAt.IsZero() {
		t.Fatalf("unexpected durable review after reopen: %+v", durableReview)
	}
	durableAny, err := reopened.ShellSessions().LatestTranscriptReviewAnyScope(taskID, sessionID)
	if err != nil {
		t.Fatalf("latest any-scope review after reopen: %v", err)
	}
	if durableAny == nil || durableAny.ReviewID != "srev_2" {
		t.Fatalf("unexpected durable any-scope review after reopen: %+v", durableAny)
	}
}

func TestShellSessionTranscriptReviewGapAcknowledgmentPersistenceAndLatest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shell-transcript-review-gap-ack.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.ShellSessions()
	taskID := common.TaskID("tsk_ack")
	first := time.Unix(1710001000, 0).UTC()
	second := first.Add(45 * time.Second)
	if _, err := repo.AppendTranscriptReviewGapAcknowledgment(shellsession.TranscriptReviewGapAcknowledgment{
		AcknowledgmentID:         "sack_1",
		TaskID:                   taskID,
		SessionID:                "shs_1",
		Class:                    shellsession.TranscriptReviewGapAckStaleReview,
		ReviewState:              "global_review_stale",
		ReviewScope:              "",
		ReviewedUpToSequence:     120,
		OldestUnreviewedSequence: 121,
		NewestRetainedSequence:   160,
		UnreviewedRetainedCount:  40,
		TranscriptState:          shellsession.TranscriptStateTranscriptOnlyPartial,
		RetentionLimit:           200,
		RetainedChunks:           200,
		DroppedChunks:            57,
		ActionContext:            "task.operator.next:start_local_run",
		Summary:                  "acknowledged stale retained transcript evidence before progression",
		CreatedAt:                first,
	}); err != nil {
		t.Fatalf("append first review-gap acknowledgment: %v", err)
	}
	if _, err := repo.AppendTranscriptReviewGapAcknowledgment(shellsession.TranscriptReviewGapAcknowledgment{
		AcknowledgmentID:         "sack_2",
		TaskID:                   taskID,
		SessionID:                "shs_2",
		Class:                    shellsession.TranscriptReviewGapAckSourceScopedOnly,
		ReviewState:              "source_scoped_review_current",
		ReviewScope:              shellsession.TranscriptSourceWorkerOutput,
		ReviewedUpToSequence:     80,
		OldestUnreviewedSequence: 0,
		NewestRetainedSequence:   80,
		UnreviewedRetainedCount:  0,
		TranscriptState:          shellsession.TranscriptStateBoundedAvailable,
		RetentionLimit:           200,
		RetainedChunks:           80,
		DroppedChunks:            0,
		ActionContext:            "operator_acknowledge_review_gap",
		Summary:                  "acknowledged source-scoped review coverage",
		CreatedAt:                second,
	}); err != nil {
		t.Fatalf("append second review-gap acknowledgment: %v", err)
	}

	latestAny, err := repo.LatestTranscriptReviewGapAcknowledgment(taskID, "")
	if err != nil {
		t.Fatalf("latest review-gap acknowledgment any session: %v", err)
	}
	if latestAny == nil || latestAny.AcknowledgmentID != "sack_2" {
		t.Fatalf("unexpected latest any-session review-gap acknowledgment: %+v", latestAny)
	}

	latestSessionOne, err := repo.LatestTranscriptReviewGapAcknowledgment(taskID, "shs_1")
	if err != nil {
		t.Fatalf("latest review-gap acknowledgment by session: %v", err)
	}
	if latestSessionOne == nil || latestSessionOne.AcknowledgmentID != "sack_1" {
		t.Fatalf("unexpected latest session-specific review-gap acknowledgment: %+v", latestSessionOne)
	}

	historyAny, err := repo.ListTranscriptReviewGapAcknowledgments(taskID, "", 10)
	if err != nil {
		t.Fatalf("list any-session review-gap acknowledgments: %v", err)
	}
	if len(historyAny) != 2 || historyAny[0].AcknowledgmentID != "sack_2" || historyAny[1].AcknowledgmentID != "sack_1" {
		t.Fatalf("unexpected any-session acknowledgment history order: %+v", historyAny)
	}

	historySessionOne, err := repo.ListTranscriptReviewGapAcknowledgments(taskID, "shs_1", 10)
	if err != nil {
		t.Fatalf("list session-specific review-gap acknowledgments: %v", err)
	}
	if len(historySessionOne) != 1 || historySessionOne[0].AcknowledgmentID != "sack_1" {
		t.Fatalf("unexpected session-specific acknowledgment history: %+v", historySessionOne)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	durable, err := reopened.ShellSessions().LatestTranscriptReviewGapAcknowledgment(taskID, "")
	if err != nil {
		t.Fatalf("latest durable review-gap acknowledgment after reopen: %v", err)
	}
	if durable == nil || durable.AcknowledgmentID != "sack_2" || durable.CreatedAt.IsZero() {
		t.Fatalf("unexpected durable review-gap acknowledgment after reopen: %+v", durable)
	}
}

func TestContinuityTransitionReceiptPersistenceOrderingAndReopenDurability(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "continuity-transition-receipts.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}

	repo := store.TransitionReceipts()
	taskID := common.TaskID("tsk_transition_receipts")
	base := time.Unix(1713000000, 0).UTC()
	records := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               "ctr_001",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffLaunch,
			BranchClassBefore:       "LOCAL",
			BranchClassAfter:        "HANDOFF_CLAUDE",
			HandoffContinuityBefore: "ACCEPTED_NOT_LAUNCHED",
			HandoffContinuityAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
			Summary:                 "launch transition 1",
			CreatedAt:               base,
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_002",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffResolution,
			BranchClassBefore:       "HANDOFF_CLAUDE",
			BranchClassAfter:        "LOCAL",
			HandoffContinuityBefore: "FOLLOW_THROUGH_STALLED",
			HandoffContinuityAfter:  "RESOLVED",
			Summary:                 "resolution transition 2",
			CreatedAt:               base.Add(10 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_003",
			TaskID:                  taskID,
			TransitionKind:          transition.KindHandoffResolution,
			BranchClassBefore:       "HANDOFF_CLAUDE",
			BranchClassAfter:        "LOCAL",
			HandoffContinuityBefore: "FOLLOW_THROUGH_PROOF_OF_LIFE",
			HandoffContinuityAfter:  "RESOLVED",
			Summary:                 "resolution transition 3",
			CreatedAt:               base.Add(10 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	latest, err := repo.LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest transition receipt: %v", err)
	}
	if latest.ReceiptID != "ctr_003" {
		t.Fatalf("expected latest transition receipt ctr_003, got %+v", latest)
	}

	page, err := repo.ListByTask(taskID, 2)
	if err != nil {
		t.Fatalf("list transition receipt page: %v", err)
	}
	if len(page) != 2 || page[0].ReceiptID != "ctr_003" || page[1].ReceiptID != "ctr_002" {
		t.Fatalf("unexpected transition receipt ordering page: %+v", page)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()

	reopenedLatest, err := reopened.TransitionReceipts().LatestByTask(taskID)
	if err != nil {
		t.Fatalf("latest transition receipt after reopen: %v", err)
	}
	if reopenedLatest.ReceiptID != "ctr_003" || reopenedLatest.CreatedAt.IsZero() {
		t.Fatalf("unexpected durable latest transition receipt after reopen: %+v", reopenedLatest)
	}
	reopenedHistory, err := reopened.TransitionReceipts().ListByTask(taskID, 10)
	if err != nil {
		t.Fatalf("list transition receipts after reopen: %v", err)
	}
	if len(reopenedHistory) != 3 || reopenedHistory[0].ReceiptID != "ctr_003" || reopenedHistory[1].ReceiptID != "ctr_002" || reopenedHistory[2].ReceiptID != "ctr_001" {
		t.Fatalf("unexpected durable transition receipt history after reopen: %+v", reopenedHistory)
	}
}

func TestContinuityTransitionReceiptFilteredReadSupportsDeterministicPagination(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "continuity-transition-receipts-filtered.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	repo := store.TransitionReceipts()
	taskID := common.TaskID("tsk_transition_receipts_filtered")
	base := time.Unix(1713500000, 0).UTC()
	records := []transition.Receipt{
		{
			Version:                 1,
			ReceiptID:               "ctr_130",
			TaskID:                  taskID,
			HandoffID:               "hnd_a",
			TransitionKind:          transition.KindHandoffLaunch,
			HandoffContinuityBefore: "ACCEPTED_NOT_LAUNCHED",
			HandoffContinuityAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
			CreatedAt:               base.Add(4 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_120",
			TaskID:                  taskID,
			HandoffID:               "hnd_a",
			TransitionKind:          transition.KindHandoffLaunch,
			HandoffContinuityBefore: "ACCEPTED_NOT_LAUNCHED",
			HandoffContinuityAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
			CreatedAt:               base.Add(3 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_110",
			TaskID:                  taskID,
			HandoffID:               "hnd_a",
			TransitionKind:          transition.KindHandoffResolution,
			HandoffContinuityBefore: "FOLLOW_THROUGH_STALLED",
			HandoffContinuityAfter:  "RESOLVED",
			CreatedAt:               base.Add(2 * time.Second),
		},
		{
			Version:                 1,
			ReceiptID:               "ctr_100",
			TaskID:                  taskID,
			HandoffID:               "hnd_b",
			TransitionKind:          transition.KindHandoffLaunch,
			HandoffContinuityBefore: "ACCEPTED_NOT_LAUNCHED",
			HandoffContinuityAfter:  "LAUNCH_COMPLETED_ACK_CAPTURED",
			CreatedAt:               base.Add(1 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	anchor, err := repo.GetByTaskReceipt(taskID, "ctr_120")
	if err != nil {
		t.Fatalf("get transition receipt anchor: %v", err)
	}
	if anchor.ReceiptID != "ctr_120" {
		t.Fatalf("expected anchor ctr_120, got %+v", anchor)
	}

	filteredPage, err := repo.ListByTaskFiltered(taskID, transition.ReceiptListFilter{
		Limit:           2,
		BeforeReceiptID: anchor.ReceiptID,
		BeforeCreatedAt: anchor.CreatedAt,
	})
	if err != nil {
		t.Fatalf("list filtered transition receipts page: %v", err)
	}
	if len(filteredPage) != 2 || filteredPage[0].ReceiptID != "ctr_110" || filteredPage[1].ReceiptID != "ctr_100" {
		t.Fatalf("unexpected filtered pagination ordering: %+v", filteredPage)
	}

	kindFiltered, err := repo.ListByTaskFiltered(taskID, transition.ReceiptListFilter{
		Limit:          10,
		TransitionKind: transition.KindHandoffResolution,
	})
	if err != nil {
		t.Fatalf("list transition receipts filtered by kind: %v", err)
	}
	if len(kindFiltered) != 1 || kindFiltered[0].ReceiptID != "ctr_110" {
		t.Fatalf("unexpected kind-filtered transition receipts: %+v", kindFiltered)
	}

	handoffFiltered, err := repo.ListByTaskFiltered(taskID, transition.ReceiptListFilter{
		Limit:     10,
		HandoffID: "hnd_b",
	})
	if err != nil {
		t.Fatalf("list transition receipts filtered by handoff: %v", err)
	}
	if len(handoffFiltered) != 1 || handoffFiltered[0].ReceiptID != "ctr_100" {
		t.Fatalf("unexpected handoff-filtered transition receipts: %+v", handoffFiltered)
	}
}

func TestContinuityTransitionReceiptListByTaskAfterSupportsDeterministicForwardWindow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "continuity-transition-receipts-after.db")
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer store.Close()

	repo := store.TransitionReceipts()
	taskID := common.TaskID("tsk_transition_receipts_after")
	base := time.Unix(1713600000, 0).UTC()
	records := []transition.Receipt{
		{
			Version:        1,
			ReceiptID:      "ctr_a",
			TaskID:         taskID,
			TransitionKind: transition.KindHandoffLaunch,
			CreatedAt:      base.Add(1 * time.Second),
		},
		{
			Version:        1,
			ReceiptID:      "ctr_b",
			TaskID:         taskID,
			TransitionKind: transition.KindHandoffLaunch,
			CreatedAt:      base.Add(2 * time.Second),
		},
		{
			Version:        1,
			ReceiptID:      "ctr_c",
			TaskID:         taskID,
			TransitionKind: transition.KindHandoffResolution,
			CreatedAt:      base.Add(2 * time.Second),
		},
		{
			Version:        1,
			ReceiptID:      "ctr_d",
			TaskID:         taskID,
			TransitionKind: transition.KindHandoffResolution,
			CreatedAt:      base.Add(3 * time.Second),
		},
	}
	for _, record := range records {
		if err := repo.Create(record); err != nil {
			t.Fatalf("create transition receipt %s: %v", record.ReceiptID, err)
		}
	}

	page, err := repo.ListByTaskAfter(taskID, "ctr_b", base.Add(2*time.Second), 10)
	if err != nil {
		t.Fatalf("list transition receipts after anchor: %v", err)
	}
	if len(page) != 2 || page[0].ReceiptID != "ctr_c" || page[1].ReceiptID != "ctr_d" {
		t.Fatalf("unexpected forward window ordering after anchor: %+v", page)
	}

	limited, err := repo.ListByTaskAfter(taskID, "ctr_b", base.Add(2*time.Second), 1)
	if err != nil {
		t.Fatalf("list transition receipts after anchor with limit: %v", err)
	}
	if len(limited) != 1 || limited[0].ReceiptID != "ctr_c" {
		t.Fatalf("unexpected limited forward window ordering after anchor: %+v", limited)
	}
}
