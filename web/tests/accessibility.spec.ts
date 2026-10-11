import AxeBuilder from "@axe-core/playwright";
import type { Page } from "@playwright/test";
import { sizeClassOf } from "../src/util";
import { expect, open, row, test, type FixtureIssue } from "./harness";

const axeTags = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"];

const fixture: FixtureIssue[] = [
  { id: "a-epic", title: "[a] Epic: Accessible responsive web", type: "epic", priority: 1, labels: ["ui"] },
  { id: "a-feature", title: "[a.1] Feature keyboard shell", type: "feature", parent: "a-epic", status: "in_progress", priority: 1, assignee: "alice" },
  { id: "a-blocker", title: "[a.2] Resolve blocker", parent: "a-epic", status: "blocked", priority: 1 },
  {
    id: "a-task",
    title: "[a.1.1] Add contrast and label checks",
    parent: "a-feature",
    blockedBy: ["a-blocker"],
    priority: 1,
    labels: ["accessibility"],
    description: "### Accessibility\n\nEvery size class keeps labelled controls, visible focus and AA contrast.",
    comments: ["Keyboard-only users can reach the same commands."],
  },
  { id: "a-done", title: "[a.1.2] Closed reference task", parent: "a-feature", status: "closed", priority: 3 },
];

test.use({ fixture: { issues: fixture } });

function formatViolations(label: string, violations: Awaited<ReturnType<AxeBuilder["analyze"]>>["violations"]): string {
  if (!violations.length) return "";
  return [label, ...violations.map(v => {
    const nodes = v.nodes.slice(0, 3).map(n => `  ${n.target.join(", ")}: ${n.failureSummary || n.html}`);
    return `${v.id} (${v.impact || "unknown"}): ${v.help}\n${nodes.join("\n")}`;
  })].join("\n\n");
}

async function expectNoAxeViolations(page: Page, label: string): Promise<void> {
  const results = await new AxeBuilder({ page }).include("#app").withTags(axeTags).analyze();
  expect(results.violations, formatViolations(label, results.violations)).toEqual([]);
}

async function closeDetail(page: Page): Promise<void> {
  const close = page.locator('.detail [data-act="dclose"]').first();
  if (await close.count()) await close.click();
  await expect(page.locator(".detail")).toHaveCount(0);
}

test("tree, board, detail, search and more pass WCAG checks in this size class", async ({ page, project }) => {
  await open(page, project);
  const cls = sizeClassOf(page.viewportSize()!.width);
  await expect(page.locator("#app")).toHaveClass(new RegExp(`(^| )${cls}( |$)`));
  await expectNoAxeViolations(page, `${cls}: tree`);

  await row(page, "a-task").click();
  await expect(page.locator(".detail")).toHaveAttribute("data-id", "a-task");
  await expectNoAxeViolations(page, `${cls}: detail`);
  await closeDetail(page);

  await page.locator('#bar [data-view="board"]').click();
  await expect(page.locator(cls === "phone" ? "#bcol" : "#wboard")).toBeVisible();
  await expectNoAxeViolations(page, `${cls}: board`);

  await page.locator('#bar [data-view="search"]').click();
  await expect(page.locator("#sIn")).toBeVisible();
  await expectNoAxeViolations(page, `${cls}: search`);

  await page.locator('#bar [data-view="more"]').click();
  await expect(page.locator("#moreList")).toBeVisible();
  await expectNoAxeViolations(page, `${cls}: more`);
});

test("keyboard-only commands keep named controls and visible focus", async ({ page, project }) => {
  await open(page, project);
  await expect(page.locator('#bar [data-view="tree"]')).toHaveAccessibleName("Tree");
  await expect(page.locator('#bar [data-view="board"]')).toHaveAccessibleName("Board");
  await expect(page.locator('#bar [data-view="search"]')).toHaveAccessibleName("Search");
  await expect(page.locator('#hd [data-act="create"]')).toHaveAccessibleName("Create issue");
  await expect(page.locator("#hdot")).toHaveAccessibleName("Data source health");

  await page.keyboard.press("j");
  await page.keyboard.press("j");
  await page.keyboard.press("j");
  await expect(page.locator("#treeRows .row.sel")).toHaveAttribute("data-id", "a-task");
  await expect(row(page, "a-task").locator(".rc")).toHaveCSS("box-shadow", /rgb/);
  await page.keyboard.press("Enter");
  await expect(page.locator(".detail")).toHaveAttribute("data-id", "a-task");
  await expect(page.locator('.detail [data-act="edit"]')).toHaveAccessibleName("Edit");
  await expect(page.locator('.detail [data-act="actions"]')).toHaveAccessibleName("More");
});
