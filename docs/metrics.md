# Pull Request Metrics

updex tracks pull request acceptance rate as a feedback signal for contributor
and agent changes.

## Acceptance rate

For a reporting period, the acceptance rate is:

```text
merged pull requests / (merged pull requests + closed, unmerged pull requests)
```

Count pull requests by the date they were merged or closed. Open pull requests
are excluded because their outcome is not yet known. Report the raw merged and
closed-unmerged counts alongside the percentage, and review the metric at least
quarterly.

Acceptance rate is a trend, not a quality gate by itself. Review changes in the
rate together with reviewer feedback and CI results to identify recurring
causes of rejected changes and improve contribution guidance.
