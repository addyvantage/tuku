# Tuku

Tuku is a local-first orchestration and continuity control plane for coding-agent execution.

Tuku owns canonical task state, continuity state, and canonical responses. Worker output is treated as bounded evidence, not final truth.

## Product Contract
- Canonical response owner: Tuku synthesizes operator-facing truth.
- Worker adapters are bounded executors (`codex`, `claude` handoff path).
- Evidence over claims: transcripts, follow-up receipts, closure/risk posture are advisory and bounded.
- No hidden policy engine: advisory posture does not silently become hard authority.

## Current Capability Surface

### Core task control
- Durable task/capsule/intent/brief/checkpoint/run/proof state in SQLite.
- CLI + daemon over Unix socket IPC.
- `start`, `message`, `run`, `checkpoint`, `continue`, `next`, `status`, `inspect`.

### Continuity + handoff control plane
- Handoff lifecycle: create, accept, launch, follow-through, resolution.
- Recovery actions and operator-step projections.
- Active branch ownership and continuity/recovery posture projections.

### Evidence and transcript posture
- Durable shell session registry.
- Bounded transcript read/review/review-history surfaces.
- Review gap acknowledgments and transition receipts.

### Incident intelligence
- Incident triage + follow-up progression (`RECORDED_PENDING`, `PROGRESSED`, `CLOSED`, `REOPENED`).
- Closure intelligence (bounded, conservative classes).
- Cross-incident task risk derivation (`incident risk`).

### Intent + brief front-end of execution
- Compiled Intent v1: bounded deterministic extraction of objective/scope/constraints/done-criteria/ambiguity/readiness.
- Brief Generation v2: compiled-intent-driven brief posture and worker framing with explicit clarification-needed state.

### Operator human-mode parity
- Human-mode summaries across major commands (`--human`) with consistent digest/window/detail ordering.

## Requirements
- Go 1.22+
- macOS/Linux shell environment
- Optional local workers on `PATH`:
  - `codex` for live local execution shell
  - `claude` for handoff launch path

## Build
```bash
go build ./cmd/tuku ./cmd/tukud
```

## Run

### 1) Start daemon
```bash
go run ./cmd/tukud
```

Default local runtime paths:
- DB: `~/Library/Application Support/Tuku/tuku.db`
- Socket: `~/Library/Application Support/Tuku/run/tukud.sock`

### 2) CLI help
```bash
go run ./cmd/tuku help
```

## Common command flow

```bash
# start task
go run ./cmd/tuku start --goal "Implement bounded feature" --repo .

# compile intent + generate brief
go run ./cmd/tuku message --task <TASK_ID> --text "Implement X with Y constraints"

# read operator summaries
go run ./cmd/tuku status --task <TASK_ID> --human
go run ./cmd/tuku inspect --task <TASK_ID> --human

go run ./cmd/tuku intent --task <TASK_ID>
go run ./cmd/tuku brief --task <TASK_ID> --human

# run lifecycle
go run ./cmd/tuku run --task <TASK_ID> --mode noop --action start
go run ./cmd/tuku checkpoint --task <TASK_ID> --human
go run ./cmd/tuku continue --task <TASK_ID> --human

# continuity incident surfaces
go run ./cmd/tuku incident --task <TASK_ID>
go run ./cmd/tuku incident triage --task <TASK_ID> --posture triaged --summary "bounded triage"
go run ./cmd/tuku incident followup --task <TASK_ID> --action progressed --summary "bounded follow-up"
go run ./cmd/tuku incident closure --task <TASK_ID>
go run ./cmd/tuku incident risk --task <TASK_ID>

# shell + transcripts
go run ./cmd/tuku shell --task <TASK_ID>
go run ./cmd/tuku shell-sessions --task <TASK_ID> --human
go run ./cmd/tuku shell transcript --task <TASK_ID> --session <SESSION_ID>
```

## Scope and Non-Goals (v1)
- Local-first runtime and CLI/daemon foundations.
- No cloud dependency for core runtime.
- No broad web UI.
- No multi-agent role orchestration.
- No automation connector layer.

## Testing
```bash
go test ./...
```

## Project Governance
- Contributions: see [CONTRIBUTING.md](CONTRIBUTING.md)
- Security reporting: see [SECURITY.md](SECURITY.md)
- Community standards: see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- License: [MIT](LICENSE)

## Repository Layout
- `cmd/tuku`: CLI
- `cmd/tukud`: daemon
- `internal/orchestrator`: centralized derivations and orchestration
- `internal/runtime/daemon`: IPC route handling
- `internal/ipc`: request/response payload contracts
- `internal/storage/sqlite`: durable local persistence
- `internal/tui/shell`: shell/TUI operator surfaces
- `docs/architecture`: architecture notes
