# Change Risk Tiers

Every pull request must declare one risk tier. The classification determines
the review evidence and scrutiny needed in addition to the repository's normal
CI and review gates.

Choose the highest tier whose criteria match any part of the change. Consider
blast radius, privileges and trust boundaries, reversibility, compatibility,
and release or supply-chain impact. When uncertain, choose the higher tier.
Reviewers may reclassify a pull request if its scope or risk changes.

## Tier 1: Low

Changes that do not alter runtime behavior or security boundaries.

Examples include:

- documentation, comments, spelling, or formatting;
- test-only changes that do not modify production behavior;
- mechanical refactoring with equivalent behavior; and
- repository metadata that does not change CI permissions or release behavior.

**Required evidence:** Describe the scope, explain why behavior is unchanged,
and complete the applicable standard checks. If tests are not applicable, say
why in the pull request.

## Tier 2: Moderate

Changes to normal product behavior with limited, understood impact and no
security-sensitive or privileged boundary changes.

Examples include:

- compatible SDK or CLI behavior changes;
- ordinary bug fixes and feature additions;
- configuration parsing or output changes that do not affect a trust boundary;
- changes spanning multiple packages; and
- routine dependency updates that do not execute in a privileged build or
  release path.

**Required evidence:** Add focused success and failure-path tests, update
affected user and architecture documentation, and describe compatibility,
failure, and rollback considerations. A maintainer must confirm that the
classification and evidence are appropriate.

## Tier 3: High

Changes that affect a security control, trust boundary, privileged operation,
destructive action, broad compatibility contract, or software supply chain.

Examples include:

- SHA256 or GPG verification, download trust, or manifest handling;
- untrusted input validation, paths, symlinks, archives, or privileged
  filesystem writes;
- secrets, credentials, GitHub Actions permissions, release automation, or
  artifact provenance;
- destructive operations or multi-step mutations that could leave inconsistent
  state;
- breaking SDK, CLI, or configuration changes; and
- changes responding to a suspected vulnerability.

**Required evidence:** Obtain explicit maintainer review of the security and
reliability impact. Document trust boundaries, abuse and failure modes,
compatibility impact, and rollback or recovery. Include focused adversarial and
failure-path tests. The author or generating agent must not self-approve,
auto-merge, weaken required checks, or disclose vulnerability details in a
public pull request.

## Classification workflow

1. The author selects one tier and gives a short rationale in the pull request
   template.
2. Reviewers verify the tier before approval and apply the corresponding
   requirements above.
3. The author updates the tier and evidence if the change grows in scope.
4. Required CI, coverage, and review gates still apply at every tier. A lower
   tier never exempts a change from a gate or overrides a higher-risk criterion.

Use private maintainer reporting channels for suspected vulnerabilities rather
than opening a public issue or pull request.
