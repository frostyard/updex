# Spec: last-run record (`/var/lib/updex/last-run.json`, format 1)

This contract governs the machine-readable record that every
`UpdateFeatures` run that is not a dry run writes: the outcome of the run, and
per feature its origin, outcome, error class, version, and when it last had a
successful run (`last_ok`). snosi's health
collector (`snosi-health`) and `updex daemon status` read this file; any other tool that wants to
know how the last update went reads it the same way. The service result of
`updex-update.service` does not carry this information: a busy run and a run
whose sources are all unreachable both exit 0 under systemd
([ADR-0018](../adr/0018-skip-an-update-run-when-every-source-is-unreachable.md)),
and `UpdateResult.Error` is free text. The record carries classes and enums,
never error text.

## Interface

Path: `RuntimePaths.LastRunPath`, default `updex.DefaultLastRunPath`
(`/var/lib/updex/last-run.json`). JSON, mode `0644`.

```json
{
  "format": 1,
  "started": "2026-11-05T10:00:00Z",
  "finished": "2026-11-05T10:00:42Z",
  "outcome": "failed",
  "refresh": "done",
  "selected": false,
  "features": [
    {
      "feature": "docker",
      "origin": "image",
      "outcome": "updated",
      "version": "29.0.1",
      "at": "2026-11-05T10:00:42Z",
      "last_ok": "2026-11-05T10:00:42Z",
      "transfers": [
        {"component": "docker", "outcome": "updated", "version": "29.0.1"}
      ]
    },
    {
      "feature": "incus",
      "origin": "catalog",
      "outcome": "failed",
      "error_class": "network",
      "at": "2026-11-05T10:00:42Z",
      "last_ok": "2026-11-04T22:00:00Z",
      "transfers": [
        {"component": "incus", "outcome": "failed", "error_class": "network"}
      ]
    }
  ]
}
```

### Run

| Field | Type | Required | Constraints |
| --- | --- | --- | --- |
| `format` | integer | yes | `1` |
| `started` | string | yes | RFC 3339, UTC, whole seconds: when the run started |
| `finished` | string | yes | RFC 3339, UTC, whole seconds: when the run ended |
| `outcome` | string | yes | `completed`, `failed`, `skipped_busy`, `skipped_unreachable` |
| `error_class` | string | no | only for a failure outside every feature (see Rules) |
| `refresh` | string | yes | `done`, `failed`, `skipped`, `not_requested`, `not_run` |
| `selected` | boolean | yes | `true` when the run named features (`opts.Features`, `features update NAME...`) |
| `features` | array | yes | feature entries, never `null` (`[]` when none) |

### Feature

| Field | Type | Required | Constraints |
| --- | --- | --- | --- |
| `feature` | string | yes | the feature name |
| `origin` | string | yes | `image`, `catalog`, `local`, `unknown` (the `FeatureInfo.Origin` kinds; no origin name) |
| `outcome` | string | yes | `updated`, `current`, `held_by_rollback`, `needs_image`, `failed`, `skipped_unreachable`, `skipped_busy` |
| `error_class` | string | no | only for `failed` and `skipped_unreachable` |
| `version` | string | no | the single distinct non-empty transfer version; absent when there is none or more than one |
| `at` | string | yes | RFC 3339, UTC, whole seconds: when this entry was produced |
| `last_ok` | string | no | RFC 3339, UTC, whole seconds: the `at` of the latest entry for this feature whose outcome was `updated`, `current`, `held_by_rollback` or `needs_image`, carried across a failing run (see Rules); absent when the feature never had one |
| `transfers` | array | yes | one entry per active transfer, never `null` |

### Transfer

| Field | Type | Required | Constraints |
| --- | --- | --- | --- |
| `component` | string | yes | the transfer component |
| `outcome` | string | yes | the feature outcomes plus `not_linked` |
| `error_class` | string | no | only for `failed` and `skipped_unreachable` |
| `version` | string | no | see Rules |

### Enums

| Run `outcome` | Meaning |
| --- | --- |
| `completed` | the run returned no error (a feature held or waiting for an image is not an error) |
| `failed` | the run returned an error that is none of the two below |
| `skipped_busy` | another updex operation held the lock (`ErrBusy`); nothing ran |
| `skipped_unreachable` | the run error wraps `ErrSourcesUnreachable`: no source answered |

| `refresh` | Meaning |
| --- | --- |
| `done` | `systemd-sysext refresh` ran and succeeded |
| `failed` | the refresh failed |
| `skipped` | the refresh gate blocked it (`RefreshSkipped`, ADR-0016) |
| `not_requested` | the run had `NoRefresh` (`--no-refresh`, the timer) |
| `not_run` | the run was busy, or failed before the refresh |

