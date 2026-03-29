# Release Checklist

Use this checklist before cutting a release or announcing a significant update.

## 1. Repo hygiene

- [ ] `README.md` reflects current command surface and constraints.
- [ ] `LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, and `CODE_OF_CONDUCT.md` are present and current.
- [ ] `.gitignore` covers local/runtime artifacts.

## 2. Build and test

- [ ] `go test ./...` passes on the release commit.
- [ ] CI is green for the release commit.
- [ ] No accidental debug artifacts or local dump files are staged.

## 3. Product truth discipline

- [ ] Operator-facing wording remains bounded and conservative.
- [ ] No overclaims of correctness/completion/root-cause certainty.
- [ ] No unintended authority/policy changes.

## 4. Contract checks

- [ ] JSON/IPC compatibility preserved, or additive-only changes documented.
- [ ] Cross-surface wording parity preserved for status/inspect/shell/CLI human surfaces.

## 5. Change communication

- [ ] Summarize key capabilities added.
- [ ] Summarize intentionally unproven/non-goals.
- [ ] Call out migration notes (if any).

## 6. Tagging and publish

- [ ] Create annotated tag for release commit.
- [ ] Publish release notes with concise test and risk summary.
