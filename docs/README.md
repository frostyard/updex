# Documentation

Docs are split by the question they answer (the shape comes from
[frostyard/core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md)):

| Directory | Question | Contents |
| --- | --- | --- |
| [adr/](adr/) | **Why** did we choose this? | Architecture Decision Records — immutable once accepted; superseded, never edited |
| [design/](design/) | **How** does it fit together? | Living documents describing the current architecture |
| [specs/](specs/) | **What exactly** is the contract? | Precise, testable interface definitions |
| [plans/](plans/) | **When/in what order** do we build? | Roadmaps and phase plans; updated as work lands |

## Index

### Decisions (ADRs)

*(none yet — repo-local decisions get an ADR here; org-wide decisions live in
frostyard/core, see [org-adrs.md](org-adrs.md))*

### Design

- [updex design overview](design/overview.md) — purpose, architecture,
  key patterns, configuration, and data flow (formerly `yeti/OVERVIEW.md`)

### Specs

- [SDK API reference](specs/sdk-api.md) — public Go API surface (formerly
  `yeti/sdk-api.md`)
- [Configuration reference](specs/config-reference.md) — `.transfer` /
  `.feature` / `.catalog` file formats (formerly `yeti/config-reference.md`)

### Plans

*(none yet)*

### Process docs (uncategorized)

Pre-existing repo process docs, kept at their original paths:

- [org-adrs.md](org-adrs.md) — the frostyard/core ADRs that bind this repo
- [patterns.md](patterns.md) — transfer file pattern guide
- [risk-tiers.md](risk-tiers.md) — change risk tiers for pull requests
- [review-rubric.md](review-rubric.md) — pull request review rubric
- [AI-QUALITY-ASSURANCE.md](AI-QUALITY-ASSURANCE.md) — AI quality assurance
- [metrics/](metrics/README.md) — public metrics index
- [security/SECURITY-AI.md](security/SECURITY-AI.md) — AI security policy

## Conventions

- **New docs start from their category's `TEMPLATE.md`** (in each directory).
- New decision → new ADR with the next number; if it reverses an old one, mark
  the old one `Superseded by NNNN` rather than editing it.
- Design docs are updated in place to always reflect reality.
- Specs change only alongside the code that implements them.
- Adding a doc means adding it to the index above.
