# Spec: PR review rubric

One paragraph: the checklist every frostyard/updex pull-request review
applies, kept consistent, actionable, and focused on the risks the pull
request introduces. Consumers: human reviewers, the
[review runbook](../../.github/prompts/review.prompt.md), the
[PR template](../../.github/pull_request_template.md), whose sections mirror
these checks, and [`.github/policies/ai-governance.json`](../../.github/policies/ai-governance.json),
which names this file as the authoritative review source.
`docs/review-rubric.md` is a conformance alias for this file
([ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)).

## Interface

Every review verifies each row; a PR merges only when all applicable rows
pass.

| Check | How to verify |
| --- | --- |
| Risk classification | The author selected the highest applicable tier from the [risk tier guide](../risk-tiers.md) and gave a rationale; the tests, analysis, documentation, and oversight that tier requires are present. |
| Correctness and scope | The change solves the linked problem and handles the relevant error cases; the diff is focused — no unrelated refactors or generated artifacts. |
| Architecture and API | Business logic lives in the public SDK (`updex/` and the supporting packages), CLI code in `cmd/` is a thin wrapper; SDK methods take `context.Context` first plus an options struct and return typed results; SDK packages import no CLI package; breaking API changes are intentional and documented in [specs/sdk-api.md](sdk-api.md). |
| Security and reliability | Inputs, paths, downloaded content, and managed files are validated (`catalog.ValidateSysextName`, `os.Lstat` on managed paths); SHA256 and GPG verification are never bypassed; errors expose no credentials; multi-step mutations snapshot and roll back ([ADR-0005](../adr/0005-transactional-writes-lstat-checks.md)). |
| Build gate green | `make fmt` leaves no diff and `make ci` passes: tidy, `go vet`, gofmt, golangci-lint (`.golangci.yml`), non-E2E unit tests, the 80.0% total statement-coverage floor (`make coverage-check`, `scripts/check-coverage.sh`), race tests, linux amd64/arm64 builds — the same fail-fast order as [`.github/workflows/test.yml`](../../.github/workflows/test.yml). |
| Tests | New or changed behavior has focused, table-driven tests including meaningful failure paths, or the PR explains why tests do not apply; CLI/e2e changes also pass `go test -v ./cmd/updex ./tests/e2e/...`; total statement coverage stays at or above the enforced 80.0% floor (`make coverage-check`). Codecov's project (−1% max) and patch (70%) statuses from `codecov.yml` apply only once the repository is onboarded on codecov.io and are advisory until then. |
| Docs housekeeping | User-facing (`README.md`) and agent-oriented docs (`docs/design/overview.md`, `docs/specs/*`, `AGENTS.md`) reflect the behavior change; new docs start from their category `TEMPLATE.md`, are indexed in [docs/README.md](../README.md), and cross-link both ways; a new significant decision ⇒ ADR first, in the same change. |
| Docs-integrity gate green | `node scripts/check-docs.mjs` passes: every doc indexed, every relative link resolving, every symlink alias intact (thresholds in `.coverage-thresholds.json`). |
| Aliases untouched | Conformance aliases ([ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)) are not edited directly; canonical targets are. |
| Conventional title | The PR title (or lone commit subject) is `type(scope): summary`; `.github/workflows/pr-title.yml` is green. |
| Agent limits respected | The PR was not merged, approved, or released by the agent that authored it; mechanically backed by `.claude/settings.json` and `.github/policies/ai-governance.json`. |

## Rules

- Each check is independently verifiable from the PR diff plus the commands
  named in its row — a review MUST NOT rely on out-of-band context.
- Label findings by impact:
  - **Blocking:** a correctness, security, compatibility, data-loss, or
    required test/documentation issue that must be resolved before approval.
  - **Non-blocking:** a worthwhile improvement that does not prevent merging.
  - **Question:** a request for context or clarification, not an assumed
    defect.
  - **Nit:** an optional minor style suggestion; avoid nits already enforced
    by formatting or linting.
- Comments identify the affected behavior, explain its impact, and suggest a
  concrete resolution. Reviewers re-check resolved blocking findings and
  confirm required CI checks pass before approval.
- Rubric changes ride with the artifact that enforces them (the gate script,
  the workflow, or the template) in the same PR.
- The org squash-merges: the review covers the squashed result, not
  intermediate commits.

## References

- Rationale: [ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)
- Context: [design/quality-loop.md](../design/quality-loop.md)
- Related: [risk tiers](../risk-tiers.md),
  [AI security policy](../security/SECURITY-AI.md)
