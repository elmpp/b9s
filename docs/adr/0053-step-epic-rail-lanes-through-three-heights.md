---
type: ADR
id: "0053"
title: "Step the web epic rail's lanes through Compact, Open and Full heights"
status: active
date: 2026-10-12
---

## Context

On the web board for a laptop, desktop or tablet (ADR 0052), the epic rail
drew each lane as tall as its tallest cell. `Tab`, `Shift-Tab` or the arrow
in the epic cell folded a lane to the epic cell's height, and each of its
cells then showed only "<n> hidden".

On 2026-10-12 the operator used the board at about 2000 px wide and found
both states wasteful. A folded lane left the width of the board empty beside
its epic. An open lane with 7 open cards and about 18 blocked ones stretched
down the whole screen, so no other epic was in view. A first version of
`b9m-vdp.4.18` showed the cards that fit in a folded lane and "+n more" for
the rest. The operator replaced it the same morning with lane heights and
cells that scroll, and ruled out letting a cell's cards continue in a second
column beside the first.

## Decision

**Every lane of the web epic rail has three heights. Compact, where every
lane starts, is the epic cell's height and never shorter than three one-line
cards. Open is tall enough for every open and in progress card. Full is as
tall as the lane's tallest cell. A cell with more cards than its lane's
height scrolls inside the lane, with a fade at each edge that has cards past
it.**

- The epic cell's arrow (▸ ▿ ▾) and `Tab` step one lane Compact, Open, Full
  and back to Compact. `Shift-Tab` steps every lane on the board together.
  Lanes at different heights go back to Compact first.
- Grouped by priority the board has no open and in progress columns, so a
  lane steps between Compact and Full.
- A cell that must not set its lane's height holds its cards in a box that
  is positioned out of the flow inside the grid cell. The grid then takes
  the row's height from the epic cell (Compact) or from the open and in
  progress cells (Open).
- A redraw keeps each cell's scroll. The cursor keys scroll a cell to keep
  the selected card in view and clear of the fade.
- The fade is a mask over the cell's cards, so it adds no colour.
- The height is view state for the session (`S.board.laneHeight`) and is not
  saved. Epic rows (`v`) and the phone board keep their fold to the lane's
  header. The TUI's board keeps its fold.

## Options considered

- **Three heights, with cells that scroll** (chosen): the board starts
  short, a long column no longer pushes the other epics away, and every
  card stays reachable with the wheel and the keys. The cost is a scroll
  area inside a scroll area: the wheel over a cell scrolls the cell first.
- **Fold to the epic's height, show the cards that fit and "+n more"**: no
  scroll inside the board, but the hidden cards are out of reach of the
  keys, and the count changes with every resize and every selection.
- **Let a cell's cards continue in more columns** to fill the width:
  declined by the operator, because cards of one status in two columns read
  as two statuses.
- **One fixed height for every lane**: a height that suits an epic with
  three cards cuts one with twenty short, and the reverse wastes the board.
- **Keep the open and folded states**: the two problems the operator
  reported.

## Consequences

- The board starts with every lane Compact, so far more epics fit on a
  screen. A cell with many cards shows its first ones and a fade.
- The wheel over a cell scrolls that cell until its end. The board scrolls
  from the epic column and from the space between the cells.
- `h` and `l` prefer a card in the cursor's own lane, because a scrolled
  cell holds cards level with the lanes above and below it.
- A lane that goes to Full drops its scroll boxes, and their scroll with
  them.
- The scroll boxes cost WebKit about 6% when it switches to a board of 1000
  issues (99 to 104 ms against 91 to 98 ms on an iPad Mini emulation), and
  Chromium nothing measurable. `b9m-vdp.4.22` follows the WebKit time.
- Re-evaluate if the scroll inside the board makes it hard to scroll with a
  trackpad, if people want the heights to survive a reload, or if the TUI's
  board should get the same heights.

## Advice

The operator asked for this on the wide board in "status · lanes rail" on
2026-10-12: at 06:11 for using the space beside a folded lane, at 06:16 for
a fixed lane height with a three-part toggle and cells that scroll, and at
06:17 against cards that continue in the next column. The words are in
`b9m-vdp.4.18`.
