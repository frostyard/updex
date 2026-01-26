# Feature Research

**Domain:** systemd-sysext management CLI
**Researched:** 2026-01-26
**Confidence:** HIGH (official systemd documentation verified)

## Feature Landscape

### Table Stakes (Users Expect These)

Features users assume exist. Missing these = product feels incomplete.

| Feature | Why Expected | Complexity | Notes | updex Status |
|---------|--------------|------------|-------|--------------|
| `list` — Show versions | Core operation to see what's installed vs available | LOW | updatectl: `list [TARGET[@VERSION]]` | ✓ Implemented |
| `check` — Check for updates | Users need to know if updates exist | LOW | updatectl: `check [TARGET...]` | ✓ Implemented (as `check-new`) |
| `update` — Install updates | Primary purpose of the tool | MEDIUM | updatectl: `update [TARGET[@VERSION]...]` | ✓ Implemented |
| `vacuum` — Remove old versions | Disk space management per InstancesMax | LOW | updatectl: `vacuum [TARGET...]` | ✓ Implemented |
| `features list` — Show optional features | Feature-based grouping is core to sysupdate | LOW | updatectl: `features [FEATURE]` | ✓ Implemented |
| `features enable/disable` — Toggle features | Control which transfers are active | LOW | updatectl: `enable/disable FEATURE...` | ✓ Implemented |
| `components` — List update targets | Show what components can be updated | LOW | systemd-sysupdate: `components` | ✓ Implemented |
| `pending` — Check for pending updates | Detect installed but not-yet-active updates | LOW | systemd-sysupdate: `pending` | ✓ Implemented |
| JSON output | Scripting and automation support | LOW | Both tools support `--json` | ✓ Implemented |
| GPG signature verification | Security requirement for downloads | MEDIUM | `--verify` option | ✓ Implemented |
| Component filtering | Operate on specific component | LOW | `-C/--component=` option | ✓ Implemented |
| Version pattern matching | Core to identifying versions | HIGH | `@v` wildcard in patterns | ✓ Implemented |

### Differentiators (Competitive Advantage over updatectl)

Features that set updex apart. Not required, but valuable.

| Feature | Value Proposition | Complexity | Notes | updex Status |
|---------|-------------------|------------|-------|--------------|
| `discover` — Browse remote repos | Find extensions without prior knowledge | MEDIUM | updatectl has no discovery mechanism | ✓ Implemented |
| `install` — Install from URL | One-step install without manual .transfer setup | MEDIUM | updatectl requires pre-configured .transfer files | ✓ Implemented |
| `remove` — Remove extensions | Clean uninstall of extensions + config | MEDIUM | updatectl doesn't have explicit remove | ✓ Implemented |
| Debian availability | Works on Debian Trixie where updatectl isn't packaged | LOW | Core reason updex exists | ✓ Achieved |
| Multi-registry support | Aggregate extensions from multiple sources | MEDIUM | updatectl works per-target, no aggregation | ✓ Implemented |
| Progress bar | Better UX for downloads | LOW | updatectl doesn't show download progress | ✓ Implemented |
| Simpler mental model | "disable = remove files" vs "disable = stop updates" | LOW | updatectl disable just changes config | — Pending |

### updatectl Features NOT in updex (Gap Analysis)

| Feature | What It Does | Complexity | Priority | Notes |
|---------|--------------|------------|----------|-------|
| `--now` with enable/disable | Immediately download/cleanup when toggling features | MEDIUM | HIGH | updatectl: `enable --now` downloads immediately |
| `--reboot` | Reboot after update completes | LOW | MEDIUM | updatectl: `update --reboot` |
| `--offline` | Prevent network access, list local only | LOW | LOW | updatectl: `list --offline` |
| `reboot` command | Reboot if pending update exists | LOW | LOW | systemd-sysupdate: `reboot` |
| D-Bus API | systemd-sysupdated daemon interface | HIGH | LOW | Not needed for CLI-only tool |
| Partition updates | Update GPT partitions directly | HIGH | OUT OF SCOPE | updex focuses on url-file → regular-file |
| Disk image mode | Update offline disk images | HIGH | OUT OF SCOPE | systemd-sysupdate: `--image=` |

### Anti-Features (Commonly Requested, Often Problematic)

Features that seem good but create problems.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Full D-Bus daemon | "Be like updatectl" | Massive complexity, requires polkit, systemd service architecture | CLI-only is fine for this use case |
| Partition operations | "Complete parity with systemd-sysupdate" | Extremely complex, dangerous, requires privileged access | Focus on file-based transfers only |
| Auto-update by default | "Convenience" | Security concern, unexpected system changes | Opt-in timer/service installation |
| Global extension disable | "Stop extension from running" | Conflates update management with runtime management | Separate concerns: updex manages updates, systemd-sysext manages runtime |
| Rollback | "Undo updates" | Complex state management, not how sysext A/B works | Use `update VERSION` to pin specific version |

## Feature Dependencies

