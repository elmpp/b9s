---
type: ADR
id: "0054"
title: "Scroll only the cursor's cell in the web epic rail"
status: active
date: 2026-10-12
---

## Context

ADR 0053 gave every lane of the web epic rail three heights, and let a cell
with more cards than its lane's height scroll inside the lane. It named the
cost, a scroll area inside a scroll area where the wheel over a cell scrolls
the cell first, and it named a reason to look again: a scroll inside the
board that makes the board hard to scroll with a trackpad.

On 2026-10-12 the operator used the wide board in "status · lanes rail" with
every lane Compact. Every cell with more cards than its lane drew a scroll
bar, and the wheel stopped in each such cell on the way down the epics. The
operator asked for no scroll bars in the lanes, except in the box of the
issue that has the focus, and asked to keep the fades over the hidden cards.

## Decision

**A cell of the epic rail scrolls only while it holds the cursor's card.
Every other cell clips the cards past its lane's height behind the edge
fades, draws no scroll bar, and leaves the wheel to the board.**

- `railCell` marks the box of the cell that holds the cursor's card (`cur`),
  and only that box has `overflow-y: auto`. Every other box has
  `overflow: hidden`.
- When the cursor leaves a cell, the cell stops scrolling and keeps its scroll
  position through the redraw.
- A click on a card puts the cursor there, so its cell scrolls. The cursor
  keys and the page keys move through every card of the cell and scroll it to
  keep the card in view, as before.
- Every box keeps its fades, the cursor's box included. No new colour.
- Full lanes, epic rows (`v`) and the phone board have no box, so they do not
  change.

This replaces the scroll rule of ADR 0053. Its three heights, its keys and its
fades stay in force.

## Options considered

- **Scroll only the cursor's cell** (chosen): the wheel moves through the
  epics and never stops in a cell, the one scroll bar marks the cell the keys
  work in, and the cards past an edge stay in reach of the keys, and of the
  wheel once the cursor is in their cell.
- **Every cell scrolls, with its scroll bar hidden**: no bars, but the wheel
  still stops in each cell, which is the larger half of the problem.
- **Keep the rule of ADR 0053**: the problem the operator reported.

## Consequences

- To bring the hidden cards of a cell into view with the wheel, a person first
  clicks a card in it or moves the cursor there with the keys.
- Where scroll bars always show (a Mac set to show them, Windows, Linux), the
  cursor's cell draws its bar inside the cell, so its cards are a scroll bar
  narrower than the cards beside them until the cursor leaves.
- A cell the cursor left can show cards from the middle of its list, with a
  fade at both edges.
- A box with hidden overflow does not scroll by touch either, so on a tablet a
  finger on a cell without the cursor pans the board.
- `wide.spec.ts` checks the computed overflow of the boxes, that the wheel
  over a cell without the cursor scrolls `#wboard`, that the wheel over the
  cursor's cell scrolls that cell, and that a cell keeps its place when the
  cursor leaves it.

## Advice

The operator asked for this on 2026-10-12 at 09:07, on the wide board with
Compact lanes: "we shouldn't have scroll bars at all, epics. Only if we
highlight an issue, only if the focus becomes on an issue in an epic, that
issue, its surrounding box [...] should get the scroll bar. Otherwise, it
doesn't need to scroll by yet. We do want to show in each box the fading
overflow hidden." The full words are in `b9m-vdp.4.26`.
