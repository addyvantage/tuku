package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type WorkerPrerequisiteState string

const (
	WorkerPrerequisiteReady           WorkerPrerequisiteState = "ready"
	WorkerPrerequisiteMissingBinary   WorkerPrerequisiteState = "missing-binary"
	WorkerPrerequisiteUnauthenticated WorkerPrerequisiteState = "unauthenticated"
	WorkerPrerequisiteUnknown         WorkerPrerequisiteState = "unknown"
)

type WorkerPrerequisite struct {
	Preference        WorkerPreference
	WorkerLabel       string
	BinaryName        string
	BinaryPath        string
	Installed         bool
	Authenticated     bool
	Ready             bool
	State             WorkerPrerequisiteState
	Summary           string
	Detail            string
	InstallPackage    string
	InstallCommand    []string
	LoginCommand      []string
	AuthStatusCommand []string
}

var (
	workerPrereqLookPath       = exec.LookPath
	workerPrereqCommandContext = exec.CommandContext
)

func DetectWorkerPrerequisite(preference WorkerPreference) WorkerPrerequisite {
	spec, ok := workerPrerequisiteDefinition(preference)
	if !ok {
		return WorkerPrerequisite{
			Preference: preference,
			State:      WorkerPrerequisiteUnknown,
			Summary:    "This worker is not supported in the current Tuku runtime.",
			Detail:     "Choose Codex or Claude to open a live worker shell.",
		}
	}

	status := WorkerPrerequisite{
		Preference:        preference,
		WorkerLabel:       spec.label,
		BinaryName:        spec.binaryName,
		InstallPackage:    spec.installPackage,
		InstallCommand:    append([]string{}, spec.installCommand...),
		LoginCommand:      append([]string{}, spec.loginCommand...),
		AuthStatusCommand: append([]string{}, spec.authStatusCommand...),
		State:             WorkerPrerequisiteUnknown,
	}

	binaryPath, err := resolveWorkerBinary(spec)
	if err != nil {
		status.State = WorkerPrerequisiteMissingBinary
		status.Summary = fmt.Sprintf("%s is not installed on this machine yet.", spec.label)
		status.Detail = fmt.Sprintf("Tuku can install %s for you with `%s`.", spec.label, strings.Join(spec.installCommand, " "))
		return status
	}

	status.Installed = true
	status.BinaryPath = binaryPath

	authenticated, authKnown, authDetail := probeWorkerAuthentication(binaryPath, spec)
	if authKnown && authenticated {
		status.Authenticated = true
		status.Ready = true
		status.State = WorkerPrerequisiteReady
		status.Summary = fmt.Sprintf("%s is installed and signed in.", spec.label)
		status.Detail = fmt.Sprintf("Tuku can open a live %s session right away.", spec.label)
		return status
	}
	if authKnown && !authenticated {
		status.State = WorkerPrerequisiteUnauthenticated
		status.Summary = fmt.Sprintf("%s is installed, but it still needs sign-in.", spec.label)
		if strings.TrimSpace(authDetail) == "" {
			authDetail = fmt.Sprintf("Run `%s` to finish signing in.", strings.Join(spec.loginCommand, " "))
		}
		status.Detail = authDetail
		return status
	}

	status.Authenticated = true
	status.Ready = true
	status.State = WorkerPrerequisiteReady
	status.Summary = fmt.Sprintf("%s is installed. Tuku could not verify sign-in, so it will continue carefully.", spec.label)
	status.Detail = nonEmpty(strings.TrimSpace(authDetail), fmt.Sprintf("If %s asks you to sign in on first use, Tuku will pause for that setup.", spec.label))
	return status
}

type workerPrerequisiteSpec struct {
	label             string
	binaryName        string
	binaryEnv         []string
	installPackage    string
	installCommand    []string
	loginCommand      []string
	authStatusCommand []string
	authProbe         func(path string, spec workerPrerequisiteSpec) (bool, bool, string)
}

