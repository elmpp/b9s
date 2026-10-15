# Show bd state dimensions instead of the lane state

Status: design, option not chosen yet. Beads: `bd-nwxc`.

## Why

b9s shows a `Lane state` column in the tree and a `lane: <stage>` tag on board
cards. Both read the `lane-stage=` label that an external task runner
writes (`bd-butn.3`). That label exists only in projects that use such a
runner, so it is too specific for a general Beads viewer.

A general need sits in the same place: after a task closes, it still has to be
deployed to acceptance and to production, and nothing shows where it is
deployed. `bd` already has a generic mechanism for such facts, which b9s
should show instead.

## What `bd` provides

`bd set-state <id> <dimension>=<value> --reason "..."` does three things:

1. It writes an event bead that records the change (the audit trail).
2. It removes the issue's existing label for that dimension.
3. It adds the label `<dimension>:<value>`.

`bd state <id> <dimension>` reads one value, and `bd state list <id>` lists
them all. A deploy pipeline then reads:

```bash
bd close bd-x1 --reason="merged"
bd set-state bd-x1 deploy=acc --reason="acc build 4f2c"
bd set-state bd-x1 deploy=prd --reason="prd release v0.9.0"
bd list --label deploy:acc     # closed, not yet on production
```

Facts that shape the design:

- **A state is any label with a `:`.** `bd state list` filters labels on that
  convention, so an existing label such as `area:admin` already counts as a
  state with the dimension `area`.
- **Values have no length limit and no check.** In practice they are one word
  (`acc`, `prd`, `healthy`), but b9s must truncate them.
- **One value per dimension.** That fits a linear pipeline (acc, then prd,
  because prd implies acc). Independent environments need one dimension each
  (`acc=deployed`, `prd=deployed`).
- **Status stays untouched.** Custom statuses (`status.custom`) would keep a
  deployed task out of `closed` and break `bd ready`, epic progress and
  blocking. A state dimension does not.
- For versioned releases, the state can sit on the release milestone of
  ADR 0026 instead of on each task.

## Options

### A. One "State" column with every dimension

```
  ID       TITLE                                  STATUS   STATE
  bz6.3    Installatie en ingebruikname           closed   deploy:prd area:a…
  bz6.2    Aanschaf                               closed   deploy:acc
  bz6.1    Plan en systeemkeuze vastleggen        open     area:admin
```

Simple. But a label such as `area:admin` fills the column on every row, and
the deploy stage is lost in it.

### B. Chosen dimensions, one column each (recommended)

The user names the dimensions in a `bd` config key, read the way
`b9s.identities` is read:

```bash
bd config set b9s.state.columns deploy
```

Tree:

```
  ID       TITLE                                  STATUS   DEPLOY
  bz6.3    Installatie en ingebruikname           closed   prd
  bz6.2    Aanschaf                               closed   acc
  bz6.1    Plan en systeemkeuze vastleggen        open
```

The dimension is the header, so the cell holds only the value. A value longer
than the column ends in `…`.

Board card, in the place and style of the `lane:` tag:

```
  ● bz6.3 Installatie en ingebruikname           deploy: prd · P1
  ● bz6.2 Aanschaf                               deploy: acc · P1
```

Detail panel, every state in full, including dimensions without a column:

```
  State     deploy  prd        area  admin
```

### C. A column for every dimension b9s finds

No setup. But `area`, `deploy` and every later dimension compete for width,
so the tree grows wide and hard to predict.

## Removal (all options)

- Remove the `Lane state` tree column (`TreeColumnLaneStage`), the `lane:`
  card tag in the TUI (`pkg/ui/board_lanes.go`) and in the web
  (`web/src/render.ts`), and `dispatcherLaneStage` (`pkg/ui/tree.go`).
- Remove or rewrite the tests that assert the `lane:` tag
  (`pkg/ui/board_card_line_test.go`, `pkg/ui/board_layout_test.go`).
- Update the README and `docs/USER_GUIDE.md`, which describe the `lane` tag.
- Write an ADR that supersedes the lane tag part of ADR 0016 and records the
  chosen option.

## Open questions

1. Option A, B or C.
2. With option B, the default when `b9s.state.columns` is unset: no column, or
   `deploy` when any issue has that dimension.
3. Whether the detail panel shows the state history from the event beads, or
   only the current values.
