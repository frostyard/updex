# Post-Refactor Cleanup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove stale files, fix broken build targets, and update documentation left behind by prior refactors.

**Architecture:** Pure cleanup — deletions, one Makefile line fix, and a copilot-instructions rewrite to match the current features-based API.

**Tech Stack:** Git, Make, Markdown

---

### Task 1: Delete .planning/ directory

**Files:**
- Delete: `.planning/` (55 tracked files — milestones, phases, research, codebase docs)

**Step 1: Remove the directory from git tracking**

```bash
git rm -r .planning/
```

**Step 2: Commit**

```bash
git commit -m "chore: remove completed .planning directory

v1 milestone shipped 2026-01-26. Planning docs preserved in git history."
```

---

### Task 2: Delete docs/plans/

**Files:**
- Delete: `docs/plans/2026-03-04-reporter-migration-design.md`
- Delete: `docs/plans/2026-03-04-reporter-migration.md`
- Delete: `docs/plans/2026-03-04-clix-integration-design.md`
- Delete: `docs/plans/2026-03-04-clix-integration.md`
- Delete: `docs/plans/2026-03-04-post-refactor-cleanup-design.md`
- Delete: `docs/plans/2026-03-04-post-refactor-cleanup.md` (this file)

**Step 1: Remove from git**

```bash
git rm -r docs/plans/
```

If `docs/` is now empty, remove it too:

```bash
rmdir docs/ 2>/dev/null || true
```

**Step 2: Commit**

```bash
git commit -m "chore: remove completed design docs

Refactor designs preserved in git history."
```

---

### Task 3: Fix Makefile clean target

**Files:**
- Modify: `Makefile:25-27`

**Step 1: Fix the clean target**

Change line 26 from:

```makefile
	rm -f updex
```

To:

```makefile
	rm -rf build/
```

This matches the build target which outputs to `build/updex`.

**Step 2: Verify**

```bash
make build && ls build/updex && make clean && ls build/updex 2>&1 | grep -q "No such file" && echo "PASS"
```

Expected: `PASS`

**Step 3: Commit**

```bash
git add Makefile
git commit -m "fix: make clean target removes build/ directory

The binary is built to build/updex but clean was removing ./updex."
```

---

### Task 4: Rewrite copilot-instructions.md

**Files:**
- Modify: `.github/copilot-instructions.md`

**Step 1: Rewrite the file**

Replace the entire file with content reflecting the current architecture:

- **Project structure**: Only list files that actually exist:
  - `updex/`: `updex.go`, `options.go`, `results.go`, `features.go`, `install.go`, `list.go`, `features_test.go`, `test_helpers_test.go`
  - `cmd/commands/`: `components.go`, `features.go`, `daemon.go`, `completion_test.go`
  - `cmd/common/`: `common.go`, `common_test.go`
  - `cmd/updex/`: `root.go`
  - `cmd/updex-cli/`: `main.go`
  - `internal/`: config, download, manifest, sysext, systemd, testutil, version
- **SDK API**: `Client` struct with `Features()`, `EnableFeature()`, `DisableFeature()`, `UpdateFeatures()`, `CheckFeatures()`
- **CLI commands**: `features` (list/enable/disable/update/check) and `daemon` (enable/disable/status)
- **Remove all instex references**
- **Dependencies**: Update table — replace `schollz/progressbar` description, add `frostyard/clix` and `frostyard/std`
- **Usage example**: Use `client.UpdateFeatures()` instead of deleted `updex.CheckNew()`/`updex.Update()`

**Step 2: Verify no stale references remain**

```bash
grep -i instex .github/copilot-instructions.md && echo "FAIL: instex still referenced" || echo "PASS"
grep "check\.go\|discover\.go\|pending\.go\|remove\.go\|vacuum\.go\|sysext\.go" .github/copilot-instructions.md && echo "FAIL: deleted files referenced" || echo "PASS"
```

Expected: `PASS` for both.

**Step 3: Commit**

```bash
git add .github/copilot-instructions.md
git commit -m "docs: update copilot-instructions for current architecture

Remove references to deleted SDK files, CLI commands, and instex binary.
Update project structure, API examples, and dependency table."
```

