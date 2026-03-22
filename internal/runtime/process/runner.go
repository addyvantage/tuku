package process

import "context"

type Spec struct {
	Command    string
	Args       []string
	WorkingDir string
	Env        []string
	Stdin      string
}

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type Runner interface {
	Run(ctx context.Context, spec Spec) (Result, error)
}
