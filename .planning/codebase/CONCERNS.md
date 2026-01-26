# Codebase Concerns

**Analysis Date:** 2026-01-26

## Tech Debt

**Deprecated OpenPGP Package:**
- Issue: Using `golang.org/x/crypto/openpgp` which is deprecated
- Files: `internal/manifest/gpg.go`
- Impact: No longer maintained; may have security vulnerabilities; staticcheck warning suppressed with nolint directive
- Fix approach: Migrate to `github.com/ProtonMail/go-crypto/openpgp` as noted in the TODO comment at line 10

**Global State for Keyring Paths:**
- Issue: `keyringPaths` is a package-level mutable slice with `SetKeyringPaths()` to override
- Files: `internal/manifest/gpg.go:16-19`, `internal/manifest/gpg.go:87-89`
- Impact: Not thread-safe; testing requires careful state management
- Fix approach: Pass keyring paths as function parameters or via a config struct

**Global CLI Flags:**
- Issue: CLI flags stored in package-level variables (`Definitions`, `JSONOutput`, `Verify`, etc.)
- Files: `cmd/common/common.go:12-18`
- Impact: Not testable in isolation; prevents parallel test execution; tight coupling between commands
- Fix approach: Pass configuration through command context or dependency injection

**Duplicated Pattern Handling Logic:**
- Issue: Pattern fallback logic (`MatchPatterns` vs `MatchPattern`) repeated in multiple places
- Files: `internal/sysext/manager.go:17-24`, `internal/sysext/manager.go:86-93`, `internal/sysext/manager.go:159-165`, `updex/install.go:219-222`, `updex/update.go:113-116`
- Impact: Code duplication; easy to introduce inconsistencies; backwards compatibility burden
- Fix approach: Create helper method on `Transfer` struct to return patterns slice, deprecate `MatchPattern` field

**Version Sorting Duplication:**
- Issue: Two different implementations of version sorting exist
- Files: `internal/version/pattern.go:138-143` (`Sort`), `updex/discover.go:185-190` (`sortVersionsDescending`)
- Impact: Inconsistent sorting behavior; `sortVersionsDescending` uses simpler string comparison while `Sort` uses semver
- Fix approach: Use `version.Sort` consistently everywhere; remove duplicate implementation

**HTTP Client Recreation:**
- Issue: New HTTP clients created for each request instead of reusing
- Files: `internal/manifest/manifest.go:26-28`, `internal/download/download.go:37-39`, `updex/discover.go:71-73`, `updex/discover.go:110-112`, `updex/install.go:136-138`
- Impact: Connection pooling not utilized; inefficient for multiple sequential requests
- Fix approach: Create shared HTTP client in `Client` struct; pass to functions that need it

## Known Bugs

**Context Parameter Unused:**
- Symptoms: Context passed to API methods but never used for cancellation
- Files: `updex/install.go:22`, `updex/update.go:15`, `updex/remove.go:11`, `updex/discover.go:15`
- Trigger: Long-running operations cannot be cancelled
- Workaround: HTTP client has hardcoded timeouts (30s for manifests, 10min for downloads)

**Ignored Error from GetInstalledVersions:**
- Symptoms: Error return from `sysext.GetInstalledVersions` ignored
- Files: `updex/install.go:246`, `updex/update.go:84`
- Trigger: Filesystem errors during version check silently ignored
- Workaround: None; error is discarded with `_`

## Security Considerations

**GPG Verification Disabled by Default:**
- Risk: Manifest integrity not verified unless explicitly enabled with `--verify` flag
- Files: `cmd/common/common.go:24`, `internal/config/transfer.go:129`
- Current mitigation: SHA256 hash verification always performed on downloaded files
- Recommendations: Consider enabling GPG verification by default; add warning when verification is disabled

**No TLS Certificate Pinning:**
- Risk: MITM attacks possible if TLS is compromised
- Files: All HTTP client usage (`internal/manifest/manifest.go`, `internal/download/download.go`, `updex/discover.go`, `updex/install.go`)
- Current mitigation: Uses system TLS configuration
- Recommendations: Consider adding certificate pinning for known repositories

**Transfer Files Downloaded from Internet Written to /etc:**
- Risk: Remote server compromise could lead to malicious config installation
- Files: `updex/install.go:70`, `updex/install.go:134-192`
- Current mitigation: Requires root privileges; manual install step
- Recommendations: Add signature verification for transfer files; warn about source trust

**No Input Sanitization on Component Names:**
- Risk: Path traversal possible in component name used to construct file paths
- Files: `updex/install.go:69-70`
- Trigger: Component name containing `../` or absolute path
- Recommendations: Validate component name matches expected pattern (alphanumeric + underscore/hyphen)

## Performance Bottlenecks

