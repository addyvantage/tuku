package shell

import (
	"context"
	"strings"
	"testing"
)

type resumeCapableStubHost struct {
	resumeSessionID string
}

func (h *resumeCapableStubHost) Start(_ context.Context, _ Snapshot) error { return nil }
func (h *resumeCapableStubHost) Stop() error                               { return nil }
func (h *resumeCapableStubHost) UpdateSnapshot(_ Snapshot)                 {}
func (h *resumeCapableStubHost) Resize(_ int, _ int) bool                  { return false }
func (h *resumeCapableStubHost) CanAcceptInput() bool                      { return false }
func (h *resumeCapableStubHost) WriteInput(_ []byte) bool                  { return false }
func (h *resumeCapableStubHost) Status() HostStatus                        { return HostStatus{} }
func (h *resumeCapableStubHost) Title() string                             { return "" }
func (h *resumeCapableStubHost) WorkerLabel() string                       { return "" }
func (h *resumeCapableStubHost) Lines(_ int, _ int) []string               { return nil }
func (h *resumeCapableStubHost) ActivityLines(_ int) []string              { return nil }
func (h *resumeCapableStubHost) SetResumeSessionID(sessionID string) {
	h.resumeSessionID = sessionID
}

func TestResolveReattachTargetRequiresAttachableSession(t *testing.T) {
	_, ok, err := resolveReattachTarget("shs_1", []KnownShellSession{{
		SessionID:    "shs_1",
		SessionClass: KnownShellSessionClassActiveUnattachable,
		HostState:    HostStateLive,
	}})
	if err == nil {
		t.Fatal("expected non-attachable reattach target to fail")
	}
	if !strings.Contains(err.Error(), "no worker session id") {
		t.Fatalf("expected missing worker-session-id guidance, got %q", err.Error())
	}
	if ok {
		t.Fatal("expected non-attachable target to return ok=false")
	}
}

func TestResolveReattachTargetSelectsAttachableSession(t *testing.T) {
	target, ok, err := resolveReattachTarget("shs_1", []KnownShellSession{{
		SessionID:             "shs_1",
		SessionClass:          KnownShellSessionClassAttachable,
		WorkerSessionID:       "wks_1",
		WorkerSessionIDSource: WorkerSessionIDSourceAuthoritative,
		AttachCapability:      WorkerAttachCapabilityAttachable,
		ResolvedWorker:        WorkerPreferenceCodex,
		HostState:             HostStateLive,
	}})
	if err != nil {
		t.Fatalf("resolve reattach target: %v", err)
	}
	if !ok {
		t.Fatal("expected attachable target to return ok=true")
	}
	if target.WorkerSessionID != "wks_1" {
		t.Fatalf("expected worker session id wks_1, got %q", target.WorkerSessionID)
	}
}

func TestResolveReattachTargetRejectsHeuristicSessionID(t *testing.T) {
	_, ok, err := resolveReattachTarget("shs_1", []KnownShellSession{{
		SessionID:             "shs_1",
		SessionClass:          KnownShellSessionClassActiveUnattachable,
		WorkerSessionID:       "wks_h",
		WorkerSessionIDSource: WorkerSessionIDSourceHeuristic,
		AttachCapability:      WorkerAttachCapabilityAttachable,
		ResolvedWorker:        WorkerPreferenceCodex,
		HostState:             HostStateLive,
		SessionClassReason:    "worker session id was heuristically detected from output and is not authoritative",
		ReattachGuidance:      "reattach requires an authoritative worker session id",
	}})
	if err == nil {
		t.Fatal("expected heuristic session-id target to fail")
	}
	if !strings.Contains(err.Error(), "heuristic") {
		t.Fatalf("expected heuristic-specific failure, got %q", err.Error())
	}
	if ok {
		t.Fatal("expected heuristic target to return ok=false")
	}
}

func TestResolveReattachTargetRejectsEndedSession(t *testing.T) {
	_, ok, err := resolveReattachTarget("shs_1", []KnownShellSession{{
		SessionID:    "shs_1",
		SessionClass: KnownShellSessionClassEnded,
		HostState:    HostStateExited,
	}})
	if err == nil {
		t.Fatal("expected ended session target to fail")
	}
	if !strings.Contains(err.Error(), "ended") {
		t.Fatalf("expected ended-session failure, got %q", err.Error())
	}
	if ok {
		t.Fatal("expected ended target to return ok=false")
	}
}

func TestConfigureHostResumeSessionUsesResumeCapableHost(t *testing.T) {
	host := &resumeCapableStubHost{}
	if !configureHostResumeSession(host, "wks_resume") {
		t.Fatal("expected resume-capable host to be configured")
	}
	if host.resumeSessionID != "wks_resume" {
		t.Fatalf("expected resume session id to be set, got %q", host.resumeSessionID)
	}
}
