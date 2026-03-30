package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/ipc"
)

const shellTranscriptFlushInterval = 1200 * time.Millisecond

type TranscriptEvidenceChunk struct {
	Source    string
	Content   string
	CreatedAt time.Time
}

type TranscriptProvider interface {
	DrainTranscriptEvidence(limit int) []TranscriptEvidenceChunk
}

type TranscriptSink interface {
	Append(taskID string, sessionID string, chunks []TranscriptEvidenceChunk) error
}

type IPCTranscriptSink struct {
	SocketPath string
	Timeout    time.Duration
}

func NewIPCTranscriptSink(socketPath string) *IPCTranscriptSink {
	return &IPCTranscriptSink{
		SocketPath: socketPath,
		Timeout:    5 * time.Second,
	}
}

func (s *IPCTranscriptSink) Append(taskID string, sessionID string, chunks []TranscriptEvidenceChunk) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(sessionID) == "" || len(chunks) == 0 {
		return nil
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	payloadChunks := make([]ipc.TaskShellTranscriptChunkAppend, 0, len(chunks))
	for _, chunk := range chunks {
		content := strings.TrimSpace(chunk.Content)
		if content == "" {
			continue
		}
		payloadChunks = append(payloadChunks, ipc.TaskShellTranscriptChunkAppend{
			Source:    strings.TrimSpace(chunk.Source),
			Content:   content,
			CreatedAt: chunk.CreatedAt,
		})
	}
	if len(payloadChunks) == 0 {
		return nil
	}
	payload, err := json.Marshal(ipc.TaskShellTranscriptAppendRequest{
		TaskID:    common.TaskID(strings.TrimSpace(taskID)),
		SessionID: strings.TrimSpace(sessionID),
		Chunks:    payloadChunks,
	})
	if err != nil {
		return err
	}
	_, err = ipc.CallUnix(ctx, s.SocketPath, ipc.Request{
		RequestID: fmt.Sprintf("shell_transcript_%d", time.Now().UTC().UnixNano()),
		Method:    ipc.MethodTaskShellTranscriptAppend,
		Payload:   payload,
	})
	return err
}

