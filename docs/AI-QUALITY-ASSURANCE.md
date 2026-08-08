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
| Test coverage | [Codecov dashboard](https://codecov.io/gh/frostyard/updex) | Project coverage stays within the configured 1% threshold and changed lines meet the 70% target |
| Pull request review | [Open pull requests](https://github.com/frostyard/updex/pulls) | Reviewers can audit the diff and CI history before merge |
| AI fix requests | [AI Fix Requested workflow](https://github.com/frostyard/updex/actions/workflows/ai-fix-requested.yml) | Labeled issues are assigned to Copilot through an auditable workflow run |
| Releases | [Release workflow](https://github.com/frostyard/updex/actions/workflows/release.yml) | Release artifacts are built from reviewed repository history |

The workflow definitions and exact coverage tolerances remain versioned in
[`.github/workflows/test.yml`](../.github/workflows/test.yml) and
[`codecov.yml`](../codecov.yml), so dashboard results can be traced to the
gates that produced them.

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

## AI fix requests

Adding the `ai-fix-requested` label to an open issue runs
[`ai-fix-requested.yml`](../.github/workflows/ai-fix-requested.yml), which
assigns the issue to GitHub Copilot on the repository's default branch. A
maintainer can retry a request with the workflow's `issue_number` manual input.
The issue must still be open and carry the label, and an issue already assigned
to Copilot is treated as complete.

The workflow requires a repository secret named `COPILOT_ASSIGN_PAT`. It must
contain a user token from a Copilot-licensed maintainer, scoped only to this
repository. For a fine-grained token, grant read access to metadata and
read/write access to Actions, Contents, Issues, and Pull requests, as required
by GitHub's agent-assignment API. The workflow does not check out issue-authored
content or grant permissions to its `GITHUB_TOKEN`; it uses the user token only
for fixed GitHub API requests. Failed assignments remain visibly labeled and
can be retried after correcting the configuration.

## Human oversight

AI assistance must be disclosed when required by the contribution process.
Maintainers remain responsible for reviewing behavior, security implications,
test evidence, and documentation. A maintainer may request revisions or reject
a change regardless of whether automated checks pass.

Suspected defects or security concerns should be reported through the
repository's standard issue or security-reporting channels rather than being
included in public logs when they contain sensitive information.
