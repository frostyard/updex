# Reporter Migration Design

Migrate from `github.com/frostyard/pm/progress` to `github.com/frostyard/std/reporter`.

## Decisions

- Remove `ProgressHelper` — call reporter directly via thin private methods on `Client`
- SDK accepts `reporter.Reporter` interface directly (no local interface)
- JSON mode uses `NoopReporter` (suppress progress, output final results only)
- `NewClient` defaults nil reporter to `reporter.NoopReporter{}`

## Dependencies

- **Add:** `github.com/frostyard/std/reporter`
- **Remove:** `github.com/frostyard/pm/progress`

## SDK Changes (`updex/`)

### `updex/updex.go`

Replace `helper *progress.ProgressHelper` with `reporter reporter.Reporter` on `Client`.
Change `ClientConfig.Progress` from `progress.ProgressReporter` to `reporter.Reporter`.
In `NewClient`, store reporter directly; default nil to `reporter.NoopReporter{}`.

### Private helper methods on `Client`

```go
func (c *Client) step(n, total int, name string)  { c.reporter.Step(n, total, name) }
func (c *Client) msg(format string, a ...any)     { c.reporter.Message(format, a...) }
func (c *Client) warn(format string, a ...any)    { c.reporter.Warning(format, a...) }
func (c *Client) errMsg(err error, msg string)    { c.reporter.Error(err, msg) }
```

### Call translation (`features.go`, `install.go`)

| Old | New |
|-----|-----|
| `c.helper.BeginAction("X")` | `c.step(n, total, "X")` |
| `c.helper.EndAction()` | remove |
| `c.helper.BeginTask("X")` | `c.msg("X")` or `c.step(...)` |
| `c.helper.EndTask()` | remove |
| `c.helper.Info("X")` | `c.msg("X")` |
| `c.helper.Warning("X")` | `c.warn("X")` |

## CLI Changes (`cmd/`)

### Delete `cmd/common/reporter.go`

The `TextReporter` now lives in `std/reporter`.

### Update `cmd/commands/components.go`

```go
func newClient() *updex.Client {
    var r reporter.Reporter
    if !common.JSONOutput {
        r = reporter.NewTextReporter(os.Stderr)
    }
    return updex.NewClient(updex.ClientConfig{
        Definitions: common.Definitions,
        Verify:      common.Verify,
        Progress:    r,
    })
}
```

## Documentation Updates

Update `.planning/codebase/ARCHITECTURE.md` and `STRUCTURE.md` references to the old reporter/progress package.
