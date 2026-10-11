// Where b9s web spends its time, measured inside the page.
//
// An init script records input events, DOM mutations, animation frames,
// Server-Sent Events and fetches with performance.now(), so the cost of a
// tap is the time from its pointerup to the frame that shows the change, not
// a round trip through Playwright. Each metric runs RUNS times per variant
// (an emulated Pixel 7, the same phone with a 4x slower CPU and on a slow 4G
// link, an iPhone 14 in WebKit, a 1440 x 900 desktop) against fixtures of
// MEASURE_SIZES issues, served by a real b9s web with the fake bd. The
// report lands in .b9s-e2e/measure/report/ as summary.json and summary.md;
// docs/plans/2026-10-11-responsive-web/measurements.md is written from it.
//
// The project is listed only when it is asked for (playwright.config.ts),
// so a plain run and CI never pay for it:
//   npx playwright test --project measure
//
// B9S_MEASURE_URL=http://127.0.0.1:7979/ measures a server that is already
// running instead, read-only: the swipe, write and live-update metrics are
// skipped, and nothing but a project switch is posted. B9S_MEASURE_COOKIE=
// "name=value" sends that cookie, for a server behind a login proxy.
// B9S_MEASURE_RUNS, B9S_MEASURE_VARIANTS=pixel7,desktop and
// B9S_MEASURE_SIZES=1000 narrow a run.

import { devices, expect, test, type Browser, type BrowserContextOptions, type CDPSession, type Page, type Playwright } from "@playwright/test";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { BIN, startProject, stopProject, toJSONL, type Project } from "./harness";
import { MEASURE_SIZES, measureFixture } from "./measure-fixture";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const FAKE_BD = path.join(__dirname, "fake-bd.mjs");
const OUT = path.resolve(__dirname, "../../.b9s-e2e/measure/report");
const RUNS = Number(process.env.B9S_MEASURE_RUNS || 5);
const REMOTE = process.env.B9S_MEASURE_URL ? process.env.B9S_MEASURE_URL.replace(/\/?$/, "/") : "";
const ONLY_VARIANTS = (process.env.B9S_MEASURE_VARIANTS || "").split(",").filter(Boolean);
const SIZES: (number | "live")[] = REMOTE ? ["live"] : MEASURE_SIZES.filter(s => !process.env.B9S_MEASURE_SIZES || process.env.B9S_MEASURE_SIZES.split(",").includes(String(s)));

type Metric = "load" | "switch" | "detail" | "fold" | "chip" | "board" | "swipe" | "write" | "live" | "scroll" | "drag";
const WRITES: Metric[] = ["swipe", "write", "live"];

interface Variant {
  name: string;
  engine: "chromium" | "webkit";
  context: BrowserContextOptions;
  touch: boolean;
  /** Emulation.setCPUThrottlingRate: 4 is roughly a mid-range phone next to this laptop. */
  cpu?: number;
  /** Network.emulateNetworkConditions: ms of latency and bytes per second. */
  network?: { latency: number; download: number; upload: number };
  /** the metrics this variant runs; every metric when absent */
  only?: Metric[];
}

const DESKTOP: BrowserContextOptions = { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } };
const SLOW_4G = { latency: 150, download: 1.6e6 / 8, upload: 750e3 / 8 };
const VARIANTS: Variant[] = [
  { name: "pixel7", engine: "chromium", context: devices["Pixel 7"], touch: true },
  { name: "pixel7-cpu4x", engine: "chromium", context: devices["Pixel 7"], touch: true, cpu: 4 },
  { name: "pixel7-slow4g", engine: "chromium", context: devices["Pixel 7"], touch: true, network: SLOW_4G, only: ["load", "switch", "swipe", "write", "live"] },
  { name: "iphone14", engine: "webkit", context: devices["iPhone 14"], touch: true },
  { name: "desktop", engine: "chromium", context: DESKTOP, touch: false },
];

/* ================= the in-page recorder ================= */

interface Recorder {
  inputs: { type: string; t: number }[];
  sse: { type: string; t: number; data: string }[];
  fetches: { url: string; method: string; start: number; end: number; status: number }[];
  longtasks: { start: number; dur: number }[];
  events: { name: string; start: number; dur: number; ps: number; pe: number }[];
  lcp: number;
  marks: { firstRow?: number; bootHidden?: number; painted?: number; live?: number };
  mutations: number[];
  lastTreeMutation: number;
  pred: Record<string, (a: string) => boolean>;
  waitFor(test: () => boolean, timeoutMs: number, name?: string): Promise<Settled>;
  arm(test: () => boolean, timeoutMs: number, name?: string): void;
  settle(name?: string): Promise<Settled>;
  quiet(ms: number, timeoutMs: number): Promise<number>;
  startFrames(): void;
  stopFrames(): number[];
}
interface Settled { dom: number; frame: number }
type W = Window & { __m: Recorder };

