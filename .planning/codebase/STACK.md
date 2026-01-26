# Technology Stack

**Analysis Date:** 2026-01-26

## Languages

**Primary:**
- Go 1.25.6 - All application code

**Secondary:**
- Bash - Build scripts (`scripts/completions.sh`, `scripts/manpages.sh`)
- YAML - CI/CD configuration (`.github/workflows/*.yml`, `.goreleaser.yaml`)

## Runtime

**Environment:**
- Go 1.25.6 (specified in `go.mod` line 3)
- Linux target (single platform, as this tool manages systemd-sysext)

**Package Manager:**
- Go modules
- Lockfile: `go.sum` (present, 104 lines)

## Frameworks

**Core:**
- [spf13/cobra](https://github.com/spf13/cobra) v1.10.2 - CLI command framework
- [charmbracelet/fang](https://github.com/charmbracelet/fang) v0.4.4 - Enhanced Cobra execution with signal handling

**Build/Dev:**
- [goreleaser-pro](https://goreleaser.com/) v2 - Release automation (builds, packages, publishes)
- GNU Make - Local development tasks (`Makefile`)
- [svu](https://github.com/caarlos0/svu) - Semantic version utility for releases

## Key Dependencies

**Critical (Direct):**
- `github.com/spf13/cobra` v1.10.2 - CLI structure and argument parsing
- `github.com/charmbracelet/fang` v0.4.4 - Command execution wrapper with signal handling
- `github.com/hashicorp/go-version` v1.8.0 - Semantic version parsing and comparison
- `gopkg.in/ini.v1` v1.67.1 - INI file parsing for `.transfer` and `.feature` configs
- `golang.org/x/crypto` v0.47.0 - GPG signature verification (openpgp package)

**Compression Libraries:**
- `github.com/klauspost/compress` v1.18.2 - zstd decompression
- `github.com/ulikunitz/xz` v0.5.15 - xz decompression

**Progress/UI:**
- `github.com/schollz/progressbar/v3` v3.19.0 - Download progress bars
- `github.com/frostyard/pm/progress` v0.2.1 - Progress reporting abstraction
- `charm.land/lipgloss/v2` v2.0.0-beta.3 - Terminal styling (indirect)

**Documentation Generation:**
- `github.com/muesli/mango-cobra` v1.2.0 - Man page generation from Cobra commands

## Configuration

**Environment:**
- No `.env` files used
- Build-time configuration via ldflags:
  - `main.version` - Version string
  - `main.commit` - Git commit hash
  - `main.date` - Build date
  - `main.builtBy` - Build system identifier
- Runtime configuration via `.transfer` and `.feature` INI files

**Key Config Files:**
- `go.mod` - Go module definition and dependencies
- `go.sum` - Dependency checksums
- `.goreleaser.yaml` - Release build configuration
- `Makefile` - Development commands
- `.svu.yaml` - Semantic versioning tool config

**Build Configuration:**
- `CGO_ENABLED=0` - Static binary builds
- `-trimpath` flag for reproducible builds
- ldflags for stripping debug info (`-s -w`)

## Platform Requirements

**Development:**
- Go 1.25+ toolchain
- Make (optional, for convenience commands)
- golangci-lint (optional, for linting)

**Production:**
- Linux only (systemd-sysext is Linux-specific)
- amd64 architecture (single target in release config)
- systemd with sysext support (for full functionality)

**Build Targets:**
- Architecture: amd64 only (production), amd64+arm64 (CI tests)
- OS: Linux only
- Package formats: tar.gz, deb, rpm, apk

---

*Stack analysis: 2026-01-26*
