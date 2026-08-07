# Contributing to updex

Thanks for your interest in improving updex! This guide covers everything you
need to get a change merged: environment setup, the architecture rules the
codebase follows, testing conventions, and what CI expects.

## Getting Started

### Prerequisites

- **Go 1.26.5 or newer** (the module targets `go 1.26.5`)
- `make`
- Optional: [`golangci-lint`](https://golangci-lint.run/) for linting,
  [`svu`](https://github.com/caarlos0/svu) for release tagging

updex manages [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html)
images, so end-to-end use requires a systemd-based Linux system and root
privileges. Building and running the unit tests does not — the test suite uses
`t.TempDir()` and mock runners instead of touching the real system.

### Build and test

```bash
make build        # build build/updex
make fmt          # gofmt all sources — run after every change
make lint         # golangci-lint (skipped with a message if not installed)
make test         # go test -v ./...
make check        # fmt + lint + test
make test-cover   # tests with an HTML coverage report
make tidy         # go mod tidy
```

Run a single test with the standard Go tooling:

```bash
go test -v -run TestName ./updex/
```

Run `make check` before opening a pull request.

## Architecture

updex is **SDK-first**: the real logic lives in public Go packages, and the
Cobra commands under `cmd/` are thin wrappers that parse flags, call the SDK,
and format output.

```
cmd/updex-cli/    Binary entry point
cmd/updex/        Cobra commands (features, components, catalog, daemon)
updex/            Public SDK: Client + option/result structs
catalog/          Sysext catalog primitives (.catalog repos, list/fetch/render)
config/           .transfer/.feature parsing, search paths, components, drop-ins
download/         HTTP download, SHA256 verification, decompression
manifest/         SHA256SUMS fetch/parse and GPG verification
version/          @v pattern matching and version comparison
sysext/           systemd-sysext integration, extension links, vacuum planning
systemd/          Timer/service unit generation and systemctl management
internal/testutil HTTP test server helpers
```

`yeti/OVERVIEW.md` is the detailed architecture document — read it (plus
`yeti/sdk-api.md` and `yeti/config-reference.md`) before making non-trivial
changes. The `yeti/` directory is written for AI agents and maintainers who
need decision rationale, not as user-facing documentation.

### Adding a new operation

1. Implement it in the SDK as a method on `Client` in `updex/<operation>.go`,
   taking `context.Context` first and a dedicated options struct second, and
   returning a dedicated result struct plus an error.
2. Add a thin Cobra wrapper in `cmd/updex/<operation>.go` that builds the
   options from flags, calls the SDK method, and formats the output.
3. Register the command with the root command in `cmd/updex/root.go`.
4. Update the documentation (see [Documentation](#documentation)).

## Code Conventions

- **The SDK must never import CLI packages.** No Cobra, no pflag, no
  `cmd/...` imports in `updex/` or the supporting packages.
- **Structured returns.** SDK methods return typed structs, never
  preformatted strings. Formatting belongs in `cmd/`.
- **Context first.** Every public SDK method takes `context.Context` as its
  first parameter.
- **Options structs.** New knobs go on the relevant `*Options` struct rather
  than as extra positional parameters or package-level state. Feature-scoped
  operations carry a `Component string` field resolved through
  `Client.loadDomain`.
- **Errors** are lowercase with no trailing punctuation and are wrapped with
  `fmt.Errorf("context: %w", err)`. Do not leak credentials or
  credential-bearing URLs into error messages.
- **Formatting** is plain `gofmt` (`make fmt`); CI fails on unformatted files.
- **Modern Go idioms**: `any` over `interface{}`; the `slices`, `maps`, and
  `cmp` packages; `slices.SortFunc`; `strings.SplitSeq`; `wg.Go()`; `omitzero`
  JSON tags for slice/map/struct fields.
- **Dependencies**: prefer the standard library. Only add a dependency when
  it is genuinely necessary, and run `make tidy` afterwards.

## Testing

- Table-driven tests with descriptive names; cover error paths, not just the
  happy path.
- Use `t.TempDir()` for filesystem work and `t.Context()` for contexts.
- Mock system commands through the `sysext.SysextRunner` and
  `systemd.SystemctlRunner` interfaces; inject them via
  `ClientConfig.SysextRunner` rather than mutating global state.
- Use `internal/testutil.NewTestServer()` for HTTP sources and manifests.
- Override package vars such as `config.SearchRoots`, `sysext.SysextDir`,
  `catalog.ConfigRoots`, and `catalog.CacheDir` instead of writing to real
  system paths, and restore them with `t.Cleanup`.
- Keep tests idempotent so they can run in parallel.

## Security

Changes that touch downloads, configuration parsing, or filesystem writes get
extra scrutiny:

- Always verify SHA256 hashes before installing, and keep GPG verification of
  `SHA256SUMS` working.
- Validate names and paths (see `catalog.ValidateSysextName`) so nothing
  traversal-shaped reaches `filepath.Join` or a URL.
- Use `os.Lstat` and reject non-regular files before writing to a managed
  definition path — `os.Stat` reports a dangling symlink as absent, which
  would let a root-privileged write escape its directory.
- Make multi-file writes recoverable: snapshot before writing and roll back on
  any failure, as `updex.Client.CatalogAdd` does.
- Never commit secrets or credentials.

If you believe you have found a security vulnerability, please report it
privately to the maintainers rather than opening a public issue.

## Documentation

A change is not complete until the documentation matches it. When behavior
changes, update:

- `README.md` — user-facing usage and flags
- `yeti/OVERVIEW.md`, `yeti/sdk-api.md`, `yeti/config-reference.md` — the
  agent-oriented architecture, SDK, and configuration references
- `CLAUDE.md` / `AGENTS.md` — when conventions or the build workflow change
- `docs/` — topic guides such as `docs/patterns.md`

Durable, non-obvious rationale that future contributors would otherwise have
to rediscover belongs in `yeti/learnings/`.

## Pull Requests

1. Fork the repository and create a branch off `main`.
2. Keep changes focused; unrelated fixes belong in a separate PR.
3. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit
   messages (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`) — the
   release tooling derives version bumps from them.
4. Run `make check` and make sure it passes.
5. Update the documentation and add tests for your change.
6. Open the PR with a description of what changed and why, and link any
   related issue.

CI runs on every pull request (`.github/workflows/test.yml`) and must pass:
lint (golangci-lint), security scan (`govulncheck`), unit tests with coverage,
race-detector tests, verification (`go mod tidy` cleanliness, `go vet`,
`gofmt`), and cross-compiled builds for linux/amd64 and linux/arm64.

Coverage from the unit test job is uploaded to Codecov. The gates are defined
in `codecov.yml`: the project coverage must not drop more than 1% against the
base commit, and changed lines in a pull request should be at least 70%
covered.

## Releases

Releases are tagged with semantic versions (`vMAJOR.MINOR.PATCH`) and built by
GoReleaser. Maintainers run `make bump`, which builds, tests, formats, lints,
and then tags the next version with `svu` from a clean working tree.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE) that covers this project.
