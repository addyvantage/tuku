package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"tuku/internal/adapters/adapter_contract"
	"tuku/internal/domain/brief"
	"tuku/internal/domain/repoindex"
	gitdiff "tuku/internal/git/diff"
	gitworktree "tuku/internal/git/worktree"
	"tuku/internal/runtime/process"
	"tuku/internal/storage/fileblob"
)

type validationCommandResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
}

func (c *Coordinator) enrichExecutionResultWithValidation(ctx context.Context, prepared *preparedRealRun, execResult adapter_contract.ExecutionResult) adapter_contract.ExecutionResult {
	execResult = c.enrichExecutionResultWithRepoEvidence(prepared, execResult)
	outcome := c.runPostExecutionValidation(ctx, prepared, execResult)
	execResult.ValidationSignals = mergeSignals(execResult.ValidationSignals, outcome.signals)
	if execResult.OutputArtifactRef == "" && outcome.artifactRef != "" {
		execResult.OutputArtifactRef = outcome.artifactRef
	}
	return execResult
}

type validationOutcome struct {
	signals     []string
	artifactRef string
}

func (c *Coordinator) enrichExecutionResultWithRepoEvidence(prepared *preparedRealRun, execResult adapter_contract.ExecutionResult) adapter_contract.ExecutionResult {
	diffSummary, worktreeSummary := captureRepoEvidence(prepared.Capsule.RepoRoot)
	if diffSummary != "" || worktreeSummary != "" {
		execResult.StructuredSummary = mergeStructuredSummary(execResult.StructuredSummary, map[string]any{
			"repo_diff_summary": diffSummary,
			"worktree_summary":  worktreeSummary,
		})
	}
	return execResult
}

func (c *Coordinator) runPostExecutionValidation(ctx context.Context, prepared *preparedRealRun, execResult adapter_contract.ExecutionResult) validationOutcome {
	commands := c.validationCommandsForRun(prepared, execResult)
	if len(commands) == 0 {
		return validationOutcome{signals: []string{"no automatic validator matched the current bounded run evidence"}}
	}

	runner := process.NewLocalRunner()
	results := make([]validationCommandResult, 0, len(commands))
	signals := []string{}
	for _, spec := range commands {
		var result validationCommandResult
		if spec.Command == "__tuku_json_validation__" {
			result = validationCommandResult{
				Command:  spec.Args[0],
				Stdout:   spec.Args[1],
				Stderr:   spec.Args[2],
				ExitCode: parseExitCodeArg(spec.Args[3]),
			}
		} else {
			res, err := runner.Run(ctx, spec)
			result = validationCommandResult{
				Command:  strings.Join(append([]string{spec.Command}, spec.Args...), " "),
				ExitCode: res.ExitCode,
				Stdout:   res.Stdout,
				Stderr:   res.Stderr,
			}
			if err != nil {
				result.Stderr = strings.TrimSpace(strings.Join([]string{result.Stderr, err.Error()}, "\n"))
				if result.ExitCode == 0 {
					result.ExitCode = -1
				}
			}
		}
		results = append(results, result)
		signals = append(signals, signalsFromValidationResult(result)...)
	}

	report := renderValidationReport(prepared, results)
	artifactRef := writeValidationArtifact(string(prepared.TaskID), string(prepared.RunID), report)
	if artifactRef != "" {
		signals = append(signals, fmt.Sprintf("validation artifact saved: %s", artifactRef))
	}
	return validationOutcome{signals: signals, artifactRef: artifactRef}
}

func (c *Coordinator) validationCommandsForRun(prepared *preparedRealRun, execResult adapter_contract.ExecutionResult) []process.Spec {
	repoRoot := normalizedRepoRoot(prepared.Capsule.RepoRoot)
	targetFiles := validationTargetFiles(prepared, execResult)
	index, err := c.resolveRepoIndex(prepared.Capsule.RepoRoot, prepared.Capsule.HeadSHA, prepared.Capsule.WorkingTreeDirty)
	if err != nil {
		index = repoindex.Snapshot{}
	}
	return plannedValidationSpecs(repoRoot, prepared.Brief.Posture, index, targetFiles)
}

