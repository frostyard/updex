# Pitfalls Research

**Domain:** systemd-sysext management CLI with auto-update
**Researched:** 2026-01-26
**Confidence:** MEDIUM (based on codebase analysis, Flatcar sysext-bakery patterns, and domain knowledge from training)

## Critical Pitfalls

### Pitfall 1: Removing Active/Merged Extensions Without Unmerge

**What goes wrong:**
Deleting sysext files while extensions are merged (active) can cause:
- Processes crash when dynamically loading libraries from the extension
- System instability if extension provides critical services
- Orphaned mounts in `/run/extensions` that confuse systemd-sysext

**Why it happens:**
- Developer assumes file deletion is atomic and safe
- Conflates "installed" (files exist) with "active" (merged into system)
- No check for current merge state before removal

**How to avoid:**
1. Always check merge state via `/run/extensions` before removal
2. Require explicit `--now` flag to unmerge before deletion
3. Warn users when deleting files for a merged extension without `--now`
4. Consider refusing deletion of active extensions without explicit confirmation

**Warning signs:**
- Remove command doesn't check `/run/extensions`
- No distinction between "installed" and "active" in CLI output
- Tests don't cover removal of active extensions

**Phase to address:**
Phase 1 (Core UX improvements) — This is a safety-critical operation that must be correct before auto-update.

---

### Pitfall 2: Auto-Update During Active Merge State

**What goes wrong:**
Automatic updates that replace extension files while extensions are merged can:
- Replace files that are currently mapped into the filesystem
- Cause undefined behavior when processes access replaced files
- Leave system in inconsistent state between merged content and disk content

**Why it happens:**
- Auto-update timer fires while system is running normally (always merged)
- No coordination between update process and merge lifecycle
- Treating updates like simple file downloads without understanding overlay semantics

**How to avoid:**
1. Auto-update should only download new versions, not activate them
2. Activation requires explicit reboot or `systemctl restart systemd-sysext`
3. Use staging directory pattern: download to `/var/lib/extensions.d/`, symlink only after unmerge/merge cycle
4. Auto-update service should have `Conflicts=` with any extension-dependent services

**Warning signs:**
- Auto-update directly modifies `/var/lib/extensions`
- No staging directory separation
- Update operation doesn't check for merge state
- No documentation about when updates take effect

**Phase to address:**
Phase 2 (Auto-update implementation) — Must design this correctly from the start; retrofit is expensive.

---

### Pitfall 3: Symlink Races on Update

**What goes wrong:**
Updating the `CurrentSymlink` (e.g., `myext.raw -> myext_1.2.3.raw`) while systemd-sysext is reading:
- Race condition during merge/refresh operations
- Stale symlink target if update happens mid-merge
- Inconsistent state between what's merged and what symlink points to

**Why it happens:**
- Symlink update is not atomic with merge operation
- Assuming symlink update propagates instantly to merged filesystem
- No locking between updex and systemd-sysext operations

**How to avoid:**
1. Never update symlinks while merge is active
2. Update pattern: unmerge -> update symlink -> merge
3. Use lock file or systemd `Conflicts=` to prevent concurrent operations
4. Consider atomic rename pattern: create new symlink with temp name, then `mv`

**Warning signs:**
- `UpdateSymlink()` called without checking merge state
- No locking around symlink operations
- Tests pass because race window is small, not because code is correct

**Phase to address:**
Phase 1 (Core UX improvements) — Foundation for safe operations; auto-update builds on this.

---

### Pitfall 4: Feature Disable Without File Cleanup (Current Issue)

**What goes wrong:**
Disabling a feature only writes `Enabled=false` to drop-in but doesn't:
- Remove downloaded extension files
- Unlink from `/var/lib/extensions`
- Actually stop the feature's effects until explicit action

**Why it happens:**
- Matching systemd drop-in semantics (config-only changes)
- Assuming users will run `vacuum` or `remove` separately
- Not considering user mental model: "disable" should make it go away

**How to avoid:**
1. `features disable --remove` removes files (already implemented but not default)
2. Consider making `--remove` the default for `features disable`
3. Clear documentation about what "disable" means vs "remove"
4. CLI should confirm what will happen before proceeding

