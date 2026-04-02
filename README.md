# Tuku

Tuku is a local-first orchestration brain for coding work.

It runs as two local binaries:
- `tukud`: daemon that owns canonical task/conversation state and proof events.
- `tuku`: CLI operator surface for task control, shell sessions, and continuity workflows.

## Core Contracts

These are repository-level invariants:
- Tuku owns canonical conversation and task state.
- Worker output is evidence, not final user response.
- Final user-facing response is synthesized by Tuku from canonical state plus proof events.
- v1 is local-first and does not require a cloud dependency on the core runtime path.

## v1 Scope

Included:
- Local daemon + CLI runtime foundations
- SQLite persistence
- Unix socket IPC between CLI and daemon
- Worker adapters for Codex and Claude in terminal shell flows
- Continuity primitives (checkpoint, recovery, incident, handoff, transcript review)

Not included:
- Broad web UI
- Multi-agent role orchestration
- Automation connectors

## Architecture (Compact)

```text
Operator Terminal
      |
      v
   tuku (CLI) -----------------------------+
      |                                    |
      | Unix socket IPC                    | Optional live worker PTY host
      v                                    | (Codex / Claude via shell UI)
  tukud (Daemon)                           |
      |                                    |
      v                                    |
  Orchestrator / Coordinator               |
      |                                    |
      | writes canonical state + proof     |
      v                                    |
   SQLite Store                            |
      |                                    |
      +--> Worker Adapter (evidence only) -+
      |
      +--> Canonical Synthesizer --> Final user-facing response
```

Key rule: raw worker output is never returned directly; Tuku synthesizes final output from canonical state and proof events.

## Install

### Option A: npm package (`tukuai`)

```bash
npm install -g tukuai
tuku
```

Notes:
- Package name is `tukuai`; executable is `tuku`.
- Launcher installs/runs bundled native `tuku` + `tukud` binaries.
- If bundled binaries are unavailable, launcher falls back to GitHub release download, then source build.
- Source-build fallback requires Go.

### Option B: Build from source

```bash
go build ./cmd/tuku ./cmd/tukud
./tuku help
```

## Prerequisites

Required:
- macOS or Linux
- Go 1.22+ (for source builds and local repo development)

Optional for live worker sessions:
- `codex` on `PATH` or `TUKU_SHELL_CODEX_BIN`
- `claude` on `PATH` or `TUKU_SHELL_CLAUDE_BIN` / `TUKU_CLAUDE_BIN`

For npm install path:
- Node.js 18+

## Quick Start

### 1) Start in a repo directory

```bash
cd /path/to/your/git/repo
tuku
```

What happens:
- Tuku resolves/creates task continuity for the current repo.
- The daemon auto-starts if needed.
- A worker shell flow opens (or a worker is selected if unset).

If you run `tuku` outside a git repo, Tuku opens a local scratch mode (no daemon task continuity claim).

### 2) Explicit worker routing

```bash
tuku --worker auto
tuku --worker codex
tuku --worker claude
```

Shortcuts:

```bash
tuku chat
# equivalent explicit shortcuts
tuku chat codex
tuku chat claude
```

### 3) Optional bubble UI entry

```bash
tuku ui --worker codex
```

`ui` requires running inside a git repository.

## Minimal End-to-End CLI Flow

```bash
# start daemon manually (optional; CLI can auto-start it)
go run ./cmd/tukud

# create a task
go run ./cmd/tuku start --goal "Manual smoke" --repo /absolute/path/to/repo

# add operator message
go run ./cmd/tuku message --task <TASK_ID> --text "Begin implementation."

# run lifecycle
go run ./cmd/tuku run --task <TASK_ID> --mode noop --action start
go run ./cmd/tuku run --task <TASK_ID> --action complete --run-id <RUN_ID>

# continuity controls
go run ./cmd/tuku checkpoint --task <TASK_ID>
go run ./cmd/tuku continue --task <TASK_ID>

# inspect canonical state
go run ./cmd/tuku status --task <TASK_ID> --human
go run ./cmd/tuku inspect --task <TASK_ID> --human
```

## Command Surface

Show usage:

```bash
tuku help
```

Most-used command groups:
- Task lifecycle: `start`, `message`, `run`, `next`, `continue`, `checkpoint`, `status`, `inspect`
- Shell continuity: `shell`, `shell-sessions`, `shell transcript`, `shell transcript review`, `shell transcript history`
- Handoffs: `handoff-create`, `handoff-accept`, `handoff-launch`, `handoff-followthrough`, `handoff-resolve`
- Incident/recovery: `incident ...`, `recovery ...`, `transition history`, `operator acknowledge-review-gap`
- Intent/briefing: `intent`, `brief`, `plan`, `benchmark`

## Runtime Paths and Environment Overrides

Defaults:
- Data root: `~/Library/Application Support/Tuku`
- SQLite DB: `~/Library/Application Support/Tuku/tuku.db`
- Socket root: `/tmp/tuku/<user-hash>/`
- Socket path: `/tmp/tuku/<user-hash>/tukud.sock`
- Cache root: `~/Library/Application Support/Tuku/cache`
- Scratch notes: `~/Library/Application Support/Tuku/scratch/`

Path overrides:
- `TUKU_DATA_DIR`
- `TUKU_DB_PATH`
- `TUKU_RUN_DIR`
- `TUKU_SOCKET_PATH`
- `TUKU_CACHE_DIR`

Worker host overrides:
- Codex: `TUKU_SHELL_CODEX_BIN`, `TUKU_SHELL_CODEX_ARGS`
- Claude: `TUKU_SHELL_CLAUDE_BIN`, `TUKU_SHELL_CLAUDE_ARGS`
- Claude fallback envs: `TUKU_CLAUDE_BIN`, `TUKU_CLAUDE_ARGS`, `TUKU_CLAUDE_TIMEOUT_SEC`

Launcher/install overrides:
- `TUKU_SKIP_DOWNLOAD=1` to skip postinstall download attempt
- `TUKU_INSTALL_ROOT` to change launcher-managed binary install root
- `TUKU_RELEASE_REPO` and `TUKU_ASSET_PREFIX` for custom release asset lookup

## Architecture Map

- `cmd/tuku`: CLI entrypoint
- `cmd/tukud`: daemon entrypoint
- `internal/app`: wiring for CLI + daemon applications
- `internal/orchestrator`: core coordination and lifecycle logic
- `internal/runtime/daemon`: Unix socket daemon service
- `internal/storage/sqlite`: persistence layer
- `internal/response/canonical`: canonical response synthesis
- `internal/tui/shell`: worker-native terminal shell surface
- `internal/ipc`: daemon/CLI request-response contracts

See also: [`docs/architecture/v1-bootstrap.md`](docs/architecture/v1-bootstrap.md)

## Development

Run tests:

```bash
go test ./...
```

Contributing expectations in this repo:
- Keep changes in small, reviewable slices.
- Preserve package boundaries and domain contracts.
- Do not bypass canonical synthesis by returning raw worker output.
