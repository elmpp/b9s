// The app shell per size class (docs/adr/0052). util.ts puts the class of the
// app's width on #app: a phone reaches the views from a bottom bar and opens
// the detail as a sheet, every other class has a rail at the left and a side
// panel. A rotation or a resized window keeps the place: the view, the
// selection, the list's scroll and the open detail with its own scroll.

import type { Page } from "@playwright/test";
import { sizeClassOf } from "../src/util";
import { expect, open, row, SAMPLE, test, type FixtureIssue } from "./harness";

const box = async (page: Page, sel: string) => (await page.locator(sel).boundingBox())!;
type FakeVisualViewport = { offsetLeft: number; offsetTop: number; width: number; height: number; scale: number };

async function fakeVisualViewport(page: Page, vv: FakeVisualViewport): Promise<void> {
  await page.addInitScript((initial) => {
    const target = new EventTarget();
    const fake = target as VisualViewport;
    Object.defineProperties(fake, {
      offsetLeft: { get: () => initial.offsetLeft },
      offsetTop: { get: () => initial.offsetTop },
      pageLeft: { get: () => initial.offsetLeft },
      pageTop: { get: () => initial.offsetTop },
      width: { get: () => initial.width },
      height: { get: () => initial.height },
      scale: { get: () => initial.scale },
    });
    Object.defineProperty(window, "visualViewport", { configurable: true, value: fake });
  }, vv);
}

/** classOn waits for #app to carry the class of the viewport's width, and returns it. */
async function classOn(page: Page): Promise<string> {
  const want = sizeClassOf(page.viewportSize()!.width);
  await expect(page.locator("#app")).toHaveClass(new RegExp(`(^| )${want}( |$)`));
  return want;
}

test("#app carries the class of its width, with the bar at the bottom on a phone and a rail elsewhere", async ({ page, project }) => {
  await open(page, project);
  const cls = await classOn(page);
  const rootTouchAction = await page.locator("#app").evaluate(el => getComputedStyle(el).touchAction);
  expect(rootTouchAction.split(" ").sort().join(" ")).toBe("pan-x pan-y");
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
    // The paragraph at the top of the text is what a rotation keeps: the text reflows at the other
    // pane's width and the browser holds that paragraph in place, so its offset in px moves with it.
    const topPara = () => page.locator("#dbody").evaluate(el => {
      const top = el.getBoundingClientRect().top;
      return [...el.querySelectorAll(".md p")].findIndex(p => p.getBoundingClientRect().bottom > top);
    });
    let para = await topPara();
    expect(para).toBeGreaterThan(1);

    const place = async (vp: { width: number; height: number }) => {
      await page.setViewportSize(vp);
      const cls = await classOn(page);
      await expect(d).toHaveAttribute("data-id", "m-30");
      await expect(d).toBeVisible();
      await expect(page.locator("#treeRows .row.sel")).toHaveAttribute("data-id", "m-30");
      await expect(r).toBeInViewport();
      await expect.poll(async () => Math.abs((await topPara()) - para) <= 1).toBe(true);
      para = await topPara();
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

  test("shows one status row on the status board and restores chips on the tree", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "phone", "phone only");
    await page.addInitScript(() => localStorage.setItem("b9s.chips", JSON.stringify(["open"])));
    await open(page, project);
    await classOn(page);
    await expect(page.locator("#chips")).toBeVisible();

    await page.locator('#bar [data-view="board"]').click();
    await expect(page.locator("#tabs")).toBeVisible();
    await expect(page.locator("#chips")).toBeHidden();
    await expect(page.locator('#tabs [data-tab="in_progress"]')).toHaveCount(0);
    const visibleRows = await page.locator("#chips, #tabs").evaluateAll(rows => rows.filter(row => {
      const r = row.getBoundingClientRect(), s = getComputedStyle(row);
      return r.width > 0 && r.height > 0 && s.display !== "none" && s.visibility !== "hidden";
    }).map(row => row.id));
    expect(visibleRows).toEqual(["tabs"]);

    const filter = page.locator('#tabs [data-act="clearstatusfilter"]');
    await expect(filter).toContainText(/filter: open/i);
    await filter.click();
    await expect(page.locator('#tabs [data-tab="in_progress"]')).toBeVisible();
    await expect(filter).toHaveCount(0);

    await page.locator('#bar [data-view="tree"]').click();
    await expect(page.locator("#chips")).toBeVisible();
  });

  test("keeps status chips visible when the phone board groups by priority", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "phone", "phone only");
    await page.addInitScript(() => localStorage.setItem("b9s.boardGroup", JSON.stringify("priority")));
    await open(page, project);
    await classOn(page);
    await page.locator('#bar [data-view="board"]').click();
    await expect(page.locator('#tabs [data-tab="1"]')).toBeVisible();
    await expect(page.locator("#chips")).toBeVisible();
  });

  test("draws active status chips differently from inactive ones", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "phone", "phone only");
    await open(page, project);
    await classOn(page);
    const styles = await page.locator('#chips [data-chip="open"], #chips [data-chip="closed"]').evaluateAll(buttons => buttons.map(button => {
      const s = getComputedStyle(button);
      return { border: s.borderTopColor, color: s.color };
    }));
    expect(styles).toHaveLength(2);
    expect(styles[0].border !== styles[1].border || styles[0].color !== styles[1].color).toBe(true);
  });

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

  test("keeps the shell controls inside a zoomed visual viewport", async ({ page, project }) => {
    const viewport = page.viewportSize()!;
    test.skip(sizeClassOf(viewport.width) !== "phone", "phone only");
    const vv = {
      offsetLeft: 16,
      offsetTop: Math.round(viewport.height * 0.16),
      width: viewport.width - 40,
      height: Math.round(viewport.height * 0.68),
      scale: 1.18,
    };
    await fakeVisualViewport(page, vv);
    await open(page, project);
    await classOn(page);
    await expect(page.locator("#app")).toHaveClass(/vzoom/);

    for (const view of ["tree", "board", "search"] as const) {
      if (view !== "tree") await page.locator(`#bar [data-view="${view}"]`).click();
      const header = await box(page, "#hd"), bar = await box(page, "#bar");
      const right = vv.offsetLeft + vv.width, bottom = vv.offsetTop + vv.height;
      for (const control of [header, bar]) {
        expect(control.x).toBeGreaterThanOrEqual(vv.offsetLeft - 1);
        expect(control.x + control.width).toBeLessThanOrEqual(right + 1);
        expect(control.y).toBeGreaterThanOrEqual(vv.offsetTop - 1);
        expect(control.y + control.height).toBeLessThanOrEqual(bottom + 1);
      }
    }
  });
});

