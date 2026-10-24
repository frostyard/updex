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

## Auto-update units

The `frostyard-updex` package ships the auto-update timer and service
([ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md)):

- `/usr/lib/systemd/system/updex-update.timer`
- `/usr/lib/systemd/system/updex-update.service`

Both are mode 0644 and **disabled**: the package has no maintainer script and
runs no `systemctl`. The service runs
`/usr/bin/updex features update --no-refresh` as a sandboxed `oneshot`, after
`network-online.target`, only on AC power (`ConditionACPower=true`), with
`TimeoutStartSec=2h` as a backstop for a stuck run; the timer
fires every 6 hours (`OnCalendar=*-*-* 00/6:00:00`, `RandomizedDelaySec=3600s`,
`Persistent=true`).

How they are built: GoReleaser's `before.hooks` run `scripts/units.sh`, which
runs the hidden `updex-cli units --output-dir units` command. It renders
`updex.DaemonUnits()` with `systemd.GenerateTimer`/`GenerateService`, the code
that `updex daemon enable` uses, and `nfpms.contents` installs the two files.
Do not edit the unit text anywhere else. `updex/units_hook_contract_test.go`
fails when the packaged files and the generator differ, and CI runs
`systemd-analyze verify` on them and, in the nightly snapshot, `dpkg-deb -c` on
the `.deb` (`scripts/check-deb-units.sh`).

An image builder enables the timer itself, with a systemd preset line
(`enable updex-update.timer`) that `systemctl preset-all` applies. Without one,
the timer is off until an administrator runs `updex daemon enable`, which runs
`systemctl enable` and `start` on the packaged timer and writes no file. Only an
updex binary installed *without* the package gets marked unit files written to
`/etc/systemd/system`.

Administrators:

- `sudo systemctl mask updex-update.timer` stops automatic updates on the whole
  host, and survives image updates; use it for the durable opt-out.
- `updex daemon disable` stops and disables the timer and removes only the
  `/etc/systemd/system` units that updex wrote (they carry the first-line marker
  `# Generated by updex daemon enable; ...`, or equal the text that an earlier
  release wrote). It never deletes another file in `/etc` and never touches the
  packaged units. A host that has the old unit files in `/etc` keeps them until
  `daemon disable` runs; they override the packaged units.
- `updex daemon status` shows which units are in use, the next run and the last
  result; the run log is `journalctl -u updex-update.service`.

## Notes for maintainers

- The packaged CLI does **not** require Go or `make` at runtime. It requires a
  systemd-based Linux system with `systemd-sysext`; operations that modify
  system state need root.
- `updex` owns only the `00-updex.conf` feature drop-in and the
  `/var/lib/extensions` symlink for the sysexts it manages. Administrator
  drop-ins always override and always survive, so it is safe for a package to
  ship its own `.transfer`/`.feature` definitions or `*.feature.d` drop-ins.
- Catalog-generated files carry a
  `# Generated by updex catalog (repo: <name>)` ownership header; `updex`
  refuses to overwrite or remove definitions it did not generate. Hand-written
  or package-shipped definitions sharing a name are therefore never clobbered
  by `updex catalog`.
- To offer unattended updates, enable the packaged timer: with a preset in the
  image (see "Auto-update units") or with `updex daemon enable`. It downloads,
  verifies and links a new revision every 6 hours and never activates it; the
  revision takes effect at the next boot (see
  [ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md), which
  supersedes [ADR-0007](../adr/0007-daemon-stages-never-activates.md)).
