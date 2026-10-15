---
plan_slug: 2026-10-11-responsive-web
phase: requirements
rig: b9s-alpha
rig_root: ~/gascity/rigs/b9s-alpha
artifact_root: docs/plans
epic: b9m-vdp
status: approved
created_at: 2026-10-11T08:12:54Z
updated_at: 2026-10-11T08:18:14Z
---

# Requirements: Responsive, native-feeling b9s web for every screen size

## Problem Statement

b9s web was built phone-first (epic `b9m-foit`, all 39 tasks closed) as the
TUI carried into a browser. Three things now hold it back.

1. **Two fixed layouts.** One media query (`web/src/util.ts` `WIDE`, 720 x 500
   px) switches between the phone tree with a bottom sheet and the wide board
   with a side detail panel. Tablets fall into one mode or the other, and the
   desktop layout keeps phone chrome: the bottom tab bar stretches across a
   1440 px screen, and tree and board are separate tabs.
2. **A TUI in a browser, and loud.** Monospace everywhere, terminal key hints
   in the chrome, and the Dracula palette with nine saturated accents (purple,
   cyan, green, red, orange, yellow and three blues, 47 literal colours in
   `web/src/app.css`). The operator calls the result "parade / carnival".
3. **Speed is unmeasured where it matters.** Emulated rendering is fast
   (`b9m-foit.16`: 1000 issues load in 263 to 519 ms, a full re-render takes
   11 to 77 ms), but nobody has measured the real deployments: first load,
   project switch, writes through `bd`, and live updates on the hosted board and
   a local `b9s web`. The operator wants it to feel more responsive.

## Solution

Redesign b9s web so that it:

- works at every size from a 360 px phone to a 2560 px and wider monitor,
  with layouts that adapt to the space instead of two fixed modes;
- feels native on each device: touch-first patterns on phones and tablets,
  and mouse, hover, context menus and keyboard on laptops and desktops;
- keeps the b9s identity (dark, dense, calm, the same information and
  functions) with a new, cleaner web look and a restrained palette in which
  colour only marks meaning;
- is measured first and then made faster where the numbers show it is slow,
  with budgets that the Playwright suite enforces.

The design goes through clickable mockups that the operator approves before
any UI implementation, as `b9m-foit.1` did.

## User Stories

### 1. Phone user

As a phone user, I use b9s web one-handed with navigation that feels like a
phone app, not a terminal.

- Works from 360 to 440 px wide in portrait, and in landscape, without
  horizontal scroll.
- Navigation, sheets and back behave the way iOS and Android users expect.
- Touch targets are at least 44 px, and the outer 24 px edge-gesture rule of
  `b9m-foit` still holds.
- Every function in the parity list (Other Notes) stays reachable.

### 2. Tablet user

As a tablet user, portrait or landscape, I get a layout made for a tablet,
not a stretched phone or a squeezed desktop.

- From about 768 to 1366 px, the list and an issue's detail can be on screen
  together.
- Touch works everywhere, and a hardware keyboard works when one is attached.
- Rotating the tablet keeps the selection, scroll position and open detail.

### 3. Laptop and desktop user

As a laptop or desktop user (1280 to 1920 px), I use b9s web like a native
web app.

- No phone chrome on desktop: no bottom tab bar, no swipe-only actions.
- Mouse: hover states, a context menu on rows and cards, and drag and drop on
  the board.
- Keyboard: the TUI keybindings keep working (`b9m-foit.24`), and focus is
  always visible.
- Tree or board and an issue's detail can be seen together without the detail
  covering the list.

### 4. Wide-monitor user

As a user on a 2560 px or wider monitor, the extra room shows more work, not
more empty space.

- Panes and board columns grow to use the width, and text lines stay at a
  readable length.
- Nothing stretches to the full width where that hurts reading, such as the
  issue description.

### 5. A calm, clean look that is still b9s

As any user, the UI looks calm and clean and still reads as b9s.

- Neutral surfaces and text carry the screen. Colour is kept for meaning
  (status, priority, blocked, errors) and is never the only carrier of it.
- At most a small, fixed set of accent colours, defined once as design tokens,
  with no literal colours left in the stylesheet.
- Text meets WCAG 2.2 AA contrast.
- Information density stays close to today's: a phone shows at least as many
  issues per screen as the current two-line rows.

### 6. Measure before we optimise

As the operator, I know where b9s web is slow before anyone changes code for
speed.

- Measured on the hosted board and on a local `b9s web`, on a phone and a
  desktop, with at least 1000 issues: first load, project switch, tap or click
  to visible feedback, write round trip through `bd`, live-update latency, and
  scroll and gesture smoothness.
- The numbers and the method are recorded in the epic, so the run can be
  repeated.
- Speed budgets for story 7 are set from these numbers.

### 7. It feels fast

As any user, b9s web responds at once.

- Every tap or click shows feedback within 100 ms, optimistic where the write
  path allows it.
- First load, project switch and write round trip meet the budgets set in
  story 6.
- Playwright perf tests enforce the budgets on every run, and today's budgets
  in `web/tests/perf.spec.ts` do not regress.

### 8. Approve the design first

As the operator, I choose the design before implementation starts.

- Clickable HTML mockups in `docs/mockups/` show the phone, tablet, desktop
  and wide layouts with the new look, in the dark theme.
- The operator picks a direction, and an ADR records the choice and why.

## Out Of Scope

- New product features beyond today's function set, apart from what native
  patterns need (for example a context menu on desktop).
- The terminal UI (`pkg/ui`) and its theme.
- A light theme.
- Native iOS or Android apps. b9s web stays a PWA served by the same binary.
- Changes to the data model or the write path: every write still goes through
  `bd`.

## Other Notes

- **Function parity list** (from the `b9m-foit` design): tree with fold,
  expand all and collapse all; detail with Markdown, Mermaid, children
  progress, relations and comments; the query language with predicates, fuzzy
  words and completion; status chips; branch and subtree focus; follow mode;
  sort and columns; marks and bulk close, status change and delete; edit,
  create, status set, close, delete and comment; board by status, priority or
  type with epic lanes and column fold; dependency drill-down; projects and the
  all-projects view; source health; Memory Beads views; live updates.
- **Repo constraints:** one binary with assets embedded through `go:embed`
  and built with esbuild, no Node at runtime; the JSON contract is generated
  from Go types (`web/src/api.gen.ts`); the Playwright suite in `web/tests`
  with device profiles runs in CI (`b9m-foit.19`); ADR 0019 serves the web UI
  from `b9s web`; the hosted board runs the image behind a login proxy
  (`b9m-foit.22`); the Dolt password never reaches the browser (ADR 0013).
- **Operator's direction for the mockups:** "more phone and web native, less
  a TUI turned into a web app", "retain the original look and feel", "less
  colourful and parade / carnival looking", "cleaner, better". Native web
  typography for text, with monospace kept where it means something (IDs,
  code, commands), is a candidate for the mockups to test.
- **Theme tokens (operator, 2026-10-11):** the web tokens leave the source
  they share with `pkg/ui` today. The TUI keeps its own theme.
- **Workers:** the work is split evenly between `b9s-alpha/codex` and
  `b9s-alpha/claude` workers (operator, 2026-10-11).
- **Approved** by the operator on 2026-10-11.
