package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"tuku/internal/domain/common"
)

const DefaultRepoContinueGoal = "Continue work in this repository"

type ResolveShellTaskResult struct {
	TaskID   common.TaskID
	RepoRoot string
	Created  bool
}

func (c *Coordinator) ResolveShellTaskForRepo(ctx context.Context, repoRoot string, defaultGoal string) (ResolveShellTaskResult, error) {
	root := filepath.Clean(strings.TrimSpace(repoRoot))
	if root == "" {
		return ResolveShellTaskResult{}, fmt.Errorf("repo root is required")
	}

	caps, err := c.store.Capsules().LatestByRepoRoot(root)
	if err == nil {
		return ResolveShellTaskResult{
			TaskID:   caps.TaskID,
			RepoRoot: caps.RepoRoot,
			Created:  false,
		}, nil
	}
	if !errorsIsNoRows(err) {
		return ResolveShellTaskResult{}, err
	}

	goal := strings.TrimSpace(defaultGoal)
	if goal == "" {
		goal = DefaultRepoContinueGoal
	}
	started, err := c.StartTask(ctx, goal, root)
	if err != nil {
		return ResolveShellTaskResult{}, err
	}
	return ResolveShellTaskResult{
		TaskID:   started.TaskID,
		RepoRoot: started.RepoAnchor.RepoRoot,
		Created:  true,
	}, nil
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
