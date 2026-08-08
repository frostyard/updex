# Pull Request Review Rubric

Use this rubric when reviewing changes to updex. Approve only when every
applicable item is satisfied; leave actionable feedback for any unmet item.

## Correctness

- The change solves the stated problem without unrelated modifications.
- Error paths and edge cases are handled without changing existing behavior
  unintentionally.
- Public SDK operations remain in `updex/`; CLI commands remain thin wrappers.

## Tests and Quality

- New or changed behavior has focused tests, including relevant failure cases.
- `make check` passes, or the pull request explains why a check is not
  applicable.
- Public APIs, user-facing behavior, and architecture documentation are updated
  where needed.

## Security

- Inputs, names, and paths are validated before filesystem or network use.
- Downloads retain hash and signature verification where applicable.
- Privileged file operations reject unsafe file types and cannot escape their
  intended directories.
- No secrets, credentials, or credential-bearing URLs are committed or exposed
  in errors.

## Maintainability

- The implementation follows existing Go conventions and uses standard library
  functionality where practical.
- Errors are wrapped with context, remain lowercase, and have no trailing
  punctuation.
- Dependencies, compatibility changes, and operational side effects are
  justified in the pull request.

## Review Outcome

- **Approve** when all applicable checks pass and CI is green.
- **Request changes** for correctness, security, compatibility, or missing-test
  issues.
- **Comment** for non-blocking suggestions that do not affect safe operation or
  maintainability.
