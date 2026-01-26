# External Integrations

**Analysis Date:** 2026-01-26

## APIs & External Services

**HTTP File Downloads:**
- Purpose: Download sysext images and SHA256SUMS manifests from remote HTTP sources
- Implementation: `internal/download/download.go`
- Client: Standard `net/http` with 10-minute timeout for large files
- Auth: None (public URLs assumed)

**systemd-sysext:**
- Purpose: System extension management on Linux
- Integration: CLI calls via `exec.Command`
- Commands used: `refresh`, `merge`, `unmerge`
- Implementation: `internal/sysext/manager.go` (lines 405-428)

## Data Storage

**Databases:**
- None - All state is stored on filesystem

**File Storage:**
- Local filesystem only
- Default sysext path: `/var/lib/extensions`
- Config search paths (priority order):
  - `/etc/sysupdate.d/*.transfer`
  - `/run/sysupdate.d/*.transfer`
  - `/usr/local/lib/sysupdate.d/*.transfer`
  - `/usr/lib/sysupdate.d/*.transfer`

**Configuration Files:**
- Format: INI (systemd-style)
- Parser: `gopkg.in/ini.v1`
- Implementation: `internal/config/transfer.go`, `internal/config/feature.go`

**Caching:**
- None - Manifests are fetched fresh each operation

## Authentication & Identity

**Auth Provider:**
- None - Tool runs as local user/root

**GPG Signature Verification:**
- Optional verification of SHA256SUMS.gpg signatures
- Implementation: `internal/manifest/gpg.go`
- Keyring locations (systemd-compatible):
  - `/etc/systemd/import-pubring.gpg`
  - `/usr/lib/systemd/import-pubring.gpg`
- Library: `golang.org/x/crypto/openpgp` (deprecated but functional)

## Monitoring & Observability

**Error Tracking:**
- None (local CLI tool)

**Logs:**
- Stdout/stderr only
- No structured logging framework
- Progress bars via progressbar/v3

## CI/CD & Deployment

**Hosting:**
- GitHub (source code)
- Cloudflare R2 (package repository via frostyard/repogen)

**CI Pipeline:**
- GitHub Actions
- Workflows: `.github/workflows/`
  - `test.yml` - Lint, security scan, unit tests, race detection, build verification
  - `release.yml` - GoReleaser-based releases
  - `snapshot.yml` - Nightly/snapshot builds

**Release Process:**
1. Tag pushed to GitHub
2. GoReleaser Pro builds binaries
3. Creates deb/rpm/apk packages
4. Publishes to GitHub releases
5. Publishes to frostyard repository (Cloudflare R2)

**Package Distribution:**
- GitHub Releases (tar.gz archives)
- frostyard repo (deb, rpm, apk via repogen action)
- Repository URL: https://repository.frostyard.org

## Environment Configuration

**Required env vars (CI only):**
- `GITHUB_TOKEN` - GitHub API access (releases)
- `GORELEASER_KEY` - GoReleaser Pro license
- `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` - Cloudflare R2 storage
- `CLOUDFLARE_ZONE`, `CLOUDFLARE_API_TOKEN` - Cache purging
- `REPOGEN_GPG_KEY` - Package signing

**Runtime env vars:**
- None required for CLI operation

**Secrets location:**
- GitHub Actions secrets (CI only)
- No local secrets management

## Webhooks & Callbacks

**Incoming:**
- None

**Outgoing:**
- None

## Remote Manifest Format

**SHA256SUMS:**
- Standard sha256sum output format
- Location: `{base_url}/SHA256SUMS`
- Optional signature: `{base_url}/SHA256SUMS.gpg`
- Implementation: `internal/manifest/manifest.go`

**Extension Repository Index:**
- Plain text file listing extension names
- Location: `{base_url}/ext/index`
- One extension name per line

## Compression Support

**Supported Formats:**
- xz (via `github.com/ulikunitz/xz`)
- gzip (via stdlib `compress/gzip`)
- zstd (via `github.com/klauspost/compress/zstd`)

**Detection:**
- By file extension in URL
- Implementation: `internal/download/download.go` (lines 127-139)

---

*Integration audit: 2026-01-26*
