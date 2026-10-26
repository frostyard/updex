# 0018 — Skip an update run when every source is unreachable

- **Status:** Accepted
- **Date:** 2026-10-26

## Context

[ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md) ships a
systemd timer that runs `updex features update --no-refresh` every 6 hours,
and its "Failure behaviour" section says the run exits non-zero when any
enabled feature failed. That rule is right for a real failure: a broken
transfer, a bad signature, a full disk.

A host that is offline is not a real failure. Its sources cannot be reached,
so every enabled feature fails, `updex-update.service` becomes `failed`, and
`systemctl is-system-running` reports `degraded` until a later run succeeds.
On a laptop that is often offline this state lasts up to 6 hours
(frostyard/updex#471).

The `Wants=`/`After=network-online.target` ordering does not help: on many
setups the target is reached without a working uplink.

On 2026-10-26 Brian chose option 2 of the issue: a run under systemd whose
sources are all unreachable is a skip.

## Decision

`UpdateFeatures` returns an error that wraps the new exported sentinel
`updex.ErrSourcesUnreachable` (`no update source is reachable`) only when all
of these hold:

1. at least one transfer failed;
2. no HTTP request of the run got an HTTP response, of any status;
3. every transfer error except the collateral `not linked: another transfer
   of feature <name> failed` is a network failure: a DNS error, a failed dial
   (`syscall.ECONNREFUSED`, `EHOSTUNREACH`, `ENETUNREACH`, `ENETDOWN`), or a
   timeout that expired before a response arrived;
4. the run's own context is still live (a cancelled or stopped run never
   skips);
5. no refresh failed.

Every other failure keeps today's shape. When the rule holds, the first
joined error becomes `one or more components failed to update: no update
source is reachable`, the transfer errors stay in the chain after it, and the
results slice is unchanged.

The SDK learns rule 2 from a per-operation tracker carried in the run's
context. `NewClient` uses a shallow copy of its HTTP client whose transport
wraps the given transport and records whether a response came back. A request
outside `UpdateFeatures` carries no tracker and is unaffected.

Only the CLI applies the skip, and only under systemd (the environment
variable `INVOCATION_ID` is set, as for the timer's service).
`updex features update` prints its normal output first (the text table, or
the results array with `--json`), writes exactly one line to stderr,
`updex: no update source is reachable; skipping this run`, and exits 0. In an
interactive shell the command still returns the error and exits non-zero.
`--json` output is unchanged, so consumers see the same document. The systemd
units do not change.

TLS and x509 errors, HTTP error statuses, verification failures, hash
mismatches, disk errors, "no versions available" and a cancelled context are
not unreachable: a captive portal or an interception must still fail the
unit.

## Consequences

- An offline host skips its scheduled runs instead of failing
  `updex-update.service`; the next run with a working network updates as
  before.
- The per-transfer warnings still reach the journal, so
  `journalctl -u updex-update.service` shows which sources failed.
- `updex features update` in a shell and `updex features check` still report
  the failure, with a non-zero exit.
- **Negative:** a source host name that is wrong for ever (DNS not found)
  also looks offline, so the timer never fails for it. Only a manual run or
  `features check` shows the mistake. This is accepted because the journal
  keeps the warnings and a real failure of any other kind still fails.
- The tracker follows the transport a client was built with. Code that
  replaces `client.httpClient.Transport` after `NewClient` bypasses it, so
  such a client never reports an unreachable run; only tests do this.
- Behind an HTTP proxy, the proxy answers (for example `502 Bad Gateway`)
  even when the source is offline, so such a run counts as answered and still
  fails. Only a proxy that cannot be reached itself gives a skip.

## Alternatives considered

- **Option 1 — keep failing:** the unit stays `failed` and the system
  `degraded` for every offline laptop, for up to 6 hours at a time, so a
  real failure is hard to tell from a routine one. Not chosen.
- **Option 3 — `Restart=on-failure` or a shorter timer:** retries the run
  sooner or restarts the unit, but the unit still ends `failed` whenever the
  host stays offline, and it adds load to a host that has no network. It does
  not tell "offline" apart from "broken".

## References

- Amends: [ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md)
  ("Failure behaviour": the run exits non-zero when any enabled feature
  failed)
- Builds on: [ADR-0008](0008-bounded-retry-no-resume.md) (bounded retries and
  the network errors that are transient)
- Shapes: [design/overview.md](../design/overview.md),
  [specs/sdk-api.md](../specs/sdk-api.md), [README.md](../../README.md)
