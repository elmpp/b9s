// Adding a project from the browser (ADR 0057): the hand-off to an AI with
// its live checklist, and a plan without code that b9s creates itself.
// The "AI" here is the test writing files into the repository; the fake bd
// on PATH answers bd init and bd count.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { SAMPLE, expect, row, startProject, stopProject, test, type Project } from "./harness";

let tmp = "";
let project: Project;

test.beforeEach(async () => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), "b9s-onboard-e2e-"));
  fs.mkdirSync(path.join(tmp, "code", "app"), { recursive: true });
  execFileSync("git", ["init", "-q", path.join(tmp, "code", "app")]);
  fs.mkdirSync(path.join(tmp, "plans"));
  project = await startProject(SAMPLE, { B9S_REPO_ROOTS: path.join(tmp, "code") }, false, undefined, ["--projects-root", path.join(tmp, "plans")]);
});

test.afterEach(() => {
  stopProject(project);
  if (!process.env.B9S_KEEP_TEST_FIXTURES) fs.rmSync(tmp, { recursive: true, force: true });
});

const sheet = (page: import("@playwright/test").Page) => page.locator("#sheetHost .msheet");
const choose = (page: import("@playwright/test").Page, text: string) => sheet(page).locator("button.opt", { hasText: text }).first();

test("the #/add link hands a repository to the AI and ticks each step it sees", async ({ page }) => {
  await page.goto(project.url + "#/add");
  await expect(page.locator("#boot")).toBeHidden({ timeout: 10000 });
  await expect(sheet(page).locator("h3")).toHaveText("Add a project");
  await choose(page, "In a code repository").click();
  await choose(page, "app").click();

  await expect(sheet(page).locator("h3")).toHaveText("Set up app");
  await expect(sheet(page).locator(".obPrompt")).toContainText("b9s onboard --agent");
  await expect(sheet(page).locator('#obSteps tr[data-step="git"]')).toHaveClass(/done/);
  await expect(sheet(page).locator('#obSteps tr[data-step="beads"]')).not.toHaveClass(/done/);

  // The AI's work, as it lands in the repository.
  const repo = path.join(tmp, "code", "app");
  fs.mkdirSync(path.join(repo, ".beads"));
  fs.writeFileSync(path.join(repo, ".beads", "metadata.json"), "{}\n");
  fs.writeFileSync(path.join(repo, ".beads", "issues.jsonl"), JSON.stringify({
    id: "a-1", title: "From the AI", status: "open", priority: 2, issue_type: "task",
    created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
  }) + "\n");
  fs.writeFileSync(path.join(repo, "AGENTS.md"), "Track work with bd.\n");

  await expect(sheet(page).locator('#obSteps tr[data-step="beads"]')).toHaveClass(/done/, { timeout: 6000 });
  await expect(sheet(page).locator('#obSteps tr[data-step="instructions"]')).toHaveClass(/done/);
  await expect(sheet(page).locator('#obSteps tr[data-step="issues"]')).toHaveClass(/done/);
  await expect(sheet(page).locator('#obSteps tr[data-step="committed"]')).not.toHaveClass(/done/);

  await choose(page, "Open app now").click();
  await expect(sheet(page)).toHaveCount(0);
  await expect(row(page, "a-1")).toBeVisible({ timeout: 10000 });
});

test("a plan without code gets its own folder and a prompt for the AI", async ({ page }) => {
  await page.goto(project.url);
  await expect(page.locator("#boot")).toBeHidden({ timeout: 10000 });
  await page.locator('[data-act="projects"]').first().click();
  await choose(page, "Add a project").click();
  await choose(page, "A plan without code").click();
  await sheet(page).locator("#obName").fill("Q1 roadmap");
  await sheet(page).locator('[data-sa="obcreate"]').click();

  await expect(sheet(page).locator("h3")).toHaveText("Fill Q1 roadmap", { timeout: 10000 });
  expect(fs.existsSync(path.join(tmp, "plans", "q1-roadmap", ".beads", "metadata.json"))).toBe(true);

  await sheet(page).locator("#obGoal").fill("Launch the pricing page by March");
  await sheet(page).locator('[data-sa="obplanprompt"]').click();
  await expect(sheet(page).locator(".obPrompt")).toContainText("Launch the pricing page by March");
  await expect(sheet(page).locator("#obGoal")).toHaveValue("Launch the pricing page by March");
});

test("a second plan with the same name is refused and leaves the first alone", async ({ page }) => {
  fs.mkdirSync(path.join(tmp, "plans", "q1-roadmap"));
  await page.goto(project.url + "#/add");
  await expect(page.locator("#boot")).toBeHidden({ timeout: 10000 });
  await choose(page, "A plan without code").click();
  await sheet(page).locator("#obName").fill("Q1 roadmap");
  await sheet(page).locator('[data-sa="obcreate"]').click();
  await expect(page.locator("#toast")).toContainText("already exists");
  await expect(row(page, "t-1")).toBeAttached();
});
