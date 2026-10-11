import type { Page } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { expect, open, row, startProject, stopProject, swipe, test, toJSONL } from "./harness";
import { measureFixture } from "./measure-fixture";

const BIG = measureFixture(1000, "p");

const BUDGET = {
  loadMs: 1500,
  tapFeedbackMs: 100,
  projectSwitchMs: 500,
  writeRoundTripMs: 800,
  liveUpdateMs: 1000,
  fullRenderMs: 250,
  longTaskMs: 0,
};

test.use({ fixture: { issues: BIG } });
test.describe.configure({ mode: "serial" });

const attr = (s: string) => s.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
const rowStatusPred = (id: string, status: string) => `document.querySelector("#treeRows .row[data-id=\\"${attr(id)}\\"] .stw")?.textContent === ${JSON.stringify(status)}`;

async function feedbackMs(page: Page, predicate: string, action: () => Promise<void>, timeoutMs = 5000, inputMode: "first" | "last" = "first"): Promise<number> {
  await page.evaluate(([predicate, timeoutMs, inputMode]) => {
    type PerfWindow = Window & { __b9sPerfWait?: Promise<number> };
    const w = window as PerfWindow;
    w.__b9sPerfWait = new Promise((resolve, reject) => {
      const test = new Function(`return (${predicate});`) as () => boolean;
      let input = NaN, done = false;
      let mo: MutationObserver | null = null;
      const types = ["pointerup", "touchend", "click", "keydown"];
      const cleanup = () => {
        window.clearTimeout(timer);
        mo?.disconnect();
        types.forEach(t => window.removeEventListener(t, onInput, true));
      };
      const finish = () => {
        if (done || !Number.isFinite(input)) return;
        done = true;
        cleanup();
        resolve(performance.now() - input);
      };
      const check = () => {
        if (!Number.isFinite(input)) return;
        try { if (test()) finish(); } catch { /* not ready */ }
      };
      const onInput = (e: Event) => {
        if (inputMode === "first" && Number.isFinite(input)) return;
        input = e.timeStamp || performance.now();
        setTimeout(check, 0);
      };
      const timer = window.setTimeout(() => { cleanup(); reject(new Error(`feedback budget predicate did not match within ${timeoutMs} ms`)); }, timeoutMs);
      types.forEach(t => window.addEventListener(t, onInput, true));
      mo = new MutationObserver(check);
      mo.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
      check();
    });
  }, [predicate, timeoutMs, inputMode] as const);
  await action();
  return page.evaluate(() => (window as Window & { __b9sPerfWait: Promise<number> }).__b9sPerfWait);
}

async function firstOpenLeaf(page: Page, exclude: string[] = []): Promise<string> {
  const id = await page.evaluate(exclude => {
    const skip = new Set(exclude);
    for (const r of document.querySelectorAll<HTMLElement>("#treeRows .row")) {
      const id = r.dataset.id || "";
      if (skip.has(id)) continue;
      if (r.querySelector(".stw")?.textContent === "open" && r.querySelector(".chev")?.textContent === "·") return id;
    }
    return "";
  }, exclude);
  expect(id).toBeTruthy();
  await row(page, id).scrollIntoViewIfNeeded();
  return id;
}

async function waitForFixtureStatus(project: { issues(): Record<string, unknown>[] }, id: string, status: string, timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (project.issues().find(i => i.id === id)?.status === status) return;
    await new Promise(r => setTimeout(r, 20));
  }
  throw new Error(`${id} did not become ${status} within ${timeoutMs} ms`);
}

async function lastWriteFetchMs(page: Page): Promise<number> {
  return page.evaluate(() => {
    const writes = performance.getEntriesByType("resource")
      .filter((e): e is PerformanceResourceTiming => e.name.endsWith("api/write"));
    const rec = writes[writes.length - 1];
    return rec ? rec.responseEnd - rec.startTime : NaN;
  });
}

