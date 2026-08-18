# 0012 — ACMM conformance via canonical aliases

- **Status:** Accepted
- **Date:** 2026-08-18

## Context

The Hive ACMM evaluation
([updex#139–#239](https://github.com/frostyard/updex/issues/139), label
`acmm`) grades repositories by checking that fixed paths exist — test suites,
templates, style configs, rubrics, metrics, agent-safety settings. Each
criterion lists acceptable paths and states "the content can follow your
project's conventions." updex closed every issue in that range in July 2026
with real files at ACMM's paths: a `CONTRIBUTING.md` and a
`.github/copilot-instructions.md` that had drifted apart from `AGENTS.md`
(the copilot file still recommended `--verify=false`, which #245 established
as wrong), `docs/review-rubric.md`, `docs/AI-QUALITY-ASSURANCE.md`, and a
`docs/metrics/README.md` carrying the acceptance-metric definition — none of
them in the four-category `docs/` tree that
[frostyard/core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md)
prescribes, and with no gate guarding index coverage, links, or aliases.

frostyard/core then solved the identical issue set with
[core ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md):
committed relative symlinks to canonical content wherever a canonical
equivalent exists, genuinely new artifacts only where none does, all guarded
by a docs-integrity gate. repogen followed with
[repogen ADR-0012](https://github.com/frostyard/repogen/blob/main/docs/adr/0012-acmm-conformance-via-canonical-aliases.md).
Duplicating content into ACMM's paths — what updex had — guarantees drift,
exactly what core ADR-0002 rejected.

## Decision

The divergent instruction files merge into one canonical **`AGENTS.md`**
(core ADR-0002/0018 pattern, both already binding via
[org-adrs.md](../org-adrs.md)). ACMM's required paths are satisfied by
**committed relative symlinks to canonical content** wherever a canonical
equivalent exists, and by genuinely new artifacts only where none does.
Canonical content lives where org conventions put it — the four-category
`docs/` tree and `AGENTS.md` — never at the ACMM path.

The alias table (edit the targets, never the aliases):

| Alias | Target | Criterion |
| --- | --- | --- |
| `CLAUDE.md` | `AGENTS.md` | (agent surface, core ADR-0002) |
| `GEMINI.md` | `AGENTS.md` | (agent surface, core ADR-0002) |
| `CONTRIBUTING.md` | `AGENTS.md` | contributing guide (#141) |
| `.cursorrules` | `AGENTS.md` | cursor rules (#149) |
| `.github/copilot-instructions.md` | `../AGENTS.md` | (agent surface, core ADR-0002) |
| `.claude/skills` | `../.agents/skills` | simple skills (#152) |
| `docs/metrics.md` | `specs/pr-acceptance-metric.md` | PR acceptance metric (#162) |
| `docs/review-rubric.md` | `specs/pr-review-rubric.md` | PR review rubric (#163/#172) |
| `docs/quality.md` | `design/quality-loop.md` | quality dashboard (#164/#171) |

Rules:

- **Directory criteria always get real git trees** (`tests/e2e/`,
  `.github/ISSUE_TEMPLATE/`, `.github/prompts/`, `.github/policies/`,
  `.memory/`, `docs/metrics/`) — an evaluator reading the git tree via API
  sees a symlink as a blob, not a tree. `docs/metrics/` is the one real tree
  under `docs/` outside the four categories: the public-metrics criterion
  (#238) names the directory, while the acceptance-metric criterion (#162)
  names `docs/metrics.md`; the directory's `README.md` is the public
  metrics index (signal table, privacy contract) and links to the spec for
  the definition instead of restating it.
- **Aliases are not docs**: they get no `docs/README.md` index entries and
  carry no cross-link obligations; the canonical target does.
- **Higher-level criteria already satisfied by real, fleet-conventional
  files stay put**: `docs/risk-tiers.md` (#192),
  `docs/security/SECURITY-AI.md` (#193), `.github/policies/ai-governance.json`
  (#239), `.github/auto-qa-tuning.json` (#187), `.github/labeler.yml`
  (#190), and the `nightly-compliance`, `ai-fix-requested`, and
  `claude-code-review` workflows (#188, #191, #237) keep the paths the
  other fleet repos at those levels use.
- Genuinely new artifacts, each doing real work: the merged `AGENTS.md`;
  `tests/e2e/README.md` naming the existing black-box suite as the e2e entry
  point (#160); the reshaped `.github/pull_request_template.md` (#139)
  mirroring `make ci`, risk tiers, docs housekeeping, and the aliases rule;
  `.coverage-thresholds.json` enforced by `scripts/check-docs.mjs` in the
  new `docs-gate` CI job — docs-index coverage, link integrity, symlink
  resolution (#147, alongside the existing `codecov.yml`);
  `.github/prompts/README.md` and `review.prompt.md` (#150); the `.memory/`
  inbox with core ADR-0018's append-only five-field schema (#153);
  [specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md) (#162),
  [specs/pr-review-rubric.md](../specs/pr-review-rubric.md) (#163), and
  [design/quality-loop.md](../design/quality-loop.md) (#164) — each carrying
  the substantive content of the file it replaces; `.claude/settings.json`
  denying merge-own-PR, approve-own-work, release-publishing, and pushes to
  `main` at the tool layer; and the fleet-shaped
  `.claude/session-summary.md` (#165).

## Consequences

- One canonical body of content per criterion; conformance paths cannot
  drift from it. `docs/AI-QUALITY-ASSURANCE.md` is gone (its content lives in
  `design/quality-loop.md`, reachable via `docs/quality.md`); the old
  `CONTRIBUTING.md` sections keep their headings inside `AGENTS.md`
  (`## Testing`, `## Security`, `## Pull requests`) so existing anchors
  resolve through the alias.
- GitHub's web renderer shows a symlinked `.md` as its target path rather
  than its content; checkouts on Windows need `core.symlinks=true` or WSL.
- The alias table above is the registry; adding or removing an alias means
  amending it here (a new ADR if the mechanism itself changes).
- `scripts/check-docs.mjs` fails CI on any broken alias, unindexed doc, or
  dead relative link, making the lattice self-guarding;
  `updex/public_metrics_contract_test.go` pins the metrics tree, the spec's
  formula, and the Auto-QA signal path.
- Contingency: if the ACMM evaluator rejects a symlink for one of the file
  criteria (#141, #149, #162, #163, #164), that alias is replaced by a real
  stub file pointing at the canonical doc — a one-commit change that does
  not reverse this decision.

## Alternatives considered

- **Keep the real duplicate files at the ACMM paths:** the state before
  this ADR; guaranteed drift, and already drifted between `CLAUDE.md`,
  `CONTRIBUTING.md`, and `.github/copilot-instructions.md`.
- **Content-free stub files:** a second class of "doc" that the index and
  cross-link rules would nominally govern; symlinks are aliases, not docs.
- **Move `docs/risk-tiers.md` and `docs/security/SECURITY-AI.md` into
  `docs/specs/` behind aliases:** consistent with ADR-0025 in isolation, but
  every fleet repo at ACMM L4 keeps them at these paths and core has no
  precedent to follow yet; kept put to stay fleet-consistent (a later org
  decision may move them everywhere at once).

## References

- Shapes: [design/quality-loop.md](../design/quality-loop.md),
  [specs/pr-review-rubric.md](../specs/pr-review-rubric.md),
  [specs/pr-acceptance-metric.md](../specs/pr-acceptance-metric.md)
- Pattern source:
  [core ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md)
  and
  [repogen ADR-0012](https://github.com/frostyard/repogen/blob/main/docs/adr/0012-acmm-conformance-via-canonical-aliases.md),
  building on core ADR-0002/0018/0019/0025 (see [org-adrs.md](../org-adrs.md))
