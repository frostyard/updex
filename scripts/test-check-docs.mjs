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