async function smoothSample(page: Page, action: () => Promise<void>): Promise<{ frames: number; frameP95: number; slowFrames: number; longTaskMs: number }> {
  await page.evaluate(() => {
    const w = window as Window & {
      __b9sFrames?: number[];
      __b9sFrameReq?: number;
      __b9sLongTasks?: number[];
      __b9sLongTaskObserver?: PerformanceObserver;
    };
    w.__b9sFrames = [];
    w.__b9sLongTasks = [];
    try {
      w.__b9sLongTaskObserver?.disconnect();
      w.__b9sLongTaskObserver = new PerformanceObserver(list => {
        for (const e of list.getEntries()) w.__b9sLongTasks?.push(e.duration);
      });
      w.__b9sLongTaskObserver.observe({ type: "longtask", buffered: true });
    } catch { /* WebKit has no longtask observer */ }
    const loop = (t: number) => {
      if (!w.__b9sFrames) return;
      w.__b9sFrames.push(t);
      w.__b9sFrameReq = requestAnimationFrame(loop);
    };
    w.__b9sFrameReq = requestAnimationFrame(loop);
  });
  await action();
  await page.waitForTimeout(120);
  return page.evaluate(() => {
    const w = window as Window & {
      __b9sFrames?: number[];
      __b9sFrameReq?: number;
      __b9sLongTasks?: number[];
      __b9sLongTaskObserver?: PerformanceObserver;
    };
    if (w.__b9sFrameReq) cancelAnimationFrame(w.__b9sFrameReq);
    w.__b9sLongTaskObserver?.disconnect();
    const frames = w.__b9sFrames || [];
    const longTaskMs = (w.__b9sLongTasks || []).reduce((a, b) => a + b, 0);
    w.__b9sFrames = undefined;
    w.__b9sLongTasks = undefined;
    const intervals: number[] = [];
    for (let n = 1; n < frames.length; n++) intervals.push(frames[n] - frames[n - 1]);
    intervals.sort((a, b) => a - b);
    const frameP95 = intervals.length ? intervals[Math.min(intervals.length - 1, Math.ceil(intervals.length * 0.95) - 1)] : 0;
    return { frames: frames.length, frameP95, slowFrames: intervals.filter(x => x > 33.4).length, longTaskMs };
  });
}

test("1000 issue reads meet load and tap feedback budgets", async ({ page, project }) => {
  const t0 = Date.now();
  await open(page, project);
  const loadMs = Date.now() - t0;
  expect(await page.locator("#treeRows .row").count()).toBeGreaterThan(600);
  expect(loadMs).toBeLessThan(BUDGET.loadMs);

  const detailId = await firstOpenLeaf(page);
  const detailMs = await feedbackMs(page, `!!document.querySelector(".detail[data-id=\\"${attr(detailId)}\\"]")`, () => row(page, detailId).click());
  expect(detailMs).toBeLessThanOrEqual(BUDGET.tapFeedbackMs);
  await page.locator('.detail [aria-label="Close"]').click();
  await expect(page.locator(".detail")).toHaveCount(0);

  const renderMs = await page.evaluate(async () => {
    const chip = document.querySelector<HTMLElement>('[data-chip="closed"]')!;
    const t = performance.now();
    chip.click();
    await new Promise(r => requestAnimationFrame(() => setTimeout(r, 0)));
    return performance.now() - t;
  });
  expect(renderMs).toBeLessThan(BUDGET.fullRenderMs);
  const count = await page.locator("#treeRows .row").count();
  const chipMs = await feedbackMs(page, `document.querySelectorAll("#treeRows .row").length !== ${count}`, () => page.locator('#chips [data-chip="closed"]').click());
  expect(chipMs).toBeLessThanOrEqual(BUDGET.tapFeedbackMs);

  const boardMs = await feedbackMs(page, `!document.getElementById("vBoard")?.hidden && !!document.querySelector("#vBoard [data-card]")`, () => page.locator('#bar [data-view="board"]').click());
  expect(boardMs).toBeLessThanOrEqual(BUDGET.tapFeedbackMs);
  test.info().annotations.push({ type: "perf", description: `load ${loadMs} ms, detail ${Math.round(detailMs)} ms, chip ${Math.round(chipMs)} ms, board ${Math.round(boardMs)} ms` });
});