// Installed before the app's own script runs. Every timestamp is
// performance.now() of the page, so numbers from different sources compare.
const INIT = `(() => {
  if (window.__m) return;
  const now = () => performance.now();
  const M = { inputs: [], sse: [], fetches: [], longtasks: [], events: [], lcp: 0, marks: {}, mutations: [], lastTreeMutation: 0, waiters: [], armed: {}, frames: null, req: 0 };
  window.__m = M;
  try { performance.setResourceTimingBufferSize(20000); } catch (e) { /* the default buffer then */ }
  // After the next frame and the task after it, the change is on screen.
  const painted = fn => requestAnimationFrame(() => setTimeout(() => fn(now()), 0));
  const check = records => {
    const t = now();
    M.mutations.push(t);
    if (M.mutations.length > 5000) M.mutations.splice(0, 2500);
    const tree = document.getElementById("treeRows");
    if (tree && records.some(r => tree.contains(r.target))) M.lastTreeMutation = t;
    if (M.marks.firstRow == null && document.querySelector("#treeRows .row")) M.marks.firstRow = t;
    if (M.marks.bootHidden == null) { const b = document.getElementById("boot"); if (b && b.hidden) { M.marks.bootHidden = t; painted(p => { M.marks.painted = p; }); } }
    if (M.marks.live == null && document.body && document.body.dataset.live === "live") M.marks.live = t;
    if (M.waiters.length) {
      const keep = [];
      for (const w of M.waiters) { let ok = false; try { ok = w.test(); } catch (e) { ok = false; } if (ok) w.done(t); else keep.push(w); }
      M.waiters = keep;
    }
  };
  const observe = () => new MutationObserver(check).observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
  if (document.documentElement) observe(); else document.addEventListener("readystatechange", observe, { once: true });
  for (const type of ["pointerdown", "pointerup", "touchstart", "touchend", "click", "keydown", "wheel"]) {
    window.addEventListener(type, e => { M.inputs.push({ type, t: e.timeStamp }); if (M.inputs.length > 2000) M.inputs.splice(0, 1000); }, { capture: true, passive: true });
  }
  const ES = window.EventSource;
  if (ES) window.EventSource = class extends ES {
    constructor(...args) {
      super(...args);
      for (const type of ["hello", "changed", "project", "health"]) super.addEventListener(type, e => { M.sse.push({ type, t: now(), data: String(e.data || "") }); });
    }
  };
  const fetch0 = window.fetch;
  window.fetch = function (input, init) {
    const url = typeof input === "string" ? input : (input && input.url) || String(input);
    const r = { url, method: (init && init.method) || "GET", start: now(), end: 0, status: 0 };
    M.fetches.push(r);
    return fetch0.apply(this, arguments).then(res => { r.end = now(); r.status = res.status; return res; }, err => { r.end = now(); r.status = -1; throw err; });
  };
  const rowOf = id => document.querySelector('#treeRows .row[data-id="' + CSS.escape(id) + '"]');
  M.pred = {
    present: a => !!document.querySelector(a),
    absent: a => !document.querySelector(a),
    live: () => M.marks.live != null,
    rowGlyph: a => { const k = a.indexOf("|"); const el = rowOf(a.slice(0, k)); const st = el && el.querySelector(".st"); return !!st && st.textContent === a.slice(k + 1); },
    rowCountNot: a => document.querySelectorAll("#treeRows .row").length !== Number(a),
    detailFull: a => { const d = document.querySelector('.detail[data-id="' + CSS.escape(a) + '"] .dbody'); return !!d && !!d.querySelector(".md") && !/Loading…/.test(d.textContent || ""); },
    boardReady: () => { const v = document.getElementById("vBoard"); return !!v && !v.hidden && !!v.querySelector("[data-card]"); },
    treeShown: () => { const v = document.getElementById("vTree"); return !!v && !v.hidden && !!v.querySelector("#treeRows .row"); },
    projectsListed: () => !!document.querySelector('.msheet [data-sa="proj"]:not(.cur):not([data-val="*"])'),
    projectNamed: a => Array.from(document.querySelectorAll('.msheet [data-sa="proj"] .x')).some(el => (el.firstChild && el.firstChild.textContent || "") === a),
    headerProject: a => { const h = document.querySelector("#hd .proj"); return !!h && (h.textContent || "").includes(a) && !!document.querySelector("#treeRows .row"); },
    writeDone: a => {
      const t0 = Number(a);
      const wr = M.fetches.find(f => f.method === "POST" && f.url.endsWith("api/write") && f.start >= t0);
      if (!wr || !wr.end) return false;
      const snap = M.fetches.find(f => f.url.endsWith("api/snapshot") && f.start >= wr.start && f.end > 0);
      return !!snap && M.lastTreeMutation > snap.end;
    },
  };
  // The user timing marks put the DOM change and its frame into a Chrome trace.
  M.waitFor = (test, timeoutMs, name = "wait") => new Promise((resolve, reject) => {
    const timer = setTimeout(() => { M.waiters = M.waiters.filter(w => w.done !== done); reject(new Error("waitFor " + name + ": nothing matched within " + timeoutMs + " ms")); }, timeoutMs);
    const done = t => { clearTimeout(timer); performance.mark("b9s-dom:" + name); painted(p => { performance.mark("b9s-frame:" + name); resolve({ dom: t, frame: p }); }); };
    let ok = false;
    try { ok = test(); } catch (e) { ok = false; }
    if (ok) done(now()); else M.waiters.push({ test, done });
  });
  // A predicate is armed before the action it waits for: a waiter registered
  // afterwards may find the change already made and time the registration.
  M.arm = (test, timeoutMs, name = "main") => { const p = M.waitFor(test, timeoutMs, name); p.catch(() => {}); M.armed[name] = p; };
  M.settle = (name = "main") => { const p = M.armed[name]; delete M.armed[name]; return p || Promise.reject(new Error("settle: nothing armed as " + name)); };
  // quiet resolves once no mutation has happened for ms: a write's second
  // reload, which the file watcher sends 250 ms after the first, re-sorts
  // the tree, and a gesture that starts before it lands on another row.
  M.quiet = (ms, timeoutMs) => new Promise((resolve, reject) => {
    const t0 = now();
    const iv = setInterval(() => {
      const last = M.mutations.length ? M.mutations[M.mutations.length - 1] : 0;
      if (now() - last >= ms) { clearInterval(iv); resolve(now()); }
      else if (now() - t0 > timeoutMs) { clearInterval(iv); reject(new Error("quiet: the page kept changing for " + timeoutMs + " ms")); }
    }, 50);
  });
  M.startFrames = () => { M.frames = []; const loop = ts => { if (!M.frames) return; M.frames.push(ts); M.req = requestAnimationFrame(loop); }; M.req = requestAnimationFrame(loop); };
  M.stopFrames = () => { cancelAnimationFrame(M.req); const f = M.frames || []; M.frames = null; return f; };
  try { new PerformanceObserver(l => { for (const e of l.getEntries()) M.longtasks.push({ start: e.startTime, dur: e.duration }); }).observe({ type: "longtask", buffered: true }); } catch (e) { /* WebKit */ }
  try { new PerformanceObserver(l => { for (const e of l.getEntries()) M.events.push({ name: e.name, start: e.startTime, dur: e.duration, ps: e.processingStart, pe: e.processingEnd }); }).observe({ type: "event", durationThreshold: 16, buffered: true }); } catch (e) { /* WebKit */ }
  try { new PerformanceObserver(l => { for (const e of l.getEntries()) M.lcp = e.startTime; }).observe({ type: "largest-contentful-paint", buffered: true }); } catch (e) { /* WebKit */ }
})();`;

