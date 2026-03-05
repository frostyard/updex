---
tracker:
  kind: github
  repo: frostyard/updex
  active_states:
    - "symphony:todo"
    - "symphony:in-progress"
  terminal_states:
    - "symphony:done"
    - "symphony:cancelled"

polling:
  interval_ms: 30000

workspace:
  root: /tmp/symphony_workspaces
  repo_url: git@github.com:frostyard/updex.git
  base_branch: main

hooks:
  before_run: |
    git pull origin main --rebase
  timeout_ms: 60000

agent:
  max_concurrent_agents: 2
  max_turns: 10
  max_retry_backoff_ms: 300000

claude:
  command: claude --print
  model: sonnet
  turn_timeout_ms: 3600000
  stall_timeout_ms: 300000
  permission_mode: bypassPermissions

server:
  port: 8080
---

You are working on issue {{ .Issue.Identifier }}: {{ .Issue.Title }}

{{ if .Issue.Description }}{{ deref .Issue.Description }}{{ end }}

When creating pull requests, always create them as drafts using `gh pr create --draft`.

{{ if gt .Attempt 0 }}
This is continuation attempt {{ .Attempt }}.
Check the current state and continue where the previous session left off.
{{ end }}
