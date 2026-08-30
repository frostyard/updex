# Org-wide decisions (frostyard/core ADRs)

Conventions this repository follows that are decided at the org level are
recorded as ADRs in
[frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr).
The ones that bind updex:

- [ADR-0004 — Product-namespaced filesystem paths, split by lifetime tier](https://github.com/frostyard/core/blob/main/docs/adr/0004-product-namespaced-filesystem-tiers.md) — the /etc|run|usr/local/lib|usr/lib updex/catalogs.d config roots
- [ADR-0007 — The Frostyard sysext filename pattern and derived versions](https://github.com/frostyard/core/blob/main/docs/adr/0007-frostyard-sysext-filename-pattern.md) — docs/patterns.md's "Frostyard Pattern"; dpkg-aware version comparison for `+`/`:`/`~`
- [ADR-0008 — Sysext distribution layout and update contract](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md) — /var/lib/extensions.d staging, activation-symlink naming, Verify default-on
- [ADR-0009 — repository.frostyard.org is the single artifact origin](https://github.com/frostyard/core/blob/main/docs/adr/0009-single-artifact-origin-repository-frostyard-org.md) — catalog and transfer URLs resolve against it
- [ADR-0010 — Publish packages through the shared repogen action](https://github.com/frostyard/core/blob/main/docs/adr/0010-publish-packages-via-repogen-to-r2.md) — release.yml publish step
- [ADR-0011 — Distro packages are named frostyard-<tool>](https://github.com/frostyard/core/blob/main/docs/adr/0011-frostyard-prefixed-package-names.md) — frostyard-updex; the permanent instex compat symlink
- [ADR-0012 — svu-derived versions, make bump, and the rolling dev prerelease](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md) — .svu.yaml, dev tag, incmajor template, goreleaser-nightly concurrency
- [ADR-0013 — Component releases trigger image rebuilds via repository_dispatch](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md) — release.yml dispatches `build` to frostyard/snosi
- [ADR-0014 — One GPG repository key, baked into images](https://github.com/frostyard/core/blob/main/docs/adr/0014-single-gpg-trust-root.md) — manifest/gpg.go keyring search order
- [ADR-0015 — os-release is the image identity surface](https://github.com/frostyard/core/blob/main/docs/adr/0015-os-release-image-identity.md) — config/transfer.go's VARIANT_ID → IMAGE_ID → ID ladder
- [ADR-0018 — Org-wide agent instruction and knowledge surfaces](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md) — AI docs now live in docs/ per [ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) (formerly yeti/), plus .knowledge/ and .memory/ (note: this repo's symlink direction was drift; the fix closes frostyard/core#1)
- [ADR-0019 — Repository governance as machine-readable policy with risk tiers](https://github.com/frostyard/core/blob/main/docs/adr/0019-governance-as-code-and-risk-tiers.md) — .github/policies/ai-governance.json, risk tiers
- [ADR-0020 — Trust boundaries for AI automation in CI](https://github.com/frostyard/core/blob/main/docs/adr/0020-ai-automation-trust-boundaries.md) — ai-fix workflows, pull_request_target safety rules
- [ADR-0021 — SHA-pinned actions and least-privilege CI workflows](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md) — pinned actions, permissions: {}
- [ADR-0026 — Distribute core skills via sync PRs](https://github.com/frostyard/core/blob/main/docs/adr/0026-distribute-core-skills-via-sync-prs.md) — `.agents/skills/frostyard-{go-repo,repo-docs,acmm-conformance}` are core-managed (each carries a `.synced-from-core` marker) and are edited only in frostyard/core; core's sync PRs overwrite local edits, so repo-specific guidance goes in AGENTS.md or docs/
- [ADR-0033 — Permit link-only maintenance in immutable ADRs](https://github.com/frostyard/core/blob/main/docs/adr/0033-link-maintenance-in-immutable-adrs.md) — Accepted/Superseded ADRs in docs/adr/ may receive link-only repairs (moved target, or a commit permalink labeled historical) but no change to the decision text; `scripts/check-docs.mjs` checks relative links in Superseded ADRs under the same 1.0 link-integrity requirement as every other doc (no exemption)
- [ADR-0034 — Cancel stale rolling dev releases](https://github.com/frostyard/core/blob/main/docs/adr/0034-cancel-stale-rolling-dev-releases.md) — snapshot.yml's `goreleaser-nightly` concurrency group with `cancel-in-progress: true`, `workflow_run` on Tests/main, and the success guard; pinned by `updex/snapshot_workflow_contract_test.go`
- [ADR-0038 — make ci stays canonical; the test-name filter is chairlift-only](https://github.com/frostyard/core/blob/main/docs/adr/0038-scope-the-test-name-filter-to-chairlift.md) — `make ci` mirrors updex's credential-free CI jobs while all hermetic tests run without name filtering
- [ADR-0042 — A merge queue on the default branch](https://github.com/frostyard/core/blob/main/docs/adr/0042-adopt-a-merge-queue-on-the-default-branch.md) — every workflow producing a required check also triggers on `merge_group`; `pr-title.yml` skips its lint step there rather than excluding the job
- [ADR-0043 — Pin repository tools in mise and name the verify gate](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md) — `mise.toml`/`mise.lock` pin and verify `golangci-lint`; the Makefile derives `GOLANGCI_LINT_VERSION` from `mise.toml` for `make lint-version-check`
- [ADR-0044 — Expose the make gate triad in every repository](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md) — the Makefile's `verify`, `check`, and `ci` targets

When changing behavior covered by one of these, update or supersede the ADR
in frostyard/core first, then change this repo in the same effort.
