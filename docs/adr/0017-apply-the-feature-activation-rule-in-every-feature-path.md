# 0017 — Apply the sysupdate.d activation rule in every feature path

- **Status:** Accepted
- **Date:** 2026-10-26

## Context

A `.transfer` file can name features in two lists. `Features=` is an OR list and
`RequisiteFeatures=` is an AND list. systemd `sysupdate.d(5)` makes a transfer
*active* only when both conditions hold: `Features=` is empty or at least one
listed feature is enabled, and every feature in `RequisiteFeatures=` is enabled.

updex parsed both lists, but the feature commands did not apply the rule
([frostyard/updex#455](https://github.com/frostyard/updex/issues/455)).
`UpdateFeatures`, `CheckFeatures`, `EnableFeature{Now}`, `DisableFeature{Now}`
and `RollbackFeature` selected transfers with `config.GetTransfersForFeature`,
which returns every transfer that names the feature in either list.
`config.FilterTransfersByFeatures`, which implements the rule, had no caller
outside its tests. So a transfer with `Features=maild` and
`RequisiteFeatures=lodge` was downloaded and linked by `enable maild --now` and
by `features update` while `lodge` was disabled, and `update` with only `lodge`
enabled also installed it. The config reference and the design overview
described two different behaviours.

`DisableFeature{Now}` has a mirror problem: it removed every transfer that named
the feature, also one that another enabled feature in its `Features=` still
activates. The rollback holds (ADR-0016) were read over the same association
set, so a record of feature `g` that names a transfer `f` does not act on still
held `f`.

## Decision

updex applies the activation rule in every feature path.

- `config.CheckTransferActivation` is the single implementation of the rule. It
  returns whether a transfer is active and, when it is not, the
  `RequisiteFeatures=` entries that are not enabled and the `Features=` list when
  none of it is enabled. A feature is enabled when it is in the loaded domain,
  enabled and not masked; a name that is not loaded is never enabled. A transfer
  with neither list belongs to no feature and no feature command touches it.
- `config.GetTransfersForFeature` stays the **association** lookup. It never
  decides what updex downloads, links, removes or holds.
- The SDK splits the associated transfers with `splitFeatureTransfers`: the
  loaded domain's enabled set, with the feature being processed forced to the
  state the command gives it. That is **enabled** for `UpdateFeatures`,
  `CheckFeatures`, `EnableFeature{Now}`, `RollbackFeature` and the hold read in
  `Features`, and **disabled** for `DisableFeature{Now}`.
- Only the active ("acted-on") transfers are downloaded, linked, vacuumed and
  rolled back. The others are reported, not hidden: `Inactive` on
  `UpdateFeaturesResult`, `CheckFeaturesResult` and `FeatureActionResult`, with
  the missing requisites and the `Features=` list that has no enabled entry, and
  one text line each in the CLI. The results are additive JSON fields.
- `EnableFeature{Now}` installs the transfers that are active with the feature
  enabled, so `enable lodge --now` installs `Features=maild
  RequisiteFeatures=lodge` when `maild` is already enabled, and `enable maild
  --now` with `lodge` disabled downloads nothing for it.
- `DisableFeature{Now}` **keeps** a transfer that is still active with the
  feature disabled (another enabled feature in its `Features=`): no unlink, no
  removal of images, no part in the merged-state `--force` check. It is reported
  in `FeatureActionResult.Kept`. Only the other transfers are removed. When
  nothing is left to remove, there is no unmerge, no refresh and no refresh-gate
  preflight; only the drop-in is written. `CatalogRemove` is unchanged: a
  catalog transfer has `Features=<name>` as its only `Features=` entry, so it is
  never kept.
- Rollback holds are computed over the acted-on set. `collectRollbackHolds`,
  `rollbackHoldReason` and `deleteFinishedRollbackRecords` get the active
  transfers, through `installTransfers`, `CheckFeatures` and `Features`. A record
  of `g` that names only a transfer which `f` does not act on no longer holds `f`.
  `RollbackFeature` rolls back, and records, only the active transfers, and fails
  with `feature '<name>' has no active transfers to roll back` when there is
  none. One exception: an existing rejection of an *inactive associated*
  transfer of the feature stays in the record (`preservedRejections`), with no
  link change. Otherwise a repeated rollback, while a requisite feature is
  disabled, would erase the rejection, and enabling the requisite feature again
  would let `update` link the rejected version (ADR-0016). A dry run says it in
  one reporter line per kept entry. An entry for a component that is neither
  active nor an inactive associated transfer is dropped, as before, so a removed
  transfer does not hold the feature for ever. `rollbackFeaturesOf` stays the association (it reads more records, never
  fewer, so it keeps failing closed).

## Consequences

- A feature no longer installs a transfer that waits for another feature, and
  `features update`, `check` and `list` agree on what is held.
- `disable --now` no longer removes an image that another enabled feature needs.
  An operator who wants that image gone disables every feature that activates it.
- The `Component` scope and `-C` limit the loaded domain, so a requisite feature
  outside it counts as not enabled and keeps the transfer inactive.
- A transfer that is inactive for every enabled feature stays on disk as it is
  (its link and images are not removed by `update`); `disable --now` of one of its
  features removes it.
- A rollback of a feature while a requisite feature is disabled does not roll
  back the transfer that waits, and a first rollback adds no entry for it. When
  the requisite feature is enabled later, the transfer is a new part of the
  feature without a hold, unless the record already rejected a version of it:
  that rejection is kept.
- A record of `g` is still read when `g` names an acted-on transfer, also when the
  record rejects only another component. That is the ADR-0016 rule and is unchanged.

## Alternatives considered

- **Filter in `config.GetTransfersForFeature`:** it would make the association
  lookup depend on the enabled set and hide inactive transfers from the status
  read and the rollback record readers, which must stay fail closed.
- **Treat a requisite-only association as "belongs to the feature" for disable:**
  `RequisiteFeatures=name` means the transfer needs `name`, so disabling `name`
  makes it inactive and it is removed, as before.
- **Remove a transfer on `disable --now` even when another enabled feature
  activates it:** the other feature would keep a link to a removed image, or its
  next `update` would download it again.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [specs/config-reference.md](../specs/config-reference.md),
  [specs/sdk-api.md](../specs/sdk-api.md)
- Builds on: [ADR-0016](0016-automatic-sysext-updates-apply-at-next-boot.md),
  [ADR-0002](0002-skip-non-sysext-transfers.md)
