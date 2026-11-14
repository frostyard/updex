# 0022 — Retry a failed suite-switch run at boot

- **Status:** Accepted
- **Date:** 2026-11-13

## Context

[ADR-0021](0021-link-the-running-suites-images-at-boot-after-a-suite-switch.md)
adds `updex-suite-switch.service`: a `oneshot` that runs
`updex features update --suite-switch` once per boot, so the first boot after
a suite switch links and merges the running suite's image of a feature whose
link still names the other suite's image. Its consequence is that a failed run
leaves the feature missing until a later boot or a manual run.

A run can fail for a reason that goes away a few minutes later:

- The new suite has no published image yet, or the source does not answer.
- The network is not usable yet, although `network-online.target` is reached.
- Another updex operation holds the lock. Under systemd a busy lock is an
  error for `--suite-switch`, not a skip, because no later timer run on this
  boot refreshes.

The timer service does not close the gap: it runs with `--no-refresh`
([ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md)), so the
feature comes back only at the next boot. Boot must never wait for the unit
(`DefaultDependencies=no`, ADR-0021).

## Decision

`updex-suite-switch.service` restarts a failed run, a bounded number of times.
`updex.SuiteSwitchUnit()` adds 4 lines, and the unit keeps every other line of
ADR-0021:

- `[Unit]` `StartLimitIntervalSec=12h` and `StartLimitBurst=4`: 1 start and at
  most 3 retries in the window. Then systemd marks the unit failed with the
  result `start-limit-hit` and starts it no more until the next boot (or
  `systemctl reset-failed`).
- `[Service]` `Restart=on-failure` and `RestartSec=5min`. `on-failure` covers
  a non-zero exit, a signal and the `TimeoutStartSec=2h` timeout. systemd
  supports `Restart=` with `Type=oneshot` from version 244 (trixie has 257,
  forky 262).

The `systemd.ServiceConfig` generator needs no change: the start limit goes in
`UnitDirectives`, the restart lines in `Directives` before the refresh
sandbox. `updex-update.timer` and `updex-update.service` do not change.

## Consequences

- A failure that goes away within about 15 minutes (3 waits of 5 minutes plus
  the run times) now brings the feature back on that boot.
- Boot still does not wait. `DefaultDependencies=no` stays, so
  `multi-user.target` gets no implicit `After=` on the unit. The unit is
  ordered before no boot target, no unit `Requires=`, `BindsTo=` or
  `Requisite=` it, and its `[Install]` has only `WantedBy=multi-user.target`.
  Between 2 tries the unit is `activating (auto-restart)` with no job queued
  (checked on systemd 262), so no job waits for it.
- `Before=updex-update.service` orders only jobs that are queued together. A
  timer run that starts during the 5-minute wait does not wait for the retry,
  and a retry does not wait for a running timer service. The two can then
  overlap, and the operation lock decides: the timer service skips a busy lock
  under systemd (exit 0), and the suite-switch run fails and counts 1 more
  start. A busy lock fails at once, so a loop of busy runs reaches the start
  limit within about 15 minutes. Nothing depends on the unit's success.
- When a timer run links the running suite's image between 2 tries, the next
  try finds no link to another suite and exits 0 without a refresh; the
  feature then merges at the next boot, as ADR-0021 states for a timer run
  that comes first.
- The bound holds also when every run hangs until `TimeoutStartSec=2h`. The
  start limit counts starts in a fixed window that begins at the first start.
  Hung runs start at 0, 2h05m, 4h10m and 6h15m; the 4th run ends at 8h15m,
  and the 5th start comes at 8h20m = 4 × (2h + 5min). That is inside the 12h
  window, so systemd refuses it and the unit is failed with
  `start-limit-hit`. The rule is: `StartLimitIntervalSec` must be longer than
  `StartLimitBurst` × (`TimeoutStartSec` + `RestartSec`). A 6h window breaks
  it: only 3 hung starts fit in it, a new window begins at 6h15m, and the
  retries never end. 12h leaves 3h40m of margin.
  `TestSuiteSwitchUnitRetryIsBounded` and the units contract test check the
  rule, so a later change to 1 of the 4 values cannot break it unnoticed.
- In the worst case (every run hangs) the unit tries for about 8h20m after
  boot, 1 run at a time; the timer runs as usual in that time.
- `journalctl -u updex-suite-switch.service` shows each try; `systemctl
  status` shows `activating (auto-restart)` between tries and `failed` with
  `start-limit-hit` after the last one.

## Alternatives considered

- **No retry (ADR-0021 as it was):** a short outage at boot leaves the feature
  missing for a whole boot.
- **Retry inside updex (a loop around the update):** it keeps the unit
  `activating`, and so its start job, for the whole loop, and it repeats what
  systemd's restart logic already does with a visible state and journal.
- **Wait for the lock instead of failing:** updex never waits for the lock
  (ADR-0016); a retry from systemd gives the same result without that change.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [specs/cli-reference.md](../specs/cli-reference.md),
  [specs/sdk-api.md](../specs/sdk-api.md)
- Builds on: [ADR-0021](0021-link-the-running-suites-images-at-boot-after-a-suite-switch.md),
  [ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md)
