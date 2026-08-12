# Issue Triage Prompt

Use this prompt when triaging a new GitHub issue or bug report for updex.

## Instructions

1. Read `docs/design/overview.md` and any relevant notes under `.memory/` for
   architecture and prior gotchas before proposing a fix.
2. Classify the issue:
   - **SDK bug** — logic error in `updex/`, `config/`, `catalog/`,
     `download/`, `manifest/`, `version/`, `sysext/`, or `systemd/`.
   - **CLI bug** — flag parsing or output formatting in `cmd/updex/`.
   - **Docs gap** — missing or stale guidance in `AGENTS.md`, `README.md`, or
     `docs/`.
   - **ACMM/process gap** — repository hygiene or agent-maturity criterion
     (see `.github/copilot-instructions.md`).
3. For a reproducible bug, write a minimal failing test first (table-driven,
   using `t.TempDir()` and mock `Runner`s where systemd/sysext commands are
   involved), confirm it fails, then implement the smallest fix that makes it
   pass.
4. Keep the SDK-first boundary intact: fixes to business logic belong in the
   SDK package, not in `cmd/`.
5. Run `make fmt && make test` (and `make lint` if touching exported APIs)
   before proposing the change.
6. If the fix reveals a durable architectural fact or pitfall, fold it into
   the right `docs/` page (or drop a note in `.memory/`) so future agents
   avoid repeating the mistake.
