---
plan_slug: 2026-10-11-responsive-web
phase: implementation-plan
rig: b9s-alpha
rig_root: /Users/andrevanderheijden/gascity/rigs/b9s-alpha
artifact_root: /Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/plans
requirements_file: /Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/plans/2026-10-11-responsive-web/requirements.md
epic: b9m-vdp
status: approved
created_at: 2026-10-11T08:18:14Z
updated_at: 2026-10-11T08:21:40Z
---

# Implementation Plan: Responsive, native-feeling b9s web for every screen size

## Summary

Five features under epic `b9m-vdp`, run as one chain of tasks with a single
fork at the start:

```
  A1 measure ─────────────────────────────────────────────┐
                                                          ▼
  B1 mockups ─▶ B2 operator picks ─▶ B3 ADR ─▶ C1 tokens ─▶ C2 layout shell
      ─▶ D1 phone ─▶ D2 tablet ─▶ D3 desktop ─▶ D4 wide ─▶ D5 board+detail
      ─▶ D6 sheets+projects+memory ─▶ A2 speed budgets ─▶ E1 a11y ─▶ E2 docs
```

A1 and B1 run in parallel because they write disjoint files. Everything after
B2 edits `web/src/app.css` and `web/src/render.ts`, and the rig's workers share
one checkout (`rigs/b9s-alpha`, no worktrees), so those tasks run one at a
time. The operator approved the plan with the instruction to start the work.

## Current System

- **Frontend** (`web/src`, 4601 lines of TypeScript and CSS, no framework):
  `main.ts` boots, `state.ts` and `data.ts` hold state and fetches, `render.ts`
  (796 lines) builds the DOM, `gestures.ts` (680) handles touch, `keys.ts`
  (367) the TUI keybindings, `actions.ts` (753) writes with optimistic updates
  through `send(r, optimistic)`, `memory.ts` the Memory Beads views, `nav.ts`
  browser history, `api.gen.ts` the contract generated from Go types
  (`make web-types`).
- **Layout:** one switch, `WIDE = "(min-width: 720px) and (min-height:
  500px)"` (`web/src/util.ts:82`), used by `app.css:369` (detail becomes a side
  panel `min(460px, 42vw)`) and `gestures.ts:670` (re-render on change). The
  bottom bar (Tree, Board, Search, More) shows at every width.
- **Look:** Dracula tokens at the top of `app.css` (`--accent #bd93f9`,
  `--cyan`, `--green`, `--red`, `--orange`, `--yellow`, `--blue`, `--feat`,
  `--hi`), 47 literal colours in the stylesheet, `--mono` for all text.
- **Server:** `pkg/web/server.go` serves the embedded assets, the JSON API and
  Server-Sent Events. Writes run the `bd` CLI. Build: `make web` (esbuild),
  live preview `make web-dev`.
- **Tests:** Playwright in `web/tests` with projects `pixel` (Pixel 7),
  `iphone` (iPhone 14), `desktop` (1440 x 900), `ipad` (iPad Pro 11, wide and
  keys specs only), `pixel-perf` and `iphone-perf` for `perf.spec.ts`
  (1000 issues: load under 5 s, full re-render under 500 ms). The suite runs
  in CI (`b9m-foit.19`). Fixtures use `web/tests/fake-bd.mjs`. Go tests run
  offline (modules are vendored).
- **Docs:** ADRs up to `docs/adr/0051`; ADR 0019 serves the web UI from
  `b9s web`; mockups live in `docs/mockups/*.html`.

## Proposed Implementation

### A. Measure and speed

- **A1. Measure b9s web where it runs.** A Playwright script
  `web/tests/measure.spec.ts`, kept out of the default CI projects, records
  with the Performance API and Chrome traces: first load, project switch, tap
  or click to the first visible change, write round trip (request to the
  refetched row), live-update latency (a `bd` write to the row change on
  screen), and frame timing while scrolling and swiping. It runs against a
  local `b9s web` with 1000 and 5000 issues (a JSONL fixture made by a
  generator in the same folder) and, read-only, against beads.osen.co when the
  login proxy lets the worker in; when it does not, the report says so and
  gives the operator the one command to run. Writes are measured only
  against the local fixture with `fake-bd.mjs`, never against the shared
  server. Output: `docs/plans/2026-10-11-responsive-web/measurements.md` with
  the numbers, the method, the ranked slow paths and proposed budgets. No
  change to `web/src` or `pkg/web`.
- **A2. Meet the budgets.** Fix the slow paths that A1 ranks, set the budgets
  in `web/tests/perf.spec.ts` (tap feedback 100 ms, the rest from A1) for the
  phone, tablet and desktop perf projects, and make every write action in
  `actions.ts` give feedback before the `bd` round trip. Runs after D6 so that
  it measures the new layouts.

### B. Design direction

