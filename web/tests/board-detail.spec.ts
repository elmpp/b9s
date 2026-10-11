import type { Page } from "@playwright/test";
import { expect, open, test } from "./harness";

async function tokenColor(page: Page, token: string): Promise<string> {
  return page.evaluate(t => {
    const el = document.createElement("span");
    el.style.color = `var(${t})`;
    document.body.append(el);
    const color = getComputedStyle(el).color;
    el.remove();
    return color;
  }, token);
}

test.use({
  fixture: {
    issues: [
      { id: "d-epic", title: "[d] Epic: Calm board detail", type: "epic", priority: 1, description: "The board groups work by epic lanes." },
      { id: "d-feature", title: "[d.1] Feature shell", type: "feature", parent: "d-epic", status: "in_progress", priority: 1 },
      { id: "d-blocker", title: "[d.2] Resolve relation", parent: "d-epic", status: "in_progress", priority: 1 },
      {
        id: "d-task",
        title: "[d.1.1] Render Markdown detail",
        parent: "d-feature",
        blockedBy: ["d-blocker"],
        priority: 1,
        labels: ["ui"],
        description: "### Markdown\n\nServe **Markdown** with `code` in the calm detail pane.\n\n```mermaid\ngraph LR\nA[Detail] --> B[Calm]\n```",
        comments: ["Comment text keeps the same neutral detail treatment."],
      },
    ],
  },
});

test("board cards, epic lanes and rich detail use the calm look in this size class", async ({ page, project }) => {
  await open(page, project);
  const muted = await tokenColor(page, "--muted");
  const text = await tokenColor(page, "--text");
  const textSoft = await tokenColor(page, "--text-soft");
  const dangerColor = await tokenColor(page, "--danger");
  const raised = await tokenColor(page, "--surface-raised");

  await page.locator('#bar [data-view="board"]').click();
  const allTab = page.locator('#tabs [data-tab="*"]');
  if (await allTab.count()) await allTab.click();
  const card = page.locator('[data-card="d-task"]');
  await expect(card).toBeVisible();
  await expect(card).toHaveCSS("background-color", raised);
  await expect(card.locator(".tags .tp")).toHaveCSS("color", muted);
  await expect.poll(() => card.locator(".st").evaluate(el => getComputedStyle(el).backgroundColor)).toBe(dangerColor);

  const lane = page.locator('.lane[data-lane="d-epic"], .wepic[data-epic="d-epic"]').first();
  await expect(lane).toBeVisible();
  await expect(lane).toHaveCSS("color", muted);

  await card.click();
  const detail = page.locator(".detail");
  await expect(detail).toHaveAttribute("data-id", "d-task");
  await expect(detail.locator(".md h3").first()).toHaveCSS("color", text);
  await expect(detail.locator(".md code").first()).toHaveCSS("color", textSoft);
  await expect(detail.locator(".mermaid-diagram svg")).toBeVisible();
  await expect(detail.locator('.rel[data-nav="d-feature"]')).toContainText("Feature shell");
  await expect(detail.locator('.rel[data-nav="d-blocker"]')).toContainText("Resolve relation");
  await expect(detail.locator(".cm", { hasText: "Comment text" })).toBeVisible();
  await expect(detail.locator(".cm .who b").first()).toHaveCSS("color", textSoft);
  await expect(detail.locator(".pill").filter({ hasText: /open/ })).toHaveCSS("color", muted);
  await expect.poll(() => detail.locator(".pill.status .sdot").evaluate(el => getComputedStyle(el).backgroundColor)).toBe(dangerColor);
});
