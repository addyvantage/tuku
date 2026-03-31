package orchestrator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tuku/internal/domain/common"
	"tuku/internal/domain/intent"
	"tuku/internal/domain/repoindex"
)

const repoIndexMaxFileBytes int64 = 256 * 1024

func (c *Coordinator) resolveRepoIndex(repoRoot string, headSHA string, workingTreeDirty bool) (repoindex.Snapshot, error) {
	repoRoot = normalizedRepoRoot(repoRoot)
	headSHA = strings.TrimSpace(headSHA)
	if !workingTreeDirty && headSHA != "" {
		snapshot, err := c.store.RepoIndexes().GetByRepoHead(repoRoot, headSHA)
		if err == nil {
			return snapshot, nil
		}
		if err != nil && !errorsIsNoRows(err) {
			return repoindex.Snapshot{}, err
		}
	}
	if !workingTreeDirty {
		snapshot, err := c.store.RepoIndexes().LatestByRepo(repoRoot)
		if err == nil && (headSHA == "" || strings.TrimSpace(snapshot.HeadSHA) == headSHA) {
			return snapshot, nil
		}
		if err != nil && !errorsIsNoRows(err) {
			return repoindex.Snapshot{}, err
		}
	}

	snapshot, err := buildPersistentRepoIndex(repoRoot, headSHA, c.clock(), common.RepoIndexID(c.idGenerator("ridx")))
	if err != nil {
		return repoindex.Snapshot{}, err
	}
	if err := c.store.RepoIndexes().Save(snapshot); err != nil {
		return repoindex.Snapshot{}, err
	}
	return snapshot, nil
}

func buildPersistentRepoIndex(repoRoot string, headSHA string, now time.Time, repoIndexID common.RepoIndexID) (repoindex.Snapshot, error) {
	repoRoot = normalizedRepoRoot(repoRoot)
	files := make([]repoindex.File, 0, 128)
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".svn", ".hg", "node_modules", "vendor", "dist", "build", ".next", ".turbo", ".cache", "coverage", "tmp":
				if path != repoRoot {
					return fs.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !isPromptTriageCandidateFile(rel) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > repoIndexMaxFileBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || isBinaryLike(data) {
			return nil
		}
		text := string(data)
		kinds := inferIndexedKinds(rel, text)
		symbols := inferIndexedSymbols(text)
		files = append(files, repoindex.File{
			Path:          rel,
			TokenEstimate: estimateTextTokens(text),
			Kinds:         kinds,
			Symbols:       symbols,
			SearchTerms:   inferIndexedSearchTerms(rel, kinds, symbols),
		})
		return nil
	})
	if err != nil {
		return repoindex.Snapshot{}, fmt.Errorf("build repo index: %w", err)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	snapshot := repoindex.Snapshot{
		RepoIndexID: common.RepoIndexID(strings.TrimSpace(string(repoIndexID))),
		RepoRoot:    repoRoot,
		HeadSHA:     headSHA,
		Files:       files,
		BuiltAt:     now,
	}
	deriveRepoIndexCounts(&snapshot)
	return snapshot, nil
}

func inferIndexedSearchTerms(path string, kinds []string, symbols []string) []string {
	raw := []string{strings.ToLower(filepath.ToSlash(path))}
	raw = append(raw, kinds...)
	raw = append(raw, symbols...)
	terms := make([]string, 0, 24)
	for _, item := range raw {
		terms = append(terms, promptTriageTermsFromText(strings.ToLower(item))...)
	}
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(base, "index") {
		terms = append(terms, "index")
	}
	return dedupeNonEmpty(terms, 24)
}

func deriveRepoIndexCounts(snapshot *repoindex.Snapshot) {
	if snapshot == nil {
		return
	}
	snapshot.FileCount = len(snapshot.Files)
	totalTokens := 0
	symbolCount := 0
	routeCount := 0
	componentCount := 0
	testCount := 0
	for _, file := range snapshot.Files {
		totalTokens += file.TokenEstimate
		symbolCount += len(file.Symbols)
		if containsFold(file.Kinds, "route") {
			routeCount++
		}
		if containsFold(file.Kinds, "component") {
			componentCount++
		}
		if containsFold(file.Kinds, "test") {
			testCount++
		}
	}
	snapshot.TotalTokenEstimate = totalTokens
	snapshot.SymbolCount = symbolCount
	snapshot.RouteCount = routeCount
	snapshot.ComponentCount = componentCount
	snapshot.TestCount = testCount
}

