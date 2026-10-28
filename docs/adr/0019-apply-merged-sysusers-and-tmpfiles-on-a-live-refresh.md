# 0019 — Apply merged sysusers and tmpfiles on a live refresh

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

A lab run on Snow `20261028124727` (frostyard/lab PR #163, run record
`docs/plans/2026-10-28-snow-sysext-probe-run.md`, run
`dev-snow-bootc-lifecycle-kb7qb`) enabled the `notesd` and `maild` features
with `updex features enable <feature> --now`:

- Both commands returned 0. Both sysexts merged into `/usr`.
- The accounts `frostmail`, `frostnotes`, `lodge` and `lodge-apps` did not
  exist. Only the sysexts' own `/usr/lib/sysusers.d` fragments declare them.
- `frostnotes-lodge.socket` and `frostmail.socket` name those accounts in
  `SocketUser=`/`SocketGroup=`. `multi-user.target` upholds them. Each start
  failed (`Failed to listen`) until `start-limit-hit`, and the services failed
  on the dependency.
- After a reboot every check passed.

The cause is the order of the steps on a live refresh. `systemd-sysext
refresh` merges the images and then, because the images set
`EXTENSION_RELOAD_MANAGER=1`, reloads the service manager. The reload makes
the new unit files visible, and `Upholds=` starts the sockets at once. Nothing
runs `systemd-sysusers` or `systemd-tmpfiles` for the merged `/usr`: at boot
snosi's `reload-sysext.service` runs both after `systemd-sysext.service` and
before `daemon-reload`, but nothing does it on a live refresh. updex runs the
only live refresh on these hosts (`enable --now`, `disable --now`, `update`
without `--no-refresh`, `rollback --now`).

systemd gives no hook between the merge and the reload. Since systemd 255,
`systemd-sysext --no-reload` skips the reload (and, since 262, also the
`EXTENSION_RESTART_UNITS=` and `EXTENSION_RELOAD_OR_RESTART_UNITS=` handling).
Before 255, `refresh` never reloads the manager. From 255 to 259, a `refresh`
always unmounts and remounts the overlay and then applies those settings for
the merged set. Since 260, a `refresh` does nothing when no image was added,
removed or changed, unless it gets `--always-refresh=yes` (added in 260).

## Decision

`sysext.DefaultRunner.Refresh` activates the merged images in the boot order.
It first reads the major version from `systemd-sysext --version`, then runs:

1. `systemd-sysext refresh --no-reload`: merge the new set; the manager does
   not see it yet.
2. `systemd-sysusers`: create the accounts the merged `sysusers.d` declares.
3. `systemd-tmpfiles --create --graceful`: create the merged `tmpfiles.d`
   paths. No `--remove` and no `--boot`: those belong to boot.
4. `systemd-sysext refresh --always-refresh=yes`: merge the same set again,
   now with systemd's own reload and unit-restart handling, so a unit the
   reload starts finds its accounts and paths. `--always-refresh=yes` is
   needed because the set did not change since step 1, and systemd 260 and
   newer would skip a plain refresh, and with it the reload.

By version:

- 260 and newer, or a version updex cannot read: the 4 steps above. An
  unreadable version takes the newest form, so an older systemd fails loudly
  on the unknown option instead of skipping the reload silently.
- 255 to 259: step 4 is a plain `systemd-sysext refresh` (it always
  remounts; `--always-refresh` does not exist).
- Older than 255: step 1 is a plain `systemd-sysext refresh` (it never
  reloads, and `--no-reload` does not exist), then steps 2 and 3; no step 4.

Failures:

- Step 1 fails: `Refresh` returns `systemd-sysext refresh failed: …` and runs
  nothing else (as before).
- Step 2 fails: `Refresh` continues with steps 3 and 4 (the merged set must
  still reach the manager) and then returns an error that names
  `systemd-sysusers`. When the last refresh succeeded, that error wraps
  `sysext.ErrActivationIncomplete`: the extensions are merged, but their
  accounts may be missing. The SDK then says so, and says that a reboot
  finishes the activation, instead of the plain refresh-failure text (a
  plain `systemd-sysext refresh` does not run `systemd-sysusers`).
- Step 3 fails: `Refresh` writes a warning to stderr and continues; it is not
  an error. One bad rule in any merged sysext must not fail every refresh
  (the same rule as snosi#1167 for `reload-sysext.service`).
- Step 4 fails: `Refresh` returns `systemd-sysext refresh failed: …`, joined
  with a step 2 error if there is one. The merge state is then not known, so
  it never wraps `ErrActivationIncomplete`.

The SDK calls `Refresh` exactly as before, so every operation that refreshes
gets the sequence. Each such failure keeps the existing refresh-failure
contract (`RefreshError`, `Success=false`, a non-zero CLI exit). The timer
(`--no-refresh`) runs none of it. `Merge` and `Unmerge` do not change: the SDK
never calls `Merge`, and `Unmerge` adds no files.

updex does not restart, reset or start units itself. With the accounts and
paths in place before the manager reload, `Upholds=`, `WantedBy=`, and the
`EXTENSION_*` settings start what the image asks for.

## Consequences

- `updex features enable <feature> --now` activates a sysext that ships its
  own accounts or paths in the running boot, as a reboot does.
- A live refresh now remounts the overlay twice. Each remount has the brief
  gap the `systemd-sysext` man page describes, in which the merged files are
  absent. Processes that read sysext files during a refresh see two gaps, not
  one.
- On systemd 260 and newer, step 4 forces a remount and a manager reload also
  when nothing changed, for example `updex features update` with nothing new.
  systemd alone would have skipped that refresh.
- Each refresh runs `systemd-sysext --version` once more.
- Debian trixie ships 257 and snosi ships 262. Older than 255 gets the
  accounts and paths after the merge; it has no reload in the refresh, so no
  unit starts before them.
- `systemd-sysusers` and `systemd-tmpfiles --create` act on the whole merged
  configuration, not only on the new sysext. Both are idempotent and are what
  the boot runs; a live refresh now changes `/etc/passwd`, `/etc/group` and
  tmpfiles paths where it did not before.
- `sysctl.d` from a sysext is still applied only at boot: which keys to apply
  live is image policy (snosi's `reload-sysext.service` applies a reviewed
  `--prefix` list).
- A unit that already hit its start limit in an earlier failed refresh stays
  failed until `systemctl reset-failed <unit>` or a reboot.
- A plain `systemd-sysext refresh` run by hand still has the old order.

## Alternatives considered

- **Run `systemd-sysusers` and `systemd-tmpfiles` after a plain refresh,
  then restart the feature's failed units:** the reload in the refresh starts
  upheld units before the accounts exist, so they fail first. updex would have
  to know each feature's units and their start limits, which is unit policy,
  not update policy.
- **One plain refresh, with `systemd-sysusers` before it:** the accounts are
  declared inside the images, which are not merged yet.
- **`--no-reload`, then `systemctl daemon-reload` from updex:** updex would
  have to copy systemd's `EXTENSION_RELOAD_MANAGER=` decision and the 262
  `EXTENSION_RESTART_UNITS=`/`EXTENSION_RELOAD_OR_RESTART_UNITS=` handling,
  and follow every later change to them. The second refresh lets systemd keep
  that logic.
- **A snosi unit that runs sysusers on refresh:** systemd has no event for a
  refresh to trigger it, and it would also run after the reload. It would fix
  snosi only, not other hosts that run updex.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [specs/sdk-api.md](../specs/sdk-api.md)
- Builds on: [ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md)
  (the timer never refreshes)