**Serial Extension Processing:**
- Problem: Extensions processed one at a time in update/check operations
- Files: `updex/update.go:31-198`, `updex/check.go`
- Cause: Sequential loop without concurrency
- Improvement path: Use goroutines with worker pool for parallel manifest fetches; serialize only file operations

**Manifest Re-fetched for Update Check:**
- Problem: Manifest fetched twice when checking then updating
- Files: `updex/update.go:102` (manifest fetch after version check which also fetches)
- Cause: No caching of manifest data
- Improvement path: Cache manifests by URL with TTL; share between operations

**Pattern Parsing on Every Match:**
- Problem: `ExtractVersionMulti` parses pattern strings to `Pattern` structs on every call
- Files: `internal/version/pattern.go:102-115`
- Cause: No caching of parsed patterns
- Improvement path: Parse patterns once when loading transfer config; store `*Pattern` instead of strings

## Fragile Areas

**Version Extraction from Filenames:**
- Files: `updex/discover.go:166-182`, `internal/version/pattern.go:69-77`
- Why fragile: Assumes specific filename format; hardcoded underscore delimiter in discover
- Safe modification: Add comprehensive test cases before changing; ensure both extractors produce same results
- Test coverage: `internal/version/pattern_test.go` exists but `discover.go` extraction not tested

**Symlink Management:**
- Files: `internal/sysext/manager.go:132-149`, `internal/sysext/manager.go:275-323`
- Why fragile: Complex symlink chains (staging symlink -> actual file -> sysext symlink); race conditions possible
- Safe modification: Add integration tests; verify atomic operations
- Test coverage: `internal/sysext/manager_test.go` exists but symlink operations not fully covered

**systemd-sysext Command Execution:**
- Files: `internal/sysext/manager.go:404-428`
- Why fragile: Depends on external `systemd-sysext` binary availability; stdout/stderr piped directly
- Safe modification: Mock exec.Command in tests; add graceful degradation
- Test coverage: Not tested (requires systemd)

## Scaling Limits

**Memory Usage for Large Manifests:**
- Current capacity: Entire manifest loaded into memory (`io.ReadAll`)
- Files: `internal/manifest/manifest.go:41-43`, `updex/discover.go:85-87`, `updex/discover.go:124-126`
- Limit: Very large SHA256SUMS files could cause OOM
- Scaling path: Stream-process manifest files; limit maximum size

**No Parallel Downloads:**
- Current capacity: One file at a time
- Limit: Large extension updates are slow
- Scaling path: Add concurrency option for multi-file downloads

## Dependencies at Risk

**Deprecated Crypto Package:**
- Risk: `golang.org/x/crypto/openpgp` is deprecated and frozen
- Impact: No security fixes; may be removed in future Go versions
- Migration plan: Use `github.com/ProtonMail/go-crypto/openpgp` as drop-in replacement

**Outdated Indirect Dependencies:**
- Risk: Multiple indirect dependencies have available updates
- Impact: Missing bug fixes and performance improvements
- Migration plan: Run `go get -u` periodically; test thoroughly after updates

## Missing Critical Features

**No Rollback Mechanism:**
- Problem: No way to revert to previous version after failed update
- Blocks: Safe automated updates; confidence in update process

**No Update Verification:**
- Problem: No post-install validation that extension works correctly
- Blocks: Automated update pipelines; early detection of corrupt downloads

**No Proxy Support:**
- Problem: HTTP clients don't respect `HTTP_PROXY`/`HTTPS_PROXY` environment variables
- Blocks: Use in corporate/restricted network environments

**No Resume for Failed Downloads:**
- Problem: Partial downloads lost on failure; must restart from beginning
- Blocks: Reliable updates over unreliable networks

## Test Coverage Gaps

**No Tests for updex Package:**
- What's not tested: All files in `updex/` directory (core business logic)
- Files: `updex/install.go`, `updex/update.go`, `updex/discover.go`, `updex/remove.go`, `updex/list.go`, `updex/check.go`, `updex/vacuum.go`, `updex/features.go`
- Risk: Core functionality can regress without detection
- Priority: High

**No Integration Tests:**
- What's not tested: End-to-end flows involving file system, network, systemd-sysext
- Risk: Component interactions may fail in production
- Priority: Medium

**Limited sysext Manager Tests:**
- What's not tested: `LinkToSysext`, `UnlinkFromSysext`, `Refresh`, `Merge`, `Unmerge`
- Files: `internal/sysext/manager.go`
- Risk: File system operations may have edge cases
- Priority: Medium

**No Tests for Command Handlers:**
- What's not tested: All `cmd/commands/*.go` RunE functions
- Risk: CLI behavior changes undetected; error handling paths untested
- Priority: Low (thin layer over updex package)

---

*Concerns audit: 2026-01-26*
