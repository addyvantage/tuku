package repoindex

import (
	"time"

	"tuku/internal/domain/common"
)

type File struct {
	Path          string   `json:"path,omitempty"`
	TokenEstimate int      `json:"token_estimate,omitempty"`
	Kinds         []string `json:"kinds,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
	SearchTerms   []string `json:"search_terms,omitempty"`
}

type Snapshot struct {
	RepoIndexID        common.RepoIndexID `json:"repo_index_id,omitempty"`
	RepoRoot           string             `json:"repo_root,omitempty"`
	HeadSHA            string             `json:"head_sha,omitempty"`
	Files              []File             `json:"files,omitempty"`
	FileCount          int                `json:"file_count,omitempty"`
	SymbolCount        int                `json:"symbol_count,omitempty"`
	RouteCount         int                `json:"route_count,omitempty"`
	ComponentCount     int                `json:"component_count,omitempty"`
	TestCount          int                `json:"test_count,omitempty"`
	TotalTokenEstimate int                `json:"total_token_estimate,omitempty"`
	BuiltAt            time.Time          `json:"built_at,omitempty"`
}
