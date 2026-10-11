---
type: ADR
id: "0052"
title: "Use the calm terminal direction for responsive native web"
status: active
date: 2026-10-11
---

## Context

b9s web started as a phone UI served by `b9s web` (ADR 0019), with one
`WIDE` media query that turns a phone detail sheet into a side panel. That
leaves tablets between modes, keeps the phone bottom bar on desktop, and uses
the louder Dracula-derived palette from the original web implementation.

The responsive web plan approved for `b9m-vdp` requires a UI that works from
360 px phones to 2560 px monitors, feels native on each class of device, and
keeps the b9s identity with less colour. The operator approved direction A,
"calm terminal", from the responsive mockups on 2026-10-11 in decision bead
`b9m-vdp.2.2`.

References:

- [docs/plans/2026-10-11-responsive-web/requirements.md](../plans/2026-10-11-responsive-web/requirements.md)
- [docs/plans/2026-10-11-responsive-web/implementation-plan.md](../plans/2026-10-11-responsive-web/implementation-plan.md)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/index.html](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/index.html)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-tree.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-tree.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-detail.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-detail.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-board.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-board.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-search.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-search.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-edit.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-phone-edit.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-tablet-portrait.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-tablet-portrait.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-tablet-landscape.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-tablet-landscape.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-desktop.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-desktop.png)
- [/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-wide.png](/Users/andrevanderheijden/gascity/rigs/b9s-alpha/docs/mockups/responsive-web/shots/a-wide.png)

## Decision

**The responsive web redesign uses direction A, "calm terminal": compact
command chrome, monospace rails, native web text where prose is read, and
colour only as small state marks.**

The web app replaces the current two-mode layout with four app size classes
computed from the app width:

| Class | Width | Mockup frame | Layout rule |
| --- | --- | --- | --- |
| `phone` | `< 768px` | 390 x 780 | Bottom navigation, touch sheets, back navigation, 44 px targets, and no horizontal scroll. |
| `tablet` | `768px - 1279px` | 820 x 1080 and 1180 x 820 | Touch-first split view with list and detail together where space allows. |
| `desktop` | `1280px - 2559px` | 1440 x 900 | Sidebar navigation, hover, context menus, keyboard focus, and board or tree beside detail. |
| `wide` | `>= 2560px` | 2560 x 960 | More panes and board columns, with readable line lengths for prose. |

The selected token set is direction A from
`docs/mockups/responsive-web/index.html`:

| Token | Value |
| --- | --- |
| `--surface-0` | `#070809` |
| `--surface-1` | `#0d1012` |
| `--surface-2` | `#111619` |
| `--surface-3` | `#171e22` |
| `--surface-raised` | `#12181b` |
| `--line` | `#293238` |
| `--line-soft` | `#1b2327` |
| `--text` | `#f3f4ef` |
| `--text-soft` | `#c4c8c2` |
| `--muted` | `#828a83` |
| `--ghost` | `#5f665f` |
| `--open` | `#7bd88f` |
| `--progress` | `#7cc7d8` |
| `--priority` | `#d2b25c` |
| `--danger` | `#dc7a75` |
| `--accent` | `#7cc7d8` |
| `--focus` | `rgba(124, 199, 216, 0.24)` |
| `--hover` | `rgba(124, 199, 216, 0.07)` |
| `--selected` | `rgba(124, 199, 216, 0.12)` |
| `--shadow` | `rgba(0, 0, 0, 0.60)` |

The typography tokens are:

| Token | Value |
| --- | --- |
| `--ui` | `system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif` |
| `--mono` | `ui-monospace, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace` |

The app uses monospace for IDs, code, command labels and navigation rails. It
uses the UI font for issue titles, body text, form labels, comments and prose.
Issue rows stay compact at roughly the mockup's 38 px row height.

The web token file owns these values. The web UI no longer takes its colour,
font, spacing or radius values from the theme source shared with `pkg/ui`.
`pkg/ui` keeps the terminal theme, while `web/src/tokens.css` becomes the
browser source of truth.

## Options considered

- **A calm terminal** (chosen): keeps the strongest b9s identity, removes the
  parade of accents, and still lets each device use native navigation,
  sheets, split views and context menus.
- **B workbench**: gave the most conventional web work surface, but its cards
  and segmented controls moved further from the operator's request to retain
  the original b9s feel.
- **C graphite**: reduced colour the most, but made b9s feel more generic and
  made the terminal identity too faint.

## Consequences

- The next implementation step creates web tokens first, then migrates layout
  and components to those tokens. Literal colours outside the token source are
  a regression.
- Size-class logic replaces `WIDE`. Every later Playwright screen check should
  cover at least one `phone`, `tablet`, `desktop` and `wide` viewport.
- Phones keep bottom navigation and sheets. Tablet, desktop and wide layouts
  must not keep phone chrome only because it already exists.
- Colour carries meaning only when paired with text, shape or placement:
  open, in progress, priority, blocked, danger, focus, hover and selection.
- The TUI and web UI now have separate presentation token sources, while they
  continue to share data rules, query semantics and `bd` writes through the
  architecture decided in ADR 0019.
