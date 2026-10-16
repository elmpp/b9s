#!/usr/bin/env node
// Reads a Playwright JSON report of a --repeat-each run and names every test
// that both passed and failed in it (a flake) and every test that failed in
// each repeat (a failure). Exits 1 when it names anything, 0 otherwise.
//
//   node scripts/playwright-flakes.mjs .b9s-e2e/flakes.json
//
// The nightly web-flakes job runs it (.github/workflows/nightly.yml); locally,
// PLAYWRIGHT_JSON_OUTPUT_NAME=flakes.json npx playwright test --repeat-each 5 --reporter json
// writes the input.

import fs from "node:fs";

const file = process.argv[2];
if (!file) {
  console.error("usage: playwright-flakes.mjs <report.json>");
  process.exit(2);
}
const report = JSON.parse(fs.readFileSync(file, "utf8"));

// One entry per (project, file, line, title path); each repeat adds a status.
const runs = new Map();
function walk(suite, titles) {
  for (const spec of suite.specs || []) {
    for (const t of spec.tests || []) {
      const key = `[${t.projectName}] › ${spec.file}:${spec.line} › ${[...titles, spec.title].join(" › ")}`;
      if (!runs.has(key)) runs.set(key, []);
      runs.get(key).push(t.status);
    }
  }
  for (const s of suite.suites || []) walk(s, s.title ? [...titles, s.title] : titles);
}
for (const s of report.suites || []) walk(s, []);

const flaky = [], failing = [];
for (const [key, statuses] of runs) {
  const bad = statuses.filter(s => s === "unexpected" || s === "flaky").length;
  const good = statuses.filter(s => s === "expected").length;
  if (bad && good) flaky.push(`${key}  (${bad}/${statuses.length} failed)`);
  else if (bad) failing.push(`${key}  (${bad}/${statuses.length} failed)`);
}

console.log(`${runs.size} tests, ${flaky.length} flaky, ${failing.length} failing`);
if (flaky.length) console.log("\nFLAKY (passed and failed in one run):\n  " + flaky.join("\n  "));
if (failing.length) console.log("\nFAILING (every repeat):\n  " + failing.join("\n  "));
console.log(`\n=== PLAYWRIGHT_FLAKES DONE tests=${runs.size} flaky=${flaky.length} failing=${failing.length} ===`);
process.exit(flaky.length || failing.length ? 1 : 0);
