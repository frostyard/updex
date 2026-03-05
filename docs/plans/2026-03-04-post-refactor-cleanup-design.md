# Post-Refactor Cleanup Design

**Date:** 2026-03-04

## Problem

Several refactors have left behind stale documentation, unused directories, and incorrect build targets. The main offenders are `.planning/` (55 files from the completed v1 milestone), `.github/copilot-instructions.md` (references 8+ deleted SDK files and the old `instex` binary), and a broken Makefile `clean` target.

## Changes

### Deletions

| What | Files | Reason |
|------|-------|--------|
| `.planning/` | 55 tracked files | Completed v1 milestone, preserved in git history |
| `docs/plans/` | 4 design docs + this file | Completed refactor designs |
| `bin/` | empty dir | Unused |
| `build/instex` | 1 local artifact | Old binary name, gitignored |

### Fixes

- **Makefile `clean` target**: change `rm -f updex` to `rm -rf build/`

### Rewrites

- **`.github/copilot-instructions.md`**: update to reflect current architecture
  - Remove references to deleted SDK files (check.go, discover.go, install.go, list.go, pending.go, remove.go, update.go, vacuum.go, sysext.go)
  - Remove references to deleted CLI commands
  - Remove `instex` references
  - Update project structure tree to match actual files
  - Update SDK usage examples to use Features API
  - Verify dependency table accuracy

### Out of scope

- TODO in `gpg.go` about openpgp migration (legitimate future work)
- `.goreleaser.yaml` instex symlink (backward compat)
