<!-- readme-kind: tool -->
# updex

updex is a Go SDK and CLI for managing
[systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html)
images. It provides the `url-file` transfer functionality of
`systemd-sysupdate` on systems such as Debian Trixie that do not ship that
tool. Use the CLI to manage extensions, or import
`github.com/frostyard/updex/v2/updex` from Go.

[![Tests](https://github.com/frostyard/updex/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/frostyard/updex/actions/workflows/ci.yml?query=branch%3Amain)
[![codecov](https://codecov.io/gh/frostyard/updex/graph/badge.svg?branch=main)](https://codecov.io/gh/frostyard/updex)

**Status:** stable. The tested host is Debian Trixie, and the v2 series gets the fixes that `SECURITY.md` names.

**Documentation:** the [user guide on frostyard.org](https://frostyard.org/docs/tools/updex/); for maintainers, [docs/README.md](docs/README.md), with the [CLI reference](docs/specs/cli-reference.md) for every command, flag and message.

## What it does

- **Features group transfers.** Enable, disable, check and update groups of
  sysext images described by standard `.feature` and `.transfer` files.
  [Features and components](docs/specs/cli-reference.md#features-and-components)
- **Components.** Discover named systemd-sysupdate components alongside the
  legacy default directory.
  [Components](docs/specs/config-reference.md#components)
- **Catalogs.** Browse and install extensions from a configured sysext
  catalog, and updex owns every file it generates.
  [Catalogs](docs/specs/cli-reference.md#catalogs)
- **Checked downloads.** Verify SHA256 hashes and, by default, a GPG signature
  on `SHA256SUMS`; retry transient failures and decompress xz, gz and zstd
  images. [Remote manifest format](docs/specs/config-reference.md#remote-manifest-format)
- **Rollback and holds.** Link an older staged version and hold updates until
  a newer one is published.
  [Rolling back](docs/specs/cli-reference.md#rolling-back)
- **Revisions that need a newer base.** A revision that sets
  `UPDEX_MIN_IMAGE_VERSION` stays staged until the image for the next boot is
  new enough. [Minimum image version](docs/specs/cli-reference.md#minimum-image-version)
- **Automatic updates.** A systemd timer downloads, verifies and links new
  revisions, which take effect at the next boot.
  [Automatic updates](docs/specs/cli-reference.md#automatic-updates)
- **JSON output.** One JSON document per command, with progress on stderr, for
  scripts. [JSON output](docs/specs/cli-reference.md#json-output)

## Install

Download the `.deb` and `checksums.txt` from the
[latest release](https://github.com/frostyard/updex/releases/latest), then
verify and install the package:

```sh
sha256sum --ignore-missing --check checksums.txt
gh attestation verify <downloaded-artifact> --repo frostyard/updex
sudo apt install ./frostyard-updex_<version>_amd64.deb
```

The checksum detects corruption or truncation. The build-provenance
attestation binds the artifact to a tag release built by this repository's
workflow, so a checksum served beside the artifact is not the only evidence;
it needs the [GitHub CLI](https://cli.github.com/). The
[CLI reference](docs/specs/cli-reference.md#install) lists the rpm, apk and
tarball artifacts and the build-from-source steps.

## Use

```sh
updex features list
sudo updex features enable docker --now
updex features check
sudo updex daemon enable
```

[Features and components](docs/specs/cli-reference.md#features-and-components)
explains what each command reports. `updex components` lists the discovered
components, and `updex catalog list` shows what the configured catalogs offer.

## Requirements

updex needs a systemd-based Linux system with `systemd-sysext`, and commands
that change system state need root. A **compressed** EROFS sysext also needs
`erofs-utils >= 1.8.5` on the host, because updex reads the image's
`extension-release` file with `dump.erofs` without mounting it; all frostyard
sysexts are uncompressed. `systemd-sysext.service` must be enabled for
sysexts to merge at boot, and the state directories `/var/lib/extensions` and
`/var/lib/extensions.d` must exist; a packager or an image build does both. See
the [packaging and maintainer guide](docs/design/packaging-and-maintainers.md).

## Develop

```sh
mise install   # the pinned tools
make ci        # what CI runs
```

The "Getting started" section of [AGENTS.md](AGENTS.md) has the prerequisites,
the build and test targets, and the rest of the gate. `make verify` runs the
same static checks without writing to the tree, for a read-only review.

## Where to go next

- [AGENTS.md](AGENTS.md): the rules for agents and contributors.
- [docs/README.md](docs/README.md): the decisions, designs, specs and plans.
- [CLI reference](docs/specs/cli-reference.md): every command, flag and message.
- [Configuration reference](docs/specs/config-reference.md): `.transfer`, `.feature` and `.catalog` files.
- [SDK API reference](docs/specs/sdk-api.md): use updex as a Go library.
- [SECURITY.md](SECURITY.md): report a vulnerability privately.
- [LICENSE](LICENSE): MIT.
