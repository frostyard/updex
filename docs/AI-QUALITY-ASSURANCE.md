# AI Quality Assurance

This page is the quality dashboard for changes produced with AI assistance.
AI-authored changes are held to the same review and validation standards as
all other contributions.

## Quality signals

| Signal | Source | Expected result |
| --- | --- | --- |
| Formatting and static analysis | [`Tests`](../.github/workflows/test.yml) | `gofmt`, `go vet`, and golangci-lint pass |
| Dependency security | [`Tests`](../.github/workflows/test.yml) | `govulncheck ./...` reports no known vulnerabilities |
| Unit and race tests | [`Tests`](../.github/workflows/test.yml) | Unit and race-detector jobs pass |
| Coverage | [Codecov configuration](../codecov.yml) | Project coverage drops by no more than 1%; patch coverage is at least 70% |
| Build portability | [`Tests`](../.github/workflows/test.yml) | Linux AMD64 and ARM64 builds pass |
| Human review | Pull request review | The change is focused, documented, tested, and free of committed secrets |

The pull request checks are the live dashboard. A change is quality-gated only
when every required check passes and review feedback is resolved.

## Local verification

Run the repository's complete local quality check before requesting review:

```bash
make check
make build
```

For AI-assisted changes, reviewers should also confirm that the implementation
matches the issue, does not include unrelated edits, and does not weaken
existing tests or security controls.
