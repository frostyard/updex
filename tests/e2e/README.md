# End-to-end tests

updex's e2e suite is [`e2e_test.go`](e2e_test.go) in this directory: a
black-box suite that builds the real `updex` binary and runs it as a
subprocess against fake files and HTTP sources. Successful operations are
read-only (no root required); mutating command variants are covered at the
argument-validation boundary. CLI integration tests in `cmd/updex/`
additionally override package search roots to exercise default component
discovery and fake catalogs safely.

Run it with:

```bash
go test -v ./tests/e2e/...             # the e2e suite alone
go test -v ./cmd/updex ./tests/e2e/... # plus the CLI integration tests
```

CI runs it in the `E2E Tests` job of
[`.github/workflows/test.yml`](../../.github/workflows/test.yml); the canonical
credential-free `make ci` gate also runs the same command after its coverage
check and before its non-E2E race tests. This README is the discoverable e2e
entry point named by
[ADR-0012](../../docs/adr/0012-acmm-conformance-via-canonical-aliases.md).
