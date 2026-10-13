// Adding a project (ADR 0057). B9s leads and the user's AI does the work:
// the hand-off sheet gives the AI one short prompt, the AI runs
// `b9s onboard` for the full brief, and the checklist below the prompt reads
// the repository every two seconds until the setup is committed. B9s itself
// runs only `bd init`, for a plan without code or when the user asks for it.
//
//   add ──▶ addrepo ──▶ handoff ──(checklist complete)──▶ open the project
//    │         └─(already has Beads)──────────────────────▶ open the project
//    └──▶ newplan ──(bd init in ~/B9s/<name>)──▶ plan ──▶ copy the prompt

import * as api from "./api";
import type { OnboardAgent, OnboardCheck, OnboardRepo } from "./api.gen";
import { closeSheet, enterProject, openSheet, opt, renderSheet, toast } from "./actions";
import { D } from "./data";
import { S, type Sheet } from "./state";
import { $opt, copyText, esc, parseTime, age, store } from "./util";

const POLL_MS = 2000;

let repos: OnboardRepo[] | null = null;
let agents: OnboardAgent[] = [];
/** local is the server's view: true when this browser runs on the computer b9s web runs on. */
let local = false;
let reposError = "";
let check: OnboardCheck | null = null;
let checkError = "";
let pollTimer = 0;
let planText = "";

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

function defaultAgent(): string {
  const saved = store.get<string>("onboardAgent", "");
  if (saved) return saved;
  return agents.find(a => a.installed && a.id !== "other")?.id || "claude";
}

const agentLabel = (id: string) => agents.find(a => a.id === id)?.label || id;

/** openAdd opens the first sheet: where does the work live? */
export function openAdd(): void {
  if (D.public) { toast("This demo shows one project"); return; }
  openSheet("add");
  void loadRepos();
}

async function loadRepos(): Promise<void> {
  repos = null; reposError = "";
  try {
    const r = await api.onboardRepos();
    repos = r.repos; agents = r.agents; local = r.local;
  } catch (e) { reposError = errText(e); }
  if (S.sheet && (S.sheet.kind === "add" || S.sheet.kind === "addrepo")) renderSheet();
}

function openHandoff(path: string): void {
  check = null; checkError = "";
  openSheet("handoff", { path, agent: defaultAgent() });
  void poll();
}

/**
 * poll reads the checklist while the hand-off sheet for that folder stays
 * open, and stops by itself once the sheet closes or shows another folder.
 */
async function poll(): Promise<void> {
  clearTimeout(pollTimer);
  const sh = S.sheet;
  if (!sh || sh.kind !== "handoff") return;
  const path = String(sh.path), agent = String(sh.agent);
  try {
    check = await api.onboardCheck(path, agent);
    checkError = "";
  } catch (e) { checkError = errText(e); }
  const now = S.sheet;
  if (!now || now.kind !== "handoff" || now.path !== path) return;
  if (now.agent === agent) paintChecklist();
  pollTimer = window.setTimeout(() => void poll(), POLL_MS);
}

/** paintChecklist redraws what a check changes, so a poll neither moves the scroll nor drops focus. */
function paintChecklist(): void {
  const el = $opt("#obSteps"), done = $opt("#obDone"), prompt = $opt("#obPrompt");
  if (prompt && check) prompt.textContent = check.prompt;
  if (el) el.innerHTML = checklistHTML();
  if (done) done.innerHTML = doneHTML();
}

function checklistHTML(): string {
  if (checkError) return `<div class="empty err">${esc(checkError)}</div>`;
  if (!check) return `<div class="empty">Reading the folder…</div>`;
  return `<table class="gtable obSteps">${check.steps.map(s =>
    `<tr class="${s.done ? "done" : ""}" data-step="${esc(s.id)}"><td aria-label="${s.done ? "done" : "to do"}">${s.done ? "✓" : "○"}</td><td>${esc(s.title)}${s.detail ? `<small>${esc(s.detail)}</small>` : ""}</td></tr>`).join("")}</table>`;
}

function doneHTML(): string {
  if (!check) return "";
  const beads = check.steps.find(s => s.id === "beads")?.done;
  if (check.complete) return opt("obopen", "✓", "Open " + esc(check.name), "the setup is committed", { cls: "cur" });
  if (beads) return opt("obopen", "→", "Open " + esc(check.name) + " now", "the checklist is not complete yet");
  return "";
}

