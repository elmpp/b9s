# Keep b9s responsive behind the shared Dolt tunnel

Status: plan, not implemented. Epic `bd-mlet`, plan task `bd-mlet.4`.
Measurements taken on 2026-10-18 between 08:50 and 11:25 CEST against the shared
Dolt server, through the tunnel LaunchAgent `com.osenco.dolt-tunnel`.

## Contents

- [Summary](#summary)
- [What was measured](#what-was-measured)
  - [The tunnel, not Dolt, is the bottleneck](#the-tunnel-not-dolt-is-the-bottleneck)
  - [What saturates the tunnel](#what-saturates-the-tunnel)
  - [Round trips per operation](#round-trips-per-operation)
  - [Payload sizes](#payload-sizes)
  - [Rendering cost](#rendering-cost)
- [Root cause chain](#root-cause-chain)
- [Does bd serve solve it?](#does-bd-serve-solve-it)
- [Design](#design)
  - [1. Never block the event loop on a reload](#1-never-block-the-event-loop-on-a-reload)
  - [2. Load a lean snapshot, fetch text and comments on demand](#2-load-a-lean-snapshot-fetch-text-and-comments-on-demand)
  - [3. Reload incrementally](#3-reload-incrementally)
  - [4. Back off when idle](#4-back-off-when-idle)
  - [5. Render the detail pane only when it is visible, and cache it](#5-render-the-detail-pane-only-when-it-is-visible-and-cache-it)
  - [6. bd serve next to the database, as a later source](#6-bd-serve-next-to-the-database-as-a-later-source)
- [Assumptions: tested and open](#assumptions-tested-and-open)
- [Phases and tickets](#phases-and-tickets)
- [Success criteria](#success-criteria)
- [Evidence files](#evidence-files)

## Summary

b9s is slow, and `bd` is slow with it, for one reason: the single SSH session
that carries port 3306 is saturated, so every SQL round trip waits in its queue.
At the moment of measurement a round trip through the shared tunnel took 353 to
479 ms (p50), while a fresh SSH connection to the same server answered the same
query in 36 ms and the server itself in 2 to 4 ms.

The tunnel is saturated by b9s itself. 46 b9s processes are running on this
laptop (44 with a live connection), every one polls `DOLT_HASHOF_DB()` every
500 ms, and every one reloads its full snapshot (3.4 MB for `b9s`: all 1681
issues with description, design and notes, plus all 1054 comments) on every
database change, about twice a minute. The tunnel moves 2.5 MB/s in each
direction continuously and has carried 194 GB in 27 hours.

Three b9s defects then turn the slow tunnel into a frozen UI:

- The reload runs synchronously inside `Model.Update` (bd-mlet.3). A 19.5 s
  reload is 19.5 s without a keypress.
- The reload is far bigger than it needs to be. A lean snapshot of the same
  1681 issues without text is 233 KB, twelve times smaller.
- The detail pane renders every comment through glamour on every navigation,
  even when hidden (bd-mlet.1). 66 ms per keypress on a 126-comment issue.

`bd serve` does not fix this on the laptop. One `issues.get` over HTTP still
costs 25 SQL round trips (11 s through the saturated tunnel versus 20 s for
`bd show`). It does fix it when it runs next to the database: 3 s startup,
17 to 23 ms per `issues.get`, 100 to 130 ms through an SSH forward. But its
API has no comments read, no cross-database catalog, and the events journal is
off by default and does not see non-`bd` writes, so it is a later source, not
the fix for this epic.

The plan: make b9s cheap first (async reload, lean snapshot, incremental
reload, idle backoff, lazy rendering). That removes the load that saturates the
tunnel, which makes `bd` fast again as a side effect. Then evaluate server-side
`bd serve` as an optional source for writes and change feeds.

## What was measured

### The tunnel, not Dolt, is the bottleneck

`SELECT 1` every 200 ms, same 20 seconds, same server, same user:

| Path | n | p50 | p95 | max |
|---|---|---|---|---|
| Shared tunnel (`127.0.0.1:3306`, LaunchAgent) | 37 | 353 ms | 513 ms | 618 ms |
| Fresh `ssh -L` connection, opened for the test | 81 | 36 ms | 86 ms | 711 ms |
| On the server (`127.0.0.1:3306` locally) | 300 | 4 ms | 8 ms | 25 ms |

The network and the Dolt server are both fine. The one SSH session that all
150 forwarded connections share is the queue. Bulk transfers from any client
delay every small query from every other client behind them in the same TCP
stream.

### What saturates the tunnel

| Measure | Value |
|---|---|
| b9s processes running | 46, in 18 project directories (10 in `b9s` alone) |
| b9s processes with a 3306 connection | 44 |
| Established connections through the tunnel | 150 (72 from b9s, the rest from `bd`, tests and other tools) |
| Tunnel throughput, 10 s sample | 2.5 MB/s in, 2.5 MB/s out, every second |
| Tunnel bytes since the LaunchAgent started (27 h) | 194 GB in |
| Database commits on `b9s` | 28 092 (`dolt_log`), about 2 per minute while agents work |
| b9s full reload for `b9s` | 2.86 MB issues + 0.48 MB comments, 8.2 s + 2.6 s when the tunnel is busy |

Every commit on any database makes every b9s instance on that database reload
everything. 44 instances times 3.4 MB times 2 per minute is the 2.5 MB/s.

### Round trips per operation

Counted with a logging MySQL proxy between the client and the tunnel. Time is
round trips times the current tunnel latency, which is why the same command
takes 2 s on a quiet tunnel and 20 s on a busy one.

| Operation | SQL round trips | Measured through the shared tunnel |
|---|---|---|
| `bd show <id>` (CLI) | about 36, over 3 connections | 19.9 to 20.4 s |
| `bd list` (CLI) | about 29 | about 2 s when quiet |
| `bd serve` startup | 35 | 29 s (laptop) versus 3 s (server) |
| `bd serve` `GET /v0/beads/issues/{id}` | 25 | 9.4 to 12.8 s |
| `bd serve` `GET /v0/beads/issues?status=open` | 13 | 5.0 to 5.1 s |
| `bd serve` `GET /v0/beads/ready?limit=1` | 16 | 7.9 to 11.3 s |
| `bd serve` `GET /v0/beads/stats` | 4 | 1.6 to 1.9 s |
| b9s `DOLT_HASHOF_DB()` poll | 1 | 0.35 to 0.48 s, every 500 ms, per instance |
| b9s full reload | 4 (issues, labels, dependencies, comments) | 11 to 28 s |

`bd serve` reports the unit of work itself (`uow_ms`) at 430 to 860 ms per
request. The remaining 5 to 11 s per request is the per-request open and
transaction preamble, paid in round trips.

### Payload sizes

From the `b9s` database, 1681 issues (103 open, 1524 closed), 1054 comments:

| Query | Rows | Bytes |
|---|---|---|
| Issues, all columns (what b9s loads today) | 1681 | 2 862 000 |
| Issues, lean (id, title, status, priority, type, assignee, owner, dates, is_blocked) | 1682 | 233 054 |
| Issue text only (description, design, notes, acceptance) for all issues | 1682 | 2 362 222 |
| Issue text only for issues that are not closed | 156 | 128 322 |
| Comments, all (what b9s loads today) | 1054 | 406 331 |
| Comments on issues that are not closed | 268 | 93 642 |
| `bd serve` `issues?status=open` JSON | 103 | 158 451 |
| `bd serve` `issues?status=all` JSON | 1681 | 3 531 367 |

Change rate on `b9s`: 25 issues and 437 comments changed in the last 24 hours,
2 issues in the last hour. An incremental reload by `updated_at` is a few
kilobytes.

Comments are the user's hypothesis and they do matter, but for rendering, not
for transfer: comments are 12% of the reload. The closed issues' text is 70%.

### Rendering cost

From the bd-6a1l investigation (`.b9s/latency-20261017/b9s-stages.txt`):
navigating onto bd-md8p (126 comments) costs 66.8 ms per keypress with the
comments rendered, 2.6 ms with the comments stripped. The render happens in
`syncTreeToDetail` through `updateViewportContent` on every cursor move, with
the detail pane hidden too.

## Root cause chain

```
  44 b9s instances x poll every 500 ms x full 3.4 MB reload per commit (2/min)
                     │
                     ▼
     one SSH session carries all of it: 2.5 MB/s, 194 GB/day
                     │
                     ▼
     every SQL round trip queues behind bulk transfers: p50 36 ms -> 350-480 ms
            │                                  │
            ▼                                  ▼
  bd show = 36 round trips = 20 s     b9s reload = 3.4 MB = 11-28 s
                                               │
                                               ▼
                             reload runs inside Model.Update: UI frozen (mlet.3)
                                               │
                                               ▼
                   plus 40-70 ms per keypress rendering hidden comments (mlet.1)
```

Fix the top of the chain and everything under it shrinks. Fix only the bottom
(async reload) and the UI stays usable but shows stale data for 20 s and `bd`
stays slow.

## Does bd serve solve it?

GitHub b9s#11 proposes `bd events` (an ordered change journal) and `bd serve`
(HTTP on loopback) so b9s stops polling and diffing snapshots. Tested against
beads 1.3.0-rc.1:

What it solves:

- **Next to the database it is fast.** On the Hetzner server, `bd serve`
  starts in 3 s and answers `issues.get` in 17 to 23 ms, `issues?status=open`
  (158 KB) in 43 ms. Through a dedicated SSH forward from the laptop that is
  100 to 130 ms and 190 to 220 ms: one HTTP round trip instead of 25 SQL ones.
- **Writes without forking `bd`.** `issues.update`, `close`, `addComment`,
  `claim` exist. b9s today forks `bd` for every write, and each fork pays the
  35-query preamble (7 to 25 s on a busy tunnel).
- **A stable contract.** b9s would stop depending on Dolt table shapes that
  move with the schema (the `due_at`/`due_date` fallback in `dolt.go` is one
  scar of that already).
- **Wisps and Gas City rigs** (the elmpp comment on #11) are reachable through
  it and not through b9s's SQL reader.

What it does not solve:

- **On the laptop it is the same queue.** 25 round trips per `issues.get`
  through the shared tunnel is 9 to 13 s. Half of `bd show`, still unusable.
- **No comments read.** `GET /v0/beads/issues/{id}` returns
  `comments_omitted: true` and `GET .../comments` is 404. Only `addComment`
  exists. b9s would keep a SQL path for comments.
- **One workspace per server.** `bd serve` serves one database. b9s's
  `:project` catalog (ADR 0007, 0015) lists every database the startup user can
  read; that would need one `bd serve` per database plus a catalog b9s builds
  itself.
- **The journal is off, partial, and per clone.** `events-journal` is a
  per-workspace opt-in written to `.beads/config.yaml`; writes through the
  beads Go library, `bd dolt pull` and Dolt merges are not journaled. The
  `DOLT_HASHOF_DB()` poll plus a reload stays as the backstop, as agreed on
  #11.
- **It refuses `--readonly`.** A read-only consumer cannot start one, so the
  server-side process needs the write-capable project user and a token file.
- **It is a deployment.** A supervised process per database on the Hetzner
  host, a token file, and a forward per database in the LaunchAgent. That is
  `osenco-infra` work, not b9s work.

Verdict: `bd serve` next to the database is the right long-term transport for
writes and for a change feed, and worth a spike after this epic. It is not the
fix for the slowness, because the slowness is self-inflicted load, and a b9s
that reloads 3.4 MB through HTTP is as heavy as one that does it through SQL.

## Design

Six changes, ordered by payoff per line of code. The first five are b9s-only
and land under `bd-mlet`. The sixth is a spike that ends in an ADR.

### 1. Never block the event loop on a reload

Owner: bd-mlet.3 (exists).

`FileChangedMsg` today calls `datasource.LoadFromSource` inside `Update`. The
experimental `BackgroundWorker` (`--background-mode`) is JSONL-only
(`loader.LoadIssuesFromFile`) and does not help the Dolt path.

- Turn the reload into a `tea.Cmd` that runs the loads off the event loop and
  returns a `SnapshotLoadedMsg{generation, issues, comments, err}`.
- One reload in flight at a time. A change notification during a reload sets
  a `dirty` flag; when the reload returns, if `dirty`, start one more. Rapid
  commits coalesce into two reloads at most.
- A generation counter rejects a reply from an older generation (a project
  switch, ADR 0012, already has this shape).
- The current snapshot stays usable while the next one loads. The header shows
  a small `reloading` indicator and the age of the snapshot.
- Bound the load with a context timeout (60 s) and surface the timeout in the
  status line instead of hanging.

Test: a fake source whose `LoadFromSource` blocks on a channel; send keys
while it blocks; assert the cursor moved and the tree rendered.

### 2. Load a lean snapshot, fetch text and comments on demand

New task under bd-mlet.

Split `LoadIssuesFiltered` into two reads:

- **Lean columns for every non-tombstone issue**: id, title, status, priority,
  type, assignee, owner, created/updated/closed, due, defer, is_blocked,
  external_ref, plus labels and dependencies. 233 KB for `b9s`. This is what
  the tree, the board, filters and sorting need.
- **Text columns on demand**: description, design, notes, acceptance for one
  issue, loaded when the detail pane shows it, then cached on the issue. The
  initial load also prefetches text for issues that are not closed (128 KB),
  so the common case shows no gap.
- **Comments on demand**, the same way: prefetch for issues that are not
  closed (94 KB), lazy for the rest.

Search is the one feature that reads text for all issues (ADR 0002, 0004:
plain fuzzy search is limited to primary fields, so it is unaffected; the
`desc:` and full-text forms need the text). Those forms trigger a one-time
text load for the closed set, with a progress line, and cache it.

Per-reload payload drops from 3.4 MB to about 450 KB (lean + open text + open
comments). With 44 instances that alone takes the tunnel from 2.5 MB/s to
about 0.35 MB/s.

Test: a snapshot of `b9s` on the scratch Dolt server
(`B9S_TEST_DOLT_SCRATCH_ADDR`), assert the lean query selects no text column,
assert the detail pane triggers exactly one text load per issue.

### 3. Reload incrementally

New task under bd-mlet, after 2.

A change notification does not mean "reload everything". After the first
full lean load, a change reloads:

- `issues WHERE updated_at > :last_seen` (lean columns), merged by id,
- `comments WHERE created_at > :last_seen`, appended,
- labels and dependencies for the changed ids only.

`updated_at` is not bumped by label changes (upstream gastownhall/beads#5442)
and deletes leave a tombstone status, so the incremental path cannot be the
only path. Every N incremental reloads (or every 5 minutes) a full lean reload
(233 KB) reconciles: removed ids, label-only changes, clock skew.

`last_seen` comes from the server clock (`SELECT UTC_TIMESTAMP()` in the same
round trip), never from the laptop.

For `b9s` the incremental read is 2 issues and a handful of comments per hour.
The per-change payload drops from 450 KB to a few kilobytes, and the hash
poll (1 round trip, 500 ms) becomes the only steady cost.

Test: scratch server; insert and update rows between two reloads; assert the
second reload issues `WHERE updated_at >` and the merged snapshot matches a
full reload.

### 4. Back off when idle

New task under bd-mlet.

44 instances are open because terminals stay open. An instance nobody has
touched should cost nearly nothing:

- Poll every 500 ms while the user is active (a keypress in the last 2
  minutes) and the terminal has focus (Bubble Tea `FocusMsg`/`BlurMsg`).
- Poll every 5 s after 2 minutes idle, every 30 s after 15 minutes.
- On the first keypress after idle, poll immediately and reload if changed, so
  the user never sees stale data for longer than one round trip.
- Keep `refresh.poll_interval` as the active-state interval. Add
  `refresh.idle_poll_interval` with the schedule above as the default.

With 3 and 4 together, an idle instance costs one 100-byte round trip every
30 s instead of 3.4 MB per commit.

Test: a fake clock; assert the interval schedule and the immediate poll on
resume.

### 5. Render the detail pane only when it is visible, and cache it

Owner: bd-mlet.1 (exists).

- `syncTreeToDetail` skips `updateViewportContent` when `showDetails` is off
  or the pane is hidden by width (`detailHiddenByNarrow`). Showing the pane
  renders then.
- Cache each rendered comment by (comment id, width, theme, oldest-first).
  Comments are immutable in beads, so the cache never invalidates on content,
  only on width and theme.
- Render the issue body and the comment list as two cached parts, so a new
  comment re-renders one comment, not 126.

Target: under 5 ms per keypress on bd-md8p with the pane visible, under 1 ms
with it hidden. The bd-6a1l harness (`.b9s/latency-20261017/main.go`) already
measures this.

### 6. bd serve next to the database, as a later source

New spike task under bd-mlet, after 1 to 5, ending in an ADR.

Prototype on the Hetzner host, one `bd serve` for `b9s` on loopback with a
token file, supervised, forwarded by the LaunchAgent on its own port. b9s
gains a `SourceTypeBeadsHTTP` that:

- reads the lean list through `issues?status=...` and `issues/{id}`,
- writes through `issues/{id}` (PATCH), `:close`, `:claim`, `/comments`
  instead of forking `bd` (one HTTP round trip, 100 ms, instead of 35 SQL
  round trips),
- keeps SQL for comments reads and the project catalog until upstream adds
  them,
- uses `events:watch` (SSE) when the journal is on, and the `DOLT_HASHOF_DB()`
  poll as backstop.

Upstream asks to file before the spike (gastownhall/beads): a comments read
endpoint, a `--readonly` serve, and a list of which operations the journal
sees. Upstream already tracks the round-trip preamble (#6114, 35 to 40 round
trips per invocation, with a client-side bootstrap cache proposal) and running
`bd` next to the database (#7517); b9s should reference those rather than
duplicate them.

Do not start this before 1 to 5 land. It changes the transport; it does not
change how much b9s asks for.

## Assumptions: tested and open

| Assumption | Status | Evidence |
|---|---|---|
| The Dolt server is slow | Rejected | 4 ms p50 on the server for `SELECT 1`; 85 to 300 ms for the full issues select |
| The network to Hetzner is slow | Rejected | 36 ms p50 on a fresh SSH connection at the same moment the shared tunnel showed 353 ms |
| The shared SSH session is the queue | Confirmed | 2.5 MB/s continuous, 194 GB in 27 h, 150 forwarded connections, 353 to 479 ms p50 |
| b9s causes the load | Confirmed | 44 connected instances, full 3.4 MB reload per commit, 2 commits/min |
| Comment-heavy issues are the cause | Partly | Comments are 12% of the reload. They dominate render cost (66.8 vs 2.6 ms per keypress), not transfer |
| `bd` is slow for the same reason | Confirmed | `bd show` 36 round trips; 20 s at 480 ms, about 2 s when the tunnel is quiet |
| `bd serve` on the laptop fixes latency | Rejected | 25 round trips per `issues.get`, 9 to 13 s through the shared tunnel |
| `bd serve` next to the database fixes latency | Confirmed | 3 s startup, 17 to 23 ms `issues.get`, 100 to 130 ms over a dedicated forward |
| `bd serve` can serve comments | Rejected | `comments_omitted: true`, `GET .../comments` 404 |
| A lean snapshot is much smaller | Confirmed | 233 KB vs 2 862 KB |
| Incremental reloads are small | Confirmed | 25 issues and 437 comments per day, 2 issues per hour |
| The events journal works for `b9s` | Open | Off by default (`409 events_journal_disabled`); turning it on writes `.beads/config.yaml` and was not tested |
| One SSH connection per b9s would scale | Open | One fresh connection scaled; 44 were not tried, and sshd on the host would see 44 sessions |
| Idle backoff removes most of the load | Open, by arithmetic | Depends on how many of the 44 instances are idle; the per-project count (10 in `b9s`) says most |

## Phases and tickets

| Phase | Ticket | Scope | Depends on |
|---|---|---|---|
| 1 | bd-mlet.3 | Async, coalesced reload with a generation counter | |
| 1 | bd-mlet.1 | Detail pane: render only when visible, cache per comment | |
| 2 | bd-mlet.5 | Lean snapshot; text and comments on demand | |
| 3 | bd-mlet.6 | Incremental reload by `updated_at` with periodic reconciliation | bd-mlet.5 |
| 3 | bd-mlet.7 | Idle backoff on the hash poll, immediate poll on resume | |
| 4 | bd-mlet.8 | Spike: `bd serve` next to the database as a source, ending in an ADR | bd-mlet.5, bd-mlet.6 |

Phase 1 makes the UI usable today whatever the tunnel does. Phase 2 and 3
remove the load. Phase 4 is the transport change from b9s#11.

Each phase is one squashed commit on `main` with its tests, per the landing
rules in `AGENTS.md`. Phases 2 and 3 get one ADR between them: "Load a lean
snapshot and reload incrementally", superseding nothing (ADR 0025 covers the
embedded store and stays).

An operational step that needs no code: close the b9s instances that are not
in use. Ten instances in one project directory are ten reloads per commit.
The idle backoff in phase 3 makes this unnecessary, but it costs nothing today.

## Success criteria

Measured with the bd-6a1l harness and the probes in `.b9s/latency-20261018/`:

| Criterion | Today | Target |
|---|---|---|
| Keypress to render, bd-md8p, pane visible | 66.8 ms | under 5 ms |
| Keypress to render during a reload | blocked for the reload (19.5 s seen) | under 5 ms |
| Bytes transferred per database change, one active instance | 3.4 MB | under 20 KB (incremental), under 450 KB (reconcile) |
| Bytes per hour, one idle instance | about 400 MB | under 50 KB |
| Shared tunnel `SELECT 1` p50 with all instances open | 353 to 479 ms | under 60 ms |
| `bd show` through the tunnel with all instances open | 20 s | under 3 s |

The last two are not b9s code, they are the consequence of the others, and
they are the user-visible outcome that matters.

## Evidence files

Gitignored, kept for the implementation tasks:

- `.b9s/latency-20261017/`: the bd-6a1l harness, stage timings, pprof.
- `.b9s/latency-20261018/probe/main.go`: times any query and reports bytes.
- `.b9s/latency-20261018/proxy/main.go`: MySQL proxy that logs every round trip.
- `.b9s/latency-20261018/latloop/main.go`: `SELECT 1` latency loop with 5 s buckets.
- `.b9s/latency-20261018/ws-proxy/`: throwaway workspace that points `bd serve` at the proxy.

The server-side `bd serve` test used a throwaway workspace in `/tmp` on the
Hetzner host and was removed after the measurements.
