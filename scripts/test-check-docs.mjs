#!/usr/bin/env node
// Fixture-based self-test for scripts/check-docs.mjs (ADR-0012): proves a
// valid isolated fixture passes and that an unindexed category document, a
// dead relative Markdown link, and a broken or repository-escaping symlink
// each fail with their expected diagnostic. Zero dependencies; Node >= 20.
import { mkdtempSync, mkdirSync, writeFileSync, symlinkSync, rmSync, cpSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const realChecker = join(scriptsDir, "check-docs.mjs");

const THRESHOLDS = JSON.stringify({
  never_relax: true,
  docs_index_coverage: 1.0,
  link_integrity: 1.0,
  symlink_resolution: 1.0,
});

const workDir = mkdtempSync(join(tmpdir(), "check-docs-test-"));
let failures = 0;

function buildFixture(name) {
  const root = join(workDir, name);
  mkdirSync(join(root, "scripts"), { recursive: true });
  cpSync(realChecker, join(root, "scripts", "check-docs.mjs"));
  writeFileSync(join(root, ".coverage-thresholds.json"), THRESHOLDS);

  for (const cat of ["adr", "design", "specs", "plans"]) {
    mkdirSync(join(root, "docs", cat), { recursive: true });
  }
  mkdirSync(join(root, ".agents", "skills", "example"), { recursive: true });
  writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "# Example skill\n");
  writeFileSync(join(root, "docs", "adr", "0001-example.md"), "# ADR 0001\n\nExample decision.\n");
  writeFileSync(join(root, "docs", "design", "example.md"), "# Design\n\nExample design doc.\n");
  writeFileSync(
    join(root, "docs", "specs", "pr-acceptance-metric.md"),
    "# PR Acceptance Metric\n\n## Definition\n\nExample.\n\n## Rules\n\nExample.\n",
  );
  writeFileSync(join(root, "docs", "plans", "example.md"), "# Plan\n\nExample plan.\n");
  writeFileSync(
    join(root, "docs", "README.md"),
    ["# Docs index", "", "- adr/0001-example.md", "- design/example.md", "- specs/pr-acceptance-metric.md", "- plans/example.md", ""].join(
      "\n",
    ),
  );
  return root;
}

function run(root) {
  return spawnSync(process.execPath, [join(root, "scripts", "check-docs.mjs")], {
    cwd: root,
    encoding: "utf8",
  });
}

function assertCase(name, mutate, wantCode, wantDiagnostic) {
  const root = buildFixture(name);
  mutate(root);
  const result = run(root);
  const output = `${result.stdout}\n${result.stderr}`;
  const codeOk = result.status === wantCode;
  const diagOk = wantDiagnostic === null || output.includes(wantDiagnostic);
  if (codeOk && diagOk) {
    console.log(`PASS: ${name}`);
  } else {
    failures++;
    console.log(`FAIL: ${name} (exit ${result.status}, want ${wantCode}; diagnostic ${diagOk ? "found" : "missing"})`);
    console.log(output);
  }
}

assertCase("valid-fixture-passes", () => {}, 0, null);

assertCase(
  "unindexed-category-document-fails",
  (root) => {
    writeFileSync(join(root, "docs", "adr", "0002-orphan.md"), "# ADR 0002\n\nOrphan.\n");
  },
  1,
  "index: docs/adr/0002-orphan.md has no line in docs/README.md",
);

assertCase(
  "dead-relative-link-fails",
  (root) => {
    writeFileSync(join(root, "docs", "design", "example.md"), "# Design\n\nExample design doc.\n\n[dead](./nonexistent.md)\n");
  },
  1,
  "link: docs/design/example.md -> ./nonexistent.md does not resolve",
);

assertCase(
  "escaped-backticks-guard-real-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "\\` [x](missing.md) \\`\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "spans-match-by-backtick-run-length",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "`` a ` `` [x](missing.md)\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "unclosed-double-backtick-run-guards-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "`` [x](missing.md) `\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "adjacent-headings-unpaired-backticks-guard-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "# First `\n# [x](missing.md) `\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "multiline-span-guards-link-outside-pair",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "`code\ncontinued` [x](missing.md) `tail`\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "standalone-gfm-table-row-guards-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "| ` | [x](missing.md) ` |\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "gfm-table-header-delimiter-guards-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "| Code | Link |\n| --- | --- |\n| ` | [x](missing.md) ` |\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "raw-html-backtick-guards-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "<span title=\"`\">[x](missing.md)`</span>\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "email-autolink-backtick-guards-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "<1`@example.com> [x](missing.md) `\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "code-span-in-link-text-does-not-hide-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "[`code`](missing.md)\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "go-generic-calls-in-code-spans-pass",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "- `errors.AsType[T](err)`\n\n`reflect.TypeAssert[T](v)`\n");
  },
  0,
  null,
);

assertCase(
  "double-backtick-span-exact-run-length",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "`` a ` [x](missing.md) ``\n");
  },
  0,
  null,
);

assertCase(
  "less-than-without-greater-than-permits-code-span",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "Go <1.26 and `errors.AsType[T](err)`\n");
  },
  0,
  null,
);

assertCase(
  "naive-fence-removal-keeps-cross-fence-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "` ```x` y``` [l](missing.md) `\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "list-item-fence-does-not-hide-later-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "- ```\n  code\n  ```\n[x](missing.md)\n\n```\nz\n```\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "link-destination-backtick-is-not-span-delimiter",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "[a](https://example.com/a`b) [x](missing.md) `tail\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "link-title-backtick-is-not-span-delimiter",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "[a](u \"t`\") [x](missing.md) `tail\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "full-reference-label-backtick-is-not-span-delimiter",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "[a][b`c] [x](missing.md) `tail\n\n[b`c]: https://example.com\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "unicode-blank-line-does-not-hide-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "`head\n\u00a0\nend` [x](missing.md) `tail\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "lone-cr-line-endings-do-not-hide-link",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "`head\r\r[x](missing.md) `tail\r");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "crlf-line-endings-permit-code-span",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "Use `errors.AsType[T](err)` here.\r\n");
  },
  0,
  null,
);

assertCase(
  "gfm-www-autolink-consumes-backtick",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "www.example.com/a`b [x](missing.md) `tail\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "gfm-https-autolink-consumes-backtick",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "https://example.com/a`b [x](missing.md) `tail\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "inline-math-consumes-backtick",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "$a`$ [x](missing.md) `tail\n");
  },
  1,
  "-> missing.md does not resolve",
);

assertCase(
  "autolink-and-go-generic-markers-in-code-spans-pass",
  (root) => {
    writeFileSync(join(root, ".agents", "skills", "example", "SKILL.md"), "See `https://example.com/x` and `errors.AsType[T](err)`.\n");
  },
  0,
  null,
);

assertCase(
  "broken-symlink-fails",
  (root) => {
    symlinkSync(join(root, "nonexistent-target.md"), join(root, "broken-link.md"));
  },
  1,
  "symlink: broken-link.md is broken",
);

assertCase(
  "escaping-symlink-fails",
  (root) => {
    const outside = join(workDir, "outside-target.md");
    writeFileSync(outside, "# Outside\n");
    symlinkSync(outside, join(root, "escaping-link.md"));
  },
  1,
  "escapes the repo",
);

rmSync(workDir, { recursive: true, force: true });

if (failures > 0) {
  console.error(`test-check-docs: ${failures} assertion(s) failed`);
  process.exit(1);
}
console.log("test-check-docs: all assertions passed");
