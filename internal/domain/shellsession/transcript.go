package shellsession

import (
	"time"

	"tuku/internal/domain/common"
)

const (
	DefaultTranscriptRetentionChunks = 200
	MaxTranscriptChunkChars          = 1200
)

type TranscriptSource string

const (
	TranscriptSourceWorkerOutput TranscriptSource = "worker_output"
	TranscriptSourceSystemNote   TranscriptSource = "system_note"
	TranscriptSourceFallback     TranscriptSource = "fallback_note"
)

type TranscriptState string

const (
	TranscriptStateNone                    TranscriptState = "none"
	TranscriptStateBoundedAvailable        TranscriptState = "bounded_available"
	TranscriptStateBoundedPartial          TranscriptState = "bounded_partial"
	TranscriptStateTranscriptOnlyAvailable TranscriptState = "transcript_only_bounded_available"
	TranscriptStateTranscriptOnlyPartial   TranscriptState = "transcript_only_bounded_partial"
)

type TranscriptChunk struct {
	ChunkID    common.EventID
	TaskID     common.TaskID
	SessionID  string
	SequenceNo int64
	Source     TranscriptSource
	Content    string
	CreatedAt  time.Time
}

type TranscriptSourceCount struct {
	Source TranscriptSource
	Chunks int
}

type TranscriptSummary struct {
	TaskID           common.TaskID
	SessionID        string
	RetainedChunks   int
	DroppedChunks    int
	RetentionLimit   int
	LastSequenceNo   int64
	LastChunkAt      time.Time
	OldestSequenceNo int64
	OldestChunkAt    time.Time
	NewestSequenceNo int64
	NewestChunkAt    time.Time
	SourceCounts     []TranscriptSourceCount
}
