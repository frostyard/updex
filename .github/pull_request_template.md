<!-- The org squash-merges: branch off main, never stack on another PR's
branch. Title and commits use Conventional Commits (`type(scope): summary`)
— .github/workflows/pr-title.yml enforces it. Reviews apply
docs/specs/pr-review-rubric.md. -->

## Summary

<!-- What changes and why, in a few sentences. Link the issue(s) this
closes. -->

## Checks

<!-- The build gate from AGENTS.md — run before opening the PR. -->

- [ ] `make fmt` — code is formatted
- [ ] `make ci` — tidy, vet, gofmt, lint (`.golangci.yml`), unit tests, the
      80.0% coverage floor (`make coverage-check`), race tests, linux
      amd64/arm64 builds
- [ ] CLI/e2e changes: `go test -v ./cmd/updex ./tests/e2e/...` green
- [ ] New or changed behavior has focused tests, including failure paths

## Risk classification

<!-- Select the highest applicable tier from docs/risk-tiers.md and give the
rationale. -->

- [ ] Tier 1: Low
- [ ] Tier 2: Moderate
- [ ] Tier 3: High

**Rationale:**

-

## Docs housekeeping

<!-- Delete rows that don't apply (no docs touched). -->

- [ ] `README.md`, `docs/design/overview.md`, `docs/specs/*` updated for
      behavior changes; `AGENTS.md` for convention/workflow changes
- [ ] New docs started from their category's `TEMPLATE.md` and indexed in
      `docs/README.md`
- [ ] New significant decision recorded as an ADR *first*, in this PR
- [ ] Conformance aliases (ADR-0012) untouched — canonical targets edited
      instead

## Verification

<!-- Paste evidence the gates ran locally. -->

- [ ] `node scripts/check-docs.mjs` green
- [ ] Checked against the
      [PR review rubric](https://github.com/frostyard/updex/blob/main/docs/specs/pr-review-rubric.md)