/* ================= page helpers ================= */

type Pred = [kind: string, arg?: string];

const pageNow = (page: Page) => page.evaluate(() => performance.now());
const arm = (page: Page, pred: Pred, ms = 15000, name = "main") => page.evaluate(([k, a, ms, name]) => { const w = (window as W).__m; w.arm(() => w.pred[k](a), ms, name); }, [pred[0], pred[1] || "", ms, name] as const);
const settle = (page: Page, name = "main") => page.evaluate(name => (window as W).__m.settle(name), name);
const waitDom = (page: Page, pred: Pred, ms = 15000) => page.evaluate(([k, a, ms]) => { const w = (window as W).__m; return w.waitFor(() => w.pred[k](a), ms, k); }, [pred[0], pred[1] || "", ms] as const);
const quiet = (page: Page, ms = 600, timeout = 20000) => page.evaluate(([ms, t]) => (window as W).__m.quiet(ms, t), [ms, timeout] as const);

/** firstInput is the pointerup, touchend, click or keydown the app acted on. */
const firstInput = (page: Page, t0: number) => page.evaluate(t0 => {
  const w = (window as W).__m;
  for (const type of ["pointerup", "touchend", "click", "keydown"]) { const i = w.inputs.find(i => i.type === type && i.t >= t0); if (i) return i.t; }
  return NaN;
}, t0);
const fetchesAfter = (page: Page, t0: number) => page.evaluate(t0 => (window as W).__m.fetches.filter(f => f.start >= t0), t0);
const sseAfter = (page: Page, t0: number) => page.evaluate(t0 => (window as W).__m.sse.filter(e => e.t >= t0), t0);
const firstMutationAfter = (page: Page, t0: number) => page.evaluate(t0 => (window as W).__m.mutations.find(t => t > t0) ?? NaN, t0);
/** eventDuration is the Event Timing API's input-to-paint figure, Chromium only, in 8 ms steps. */
const eventDuration = (page: Page, t0: number) => page.evaluate(t0 => Math.max(0, ...(window as W).__m.events.filter(e => e.start >= t0 && /pointerup|click|touchend|keydown/.test(e.name)).map(e => e.dur)), t0);

const rowSel = (id: string) => `#treeRows .row[data-id="${id}"]`;
const rowInfo = (page: Page, limit = 120) => page.evaluate(limit => [...document.querySelectorAll<HTMLElement>("#treeRows .row")].slice(0, limit).map(r => ({
  id: r.dataset.id || "", glyph: r.querySelector(".st")?.textContent || "", kids: !!r.querySelector(".chev:not(.none)"), folded: r.querySelector(".chev")?.textContent === "▸",
})), limit);
const rowCount = (page: Page) => page.locator("#treeRows .row").count();

interface Timing { t0: number; input: number; dom: number; frame: number; toDom: number; toFrame: number; eventDur: number }

interface Also { name: string; pred: Pred; ms?: number }

/**
 * timed arms pred (and every later change in also, which settle(page, name)
 * collects), runs act, and reports the time from the input event to the DOM
 * change and to the painted frame.
 */
async function timed(page: Page, pred: Pred, act: () => Promise<void>, ms = 15000, also: Also[] = []): Promise<Timing> {
  const t0 = await pageNow(page);
  await arm(page, pred, ms);
  for (const a of also) await arm(page, a.pred, a.ms || ms, a.name);
  await act();
  const r = await settle(page);
  const input = await firstInput(page, t0);
  if (!(input >= t0)) throw new Error(`no input event was recorded for ${pred[0]}`);
  await page.waitForTimeout(60);
  return { t0, input, dom: r.dom, frame: r.frame, toDom: r.dom - input, toFrame: r.frame - input, eventDur: await eventDuration(page, t0) };
}

/**
 * target scrolls the element to the middle of its list and returns its box.
 * WebKit's scrollIntoViewIfNeeded leaves a row of a content-visibility list
 * where it is, so the scroll is explicit, and a point off screen is an error
 * rather than a tap on whatever is there.
 */
async function target(page: Page, sel: string): Promise<{ x: number; y: number; width: number; height: number }> {
  const loc = page.locator(sel).first();
  await loc.evaluate(el => el.scrollIntoView({ block: "center", inline: "nearest" }));
  const box = await loc.boundingBox();
  const vp = page.viewportSize();
  const cy = box ? box.y + box.height / 2 : -1;
  if (!box || !vp || cy < 0 || cy > vp.height) throw new Error(`${sel} is not on screen: ${JSON.stringify(box)}`);
  return box;
}

async function tap(page: Page, v: Variant, sel: string, xFraction = 0.5): Promise<void> {
  const box = await target(page, sel);
  const x = box.x + box.width * xFraction, y = box.y + box.height / 2;
  if (v.touch) await page.touchscreen.tap(x, y); else await page.mouse.click(x, y);
}

