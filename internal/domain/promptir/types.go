package promptir

import "time"

type SerializerKind string

const (
	SerializerNaturalLanguage SerializerKind = "natural_language"
	SerializerStructured      SerializerKind = "structured_json"
)

type TargetKind string

const (
	TargetFile      TargetKind = "file"
	TargetSymbol    TargetKind = "symbol"
	TargetRoute     TargetKind = "route"
	TargetComponent TargetKind = "component"
	TargetTest      TargetKind = "test"
)

type Target struct {
	Path    string     `json:"path,omitempty"`
	Name    string     `json:"name,omitempty"`
	Kind    TargetKind `json:"kind,omitempty"`
	Score   int        `json:"score,omitempty"`
	Reasons []string   `json:"reasons,omitempty"`
}

type ValidatorPlan struct {
	Summary   string   `json:"summary,omitempty"`
	Strategy  string   `json:"strategy,omitempty"`
	Commands  []string `json:"commands,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
	Estimated string   `json:"estimated,omitempty"`
}

type ConfidenceScore struct {
	Value  float64 `json:"value,omitempty"`
	Level  string  `json:"level,omitempty"`
	Reason string  `json:"reason,omitempty"`
}

type Packet struct {
	Version int `json:"version"`

	UserGoal              string          `json:"user_goal,omitempty"`
	NormalizedTaskType    string          `json:"normalized_task_type,omitempty"`
	Objective             string          `json:"objective,omitempty"`
	Operation             string          `json:"operation,omitempty"`
	ScopeSummary          string          `json:"scope_summary,omitempty"`
	RankedTargets         []Target        `json:"ranked_targets,omitempty"`
	OperationPlan         []string        `json:"operation_plan,omitempty"`
	Constraints           []string        `json:"constraints,omitempty"`
	NonGoals              []string        `json:"non_goals,omitempty"`
	ValidatorPlan         ValidatorPlan   `json:"validator_plan,omitempty"`
	MemoryDigest          string          `json:"memory_digest,omitempty"`
	OutputContract        []string        `json:"output_contract,omitempty"`
	Confidence            ConfidenceScore `json:"confidence,omitempty"`
	EvidenceRequirements  []string        `json:"evidence_requirements,omitempty"`
	NaturalLanguageTokens int             `json:"natural_language_tokens,omitempty"`
	StructuredTokens      int             `json:"structured_tokens,omitempty"`
	StructuredCheaper     bool            `json:"structured_cheaper,omitempty"`
	DefaultSerializer     SerializerKind  `json:"default_serializer,omitempty"`
	CompiledAt            time.Time       `json:"compiled_at,omitempty"`
}