func signalsFromValidationResult(result validationCommandResult) []string {
	signals := []string{}
	switch {
	case strings.HasPrefix(result.Command, "gofmt -l"):
		files := splitNonEmptyLines(result.Stdout)
		if len(files) == 0 && result.ExitCode == 0 {
			signals = append(signals, "validation: gofmt reported no formatting drift")
		} else if len(files) > 0 {
			signals = append(signals, fmt.Sprintf("validation: gofmt reported %d unformatted file(s)", len(files)))
		} else {
			signals = append(signals, fmt.Sprintf("validation: gofmt failed with exit code %d", result.ExitCode))
		}
	case strings.HasPrefix(result.Command, "go test"):
		if result.ExitCode == 0 {
			signals = append(signals, "validation: go test passed")
		} else {
			signals = append(signals, fmt.Sprintf("validation: go test failed with exit code %d", result.ExitCode))
		}
	case strings.HasPrefix(result.Command, "git diff --check"):
		if result.ExitCode == 0 {
			signals = append(signals, "validation: git diff --check reported no diff hygiene issues")
		} else {
			signals = append(signals, fmt.Sprintf("validation: git diff --check failed with exit code %d", result.ExitCode))
		}
	case strings.HasPrefix(result.Command, "json-validate "):
		if result.ExitCode == 0 {
			signals = append(signals, fmt.Sprintf("validation: %s passed", result.Command))
		} else {
			signals = append(signals, fmt.Sprintf("validation: %s failed", result.Command))
		}
	default:
		if result.ExitCode == 0 {
			signals = append(signals, fmt.Sprintf("validation: %s passed", result.Command))
		} else {
			signals = append(signals, fmt.Sprintf("validation: %s failed with exit code %d", result.Command, result.ExitCode))
		}
	}
	return signals
}

