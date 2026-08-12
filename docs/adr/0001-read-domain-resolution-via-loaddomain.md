# 0001 — Resolve every operation's read scope through loadDomain

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

updex reads `.feature`/`.transfer` definitions from three possible scopes: a
`-C`/`--definitions` directory given verbatim, a single named
systemd-sysupdate component (`--component`, sysupdate.d(5) "Components"), or
the default union of the legacy `sysupdate.d/` directory plus every
discovered component. Before component support, each SDK method loaded its
own configuration; adding a second scoping axis to each method independently
would have let precedence and conflict rules drift between operations, and
made "which files does this command see?" answerable only per method. The
SDK is also consumed as a library (pilothouse, snosi scripts), where
package-level flag state would leak between concurrent clients.

## Decision

Every SDK method resolves its read domain through the single unexported
`Client.loadDomain(component string)` (`updex/domain.go`), with a fixed
precedence:

1. `ClientConfig.Definitions` set → exactly that one directory, loaded
   verbatim with no component concept; `component` must be empty, otherwise
   `loadDomain` errors ("cannot combine --definitions with --component").
2. `component` non-empty → only that named component's own search paths
   (`config.LoadComponentFeatures`/`LoadComponentTransfers`).
3. Otherwise → the union of the legacy default directory and every
   discovered component (`config.LoadAllFeatures`/`LoadAllTransfers`); name
   collisions resolve to the most specific source and surface as warnings
   through the client's reporter, never as errors.

Scoping travels as a `Component string` field on each per-operation options
struct, never as package-level SDK state. Catalog operations, which are
component-scoped by construction, reject a `Definitions` override at the
same choke point (`Client.catalogRepos`, `updex/catalog.go`).

## Consequences

- Precedence and the `-C`-vs-`--component` conflict are enforced in one
  place; a new SDK method gets correct scoping by calling `loadDomain`.
- Multiple `Client` instances with different scopes coexist in one process.
- Every options struct for a feature-scoped operation must carry
  `Component`, which is mild boilerplate (`FeaturesOptions` had to become a
  variadic parameter on `Features` to keep the old signature compiling).
- A caller cannot combine an explicit definitions directory with component
  discovery, even when that might seem convenient; the union scope is only
  available as the default.

## Alternatives considered

- **Per-method loading (status quo ante):** each method growing its own
  component handling would duplicate the precedence rules and let them
  drift; rejected after component support made scope a three-way choice.
- **Package-level configuration (global flags/vars):** simpler wiring for
  the CLI, but leaks state between library consumers and concurrent
  clients; the SDK deliberately keeps all state on `Client`.
- **Allowing `-C` plus `--component` together:** there is nothing coherent
  for it to mean — a definitions override is a flat directory with no
  components — so the combination is rejected loudly instead of guessed at.

## References

- Implements: [`updex/domain.go`](../../updex/domain.go) (`loadDomain`),
  [`updex/catalog.go`](../../updex/catalog.go) (`catalogRepos`),
  [`config/component.go`](../../config/component.go)
  (`ComponentSearchPaths`, `LoadAll*` union)
- Shapes: [design/overview.md — Components](../design/overview.md#components-configcomponentgo),
  [specs/sdk-api.md — Component scoping](../specs/sdk-api.md#component-scoping),
  [specs/config-reference.md — Components](../specs/config-reference.md#components)
- Builds on: [core ADR-0004 — Product-namespaced filesystem paths, split by lifetime tier](https://github.com/frostyard/core/blob/main/docs/adr/0004-product-namespaced-filesystem-tiers.md)
