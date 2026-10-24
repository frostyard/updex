# Packaging & maintainer guide

Guidance for OS-image builders, distribution packagers, and maintainers who
ship `updex` inside a system image or package. It documents the runtime
prerequisites `updex` expects so that end users get a working experience out
of the box, and the post-install steps a package should perform on their
behalf.

`updex` is a CLI over `systemd-sysext`/`systemd-sysupdate`; it does not create
the directories or enable the services those subsystems require. When those
prerequisites are missing, commands such as `updex catalog` or
`updex features` appear broken to the user even though `updex` itself is
installed correctly. Package the prerequisites below alongside the binary.

## Runtime prerequisites

### 1. sysext state directories must exist

`systemd-sysext` merges extension images from `/var/lib/extensions`, and
`updex` stages downloaded versions under `/var/lib/extensions.d`. Both must
exist and be owned by root:

```bash
install -d -m 0755 -o 0 -g 0 /var/lib/extensions /var/lib/extensions.d
```

On SELinux-enforcing systems (Fedora and derivatives), relabel them so
`systemd-sysext` can read them:

```bash
if command -v selinuxenabled >/dev/null 2>&1 \
    && command -v restorecon >/dev/null 2>&1 \
    && selinuxenabled; then
    restorecon -RFv /var/lib/extensions /var/lib/extensions.d
fi
```

### 2. `systemd-sysext.service` must be enabled

Extensions are only merged at boot when the service is enabled:

```bash
systemctl enable systemd-sysext.service
```

Enable it in the image build (or in the package's post-install scriptlet) so
that sysexts installed by `updex` are activated on the next boot without
manual intervention.

### 3. Catalogs must be configured for `updex catalog` to work

`updex catalog list/search/add/remove` only functions when at least one
catalog is configured. `updex` ships **no** built-in catalogs — they are
system-specific — so `updex catalog list` returns nothing until you drop in a
`<name>.catalog` INI file. Catalogs are searched (earlier directories win per
filename) in:

- `/etc/updex/catalogs.d/`
- `/run/updex/catalogs.d/`
- `/usr/local/lib/updex/catalogs.d/`
- `/usr/lib/updex/catalogs.d/`

Package-shipped catalogs belong under `/usr/lib/updex/catalogs.d/` so that
administrators can override or mask them from `/etc/updex/catalogs.d/`. For
example, to ship the [fedora-sysexts](https://fedora-sysexts.github.io/)
catalogs:

```ini
# /usr/lib/updex/catalogs.d/fedora.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/fedora
ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/
```

```ini
# /usr/lib/updex/catalogs.d/community.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/community
ListURL=https://api.github.com/repos/fedora-sysexts/community/contents/
```

- `SiteURL` (required) — base URL the catalog serves artifacts from.
- `ListURL` (optional) — GitHub contents API endpoint used by
  `catalog list`/`search` to enumerate available sysexts (`add`/`remove` never
  use it). Users can set `GITHUB_TOKEN` to raise the `https://api.github.com`
  rate limit; custom origins and cross-origin redirects never receive it.
- `Component` (optional) — the systemd-sysupdate component the generated files
  are written under; defaults to `catalog-<name>`.

See the [Sysext Catalogs](../../README.md#sysext-catalogs) section of the
README and the [configuration reference](../specs/config-reference.md) for the
full `.catalog` file format.

### 4. `erofs-utils` >= 1.8.5 for compressed sysext images

`updex` reads a sysext's `extension-release` file without mounting the image.
An uncompressed EROFS image is read in pure Go and needs nothing else. A
compressed EROFS image (lz4, lz4hc, zstd, ...) is read with `dump.erofs` from
[erofs-utils](https://git.kernel.org/pub/scm/linux/kernel/git/xiang/erofs-utils.git),
which prints the file out of the image. The command line it uses needs
erofs-utils **1.8.5** (`--cat` arrived in 1.8.5; `--offset` and `--ls` in
1.8):

- the packaged `updex` for deb, rpm and apk declares the dependency
  (`erofs-utils (>= 1.8.5)`, `erofs-utils >= 1.8.5`, `erofs-utils>=1.8.5`
  respectively; see `.goreleaser.yaml`);
- a standalone binary from the release tarball needs erofs-utils >= 1.8.5 on
  the host;
- without it, an update of a compressed sysext fails closed: nothing is
  linked, and the error says `erofs-utils >= 1.8.5 is required to read
  compressed EROFS images; install erofs-utils`. Uncompressed sysexts are
  unaffected.

All frostyard sysexts are uncompressed today, so the dependency is a
correctness guarantee for third-party or future compressed images, not a
requirement for the common case.

## Recommended package post-install steps

A package (RPM `%post`, deb `postinst`, or an image build step) should perform
the prerequisites above so the tool works immediately:

```bash
# Ensure sysext state directories exist.
install -d -m 0755 -o 0 -g 0 /var/lib/extensions /var/lib/extensions.d

# Relabel for SELinux where applicable.
if command -v selinuxenabled >/dev/null 2>&1 \
    && command -v restorecon >/dev/null 2>&1 \
    && selinuxenabled; then
    restorecon -RFv /var/lib/extensions /var/lib/extensions.d
fi

# Merge sysexts at boot.
systemctl enable systemd-sysext.service
```

Ship any system-appropriate catalog files under
`/usr/lib/updex/catalogs.d/` as part of the package payload.

## Notes for maintainers

- The packaged CLI does **not** require Go or `make` at runtime. It requires a
  systemd-based Linux system with `systemd-sysext`; operations that modify
  system state need root.
- The package dependency on erofs-utils (>= 1.8.5) is what lets the minimum
  image version check read a compressed EROFS sysext. Do not drop it: without
  the tool such an image fails closed and cannot be updated.
- `updex` owns only the `00-updex.conf` feature drop-in and the
  `/var/lib/extensions` symlink for the sysexts it manages. Administrator
  drop-ins always override and always survive, so it is safe for a package to
  ship its own `.transfer`/`.feature` definitions or `*.feature.d` drop-ins.
- Catalog-generated files carry a
  `# Generated by updex catalog (repo: <name>)` ownership header; `updex`
  refuses to overwrite or remove definitions it did not generate. Hand-written
  or package-shipped definitions sharing a name are therefore never clobbered
  by `updex catalog`.
- To offer unattended updates, enable the staging daemon with
  `updex daemon enable` (it stages downloads daily with jitter but never
  activates them automatically; see
  [ADR-0007](../adr/0007-daemon-stages-never-activates.md)).