/** drag moves a row sideways by a fraction of its width with real pointer events, as harness.swipe does. */
async function drag(page: Page, sel: string, fraction: number, steps = 12, pauseMs = 0): Promise<void> {
  const box = await target(page, sel);
  const y = box.y + box.height / 2, x0 = box.x + box.width / 2 - (fraction > 0 ? box.width * 0.3 : -box.width * 0.3);
  const x1 = x0 + fraction * box.width;
  await page.mouse.move(x0, y);
  await page.mouse.down();
  for (let s = 1; s <= steps; s++) {
    await page.mouse.move(x0 + ((x1 - x0) * s) / steps, y);
    if (pauseMs) await page.waitForTimeout(pauseMs);
  }
  await page.mouse.up();
}

/* ================= sessions ================= */

interface Session { page: Page; cdp: CDPSession | null; browser: Browser; close(): Promise<void>; posts: string[] }

const launched = new Map<string, Browser>();

/**
 * browserFor launches each engine once per worker. Headless Chromium on
 * macOS paces its frames by a timer that the system coalesces when no
 * window is on screen, to 80 to 150 ms between frames on this laptop, so
 * every time to a frame would be a timer artifact. A headed window placed
 * off screen is paced by the display instead, at 60 to 120 Hz, and shows
 * nothing. B9S_MEASURE_HEADLESS=1 keeps the windowless run for machines
 * where a window is unwelcome; its frame figures then carry that caveat.
 * WebKit's headless mode is an off-screen window already and paces at 60 Hz.
 */
async function browserFor(playwright: Playwright, engine: Variant["engine"]): Promise<Browser> {
  let b = launched.get(engine);
  if (!b) {
    const headless = engine === "webkit" || process.env.B9S_MEASURE_HEADLESS === "1";
    b = await playwright[engine].launch({
      headless,
      ...(engine === "chromium" && process.env.B9S_MEASURE_CHANNEL ? { channel: process.env.B9S_MEASURE_CHANNEL } : {}),
      args: engine === "chromium" && !headless ? ["--window-position=-4000,-4000", "--window-size=600,900"] : [],
    });
    launched.set(engine, b);
  }
  return b;
}
test.afterAll(async () => { for (const b of launched.values()) await b.close(); launched.clear(); });

async function newSession(browser: Browser, v: Variant, url: string): Promise<Session> {
  // Reduced motion drops the app's transitions, as every other project does
  // (motion.spec.ts), so a number is the app's work and not a 240 ms slide.
  const context = await browser.newContext({ ...v.context, reducedMotion: "reduce" });
  if (REMOTE && process.env.B9S_MEASURE_COOKIE) {
    const [name, ...rest] = process.env.B9S_MEASURE_COOKIE.split("=");
    await context.addCookies([{ name, value: rest.join("="), url }]);
  }
  await context.addInitScript(INIT);
  const page = await context.newPage();
  const posts: string[] = [];
  page.on("request", r => { if (r.method() !== "GET" && !r.url().endsWith("api/projects/open")) posts.push(r.url()); });
  let cdp: CDPSession | null = null;
  if (v.engine === "chromium") {
    cdp = await context.newCDPSession(page);
    if (v.cpu) await cdp.send("Emulation.setCPUThrottlingRate", { rate: v.cpu });
    if (v.network) {
      await cdp.send("Network.enable");
      await cdp.send("Network.emulateNetworkConditions", { offline: false, latency: v.network.latency, downloadThroughput: v.network.download, uploadThroughput: v.network.upload });
    }
  }
  return { page, cdp, browser, posts, close: () => context.close() };
}

/* ================= samples and statistics ================= */

interface Sample { name: string; metric: Metric; variant: string; size: number | string; run: number; values: Record<string, number> }
const samples: Sample[] = [];
const sampleName = (v: Variant, size: number | string) => `${size}-${v.name}`;
const record = (metric: Metric, v: Variant, size: number | string, run: number, values: Record<string, number>) => samples.push({ name: sampleName(v, size), metric, variant: v.name, size, run, values });

function stats(xs: number[]): { n: number; median: number; p95: number; min: number; max: number } {
  const s = [...xs].sort((a, b) => a - b);
  const n = s.length;
  // Nearest rank: with five runs, p95 is the slowest run.
  const rank = (p: number) => s[Math.min(n - 1, Math.max(0, Math.ceil(p * n) - 1))];
  return { n, median: n % 2 ? s[(n - 1) / 2] : (s[n / 2 - 1] + s[n / 2]) / 2, p95: rank(0.95), min: s[0], max: s[n - 1] };
}

function frameStats(frames: number[]): Record<string, number> {
  const iv: number[] = [];
  for (let n = 1; n < frames.length; n++) iv.push(frames[n] - frames[n - 1]);
  if (!iv.length) return { frames: frames.length };
  const s = stats(iv);
  const span = frames[frames.length - 1] - frames[0];
  return { frames: frames.length, fps: span > 0 ? (1000 * iv.length) / span : 0, frameP95: s.p95, frameMax: s.max, slowFrames: iv.filter(x => x > 33.4).length };
}

/* ================= metrics ================= */

const TRACE_CATEGORIES = ["blink.user_timing", "benchmark", "cc", "viz", "toplevel", "devtools.timeline", "disabled-by-default-devtools.timeline", "disabled-by-default-devtools.timeline.frame"];

