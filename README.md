# Tuku

Tuku is a local-first orchestration brain for coding work.

## Current Manual-Test Scope
This repository currently supports a runnable local flow for:
- `task.start`
- `task.message`
- `task.shell.snapshot`
- `task.run`
- `task.status`
- `task.inspect`
- `task.checkpoint`
- `task.continue`
- `task.handoff.create`
- `task.handoff.accept`
- `task.handoff.launch`
- `tuku`
- `tuku chat`
- `tuku shell --task <TASK_ID>`

Tuku remains the canonical response owner:
- worker output is captured as evidence
- Tuku emits the final canonical user-facing response

## Prerequisites
- macOS (current focus)
- Go toolchain installed and on `PATH` for local source builds and repository development
- `codex` executable installed and on `PATH` for direct live PTY shell usage
  - Optional override: `TUKU_SHELL_CODEX_BIN=/absolute/path/to/codex`
  - Optional args: `TUKU_SHELL_CODEX_ARGS="..."`
- `claude` executable installed and on `PATH` for real handoff launch tests
  - Optional override: `TUKU_CLAUDE_BIN=/absolute/path/to/claude`
  - Optional args: `TUKU_CLAUDE_ARGS="--your --flags"`
  - Optional timeout: `TUKU_CLAUDE_TIMEOUT_SEC=90`

For end users installing from npm, Go is not required and Codex/Claude are optional on first run. Tuku now:
- boots from bundled native `tuku` and `tukud` binaries
- opens the worker picker even on a fresh machine
- checks whether Codex or Claude is installed and signed in
- offers to install the selected worker with npm if it is missing
- offers to run the worker login flow if it is installed but not signed in

## Build
```bash
cd /Users/kagaya/Desktop/Tuku
go build ./cmd/tuku ./cmd/tukud
```

## Simple Global Install (npm)
If you want a CodeMaster-style install where users run one command and then type `tuku`, this repo now includes an npm launcher package.

Install:
```bash
npm install -g tukuai
```

Then run:
```bash
tuku
```

How it works:
- the npm `tuku` command is a thin Node launcher
- the published npm tarball bundles native `tuku` and `tukud` binaries for supported macOS and Linux targets
- on install/first run it installs those bundled binaries into `~/.tukuai/bin`
- if a bundled binary is unavailable for the current machine, it falls back to GitHub release download
- if release binaries are unavailable too, it finally falls back to building from bundled Go source automatically (requires `go` on PATH)
- then it executes native `tuku` and ensures `tukud` is on PATH for daemon bootstrap
- when you choose Codex or Claude in the primary launcher, Tuku checks whether that worker is installed and signed in before opening the live shell
- if the worker is missing, Tuku shows an in-terminal setup prompt and can run the npm install for you
- if the worker is installed but not signed in, Tuku shows an in-terminal sign-in prompt before continuing

Release asset naming convention expected by the fallback downloader:
- `tuku-tuku-darwin-arm64`
- `tuku-tukud-darwin-arm64`
- `tuku-tuku-darwin-amd64`
- `tuku-tukud-darwin-amd64`
- `tuku-tuku-linux-arm64`
- `tuku-tukud-linux-arm64`
- `tuku-tuku-linux-amd64`
- `tuku-tukud-linux-amd64`

These assets should be uploaded to GitHub Releases at tag `v<version>` in the repo configured by `releaseRepo` (default: `addyvantage/tuku` in `package.json`).

Optional environment overrides:
- `TUKU_RELEASE_REPO=owner/repo` (override release source repo)
- `TUKU_ASSET_PREFIX=tuku` (override asset name prefix)
- `TUKU_CLI_VERSION=0.1.2` (force specific release version)
- `TUKU_INSTALL_ROOT=/custom/path` (override install root)

## Start Local Daemon
Run in one terminal:
```bash
cd /Users/kagaya/Desktop/Tuku
go run ./cmd/tukud
```

This manual daemon path remains useful for debugging and development. The primary `tuku` / `tuku chat` entry now tries to start the local daemon automatically when it is not already running.

Default local paths:
- SQLite DB: `~/Library/Application Support/Tuku/tuku.db`
- Unix socket: a short temp-backed run root such as `/tmp/tuku/<user-hash>/tukud.sock` on implicit defaults, or `TUKU_RUN_DIR/tukud.sock` when `TUKU_RUN_DIR` is set
- Scratch intake notes: `~/Library/Application Support/Tuku/scratch/`

Optional runtime path overrides (recommended for isolated dev/test runs):
- `TUKU_DATA_DIR=/absolute/path` (base for DB and default scratch storage)
- `TUKU_RUN_DIR=/absolute/path` (base for unix socket when `TUKU_SOCKET_PATH` is not set)
- `TUKU_CACHE_DIR=/absolute/path` (stores scratch notes when set)
- `TUKU_DB_PATH=/absolute/path/tuku.db` (exact DB file override)
- `TUKU_SOCKET_PATH=/absolute/path/tukud.sock` (exact socket file override)

