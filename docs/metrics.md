# Project metrics

## Pull request acceptance

The pull request acceptance rate measures how often resolved pull requests are
merged:

```text
accepted PRs / (accepted PRs + closed, unmerged PRs) × 100
```

An accepted PR is any pull request merged during the reporting period. Open
pull requests are excluded. Report the rate monthly from GitHub pull request
data, using the merge or close date to assign each pull request to a period.
Track the accepted and closed-unmerged counts alongside the percentage so that
changes in review volume remain visible.

### Reporting

After each month closes, set the inclusive UTC date range and calculate the
metric from GitHub:

```bash
START=2026-07-01
END=2026-07-31

gh pr list --state all --search "closed:${START}..${END}" --limit 1000 \
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
rate.
