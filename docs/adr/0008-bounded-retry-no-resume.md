# 0008 — Bounded whole-attempt retries; checksum mismatch is fatal

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

Manifest fetches and image downloads cross the network to
repository.frostyard.org and catalog release hosts, where transient
failures (connection resets, timeouts, 5xx, rate limiting) are routine and
must not fail an update run. But not every failure is transient: TLS/cert
errors, 4xx responses, and hash mismatches indicate misconfiguration or
tampering, and retrying them wastes time or — worse — masks an integrity
failure. Downloads can also fail mid-body, raising the question of
HTTP range/resume support and how progress reporting behaves across
attempts.

## Decision

One shared policy lives in `internal/retry` and is used by both
`manifest.Fetch` and `download.Download`:

- `retry.DefaultConfig` is 3 total attempts with 1s exponential backoff
  (1s, 2s), context-cancellable during the sleep.
- Only errors explicitly marked transient are retried. HTTP 429 and all
  5xx are marked transient; transport and body-read failures are marked
  via `TransientIfNetwork` (timeouts, `ECONNRESET`/`ECONNREFUSED`/
  `ECONNABORTED`/`EPIPE`, `io.EOF`/`ErrUnexpectedEOF`). Everything else —
  TLS errors, 4xx other than 429, and SHA256 mismatches — fails
  immediately; a checksum mismatch is never retried because the same
  bytes arriving again is not a fix, it is the failure.
- There is no range/resume: each download attempt starts from scratch
  with a fresh `os.CreateTemp` file, hashing while writing, and the temp
  file is removed on any failure. Progress is attempt-local —
  `OnDownloadProgress` is invoked once per attempt and must return a
  fresh writer each time.

## Consequences

- Flaky networks and origin hiccups self-heal without operator
  intervention, while integrity and configuration failures surface on the
  first attempt.
- Restarting large downloads from byte zero wastes bandwidth on late
  failures; accepted because sysext images are modest and resume would
  complicate hash-while-downloading (the hash covers the whole body, so a
  resumed attempt would need re-reading the partial file).
- Progress bars must tolerate restarting from zero on retry; the SDK
  contract (fresh writer per attempt) encodes this.
- New network paths must classify their errors through
  `retry.Transient`/`TransientIfNetwork` rather than inventing local
  policies.

## Alternatives considered

- **Retrying everything a fixed number of times:** retries checksum
  mismatches and 4xx, hiding tampering and misconfiguration behind delay.
- **HTTP Range resume across attempts:** saves bandwidth on large files
  but breaks streaming hash verification and adds server-capability
  probing; not worth it at sysext image sizes.
- **Per-callsite retry loops:** the pre-`internal/retry` state; policies
  drift and error classification gets re-invented inconsistently.

## References

- Implements: [`internal/retry/retry.go`](../../internal/retry/retry.go)
  (`DefaultConfig`, `Do`, `Transient`, `TransientIfNetwork`),
  [`download/download.go`](../../download/download.go) (per-attempt temp
  file, 429/5xx classification, fatal hash mismatch),
  [`manifest/manifest.go`](../../manifest/manifest.go) (same
  classification for `SHA256SUMS` fetches)
- Shapes: [design/overview.md — Data Flow](../design/overview.md#feature-update-end-to-end),
  [specs/sdk-api.md — download](../specs/sdk-api.md#download)
- Builds on: [core ADR-0023 — External downloads are version-pinned and checksum-verified](https://github.com/frostyard/core/blob/main/docs/adr/0023-verified-pinned-downloads.md),
  [core ADR-0009 — repository.frostyard.org is the single artifact origin with frozen namespaces](https://github.com/frostyard/core/blob/main/docs/adr/0009-single-artifact-origin-repository-frostyard-org.md)