**Warning signs:**
- User runs `features disable devel`, expects devel extensions gone
- Extensions still merged after disable
- Disk space not freed after disable

**Phase to address:**
Phase 1 (Core UX improvements) — This is the specific issue noted in project context.

---

### Pitfall 5: Breaking Running System with Auto-Update

**What goes wrong:**
Auto-update installs new version that is incompatible with:
- Currently running kernel (ABI mismatch)
- Other installed extensions (version conflicts)
- System services that depend on extension (service disruption)

**Why it happens:**
- No validation of compatibility before update
- Blind "always update to latest" strategy
- No rollback mechanism when update causes problems

**How to avoid:**
1. Keep previous version (InstancesMax >= 2) for rollback
2. Don't auto-merge new versions — require reboot or explicit command
3. Implement version pinning for production systems
4. Log what was updated, when, and from what version
5. Consider "canary" mode: update staging, wait for admin to approve merge

**Warning signs:**
- InstancesMax = 1 (no rollback possible)
- Auto-update immediately activates new versions
- No audit log of updates
- No version constraint configuration

**Phase to address:**
Phase 2 (Auto-update implementation) — Core safety requirement for auto-update.

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Calling `Unmerge()` without service coordination | Simple implementation | Service disruption, data loss | Testing only |
| Hardcoding `/var/lib/extensions` | Works for standard installs | Breaks custom setups, containers | Never in library code |
| Global `Refresh()` after every operation | Ensures consistency | Performance, service restarts | Acceptable for CLI, not for library |
| Skipping GPG verification by default | Faster updates | Security risk | Development only |
| Not tracking which files belong to which extension | Simpler file management | Can't reliably remove single extension | Never |

## Integration Gotchas

Common mistakes when connecting to external services.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| systemd-sysext | Assuming `refresh` is instantaneous | Check journal for completion, handle errors |
| HTTP downloads | No timeout, no retry | Configure timeout, exponential backoff with max retries |
| GPG verification | Trusting any valid signature | Verify against known keyring, pin expected key IDs |
| SHA256SUMS | Assuming file format is consistent | Handle both `hash  filename` and `hash filename` formats |
| systemd timers | Assuming timer fires at exact time | Handle clock skew, missed timers, catchup behavior |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Re-downloading SHA256SUMS on every operation | Slow list/check commands | Cache with TTL (e.g., 5 minutes) | Multiple extensions, frequent checks |
| Reading all transfer files on every command | Slow startup | Lazy loading, cache parsed configs | 50+ transfer files |
| Full directory scan for installed versions | Slow on large directories | Index file or database | 100+ versions per extension |
| Synchronous GPG verification | Blocks CLI for seconds | Async with progress, or cache verification result | Slow/remote keyserver |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Downloading extension without hash verification | Malicious code execution as root | Always verify SHA256 before unpacking/installing |
| GPG verification using system keyring | Trusting any key user has imported | Maintain updex-specific keyring in `/etc/updex.d/trusted.gpg.d/` |
| Running as root when not needed | Privilege escalation surface | Separate check/list (unprivileged) from install/remove (root) |
| Storing download URLs in world-readable config | Exposes internal network structure | Config files should be root-readable only if they contain sensitive URLs |
| Not validating extension image format | Path traversal, symlink attacks | Verify extension-release.d contents, reject suspicious paths |

## UX Pitfalls

