# Phase 1: Test Foundation - Context

**Gathered:** 2026-01-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Establish testing infrastructure and patterns so developers can write and run tests for core operations (list, check, update, install, remove) and config parsing (transfer files, feature files) without needing root privileges. Tests use mocked filesystem/systemd and include HTTP server mocking utilities.

</domain>

<decisions>
## Implementation Decisions

### Mocking strategy
- Claude's discretion on filesystem mocking approach (interface abstraction vs temp directories)
- Claude's discretion on systemd mocking approach (interface abstraction vs exec mocking)
- Claude's discretion on mock strictness (strict vs lenient per situation)
- Claude's discretion on mock composition patterns (fixtures vs individual setup)

### Test organization
- Claude's discretion on test file location (co-located vs separate directory)
- Claude's discretion on test helper organization
- Fixtures live in `testdata/` directories (Go convention)
- Claude's discretion on table-driven vs individual test functions

### HTTP mocking for registries
- Registry is a plain HTTP server (not OCI/Docker registry)
- Claude's discretion on HTTP mocking approach (httptest.Server, client interface, or recorded fixtures)
- Claude's discretion on response realism vs maintainability
- Tests must cover HTTP error scenarios (timeouts, 404s, malformed responses)

### Coverage expectations
- Claude's discretion on coverage depth per operation
- Track coverage metrics but don't enforce thresholds in CI
- Claude's discretion on config parsing test thoroughness
- Claude will check for existing tests and decide whether to preserve/migrate/replace

### Claude's Discretion
- Filesystem mocking approach
- Systemd mocking approach
- Mock strictness levels
- Mock composition patterns
- Test file location
- Test helper organization
- Table-driven vs individual tests
- HTTP mocking approach
- Response realism balance
- Coverage depth per operation
- Config parsing test thoroughness
- Handling of any existing tests

</decisions>

<specifics>
## Specific Ideas

- Registry is a plain HTTP server, not OCI-compliant — don't over-engineer registry mocking
- Fixtures in `testdata/` directories following Go conventions

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 01-test-foundation*
*Context gathered: 2026-01-26*
