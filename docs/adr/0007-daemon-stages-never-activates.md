# 0007 — The auto-update daemon stages updates but never activates them

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

Unattended updates on a running system carry a specific hazard for
sysexts: `systemd-sysext refresh` re-merges extensions immediately,
changing `/usr` under running workloads with no operator present. updex
needs periodic automatic downloads (so a reboot picks up current versions)
without ever changing the live system behind the operator's back. The
scheduling mechanism also writes into `/etc/systemd/system/`, a directory
administrators own, so it must not clobber units it did not create.

## Decision

`updex daemon enable` (`cmd/updex/daemon.go`) installs a systemd timer that
stages but never activates:

- Units are named `updex-update.timer`/`updex-update.service` — the
  `<tool>-<verb>` convention (`unitName` constant).
- The timer runs `OnCalendar=daily` with `Persistent=true` and
  `RandomizedDelaySec=3600`, spreading load on the artifact origin across
  the fleet.
- The service is a `oneshot` running
  `/usr/bin/updex features update --no-refresh`: new versions are
  downloaded, verified, and staged, and the activation symlinks updated,
  but the final `systemd-sysext refresh` is skipped — nothing changes in
  the running system until a later refresh or reboot.
- Installation refuses to overwrite existing unit files, both at the CLI
  (`daemon enable` errors when `mgr.Exists(unitName)`) and in
  `systemd.Manager.Install`, which errors if either file already exists
  and rolls back the timer file if the service write fails. Reinstalling
  requires an explicit `daemon disable` first.

## Consequences

- Automatic updates are safe to enable on production machines: the running
  system is immutable between reboots, and reboots always boot current
  staged versions.
- Users must understand the two-phase model — `daemon status` showing an
  active timer does not mean the newest version is *running*; the CLI
  output says "Reboot required" for this reason.
- A hand-edited `updex-update.service` is never silently replaced;
  the disable-then-enable dance is the only upgrade path for the units.
- The daily-with-jitter schedule is fixed at install; changing it means
  disable/enable or a systemd drop-in on the timer.

## Alternatives considered

- **Refreshing after download (full activation):** mutates `/usr` under
  running workloads unattended; rejected as the core hazard.
- **A long-running daemon process:** a systemd timer plus oneshot service
  is the platform-native scheduler — no extra process to supervise, and
  `Persistent=true` covers missed windows.
- **Overwriting existing units on enable (idempotent install):** would
  silently destroy administrator customizations; an explicit error
  pointing at `daemon disable` keeps ownership clear.

## References

- Implements: [`cmd/updex/daemon.go`](../../cmd/updex/daemon.go)
  (`unitName`, `runDaemonEnable`),
  [`systemd/unit.go`](../../systemd/unit.go) (unit generation),
  [`systemd/manager.go`](../../systemd/manager.go) (`Install` overwrite
  refusal)
- Shapes: [design/overview.md — Auto-update daemon](../design/overview.md#auto-update-daemon)
- Builds on: [core ADR-0008 — Sysext distribution layout and update contract](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md)
