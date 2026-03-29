package policy

import (
	"time"

	"tuku/internal/domain/common"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
)

type DecisionStatus string

const (
	DecisionPending  DecisionStatus = "PENDING"
	DecisionApproved DecisionStatus = "APPROVED"
	DecisionRejected DecisionStatus = "REJECTED"
)

type Decision struct {
	DecisionID      common.DecisionID `json:"decision_id"`
	TaskID          common.TaskID     `json:"task_id"`
	OperationType   string            `json:"operation_type"`
	RiskLevel       RiskLevel         `json:"risk_level"`
	RequestedAt     time.Time         `json:"requested_at"`
	ResolvedAt      *time.Time        `json:"resolved_at,omitempty"`
	ResolvedBy      string            `json:"resolved_by,omitempty"`
	Status          DecisionStatus    `json:"status"`
	Reason          string            `json:"reason,omitempty"`
	ScopeDescriptor string            `json:"scope_descriptor"`
}

type Engine interface {
	Evaluate(operationType string, scopeDescriptor string) (Decision, error)
}
