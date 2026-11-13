# Spec: updex CLI

The contract of the `updex` command that operators and automation consume: the
commands, the global flags, the state they change, and the text output, JSON
fields and exit statuses that scripts and systemd units depend on. The
`.transfer`, `.feature` and `.catalog` file formats and the rules behind them
are the [configuration reference](config-reference.md), and the Go API is
[sdk-api.md](sdk-api.md).

## Interface

### Install

Download the latest CLI package for your system from the
[GitHub releases page](https://github.com/frostyard/updex/releases/latest):

| System                    | Release artifact                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------- |
| Debian/Ubuntu             | `frostyard-updex_<version>_amd64.deb` or `frostyard-updex_<version>_arm64.deb`        |
| Fedora/RHEL               | `frostyard-updex-<version>-1.x86_64.rpm` or `frostyard-updex-<version>-1.aarch64.rpm` |
| Alpine                    | `frostyard-updex_<version>_x86_64.apk` or `frostyard-updex_<version>_aarch64.apk`     |
| Other Linux distributions | `updex_<version>_linux_amd64.tar.gz` or `updex_<version>_linux_arm64.tar.gz`          |

The matrix lists the package and archive formats. Each release also publishes
`checksums.txt`. A Debian-family host installs the downloaded file with:

```bash
sudo apt install ./frostyard-updex_<version>_amd64.deb
```

Download `checksums.txt` from the same release and verify the artifact before
installing it:

```bash
sha256sum --ignore-missing --check checksums.txt
gh attestation verify <downloaded-artifact> --repo frostyard/updex
```

The checksum detects corruption or truncation; the GitHub build-provenance
attestation binds the artifact to a tag release built by this repository's
workflow, rather than trusting only a checksum served alongside it. The
[GitHub CLI](https://cli.github.com/) is needed for attestation verification;
`gh attestation verify` also works on `checksums.txt`.

A host with a compressed EROFS sysext also needs `erofs-utils >= 1.8.5`; see
[erofs-utils for compressed sysext images](../design/packaging-and-maintainers.md).

### Build from source

With [Go 1.26.11](https://go.dev/doc/install) and `make`, run `make build` to
create `build/updex`, or `make install` to put the binary in `GOPATH/bin`.
Building and unit testing do not require systemd. See
[CONTRIBUTING.md](../../CONTRIBUTING.md) for the development workflow and the
[SDK API reference](sdk-api.md) to use the Go library.

### Everyday CLI usage

Configured `.feature` and `.transfer` files are read from `/etc`, `/run`,
`/usr/local/lib`, and `/usr/lib` under `sysupdate.d/` and discovered
`sysupdate.<name>.d/` directories. Use `updex components` to see named
components. See the [configuration reference](config-reference.md)
for file formats, search precedence, and examples.

### Features and components

```bash
updex features list                       # Show features and their origins
sudo updex features enable docker         # Enable for the next update
sudo updex features enable docker --now   # Enable and download now
sudo updex features disable docker        # Stop future updates; stays merged until --now
sudo updex features disable docker --now  # Unmerge and remove images now
sudo updex features disable docker --now --force # If currently merged; reboot required
sudo updex features update                # Install newest versions
sudo updex features update --no-vacuum    # Keep older versions
sudo updex features rollback docker       # Link the previous staged version (next boot)
sudo updex features rollback docker --to r4 --now  # A given version, applied by a refresh
sudo updex features rollback --clear docker        # Delete the rollback record
sudo updex --dry-run features update      # Preview without modifying state
updex features check                      # Check for updates (read-only)
sudo updex features update docker         # Update only the docker feature
updex features check docker               # Check only the docker feature
updex components                          # List discovered named components
updex features list --component=docker    # Inspect one named component
sudo updex features update --component=docker
```

Without `--component`, `features` commands use the union of the legacy default
directory and discovered named components. The `--component` flag applies to
every `features` subcommand and cannot be combined with `-C, --definitions`,
which instead reads one explicit directory and bypasses discovery.

### Naming features

`features update` and `features check` take optional
feature names. With no name they act on every enabled feature, as before. With
names they act only on those features and leave the other enabled features
alone. Every name is checked against the loaded domain (after `--component` or
`-C` scoping) before anything is downloaded, linked, vacuumed or refreshed, and
an error names each problem: `feature 'x' not found`,
`feature 'x' is not enabled; run 'updex features enable x' first`, or
`feature 'x' is masked`. A feature-name validation failure changes nothing and
exits non-zero. The refresh after the update still activates every pending link
on the host, so the warning lists pending links of enabled features that the
run does not name (use `--no-refresh` to leave them alone).

### `Features=` and `RequisiteFeatures=`

A transfer is active when its
`Features=` is empty or at least one listed feature is enabled (OR), and every
feature in its `RequisiteFeatures=` is enabled (AND), as in
`sysupdate.d(5)`. A feature that does not exist, is disabled or is masked is
not enabled. The feature commands act only on active transfers, and say which
ones they left alone:

- `features update` and `features check` skip an inactive transfer: nothing is
  downloaded, linked or vacuumed, and the output shows
  `<feature>/<component>: inactive: waits for feature(s) lodge` (or
  `needs one of features: maild`; JSON `inactive`).
- `features enable maild --now` installs the transfers that are active with
  `maild` enabled and reports, without downloading, those that still wait for
  another feature. Enabling that feature later (`enable lodge --now`) installs
  them.
- `features disable a --now` removes only the transfers that `a` no longer
  activates. A transfer that another enabled feature in its `Features=` keeps
  active (`Features=a b`, with `b` enabled) is kept, with its link and
  images, and reported as `<component>: kept: still active through feature(s) b`
  (JSON `kept`). When nothing is left to remove, there is no unmerge and no
  refresh.

### Refresh failure

If the final `systemd-sysext refresh` fails after
`enable --now`, `disable --now`, or `update`, updex reports completed steps
and exits non-zero (`refresh_error` in JSON). After `enable --now` (and
`rollback --now`), reboot: a plain `systemd-sysext refresh` runs no
`systemd-sysusers`, so it can merge the new sysext without its accounts. After
`disable --now`, the remaining extensions may stay unmerged: run
`systemd-sysext refresh` manually or reboot (their accounts already exist).

### Accounts and paths

With `--now` (and `update` without `--no-refresh`),
updex runs `systemd-sysusers` and `systemd-tmpfiles --create` for the merged
sysexts before the service manager reloads, so a sysext's own accounts and
directories exist at once, as after a reboot. A `systemd-sysusers` failure
fails the refresh with a message that the images are merged and a reboot
finishes the activation; a `systemd-tmpfiles` failure only prints a warning.
Older than systemd 255 gets the accounts after the merge.

### Check errors

If a component's manifest cannot be fetched or verified,
`features check` reports `UPDATE=error` (JSON: `error`), continues reporting
healthy components, and exits non-zero. Do not interpret an errored row as
"up to date."

### What "current" means

`features check` compares the newest available
version with the version the systemd-sysext link in `/var/lib/extensions`
points to, because that link is the image systemd-sysext merges. If no usable link
exists it falls back to the staged image. A link still on an older image
therefore shows an update, and `features update` repoints the link without
downloading again and reports that a reboot activates it. The JSON field
`current_version` keeps this meaning.

### Running and linked versions

`features check` and `features list` show,
for each transfer, two more versions:

- `RUNNING` (JSON `running_version`): the version merged now, read from the
  `.systemd-sysext` metadata that systemd-sysext keeps under `/usr` and `/opt`.
  `-` (empty in JSON) means the image is not merged; `unknown` means it is
  merged but the metadata records no version.
- `LINKED` (JSON `linked_version`): the version that
  `/var/lib/extensions/<name>.<ext>` names, which the next boot merges. `-`
  (empty) means there is no usable link (absent, dangling, or foreign).

When `LINKED` differs from `RUNNING`, an update is pending a reboot. The JSON
field `pending_reboot` is `true` and the text output ends with a
`PENDING REBOOT: ...` line that names each transfer (`docker: r1 -> r2`;
`not merged` when nothing runs). A host that cannot tell (`RUNNING` is
`unknown`, or the state could not be read) is not marked. `features check`
puts the two columns between `CURRENT` and `NEWEST`; `features list` appends
them after `TRANSFERS` and adds a `transfer_status` array to each feature in
JSON (`component`, `running_version`, `linked_version`, `pending_reboot`,
`error`). A feature with several transfers shows one value per transfer in
the order of `TRANSFERS`, for example `2.0.0, -`, and `error` replaces both
values of a transfer whose state could not be read (the rest of the listing
still prints; `features check` reports it as that row's `error`).

### Refresh warning

`features update` (without `--no-refresh`),
`features enable --now` and `features disable --now` run
`systemd-sysext refresh`, which activates every linked-but-not-merged sysext on
the host, not only the ones you asked about. Before the refresh, updex warns
and lists the others (`systemd-sysext refresh will also activate 1 sysext(s)
that are linked but not merged: beta: 1.0.0 -> 2.0.0`). It also lists the
sysexts that are merged but have no link, because the refresh unmerges them
under their running workload (`… will also deactivate 1 sysext(s) that are
merged but not linked: beta: 1.0.0 -> not linked`). The JSON result carries
the same list as `pending_activations` (`component`, `running_version`,
`linked_version`, and `deactivate: true` for a deactivation). For `update`, the enabled features are the run's own, so the
list holds transfers that are not part of an enabled feature, such as a disabled
feature whose link still names a newer image. With `--dry-run` it says what the
refresh would activate. With `--no-refresh` nothing is listed, because nothing
is refreshed. The warning does not prevent the activation. It sees only links
of transfers that updex's definitions describe.

### A feature's transfers update together

`features update` (and
`features enable --now`) downloads every transfer of a feature first and links
them all only when every download succeeded. If one fails, the feature keeps
all its old links, the images already downloaded stay on disk for the next run,
and the table shows the failure on that transfer and
`not linked: another transfer of feature <name> failed` on the others. Other
features still update. If a link fails part-way, the links already moved are
put back.

### Rolling back

`updex features rollback <name>` links every transfer of
a feature to an older version that is still staged on disk, and records the
version it rolled back from as rejected. By default each transfer goes to its
own newest staged version older than the one its link names; `--to VERSION`
sets one version for all of them. If any transfer has no such version on disk
(or no link), nothing changes and the command fails with, for example,
`version r4 of docker is not on disk`. The links move together: if one fails,
the others are put back. Without `--now` the rollback takes effect at the next
boot; with `--now` it takes effect through a `systemd-sysext refresh` (the
warning lists other sysexts that the refresh activates too; `--no-refresh`
skips it). `--dry-run` previews and needs no root.

The record is `/var/lib/updex/rollback/<feature>` (JSON, one rejected version
per transfer). While it exists, `updex features update` and the timer link
nothing at or below the rejected version and download nothing for that
feature: the run reports `held by rollback: rejected <version>` and exits 0.
When a newer version is published, the next update downloads and links it
(all transfers of the feature together) and deletes the record. A feature is
held as a whole: if one transfer has a newer version and another does not,
neither moves. `updex features rollback --clear <name>` deletes the record
without changing a link (a missing record is not an error); the next update
then links the newest version again. If the record cannot be read (malformed,
not a regular file), updates link nothing for that feature and report an
error until you fix or clear it. The vacuum keeps the rejected version while
the record exists. `features check` shows `held (rejected <version>)`, or
`held (by rollback of <feature>: …)` when another component of the record is
the reason, in the `UPDATE` column (JSON `rollback_rejected` and
`rollback_held_by`), and `features list` ends with a `HELD BY ROLLBACK:` line.

A transfer can belong to more than one feature (`Features=` or
`RequisiteFeatures=` in its `.transfer` file). The hold covers **every**
feature that shares a transfer it acts on (an active transfer, see above):
`features update`, `features enable --now`, `features check` and
`features list` read the rollback records of all of them, whether or not a
feature is enabled. A record that applies only to a transfer that is not active
for a feature does not hold that feature, and a rollback records only the
feature's active transfers (it keeps an earlier rejection of a transfer that is
inactive at that time). When several records reject the same
transfer, the highest rejected version applies. If one of those records cannot
be read, updates link nothing for every feature that shares the transfer and
name the feature whose record failed.

While a rollback record of feature G exists, only G advances the transfers its
record names: G's transfers link all or none, and G's record is deleted only
after that link. Any other feature that shares such a transfer is held as a
whole instead: nothing is downloaded or linked for it, `features update` exits
0, and the message says `held by rollback of grp: it advances only when grp
updates (or run 'updex features rollback --clear grp')`. So the all-or-none
guarantee holds in every processing order, and after a download or link failure
in G: an image that G's failed run staged is never linked by the other
feature. The hold lasts while the record exists, also when G is disabled or
masked; updex never clears the record by itself. `features rollback --clear G`,
or a successful update of G, ends it. If two features that share a transfer
both have a rollback record, each is held by the other's record; clear one of
them so the other can advance. `features check` shows it as
`rollback_held_by`.

G's own update resumes when every component its record names, including one
that G's current transfers do not use, has a version newer than the rejected
one. A component the record names whose transfer is not in the loaded
definitions (for example with `-C` or a narrowed `--component`) holds too,
because updex cannot tell whether a newer version exists.

A publisher that bumps only one component of a multi-transfer feature keeps
the hold, because every transfer must have a version newer than its rejected
one. A rollback across an image update can need the older OS image too: an
older sysext revision may expect a file that a newer base image dropped (the
newer revision then carried it). Roll back the image as well in that case.

### Minimum image version

A sysext revision that needs a newer base image
can set `UPDEX_MIN_IMAGE_VERSION=<digits>` in its
`usr/lib/extension-release.d/extension-release.<name>` (snosi sets it by hand,
only on a revision that depends on a base change; `systemd-sysext` ignores the
key). After the download and before the link, `features update` (and
`features enable --now`) reads the key from the downloaded image, without
mounting it, and compares it with the `IMAGE_VERSION` of the image that boots
next: the rollback deployment when `bootc status --json` reports a queued
rollback (`rollbackQueued`), else the staged deployment, else the booted
`/etc/os-release`. Both values must be digits only (snosi uses
14-digit timestamps) and are compared as numbers. When the host image is older
than the minimum, or either value is missing or not a number, updex links
**nothing** of that feature (its transfers link together), keeps the old links
and the downloaded images, and reports `needs image >= <value>`. This is not an
error: other features continue and the run exits 0. The image stays staged, so
the next run checks it again and links it once the host image is new enough
(after the host stages or boots a newer image). The update table shows
`not linked: needs image >= <value>`, and the JSON result has `needs_image`
(and `not_linked` on the feature's other transfers). `features check` shows
the same for a staged image that is not linked: `needs image >= <value>` in the
`UPDATE` column (JSON `needs_image`, `next_boot_image_version`) and a
`NEEDS NEWER IMAGE:` line; `features list` ends with the same line. They never
download, so they cannot tell about a newer image that is only published.
Without the key nothing changes. An image with no EROFS filesystem at all (for
example a squashfs sysext from another catalog) is treated as having no key.
An uncompressed EROFS image is read in pure Go. A compressed one is read with
`dump.erofs` from erofs-utils >= 1.8.5, which prints the file out of the image;
a host without that fails closed for such an image instead of skipping the
check (see [erofs-utils for compressed sysext images](../design/packaging-and-maintainers.md)).

### The link and an immediate refresh are checked against different images

The link is what the next boot merges, so it is checked against the next-boot
image. A `systemd-sysext refresh` merges into the image the host runs now: on a
bootc host `/etc/os-release` and `/usr/lib/os-release` describe the booted
deployment, not a staged one. Before every refresh it runs (`features update`
without `--no-refresh`, `features enable --now`, `features rollback --now`),
updex therefore compares the key of every sysext the refresh would merge with
the running image's `IMAGE_VERSION` too. It checks every link whose merged
version it cannot prove equal to the linked version, including a merged image
whose version the metadata does not record (`unknown`). A merged state or link
that cannot be read stops the refresh as well: updex fails closed. When that
check fails, including when
the running version is missing or not numeric, updex **keeps the links** (they
are right for the next boot), **skips the refresh**, and says so:
`refresh skipped: <component> needs image >= <value> (running image <running>);
it takes effect at the next boot` (JSON `refresh_skipped`). This is not an
error: the command exits 0 and the change takes effect at the next boot. An
image with no EROFS filesystem is treated as having no key here as well.
`features disable --now` is the exception: it unmerges before its refresh, so
skipping the refresh would leave every sysext unmerged; it refuses the whole
command instead, changes nothing, and tells you to run it without `--now` and
reboot. The timer runs with `--no-refresh` and is unchanged.
An image that has an EROFS filesystem but whose extension-release file cannot
be read (a failed EROFS open, a failed open, stat or read of the file, a
missing file, several without a match, over 64 KiB, or a compressed image
whose `dump.erofs` fallback is missing, too old or fails), an image the link
step could not resolve, or a `bootc` that is installed but fails, is an error
for the features concerned: nothing is linked for them. A queued rollback or a
staged deployment whose version `bootc` does not report is an error too: bootc
reads that version from the OCI manifest annotation
`org.opencontainers.image.version` of the OS image, so an OS image without it
blocks these features while an update is staged, until the reboot into it.

### What the vacuum keeps

After the links succeeded, `features update`
removes old images but keeps, per transfer, the linked version, the merged
version and `ProtectVersion=` as well as the newest `InstancesMax` versions; a
host that linked a new revision but has not rebooted keeps up to 3 images per
transfer. For a transfer that selects its suite with `%w` (os-release
`VERSION_ID`), the images of other suites (for example the trixie images on a
host that switched to forky) keep only the newest version of each other suite,
so a switch back can link it without a download; the image the link points to
and a merged image are always kept. A real run prints
`Removed other-suite image <file> of <component>` for each one it removes; a
dry run adds `; would remove other-suite images: [...]` to the message (JSON
`removed_other_suite_images`). When the merged state names the sysext without
its image file (systemd older than 258 records only the extension name), the
merged image could be any of them, so every other-suite image stays and a real
run prints `Kept every other-suite image of <component>: the merged state names
it without its image file`. `features disable --now` removes the images of
every suite, so it also needs `--force` when an image of another suite is
active (`Extension <component> (image <file> of another suite) is active`). See
[Retention and active versions](config-reference.md#retention-and-active-versions) for the full rule.

### One updex operation at a time

Every command that changes state
(`features update`, `features enable`, `features disable`, `catalog add`,
`catalog remove`, `daemon enable`, `daemon disable`) takes an exclusive lock on
`/run/updex/lock` and never waits for it. Dry runs and read-only commands
(`features list`, `features check`, `components`, `catalog list`,
`daemon status`) do not take it. `features rollback` takes it too. When another updex operation holds the lock:

- in an interactive shell the command prints
  `another updex operation is running` and exits non-zero;
- `updex features update` run by systemd (the environment variable
  `INVOCATION_ID` is set, as for the timer's service) logs one line to stderr
  (`skipping this run`) and exits 0, because the next scheduled run tries
  again. Every other command still fails.

### An offline host is a skip, not a failure

When no update source can be
reached at all (a DNS failure, a refused or impossible connection, no HTTP
response of any status), `updex features update` run by systemd prints its
normal output and warnings, adds the line
`updex: no update source is reachable; skipping this run` and exits 0, so `updex-update.service` does not stay `failed` until the next
successful run ([ADR-0018](../adr/0018-skip-an-update-run-when-every-source-is-unreachable.md)).
A partial failure, a source that answered with an error status, a
verification failure and a failed refresh still exit non-zero, and a run in
an interactive shell still reports the error. `--json` output is unchanged.

### Catalogs

```bash
updex catalog list                        # Browse configured catalogs
updex catalog search zoxide               # Search by name
sudo updex catalog add fedora/zoxide      # Install, enable, and download
sudo updex catalog remove fedora/zoxide   # Remove generated files and images
```

Use `REPO/NAME` to disambiguate; a bare `NAME` works when unambiguous.
`updex catalog list --no-cache` forces a live listing. Catalogs must be
configured first: updex ships no built-in catalogs, because
they are specific to the host system. A repo is defined by a `<name>.catalog`
file under `/etc/updex/catalogs.d/`, `/run/updex/catalogs.d/`,
`/usr/local/lib/updex/catalogs.d/` or `/usr/lib/updex/catalogs.d/`, earlier
paths winning per filename; the [configuration reference](config-reference.md#catalog-files-catalog)
lists every key. After adding a sysext, ordinary `features` commands and the
daemon manage it; the [SDK API reference](sdk-api.md#cataloglist--catalogadd--catalogremove)
covers ownership and rollback.

### Unsigned catalogs

By default every catalog sysext must have a GPG-signed `SHA256SUMS`, whatever
`Verify=` the catalog's own `.conf` declares. Some catalogs, including the
[Fedora and community catalogs](https://fedora-sysexts.github.io/), publish
`SHA256SUMS` without a `SHA256SUMS.gpg` and set `Verify=false`. Adding one of
their sysexts fails with an error suggesting `AllowUnsigned=yes`.

To accept such a catalog, opt in for that catalog only, in its `[Catalog]`
section:

```ini
# /etc/updex/catalogs.d/fedora.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/fedora
AllowUnsigned=yes
```

`AllowUnsigned=yes` lets the catalog's own `Verify=` decide; `--verify` still
forces a signature. updex never falls back to an unsigned download after a
signature turns out to be missing or invalid. The policy is read from the
local `.catalog` file every time a sysext is checked or updated, so removing
`AllowUnsigned` requires signatures again on the next run.
`AllowUnsigned=yes` cannot be combined with `AllowInsecure=yes`: without
signatures, HTTPS is the only check on where an image came from. The policy, the protection that remains and the upgrade
notes for sysexts added with v2.0.0 to v2.0.2 are in
[signature policy for catalog sysexts](config-reference.md#signature-policy-for-catalog-sysexts).

### Automatic updates

```bash
sudo updex daemon enable             # Enable and start the update timer
updex daemon status                  # Timer state, next run, last update
sudo updex --dry-run daemon enable   # Preview what enable would do
sudo updex daemon disable            # Stop and disable it
```

The `frostyard-updex` package ships `updex-update.timer` and
`updex-update.service` in `/usr/lib/systemd/system`, **disabled**. An OS image
can enable the timer with a preset; otherwise `updex daemon enable` runs
`systemctl enable` and `start` on it and writes nothing. When the package's
units are absent (updex installed without the package), `daemon enable`
writes the units to `/etc/systemd/system` instead, with a marker comment
on the first line, and `daemon disable` removes only files that carry it (and
the identical units that older updex releases wrote).

- **Schedule:** every 6 hours (`OnCalendar=*-*-* 00/6:00:00`) with up to one
  hour of random delay; a run missed while the host was off happens at the next
  boot (`Persistent=true`). An administrator can change it with a drop-in such
  as `/etc/systemd/system/updex-update.timer.d/override.conf`, and
  `updex daemon status` shows the schedule that actually applies, drop-ins
  included.
- **What a run does:** `updex features update --no-refresh` downloads, verifies
  and links the new revision. It never runs `systemd-sysext refresh`, so the
  new revision takes effect at the next boot. A run that finds nothing new
  fetches only the manifests.
- **Power and network:** the service waits for `network-online.target` and
  skips a run while the host is on battery (`ConditionACPower=true`; a host
  without a battery always runs). updex does not detect metered networks.
  A host that is offline still reaches `network-online.target` on many
  setups; its downloads then fail, but a run that no source answered is a
  skip: after its usual warnings it logs one skip line and exits 0
  (`updex-update.service` stays successful). A partial failure, or a source that answered with an error,
  still fails the unit ([ADR-0018](../adr/0018-skip-an-update-run-when-every-source-is-unreachable.md)).
- **Time limit:** the service has `TimeoutStartSec=2h`. systemd stops a stuck
  run after 2 hours, which releases the operation lock `/run/updex/lock`.
- **Lock:** `updex daemon enable` and `updex daemon disable` take the same
  operation lock as the other mutating commands, also when they only enable or
  disable the packaged timer.
- **Status:** `updex daemon status` shows the unit files in use (`packaged`, or
  `etc` when a file in `/etc/systemd/system` overrides the package), whether the
  timer is masked (`Masked: yes`), the next run and the time and result of the
  last run since boot. It also reads the persistent last-update record, even
  without a timer: the finished time, run outcome, refresh state, and each
  feature that did not succeed, with its error class and version when known.
  This distinguishes a busy or unreachable run from systemd's successful exit
  and keeps the update result visible after a reboot. `--json` includes
  `last_update`, or `last_update_error` when the record cannot be read. A
  missing or unknown-format record means no record; an invalid record does
  not fail status. The run log is `journalctl -u updex-update.service`.
- **Last-run record:** every `updex features update` that is not a dry run
  writes `/var/lib/updex/last-run.json`: the run outcome (`completed`,
  `failed`, `skipped_busy`, `skipped_unreachable`), the refresh state, and per
  feature its origin, outcome (`updated`, `current`, `held_by_rollback`,
  `needs_image`, `failed`, ...), error class (`network`, `http_status`,
  `signature`, `checksum`, `disk_space`, `link`, `config`, `other`) and
  version. It never contains error text. Health tools such as snosi's
  collector read it instead of the journal; the format is
  [last-run record](last-run-record.md). A failure to
  write it is a warning and never fails the update.
- **Opt out:** `sudo systemctl mask updex-update.timer` stops automatic updates
  on the whole host and survives image updates. A mask is a symlink to
  `/dev/null`; `updex daemon disable` stops and disables the timer and removes
  the unit files updex wrote, but it never removes a mask. It reports the mask
  (`masked_files` in JSON) and the command that undoes it
  (`sudo systemctl unmask updex-update.timer`). Stopping the timer alone does
  not survive an image update: an image preset can enable it again. To stop one
  sysext, run `updex features disable <name>`.

### Global flags

| Flag                | Description                                               |
| ------------------- | --------------------------------------------------------- |
| `-C, --definitions` | Path to directory containing .transfer and .feature files |
| `--verify`          | Force GPG signature verification on SHA256SUMS            |
| `--no-refresh`      | Skip running systemd-sysext refresh after install/update  |
| `--json`            | Output in JSON format (jq-compatible)                     |
| `-n, --dry-run`     | Preview changes without modifying filesystem              |
| `-v, --verbose`     | Enable verbose output                                     |
| `-s, --silent`      | Suppress progress/reporting noise; with `--json`, still emit the final machine-readable result |

`--verify` only forces verification: without it, signatures are still
verified unless a transfer explicitly sets `Verify=no` (for catalog-added
sysexts, only when the catalog also sets `AllowUnsigned=yes`; see
[Unsigned catalogs](#unsigned-catalogs)). CLI dry runs of
mutating feature commands still require root.

### JSON output

With `--json`, stdout carries only the command's result, as one JSON
document. Progress and warning events are JSON Lines on stderr, for example
`{"type":"message","message":"Installed version 2.0.0",...}`, so a consumer
can read stderr for live progress and still parse stdout with one
`json.Unmarshal`. A command that fails before it has a result (for example a
missing root privilege, or `daemon disable` with no timer installed) can print
nothing on stdout; it exits non-zero and writes the error to stderr. Check the
exit status before you parse stdout. The interactive download bar is off in JSON mode, and in
text mode it appears only when stderr is a terminal (under systemd the journal
would otherwise receive raw carriage-return text).

`features update --json` reports `linked: true` for a transfer whose link this
run moved, including one that needed no download because its image was already
staged; the text table shows `linked` in its `STATUS` column for that row, not
`up to date`.

Use `updex features list --json | jq '.[] | select(.enabled)'` to list enabled
features for a script.

## Rules

- Every command that changes state takes the exclusive operation lock on
  `/run/updex/lock` and never waits for it; read-only commands and dry runs
  never take it.
- With `--json`, stdout carries the result alone and progress and warnings are
  JSON Lines on stderr; a command that fails before it has a result prints
  nothing on stdout and exits non-zero.
- `--component` cannot be combined with `-C, --definitions`: a definitions
  directory is one flat directory with no components. For the same reason,
  `updex components` and every `catalog` command fail when `-C, --definitions`
  is set, instead of reading the host's search roots.
- Run by systemd (`INVOCATION_ID` set), `features update` treats a busy lock
  and an unreachable source as skips that exit 0; every other command, and an
  interactive shell, treats them as errors.
- The global flags table above is pinned to `updex --help` by
  `TestE2E_GlobalFlagsAreDocumentedInCLIReference` in `tests/e2e/e2e_test.go`:
  a global flag with no row fails that test.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| `tests/e2e/e2e_test.go` (`TestE2E_GlobalFlagsAreDocumentedInCLIReference`) | Reads the [global flags](#global-flags) table and fails when `updex --help` lists a global flag with no row. |
| `Client.catalogRepos` error text (`updex/catalog.go`) | Points the reader at the [catalogs](#catalogs) section. |

## References

- Rationale: [ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md),
  [ADR-0017](../adr/0017-apply-the-feature-activation-rule-in-every-feature-path.md),
  [ADR-0018](../adr/0018-skip-an-update-run-when-every-source-is-unreachable.md),
  [ADR-0020](../adr/0020-write-a-machine-readable-last-run-record.md)
- Context: [configuration reference](config-reference.md),
  [design overview](../design/overview.md),
  [packaging and maintainer guide](../design/packaging-and-maintainers.md)
