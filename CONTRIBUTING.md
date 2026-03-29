# Contributing to Tuku

Thanks for contributing.

Tuku is a local-first orchestration and continuity control plane. Please optimize for small, reviewable changes that preserve canonical task truth and bounded advisory semantics.

## Development Setup

Requirements:

- Go 1.22+
- POSIX shell environment

Run locally:

```bash
go build ./cmd/tuku ./cmd/tukud
go test ./...
```

## Branch and PR Workflow

1. Create a branch from `main`.
2. Keep commits focused and reviewable.
3. Run formatting and tests before pushing.
4. Open a PR with:
   - problem statement
   - scope boundaries
   - tests added/updated
   - explicit non-goals

## Engineering Expectations

- Preserve package boundaries and domain contracts.
- Prefer deterministic bounded derivations over opaque behavior.
- Keep wording conservative in operator-facing surfaces.
- Avoid semantic overreach:
  - do not imply correctness/completion/root-cause resolution unless proven by existing model semantics
  - do not widen authority semantics without explicit design approval

## Coding Standards

Before commit:

```bash
gofmt -w $(git ls-files '*.go')
go vet ./...
go test ./...
```

General guidelines:

- Avoid large refactors unless explicitly requested.
- Add focused tests with any behavior change.
- Preserve JSON/IPC compatibility unless a change is intentionally additive and documented.

## Reporting Bugs

Open an issue with:

- expected behavior
- observed behavior
- reproduction steps
- logs/output snippets (redacted)
- OS and Go version

For security issues, see [SECURITY.md](SECURITY.md).