function repoRow(r: OnboardRepo): string {
  const when = r.modified ? age(parseTime(r.modified)) : "";
  return opt(r.has_beads ? "obopenpath" : "obrepo", r.has_beads ? "●" : "○", esc(r.name),
    esc(r.path.replace(/^\/Users\/[^/]+/, "~")) + (when ? " · " + esc(when) : "") + (r.has_beads ? " · has Beads" : ""), { val: r.path });
}

export function onboardBody(sh: Sheet): [string, string, string] | null {
  switch (sh.kind) {
    case "add": return ["Add a project", "Where does the work live?",
      opt("obrepos", "⌂", "In a code repository", "your AI sets up Beads; B9s shows each step")
      + opt("obplan", "✎", "A plan without code", "a roadmap or discovery work: B9s creates it now")
      + (reposError ? `<div class="empty err">${esc(reposError)}</div>` : "")];
    case "addrepo": {
      if (reposError) return ["Choose the repository", "", `<div class="empty err">${esc(reposError)}</div>`];
      if (!repos) return ["Choose the repository", "", `<div class="empty">Looking for repositories…</div>`];
      const pick = local ? opt("obpick", "…", "Choose a folder…", "opens the macOS folder picker") : "";
      const rows = repos.length ? `<div class="sec">Recent repositories</div>` + repos.map(repoRow).join("") : `<div class="empty">No git repositories found in your usual folders.</div>`;
      return ["Choose the repository", "The folder that holds the code", pick
        + `<div class="form"><label>Folder<input id="obPath" placeholder="/Users/me/code/app" autocapitalize="off" autocorrect="off" spellcheck="false"></label></div>`
        + `<div class="fbtns"><button data-sa="cancel">Cancel</button><button class="pri" data-sa="obpath">Use this folder</button></div>` + rows];
    }
    case "handoff": {
      const agent = String(sh.agent), path = String(sh.path);
      const name = path.split("/").pop() || path;
      const prompt = check?.prompt && check.agent === agent ? check.prompt : "";
      const installed = agents.find(a => a.id === agent)?.installed;
      const tabs = `<div class="segs obAgents" data-seg="agent">${(agents.length ? agents : [{ id: agent, label: agent, installed: false }]).map(a =>
        `<button type="button" data-sa="obagent" data-val="${esc(a.id)}" class="${a.id === agent ? "on" : ""}">${esc(a.label)}</button>`).join("")}</div>`;
      const start = agent !== "other" && local
        ? opt("obstart", "▶", "Start " + esc(agentLabel(agent)), installed ? "in Terminal, in this folder, with the prompt" : "not found on this computer", installed ? {} : { cls: "dim" })
        : "";
      return ["Set up " + esc(name), "Your AI does the setup. B9s ticks off each step it sees.",
        `<div class="form"><label>Your AI${tabs}</label></div>`
        + `<div class="sec">Give it this prompt</div><div class="cmd obPrompt" id="obPrompt">${esc(prompt || "…")}</div>`
        + opt("obcopy", "⧉", "Copy the prompt", agent === "other" ? "paste it into your AI, in this folder" : "")
        + start
        + (local ? opt("obterm", "›_", "Open in Terminal", "a shell in this folder with b9s and bd on PATH") : "")
        + `<div class="sec">Progress</div><div id="obSteps">${checklistHTML()}</div><div id="obDone">${doneHTML()}</div>`
        + `<div class="sec">Other ways</div>`
        + opt("obmate", "✉", "Ask a teammate", "copies a message that explains the setup")
        + opt("obinit", "◇", "Let B9s create the Beads project", "bd init only: no instructions or hooks for your AI")];
    }
    case "newplan": return ["A plan without code", "B9s runs bd init in its own folder. No AI, no git.",
      `<div class="form"><label>Name<input id="obName" placeholder="Q1 roadmap" autocapitalize="sentences"></label></div>`
      + `<div class="fbtns"><button data-sa="cancel">Cancel</button><button class="pri" data-sa="obcreate">Create plan</button></div>`];
    case "plan": return ["Fill " + esc(String(sh.name || "the plan")), "Optional: let your AI write the first issues",
      `<div class="form"><label>Goal<textarea id="obGoal" placeholder="Launch the new pricing page by March">${esc(sh.goal || "")}</textarea></label>
        <label class="chk"><input type="checkbox" id="obPlanOnly"${sh.planOnly === false ? "" : " checked"}> Plan only: no code</label>
        <label class="chk"><input type="checkbox" id="obAskFirst"${sh.askFirst === false ? "" : " checked"}> Show me the issues before you create them</label></div>`
      + `<div class="fbtns"><button data-sa="cancel">Done</button><button class="pri" data-sa="obplanprompt">Copy the prompt</button></div>`
      + (planText ? `<div class="cmd obPrompt">${esc(planText)}</div>` : "")];
  }
  return null;
}

