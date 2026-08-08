# Cross-session knowledge

This directory is the discovery entry point for tools that look for a
`.knowledge/` store. Updex keeps cross-session context in the existing stores
that match each artifact's lifetime:

- [`.claude/session-summary.md`](../.claude/session-summary.md) contains concise,
  temporary handoffs for unfinished work.
- [`.memory/`](../.memory/) is reserved for correction and memory artifacts.
- [`yeti/learnings/`](../yeti/learnings/) contains durable architecture
  decisions and non-obvious lessons that future contributors should not have
  to rediscover.

Record repository-specific facts with the evidence that established them. Move
reusable conclusions into `yeti/learnings/` instead of growing an unbounded
session log. Never commit credentials, tokens, private user data, or non-public
vulnerability details to any learning artifact.
