---
type: ADR
id: "0056"
title: "Limit the branch filter with an exact branch: predicate"
status: active
date: 2026-10-14
---

## Context

`f`, `:branch` and `b9s ctl branch` limit the tree to one branch by writing the
branch's issue ID into the shared query (ADR 0001). The tree keeps every match's
subtree and ancestors on screen. That worked while the branch was always a
top-level root, whose ID rarely occurs inside another ID.

`f` now steps down the parent path of the selected issue: the root epic, then a
child epic, then a feature. Beads gives children hierarchical IDs, so a level
such as `doc-25s.2` is a substring of `doc-25s.2.1`. Query matching is fuzzy for
every string field (ADR 0004), so the query matched `doc-25s.2.1` as well. In the
dell-gb10 project that issue had been moved under the sibling feature
`doc-25s.1`, which then stayed on screen as context with its whole subtree. The
same happens to `bd-x.1` against `bd-x.10` to `bd-x.19` in any epic with ten or
more children.

## Decision

**A new `branch:<id>` predicate matches the one issue whose ID equals `<id>`,
ignoring case. `f`, `:branch` and `ctl branch` write `branch:<id>` instead of the
bare ID.** The tree adds the issue's subtree and ancestors as it does for any
match, so the predicate limits the tree to exactly that branch.

## Options considered

- **`branch:` predicate** (chosen): exact where the caller means one issue,
  visible in the query bar, and typed by hand like any other predicate. Other
  fields keep the fuzzy matching of ADR 0004.
- **Make `id:` exact**: reverses ADR 0004 for a field people type in fragments,
  such as `id:7rt1`.
- **Treat a plain word that equals a loaded ID as exact**: typing `bd-x.1` on the
  way to `bd-x.12` would hide `bd-x.12` for one keystroke, and the rule would be
  invisible in the query bar.
- **Match descendants inside the predicate**: would make `branch:` mean the
  subtree in the list and the board too, but a predicate sees one issue and no
  parent map. Rejected for now.

## Consequences

The branch filter shows only the issues under the chosen level, whatever their
IDs are. The query bar reads `/ branch:<id>` rather than `/ <id>`. In the flat
list and the board, which add no context rows, `branch:<id>` shows the one issue;
the board's own `f` keeps its separate branch limit. Re-evaluate if the list or
the board should show a whole branch for a query: that needs a parent lookup in
query matching.
