# updex

updex is a Go SDK and CLI for managing [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html) images. It provides the `url-file` transfer functionality of `systemd-sysupdate` on systems such as Debian Trixie that do not ship that tool. Use the CLI to manage extensions or import `github.com/frostyard/updex/v2/updex` from Go.

[![Tests](https://github.com/frostyard/updex/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/frostyard/updex/actions/workflows/test.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/frostyard/updex/graph/badge.svg?branch=main)](https://codecov.io/gh/frostyard/updex)

## Features

- Enable, disable, check, and update groups of sysext images using standard `.feature` and `.transfer` files.
- Discover named systemd-sysupdate components alongside the legacy default directory.
- Browse and install extensions from configured sysext catalogs.
- Verify SHA256 hashes and, by default, GPG signatures on `SHA256SUMS`; retry transient network failures and decompress xz, gz, or zstd images.
- Retain configured versions and stage automatic updates through a systemd timer.
- Produce JSON output for scripts.

## Installation

Download the latest CLI package for your system from the
[GitHub releases page](https://github.com/frostyard/updex/releases/latest):

| System                    | Release artifact                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------- |
| Debian/Ubuntu             | `frostyard-updex_<version>_amd64.deb` or `frostyard-updex_<version>_arm64.deb`        |
| Fedora/RHEL               | `frostyard-updex-<version>-1.x86_64.rpm` or `frostyard-updex-<version>-1.aarch64.rpm` |
| Alpine                    | `frostyard-updex_<version>_x86_64.apk` or `frostyard-updex_<version>_aarch64.apk`     |
| Other Linux distributions | `updex_<version>_linux_amd64.tar.gz` or `updex_<version>_linux_arm64.tar.gz`          |

Download `checksums.txt` from the same release and verify the downloaded
artifact before installing it:

```bash
sha256sum --ignore-missing --check checksums.txt
gh attestation verify <downloaded-artifact> --repo frostyard/updex
```

The checksum detects corruption or truncation; the GitHub build-provenance
attestation binds the artifact to a tag release built by this repository's
workflow, rather than trusting only a checksum served alongside it. The
[GitHub CLI](https://cli.github.com/) is needed for attestation verification;
`gh attestation verify` also works on `checksums.txt`.

The packaged CLI needs no Go toolchain. It needs a systemd-based Linux system
with `systemd-sysext`; commands that modify system state require root.

### Build from source

With [Go 1.26.10](https://go.dev/doc/install) and `make`, run `make build` to
create `build/updex`, or `make install` to put the binary in `GOPATH/bin`.
Building and unit testing do not require systemd. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow and the
[SDK API reference](docs/specs/sdk-api.md) to use the Go library.

## Everyday CLI usage

Configured `.feature` and `.transfer` files are read from `/etc`, `/run`,
`/usr/local/lib`, and `/usr/lib` under `sysupdate.d/` and discovered
`sysupdate.<name>.d/` directories. Use `updex components` to see named
components. See the [configuration reference](docs/specs/config-reference.md)
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
sudo updex --dry-run features update      # Preview without modifying state
updex features check                      # Check for updates (read-only)
updex components                          # List discovered named components
updex features list --component=docker    # Inspect one named component
sudo updex features update --component=docker
```

Without `--component`, `features` commands use the union of the legacy default
directory and discovered named components. The `--component` flag applies to
every `features` subcommand and cannot be combined with `-C, --definitions`,
which instead reads one explicit directory and bypasses discovery.

**Refresh failure:** If the final `systemd-sysext refresh` fails after
`enable --now`, `disable --now`, or `update`, updex reports completed steps
and exits non-zero (`refresh_error` in JSON). Run `systemd-sysext refresh`
manually or reboot; after `disable --now`, extensions remain unmerged until
then.

**Check errors:** If a component's manifest cannot be fetched or verified,
`features check` reports `UPDATE=error` (JSON: `error`), continues reporting
healthy components, and exits non-zero. Do not interpret an errored row as
"up to date."

**What "current" means:** `features check` compares the newest available
version with the version the systemd-sysext link in `/var/lib/extensions`
points to, because that link is the image systemd-sysext merges. If no usable link
exists it falls back to the staged image. A link still on an older image
therefore shows an update, and `features update` repoints the link without
downloading again and reports that a reboot activates it. The JSON field
`current_version` keeps this meaning.

**Running and linked versions:** `features check` and `features list` show,
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

**Refresh warning:** `features update` (without `--no-refresh`),
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

**A feature's transfers update together:** `features update` (and
`features enable --now`) downloads every transfer of a feature first and links
them all only when every download succeeded. If one fails, the feature keeps
all its old links, the images already downloaded stay on disk for the next run,
and the table shows the failure on that transfer and
`not linked: another transfer of feature <name> failed` on the others. Other
features still update. If a link fails part-way, the links already moved are
put back.

**What the vacuum keeps:** after the links succeeded, `features update`
removes old images but always keeps, per transfer, the image the link names
(it merges at the next boot), the image merged now (the one the host runs),
`ProtectVersion=`, and the newest `InstancesMax` images (default 2). A host
that has linked a new revision but has not rebooted yet therefore keeps up to
3 images per transfer. If updex cannot read the link or the merged state, it
deletes nothing and warns.

**One updex operation at a time:** every command that changes state
(`features update`, `features enable`, `features disable`, `catalog add`,
`catalog remove`, `daemon enable`, `daemon disable`) takes an exclusive lock on
`/run/updex/lock` and never waits for it. Dry runs and read-only commands
(`features list`, `features check`, `components`, `catalog list`,
`daemon status`) do not take it. When another updex operation holds the lock:

- in an interactive shell the command prints
  `another updex operation is running` and exits non-zero;
- `updex features update` run by systemd (the environment variable
  `INVOCATION_ID` is set, as for the timer's service) logs one line to stderr
  (`skipping this run`) and exits 0, because the next scheduled run tries
  again. Every other command still fails.

### Catalogs

```bash
updex catalog list                        # Browse configured catalogs
updex catalog search zoxide               # Search by name
sudo updex catalog add fedora/zoxide      # Install, enable, and download
sudo updex catalog remove fedora/zoxide   # Remove generated files and images
```

Catalogs must be configured first (see [Sysext Catalogs](#sysext-catalogs)).
Use `REPO/NAME` to disambiguate; a bare `NAME` works when unambiguous.
`updex catalog list --no-cache` forces a live listing.

### Automatic updates

```bash
sudo updex daemon enable             # Install and start the daily systemd timer
updex daemon status                  # Inspect the timer
sudo updex --dry-run daemon enable   # Preview the timer install
sudo updex daemon disable            # Stop and remove it
```

The timer downloads and stages updates without activating them; they become
active after a later refresh or reboot.

### Global Flags

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

## Sysext Catalogs

updex ships no built-in catalogs: they are specific to the host system.
Configure a repo with a `<name>.catalog` file in `/etc/updex/catalogs.d/`,
`/run/updex/catalogs.d/`, `/usr/local/lib/updex/catalogs.d/`, or
`/usr/lib/updex/catalogs.d/` (earlier paths win). For example:

```ini
# /etc/updex/catalogs.d/fedora.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/fedora
ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/
```

`SiteURL` is required for artifacts; `ListURL` supplies `catalog list` and
`search`. After adding a sysext, ordinary `features` commands and the daemon
manage it. See the [configuration reference](docs/specs/config-reference.md#catalog-files-catalog)
for catalog keys and the [SDK API reference](docs/specs/sdk-api.md#cataloglist--catalogadd--catalogremove)
for ownership and rollback behavior.

### Unsigned catalogs

By default every catalog sysext must have a GPG-signed `SHA256SUMS`,
whatever `Verify=` the catalog's own `.conf` declares. Some catalogs,
including the [Fedora and community catalogs](https://fedora-sysexts.github.io/),
publish `SHA256SUMS` without a `SHA256SUMS.gpg` and set `Verify=false`.
Adding one of their sysexts fails with an error suggesting `AllowUnsigned=yes`.

To accept such a catalog, opt in for that catalog only:

```ini
# /etc/updex/catalogs.d/fedora.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/fedora
ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/
AllowUnsigned=yes
```

`AllowUnsigned=yes` lets the catalog's `Verify=` decide. A catalog sysext
that declares `Verify=yes`, or omits it, still requires a signature, and
`--verify` still forces one. updex never falls back to an unsigned download
after a signature turns out to be missing or invalid. The policy is read from
the local `.catalog` file every time a sysext is checked or updated, so
removing `AllowUnsigned` requires signatures again on the next run.

What still protects an unsigned catalog: HTTPS authenticates the server and
protects transport (redirects from HTTPS to HTTP are refused, and
`AllowUnsigned` does not relax the HTTPS requirement), and every image must
match its `SHA256SUMS` entry before it is installed. What is lost: an
unsigned checksum does not authenticate the publisher, so anyone able to
replace both the image and `SHA256SUMS` on the hosting infrastructure can
serve a different image.

`AllowUnsigned=yes` cannot be combined with `AllowInsecure=yes`: without
signatures, HTTPS is the only check on where an image came from.

**Upgrading from v2.0.0:** sysexts added with v2.0.0 kept the catalog's
`Verify=false` and stop updating with a signature error until their catalog
sets `AllowUnsigned=yes`. No re-add is needed unless the catalog's `SiteURL`
has changed since (for example from `http://` to `https://`): the opt-in
only covers sysexts whose transfer still fetches from the current `SiteURL`,
so re-run `sudo updex catalog add <repo>/<name>` for those.

**Upgrading from v2.0.1 or v2.0.2:** these releases dropped the catalog's
`Verify=` from generated transfers, so sysexts added with them keep requiring
a signature. Add `AllowUnsigned=yes`, then re-run
`sudo updex catalog add <repo>/<name>` for each affected sysext to regenerate
its transfer.

## JSON output

Use `updex features list --json | jq '.[] | select(.enabled)'` to list enabled
features for a script; the interactive download bar stays off stdout in JSON
mode.

## More documentation

- [Contributing and development](CONTRIBUTING.md)
- [Configuration reference](docs/specs/config-reference.md)
- [SDK API reference](docs/specs/sdk-api.md)
- [Documentation index](docs/README.md)
- [MIT License](LICENSE)
