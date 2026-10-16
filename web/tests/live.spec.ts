import { expect, open, reloadAndSettle, row, SAMPLE, test } from "./harness";

test("an outside write shows up without a reload", async ({ page, project }) => {
  await open(page, project);
  await page.evaluate(() => { (window as unknown as { marker: number }).marker = 1; });
  project.writeIssues([...SAMPLE, { id: "t-9", title: "[9] Written by someone else", priority: 1 }]);
  await expect(row(page, "t-9")).toBeVisible({ timeout: 10000 });
  expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBe(1);
});

test("the projects sheet lists the current project", async ({ page, project }) => {
  await open(page, project);
  await page.locator('[data-act="projects"]').click();
  await expect(page.locator('.msheet [data-sa="proj"]').first()).toBeVisible();
});

test("a reload settles on the selected issue", async ({ page, project }) => {
  await open(page, project);
  await row(page, "t-1").click();
  await expect(page).toHaveURL(/#\/.+\/t-1$/);
  const url = page.url();
  await page.evaluate(() => { (window as unknown as { marker: number }).marker = 1; });

  await reloadAndSettle(page, () => page.reload({ waitUntil: "commit" }));

  expect(await page.evaluate(() => document.readyState)).toBe("complete");
  expect(await page.evaluate(() => (window as unknown as { marker?: number }).marker)).toBeUndefined();
  await expect(page).toHaveURL(url);
  await expect(page.locator("body")).toHaveAttribute("data-live", "live");
  await expect(page.locator("#boot")).toBeHidden();
});
