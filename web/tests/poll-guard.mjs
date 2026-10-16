import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
// Browser specs and their guard must agree on TypeScript syntax.
const { babelParse, traverse, types } = require(path.join(
  path.dirname(require.resolve("playwright/package.json")), "lib/transform/babelBundle.js",
));

function memberCall(node, object, property) {
  const callee = node.callee;
  return (types.isMemberExpression(callee) || types.isOptionalMemberExpression(callee))
    && !callee.computed
    && types.isIdentifier(callee.object, { name: object })
    && types.isIdentifier(callee.property, { name: property });
}

function unsafePollOffsets(source, filename = "fixture.ts") {
  const offsets = [];
  const ast = babelParse(source, filename, true);
  traverse(ast, {
    "CallExpression|OptionalCallExpression"(poll) {
      if (!memberCall(poll.node, "expect", "poll")) return;
      let unsafe = false;
      poll.traverse({
        "CallExpression|OptionalCallExpression"(call) {
          if (memberCall(call.node, "page", "evaluate")) unsafe = true;
        },
      });
      if (unsafe) offsets.push(poll.node.start);
    },
  });
  return offsets;
}

const fixtures = [
  {
    name: "original",
    source: 'await expect.poll(() => page.evaluate(() => getComputedStyle(document.body).outlineColor), { timeout: 10000 }).toBe("rgb(1, 2, 3)");',
    want: 1,
  },
  {
    name: "multiline",
    source: `await expect.poll(async () => {
  return page.evaluate(() => getComputedStyle(document.body).outlineColor);
}).toBe("rgb(1, 2, 3)");`,
    want: 1,
  },
  {
    name: "nestedAssertion",
    source: 'await expect.poll(async () => { expect(page.url()).toContain("#/project/t-1"); return page.evaluate(() => getComputedStyle(document.body).outlineColor); }).toBe("rgb(1, 2, 3)");',
    want: 1,
  },
  {
    name: "splitAssertion",
    source: `const assertion = expect.poll(() => page.evaluate(() => document.title));
await assertion.toBe("b9s");`,
    want: 1,
  },
  {
    name: "locatorEvaluation",
    source: 'await expect.poll(() => page.locator("body").evaluate(el => getComputedStyle(el).outlineColor)).toBe("rgb(1, 2, 3)");',
    want: 0,
  },
  {
    name: "evaluationOutsidePoll",
    source: `await expect.poll(() => page.url()).toContain("#/");
expect(await page.evaluate(() => document.title)).toBe("b9s");`,
    want: 0,
  },
  {
    name: "quotedAndCommentedDelimiters",
    source: `await expect.poll(async () => {
  const label = ')'; // )
  /* ) */ return page.evaluate(() => document.title + label);
}).toBe("b9s)");`,
    want: 1,
  },
  {
    name: "evaluationTextOnly",
    source: 'await expect.poll(() => { /* page.evaluate(() => document.title) */ return "page.evaluate(() => document.title)"; }).toBe("b9s");',
    want: 0,
  },
  {
    name: "evaluationAfterSplitPoll",
    source: `const assertion = expect.poll(() => page.url());
const title = await page.evaluate(() => document.title);
await assertion.toContain("#/");`,
    want: 0,
  },
  {
    name: "templateInterpolation",
    source: 'await expect.poll(async () => `${await page.evaluate(() => document.title)}`).toBe("b9s");',
    want: 1,
  },
  {
    name: "regexDelimiter",
    source: 'await expect.poll(async () => { const close = /\\)/; return page.evaluate(() => document.title); }).toBe("b9s");',
    want: 1,
  },
  {
    name: "nestedTemplateInterpolation",
    source: 'await expect.poll(async () => `${({}).title || `${await page.evaluate(() => document.title)}`}`).toBe("b9s");',
    want: 1,
  },
  {
    name: "regexCharacterClass",
    source: 'await expect.poll(async () => { const close = /[)]/; return page.evaluate(() => document.title); }).toBe("b9s");',
    want: 1,
  },
  {
    name: "templateTextOnly",
    source: 'await expect.poll(() => `page.evaluate(() => document.title)`).toBe("b9s");',
    want: 0,
  },
];

for (const { name, source, want } of fixtures) {
  test(name, { timeout: 5000 }, () => {
    const offsets = unsafePollOffsets(source);
    assert.equal(offsets.length, want, source);
    if (want) assert.equal(offsets[0], source.indexOf("expect.poll"));
  });
}

test("invalid syntax fails the guard", { timeout: 5000 }, () => {
  assert.throws(() => unsafePollOffsets("expect.poll(() =>"));
});

test("browser sources avoid page.evaluate inside expect.poll", { timeout: 15000 }, () => {
  const dir = path.dirname(fileURLToPath(import.meta.url));
  const violations = [];
  for (const relative of fs.readdirSync(dir, { recursive: true })) {
    if (!/\.[cm]?[jt]sx?$/.test(relative)) continue;
    const filename = path.join(dir, relative);
    if (!fs.statSync(filename).isFile()) continue;
    const source = fs.readFileSync(filename, "utf8");
    for (const offset of unsafePollOffsets(source, filename)) {
      const line = source.slice(0, offset).split("\n").length;
      violations.push(`${filename}:${line}: page.evaluate inside expect.poll can lose its execution context during navigation. Use reloadAndSettle before reading page state, or a locator assertion.`);
    }
  }
  assert.deepEqual(violations, [], violations.join("\n"));
});
