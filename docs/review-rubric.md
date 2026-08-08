# Pull Request Review Rubric

Use this rubric to keep reviews consistent, actionable, and focused on risks
introduced by the pull request.

## Review Gates

A pull request is ready to approve when all applicable gates pass:

1. **Risk classification**
   - The author selected the highest applicable tier from the
     [change risk guide](risk-tiers.md) and provided a rationale.
   - The tests, analysis, documentation, and oversight required for that tier
     are present.
2. **Correctness and scope**
   - The change solves the linked problem and handles relevant error cases.
   - The diff is focused; unrelated refactors and generated artifacts are absent.
3. **Architecture and API**
   - Business logic is implemented in the public SDK, with CLI code kept as a
     thin wrapper.
   - Public APIs use contexts, option structs, structured results, and compatible
     behavior unless a breaking change is intentional and documented.
4. **Security and reliability**
   - Inputs, paths, downloaded content, and managed files are validated safely.
   - Errors do not expose credentials, and privileged filesystem operations are
     resistant to traversal and symlink attacks.
   - Multi-step mutations leave a consistent state on failure.
5. **Tests and verification**
   - New or changed behavior has focused tests, including meaningful failure
     paths, or the pull request explains why tests are not applicable.
   - `make check` passes, including formatting, linting, and tests.
6. **Documentation and maintainability**
   - User-facing and agent-oriented documentation reflects behavior changes.
   - Code follows repository conventions and is understandable without
     unnecessary complexity.

## Review Feedback

Label findings by impact:

- **Blocking:** A correctness, security, compatibility, data-loss, or required
  test/documentation issue that must be resolved before approval.
- **Non-blocking:** A worthwhile improvement that does not prevent merging.
- **Question:** A request for context or clarification, not an assumed defect.
- **Nit:** An optional minor style suggestion; avoid nits already enforced by
  automated formatting or linting.

Comments should identify the affected behavior, explain its impact, and suggest
a concrete resolution when possible. Reviewers should re-check resolved
blocking findings and confirm required CI checks pass before approval.