func renderValidationReport(prepared *preparedRealRun, results []validationCommandResult) string {
	lines := []string{
		fmt.Sprintf("task: %s", prepared.TaskID),
		fmt.Sprintf("run: %s", prepared.RunID),
		fmt.Sprintf("brief: %s", prepared.Brief.BriefID),
		fmt.Sprintf("context_pack: %s", prepared.ContextPack.ContextPackID),
	}
	for _, result := range results {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("$ %s", result.Command))
		lines = append(lines, fmt.Sprintf("exit_code: %d", result.ExitCode))
		if strings.TrimSpace(result.Stdout) != "" {
			lines = append(lines, "stdout:")
			lines = append(lines, strings.TrimSpace(result.Stdout))
		}
		if strings.TrimSpace(result.Stderr) != "" {
			lines = append(lines, "stderr:")
			lines = append(lines, strings.TrimSpace(result.Stderr))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeValidationArtifact(taskID, runID string, report string) string {
	root, err := defaultArtifactRoot()
	if err != nil {
		return ""
	}
	store := fileblob.NewStore(root)
	path, err := store.WriteText([]string{"artifacts", string(taskID), string(runID), "validation.txt"}, report)
	if err != nil {
		return ""
	}
	return path
}

func defaultArtifactRoot() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("TUKU_CACHE_DIR")); configured != "" {
		return filepath.Clean(configured), nil
	}
	if configured := strings.TrimSpace(os.Getenv("TUKU_DATA_DIR")); configured != "" {
		return filepath.Join(filepath.Clean(configured), "cache"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Tuku", "cache"), nil
}

func captureRepoEvidence(repoRoot string) (string, string) {
	diffSummary := ""
	if summary, err := gitdiff.Capture(repoRoot); err == nil {
		diffSummary = gitdiff.Render(summary)
	}
	worktreeSummary := ""
	if summary, err := gitworktree.Capture(repoRoot); err == nil {
		worktreeSummary = gitworktree.Render(summary)
	}
	return diffSummary, worktreeSummary
}

func packageTargetsFromChangedFiles(files []string) []string {
	set := map[string]struct{}{}
	for _, file := range files {
		dir := filepath.Dir(file)
		target := "."
		if dir != "." && dir != "" {
			target = "./" + filepath.ToSlash(dir)
		}
		set[target] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for target := range set {
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

func mergeSignals(existing []string, extra []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	for _, item := range append(append([]string{}, existing...), extra...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func validationPassed(signals []string) bool {
	if len(signals) == 0 {
		return false
	}
	for _, signal := range signals {
		lower := strings.ToLower(signal)
		if strings.Contains(lower, "failed") || strings.Contains(lower, "fail signal") || strings.Contains(lower, "unformatted") {
			return false
		}
	}
	return true
}

func filterSuffix(values []string, suffix string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(value)), strings.ToLower(suffix)) {
			out = append(out, filepath.Clean(value))
		}
	}
	return out
}

func filterShellFiles(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		lower := strings.ToLower(trimmed)
		if strings.HasSuffix(lower, ".sh") {
			out = append(out, filepath.Clean(trimmed))
		}
	}
	return out
}

func filterJSSourceFiles(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		lower := strings.ToLower(trimmed)
		if strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".jsx") || strings.HasSuffix(lower, ".mjs") || strings.HasSuffix(lower, ".cjs") {
			out = append(out, filepath.Clean(trimmed))
		}
	}
	return out
}

func filterTypeScriptSourceFiles(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		lower := strings.ToLower(trimmed)
		if strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".tsx") || strings.HasSuffix(lower, ".mts") || strings.HasSuffix(lower, ".cts") {
			out = append(out, filepath.Clean(trimmed))
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func jsonValidationResults(repoRoot string, files []string) []validationCommandResult {
	results := make([]validationCommandResult, 0, len(files))
	for _, file := range files {
		result := validationCommandResult{Command: "json-validate " + file, ExitCode: 0}
		raw, err := os.ReadFile(filepath.Join(repoRoot, file))
		if err != nil {
			result.ExitCode = 1
			result.Stderr = err.Error()
			results = append(results, result)
			continue
		}
		var payload any
		if err := json.Unmarshal(raw, &payload); err != nil {
			result.ExitCode = 1
			result.Stderr = err.Error()
		} else {
			result.Stdout = "valid JSON"
		}
		results = append(results, result)
	}
	return results
}

func parseExitCodeArg(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return n
}

func mergeStructuredSummary(raw string, fields map[string]any) string {
	payload := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	for key, value := range fields {
		if strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return string(encoded)
}

func normalizedRepoRoot(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "."
	}
	return repoRoot
}

func validationTargetFiles(prepared *preparedRealRun, execResult adapter_contract.ExecutionResult) []string {
	if files := uniqueStrings(execResult.ChangedFiles); len(files) > 0 {
		return files
	}
	targetFiles := make([]string, 0, len(prepared.Brief.PromptIR.RankedTargets))
	for _, target := range prepared.Brief.PromptIR.RankedTargets {
		if strings.TrimSpace(target.Path) != "" {
			targetFiles = append(targetFiles, target.Path)
		}
	}
	if files := uniqueStrings(targetFiles); len(files) > 0 {
		return files
	}
	return uniqueStrings(prepared.ContextPack.IncludedFiles)
}

func plannedValidatorCommands(repoRoot string, posture brief.Posture, index repoindex.Snapshot, files []string) []string {
	repoRoot = normalizedRepoRoot(repoRoot)
	files = uniqueStrings(files)
	commands := []string{}
	if len(files) > 0 && commandOnPath("git") && pathExists(filepath.Join(repoRoot, ".git")) {
		commands = append(commands, "git diff --check")
	}
	goFiles := uniqueStrings(filterSuffix(files, ".go"))
	if len(goFiles) > 0 && commandOnPath("gofmt") {
		commands = append(commands, "gofmt -l "+strings.Join(goFiles, " "))
		if fileExists(filepath.Join(repoRoot, "go.mod")) && commandOnPath("go") {
			pkgs := packageTargetsFromChangedFiles(goFiles)
			if len(pkgs) > 0 {
				commands = append(commands, "go test "+strings.Join(pkgs, " "))
			}
		}
	}
	for _, file := range filterShellFiles(files) {
		if commandOnPath("sh") {
			commands = append(commands, "sh -n "+file)
		}
	}
	for _, file := range filterJSSourceFiles(files) {
		if commandOnPath("node") {
			commands = append(commands, "node --check "+file)
		}
	}
	if eslintCommand, args, ok := eslintValidationSpec(repoRoot, files); ok {
		commands = append(commands, strings.Join(append([]string{friendlyCommandName(eslintCommand)}, args...), " "))
	}
	if tscCommand, ok := typeScriptValidatorCommand(repoRoot, files); ok {
		commands = append(commands, tscCommand)
	}
	relatedTests := relatedIndexTestFiles(index, files, 4)
	if testCommand, args, ok := frontendTestValidationSpec(repoRoot, relatedTests); ok {
		commands = append(commands, strings.Join(append([]string{friendlyCommandName(testCommand)}, args...), " "))
	}
	for _, file := range uniqueStrings(filterSuffix(files, ".json")) {
		commands = append(commands, "json parse "+file)
	}
	if posture == brief.PostureValidationOriented && fileExists(filepath.Join(repoRoot, "go.mod")) && commandOnPath("go") && !containsFold(commands, "go test ./...") {
		commands = append(commands, "go test ./...")
	}
	return dedupeNonEmpty(commands, 8)
}

func plannedValidationSpecs(repoRoot string, posture brief.Posture, index repoindex.Snapshot, files []string) []process.Spec {
	repoRoot = normalizedRepoRoot(repoRoot)
	files = uniqueStrings(files)
	commands := []process.Spec{}
	if len(files) > 0 && commandOnPath("git") && pathExists(filepath.Join(repoRoot, ".git")) {
		commands = append(commands, process.Spec{Command: "git", Args: []string{"diff", "--check"}, WorkingDir: repoRoot})
	}
	goFiles := uniqueStrings(filterSuffix(files, ".go"))
	if len(goFiles) > 0 && commandOnPath("gofmt") {
		args := append([]string{"-l"}, goFiles...)
		commands = append(commands, process.Spec{Command: "gofmt", Args: args, WorkingDir: repoRoot})
	}
	for _, file := range filterShellFiles(files) {
		if commandOnPath("sh") {
			commands = append(commands, process.Spec{Command: "sh", Args: []string{"-n", file}, WorkingDir: repoRoot})
		}
	}
	for _, file := range filterJSSourceFiles(files) {
		if commandOnPath("node") {
			commands = append(commands, process.Spec{Command: "node", Args: []string{"--check", file}, WorkingDir: repoRoot})
		}
	}
	if command, args, ok := eslintValidationSpec(repoRoot, files); ok {
		commands = append(commands, process.Spec{Command: command, Args: args, WorkingDir: repoRoot})
	}
	if command, args, ok := typeScriptValidationSpec(repoRoot, files); ok {
		commands = append(commands, process.Spec{Command: command, Args: args, WorkingDir: repoRoot})
	}
	if command, args, ok := frontendTestValidationSpec(repoRoot, relatedIndexTestFiles(index, files, 4)); ok {
		commands = append(commands, process.Spec{Command: command, Args: args, WorkingDir: repoRoot})
	}

	packages := packageTargetsFromChangedFiles(goFiles)
	if fileExists(filepath.Join(repoRoot, "go.mod")) && len(packages) > 0 && commandOnPath("go") {
		args := append([]string{"test"}, packages...)
		commands = append(commands, process.Spec{Command: "go", Args: args, WorkingDir: repoRoot})
	} else if fileExists(filepath.Join(repoRoot, "go.mod")) && posture == brief.PostureValidationOriented && commandOnPath("go") {
		commands = append(commands, process.Spec{Command: "go", Args: []string{"test", "./..."}, WorkingDir: repoRoot})
	}
	for _, result := range jsonValidationResults(repoRoot, uniqueStrings(filterSuffix(files, ".json"))) {
		commands = append(commands, process.Spec{
			Command: "__tuku_json_validation__",
			Args:    []string{result.Command, result.Stdout, result.Stderr, fmt.Sprintf("%d", result.ExitCode)},
		})
	}
	return commands
}

func eslintValidationSpec(repoRoot string, files []string) (string, []string, bool) {
	candidates := uniqueStrings(append(filterJSSourceFiles(files), filterTypeScriptSourceFiles(files)...))
	if len(candidates) == 0 || !hasESLintConfig(repoRoot) {
		return "", nil, false
	}
	command := resolveRepoExecutable(repoRoot, "eslint")
	if command == "" {
		return "", nil, false
	}
	args := append([]string{"--max-warnings", "0"}, candidates...)
	return command, args, true
}

func frontendTestValidationSpec(repoRoot string, testFiles []string) (string, []string, bool) {
	testFiles = uniqueStrings(testFiles)
	if len(testFiles) == 0 {
		return "", nil, false
	}
	if hasVitestConfig(repoRoot) {
		if command := resolveRepoExecutable(repoRoot, "vitest"); command != "" {
			return command, append([]string{"run"}, testFiles...), true
		}
	}
	if hasJestConfig(repoRoot) {
		if command := resolveRepoExecutable(repoRoot, "jest"); command != "" {
			return command, append([]string{"--runInBand"}, testFiles...), true
		}
	}
	return "", nil, false
}

func typeScriptValidatorCommand(repoRoot string, files []string) (string, bool) {
	command, args, ok := typeScriptValidationSpec(repoRoot, files)
	if !ok {
		return "", false
	}
	return strings.Join(append([]string{command}, args...), " "), true
}

func typeScriptValidationSpec(repoRoot string, files []string) (string, []string, bool) {
	if len(filterTypeScriptSourceFiles(files)) == 0 || !hasTypeScriptConfig(repoRoot) {
		return "", nil, false
	}
	if command := resolveRepoExecutable(repoRoot, "tsc"); command != "" {
		return command, []string{"--noEmit", "--pretty", "false"}, true
	}
	return "", nil, false
}

func hasTypeScriptConfig(repoRoot string) bool {
	if fileExists(filepath.Join(repoRoot, "tsconfig.json")) {
		return true
	}
	matches, err := filepath.Glob(filepath.Join(repoRoot, "tsconfig*.json"))
	return err == nil && len(matches) > 0
}

func hasESLintConfig(repoRoot string) bool {
	for _, name := range []string{"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", ".eslintrc", ".eslintrc.js", ".eslintrc.cjs", ".eslintrc.json", ".eslintrc.yaml", ".eslintrc.yml"} {
		if pathExists(filepath.Join(repoRoot, name)) {
			return true
		}
	}
	return false
}

func hasVitestConfig(repoRoot string) bool {
	for _, pattern := range []string{"vitest.config.*", "vite.config.*"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, pattern))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

func hasJestConfig(repoRoot string) bool {
	for _, pattern := range []string{"jest.config.*", "jest.*.config.*"} {
		matches, err := filepath.Glob(filepath.Join(repoRoot, pattern))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	for _, name := range []string{"package.json"} {
		if pathExists(filepath.Join(repoRoot, name)) {
			raw, err := os.ReadFile(filepath.Join(repoRoot, name))
			if err == nil && strings.Contains(strings.ToLower(string(raw)), "jest") {
				return true
			}
		}
	}
	return false
}

func resolveRepoExecutable(repoRoot string, name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	local := filepath.Join(repoRoot, "node_modules", ".bin", name)
	if fileExists(local) {
		return local
	}
	if commandOnPath(name) {
		return name
	}
	return ""
}

func friendlyCommandName(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	base := filepath.Base(command)
	if base == "." || base == string(filepath.Separator) {
		return command
	}
	return base
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func commandOnPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
