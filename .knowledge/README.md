# Cross-session knowledge

This directory is the discovery entry point for tools that look for a
`.knowledge/` store. Updex keeps cross-session context in the existing stores
that match each artifact's lifetime:

- [`.claude/session-summary.md`](../.claude/session-summary.md) contains concise,
  temporary handoffs for unfinished work.
- [`.memory/`](../.memory/) is the single learnings inbox: correction and
  memory artifacts, plus durable lessons awaiting a fold into `docs/`.
- [`docs/`](../docs/) (entry point [`docs/design/overview.md`](../docs/design/overview.md),
  formerly `yeti/`) holds durable architecture decisions and non-obvious
  lessons that future contributors should not have to rediscover.

Record repository-specific facts with the evidence that established them. Fold
reusable conclusions into the right `docs/` page (via the `.memory/` inbox if
needed) instead of growing an unbounded session log. Never commit credentials, tokens, private user data, or non-public
vulnerability details to any learning artifact.