## CLI Help
```bash
cd /Users/kagaya/Desktop/Tuku
go run ./cmd/tuku help
```

## Primary Entry
The main entry surface now resolves the current git repo automatically:
- it tries the local daemon first and starts it automatically in the common "not running yet" case
- it finds the current repo root from `cwd`
- it reuses the most recent matching task for that repo, preferring an `ACTIVE` task
- if no matching task exists, it creates a new task with a minimal continuation goal
- if that repo-backed task is newly created and a local scratch session exists for the same directory, Tuku surfaces those local notes in the initial shell as adoptable intake context without importing them automatically
- those surfaced local notes can be staged into a shell-local draft, edited locally inside the shell, and only become canonical after an explicit send
- it then opens the existing full-screen shell flow
- if no git repo is detected, it opens a simple local scratch and intake prompt without inventing repo-backed continuity
- local scratch notes entered there are persisted only on this machine and reopen when you return to the same non-repo directory

Run it with:
```bash
cd /Users/kagaya/Desktop/Tuku
go run ./cmd/tuku
go run ./cmd/tuku chat
```

Optional worker preference:
```bash
go run ./cmd/tuku --worker auto
go run ./cmd/tuku --worker codex
go run ./cmd/tuku --worker claude
go run ./cmd/tuku chat --worker auto
go run ./cmd/tuku chat --worker codex
go run ./cmd/tuku chat --worker claude
```

If the current directory is not inside a git repository, Tuku now opens a deliberate local scratch and intake prompt and says so directly instead of inventing task continuity. That no-repo mode is intentionally simpler than the repo-backed shell: normal terminal input, obvious `/help`, `/list`, and `/quit` commands, and one-line local scratch note capture. Scratch notes from that mode are stored locally under `~/Library/Application Support/Tuku/scratch/` by default (or under `TUKU_CACHE_DIR/scratch/` when `TUKU_CACHE_DIR` is set) and are not part of daemon-backed task state. When you later create the first repo-backed task in the same directory, Tuku can surface those notes as local intake context, stage them into a shell-local draft, edit that draft locally inside the shell, and then explicitly adopt them only when you send that draft through `task.message`. If the daemon cannot be started automatically for the repo-backed path, Tuku returns a direct local-daemon startup error instead of pretending the shell can continue.

## Terminal Shell
Tuku now includes a worker-native terminal shell:
- the center pane stays worker-first
- Tuku chrome only adds continuity, handoff, and proof context around it
- the default shell now opens with calmer secondary chrome: the worker pane stays dominant and inspector/activity can be toggled in as needed
- current shell can host either a real Codex PTY session or a real Claude PTY session
- the live host now tracks explicit shell-local lifecycle state: starting, live, exited, failed, fallback, transcript-only
- the worker pane now labels live sessions as live input and transcript modes as read-only so fallback state is obvious at a glance
- each shell run gets a shell-local session id and compact in-memory session journal
- the daemon now owns a narrow durable shell-session registry with compact metadata only
- shell-session reads now classify sessions as attachable, active-unattachable, stale, or ended
- live PTY-backed shells now report a durable worker-session id and narrow attach capability metadata for future reattach groundwork
- major shell lifecycle milestones are bridged into persisted proof: host started, host exited, transcript fallback activated
- terminal resize is propagated into the live worker pane when the PTY host is active
- if PTY startup fails or the live worker exits, the shell falls back to the persisted transcript view and keeps the shell usable
- if you open the shell again later, the new shell session surfaces both the latest persisted shell outcome and any other known shell sessions for the task, including stale-session uncertainty

Run it with:
```bash
go run ./cmd/tuku shell --task <TASK_ID>
```

Optional worker preference:
```bash
go run ./cmd/tuku shell --task <TASK_ID> --worker auto
go run ./cmd/tuku shell --task <TASK_ID> --worker codex
go run ./cmd/tuku shell --task <TASK_ID> --worker claude
```

Inspect daemon-known shell sessions for a task:
```bash
go run ./cmd/tuku shell-sessions --task <TASK_ID>
```
The output includes `worker_session_id`, `attach_capability`, and `session_class` with `attachable`, `active_unattachable`, `stale`, or `ended`.

Key controls:
- when the worker pane is live and focused:
  - normal typing goes to the worker session
  - use `Ctrl-G` then the next key for shell commands
- shell commands:
  - `q` quit
  - `i` toggle inspector
  - `p` toggle activity strip
  - `r` refresh state
  - `s` toggle compact status overlay
  - `h` toggle help
  - `a` stage a shell-local draft from surfaced local scratch
  - `e` edit the staged shell-local draft in the worker pane
  - `m` send the current draft through Tuku
  - `x` clear the staged shell-local draft
  - while editing the staged draft:
    - normal typing edits the shell-local draft instead of the worker session
    - `Ctrl-G` then `s` saves the edited draft and exits edit mode
    - `Ctrl-G` then `c` cancels edit mode and restores the last saved draft
  - `tab` cycle focus

