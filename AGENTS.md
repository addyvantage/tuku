# Tuku Agent Rules (Repository Contract)

This repository implements Tuku v1 as a local-first orchestration brain.

## Product authority
- Tuku owns the canonical conversation and canonical task state.
- Execution workers (Codex, Claude later) are worker adapters inside Tuku.
- Never bypass Tuku canonical response synthesis.

## Canonical response rule
- Do not return raw worker output as final user-facing response.
- Always ingest output, merge into state, ground in proof events, then synthesize response.

## Scope discipline (v1)
- Build only v1 local-first runtime and CLI/daemon foundations.
- No broad web UI.
- No multi-agent role orchestration.
- No cloud dependency for core runtime.
- No automation connectors.

## Technical constraints
- Go monorepo.
- SQLite local persistence.
- Unix socket IPC for CLI <-> daemon.
- Small, reviewable slices; avoid giant refactors.
- Preserve package boundaries and domain contracts.
