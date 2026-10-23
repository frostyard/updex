# Plan: Automatic sysext updates

This plan delivers [ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md):
a packaged `updex-update.timer` that snosi enables, safe retention, status,
and a rollback command. Phases 1–5 are updex pull requests, in this order,
each with `make ci` green. Phase 6 is a release. Phase 7 is a snosi change.
Phase 8 checks real hosts. Nothing starts before the ADR is Accepted.

## Phase 1 — Retention and atomic links (S)

- The vacuum protects the linked version and the merged version, besides
  `ProtectVersion=` and the newest `InstancesMax`
  (`sysext/manager.go` `planVacuum`, through the client's runtime paths).
- `UpdateFeatures` downloads every transfer of a feature, then links them
  all, or none when one failed.
- Mutating commands take `/run/updex/lock`; a timer run that finds it taken
  exits 0.
- Checks: table tests in `sysext/manager_test.go` (linked newest, merged
  older than `InstancesMax`, merged absent, link dangling); a feature with
  2 transfers where the second download fails keeps both old links
  (`updex/features_test.go`); a lock test with 2 clients.
- **Done when:** a test host with 3 revisions published and no reboot keeps
  its running image and its linked image.

## Phase 2 — Packaged units (S)

- A hidden `updex-cli` subcommand (or `scripts/units.sh`) writes the timer
  and service from `systemd.GenerateTimer`/`GenerateService`; GoReleaser's
  before-hook runs it, and `nfpms.contents` installs the files under
  `/usr/lib/systemd/system/`.
- The service gains `Wants=`/`After=network-online.target` and
  `ConditionACPower=true`; the timer gains the 6-hour schedule and
  `[Install] WantedBy=timers.target`.
- `daemon enable` and `daemon disable` use the packaged units when they
  exist; `daemon status` reports the next run and the last result.
- Checks: a contract test compares the generated files with the generator
  output and pins `--no-refresh`; `systemd-analyze verify` on both units in
  CI; `dpkg-deb -c` on the snapshot `.deb` lists both paths; the
  `Release config` job stays green.
- **Done when:** the nightly `dev` `.deb` installs both units, disabled.

## Phase 3 — Status (S)

- `features check` and `features list` add `RUNNING` and `LINKED`
  (JSON fields too); refreshing commands list the pending links that they
  activate.
- Checks: `cmd/updex` handler tests with temporary hierarchies
  (`setRuntimePaths`); E2E JSON shape in `tests/e2e/`.
- **Done when:** on a host with a pending revision, `updex features check`
  shows `RUNNING` ≠ `LINKED`.

## Phase 4 — Rollback (M)

- `updex features rollback <name> [--to VERSION] [--now] [--clear]` in the
  SDK (`Client.RollbackFeature`, options and result structs) and the CLI.
- The record lives in `/var/lib/updex/rollback/<name>`, written with
  `writeManagedFile`; `UpdateFeatures` skips rejected and older versions and
  deletes the record when it links a newer one.
- Checks: tests for the default target, `--to`, a target not on disk
  (no change), a timer run with the record (no relink), a newer revision
  (relink, record deleted), `--clear`; docs in README, `docs/specs/sdk-api.md`
  and `docs/design/overview.md`.
- **Done when:** after `rollback incus`, a timer run keeps the old link,
  and the next revision is linked automatically.

## Phase 5 — Minimum image version (S)

- After the download and before the link, read `UPDEX_MIN_IMAGE_VERSION`
  from the image's `extension-release.<name>`; compare it with the
  next-boot `IMAGE_VERSION` (staged deployment from `bootc status`, else
  the booted `os-release`); keep the old link and report
  `needs image >= <value>` when the image is too old or a value is missing.
- Checks: tests with fixture images and fake `bootc status` output: key
  absent (link), image new enough (link), too old (no link, other features
  continue, exit 0), staged deployment newer than booted (link), value not
  numeric (no link); the read works under `SandboxDirectives`
  (`systemd-run -p` with the same directives on a test host).
- **Done when:** a fixture revision with a future minimum stays unlinked,
  and `features check` shows why.

## Phase 6 — Release (Brian)

- Brian tags the release (`make bump`). The release workflow requests APT
  publication; apt-publisher publishes `frostyard-updex` and dispatches a
  snosi `build`.
- Checks: `apt-cache policy frostyard-updex` on a host shows the new
  version from `repository.frostyard.org/debian`; the new snosi image
  contains `/usr/lib/systemd/system/updex-update.timer`.
- **Done when:** a published snosi image carries the packaged units.

## Phase 7 — snosi enablement (Murbella)

- A preset line `enable updex-update.timer` in
  `mkosi.images/base/mkosi.extra/usr/lib/systemd/system-preset/04-bootc-update.preset`
  (or a new `05-updex.preset`); the enablement manifest lists
  `system timers.target.wants/updex-update.timer`;
  `docs/design/sysexts.md` describes the timer next to the
  `systemd-sysupdate-update.timer` mask, the `mask` opt-out, and the rule
  for `UPDEX_MIN_IMAGE_VERSION` on base-dependent revisions.
- A fail-closed image-build check: the build fails when the preset names
  `updex-update.timer` and `/usr/lib/systemd/system/updex-update.timer` is
  missing, or when the manifest lacks the entry.
- Checks: snosi CI with that build check; `test/bootc-secure-static-test.sh`
  pins the preset line next to the masks. `test/tests/05-firstboot-presets.sh`
  is a manual VM test, not a gate. This phase merges only after Phase 6, so
  the preset never names a missing unit.
- **Done when:** a published image enables the timer at first boot, and
  `preset-reconcile` enables it on an existing host.

## Phase 8 — Fleet check

- On framework and selfie after their image update:
  `systemctl is-enabled updex-update.timer` is `enabled` after the first
  boot of the new image (`preset-reconcile` runs after the boot
  transaction); `systemctl list-timers updex-update.timer` shows a next run
  from the second boot; after a run,
  `journalctl -u updex-update.service` shows it, and
  `updex daemon status` shows its result.
- After the next sysext publication, `updex features check` shows
  `LINKED` = the new revision before a reboot and `RUNNING` = it after one;
  `find /var/lib/extensions -xtype l` is empty.
- Before the first base-dependent revision after Phase 7, snosi sets
  `UPDEX_MIN_IMAGE_VERSION` on it, and a host on an older image keeps its
  old link.
- **Done when:** both hosts run a new revision that no person installed.

## Open questions

*(none)*

## References

- Implements: [ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md),
  [design/overview.md](../design/overview.md),
  [specs/sdk-api.md](../specs/sdk-api.md)