async function openProjectAt(open: () => Promise<unknown>): Promise<void> {
  clearTimeout(pollTimer);
  closeSheet();
  await enterProject(open);
}

export async function onboardAction(a: string, val: string | undefined): Promise<void> {
  const sh = S.sheet;
  switch (a) {
    case "obadd": openAdd(); break;
    case "obrepos": openSheet("addrepo"); if (!repos) void loadRepos(); break;
    case "obplan": openSheet("newplan"); requestAnimationFrame(() => $opt("#obName")?.focus()); break;
    case "obrepo": openHandoff(val!); break;
    case "obopenpath": await openProjectAt(() => api.addProject(val!)); break;
    case "obpath": {
      const v = ($opt<HTMLInputElement>("#obPath")?.value || "").trim();
      if (!v) { $opt("#obPath")?.focus(); break; }
      const known = repos?.find(r => r.path === v);
      if (known?.has_beads) { await openProjectAt(() => api.addProject(v)); break; }
      openHandoff(v);
      break;
    }
    case "obpick": {
      try {
        const r = await api.pickFolder();
        if (r.text) openHandoff(r.text);
      } catch (e) { toast("Folder picker: " + errText(e)); }
      break;
    }
    case "obagent": {
      if (!sh || sh.kind !== "handoff" || sh.agent === val) break;
      store.set("onboardAgent", val);
      sh.agent = val;
      check = null;
      renderSheet();
      void poll();
      break;
    }
    case "obcopy": {
      const text = check?.prompt || "";
      toast(text && await copyText(text) ? "Prompt copied: paste it into " + agentLabel(String(sh?.agent)) : "Clipboard refused. Select the prompt and copy it.");
      break;
    }
    case "obmate": {
      const text = check?.teammate || "";
      toast(text && await copyText(text) ? "Message copied" : "Clipboard refused");
      break;
    }
    case "obstart": {
      if (!sh) break;
      try {
        await api.startAgent(String(sh.path), String(sh.agent));
        toast(agentLabel(String(sh.agent)) + " started in Terminal");
      } catch (e) { toast("Could not start: " + errText(e)); }
      break;
    }
    case "obterm": {
      if (!sh) break;
      try { await api.openTerminal(String(sh.path)); } catch (e) { toast("Terminal: " + errText(e)); }
      break;
    }
    case "obtermhere": {
      closeSheet();
      try { await api.openTerminal("", true); toast("b9s opened in Terminal"); } catch (e) { toast("Terminal: " + errText(e)); }
      break;
    }
    case "obinit": if (sh) await openProjectAt(() => api.initBeads(String(sh.path))); break;
    case "obopen": if (sh && check) { const p = check.path; await openProjectAt(() => api.addProject(p)); } break;
    case "obcreate": {
      const name = ($opt<HTMLInputElement>("#obName")?.value || "").trim();
      if (!name) { $opt("#obName")?.focus(); break; }
      try {
        await api.createPlan(name);
      } catch (e) { toast("Could not create the plan: " + errText(e)); break; }
      planText = "";
      // createPlan has opened the plan on the server; show it under the next sheet.
      await enterProject(() => Promise.resolve());
      openSheet("plan", { name });
      break;
    }
    case "obplanprompt": {
      const goal = ($opt<HTMLTextAreaElement>("#obGoal")?.value || "").trim();
      if (!goal) { $opt("#obGoal")?.focus(); break; }
      const planOnly = !!$opt<HTMLInputElement>("#obPlanOnly")?.checked, askFirst = !!$opt<HTMLInputElement>("#obAskFirst")?.checked;
      try {
        planText = (await api.planPrompt({ goal, plan_only: planOnly, ask_first: askFirst })).text;
      } catch (e) { toast(errText(e)); break; }
      if (sh) Object.assign(sh, { goal, planOnly, askFirst });
      renderSheet();
      toast(await copyText(planText) ? "Prompt copied" : "Clipboard refused. Select the prompt below.");
      break;
    }
  }
}

/** addLink reports whether the page was opened on the add flow's link, #/add. */
export const addLink = (): boolean => /^#\/add\b/.test(location.hash);
