# Security policy

updex manages systemd system extensions and runs as root. Please report
security problems privately, as described below.

## Supported versions

Only the latest release of the `v2` series receives security fixes. Today that
is `v2.2.x`; see the
[releases page](https://github.com/frostyard/updex/releases) for the current
one. Older releases do not receive fixes: update to the latest release.

The rolling `dev` prerelease is a nightly build. It is not supported.

## Report a vulnerability

Do not open a public issue.

Use GitHub private vulnerability reporting:
https://github.com/frostyard/updex/security. Click "Report a vulnerability",
or use the direct form
https://github.com/frostyard/updex/security/advisories/new.

If you cannot use GitHub, email the maintainer at
[bketelsen@gmail.com](mailto:bketelsen@gmail.com).

## What happens next

The maintainer confirms receipt of the report. The maintainer then fixes the
problem on `main`, releases a patch, and publishes a security advisory.

## Scope

updex runs as root and installs system extensions. These areas are the most
sensitive:

- Image download and SHA256 verification: `download/`.
- `SHA256SUMS` signature verification: `manifest/`.
- Catalog rendering and the signature policy: `catalog/` and `updex/list.go`.
- The root writes of links, units and definitions: `sysext/`, `systemd/` and
  `updex/`.

AI-assisted contributions must also follow the
[AI security policy](docs/security/SECURITY-AI.md).
