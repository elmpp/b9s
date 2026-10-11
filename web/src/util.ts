// Small helpers shared by every view. No framework: the app renders HTML
// strings into a handful of fixed hosts, the way the mockup does.

export const $ = <T extends HTMLElement = HTMLElement>(sel: string): T => document.querySelector(sel) as T;
export const $opt = <T extends HTMLElement = HTMLElement>(sel: string): T | null => document.querySelector(sel) as T | null;

const ESC: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
export const esc = (s: unknown): string => String(s == null ? "" : s).replace(/[&<>"']/g, c => ESC[c]);

export function age(t: number): string {
  const s = Math.max(0, (Date.now() - t) / 1000);
  if (s < 60) return Math.floor(s) + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 86400) return Math.floor(s / 3600) + "h";
  if (s < 86400 * 14) return Math.floor(s / 86400) + "d";
  if (s < 86400 * 60) return Math.floor(s / 604800) + "w";
  return Math.floor(s / 2592000) + "mo";
}

export const fmtDate = (t: number): string => {
  if (!t) return "—";
  const d = new Date(t), p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
};

export const parseTime = (s: string): number => (s ? Date.parse(s) || 0 : 0);

/**
 * md renders the small Markdown subset issue text uses: headings, lists,
 * fences, inline code, bold and links. Everything is escaped first, and a
 * link keeps only its text: an issue must not be able to navigate the app.
 */
export function md(src: string): string {
  const out: string[] = [];
  let list: { t: "ul" | "ol"; items: string[] } | null = null;
  // Consecutive text lines form one paragraph, as in Markdown, so hard-wrapped
  // text reads as prose and bold may span a line break.
  let para: string[] = [];
  let code: string[] | null = null;
  let codeLanguage = "";
  const inline = (s: string) => esc(s)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "<u>$1</u>");
  const flush = () => {
    if (para.length) { out.push(`<p>${inline(para.join(" "))}</p>`); para = []; }
    if (list) { out.push(`<${list.t}>${list.items.map(x => `<li>${inline(x)}</li>`).join("")}</${list.t}>`); list = null; }
  };
  for (const line of src.split("\n")) {
    if (line.startsWith("```")) {
      if (code) {
        const source = esc(code.join("\n"));
        out.push(codeLanguage === "mermaid" ? `<div class="mermaid-diagram"><pre><code>${source}</code></pre></div>` : `<pre><code>${source}</code></pre>`);
        code = null;
      } else { flush(); code = []; codeLanguage = line.slice(3).trim().toLowerCase(); }
      continue;
    }
    if (code) { code.push(line); continue; }
    let m: RegExpMatchArray | null;
    if ((m = line.match(/^#{1,6}\s+(.*)/))) { flush(); out.push(`<h3>${inline(m[1])}</h3>`); }
    else if ((m = line.match(/^\s*[-*]\s+(.*)/))) {
      if (!list || list.t !== "ul") { flush(); list = { t: "ul", items: [] }; }
      list.items.push(m[1]);
    } else if ((m = line.match(/^\s*\d+[.)]\s+(.*)/))) {
      if (!list || list.t !== "ol") { flush(); list = { t: "ol", items: [] }; }
      list.items.push(m[1]);
    } else if (!line.trim()) flush();
    // A line straight after a list item continues it, as Markdown's lazy continuation does.
    else if (list) list.items[list.items.length - 1] += " " + line.trim();
    else para.push(line.trim());
  }
  if (code) out.push(`<pre><code>${esc((code as string[]).join("\n"))}</code></pre>`);
  flush();
  return out.join("");
}

/**
 * The size classes of ADR 0052, from the app's width alone. A phone on its
 * side is as wide as a tablet, and the split layout serves it better than a
 * half-height sheet on a short screen.
 */
export type SizeClass = "phone" | "tablet" | "desktop" | "wide";
export const SIZE_CLASSES: readonly SizeClass[] = ["phone", "tablet", "desktop", "wide"];
/** BREAKPOINTS are the app widths, in CSS px, at which each class above phone starts. app.css keys off the class on #app. */
export const BREAKPOINTS: Record<Exclude<SizeClass, "phone">, number> = { tablet: 768, desktop: 1280, wide: 2560 };
export const sizeClassOf = (width: number): SizeClass =>
  width >= BREAKPOINTS.wide ? "wide" : width >= BREAKPOINTS.desktop ? "desktop" : width >= BREAKPOINTS.tablet ? "tablet" : "phone";

/** The class on #app, so a read that finds the width in another class can change it. */
let current: SizeClass | null = null;
/** The app's width for the rest of this task. A task cannot see the viewport change, so one layout read serves every call in it. */
let width = -1;

/**
 * sizeClass is the class of the app's width right now, and puts it on #app.
 * It reads the width rather than remembering the observer's last answer:
 * a resize notifies a frame later, and a render in between must already
 * lay out for the new class, as a media query would have.
 */
export function sizeClass(): SizeClass {
  const app = $opt("#app");
  if (width < 0) {
    width = (app || document.documentElement).clientWidth;
    queueMicrotask(() => { width = -1; });
  }
  const now = sizeClassOf(width);
  if (app && now !== current) {
    app.classList.remove(...SIZE_CLASSES);
    app.classList.add(now);
    current = now;
  }
  return now;
}

/**
 * split is every class but phone: the detail is a side panel beside the list
 * rather than a sheet over it, the board lays its columns side by side, and a
 * rail at the left replaces the bottom bar.
 */
export const split = (): boolean => sizeClass() !== "phone";

/**
 * observeSize calls back once each time the app's width crosses into another
 * class, so a rotation or a resized window redraws at the breakpoint and not
 * on every pixel in between.
 */
export function observeSize(onChange: (now: SizeClass, was: SizeClass) => void): void {
  let seen = sizeClass();
  new ResizeObserver(() => {
    const now = sizeClass();
    if (now === seen) return;
    const was = seen;
    seen = now;
    onChange(now, was);
  }).observe($("#app"));
}

/** store keeps per-browser conveniences. Storage may be absent or refuse. */
export const store = {
  get<T>(k: string, d: T): T {
    try { const v = localStorage.getItem("b9s." + k); return v == null ? d : (JSON.parse(v) as T); } catch { return d; }
  },
  set(k: string, v: unknown): void {
    try { localStorage.setItem("b9s." + k, JSON.stringify(v)); } catch { /* storage is optional */ }
  },
  del(k: string): void {
    try { localStorage.removeItem("b9s." + k); } catch { /* storage is optional */ }
  },
};

export function haptic(ms: number): void {
  try { if (navigator.vibrate) navigator.vibrate(ms); } catch { /* not every platform vibrates */ }
}

/** uid makes idempotency keys. crypto.randomUUID needs a secure context, which plain LAN HTTP is not. */
export function uid(): string {
  try { if (crypto.randomUUID) return crypto.randomUUID(); } catch { /* fall through */ }
  return Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 12);
}

export async function copyText(text: string): Promise<boolean> {
  try { await navigator.clipboard.writeText(text); return true; } catch { /* fall back below */ }
  try {
    const ta = document.createElement("textarea");
    ta.value = text; ta.setAttribute("readonly", ""); ta.style.position = "fixed"; ta.style.opacity = "0";
    document.body.appendChild(ta); ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch { return false; }
}