| Feature / transfer `outcome` | Meaning |
| --- | --- |
| `updated` | this run linked a new image, or relinked a staged one (`UpdateResult.Linked`) |
| `current` | the newest version is already linked; also a feature with no active transfer |
| `held_by_rollback` | a rollback record holds the feature (`UpdateResult.Held`) |
| `needs_image` | the image needs a newer base image (`UpdateResult.NeedsImage`) |
| `failed` | the transfer, or a transfer of the feature, failed |
| `skipped_unreachable` | `failed` in a run whose sources were all unreachable |
| `skipped_busy` | the run was busy |
| `not_linked` | transfer only: this transfer did nothing wrong, but another transfer of its feature failed or needs an image, so its link did not move |

| `error_class` | Meaning |
| --- | --- |
| `network` | the connection failed: DNS, refused, reset, aborted, unreachable, timeout, or a TLS certificate that does not verify (a captive portal gives these) |
| `http_status` | a source answered with an HTTP error status (manifest, image, or a signature status other than 404) |
| `signature` | the signature of `SHA256SUMS` is missing (404), invalid, or cannot be checked (no usable keyring) |
| `checksum` | a download does not match its SHA256 hash |
| `disk_space` | the disk or the quota is full (`ENOSPC`, `EDQUOT`) |
| `link` | the systemd-sysext link could not be moved, its old entry was refused, or a restore failed |
| `config` | the configuration is wrong: the domain cannot be loaded, a source type or pattern, a target pattern, or a catalog signature policy that fails closed because the owning catalog is missing, cannot be loaded, or cannot be recognized |
| `other` | everything else, also a cancelled run |

## Rules

- A run that is not a dry run writes the record when it ends, with a temp
  file in the same directory plus rename, mode `0644`. The parent directory is
  created with mode `0755` when missing; both modes are set explicitly, so
  neither depends on the process umask (updex sets the mode only in a parent
  directory owned by its effective user or root that group and others cannot
  write, or that has the sticky bit; elsewhere a new directory keeps the
  umask's mode). Each run writes the record at most
  once.
- A run that got the operation lock writes its record **before** it releases
  the lock. So a later run, which can start only after the release, always
  writes after it, and an older run never replaces a newer run's record.
- A dry run writes nothing and reads nothing.
- A run whose feature names `selectFeatures` refuses (not found, masked, not
  enabled) writes nothing: nothing ran. This is also true for a busy run.
- A domain that cannot be loaded writes run `outcome` `failed`,
  `error_class` `config`, `refresh` `not_run` and no feature. A lock error that
  is not `ErrBusy` writes `failed`, `error_class` `other`, `not_run`, no
  feature. The run `error_class` is absent in every other case.
- Precedence across the transfers of a feature: `failed` > `needs_image` >
  `held_by_rollback` > `updated` > `current`. `not_linked` never decides it.
- The feature `error_class` is the class of its first `failed` transfer.
- Transfer `version`: `updated` and `current` give the linked version
  (`UpdateResult.Version`); `held_by_rollback` the version the link names;
  `needs_image` and `not_linked` the staged version; `failed` the version the
  stage step selected and then failed to stage or link, when known.
- The class of a transfer error is decided in this order: a cancelled context
  is `other`; `ENOSPC` or `EDQUOT` anywhere in the chain is `disk_space`; a
  network failure is `network`; else the outermost `internal/fault` mark set
  where the error was made; else `other`.
- When the run error wraps `ErrSourcesUnreachable`, every `failed` feature and
  transfer is `skipped_unreachable` with class `network`, and the run outcome
  is `skipped_unreachable`.
- Busy run (`ErrBusy`): the record lists every enabled, unmasked, selected
  feature with an associated transfer as `skipped_busy` (each active transfer
  too), run `outcome` `skipped_busy`, `refresh` `not_run`. When the busy run
  cannot load the domain, the record has no feature. Names that
  `selectFeatures` refuses make the busy run write nothing. A busy run that
  names features carries the previous entries of the other features (see
  Merge). The busy record is written only
  when the existing record's `finished` is before this run's `started`
  (compared in whole seconds): otherwise the lock holder already wrote a
  newer record, and it is kept. An unreadable, malformed or other-format
  existing record does not stop the write.
- Merge: a run that names features, busy or not, carries each entry of the
  previous format 1 record for a feature that is enabled and unmasked in the
  current domain and was not in this run, with its own `at`. A run that names no
  feature carries nothing.
- `last_ok`: when an entry's outcome is `updated`, `current`, `held_by_rollback`
  or `needs_image`, its `last_ok` is its own `at`. Any other outcome (`failed`,
  `skipped_unreachable`, `skipped_busy`) takes the previous record's value for
  that feature, and has no `last_ok` when the previous record holds none, or
  holds one that is not an RFC 3339 timestamp. The previous value of a feature
  is the latest valid one among its `last_ok` and the `at` of its previous
  entry when that entry's outcome was ok, so a format 1 record written before
  this field existed still gives its `at`. A carried entry of a named run is
  recomputed the same way. A run that records no feature (a domain that cannot
  be loaded, a lock error, or a busy run that cannot load the domain) carries
  nothing, so `last_ok` is lost until the next run that lists the feature: the
  record of such a run has no feature at all. ADR-0018 accepts that a source
  that is wrong for ever looks offline, so the timer skips every run; `last_ok`
  is what still shows such a feature in `updex daemon status`.
- Suite-switch run (`features update --suite-switch`, ADR-0021): a run that
  finds a link to another suite's image is a run that names the features it
  selected (`selected` is `true`, Merge applies). A run that finds none, before
  the lock or again under it, writes nothing; so does a run whose options are
  refused. A run whose domain cannot be loaded is a failed run of class
  `config` with `selected` `true` and no feature, as for any update. A run
  that skips its refresh because something else is pending (or the host's
  pending state is unknown) has `refresh` `skipped`. Each retry of
  `updex-suite-switch.service`
  ([ADR-0022](../adr/0022-retry-a-failed-suite-switch-run-at-boot.md)) is a
  new run under these same rules.
- Every write holds an exclusive `flock` on the parent directory while it
  reads the previous record and renames the new one. It takes the lock with
  `LOCK_NB` and retries every 10 ms for up to 2 s, then skips the write.
- The previous record is read with `O_NOFOLLOW` and a bound of 1 MiB; an
  unreadable, oversized, malformed or other-format record is ignored.
- A symlink or other non-regular file at the record path, and a parent that
  is not a directory, are refused. Every failure to write is one warning:
  the update's results and returned error do not change.
- The record never contains error text, a URL or a path.
- Consumers: an absent file, or a file with an unknown `format`, means "no
  record". A consumer MUST map an enum value it does not know (`outcome`,
  `refresh`, `error_class`) to `other`, so a later updex can add a value.
  `updex daemon status` reads it without a lock or writes, even without a
  timer, through `DaemonStatusResult.LastUpdate` (`last_update` in JSON).
  Its reader refuses symlinks and non-regular files, checks the opened file
  identity, and caps the read at 1 MiB. Format 1 requires valid RFC 3339
  `started` and `finished`, non-empty run `outcome` and `refresh`, non-null
  `features`, and each feature's non-empty name and outcome and non-null
  `transfers`. A `last_ok` that is present but not an RFC 3339 timestamp is
  malformed, also an empty string or `null`. A feature object with more than 1
  key that `encoding/json` matches to `last_ok` (without regard to case, in any
  order) is malformed too: the reader reads each feature object token by token
  and sees every such key. A `format` that is not the integer 1 (absent, another number,
  a string) is an unknown format. Unknown fields are accepted. Invalid or unreadable records set
  `LastUpdateError` (`last_update_error`); status still succeeds. The writer's
  previous-record reader keeps its existing permissive behavior.