test("project switch meets the measured budget", async ({ page }) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "b9s-perf-projects-"));
  const first = path.join(root, "first");
  const second = path.join(root, "second", ".beads");
  fs.mkdirSync(second, { recursive: true });
  fs.writeFileSync(path.join(second, "issues.jsonl"), toJSONL(measureFixture(1000, "q")));
  const project = await startProject(BIG, {}, false, undefined, ["--projects-root", root], first);
  try {
    await open(page, project);
    await page.locator('#hd [data-act="projects"]').click();
    const target = page.locator('.msheet [data-sa="proj"]:not(.cur):not([data-val="*"])').first();
    await expect(target).toBeVisible();
    const name = await target.locator(".x").evaluate(el => el.firstChild?.textContent || "");
    const switchMs = await feedbackMs(
      page,
      `document.querySelector("#hd .proj")?.textContent?.includes(${JSON.stringify(name)}) && !!document.querySelector("#treeRows .row")`,
      () => target.click(),
      5000,
      "last",
    );
    expect(switchMs).toBeLessThan(BUDGET.projectSwitchMs);
    test.info().annotations.push({ type: "perf", description: `project switch ${Math.round(switchMs)} ms` });
  } finally {
    stopProject(project);
  }
});

test("write and live updates meet feedback and round-trip budgets", async ({ page, project }) => {
  await open(page, project);
  const writeId = await firstOpenLeaf(page);
  const liveId = await firstOpenLeaf(page, [writeId]);
  const before = await page.locator("#treeList").evaluate(el => el.scrollTop);
  const feedback = await feedbackMs(page, rowStatusPred(writeId, "in progress"), async () => {
    await swipe(page, writeId, 0.35);
  });
  expect(feedback).toBeLessThanOrEqual(BUDGET.tapFeedbackMs);
  await waitForFixtureStatus(project, writeId, "in_progress", BUDGET.writeRoundTripMs);
  const writeFetchMs = await lastWriteFetchMs(page);
  if (Number.isFinite(writeFetchMs)) expect(writeFetchMs).toBeLessThan(BUDGET.writeRoundTripMs);
  expect(Math.abs((await page.locator("#treeList").evaluate(el => el.scrollTop)) - before)).toBeLessThan(80);

  const liveStarted = Date.now();
  project.writeIssues(BIG.map(i => i.id === writeId || i.id === liveId ? { ...i, status: "in_progress" } : i));
  await expect(row(page, liveId).locator(".stw")).toHaveText("in progress", { timeout: BUDGET.liveUpdateMs });
  const liveMs = Date.now() - liveStarted;
  expect(liveMs).toBeLessThan(BUDGET.liveUpdateMs);
  test.info().annotations.push({ type: "perf", description: `write feedback ${Math.round(feedback)} ms, live ${liveMs} ms` });
});

test("scroll and drag stay inside frame budgets", async ({ page, project }) => {
  await open(page, project);
  const list = page.locator("#treeList");
  await list.hover();
  const scroll = await smoothSample(page, async () => {
    for (let n = 0; n < 8; n++) {
      try {
        await page.mouse.wheel(0, 280);
      } catch {
        await list.evaluate(el => { el.scrollTop += 280; });
      }
      await page.waitForTimeout(30);
    }
  });
  expect(scroll.frames).toBeGreaterThan(4);
  expect(scroll.longTaskMs).toBe(BUDGET.longTaskMs);

  const dragId = await firstOpenLeaf(page);
  const drag = await smoothSample(page, () => swipe(page, dragId, 0.15));
  expect(drag.frames).toBeGreaterThan(2);
  expect(drag.longTaskMs).toBe(BUDGET.longTaskMs);
  test.info().annotations.push({ type: "perf", description: `scroll p95 ${Math.round(scroll.frameP95)} ms, drag p95 ${Math.round(drag.frameP95)} ms, long tasks ${Math.round(scroll.longTaskMs + drag.longTaskMs)} ms` });
});
