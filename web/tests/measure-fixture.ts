// Fixtures for measure.spec.ts: a project of n issues shaped like a real
// export. Epics hold features and tasks, features hold tasks, some tasks
// block a sibling, most issues carry a description and some carry comments,
// so the snapshot, the tree, the board and the detail cost what they cost on
// a real project. The random source is seeded with n, so every run on every
// machine measures the same project.
//
// Run directly to write the JSONL for a `b9s web` of your own:
//   node web/tests/measure-fixture.ts 5000 > /tmp/measure-5000.jsonl

import path from "node:path";
import { fileURLToPath } from "node:url";
import { toJSONL, type FixtureIssue } from "./harness.ts";

export const MEASURE_SIZES = [1000, 5000] as const;

/** mulberry32 is a small seeded generator: the same seed gives the same project. */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const PEOPLE = ["alice", "bob", "carol", "dave", "erin", "frank", "grace", "heidi"];
const LABELS = ["ui", "api", "perf", "docs", "mobile", "infra", "bug-bash", "needs-design", "tech-debt", "release"];
const VERBS = ["Draw", "Serve", "Cache", "Measure", "Fold", "Render", "Sync", "Parse", "Index", "Stream", "Page", "Sort"];
const NOUNS = ["the tree", "the board", "the detail sheet", "the snapshot", "epic lanes", "the query bar", "comments", "the status chips", "live updates", "the project sheet", "the write log", "the gesture help"];
const AREAS = ["Phone web", "Desktop web", "Dolt reader", "Board", "Search", "Memory", "Pairing", "Identity", "Export", "Watcher"];
const WORDS = "the phone shows every issue the terminal shows and the write path runs one bd command per change so a row moves at once and the server refetches the snapshot when bd answers".split(" ");

export function measureFixture(n: number, prefix = "m"): FixtureIssue[] {
  const rnd = mulberry32(n);
  const pick = <T,>(a: readonly T[]): T => a[Math.floor(rnd() * a.length)];
  const chance = (p: number) => rnd() < p;
  const sentence = (words: number) => {
    const w: string[] = [];
    for (let k = 0; k < words; k++) w.push(pick(WORDS));
    return w[0][0].toUpperCase() + w.join(" ").slice(1) + ".";
  };
  const description = () => {
    const paras = 1 + Math.floor(rnd() * 3);
    const out: string[] = [];
    for (let k = 0; k < paras; k++) out.push(sentence(8 + Math.floor(rnd() * 20)) + " " + sentence(6 + Math.floor(rnd() * 12)));
    if (chance(0.3)) out.push("- " + sentence(5) + "\n- " + sentence(6) + "\n- " + sentence(4));
    if (chance(0.2)) out.push("Run `bd update " + prefix + "-x --status=in_progress` first.");
    return out.join("\n\n");
  };
  const status = (): string => {
    const r = rnd();
    if (r < 0.5) return "open";
    if (r < 0.6) return "in_progress";
    if (r < 0.9) return "closed";
    return "deferred";
  };
  const priority = (): number => {
    const r = rnd();
    return r < 0.03 ? 0 : r < 0.18 ? 1 : r < 0.68 ? 2 : r < 0.9 ? 3 : 4;
  };
  const labels = (): string[] => {
    const out = new Set<string>();
    const k = chance(0.5) ? 0 : chance(0.6) ? 1 : 2;
    while (out.size < k) out.add(pick(LABELS));
    return [...out];
  };
  const comments = (): string[] => {
    if (!chance(0.4)) return [];
    const out: string[] = [];
    const k = 1 + Math.floor(rnd() * 3);
    for (let c = 0; c < k; c++) out.push(sentence(10 + Math.floor(rnd() * 15)));
    return out;
  };
  const title = (short: string) => `[${short}] ${pick(VERBS)} ${pick(NOUNS)}`;
  let minutes = 0;
  const issue = (id: string, short: string, type: string, rest: Partial<FixtureIssue> = {}): FixtureIssue => ({
    id, title: title(short), type, status: status(), priority: priority(),
    assignee: chance(0.6) ? pick(PEOPLE) : "", labels: labels(), description: description(), comments: comments(),
    updatedMinutesAgo: (minutes += 1 + Math.floor(rnd() * 90)),
    ...rest,
  });

  const out: FixtureIssue[] = [];
  // Every epic holds three features of four tasks and five issues of its own:
  // 21 issues under 1 epic, so n/22 epics fill about 95% of the project and
  // loose issues fill the rest.
  const epics = Math.max(1, Math.round(n / 22));
  for (let e = 0; e < epics && out.length < n; e++) {
    const eid = `${prefix}-e${e}`;
    out.push(issue(eid, `e${e}`, "epic", { title: `[e${e}] Epic: ${pick(AREAS)} ${e}`, status: chance(0.8) ? "open" : "closed" }));
    const siblings: string[] = [];
    for (let f = 0; f < 3 && out.length < n; f++) {
      const fid = `${eid}.f${f}`;
      out.push(issue(fid, `e${e}.f${f}`, "feature", { parent: eid }));
      siblings.push(fid);
      for (let t = 0; t < 4 && out.length < n; t++) {
        const tid = `${fid}.${t}`;
        const blocked = t > 0 && chance(0.15);
        out.push(issue(tid, `e${e}.f${f}.${t}`, chance(0.8) ? "task" : "bug", { parent: fid, ...(blocked ? { blockedBy: [`${fid}.${t - 1}`], status: "open" } : {}) }));
      }
    }
    for (let t = 0; t < 5 && out.length < n; t++) {
      const tid = `${eid}.${t}`;
      const blocked = t > 0 && chance(0.1);
      out.push(issue(tid, `e${e}.${t}`, pick(["task", "task", "bug", "chore"]), { parent: eid, ...(blocked ? { blockedBy: [pick(siblings)], status: "open" } : {}) }));
    }
  }
  for (let k = 0; out.length < n; k++) out.push(issue(`${prefix}-l${k}`, `l${k}`, pick(["task", "task", "task", "bug", "chore"])));
  return out;
}

// The CLI: node web/tests/measure-fixture.ts <issues> [prefix]
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const n = Number(process.argv[2] || 1000);
  if (!Number.isInteger(n) || n < 1) {
    process.stderr.write("usage: node web/tests/measure-fixture.ts <issues> [prefix]\n");
    process.exit(2);
  }
  process.stdout.write(toJSONL(measureFixture(n, process.argv[3] || "m")));
}