test.describe("tablet split view", () => {
  test.use({ fixture: { issues: MANY } });

  test("keeps a list pane at the left and gives the detail the rest below the chips, upright and on its side", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "tablet", "tablet only");
    await open(page, project);
    await row(page, "t-1").click();
    const d = page.locator(".detail");
    await expect(d).toHaveAttribute("data-id", "t-1");
    const split = async () => {
      expect(await classOn(page)).toBe("tablet");
      const app = await box(page, "#app"), bar = await box(page, "#bar"), chips = await box(page, "#chips"), list = await box(page, "#treeList"), db = await box(page, ".detail");
      // The list keeps 300 to 420 px from the rail's edge; the detail starts where the list ends and reaches the right edge.
      expect(list.x).toBeGreaterThanOrEqual(bar.x + bar.width - 1);
      expect(list.width).toBeGreaterThanOrEqual(299);
      expect(list.width).toBeLessThanOrEqual(421);
      expect(Math.abs(db.x - (list.x + list.width))).toBeLessThanOrEqual(1);
      expect(db.x + db.width).toBeGreaterThanOrEqual(app.x + app.width - 1);
      // Both panes start below the chips, which keep the whole width, and the detail runs to the bottom edge.
      expect(chips.x + chips.width).toBeGreaterThanOrEqual(app.x + app.width - 1);
      expect(db.y).toBeGreaterThanOrEqual(chips.y + chips.height - 1);
      expect(Math.abs(db.y - list.y)).toBeLessThanOrEqual(1);
      expect(db.y + db.height).toBeGreaterThanOrEqual(app.y + app.height - 1);
      expect(await page.locator("#app").evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
    };
    await split();
    // The pane's head buttons and relations are finger targets.
    await expect.poll(() => d.locator(".dtop button, .rel").evaluateAll(els => els.length > 1 && els.every(el => el.getBoundingClientRect().height >= 44))).toBe(true);
    // The other orientation is the same class, so nothing redraws: the detail stays and the panes refit.
    const vp = page.viewportSize()!;
    await page.setViewportSize({ width: vp.height, height: vp.width });
    await expect(d).toHaveAttribute("data-id", "t-1");
    await split();
    // Closing the detail gives the list the whole width back.
    await d.locator('[data-act="dclose"]').click();
    await expect(d).toHaveCount(0);
    const app = await box(page, "#app"), list = await box(page, "#treeList");
    expect(list.x + list.width).toBeGreaterThanOrEqual(app.x + app.width - 1);
  });

  test("a hardware keyboard works the split: j and Enter open the panel beside the list, \\ widens it, Esc closes it", async ({ page, project }) => {
    test.skip(sizeClassOf(page.viewportSize()!.width) !== "tablet", "tablet only");
    await open(page, project);
    await page.keyboard.press("j");
    await page.keyboard.press("j");
    await expect(page.locator("#treeRows .row.sel")).toHaveAttribute("data-id", "t-1");
    await page.keyboard.press("Enter");
    const d = page.locator(".detail");
    await expect(d).toHaveAttribute("data-id", "t-1");
    const app = await box(page, "#app"), bar = await box(page, "#bar");
    const railEdge = bar.x + bar.width;
    // The panel settles beside the list once it has slid in.
    const beside = async () => {
      const list = await box(page, "#treeList"), p = await box(page, ".detail");
      return Math.abs(p.x - (list.x + list.width)) <= 1 && p.x + p.width >= app.x + app.width - 1;
    };
    await expect.poll(beside).toBe(true);
    await page.keyboard.press("\\");
    await expect.poll(async () => Math.abs((await box(page, ".detail")).x - railEdge) <= 1).toBe(true);
    await page.keyboard.press("\\");
    await expect.poll(beside).toBe(true);
    // The cursor keys move the panel with the selection, as the TUI's detail pane follows the cursor.
    await page.keyboard.press("j");
    await expect(d).toHaveAttribute("data-id", "t-2");
    await expect(page.locator("#treeRows .row.sel")).toHaveAttribute("data-id", "t-2");
    await page.keyboard.press("Escape");
    await expect(d).toHaveCount(0);
    expect((await box(page, "#treeList")).width).toBeGreaterThanOrEqual(app.width - railEdge - 1);
  });
});