async function measureLoad(browser: Browser, v: Variant, url: string, size: number | string): Promise<void> {
  for (let run = 0; run < RUNS; run++) {
    const s = await newSession(browser, v, url);
    try {
      const wall = Date.now();
      await s.page.goto(url);
      await waitDom(s.page, ["live"], v.network ? 180000 : 60000);
      const wallMs = Date.now() - wall;
      const r = await s.page.evaluate(async () => {
        const w = (window as W).__m;
        const nav = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming | undefined;
        const res = (name: string) => {
          const e = performance.getEntriesByType("resource").find(e => e.name.includes(name)) as PerformanceResourceTiming | undefined;
          return e ? { ms: e.responseEnd - e.startTime, start: e.startTime, end: e.responseEnd, wireKB: e.encodedBodySize / 1024, rawKB: e.decodedBodySize / 1024 } : null;
        };
        const paint = performance.getEntriesByType("paint").find(e => e.name === "first-contentful-paint");
        let issues = 0;
        try { issues = ((await (await fetch("api/health")).json()) as { issues: number }).issues; } catch { /* the rows count still says */ }
        return {
          marks: w.marks, ttfb: nav ? nav.responseStart : 0, dcl: nav ? nav.domContentLoadedEventEnd : 0, js: res("app.js"), css: res("app.css"), session: res("api/session"), snapshot: res("api/snapshot"),
          fcp: paint ? paint.startTime : 0, lcp: w.lcp, rowsShown: document.querySelectorAll("#treeRows .row").length, issues,
          longTaskMs: w.longtasks.reduce((a, l) => a + l.dur, 0), hello: (w.sse.find(e => e.type === "hello") || { t: NaN }).t,
        };
      });
      record("load", v, size, run, {
        usable: r.marks.painted ?? NaN, firstRow: r.marks.firstRow ?? NaN, live: r.marks.live ?? NaN, wallMs,
        ttfb: r.ttfb, dcl: r.dcl, fcp: r.fcp, lcp: r.lcp,
        jsMs: r.js?.ms ?? NaN, jsKB: r.js?.wireKB ?? NaN, cssKB: r.css?.wireKB ?? NaN,
        sessionMs: r.session?.ms ?? NaN, snapshotMs: r.snapshot?.ms ?? NaN, snapshotStart: r.snapshot?.start ?? NaN, snapshotEnd: r.snapshot?.end ?? NaN, snapshotKB: r.snapshot?.wireKB ?? NaN, snapshotRawKB: r.snapshot?.rawKB ?? NaN,
        renderMs: r.marks.bootHidden != null && r.snapshot ? r.marks.bootHidden - r.snapshot.end : NaN, paintMs: r.marks.painted != null && r.marks.bootHidden != null ? r.marks.painted - r.marks.bootHidden : NaN,
        rowsShown: r.rowsShown, issues: r.issues, longTaskMs: r.longTaskMs, hello: r.hello,
      });
    } finally { await s.close(); }
  }
}

/** measureSwitch opens the project sheet and switches to the other project, then back, RUNS times in all. */
async function measureSwitch(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page } = s;
  const otherProject = '.msheet [data-sa="proj"]:not(.cur):not([data-val="*"])';
  const switchToOther = async (run?: number): Promise<void> => {
    const t0 = await pageNow(page);
    await page.locator('#hd [data-act="projects"]').click();
    const row = page.locator(otherProject).first();
    await row.waitFor({ timeout: v.network ? 120000 : 60000 });
    await expect(row).toBeVisible({ timeout: v.network ? 120000 : 60000 });
    const input = await firstInput(page, t0);
    await page.waitForTimeout(60);
    const sheetToFrame = Number.isFinite(input) ? (await pageNow(page)) - input : NaN;
    const fx = await fetchesAfter(page, t0);
    const list = fx.find(f => f.url.endsWith("api/projects") && f.end > 0);
    const target = await row.evaluate(row => ({
      key: row.getAttribute("data-val") || "",
      name: row.querySelector(".x")?.firstChild?.textContent || "",
    }));
    if (!target.key || !target.name) throw new Error(`project row had no key or name: ${JSON.stringify(target)}`);
    const sw = await timed(page, ["headerProject", target.name], () => row.click(), v.network ? 120000 : 60000);
    const after = await fetchesAfter(page, sw.t0);
    const open = after.find(f => f.url.endsWith("api/projects/open") && f.end > 0);
    const snaps = after.filter(f => f.url.endsWith("api/snapshot") && f.end > 0);
    const sse = (await sseAfter(page, sw.t0)).find(e => e.type === "project");
    if (run != null) record("switch", v, size, run, {
      toFrame: sw.frame - sw.t0, toDom: sw.dom - sw.t0, eventDur: sw.eventDur, sheetToFrame, projectsMs: list ? list.end - list.start : NaN,
      openMs: open ? open.end - open.start : NaN, snapshotMs: snaps[0] ? snaps[0].end - snaps[0].start : NaN, snapshotEnd: snaps[0] ? snaps[0].end - sw.input : NaN, snapshots: snaps.length, sseProject: sse ? sse.t - sw.input : NaN,
    });
  };
  const listed = await page.locator('#hd [data-act="projects"]').count();
  if (!listed) return;
  await tap(page, v, '#hd [data-act="projects"]');
  await expect(page.locator(".msheet")).toBeVisible();
  await page.locator('.msheet [data-sa="proj"]').first().waitFor({ timeout: v.network ? 120000 : 60000 });
  const candidates = await page.locator(otherProject).count();
  await page.keyboard.press("Escape");
  await waitDom(page, ["absent", ".msheet"]);
  if (!candidates) { record("switch", v, size, 0, { noOtherProject: 1 }); return; }
  for (let run = 0; run < RUNS; run++) {
    await quiet(page);
    await switchToOther(run);
  }
  // The write and live measurements mutate the startup fixture with fake-bd.
  if (RUNS % 2) await switchToOther();
}

async function measureDetail(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page } = s;
  const rows = (await rowInfo(page, 12)).slice(0, RUNS);
  for (const [run, row] of rows.entries()) {
    await quiet(page);
    const t = await timed(page, ["present", `.detail[data-id="${row.id}"]`], () => tap(page, v, rowSel(row.id), 0.6), 15000, [{ name: "full", pred: ["detailFull", row.id], ms: 20000 }]);
    const full = await settle(page, "full");
    const issue = (await fetchesAfter(page, t.t0)).find(f => f.url.includes("api/issue?"));
    record("detail", v, size, run, { toFrame: t.toFrame, toDom: t.toDom, eventDur: t.eventDur, fullMs: full.frame - t.input, issueMs: issue ? issue.end - issue.start : NaN });
    await tap(page, v, '.detail [data-act="dclose"]');
    await waitDom(page, ["absent", ".detail"]);
    await page.waitForTimeout(200);
  }
}

