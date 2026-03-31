package common

import "time"

type TaskID string
type ConversationID string
type MessageID string
type IntentID string
type BriefID string
type EventID string
type CheckpointID string
type ContextPackID string
type MemoryID string
type DecisionID string
type BenchmarkID string
type RunID string
type WorkerRunID string
type CapsuleVersion int64

type AuditRef struct {
	EventIDs []EventID `json:"event_ids"`
}

type TimeWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