Optional shell host environment:
- `TUKU_SHELL_CODEX_BIN=/absolute/path/to/codex`
- `TUKU_SHELL_CODEX_ARGS="..."` to append extra Codex args
- `TUKU_SHELL_CLAUDE_BIN=/absolute/path/to/claude`
- `TUKU_SHELL_CLAUDE_ARGS="..."` to append extra Claude args
- shell Claude host also falls back to `TUKU_CLAUDE_BIN` and `TUKU_CLAUDE_ARGS` if the shell-specific vars are not set

## Manual Smoke Test (End-to-End)
Run these in a second terminal while daemon is running.

1) Start a task:
```bash
go run ./cmd/tuku start --goal "Implement manual smoke path" --repo /Users/kagaya/Desktop/Tuku
```
Copy `task_id` from output.

2) Send message (intent + brief generation):
```bash
go run ./cmd/tuku message --task <TASK_ID> --text "Start implementation and prepare handoff packet."
```

3) Exercise run lifecycle in safe local mode (`noop`):
```bash
go run ./cmd/tuku run --task <TASK_ID> --mode noop --action start
```
Copy `run_id`, then complete:
```bash
go run ./cmd/tuku run --task <TASK_ID> --action complete --run-id <RUN_ID>
```

4) Create checkpoint:
```bash
go run ./cmd/tuku checkpoint --task <TASK_ID>
```

5) Continue assessment:
```bash
go run ./cmd/tuku continue --task <TASK_ID>
```

6) Create handoff packet (Claude target):
```bash
go run ./cmd/tuku handoff-create --task <TASK_ID> --target claude --mode resume --reason "manual smoke handoff"
```
Copy `handoff_id` from output.

7) Accept handoff (optional but useful for audit trail):
```bash
go run ./cmd/tuku handoff-accept --task <TASK_ID> --handoff <HANDOFF_ID> --by claude --note "manual acceptance"
```

8) Launch handoff:
```bash
go run ./cmd/tuku handoff-launch --task <TASK_ID> --handoff <HANDOFF_ID>
```

9) Inspect full state:
```bash
go run ./cmd/tuku inspect --task <TASK_ID>
```

10) Open the full-screen shell:
```bash
go run ./cmd/tuku shell --task <TASK_ID>
```

11) Read status summary:
```bash
go run ./cmd/tuku status --task <TASK_ID>
```

12) Optional: inspect proof ledger directly from SQLite:
```bash
sqlite3 "$HOME/Library/Application Support/Tuku/tuku.db" \
  "SELECT sequence_no,type,run_id,substr(payload_json,1,140) FROM proof_events WHERE task_id='<TASK_ID>' ORDER BY sequence_no DESC LIMIT 20;"
```

## Expected Smoke Signals
- `handoff-launch` response is canonical and explicitly avoids claiming downstream completion.
- `tuku shell` opens a full-screen interface with:
  - top status bar
  - worker-first pane that attempts the selected live worker PTY session first
  - toggleable right inspector
  - toggleable activity/proof strip
- shell chrome shows a session id for the current shell run
- if a prior shell ended or fell back, the next shell run surfaces that prior persisted outcome in secondary chrome
- if another attachable shell session is already known for the same task, the shell surfaces that in calm secondary chrome instead of pretending this session is alone
- if another active but non-attachable shell session is known, the shell says so without implying reconnect support
- if the daemon only knows a stale shell session, the shell says so as uncertain liveness rather than presenting it as active
- if PTY startup fails, the footer shows fallback context and the center pane falls back to transcript mode
- `inspect` includes:
  - latest intent/brief/run/checkpoint
- `handoff-create` response includes the durable handoff packet.
- `handoff-launch` response includes the launch payload and canonical launch outcome.
- proof ledger includes handoff launch and acknowledgment events.

## Current Milestone Limitations
- The shell is terminal-native, not a separate dashboard app.
- Live worker PTY support currently covers Codex and Claude only.
- The PTY host is pragmatic, not a full terminal emulator.
- ANSI/control-sequence handling is intentionally lightweight.
- Claude PTY support shares the same shell chrome and lifecycle model, but no reattach or reconnect path exists yet.
- Shell session registry metadata now survives daemon restarts via SQLite-backed durable storage.
- Stale-session classification is threshold-based only; there is no background reconciler yet.
- Only major shell lifecycle milestones are persisted into proof; raw PTY output and shell input are not.
- No background reconciliation services.
- No multi-agent orchestration.
- No full Claude session lifecycle tracking after launch invocation.
- Launch success means launch invocation completed, not downstream coding completion.