async function measureFold(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page } = s;
  const epic = (await rowInfo(page, 12)).find(r => r.kids && !r.folded);
  if (!epic) throw new Error("no unfolded row with children in the first rows");
  for (let run = 0; run < RUNS; run++) {
    await quiet(page);
    const count = await rowCount(page);
    const t = await timed(page, ["rowCountNot", String(count)], () => tap(page, v, `${rowSel(epic.id)} [data-chev]`));
    record("fold", v, size, run, { toFrame: t.toFrame, toDom: t.toDom, eventDur: t.eventDur });
    // Two taps on one chevron within 320 ms fold the whole subtree instead.
    await page.waitForTimeout(400);
  }
  if ((await rowInfo(page, 12)).find(r => r.id === epic.id)?.folded) { await tap(page, v, `${rowSel(epic.id)} [data-chev]`); await page.waitForTimeout(400); }
}

async function measureChip(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page } = s;
  for (let run = 0; run < RUNS; run++) {
    await quiet(page);
    const count = await rowCount(page);
    const t = await timed(page, ["rowCountNot", String(count)], () => tap(page, v, '#chips [data-chip="closed"]'));
    record("chip", v, size, run, { toFrame: t.toFrame, toDom: t.toDom, eventDur: t.eventDur, rows: await rowCount(page) });
    await page.waitForTimeout(150);
  }
  if (RUNS % 2) { await tap(page, v, '#chips [data-chip="closed"]'); await page.waitForTimeout(300); }
}

async function measureBoard(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page } = s;
  for (let run = 0; run < RUNS; run++) {
    await quiet(page);
    const t = await timed(page, ["boardReady"], () => tap(page, v, '#bar [data-view="board"]'), 30000);
    record("board", v, size, run, { toFrame: t.toFrame, toDom: t.toDom, eventDur: t.eventDur, cards: await page.locator("#vBoard [data-card]").count() });
    await tap(page, v, '#bar [data-view="tree"]');
    await waitDom(page, ["treeShown"]);
    await page.waitForTimeout(150);
  }
}

/** openLeaves are rows that read ○ and have no children: a swipe starts them. */
async function openLeaves(page: Page, skip: Set<string>, n: number): Promise<string[]> {
  return (await rowInfo(page, 200)).filter(r => r.glyph === "○" && !r.kids && !skip.has(r.id)).slice(0, n).map(r => r.id);
}

/** measureSwipe times a short swipe to the status glyph, then the write behind it to the row the refetched snapshot draws. */
async function measureSwipe(s: Session, v: Variant, size: number | string, used: Set<string>): Promise<void> {
  const { page } = s;
  const ids = await openLeaves(page, used, RUNS);
  if (ids.length < RUNS) throw new Error(`only ${ids.length} open leaf rows to swipe`);
  for (const [run, id] of ids.entries()) {
    used.add(id);
    await quiet(page);
    const tracing = !!process.env.B9S_MEASURE_TRACE && !!s.cdp && run === 0;
    if (tracing) await s.browser.startTracing(page, { categories: TRACE_CATEGORIES });
    const since = await pageNow(page);
    const t = await timed(page, ["rowGlyph", `${id}|◐`], () => drag(page, rowSel(id), 0.35), 30000, [{ name: "write", pred: ["writeDone", String(since)], ms: v.network ? 120000 : 30000 }]);
    const done = await settle(page, "write");
    if (tracing) { fs.mkdirSync(OUT, { recursive: true }); fs.writeFileSync(path.join(OUT, `trace-write-${v.name}-${size}.json`), await s.browser.stopTracing()); }
    const snap = await firstMutationAfter(page, t.input);
    const fx = await fetchesAfter(page, t.t0);
    const post = fx.find(f => f.method === "POST" && f.url.endsWith("api/write"))!;
    const refetch = fx.find(f => f.url.endsWith("api/snapshot") && f.start >= post.start && f.end > 0)!;
    const changed = (await sseAfter(page, t.t0)).find(e => e.type === "changed");
    record("swipe", v, size, run, { toFrame: t.toFrame, toDom: t.toDom, snapBack: snap - t.input, eventDur: t.eventDur });
    record("write", v, size, run, {
      roundTrip: done.frame - post.start, postMs: post.end - post.start, postStart: post.start - t.input, sseMs: changed ? changed.t - post.start : NaN,
      snapshotStart: refetch.start - post.start, snapshotMs: refetch.end - refetch.start, renderMs: done.dom - refetch.end, paintMs: done.frame - done.dom,
      snapshots: fx.filter(f => f.url.endsWith("api/snapshot")).length,
    });
    await page.waitForTimeout(300);
  }
}

/** measureLive writes with the fake bd outside the browser and times the row through the watcher, SSE and the refetch. */
async function measureLive(s: Session, v: Variant, size: number | string, project: Project, used: Set<string>): Promise<void> {
  const { page } = s;
  const ids = await openLeaves(page, used, RUNS);
  if (ids.length < RUNS) throw new Error(`only ${ids.length} open leaf rows for live updates`);
  const file = path.join(project.dir, ".beads", "issues.jsonl");
  const origin = await page.evaluate(() => performance.timeOrigin);
  for (const [run, id] of ids.entries()) {
    used.add(id);
    await quiet(page);
    await target(page, rowSel(id));
    const t0 = await pageNow(page);
    await arm(page, ["rowGlyph", `${id}|◐`], v.network ? 120000 : 30000);
    execFileSync(process.execPath, [FAKE_BD, "update", id, "--status=in_progress"], { cwd: project.dir, stdio: "ignore" });
    const written = fs.statSync(file).mtimeMs;
    const r = await settle(page);
    const changed = (await sseAfter(page, t0)).find(e => e.type === "changed");
    const snap = (await fetchesAfter(page, t0)).find(f => f.url.endsWith("api/snapshot") && f.end > 0);
    const at = (t: number) => origin + t - written;
    record("live", v, size, run, { latency: at(r.frame), toDom: at(r.dom), sseMs: changed ? at(changed.t) : NaN, snapshotStart: snap ? at(snap.start) : NaN, snapshotEnd: snap ? at(snap.end) : NaN, snapshotMs: snap ? snap.end - snap.start : NaN });
    await page.waitForTimeout(400);
  }
}

