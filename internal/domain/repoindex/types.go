package repoindex

import "time"

type File struct {
	Path          string   `json:"path,omitempty"`
	TokenEstimate int      `json:"token_estimate,omitempty"`
	Kinds         []string `json:"kinds,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
}

type Snapshot struct {
	RepoRoot string    `json:"repo_root,omitempty"`
	HeadSHA  string    `json:"head_sha,omitempty"`
	Files    []File    `json:"files,omitempty"`
	BuiltAt  time.Time `json:"built_at,omitempty"`
}
