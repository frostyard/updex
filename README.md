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

With [Go 1.26.7](https://go.dev/doc/install) and `make`, run `make build` to
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
sudo updex features disable docker        # Stop future updates
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
sudo updex daemon enable   # Install and start the daily systemd timer
updex daemon status        # Inspect the timer
sudo updex daemon disable  # Stop and remove it
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
verified unless a transfer explicitly sets `Verify=no`. CLI dry runs of
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
