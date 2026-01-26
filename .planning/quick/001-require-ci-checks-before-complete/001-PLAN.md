---
phase: quick-001
plan: 01
type: execute
wave: 1
depends_on: []
files_modified: [".planning/PROJECT.md"]
autonomous: true

must_haves:
  truths:
    - "PROJECT.md documents CI check requirement before completion"
    - "All six GitHub Actions jobs are listed as required"
  artifacts:
    - path: ".planning/PROJECT.md"
      provides: "CI verification constraint"
      contains: "CI verification"
  key_links: []
---

<objective>
Add CI verification constraint to PROJECT.md requiring all GitHub Actions "Tests" workflow jobs to pass before any work is considered complete.

Purpose: Prevent incomplete work (like missing `go mod tidy`) from being marked done when CI would catch the issue.
Output: Updated PROJECT.md with explicit CI check requirement.
</objective>

<execution_context>
@~/.config/opencode/get-shit-done/workflows/execute-plan.md
@~/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.github/workflows/test.yml
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add CI verification constraint to PROJECT.md</name>
  <files>.planning/PROJECT.md</files>
  <action>
Add a new constraint to the Constraints section of PROJECT.md after the existing constraints.

Add this constraint:

```
- **CI verification**: All GitHub Actions "Tests" workflow jobs must pass before work is complete:
  - lint (golangci-lint)
  - security (govulncheck)
  - verify (go mod tidy, go vet, gofmt)
  - unit-test (go test with coverage)
  - race-test (go test -race)
  - build (cross-compile linux/amd64, linux/arm64)
```

Update the "Last updated" timestamp at the bottom to today's date with the note "after adding CI constraint".
  </action>
  <verify>
Read .planning/PROJECT.md and confirm:
1. CI verification constraint is present with all 6 jobs listed
2. Timestamp is updated
  </verify>
  <done>PROJECT.md contains CI verification constraint requiring all 6 GitHub Actions jobs to pass</done>
</task>

</tasks>

<verification>
- [ ] PROJECT.md Constraints section includes CI verification requirement
- [ ] All 6 workflow jobs are enumerated (lint, security, verify, unit-test, race-test, build)
- [ ] Last updated timestamp reflects this change
</verification>

<success_criteria>
PROJECT.md clearly documents that all GitHub "Tests" workflow checks must pass before any work is considered complete, preventing future misses like the `go mod tidy` issue.
</success_criteria>

<output>
After completion, create `.planning/quick/001-require-ci-checks-before-complete/001-SUMMARY.md`
</output>