Common user experience mistakes in this domain.

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Silently succeeding when nothing changed | User doesn't know if command worked | Always report what happened: "Already up to date" vs "Updated from X to Y" |
| Requiring reboot without saying so | User wonders why changes aren't visible | Clear message: "Reboot required for changes to take effect" |
| `--now` flag that sometimes fails silently | User thinks unmerge happened but it didn't | Return error and exit code when `--now` fails |
| Different behavior for `features disable` vs `remove` | Confusion about which to use | Document clearly, consider unifying semantics |
| No dry-run mode for destructive operations | Users afraid to run commands | Implement `--dry-run` for update, remove, vacuum |
| Unclear distinction between "installed" and "active" | Users don't understand system state | Show both states in `list` output |
| Auto-update that requires manual intervention | Defeats purpose of "auto" | If fully automated, document exactly what happens; if needs manual step, don't call it "auto" |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **Remove command:** Does it check if extension is currently merged?
- [ ] **Update command:** Does it handle partial downloads (interrupted transfer)?
- [ ] **Vacuum command:** Does it protect the currently active version, not just newest?
- [ ] **Auto-update timer:** Is there a health check after update? Notification on failure?
- [ ] **Feature disable:** Are files actually removed, or just config changed?
- [ ] **GPG verification:** Is the keyring properly bootstrapped/maintained?
- [ ] **Progress reporting:** Does it work correctly for parallel downloads?
- [ ] **JSON output:** Is it consistent across all commands? Does it include error details?
- [ ] **Exit codes:** Are they consistent? Does `check-new` return different code for "no update"?
- [ ] **Root checks:** Are they at the right place (before or after config loading)?

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Extension files deleted while merged | LOW | Reboot — merge will fail gracefully, system falls back to base |
| Symlink points to deleted file | LOW | `systemd-sysext refresh` — will unmerge broken extension |
| Auto-update installed incompatible version | MEDIUM | Keep previous version, update symlink to old version, refresh |
| All versions deleted (vacuum too aggressive) | HIGH | Re-install extension from registry, may need reboot |
| Corrupted extension image | MEDIUM | Delete corrupted file, re-download with `update --force` |
| GPG keyring corrupted/missing | MEDIUM | Re-bootstrap keyring from trusted source |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Removing active extensions unsafely | Phase 1: Core UX | Test: remove command warns/errors when extension is merged |
| Auto-update during merge | Phase 2: Auto-update | Test: auto-update only stages, doesn't activate |
| Symlink race conditions | Phase 1: Core UX | Test: concurrent operations are serialized or rejected |
| Feature disable without cleanup | Phase 1: Core UX | Test: `features disable --remove` actually removes files |
| Breaking running system | Phase 2: Auto-update | Test: InstancesMax >= 2 enforced, rollback works |
| Missing dry-run mode | Phase 1: Core UX | Manual: verify `--dry-run` flag exists on destructive commands |
| Unclear installed vs active state | Phase 1: Core UX | Test: `list` output shows both installed and active columns |

## Testing Pitfalls Specific to This Domain

### Testing CLI Tools Requiring Root

**What goes wrong:**
- Tests skip root-required functionality
- Tests mock away the actual privileged operations
- CI can't run tests that need real root access

**Prevention strategies:**
1. Separate privileged operations into minimal functions that can be tested manually
2. Use test fixtures with `t.TempDir()` for file operations (works without root)
3. Use Linux namespaces (`unshare`) for pseudo-root testing where possible
4. Tag root-requiring tests with `//go:build integration` and run in privileged CI container
5. Mock `os/exec` calls to `systemd-sysext` rather than mocking the whole function

### Testing Systemd Integration

**What goes wrong:**
- Mocking `systemd-sysext` behavior inaccurately
- Tests pass but real `systemd-sysext refresh` behaves differently
- Ignoring journal entries/errors from systemd

**Prevention strategies:**
1. Integration test suite that runs in VM with real systemd (see Flatcar sysext-bakery patterns)
2. Record/replay pattern for systemd-sysext outputs
3. Parse journal for actual error conditions, not just exit codes
4. Test against multiple systemd versions if supporting wide compatibility

## Sources

- **Codebase analysis:** `internal/sysext/manager.go`, `updex/features.go`, `updex/remove.go`
- **Flatcar sysext-bakery:** GitHub repository patterns for sysext management (MEDIUM confidence)
- **systemd-sysext concepts:** Based on training data understanding of overlay fs semantics (LOW-MEDIUM confidence — should verify against current systemd docs)
- **CLI UX patterns:** General domain knowledge for system administration tools

---
*Pitfalls research for: systemd-sysext management CLI*
*Researched: 2026-01-26*
