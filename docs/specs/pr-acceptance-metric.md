# Spec: PR acceptance metric

One paragraph: defines the single quality metric this repo tracks for its
pull-request stream — the monthly acceptance rate — precisely enough that any
agent or human computes the same number from the same window. Consumers: the
[quality loop](../design/quality-loop.md), the
[public metrics index](../metrics/README.md), and the Auto-QA feedback policy
in [`.github/auto-qa-tuning.json`](../../.github/auto-qa-tuning.json), whose
`pr_acceptance_rate` signal points at this file's `## Definition`.
`docs/metrics.md` is a conformance alias for this file
([ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)).
`scripts/check-docs.mjs` pins this file's `## Definition` and `## Rules`
headings so prose and tooling cannot drift apart silently.

## Definition

```text
accepted PRs / (accepted PRs + closed, unmerged PRs) × 100
```

| Term | Meaning |
| --- | --- |
| Accepted PR | Any pull request merged into `main` during the reporting period |
| Closed, unmerged PR | Any pull request closed without merging during the reporting period |
| Reporting period | One UTC calendar month; a PR belongs to the month of its merge or close date |
| Open PRs | Excluded until merged or closed |

The result is a percentage rounded to two decimal places. A month with no
resolved pull requests reports `null` rather than a misleading 0%.

Data source (`--repo frostyard/updex` is explicit so the command works from
any checkout):

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

## Rules

- Report the rate monthly after the UTC month closes; track the month, the
  accepted count, the closed-unmerged count, and the percentage so changes in
  review volume stay visible alongside the rate.
- Interpret it with rejection reasons, superseded work, CI outcomes, review
  feedback, and sample size — acceptance alone is not a quality score for
  human or automated contributors.
- `.github/auto-qa-tuning.json` holds the current policy when a window has
  fewer than ten resolved pull requests; with enough data, a relative
  regression of ten percent or more routes the observed failure pattern to
  focused contributor guidance or a targeted local check, and relaxation
  requires two consecutive improved windows. Required, security, and
  coverage checks are never relaxed by this loop.
- Only repository metadata already public through GitHub belongs in the
  computation or its report — no credentials, private prompts, personal
  data, or telemetry from systems managed with updex (see the
  [publication and privacy contract](../metrics/README.md#publication-and-privacy-contract)).
- The metric is observational: it never gates a merge by itself; gates live
  in the [review rubric](pr-review-rubric.md) and CI.

## References

- Rationale: [ADR-0012](../adr/0012-acmm-conformance-via-canonical-aliases.md)
- Context: [design/quality-loop.md](../design/quality-loop.md),
  [public metrics index](../metrics/README.md)
- Contract test: `updex/public_metrics_contract_test.go` pins the formula,
  the metrics tree, and the Auto-QA signal path
