# AI Quality Assurance

Changes produced with AI assistance follow the same review and quality gates as
all other contributions to updex. AI-generated changes are not merged solely on
the basis of generated output.

## Quality signals

- Pull requests expose test results from `.github/workflows/test.yml`.
- Code coverage is reported by Codecov. Project and patch thresholds are defined
  in `codecov.yml`.
- Reviewers can audit the complete pull request diff and CI history before
  merge.
- Releases are built from reviewed repository history through the release
  workflow.

These signals form the project's quality dashboard: pull request checks show
whether a change builds and passes tests, while Codecov reports project and
changed-line coverage.

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
