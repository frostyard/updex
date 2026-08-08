# AI Quality Assurance

Changes produced with AI assistance follow the same review and quality gates as
all other contributions to updex. AI-generated changes are not merged solely on
the basis of generated output. The
[AI security policy](security/SECURITY-AI.md) defines the access, data-handling,
human-oversight, and tool-safety boundaries for that assistance.

[![Tests](https://github.com/frostyard/updex/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/frostyard/updex/actions/workflows/test.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/frostyard/updex/graph/badge.svg?branch=main)](https://codecov.io/gh/frostyard/updex)

## Quality dashboard

The badges above report the current state of the default branch. Use the live
views below to inspect individual workflow runs, pull request checks, and
coverage trends.

| Signal | Live view | Quality gate |
| --- | --- | --- |
| Continuous integration | [Tests workflow](https://github.com/frostyard/updex/actions/workflows/test.yml) | Lint, security, unit, end-to-end, race, verification, and build jobs pass |
| Test coverage | [Codecov dashboard](https://codecov.io/gh/frostyard/updex) | Project coverage stays within the configured 1% threshold and changed lines meet the 70% target |
| Pull request review | [Open pull requests](https://github.com/frostyard/updex/pulls) | Reviewers can audit the diff and CI history before merge |
| Releases | [Release workflow](https://github.com/frostyard/updex/actions/workflows/release.yml) | Release artifacts are built from reviewed repository history |

The workflow definitions and exact coverage tolerances remain versioned in
[`.github/workflows/test.yml`](../.github/workflows/test.yml) and
[`codecov.yml`](../codecov.yml), so dashboard results can be traced to the
gates that produced them.

## Auto-QA self-tuning

`.github/auto-qa-tuning.json` defines the machine-readable feedback policy for
the monthly [pull request acceptance metric](metrics.md#pull-request-acceptance).
A window with fewer than ten resolved pull requests holds the current policy.
With enough data, a relative regression of ten percent or more routes the
observed failure pattern to focused contributor guidance or a targeted local
check. Relaxation requires two consecutive improved windows.

Required, security, and coverage checks are never relaxed. Any policy
adjustment must be reviewed through a pull request.

## Required checks

Contributors should run the repository's standard checks before requesting
review:

```bash
make fmt
make lint
make test
make build
```

Changes should include focused tests when behavior changes. Reviewers should
confirm that the implementation is limited to the requested scope, errors are
handled safely, and no credentials or other sensitive values are present.

## Human oversight

AI assistance must be disclosed when required by the contribution process.
Maintainers remain responsible for reviewing behavior, security implications,
test evidence, and documentation. A maintainer may request revisions or reject
a change regardless of whether automated checks pass.

Suspected defects or security concerns should be reported through the
repository's standard issue or security-reporting channels rather than being
included in public logs when they contain sensitive information.
