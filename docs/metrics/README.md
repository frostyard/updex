# Public metrics

updex publishes repository quality and automation signals from public,
auditable sources. This page is the stable index for those signals and defines
the pull-request acceptance metric used by the Auto-QA feedback policy. It
contains no secrets, private prompts, or host telemetry from updex users.

## Signal index

| Signal | Public source | Interpretation |
| --- | --- | --- |
| Continuous integration | [Tests workflow runs](https://github.com/frostyard/updex/actions/workflows/test.yml) | Lint, vulnerability, unit, end-to-end, race, verification, and build evidence per commit. |
| Nightly compliance | [Nightly workflow runs](https://github.com/frostyard/updex/actions/workflows/nightly-compliance.yml) | Fresh dependency, vulnerability, race, static-analysis, and supported-build evidence. |
| Coverage | [Codecov dashboard](https://codecov.io/gh/frostyard/updex) | Project and patch coverage interpreted under [`codecov.yml`](../../codecov.yml). |
| Pull-request acceptance | [Closed pull requests](https://github.com/frostyard/updex/pulls?q=is%3Apr+is%3Aclosed) | Monthly ratio defined below; review with rejection reasons and feedback. |
| AI fix activity | [AI Fix Requested workflow runs](https://github.com/frostyard/updex/actions/workflows/ai-fix-requested.yml) | Auditable assignment attempts for issues explicitly labeled for automated work. |
| Releases | [Release workflow runs](https://github.com/frostyard/updex/actions/workflows/release.yml) | Publication evidence from reviewed repository history. |
| Quality-gate definitions | [Quality dashboard](../AI-QUALITY-ASSURANCE.md#quality-dashboard) | Canonical expectations and versioned gate sources for each signal. |

These links expose source evidence rather than a copied snapshot that can go
stale. A failed or missing signal must be investigated at its source; it is not
silently converted into a passing value here.

## Pull-request acceptance

The pull-request acceptance rate measures how often resolved pull requests are
merged:

```text
accepted PRs / (accepted PRs + closed, unmerged PRs) × 100
```

An accepted PR is any pull request merged during the reporting period. Open
pull requests are excluded. Report the rate monthly from GitHub pull-request
data, using the merge or close date to assign each pull request to a period.
Track the UTC month, accepted count, closed-unmerged count, and percentage so
that changes in review volume remain visible.

### Reporting

After each month closes, set the inclusive UTC date range and calculate the
metric from GitHub:

```bash
START=2026-07-01
END=2026-07-31

gh pr list --repo frostyard/updex --state all \
  --search "closed:${START}..${END}" --limit 1000 \
  --json mergedAt |
  jq '
    . as $prs
    | ($prs | map(select(.mergedAt != null)) | length) as $accepted
    | ($prs | map(select(.mergedAt == null)) | length) as $closed
    | {
        accepted: $accepted,
        closed_unmerged: $closed,
        acceptance_rate: (
          if ($accepted + $closed) == 0 then null
          else (($accepted * 10000 / ($accepted + $closed) | round) / 100)
          end
        )
      }
  '
```

The rate is a percentage rounded to two decimal places. A month with no
resolved pull requests reports `null` rather than a misleading 0% acceptance
rate. Interpret it alongside rejection reasons, superseded work, CI outcomes,
review feedback, and sample size; acceptance alone is not a quality score for
human or automated contributors.

## Publication and privacy contract

Only repository metadata and validation evidence already public through GitHub
or Codecov belongs in this index. Do not publish credentials, workflow secrets,
private prompts, personal data, embargoed vulnerability findings, or telemetry
from systems managed with updex. Model output and generated reports remain
untrusted until reviewed. Public metrics are audit evidence, not approval and
not a substitute for required checks or maintainer review.