- `updex daemon status` reports the stale features of the record in
  `DaemonStatusResult.StaleFeatures` (`stale_features` in JSON), in record
  order: an entry whose effective `last_ok` (as the rule above computes it) is
  more than `updex.LastOKWarnAge` (7 days) old, or whose effective `last_ok` is
  absent and whose outcome is `failed` or `skipped_unreachable`. Exactly 7 days
  is not stale, and a `last_ok` in the future is not stale. Only a feature that
  is enabled and unmasked in the current domain counts: the status command
  reads the domain without warnings, and a domain that cannot be loaded counts
  every entry as enabled (the record lists only features that were enabled when
  it was written). Status never fails because of this computation. The text
  output prints one `Warning:` line per stale feature after the feature lines.

## Derived artifacts

| Artifact | Derivation |
| --- | --- |
| snosi `snosi-health` collector | reads this file only |
| `updex/lastrun.go` (`readLastRunStatus`, `staleFeatures`) | read-only consumer for `DaemonStatus`, normalizes unknown enums and validates `last_ok`; computes the stale features |
| `updex/lastrun_status_test.go` | pins the reader's bound, its `Lstat` refusal of a symlink, FIFO or directory, format and field validation, `last_ok` validation and normalization, the stale-feature computation, and status integration |
| `cmd/updex/daemon_status_test.go` | pins last-update text (including `last ok` and `never ok`), the stale `Warning:` lines, JSON (including `last_ok` and `stale_features`) and terminal control escaping |
| `updex/lastrun_test.go` | pins the classes, outcomes, busy, merge and `last_ok` carry rules through `UpdateFeatures` |

## References

- Rationale: [ADR-0020](../adr/0020-write-a-machine-readable-last-run-record.md)
- Builds on: [ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md),
  [ADR-0018](../adr/0018-skip-an-update-run-when-every-source-is-unreachable.md)
- Context: [design/overview.md](../design/overview.md),
  [specs/sdk-api.md](sdk-api.md)
