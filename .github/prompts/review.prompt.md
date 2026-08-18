# Review a pull request

Review the given frostyard/updex PR against the repo rubric. You are
reviewing, not merging: never approve-and-merge in one act, and never merge
a PR you authored (mechanically backed by `.claude/settings.json`,
[ADR-0012](../../docs/adr/0012-acmm-conformance-via-canonical-aliases.md)).
Automated feedback is advisory: do not approve changes, weaken required
checks, or claim verification passed without evidence from the pull request.
Apply the machine-readable controls in
[policies/ai-governance.json](../policies/ai-governance.json).

1. Read [AGENTS.md](../../AGENTS.md) — the architecture rules, code
   conventions, and documentation rules the diff must satisfy. In
   particular:
   - **SDK-first design** — all business logic lives in `updex/`; `cmd/`
     handlers only parse flags, call SDK methods, and format output. SDK
     packages (`updex/`, `config/`, `catalog/`, `download/`, `manifest/`,
     `version/`, `sysext/`, `systemd/`) never import Cobra, pflag, or any
     CLI package.
   - **Context-first, structured returns** — SDK methods take
     `context.Context` first plus an options struct and return typed result
     structs plus `error`, never formatted strings.
   - **Error style** — lowercase, no trailing punctuation, wrapped with
     `fmt.Errorf("context: %w", err)`; no credentials or credential-bearing
     URLs in messages.
   - **Go 1.26 idioms** — `any`, `slices`/`maps`/`cmp`, `t.Context()`,
     `strings.SplitSeq`, `wg.Go()`, `omitzero`.
   - **Security** — SHA256 and GPG verification never bypassed; names and
     paths validated; managed paths checked with `os.Lstat`; multi-file
     writes snapshot and roll back
     ([ADR-0005](../../docs/adr/0005-transactional-writes-lstat-checks.md)).
2. Apply every row of the
   [PR review rubric](../../docs/specs/pr-review-rubric.md)
   (`docs/review-rubric.md` resolves to the same file). Check each row
   independently; cite file and line for every failure. Confirm the author
   selected the highest applicable
   [risk tier](../../docs/risk-tiers.md) and that the tier's required
   evidence is present.
3. Run the gates the rubric names:
   - `make ci` (tidy, vet, gofmt, lint, unit + race tests, linux
     amd64/arm64 builds)
   - `go test -v ./cmd/updex ./tests/e2e/...` when CLI or e2e behavior
     changed
   - `node scripts/check-docs.mjs`
4. If the diff changes SDK surface, configuration formats, or catalog
   behavior, verify [docs/specs/sdk-api.md](../../docs/specs/sdk-api.md),
   [docs/specs/config-reference.md](../../docs/specs/config-reference.md),
   [docs/design/overview.md](../../docs/design/overview.md), and
   `README.md` changed alongside the code, and that the repo-local ADRs
   still hold.
5. Report findings as review comments ordered by severity, labelled
   blocking / non-blocking / question / nit per the rubric; state plainly
   when a row passes. A PR with any failing rubric row gets "request
   changes", not silence.
