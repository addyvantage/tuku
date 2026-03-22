package conversation

import (
	"time"

	"tuku/internal/domain/common"
)

type Role string

const (
	RoleUser   Role = "user"
	RoleSystem Role = "system"
	RoleWorker Role = "worker"
)

type Message struct {
	MessageID      common.MessageID      `json:"message_id"`
	ConversationID common.ConversationID `json:"conversation_id"`
	TaskID         common.TaskID         `json:"task_id"`
	Role           Role                  `json:"role"`
	Body           string                `json:"body"`
	CreatedAt      time.Time             `json:"created_at"`
}

type Repository interface {
	Append(message Message) error
	ListRecent(conversationID common.ConversationID, limit int) ([]Message, error)
}
