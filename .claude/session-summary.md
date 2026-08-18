# Session summary

Ephemeral session state — agents replace the block below at session end
(session state lives in `.claude/`). Durable learnings go to
[.memory/](../.memory/README.md), never here; ongoing work is tracked in
GitHub issues (`gh --repo frostyard/updex`). Never include credentials,
tokens, private user data, or raw command output; link issues, PRs, and
commits instead of copying logs.

## Current state

- ACMM conformance realigned with the fleet (2026-08-18):
  [ADR-0012](../docs/adr/0012-acmm-conformance-via-canonical-aliases.md)'s
  alias lattice (`AGENTS.md` canonical; `docs/specs/pr-review-rubric.md`,
  `docs/specs/pr-acceptance-metric.md`, `docs/design/quality-loop.md`
  canonical with `docs/review-rubric.md`, `docs/metrics.md`,
  `docs/quality.md` aliases), the docs-integrity gate
  (`scripts/check-docs.mjs`, `docs-gate` CI job), and `.claude/settings.json`
  tool-layer limits.

## Last landed

- #313 propagate golangci-lint failures; #322/#319 catalog GitHub-token
  origin restriction; #316 scanner JSON output fix.

## Next

- #323 (catalog HTTPS-only URLs / redirect downgrade refusal) — separate
  security work item.
- #274 (daemon lifecycle orchestration behind the SDK boundary) — separate
  architecture work item.
