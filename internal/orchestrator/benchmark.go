package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tuku/internal/domain/benchmark"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/common"
	"tuku/internal/domain/promptir"
	rundomain "tuku/internal/domain/run"
)

func (c *Coordinator) updateBenchmarkOutcome(briefRec brief.ExecutionBrief, runRec rundomain.ExecutionRun) error {
	if briefRec.BenchmarkID == "" {
		return nil
	}
	record, err := c.store.Benchmarks().Get(briefRec.BenchmarkID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	record.BriefID = briefRec.BriefID
	record.RunID = runRec.RunID
	record.ChangedFiles = append([]string{}, runRec.ChangedFiles...)
	record.CandidateRecallAt3 = candidateRecallAtK(briefRec.PromptIR.RankedTargets, runRec.ChangedFiles, 3)
	record.UpdatedAt = c.clock()
	return c.store.Benchmarks().Save(record)
}

func candidateRecallAtK(targets []promptir.Target, changedFiles []string, k int) float64 {
	if len(changedFiles) == 0 || k <= 0 {
		return 0
	}
	topFiles := map[string]struct{}{}
	for _, target := range targets {
		if strings.TrimSpace(target.Path) == "" {
			continue
		}
		topFiles[strings.ToLower(strings.TrimSpace(target.Path))] = struct{}{}
		if len(topFiles) >= k {
			break
		}
	}
	if len(topFiles) == 0 {
		return 0
	}
	hits := 0
	seen := map[string]struct{}{}
	for _, file := range changedFiles {
		key := strings.ToLower(strings.TrimSpace(file))
		if key == "" {
			continue
		}
		if _, done := seen[key]; done {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := topFiles[key]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(seen))
}

type BenchmarkTaskResult struct {
	TaskID        common.TaskID
	Benchmark     *benchmark.Run
	Brief         *brief.ExecutionBrief
	CompiledBrief *CompiledBriefSummary
}

type ReadBenchmarkRequest struct {
	TaskID string
}

func (c *Coordinator) loadBenchmarkForTask(taskID common.TaskID, benchmarkID common.BenchmarkID) (*benchmark.Run, error) {
	if benchmarkID != "" {
		record, err := c.store.Benchmarks().Get(benchmarkID)
		if err == nil {
			recordCopy := record
			return &recordCopy, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	record, err := c.store.Benchmarks().LatestByTask(taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	recordCopy := record
	return &recordCopy, nil
}

func (c *Coordinator) ReadBenchmark(ctx context.Context, req ReadBenchmarkRequest) (BenchmarkTaskResult, error) {
	_ = ctx
	taskID := common.TaskID(strings.TrimSpace(req.TaskID))
	if taskID == "" {
		return BenchmarkTaskResult{}, fmt.Errorf("task id is required")
	}
	caps, err := c.store.Capsules().Get(taskID)
	if err != nil {
		return BenchmarkTaskResult{}, err
	}
	out := BenchmarkTaskResult{TaskID: taskID}
	benchmarkID := common.BenchmarkID("")
	if caps.CurrentBriefID != "" {
		if briefRecord, err := c.store.Briefs().Get(caps.CurrentBriefID); err == nil {
			briefCopy := briefRecord
			out.Brief = &briefCopy
			out.CompiledBrief = compiledBriefSummaryFromBrief(briefCopy)
			benchmarkID = briefRecord.BenchmarkID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return BenchmarkTaskResult{}, err
		}
	}
	record, err := c.loadBenchmarkForTask(taskID, benchmarkID)
	if err != nil {
		return BenchmarkTaskResult{}, err
	}
	out.Benchmark = record
	return out, nil
}