```
[list] ← foundation for all other operations
    └── used by → [check], [pending], [vacuum]

[install]
    └── requires → [discover] (for registry browsing)
    └── creates → .transfer files → enables [list], [update]

[update]
    └── requires → [list] (to find versions)
    └── calls → [vacuum] (after update, unless --no-vacuum)
    └── calls → systemd-sysext refresh (unless --no-refresh)

[features enable/disable]
    └── affects → which transfers are active in [list], [update]

[remove]
    └── reverse of → [install]
    └── calls → systemd-sysext refresh
```

### Dependency Notes

- **[install] creates .transfer files:** Install is the entry point for new extensions; creates config that all other commands use
- **[update] calls [vacuum]:** Automatic cleanup unless explicitly disabled
- **[features] gates [update]:** Disabled features aren't updated, enabled features are
- **All operations require [list] logic:** Version discovery is foundational

## MVP Definition

### Already Shipped (v1.0 equivalent)

updex already implements core functionality:

- [x] list, check, update, vacuum, pending — core update lifecycle
- [x] features list/enable/disable — feature management
- [x] components — component discovery
- [x] discover, install — unique differentiators
- [x] remove — clean uninstall
- [x] JSON output, GPG verification

### Add in This Milestone (v1.x)

- [ ] `--now` flag for feature enable/disable — immediately apply changes
- [ ] `--reboot` flag for update — convenience for automated updates
- [ ] Auto-update timer/service — `updex install-timer` or similar
- [ ] Disable removes files — complete the "disable = uninstall" mental model

### Future Consideration (v2+)

- [ ] `--offline` flag — local-only listing
- [ ] Improved error messages — better UX
- [ ] Shell completions — bash/zsh/fish
- [ ] Configuration profiles — save common flag combinations

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| `--now` for enable/disable | HIGH | MEDIUM | P1 |
| Auto-update timer/service | HIGH | LOW | P1 |
| Disable removes files | MEDIUM | LOW | P1 |
| `--reboot` for update | MEDIUM | LOW | P2 |
| `--offline` for list | LOW | LOW | P2 |
| Shell completions | MEDIUM | LOW | P2 |
| Improved error messages | MEDIUM | MEDIUM | P3 |

**Priority key:**
- P1: Must have for this milestone
- P2: Should have, add when possible
- P3: Nice to have, future consideration

## Competitor Feature Analysis

| Feature | updatectl | systemd-sysupdate | updex | Notes |
|---------|-----------|-------------------|-------|-------|
| List versions | ✓ | ✓ | ✓ | All equivalent |
| Check for updates | ✓ | ✓ | ✓ | All equivalent |
| Update | ✓ | ✓ | ✓ | All equivalent |
| Vacuum | ✓ | ✓ | ✓ | All equivalent |
| Features | ✓ | ✓ | ✓ | All equivalent |
| Components | implied | ✓ | ✓ | updatectl uses targets |
| Pending | — | ✓ | ✓ | updatectl via D-Bus |
| Discover remote | — | — | ✓ | **updex unique** |
| Install from URL | — | — | ✓ | **updex unique** |
| Remove extension | — | — | ✓ | **updex unique** |
| Progress bar | — | — | ✓ | **updex unique** |
| --now for features | ✓ | — | — | updex gap |
| --reboot | ✓ | ✓ | — | updex gap |
| --offline | ✓ | ✓ | — | updex gap |
| Partition ops | ✓ | ✓ | — | Out of scope |
| D-Bus API | ✓ | — | — | Out of scope |
| Timer/service | via systemd | ✓ | — | updex gap |

## Sources

- **systemd-sysupdate.xml** (official): https://raw.githubusercontent.com/systemd/systemd/main/man/systemd-sysupdate.xml
  - Commands: list, features, check-new, update, vacuum, pending, reboot, components
  - Options: --component, --definitions, --root, --image, --instances-max, --sync, --verify, --reboot, --offline
  - Confidence: HIGH

- **updatectl.xml** (official): https://raw.githubusercontent.com/systemd/systemd/main/man/updatectl.xml
  - Commands: list, check, update, vacuum, features, enable, disable
  - Options: --reboot, --offline, --now, --host
  - Confidence: HIGH

- **systemd-sysext.xml** (official): https://raw.githubusercontent.com/systemd/systemd/main/man/systemd-sysext.xml
  - Runtime management: merge, unmerge, refresh, status, list
  - Not directly relevant to updex (different tool), but informs ecosystem understanding
  - Confidence: HIGH

- **sysupdate.d.xml** (official): https://raw.githubusercontent.com/systemd/systemd/main/man/sysupdate.d.xml
  - Transfer file format, Features= and RequisiteFeatures= options
  - Confidence: HIGH

- **systemd-sysupdated.service.xml** (official): https://raw.githubusercontent.com/systemd/systemd/main/man/systemd-sysupdated.service.xml
  - D-Bus daemon for unprivileged updates
  - Not needed for updex (CLI-only tool)
  - Confidence: HIGH

---
*Feature research for: systemd-sysext management CLI (updex)*
*Researched: 2026-01-26*