func rankPromptTriageCandidatesFromIndex(index repoindex.Snapshot, terms []string, class intent.Class) ([]promptTriageCandidate, int, int) {
	candidates := make([]promptTriageCandidate, 0, len(index.Files))
	for _, file := range index.Files {
		score := scorePromptTriageIndexCandidate(file, terms, class)
		if score <= 0 {
			continue
		}
		candidates = append(candidates, promptTriageCandidate{
			Path:          file.Path,
			Score:         score,
			TokenEstimate: file.TokenEstimate,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Score > candidates[j].Score
	})
	return candidates, index.FileCount, index.TotalTokenEstimate
}

func scorePromptTriageIndexCandidate(file repoindex.File, terms []string, class intent.Class) int {
	pathLower := strings.ToLower(filepath.ToSlash(file.Path))
	score := 0
	for _, term := range dedupeNonEmpty(terms, 16) {
		if term == "" {
			continue
		}
		if containsPathToken(pathLower, term) {
			score += 12
		} else if strings.Contains(pathLower, term) {
			score += 7
		}
		if stringSliceContainsFold(file.SearchTerms, term) {
			score += 4
		}
		if stringSliceContainsFold(file.Symbols, term) {
			score += 4
		}
	}
	ext := strings.ToLower(filepath.Ext(file.Path))
	if promptTriageHasFrontendSignal(terms) {
		if promptTriageLooksFrontendPath(pathLower) {
			score += 8
		}
		if isPromptTriageFrontendExt(ext) {
			score += 5
		}
	}
	if promptTriageHasBackendSignal(terms) {
		if promptTriageLooksBackendPath(pathLower) {
			score += 8
		}
		if ext == ".go" || ext == ".ts" || ext == ".js" || ext == ".py" {
			score += 3
		}
	}
	if class == intent.ClassDebug && (strings.Contains(pathLower, "bug") || strings.Contains(pathLower, "error") || strings.Contains(pathLower, "fix")) {
		score += 3
	}
	return score
}

func repoIndexSummary(index repoindex.Snapshot) string {
	if index.FileCount == 0 {
		return ""
	}
	return fmt.Sprintf("files=%d symbols=%d components=%d routes=%d tests=%d", index.FileCount, index.SymbolCount, index.ComponentCount, index.RouteCount, index.TestCount)
}

func relatedIndexTestFiles(index repoindex.Snapshot, targetFiles []string, limit int) []string {
	if limit <= 0 || len(index.Files) == 0 {
		return nil
	}
	type scored struct {
		path  string
		score int
	}
	targetFiles = uniqueStrings(targetFiles)
	targetDescriptors := make([]struct {
		pathLower string
		dirLower  string
		stemLower string
	}, 0, len(targetFiles))
	for _, file := range targetFiles {
		pathLower := strings.ToLower(filepath.ToSlash(file))
		dirLower := strings.ToLower(filepath.ToSlash(filepath.Dir(file)))
		stemLower := strings.ToLower(trimKnownFileExtensions(filepath.Base(file)))
		targetDescriptors = append(targetDescriptors, struct {
			pathLower string
			dirLower  string
			stemLower string
		}{pathLower: pathLower, dirLower: dirLower, stemLower: stemLower})
	}
	scoredFiles := make([]scored, 0, len(index.Files))
	for _, file := range index.Files {
		if !containsFold(file.Kinds, "test") {
			continue
		}
		pathLower := strings.ToLower(filepath.ToSlash(file.Path))
		score := 0
		for _, target := range targetDescriptors {
			if pathLower == target.pathLower {
				score = max(score, 100)
				continue
			}
			if strings.Contains(pathLower, target.stemLower) {
				score += 12
			}
			if strings.HasPrefix(pathLower, target.dirLower+"/") || filepath.Dir(pathLower) == target.dirLower {
				score += 6
			}
			if dirBase := filepath.Base(target.dirLower); dirBase != "." && dirBase != "/" && strings.Contains(pathLower, dirBase) {
				score += 3
			}
		}
		if score > 0 {
			scoredFiles = append(scoredFiles, scored{path: file.Path, score: score})
		}
	}
	sort.SliceStable(scoredFiles, func(i, j int) bool {
		if scoredFiles[i].score == scoredFiles[j].score {
			return scoredFiles[i].path < scoredFiles[j].path
		}
		return scoredFiles[i].score > scoredFiles[j].score
	})
	out := make([]string, 0, min(limit, len(scoredFiles)))
	seen := map[string]struct{}{}
	for _, item := range scoredFiles {
		if _, ok := seen[item.path]; ok {
			continue
		}
		seen[item.path] = struct{}{}
		out = append(out, item.path)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func trimKnownFileExtensions(name string) string {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".test.tsx", ".test.ts", ".test.jsx", ".test.js", ".spec.tsx", ".spec.ts", ".spec.jsx", ".spec.js", "_test.go", ".tsx", ".ts", ".jsx", ".js", ".go", ".py", ".rb", ".java", ".kt", ".swift", ".rs"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSuffix(lower, suffix)
		}
	}
	ext := filepath.Ext(lower)
	return strings.TrimSuffix(lower, ext)
}
