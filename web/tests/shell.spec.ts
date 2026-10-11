// The app shell per size class (docs/adr/0052). util.ts puts the class of the
// app's width on #app: a phone reaches the views from a bottom bar and opens
// the detail as a sheet, every other class has a rail at the left and a side
// panel. A rotation or a resized window keeps the place: the view, the
// selection, the list's scroll and the open detail with its own scroll.

import type { Page } from "@playwright/test";
import { sizeClassOf } from "../src/util";
import { expect, open, row, SAMPLE, test, type FixtureIssue } from "./harness";

const box = async (page: Page, sel: string) => (await page.locator(sel).boundingBox())!;

/** classOn waits for #app to carry the class of the viewport's width, and returns it. */
async function classOn(page: Page): Promise<string> {
  const want = sizeClassOf(page.viewportSize()!.width);
  await expect(page.locator("#app")).toHaveClass(new RegExp(`(^| )${want}( |$)`));
  return want;
}

test("#app carries the class of its width, with the bar at the bottom on a phone and a rail elsewhere", async ({ page, project }) => {
  await open(page, project);
  const cls = await classOn(page);
  const app = await box(page, "#app"), bar = await box(page, "#bar"), tree = await box(page, "#treeList");
  if (cls === "phone") {
    expect(bar.width).toBeGreaterThanOrEqual(app.width - 1);
    expect(bar.y + bar.height).toBeGreaterThanOrEqual(app.height - 1);
    expect(tree.y + tree.height).toBeLessThanOrEqual(bar.y + 1);
  } else {
    expect(bar.x).toBeLessThanOrEqual(app.x + 1);
    expect(bar.width).toBeLessThan(app.width / 4);
    expect(bar.height).toBeGreaterThan(app.height * 0.7);
    expect(tree.x).toBeGreaterThanOrEqual(bar.x + bar.width - 1);
  }
  // A touch class keeps every view button a 44 px target.
  if (cls === "phone" || cls === "tablet") {
    for (const b of await page.locator("#bar button").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  }
  await page.locator('#bar [data-view="board"]').click();
  await expect(page.locator(cls === "phone" ? "#bcol" : "#wboard")).toBeVisible();
  await expect(page.locator(cls === "phone" ? "#wboard" : "#bcol")).toHaveCount(0);
});

const LONG = Array.from({ length: 60 }, (_, n) => `Paragraph ${n + 1} of a description long enough that the detail scrolls in every class.`).join("\n\n");
const MANY: FixtureIssue[] = [...SAMPLE, ...Array.from({ length: 60 }, (_, n) => ({ id: `m-${n + 1}`, title: `[m${n + 1}] Row ${n + 1}`, priority: 3, description: LONG }))];

test.describe("a long tree and a long detail", () => {
  test.use({ fixture: { issues: MANY } });

  test("a rotation and a resize keep the selection, the scroll and the open detail", async ({ page, project }) => {
    await open(page, project);
    const start = page.viewportSize()!;
    const list = page.locator("#treeList"), r = row(page, "m-30"), d = page.locator(".detail");
    // The row goes 80 px below the top of the list, where a shorter list still shows it.
    const rowTop = await r.evaluate(el => (el as HTMLElement).offsetTop - document.getElementById("treeList")!.offsetTop);
    await list.evaluate((el, top) => { el.scrollTop = top; }, rowTop - 80);
    await r.click();
    await expect(d).toHaveAttribute("data-id", "m-30");
    // The full issue arrives after the sheet opens; its text makes the detail scroll.
    await expect.poll(() => page.locator("#dbody").evaluate(el => el.scrollHeight - el.clientHeight)).toBeGreaterThan(400);
    await page.locator("#dbody").evaluate(el => { el.scrollTop = 300; });

    const place = async (vp: { width: number; height: number }) => {
      await page.setViewportSize(vp);
      const cls = await classOn(page);
      await expect(d).toHaveAttribute("data-id", "m-30");
      await expect(d).toBeVisible();
      await expect(page.locator("#treeRows .row.sel")).toHaveAttribute("data-id", "m-30");
      await expect(r).toBeInViewport();
      await expect.poll(() => page.locator("#dbody").evaluate(el => el.scrollTop)).toBeGreaterThanOrEqual(280);
      const db = await box(page, ".detail"), app = await box(page, "#app");
      // A phone's sheet spans the width; every other class shows a side panel beside the list.
      if (cls === "phone") expect(db.width).toBeGreaterThanOrEqual(app.width - 1);
      else { expect(db.width).toBeLessThan(app.width * 0.6); expect(db.height).toBeGreaterThan(app.height * 0.8); }
      return cls;
    };
    // On its side: a phone becomes a tablet, a tablet stays one, and a desktop upright is a tablet.
    await place({ width: start.height, height: start.width });
    expect(await place({ width: 390, height: 844 })).toBe("phone");
    expect(await place(start)).toBe(sizeClassOf(start.width));
  });
});

test.describe("phone native layout", () => {
  test.use({ fixture: { issues: MANY } });

  test("keeps dense 44 px rows, bottom navigation targets and sheet actions", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "phone", "phone only");
    await open(page, project);
    await classOn(page);

    const appFits = await page.locator("#app").evaluate(el => el.scrollWidth <= el.clientWidth + 1);
    expect(appFits).toBe(true);
    for (const b of await page.locator("#bar button").all()) expect((await b.boundingBox())!.height).toBeGreaterThanOrEqual(44);

    const rowBoxes = await page.locator("#treeRows .row").evaluateAll(rows => rows.map(row => {
      const r = row.getBoundingClientRect(), bar = document.getElementById("bar")!.getBoundingClientRect();
      return { height: r.height, visible: r.top >= 0 && r.bottom <= bar.top + 1 };
    }));
    expect(rowBoxes[0].height).toBeGreaterThanOrEqual(44);
    expect(rowBoxes[0].height).toBeLessThanOrEqual(46);
    expect(rowBoxes.filter(r => r.visible).length).toBeGreaterThanOrEqual(12);

    await row(page, "m-1").click();
    const d = page.locator(".detail");
    await expect(d).toHaveAttribute("data-id", "m-1");
    await expect(d.locator('.back[data-act="dclose"]')).toHaveText(/Tree/);
    await expect.poll(() => d.locator(".dacts button, .dtop button").evaluateAll(buttons =>
      buttons.length > 0 && buttons.every(button => button.getBoundingClientRect().height >= 44)
    )).toBe(true);
    await d.locator('.back[data-act="dclose"]').click();
    await expect(d).toHaveCount(0);
  });

  test("leaves the outer 24 px edge to the operating system", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "phone", "phone only");
    await open(page, project);
    const r = row(page, "m-1");
    const b = (await r.boundingBox())!;
    const y = b.y + b.height / 2;
    await page.mouse.move(12, y);
    await page.mouse.down();
    for (let s = 1; s <= 8; s++) await page.mouse.move(12 + s * 24, y);
    await page.mouse.up();

    await expect(page.locator(".detail")).toHaveCount(0);
    expect(project.issues().find(i => i.id === "m-1")?.status).toBe("open");
  });
});
