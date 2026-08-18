# Quality loop

Living document. Rationale:
[ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md).
Contracts: [specs/pr-review-rubric.md](../specs/pr-review-rubric.md),
[specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md).
`docs/quality.md` is a conformance alias for this file (ADR-0012); this page
is also the quality dashboard (formerly `docs/AI-QUALITY-ASSURANCE.md`).

[![Tests](https://github.com/frostyard/updex/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/frostyard/updex/actions/workflows/test.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/frostyard/updex/graph/badge.svg?branch=main)](https://codecov.io/gh/frostyard/updex)

## Overview

How change quality is proposed, gated, observed, and learned from in this
repo. Changes produced with AI assistance follow the same loop as every
other contribution: they are never merged solely on the basis of generated
output, and the [AI security policy](../security/SECURITY-AI.md) bounds the
access, data handling, human oversight, and tool safety of that assistance.
One loop, five stations:

```
PR template ──► review rubric ──► CI gates ──► corrections ──► promotion
(checklist)     (spec)            (test.yml,   (.memory/)      (AGENTS.md,
     ▲                             docs-gate)                    docs, skills)
     └────────────── acceptance metric (spec) observes the stream ─────────┘
```

## Design

- **Declare** — [.github/pull_request_template.md](../../.github/pull_request_template.md)
  makes every PR walk the build-gate, risk-tier
  ([docs/risk-tiers.md](../risk-tiers.md)), and docs-housekeeping
  checklists; `.github/workflows/pr-title.yml` enforces a Conventional
  Commits title.
- **Review** — the [PR review rubric](../specs/pr-review-rubric.md) is the
  contract a review applies; the
  [review runbook](../../.github/prompts/review.prompt.md) is its
  task-shaped form for agents. `Automated Copilot Code Review`
  (`.github/workflows/claude-code-review.yml`) requests an advisory review
  for each non-draft PR; maintainers audit its findings and CI history
  before merge and remain accountable for the decision.
- **Gate** — [.github/workflows/test.yml](../../.github/workflows/test.yml)
  runs on every PR and push to `main`:
  - *Lint* (pinned golangci-lint — the release named by
    `GOLANGCI_LINT_VERSION` in the `Makefile`, which the Lint job reads
    and `make ci` asserts via `make lint-version-check`, configured by
    `.golangci.yml`), *Security Scan* (pinned
    `govulncheck`), *Unit Tests* with coverage uploaded to Codecov, *E2E
    Tests* ([tests/e2e/README.md](../../tests/e2e/README.md)), *Race
    Detection*, *Verify* (tidy, `go vet`, gofmt), and *Build* for linux
    amd64/arm64 — `make ci` reproduces the credential-free subset locally in
    the same fail-fast order.
  - *Docs integrity* (`docs-gate`): `node scripts/check-docs.mjs` checks
    docs-index coverage, relative-link integrity, and symlink resolution
    against [.coverage-thresholds.json](../../.coverage-thresholds.json) —
    all 1.0, `never_relax: true` (the loop may tighten, never loosen).
  - Codecov (`codecov.yml`) gates project coverage (no more than a 1% drop
    against the base commit) and patch coverage (70% of changed lines).
  - `Nightly compliance` (`.github/workflows/nightly-compliance.yml`, 03:27
    UTC and on dispatch) re-verifies downloaded module content, requires a
    clean `go mod tidy`, queries the current Go vulnerability database with
    a pinned scanner, runs the whole suite uncached under the race detector,
    runs `go vet`, and cross-builds the supported Linux architectures. It
    has read-only permissions, persists no credentials, and never modifies
    repository state; a failure is a signal for maintainer investigation.
- **Learn** — corrections land in
  [.memory/corrections.jsonl](../../.memory/README.md) (append-only,
  five-field schema) and are promoted into `AGENTS.md`, docs, or skills;
  promotion is the only sanctioned duplication. Session continuity lives in
  [.claude/session-summary.md](../../.claude/session-summary.md).
- **Enforce mechanically** — [.claude/settings.json](../../.claude/settings.json)
  denies the forbidden acts at the tool layer: merging PRs (`gh pr merge`),
  approving own work (`gh pr review --approve`), publishing releases
  (`gh release`), and pushing to `main`;
  [.github/policies/ai-governance.json](../../.github/policies/ai-governance.json)
  states the same controls as machine-readable policy
  ([frostyard/core ADR-0019](https://github.com/frostyard/core/blob/main/docs/adr/0019-governance-as-code-and-risk-tiers.md)).
- **Observe** — the [PR acceptance metric](../specs/pr-acceptance-metric.md)
  summarizes the stream; it informs, never gates. The
  [public metrics index](../metrics/README.md) maps every live signal
  (CI, nightly, Codecov, PR stream, AI-fix runs, releases) to its public
  source. `.github/auto-qa-tuning.json` turns a sustained regression of the
  metric into tighter guidance or a targeted local check, never a relaxed
  gate.

## Automation surfaces

- **AI fix requests** — adding the `ai-fix-requested` label to an open
  issue runs [`ai-fix-requested.yml`](../../.github/workflows/ai-fix-requested.yml),
  which assigns the issue to GitHub Copilot on the default branch; a
  maintainer can retry with the workflow's `issue_number` input. The
  workflow needs a repository secret `COPILOT_ASSIGN_PAT` (a Copilot-licensed
  maintainer's user token scoped to this repository: read metadata,
  read/write Actions, Contents, Issues, Pull requests). It never checks out
  issue-authored content and grants no permissions to its `GITHUB_TOKEN`;
  failed assignments stay visibly labeled and can be retried.
- **Auto-labeling** — `Pull Request Labeler` applies documentation, Go,
  GitHub Actions, and dependency labels per `.github/labeler.yml` without
  removing labels applied by people.

## Human oversight

AI assistance is disclosed when the contribution process requires it.
Maintainers remain responsible for reviewing behavior, security
implications, test evidence, and documentation, and may request revisions or
reject a change regardless of automated results. Suspected defects or
security concerns go through the repository's standard issue or private
security-reporting channels ([AGENTS.md — Security](../../AGENTS.md#security)),
never public logs when they contain sensitive information.

## Operational notes

Re-run every gate locally before pushing:

```
make fmt && make ci
node scripts/check-docs.mjs
go test -v ./cmd/updex ./tests/e2e/...   # for CLI / e2e changes
```

Failure modes: a broken alias or missing index line fails docs-gate (fix the
canonical target or the index, never the alias); a lint finding after
pinning `.golangci.yml` means the gate was already red — fix the finding,
never loosen the config; a Codecov patch failure means changed lines lack
tests.

## References

- Rationale: [ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)
- Contracts: [specs/pr-review-rubric.md](../specs/pr-review-rubric.md),
  [specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md)
- Signals: [public metrics index](../metrics/README.md)
- Policy: [risk tiers](../risk-tiers.md),
  [AI security policy](../security/SECURITY-AI.md),
  [`.github/policies/ai-governance.json`](../../.github/policies/ai-governance.json)
