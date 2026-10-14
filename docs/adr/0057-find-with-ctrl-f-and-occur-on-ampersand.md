---
type: ADR
id: "0057"
title: "Switch the search between filter and find with Ctrl-F, and put occur on &"
status: active
date: 2026-10-14
---

## Context

`/` writes the shared query (ADR 0001). The tree keeps each hit's branch and
hides every branch without a hit, and the board and the list keep only the
issues that match. Occur, on `Ctrl-O`, narrows that further to the hit rows
alone. b9s had no way to look for text and still see the rest of the tree.

k9s, which b9s takes its keys from, has both behaviours behind `/`, chosen by
the kind of view. In a resource table, `/` is "Filter Mode" and rows without a
match disappear (`internal/view/table.go`). In a YAML, describe or log view,
`/` highlights the matches in orange, `n` and `N` step through them, and no
line is hidden (`internal/view/details.go`). The b9s tree is a table, so `/`
filters, as in k9s.

`Ctrl-O` gave occur no mnemonic, and `less` already has a key for it: `&pattern`
shows only the matching lines.

## Decision

**`/` keeps filtering. `Ctrl-F` switches the shared query between filter and
find, and occur moves from `Ctrl-O` to `&`.**

In find mode the query hides no issue in the tree, the list or the board. The
tree underlines hit titles in orange, `n` and `N` step through the hits and
unfold the branch around each one, and the query bar reads `find /`. The mode
belongs to the query state, not to its text, so it lasts through `Esc` and
later queries until `Ctrl-F` is pressed again. Status and type keys write into
the same query, so in find mode they mark issues as well, and filter nothing.

`Ctrl-F` stops paging the tree and the board. `PgDn`, `Right` (tree) and
`Ctrl-D` (board) still page down, and `Ctrl-B` still pages up.

## Options considered

- **`Ctrl-F` toggle, `/` filters by default** (chosen): keeps k9s's table
  behaviour for `/` and adds k9s's text-view behaviour on one key. Costs k9s's
  `Ctrl-F` page down.
- **`/` finds by default, `&` is the only filter**: the simplest key map, but
  `/` would no longer behave as it does in a k9s table, which is the
  interaction b9s copies.
- **A second search key for find**: keeps `Ctrl-F` for paging, but gives two
  query fields and two sets of `n`/`N` matches to keep apart.
- **Occur on `*`**: free, but Vim users read `*` as "search the word under the
  cursor".

## Consequences

- A user can look for text and keep the surrounding tree in view.
- A k9s user who pages with `Ctrl-F` finds it switching the search mode. The
  query bar and the status line name the mode, so the switch is visible.
- A user `hotkeys.yaml` binding on `Ctrl-O` no longer needs `override: true`,
  and one on `&` now does.
- Re-evaluate if the web board gets a find mode: the browser owns `Ctrl-F`
  there.