func workerPrerequisiteDefinition(preference WorkerPreference) (workerPrerequisiteSpec, bool) {
	switch preference {
	case WorkerPreferenceCodex:
		return workerPrerequisiteSpec{
			label:             "Codex",
			binaryName:        "codex",
			binaryEnv:         []string{"TUKU_SHELL_CODEX_BIN"},
			installPackage:    "@openai/codex",
			installCommand:    []string{npmCommandName(), "install", "-g", "@openai/codex"},
			loginCommand:      []string{"codex", "login"},
			authStatusCommand: []string{"codex", "login", "status"},
			authProbe:         probeCodexAuthentication,
		}, true
	case WorkerPreferenceClaude:
		return workerPrerequisiteSpec{
			label:             "Claude",
			binaryName:        "claude",
			binaryEnv:         []string{"TUKU_SHELL_CLAUDE_BIN", "TUKU_CLAUDE_BIN"},
			installPackage:    "@anthropic-ai/claude-code",
			installCommand:    []string{npmCommandName(), "install", "-g", "@anthropic-ai/claude-code"},
			loginCommand:      []string{"claude", "auth", "login"},
			authStatusCommand: []string{"claude", "auth", "status"},
			authProbe:         probeClaudeAuthentication,
		}, true
	default:
		return workerPrerequisiteSpec{}, false
	}
}

func resolveWorkerBinary(spec workerPrerequisiteSpec) (string, error) {
	for _, name := range spec.binaryEnv {
		if candidate := strings.TrimSpace(os.Getenv(name)); candidate != "" {
			return candidate, nil
		}
	}
	return workerPrereqLookPath(spec.binaryName)
}

func probeWorkerAuthentication(binaryPath string, spec workerPrerequisiteSpec) (bool, bool, string) {
	if spec.authProbe == nil {
		return false, false, ""
	}
	return spec.authProbe(binaryPath, spec)
}

func probeCodexAuthentication(binaryPath string, spec workerPrerequisiteSpec) (bool, bool, string) {
	output, err := runWorkerProbe(binaryPath, spec.authStatusCommand[1:]...)
	text := strings.TrimSpace(output)
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "not logged in"), strings.Contains(lower, "logged out"):
		return false, true, nonEmpty(text, "Codex needs sign-in before Tuku can open a live session.")
	case strings.Contains(lower, "logged in"):
		return true, true, nonEmpty(text, "Logged in.")
	case err == nil:
		return true, true, nonEmpty(text, "Codex login status is available.")
	default:
		return false, false, probeFailureDetail(spec.label, spec.loginCommand, text, err)
	}
}

func probeClaudeAuthentication(binaryPath string, spec workerPrerequisiteSpec) (bool, bool, string) {
	output, err := runWorkerProbe(binaryPath, spec.authStatusCommand[1:]...)
	text := strings.TrimSpace(output)
	if text != "" {
		var payload struct {
			LoggedIn bool   `json:"loggedIn"`
			Email    string `json:"email"`
		}
		if json.Unmarshal([]byte(text), &payload) == nil {
			if payload.LoggedIn {
				detail := "Claude authentication is ready."
				if strings.TrimSpace(payload.Email) != "" {
					detail = fmt.Sprintf("Signed in as %s.", strings.TrimSpace(payload.Email))
				}
				return true, true, detail
			}
			return false, true, "Claude is installed, but it still needs sign-in."
		}
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, `"loggedin":true`), strings.Contains(lower, "logged in"):
		return true, true, nonEmpty(text, "Claude authentication is ready.")
	case strings.Contains(lower, `"loggedin":false`), strings.Contains(lower, "not logged in"), strings.Contains(lower, "login required"):
		return false, true, nonEmpty(text, "Claude is installed, but it still needs sign-in.")
	case err == nil:
		return true, true, nonEmpty(text, "Claude auth status is available.")
	default:
		return false, false, probeFailureDetail(spec.label, spec.loginCommand, text, err)
	}
}

func runWorkerProbe(binaryPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	cmd := workerPrereqCommandContext(ctx, binaryPath, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if ctx.Err() != nil {
		return text, ctx.Err()
	}
	return text, err
}

func probeFailureDetail(workerLabel string, loginCommand []string, output string, err error) string {
	if strings.TrimSpace(output) != "" {
		return output
	}
	if err == nil {
		return fmt.Sprintf("%s is installed. Tuku could not read its sign-in status yet.", workerLabel)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%s took too long to report auth status. You can continue or run `%s` first.", workerLabel, strings.Join(loginCommand, " "))
	}
	return fmt.Sprintf("%s is installed. Tuku could not verify sign-in yet: %v", workerLabel, err)
}

func npmCommandName() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}
