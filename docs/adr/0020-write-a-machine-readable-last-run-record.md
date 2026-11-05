# 0020 — Write a machine-readable last-run record

- **Status:** Accepted
- **Date:** 2026-11-05

## Context

Plan T857, task t2: snosi's health collector (`snosi-health`) must report how
the last automatic sysext update went, per feature, without parsing text.

Today it cannot. The timer runs `updex features update --no-refresh` through
`updex-update.service`, and the service result hides two kinds of run: a run
that found another updex operation holding the lock (`ErrBusy`) and a run
whose sources were all unreachable
([ADR-0018](0018-skip-an-update-run-when-every-source-is-unreachable.md)) both
exit 0 under systemd. The journal has the text, and `UpdateResult.Error` is
free text too: a collector would have to match error strings that change with
every wording fix, and the text can carry source URLs.

The facts a collector needs are already known inside `UpdateFeatures`: which
features ran, what each transfer did (linked, current, held by a rollback,
waiting for a newer base image, failed), and where each error was made.

## Decision

Every `UpdateFeatures` run that is not a dry run writes a JSON record,
`/var/lib/updex/last-run.json` (`RuntimePaths.LastRunPath`,
`updex.DefaultLastRunPath`), format 1, defined in
[specs/last-run-record.md](../specs/last-run-record.md): the run outcome
(`completed`, `failed`, `skipped_busy`, `skipped_unreachable`), the refresh
state, and per feature its origin, outcome, error class and version, with the
same per transfer. It contains classes and enums only, never error text.

Errors get their class where they are made: a new internal package,
`internal/fault`, wraps an error with a class and keeps `Error()` byte for
byte (`Unwrap` keeps `errors.Is`/`errors.As`). `download`, `manifest`,
`sysext` and `updex` mark HTTP status, checksum, signature, configuration and
link errors at their origin; `updex` classifies a transfer error in a fixed
order (cancelled, disk space, network, the outermost mark, other).

A busy run records the features it would have acted on as `skipped_busy`, but
only when the existing record ended before the busy run started, so it never
replaces the record the lock holder writes. A run that names features carries
the previous entries of the other enabled features. Writers serialise on a
`flock` of the record directory with a 2-second bounded, non-blocking wait.

The record never fails the update: a failure to write it is one warning, and
the results and the returned error stay the same.

## Consequences

- A collector reads one small file and gets every outcome the service result
  hides, with stable enums. A consumer maps an unknown enum value to `other`,
  so a later updex can add a class or an outcome without breaking it.
- Every new error path that a collector must tell apart needs a mark at its
  origin; an unmarked error is `other`. That is the safe default, but a new
  failure kind is invisible until it is marked.
- The record reflects only `UpdateFeatures`. `EnableFeature --now`, rollback
  and the catalog commands do not write it.
- One more file under `/var/lib/updex` that updex writes as root, with the
  ADR-0005 guards (Lstat, refuse non-regular, temp file plus rename).
- A busy run reads the domain once more, read-only and without the lock.

## Alternatives considered

- **Parse the journal or `--json` output:** the text changes, carries URLs,
  and a busy or unreachable run exits 0, so the collector cannot tell it from
  a good run without parsing text.
- **Exit codes per outcome:** ADR-0018 deliberately keeps the unit green for
  an offline host; distinct exit codes would turn skips into unit failures
  again, and give no per-feature detail.
- **Classify by matching error text:** brittle, and the text is not a
  contract.

## References

- Shapes: [specs/last-run-record.md](../specs/last-run-record.md),
  [specs/sdk-api.md](../specs/sdk-api.md),
  [design/overview.md](../design/overview.md)
- Builds on: [ADR-0005](0005-transactional-writes-lstat-checks.md),
  [ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md),
  [ADR-0018](0018-skip-an-update-run-when-every-source-is-unreachable.md)
