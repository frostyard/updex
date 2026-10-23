# 0016 — Automatic sysext updates: a packaged updex timer that applies at the next boot

- **Status:** Accepted
- **Date:** 2026-10-23

## Context

A host takes a new sysext revision only when the link
`/var/lib/extensions/<name>.raw` changes, because `systemd-sysext` merges what
that link names. updex owns the link
([core ADR-0008](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md));
nothing else moves it.

- No updex timer runs on any snosi host. `updex daemon enable`
  ([ADR-0007](0007-daemon-stages-never-activates.md)) writes
  `updex-update.timer` and `updex-update.service` into `/etc/systemd/system/`
  on request, one host at a time, and no host has run it. The units are not
  in the `frostyard-updex` package.
- snosi masks `systemd-sysupdate.timer`, `systemd-sysupdate-reboot.timer` and
  `systemd-sysupdate-update.timer` in every image (snosi#1004, snosi#1115).
  The `-update` timer (systemd 262) staged new images but never moved an
  updex link, so a staged revision stayed inactive after a reboot. Its vacuum
  (`InstancesMax=3`, no `ProtectVersion=`) can delete the image that an updex
  link names, and it exits 1 for every component whose feature is disabled.
- On 2026-10-23 two hosts had incus r5, k3s `1.36.5+k3s1+r1`,
  claude-desktop 3.6556.2 and vscode 1.143.1 staged but not linked. They
  took them only after a manual `updex features update` and a reboot.
- [Core ADR-0058](https://github.com/frostyard/core/blob/main/docs/adr/0058-ship-suite-desktop-apps-as-flatpaks-and-services-as-sysexts.md)
  (Proposed) states that suite sysext updates take effect at the next boot.
  Snosi's service sysexts run long-lived workloads (incus, docker, k3s,
  tailscale, lodge). `systemd-sysext refresh` replaces `/usr` under them.
- updex's vacuum keeps the newest `InstancesMax` images (default 2) and the
  version that the legacy `CurrentSymlink` names. updex removes that
  symlink, so in practice the vacuum knows neither the linked version nor
  the merged version. A host that is not rebooted for two updates loses the
  image it runs.
- A transfer's link moves when its download succeeds. A feature with more
  than one transfer can end a failed run with some transfers linked to the
  new version and some to the old one.
- updex has no rollback. A manual relink holds only until the next
  `updex features update`, which links the newest version again.
- updex takes no lock. Two concurrent runs can race on the same link and
  staging directory.

## Decision

**One updater, scheduled, staging only.** updex is the only automatic
updater of sysexts. A systemd timer runs
`updex features update --no-refresh` on each host. The run downloads,
verifies and links. It never runs `systemd-sysext refresh`. A new revision
takes effect at the next boot. This keeps ADR-0007's rule and changes where
the units come from, the schedule, retention, status and recovery.

### Units

- The `frostyard-updex` package ships
  `/usr/lib/systemd/system/updex-update.timer` and
  `/usr/lib/systemd/system/updex-update.service`. The package does not
  enable them.
- The unit text comes from the same generator that `updex daemon enable`
  uses (`systemd.GenerateTimer`, `systemd.GenerateService`), at release
  time. A contract test fails when the packaged units and the generator
  differ.
- The service is the ADR-0007 sandboxed `oneshot` (`SandboxDirectives`),
  with `ExecStart=/usr/bin/updex features update --no-refresh`, and adds:
  - `Wants=network-online.target` and `After=network-online.target`;
  - `ConditionACPower=true`. A host on battery skips the run. A host
    without a battery always runs.
- The timer has `OnCalendar=*-*-* 00/6:00:00`, `RandomizedDelaySec=1h`,
  `Persistent=true` and `[Install] WantedBy=timers.target`. A run that
  finds nothing new fetches only the `SHA256SUMS` manifests (and their
  signatures) of enabled features.
- updex does not check for metered networks. A host on a metered network
  downloads like any other host; its administrator masks the timer to stop
  this.
- `updex daemon enable` runs `systemctl enable --now updex-update.timer`
  when the packaged units exist. It writes units to `/etc/systemd/system/`
  only when they do not (an updex binary installed without the package).
  `updex daemon disable` disables the timer and removes only units that
  updex wrote to `/etc`. It never deletes a path that the OS image ships in
  `/etc`.

### Enablement

- snosi enables the timer on every image with a preset line,
  `enable updex-update.timer`, in a base preset file. The image build's
  `systemctl preset-all` creates the `timers.target.wants` link, and the
  finalize records it in snosi's enablement manifest (snosi ADR-0002). A
  fresh install enables the timer at its first boot.
- On an existing host, `preset-reconcile.service` enables the timer once,
  when the manifest entry first appears, and skips masked units. It runs
  after the boot transaction, so the timer runs from the second boot of the
  first image that carries the preset, unless a person starts it.
- The snosi image build fails when the preset names `updex-update.timer`
  and the image has no `/usr/lib/systemd/system/updex-update.timer`, or
  when the enablement manifest has no
  `system timers.target.wants/updex-update.timer` entry.
- On a host without snosi, the timer is off until the administrator runs
  `updex daemon enable`.
- snosi keeps `systemd-sysupdate-update.timer` masked.

### Opt-out

- Whole host: `systemctl mask updex-update.timer`. A mask survives image
  updates, and `preset-reconcile` skips masked units, so it is the durable
  opt-out on snosi. `systemctl disable --now updex-update.timer` (or
  `updex daemon disable`) also lasts, but only after the image that carries
  the preset has enabled the timer once: a disable before that image
  arrives is not recorded, and `preset-reconcile` enables the timer.
- One sysext: `updex features disable <name>`. Disabled features are not
  updated. A disabled feature is not an error.
- One sysext at one version: `updex features rollback <name>` (below).

### Failure behaviour

- A link moves only after its image downloaded and its SHA256 and, when
  the transfer requires it, its `SHA256SUMS` signature verified. A failed
  download, a failed verification or a cancelled run keeps the current link
  and the current images.
- A feature's transfers link together. updex downloads every transfer of a
  feature first. If one fails, it links none of them and keeps the
  downloaded images for the next run.
- A failure in one feature does not stop the others. The run exits non-zero
  when any enabled feature failed, so `systemctl --failed` and
  `updex daemon status` show it. The next scheduled run tries again; there
  is no extra retry beyond ADR-0008's bounded retries.
- Every mutating updex command holds an exclusive lock
  (`/run/updex/lock`, `flock`). A timer run that finds the lock taken exits 0
  with a log line and does not wait.

### Minimum image version

A sysext delta is valid only against a base that has what the delta omits
(snosi design/sysexts.md). A host can run an old image for weeks, so the
timer must not link a revision that needs a newer image.

- A sysext can set `UPDEX_MIN_IMAGE_VERSION=<value>` in its
  `extension-release.<name>`. snosi sets it only on a revision that depends
  on a base change, by hand, as it sets `SYSEXT_REVISION`. Without the key,
  nothing changes. `systemd-sysext` ignores the key.
- After the download and before the link, updex reads the key from the
  downloaded image and compares it with the `IMAGE_VERSION` of the image
  that boots next: the staged deployment when `bootc status` reports one,
  else the booted image's `os-release`.
- Both values must be digits only (snosi uses 14-digit timestamps), and
  updex compares them as numbers. When the image is older than the minimum,
  or when either value is missing or not numeric while the key is set,
  updex keeps the old link and the downloaded image, and reports
  `needs image >= <value>` for that feature. The other features continue,
  and the run does not fail for this reason.

### Retention

The vacuum after an install keeps, for each transfer:

- the version that `/var/lib/extensions/<name>.raw` names (linked);
- the version merged now, read from the hierarchy metadata
  ([ADR-0015](0015-detect-merged-sysexts-from-hierarchy-metadata.md));
- the version that a rollback rejected, while the rollback record exists;
- `ProtectVersion=`; and
- the newest versions up to `InstancesMax` (default 2).

A host therefore keeps 2 images per transfer after a reboot (the running
one and one rollback target) and at most 3 before it, when it has linked a
new revision and still runs an older one.

### Status and logs

- The run logs to the journal: `journalctl -u updex-update.service`.
- `updex features check` and `updex features list` report, per transfer,
  the merged version (`RUNNING`), the linked version (`LINKED`, which the
  next boot merges) and the newest available version. A host with
  `LINKED` different from `RUNNING` has an update pending a reboot.
  `--json` carries the same fields.
- `updex daemon status` reports whether the timer is enabled, the next
  run, and the time and result of the last run (from systemd's
  `NextElapseUSecRealtime`, `ExecMainExitTimestamp` and `Result`).
- A command that runs `systemd-sysext refresh` (`features update` without
  `--no-refresh`, `features enable --now`, `features disable --now`) first
  lists the other linked-but-not-merged sysexts that the refresh activates
  too.

### Rollback

`updex features rollback <name> [--to VERSION] [--now]`:

- relinks each transfer of the feature to `VERSION`, or, by default, to the
  newest staged version older than the linked one;
- refuses when that image is not on disk, and changes nothing;
- records the version it rolled back from as rejected in
  `/var/lib/updex/rollback/<name>`. `features update` (and the timer) does
  not link a rejected version or an older one. It links the next newer
  version when one is published, and then deletes the record;
- takes effect at the next boot, or with `--now` through a refresh;
- `updex features rollback --clear <name>` deletes the record.

## Consequences

- Every snosi host takes new sysext revisions within about 7 hours of
  publication while on AC power, and runs them after its next reboot. A host
  that never reboots never runs them.
- A refresh started for another reason activates all pending links. The
  warning makes this visible; it does not prevent it.
- Hosts download more, about one image per enabled sysext per revision, and
  keep up to 3 images per transfer before a reboot. An incus image is about
  1 GB uncompressed.
- A sysext revision built against a newer base waits on a host with an
  older image only when snosi set `UPDEX_MIN_IMAGE_VERSION` on it. A
  revision without the key that needs a newer base still breaks such a
  host at its next boot; setting the key is snosi's obligation.
- updex must read a file from inside a downloaded image
  (`systemd-dissect --copy-from` or an equivalent) inside the sandboxed
  service.
- `frostyard-updex` gains 2 unit files and a release-time generation step.
  A host with the old ADR-0007 units in `/etc` keeps them; they override the
  packaged units until `updex daemon disable` removes them.
- A manual `systemd-sysupdate` run still vacuums without knowledge of the
  updex link. It stays a manual tool.

## Alternatives considered

- **Unmask `systemd-sysupdate-update.timer`:** rejected. It moves only its
  own `CurrentSymlink`, so it activates nothing. Its vacuum can delete the
  linked image, and it exits 1 on every host with a disabled feature. Two
  updaters would write `/var/lib/extensions.d`.
- **Point the updex link at sysupdate's `CurrentSymlink`:** rejected.
  `systemd-sysupdate-notify-sysext@.service` then refreshes live under
  running services, and it reverses core ADR-0008 (`CurrentSymlink` is
  legacy).
- **Refresh after each download:** rejected for the reason in ADR-0007: it
  replaces `/usr` under running workloads with no operator present.
- **Link at boot** (the timer stages only; a boot service relinks before
  `systemd-sysext.service`): rejected. It puts updex on the early boot path,
  where a failure delays or blocks the merge of every sysext, and it adds a
  second "pending" state next to the link. Its one benefit, no activation by
  an unrelated refresh, the refresh warning covers for the known commands.
- **The units in the snosi image:** rejected. The unit text depends on
  updex's flags and would drift from the updex release; hosts without snosi
  would not get them. snosi ships only the enablement.
- **Keep per-host `updex daemon enable`:** rejected. It is the state today,
  and no host has it.
- **Daily schedule (ADR-0007):** rejected. With `ConditionACPower=true`, a
  laptop on battery at the one daily time misses the day. 4 runs a day cost
  4 manifest fetches per enabled sysext.
- **A rollback that holds until released:** rejected as the default. A held
  host stops taking fixes. The rejected-version record resumes updates when
  a newer revision is published.
- **No updex image check; snosi publishes a base-dependent revision only
  after the images reach users:** rejected. That hold worked while no host
  took sysexts automatically. With the timer, every host takes a revision
  within hours, and a host that does not reboot, or holds or rolls back its
  image, runs an old image for weeks.

## References

- Shapes: [design/overview.md — Auto-update daemon](../design/overview.md#auto-update-daemon),
  [design/packaging-and-maintainers.md](../design/packaging-and-maintainers.md),
  [specs/sdk-api.md](../specs/sdk-api.md),
  [plans/0001-automatic-sysext-updates.md](../plans/0001-automatic-sysext-updates.md)
- Supersedes: [ADR-0007](0007-daemon-stages-never-activates.md)
- Builds on: [ADR-0008](0008-bounded-retry-no-resume.md),
  [ADR-0015](0015-detect-merged-sysexts-from-hierarchy-metadata.md);
  core [ADR-0008](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md),
  [ADR-0055](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md),
  [ADR-0058](https://github.com/frostyard/core/blob/main/docs/adr/0058-ship-suite-desktop-apps-as-flatpaks-and-services-as-sysexts.md)
- snosi: [ADR-0002](https://github.com/frostyard/snosi/blob/main/docs/adr/0002-ship-no-enablement-symlinks-in-etc.md),
  [ADR-0004](https://github.com/frostyard/snosi/blob/main/docs/adr/0004-sysext-authoring-rules.md),
  [design/sysexts.md](https://github.com/frostyard/snosi/blob/main/docs/design/sysexts.md),
  [snosi#1004](https://github.com/frostyard/snosi/pull/1004),
  [snosi#1115](https://github.com/frostyard/snosi/pull/1115)
- Related: [updex#456](https://github.com/frostyard/updex/pull/456)