- **B1. Clickable mockups.** `docs/mockups/responsive-web/` with an
  `index.html` that links the screen classes (phone 390 px, tablet 820 and
  1180 px, desktop 1440 px, wide 2560 px) for tree, board, detail, search and
  edit. It offers two or three visual directions on the same screens, each a
  small token set (surfaces, text, lines, at most four accent colours for
  status, priority and danger), native web typography for text with monospace
  for IDs, code and commands, and native patterns per class (phone sheets and
  back, tablet split view, desktop sidebar, hover and context menu). Static
  HTML and CSS only, no build step, so it opens from the file system.
- **B2. The operator picks a direction.** A decision bead the mayor brings to
  the operator with screenshots of B1. Not slung to a worker.
- **B3. ADR 0052.** `docs/adr/0052-responsive-native-web-redesign.md` records
  the chosen direction, the size classes and their breakpoints, the token set,
  and that the web tokens leave the theme source shared with `pkg/ui`. It
  links the requirements, this plan and the mockups.

### C. Responsive foundation

- **C1. Design tokens.** Move every colour, font, size, spacing and radius in
  `app.css` into `web/src/tokens.css` with the ADR 0052 values. No literal
  colour stays outside it. Guard: a test (`web/tests/tokens.spec.ts` or a Go
  test over the embedded CSS) fails when `app.css` holds a literal colour.
- **C2. Size classes and the app shell.** Replace `WIDE` with size classes
  (`phone`, `tablet`, `desktop`, `wide`) computed from the app width and set
  as a class on `#app`, with a `ResizeObserver` and one place in `util.ts`
  that names the breakpoints from ADR 0052. The shell picks the navigation
  per class (bottom bar on phone, a sidebar or rail on tablet and up) and a
  list and detail split where the class allows it. Selection, scroll position
  and the open detail survive a resize or rotation. Playwright gains projects
  at 360, 412, 768 portrait, 1024 landscape, 1440, 1920 and 2560 px, and a
  spec that rotates and resizes with an open detail.

### D. Screens per size class

Each task applies ADR 0052 to its part and adds Playwright specs for the size
classes it touches.

- **D1. Phone.** Native-feeling navigation and sheets, calm tree rows with at
  least today's rows per screen, 44 px targets, the 24 px edge rule.
- **D2. Tablet.** List and detail side by side in both orientations, touch
  plus hardware keyboard.
- **D3. Desktop.** Sidebar navigation and no bottom bar, hover states, a
  context menu on rows and cards with the same actions as the swipe and long
  swipe, drag and drop on the board, visible focus, the TUI keys.
- **D4. Wide.** Panes and board columns grow with the width, the description
  keeps a readable line length.
- **D5. Board and detail.** Cards, epic lanes, the detail with Markdown,
  Mermaid, relations and comments in the new look at every class.
- **D6. Sheets, projects and Memory.** Search and the query language, edit,
  create and status sheets, projects and all-projects, source health and the
  Memory Beads views in the new look at every class.

### E. Quality and docs

- **E1. Accessibility and contrast.** axe-core checks in Playwright for every
  size class (WCAG 2.2 AA), keyboard-only and screen-reader labels on the new
  controls.
- **E2. Docs.** `docs/USER_GUIDE.md` web section, new
  `docs/screenshot-*.png` for phone, tablet and desktop, `README.md`, and the
  epic outcome.

## Testing

- Every task runs `make web` and the Playwright suite (`cd web && npx
  playwright test`), plus `go vet ./...` and `go test ./... -p 2 -skip
  DoltIntegration` when it touches Go.
- New Playwright projects (C2) make every later task test all size classes.
- Perf budgets (A2) and the token guard (C1) fail CI when they regress.
- No test writes to the shared Dolt server (`.claude/CLAUDE.md`, b9s rules).

## Rollout

- **Workers:** half the tasks go to `b9s-alpha/claude`, half to
  `b9s-alpha/codex`, alternating along the chain (label `worker:*`). Both
  providers run without a sandbox (`codex
  --dangerously-bypass-approvals-and-sandbox`), so either can install
  `web/node_modules` and run Playwright. The default `mol-do-work` formula
  works in the shared checkout. `mol-polecat-commit` gives each worker its
  own worktree, which would let independent tasks run in parallel; the
  mayor decides per wave.
- **Order:** A1 (claude) and B1 (codex) start at once. The mayor slings each
  later task when the one before it closes, alternating providers.
- **Commits:** each worker commits only the files it names, with explicit
  `git add <path>`, on `b9s-alpha` `main`, with `Refs: <task id>`. The daily
  sync rebases those commits onto b9s `main`.
- **Production stays untouched:** no task deploys to beads.osen.co or
  changes its image, the Hetzner servers, the shared Dolt server or the
  operator's working setup. beads.osen.co is read-only, and never through
  the operator's browser sessions or sign-ins (operator, 2026-10-11).
- **Release:** none in this epic. The operator decides when b9s takes the
  work from b9s-alpha.

## Open Questions

- beads.osen.co access for A1 behind the login proxy.