/**
 * traceSummary reads a Chrome trace between two user timing marks: the
 * compositor's BeginFrames and DroppedFrames, and the renderer main thread's
 * busy time and tasks of 50 ms or more.
 */
function traceSummary(trace: Buffer, from: string, to: string): Record<string, number> {
  interface Ev { name: string; cat?: string; ph?: string; ts: number; dur?: number; pid?: number; tid?: number; args?: { name?: string } }
  let events: Ev[];
  try { const data = JSON.parse(trace.toString()); events = Array.isArray(data) ? data : data.traceEvents; } catch { return {}; }
  const mark = (name: string) => events.find(e => e.name === name && (e.cat || "").includes("blink.user_timing"));
  const a = mark(from), b = mark(to);
  if (!a || !b) return {};
  const threads = new Map<string, string>();
  for (const e of events) if (e.ph === "M" && e.name === "thread_name" && e.args?.name) threads.set(`${e.pid}:${e.tid}`, e.args.name);
  const out: Record<string, number> = { traceMs: (b.ts - a.ts) / 1000, traceBeginFrames: 0, traceDroppedFrames: 0, traceMainBusyMs: 0, traceLongTasks: 0 };
  for (const e of events) {
    if (e.ts < a.ts || e.ts > b.ts) continue;
    if (e.name === "BeginFrame") out.traceBeginFrames++;
    if (e.name === "DroppedFrame") out.traceDroppedFrames++;
    if (e.name === "ThreadControllerImpl::RunTask" && e.dur && threads.get(`${e.pid}:${e.tid}`) === "CrRendererMain") {
      out.traceMainBusyMs += e.dur / 1000;
      if (e.dur >= 50000) out.traceLongTasks++;
    }
  }
  out.traceMainBusyMs = Math.round(out.traceMainBusyMs);
  return out;
}

/** measureScroll flings the tree with a touch gesture or wheels it, and reads the frame times of a rAF loop meanwhile. */
async function measureScroll(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page, cdp } = s;
  for (let run = 0; run < RUNS; run++) {
    await quiet(page);
    await page.locator("#treeList").evaluate(el => { el.scrollTop = 0; });
    await page.waitForTimeout(150);
    const box = (await page.locator("#treeList").boundingBox())!;
    const tracing = !!cdp;
    if (tracing) await s.browser.startTracing(page, { categories: TRACE_CATEGORIES });
    const t0 = await page.evaluate(() => { const w = (window as W).__m; performance.mark("b9s-scroll-start"); w.startFrames(); return performance.now(); });
    if (cdp && v.touch) {
      await cdp.send("Input.synthesizeScrollGesture", { x: box.x + box.width / 2, y: box.y + box.height * 0.7, yDistance: -2500, speed: 1500, gestureSourceType: "touch" });
    } else if (v.touch) {
      // Mobile WebKit takes no wheel and Playwright has no touch swipe, so
      // the engine's own smooth scroll stands in for the fling.
      await page.locator("#treeList").evaluate(el => el.scrollTo({ top: 2500, behavior: "smooth" }));
      await page.waitForTimeout(1500);
    } else {
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      for (let n = 0; n < 12; n++) { await page.mouse.wheel(0, 250); await page.waitForTimeout(40); }
    }
    await page.waitForTimeout(120);
    const r = await page.evaluate(() => { const w = (window as W).__m; performance.mark("b9s-scroll-end"); return { frames: w.stopFrames(), t1: performance.now(), scrollTop: document.getElementById("treeList")!.scrollTop }; });
    const longtasks = await page.evaluate(([t0, t1]) => (window as W).__m.longtasks.filter(l => l.start >= t0 && l.start <= t1), [t0, r.t1] as const);
    let trace: Record<string, number> = {};
    if (tracing) {
      const buf = await s.browser.stopTracing();
      fs.mkdirSync(OUT, { recursive: true });
      if (run === 0) fs.writeFileSync(path.join(OUT, `trace-scroll-${v.name}-${size}.json`), buf);
      trace = traceSummary(buf, "b9s-scroll-start", "b9s-scroll-end");
    }
    record("scroll", v, size, run, { ...frameStats(r.frames), scrolledPx: r.scrollTop, longTasks: longtasks.length, longTaskMs: longtasks.reduce((a, l) => a + l.dur, 0), ...trace });
    await page.evaluate(() => performance.clearMarks());
  }
  await page.locator("#treeList").evaluate(el => { el.scrollTop = 0; });
}

/** measureDrag holds a row at 15% of its width, short of any action, and reads the frame times while it follows the finger. */
async function measureDrag(s: Session, v: Variant, size: number | string): Promise<void> {
  const { page } = s;
  const rows = (await rowInfo(page, 12)).slice(0, RUNS);
  for (const [run, row] of rows.entries()) {
    await quiet(page);
    await page.evaluate(() => (window as W).__m.startFrames());
    await drag(page, rowSel(row.id), 0.15, 30, 8);
    await page.waitForTimeout(60);
    const frames = await page.evaluate(() => (window as W).__m.stopFrames());
    record("drag", v, size, run, frameStats(frames));
    await page.waitForTimeout(200);
  }
}

