# .memory/ — corrections inbox

The single sanctioned inbox for durable learned corrections (pattern from
[frostyard/core ADR-0018](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md);
adopted here by
[ADR-0012](../docs/adr/0012-acmm-conformance-via-canonical-aliases.md)).

Contract:

- `corrections.jsonl` is **append-only** — one JSON object per line, five
  fields, all required:

  ```json
  {"date": "YYYY-MM-DD", "scope": "…", "correction": "…", "evidence": "…", "promoted_to": ""}
  ```

- `promoted_to` starts empty; when a correction graduates into
  [AGENTS.md](../AGENTS.md), a doc under [docs/](../docs/README.md), or a
  skill, set it to that path. Promotion is the only sanctioned duplication —
  the `frostyard-repo-docs` maintenance pass drains this inbox.
- Session continuity (what a later session needs to resume unfinished work)
  goes in [.claude/session-summary.md](../.claude/session-summary.md), not
  here.
- Never record credentials or non-public vulnerability details.
