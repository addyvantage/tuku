package shell

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectWorkerPrerequisiteMarksMissingBinary(t *testing.T) {
	origLookPath := workerPrereqLookPath
	defer func() { workerPrereqLookPath = origLookPath }()

	workerPrereqLookPath = func(string) (string, error) {
		return "", errors.New("missing")
	}

	prereq := DetectWorkerPrerequisite(WorkerPreferenceCodex)
	if prereq.State != WorkerPrerequisiteMissingBinary {
		t.Fatalf("expected missing binary state, got %s", prereq.State)
	}
	if prereq.Ready {
		t.Fatalf("expected missing worker not to be ready: %+v", prereq)
	}
	if len(prereq.InstallCommand) == 0 {
		t.Fatalf("expected install command, got %+v", prereq)
	}
}

func TestDetectWorkerPrerequisiteMarksCodexAuthNeeded(t *testing.T) {
	origLookPath := workerPrereqLookPath
	defer func() { workerPrereqLookPath = origLookPath }()

	bin := writeWorkerProbeScript(t, `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  echo "Not logged in"
  exit 1
fi
echo "unexpected"
exit 0
`)
	workerPrereqLookPath = func(string) (string, error) { return bin, nil }

	prereq := DetectWorkerPrerequisite(WorkerPreferenceCodex)
	if prereq.State != WorkerPrerequisiteUnauthenticated {
		t.Fatalf("expected unauthenticated state, got %+v", prereq)
	}
	if prereq.Ready {
		t.Fatalf("expected unauthenticated codex to block readiness: %+v", prereq)
	}
}

func TestDetectWorkerPrerequisiteMarksClaudeReadyFromAuthJSON(t *testing.T) {
	origLookPath := workerPrereqLookPath
	defer func() { workerPrereqLookPath = origLookPath }()

	bin := writeWorkerProbeScript(t, `#!/bin/sh
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo '{"loggedIn":true,"email":"dev@example.com"}'
  exit 0
fi
echo "unexpected"
exit 0
`)
	workerPrereqLookPath = func(string) (string, error) { return bin, nil }

	prereq := DetectWorkerPrerequisite(WorkerPreferenceClaude)
	if prereq.State != WorkerPrerequisiteReady || !prereq.Ready {
		t.Fatalf("expected ready claude prerequisite, got %+v", prereq)
	}
	if !prereq.Authenticated {
		t.Fatalf("expected authenticated claude prerequisite, got %+v", prereq)
	}
}

func writeWorkerProbeScript(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "worker-probe")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write worker probe script: %v", err)
	}
	return path
}