/* ================= the report ================= */

const ORDER: Metric[] = ["load", "switch", "detail", "fold", "chip", "board", "swipe", "write", "live", "scroll", "drag"];
const fmt = (x: number) => (!Number.isFinite(x) ? "–" : Math.abs(x) < 10 ? x.toFixed(1) : String(Math.round(x)));

/**
 * writeSummary saves this test's samples beside the others and rebuilds the
 * summary from every file, so a worker restarted after a failure adds to
 * the report instead of starting it over.
 */
function writeSummary(name: string): void {
  fs.mkdirSync(OUT, { recursive: true });
  fs.writeFileSync(path.join(OUT, `samples-${name}.json`), JSON.stringify(samples.filter(s => s.name === name)));
  const all: Sample[] = fs.readdirSync(OUT).filter(f => f.startsWith("samples-") && f.endsWith(".json")).flatMap(f => JSON.parse(fs.readFileSync(path.join(OUT, f), "utf8")) as Sample[]);
  const groups = new Map<string, number[]>();
  for (const s of all) for (const [field, x] of Object.entries(s.values)) {
    if (!Number.isFinite(x)) continue;
    const key = [s.metric, s.variant, s.size, field].join("|");
    const g = groups.get(key);
    if (g) g.push(x); else groups.set(key, [x]);
  }
  const rows = [...groups].map(([key, xs]) => { const [metric, variant, size, field] = key.split("|"); return { metric: metric as Metric, variant, size, field, ...stats(xs) }; });
  const env = { node: process.version, platform: `${process.platform} ${os.release()}`, arch: process.arch, cpu: os.cpus()[0]?.model, cores: os.cpus().length, memoryGB: Math.round(os.totalmem() / 2 ** 30), bin: BIN, remote: REMOTE || null, runs: RUNS, chromium: process.env.B9S_MEASURE_HEADLESS === "1" ? "headless" : "headed off-screen", loadAverage: os.loadavg().map(x => +x.toFixed(1)), generated: new Date().toISOString() };
  fs.writeFileSync(path.join(OUT, "summary.json"), JSON.stringify({ env, stats: rows, samples: all }, null, 1));
  const sizes = [...new Set(rows.map(r => r.size))];
  let md = `# b9s web measurements\n\nGenerated ${env.generated} on ${env.cpu} (${env.cores} cores), node ${env.node}, ${env.runs} runs per metric; median and p95 (nearest rank) in ms unless the field says otherwise.\n`;
  for (const metric of ORDER) {
    const mine = rows.filter(r => r.metric === metric);
    if (!mine.length) continue;
    md += `\n## ${metric}\n\n| variant | field | ${sizes.map(s => `${s} median | ${s} p95`).join(" | ")} |\n|---|---|${sizes.map(() => "---:|---:").join("|")}|\n`;
    const fields = [...new Set(mine.map(r => r.field))];
    for (const variant of VARIANTS.map(v => v.name)) for (const field of fields) {
      const cells = sizes.map(size => { const r = mine.find(r => r.variant === variant && r.size === size && r.field === field); return r ? `${fmt(r.median)} | ${fmt(r.p95)}` : "– | –"; });
      if (cells.some(c => c !== "– | –")) md += `| ${variant} | ${field} | ${cells.join(" | ")} |\n`;
    }
  }
  fs.writeFileSync(path.join(OUT, "summary.md"), md);
}

/* ================= the runs ================= */

for (const size of SIZES) {
  test.describe(`${size} issues`, () => {
    let project: Project | null = null;
    let root = "";
    let url = REMOTE;

    test.beforeAll(async () => {
      if (REMOTE) return;
      // Two projects of the same size: the server starts in one, and
      // --projects-root lists the other for the project switch.
      root = fs.mkdtempSync(path.join(os.tmpdir(), "b9s-measure-root-"));
      const first = path.join(root, "first");
      const second = path.join(root, "second", ".beads");
      fs.mkdirSync(second, { recursive: true });
      fs.writeFileSync(path.join(second, "issues.jsonl"), toJSONL(measureFixture(size as number, "k")));
      project = await startProject(measureFixture(size as number, "m"), {}, false, undefined, ["--projects-root", root], first);
      url = project.url;
    });

    test.afterAll(async () => {
      if (project) stopProject(project);
      if (root && !process.env.B9S_KEEP_TEST_FIXTURES) fs.rmSync(root, { recursive: true, force: true });
    });

    for (const v of VARIANTS) {
      test(v.name, async ({ playwright }) => {
        test.skip(ONLY_VARIANTS.length > 0 && !ONLY_VARIANTS.includes(v.name), "left out by B9S_MEASURE_VARIANTS");
        const has = (m: Metric) => (!v.only || v.only.includes(m)) && !(REMOTE && WRITES.includes(m));
        const browser = await browserFor(playwright, v.engine);
        if (has("load")) await measureLoad(browser, v, url, size);
        const s = await newSession(browser, v, url);
        try {
          await s.page.goto(url);
          await waitDom(s.page, ["live"], v.network ? 180000 : 60000);
          if (has("switch")) await measureSwitch(s, v, size);
          if (has("detail")) await measureDetail(s, v, size);
          if (has("fold")) await measureFold(s, v, size);
          if (has("chip")) await measureChip(s, v, size);
          if (has("board")) await measureBoard(s, v, size);
          const used = new Set<string>();
          if (has("swipe")) await measureSwipe(s, v, size, used);
          if (has("live") && project) await measureLive(s, v, size, project, used);
          if (has("scroll")) await measureScroll(s, v, size);
          if (has("drag")) await measureDrag(s, v, size);
          if (REMOTE) expect(s.posts, "a running server gets no write, only a project switch").toEqual([]);
        } finally {
          await s.close();
          writeSummary(sampleName(v, size));
        }
      });
    }
  });
}
