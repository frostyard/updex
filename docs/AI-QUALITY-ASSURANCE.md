# AI Quality Assurance

Changes produced with AI assistance follow the same review and quality gates as
all other contributions to updex. AI-generated changes are not merged solely on
the basis of generated output.

[![Tests](https://github.com/frostyard/updex/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/frostyard/updex/actions/workflows/test.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/frostyard/updex/graph/badge.svg?branch=main)](https://codecov.io/gh/frostyard/updex)

## Quality dashboard

The badges above report the current state of the default branch. Use the live
views below to inspect individual workflow runs, pull request checks, and
coverage trends.

| Signal | Live view | Quality gate |
| --- | --- | --- |
| Continuous integration | [Tests workflow](https://github.com/frostyard/updex/actions/workflows/test.yml) | Lint, security, unit, end-to-end, race, verification, and build jobs pass |
| Nightly compliance | [Nightly compliance workflow](https://github.com/frostyard/updex/actions/workflows/nightly-compliance.yml) | Dependency integrity, current vulnerability data, uncached race tests, static analysis, and supported Linux builds pass |
| Test coverage | [Codecov dashboard](https://codecov.io/gh/frostyard/updex) | Project coverage stays within the configured 1% threshold and changed lines meet the 70% target |
| Pull request review | [Open pull requests](https://github.com/frostyard/updex/pulls) | Reviewers can audit the diff and CI history before merge |
| Releases | [Release workflow](https://github.com/frostyard/updex/actions/workflows/release.yml) | Release artifacts are built from reviewed repository history |

The workflow definitions and exact coverage tolerances remain versioned in
[`.github/workflows/test.yml`](../.github/workflows/test.yml),
[`.github/workflows/nightly-compliance.yml`](../.github/workflows/nightly-compliance.yml),
and [`codecov.yml`](../codecov.yml), so dashboard results can be traced to the
gates that produced them.

## Nightly compliance

The nightly workflow runs against the latest default-branch revision at 03:27
UTC and can also be dispatched manually. It re-verifies downloaded module
content, requires `go mod tidy` to remain clean, queries the current Go
vulnerability database with a pinned scanner, runs the complete suite uncached
under the race detector, runs `go vet`, and cross-builds the supported Linux
architectures.

The job has read-only repository permissions, persists no checkout credentials,
uses no secrets, and never publishes or modifies repository state. A failure is
a signal for maintainer investigation; it does not automatically weaken a gate
or modify code.

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
