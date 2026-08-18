# Public metrics

updex publishes repository quality and automation signals from public,
auditable sources. This page is the stable index for those signals; the
pull-request acceptance metric consumed by the Auto-QA feedback policy is
defined in [specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md)
(`docs/metrics.md` resolves to the same file). It contains no secrets,
private prompts, or host telemetry from updex users. This directory is a real
tree because the public-metrics criterion names `docs/metrics/`
([ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)).

## Signal index

| Signal | Public source | Interpretation |
| --- | --- | --- |
| Continuous integration | [Tests workflow runs](https://github.com/frostyard/updex/actions/workflows/test.yml) | Lint, vulnerability, unit, end-to-end, race, verification, docs-integrity, and build evidence per commit. |
| Nightly compliance | [Nightly workflow runs](https://github.com/frostyard/updex/actions/workflows/nightly-compliance.yml) | Fresh dependency, vulnerability, race, static-analysis, and supported-build evidence. |
| Coverage | [Tests workflow runs](https://github.com/frostyard/updex/actions/workflows/test.yml) — Unit Tests job, step `Enforce coverage floor` (`make coverage-check`, [`scripts/check-coverage.sh`](../../scripts/check-coverage.sh)) | The enforced gate: an 80.0% total statement-coverage floor over the non-E2E packages; a regression below the floor fails the run. The [Codecov dashboard](https://codecov.io/gh/frostyard/updex) is best-effort only, pending onboarding on codecov.io — the upload is `continue-on-error` and [`codecov.yml`](../../codecov.yml) records the *intended* project/patch statuses, which take effect only once the repository is onboarded; until then Codecov reports nothing and is not a gate. |
| Pull-request acceptance | [Closed pull requests](https://github.com/frostyard/updex/pulls?q=is%3Apr+is%3Aclosed) | Monthly ratio per [specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md); review with rejection reasons and feedback. |
| AI fix activity | [AI Fix Requested workflow runs](https://github.com/frostyard/updex/actions/workflows/ai-fix-requested.yml) | Auditable assignment attempts for issues explicitly labeled for automated work. |
| Releases | [Release workflow runs](https://github.com/frostyard/updex/actions/workflows/release.yml) | Publication evidence from reviewed repository history. |
| Quality-gate definitions | [Quality loop](../design/quality-loop.md) | Canonical expectations and versioned gate sources for each signal. |

These links expose source evidence rather than a copied snapshot that can go
stale. A failed or missing signal must be investigated at its source; it is not
silently converted into a passing value here.

## Pull-request acceptance

Defined in [specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md):
`accepted PRs / (accepted PRs + closed, unmerged PRs) × 100` per UTC month,
with the reproducible `gh` + `jq` reporting command, the `null`-for-empty-month
rule, and the Auto-QA tuning guardrails.

## Publication and privacy contract

Only repository metadata and validation evidence already public through GitHub
or Codecov belongs in this index. Do not publish credentials, workflow secrets,
private prompts, personal data, embargoed vulnerability findings, or telemetry
from systems managed with updex. Model output and generated reports remain
untrusted until reviewed. Public metrics are audit evidence, not approval and
not a substitute for required checks or maintainer review.
