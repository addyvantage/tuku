package sqlite

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"tuku/internal/domain/capsule"
	"tuku/internal/domain/common"
)

func (r *capsuleRepo) LatestByRepoRoot(repoRoot string) (capsule.WorkCapsule, error) {
	root := filepath.Clean(strings.TrimSpace(repoRoot))
	row := r.q.QueryRow(`
SELECT task_id, conversation_id, version, created_at, updated_at, goal,
	acceptance_criteria_json, constraints_json,
	repo_root, worktree_path, branch_name, head_sha, working_tree_dirty, anchor_captured_at,
	current_phase, status, current_intent_id, current_brief_id,
	touched_files_json, blockers_json, next_action,
	parent_task_id, child_task_ids_json, edge_refs_json
FROM capsules
WHERE repo_root = ?
ORDER BY CASE WHEN status = 'ACTIVE' THEN 0 ELSE 1 END, updated_at DESC, task_id DESC
LIMIT 1
`, root)
	return scanCapsuleRow(row)
}

func scanCapsuleRow(row *sql.Row) (capsule.WorkCapsule, error) {
	var c capsule.WorkCapsule
	var createdAt, updatedAt, anchorCapturedAt string
	var phaseStr string
	var acceptanceJSON, constraintsJSON, touchedJSON, blockersJSON, childJSON, edgesJSON string
	var dirtyInt int
	var parentTask sql.NullString

	err := row.Scan(
		&c.TaskID, &c.ConversationID, &c.Version, &createdAt, &updatedAt, &c.Goal,
		&acceptanceJSON, &constraintsJSON,
		&c.RepoRoot, &c.WorktreePath, &c.BranchName, &c.HeadSHA, &dirtyInt, &anchorCapturedAt,
		&phaseStr, &c.Status, &c.CurrentIntentID, &c.CurrentBriefID,
		&touchedJSON, &blockersJSON, &c.NextAction,
		&parentTask, &childJSON, &edgesJSON,
	)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}

	c.CreatedAt, err = parseSQLiteTimestamp("capsule created_at", createdAt)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.UpdatedAt, err = parseSQLiteTimestamp("capsule updated_at", updatedAt)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	if anchorCapturedAt != "" {
		c.AnchorCapturedAt, err = parseSQLiteTimestamp("capsule anchor_captured_at", anchorCapturedAt)
		if err != nil {
			return capsule.WorkCapsule{}, err
		}
	}
	c.WorkingTreeDirty = dirtyInt == 1
	c.CurrentPhase = parsePhase(phaseStr)
	c.AcceptanceCriteria, err = unmarshalStringSlice(acceptanceJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.Constraints, err = unmarshalStringSlice(constraintsJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.TouchedFiles, err = unmarshalStringSlice(touchedJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.Blockers, err = unmarshalStringSlice(blockersJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.ChildTaskIDs, err = unmarshalTaskSlice(childJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	c.EdgeRefs, err = unmarshalStringSlice(edgesJSON)
	if err != nil {
		return capsule.WorkCapsule{}, err
	}
	if parentTask.Valid {
		p := common.TaskID(parentTask.String)
		c.ParentTaskID = &p
	}
	return c, nil
}

func parseSQLiteTimestamp(label string, raw string) (time.Time, error) {
	parsed, err := time.Parse(sqliteTimestampLayout, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s: %w", label, err)
	}
	return parsed, nil
}
