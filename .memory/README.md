# .memory/ — corrections inbox

The single sanctioned inbox for durable learned corrections (pattern from
[frostyard/core ADR-0018](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md);
adopted here by
[ADR-0012](../docs/adr/0012-acmm-conformance-via-canonical-aliases.md)).

Contract:

- Each correction is 1 file in `.memory/corrections/`, in the form that
  [frostyard/core ADR-0081](https://github.com/frostyard/core/blob/main/docs/adr/0081-store-each-correction-in-a-file-of-its-own.md)
  sets. 2 pull requests that each add a record add 2 different files, so they
  do not conflict.
- **Name:** `<date>-<slug>.json`; the date part equals the record's `date`,
  and the slug has at most 60 characters. A record from a pull request uses
  the slug `pr-<N>-<words>`, for example `2026-11-13-pr-412-cache-key.json`.
  A record with no pull request uses 2 to 5 words and 4 random hexadecimal
  characters, for example `2026-11-13-agent-scope-rule-3f9a.json`. The slug
  prefix `legacy-` is reserved: the `<date>-legacy-<NNNN>.json` files hold
  line `NNNN` of the old `.memory/corrections.jsonl`, which is retired here.
- **Content:** 1 JSON object with exactly 5 string fields, in this order, and
  in the canonical form `JSON.stringify(record, null, 2) + "\n"`:

  ```json
  {
    "date": "YYYY-MM-DD",
    "scope": "…",
    "correction": "…",
    "evidence": "…",
    "promoted_to": ""
  }
  ```

  `date` is a real date; `scope`, `correction` and `evidence` are not empty.
- **Permanent:** a record file is never removed or renamed. Its text never
  changes, except that an empty `promoted_to` is filled once.
- frostyard/core's `scripts/new-correction.mjs` writes a record file in the
  canonical form, and its `scripts/check-corrections.mjs --root .` checks the
  folder; a file written by hand is fine when it passes.

- `promoted_to` starts empty; when a correction graduates into
  [AGENTS.md](../AGENTS.md), a doc under [docs/](../docs/README.md), or a
  skill, set it to that path. Promotion is the only sanctioned duplication —
  the `frostyard-repo-docs` maintenance pass drains this inbox.
- A blocking review point adds 1 record file in the pull request that fixes
  it, next to the guard that catches it.
- Session continuity (what a later session needs to resume unfinished work)
  goes in [.claude/session-summary.md](../.claude/session-summary.md), not
  here.
- Never record credentials or non-public vulnerability details.
