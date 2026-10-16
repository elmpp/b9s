---
type: ADR
id: "0059"
title: "Gate pull requests on a subset of the tests and run the full matrix nightly"
status: active
date: 2026-10-16
---

## Context

CI runs every test on every push and pull request: the Go unit tests with
coverage, the 110 pseudo-terminal end-to-end tests, and the web suite of 704
Playwright tests across 18 projects (a tokens check, two phones, three desktop
browsers, an iPad, seven size classes and four timed perf projects). Each of the two jobs
takes about twelve minutes.

Of the last 40 runs on `main` (2026-10-06 to 2026-10-15), 16 failed. Six of
those failed on a test the commit had not touched: docs-only commits went red
on `wide.spec.ts` (a key pressed while the board redrew), on
`live-preview.spec.ts` (a poll through `page.evaluate` across a reload) and
on a Firefox-only hierarchy assertion that read the board before its rows had
rendered. Five more failed on `perf.spec.ts` budgets, which measure a render
beside three other workers on a four-core runner and miss by load alone. Five
were real failures. So a red run on `main` was wrong more often than it was
right, and a contributor learnt to rerun rather than read.

The suite has `retries: 0` on purpose: a retry turns a flake into a green
run nobody investigates (docs/testing.md, "Fix the race, never with a retry
or a longer timeout"). The problem is not that the tests exist. It is that
tests whose result depends on the runner's load or on a browser the change
cannot have affected sit in the path of every pull request.

## Decision

**A pull request is gated on the tests that fail for a wrong change and on
nothing else. The rest of the matrix runs nightly on a schedule, and a
nightly failure is filed as a Beads bug, never answered with a retry or a
longer timeout.**

The pull request gate (`ci.yml`, on push to `main` and on pull requests):

- `go build`, `go test ./pkg/... ./cmd/b9s` with the coverage threshold.
- `go test ./tests/e2e`, the pseudo-terminal suite. It takes about seven
  minutes and has been stable since bd-hffz.25 and bd-d924.
- The web projects `tokens`, `pixel`, `iphone`, `desktop` and `ipad`: one
  Chromium phone, one WebKit phone, one Chromium desktop and one WebKit
  tablet. Together they run every spec at least once on Chromium and once on
  WebKit, and every key binding on a desktop and a tablet.

The nightly run (`nightly.yml`, 03:00 UTC and on demand):

- The whole web matrix, including `desktop-firefox`, `desktop-safari`, the
  seven size-class projects and `measure`.
- The four perf projects on one worker, with the budgets as `perf.spec.ts`
  applies them (Chromium against the budget with the CI allowance, WebKit
  recorded without a verdict). A budget is a claim about the app alone, so
  it is measured only where the timing has the CPU to itself. The pull
  request gate does not run them at all: a number recorded there would be a
  number about the runner.
- `go test ./... -race -skip DoltIntegration -p 2`.
- A flake detector: the gate's web projects with `--repeat-each 5` on twice
  the usual workers. A test that both passes and fails in one run is a flake,
  and the job fails naming it.

Both workflows keep `retries: 0`. A Go test in `pkg/web` fails when the
Playwright config sets retries above zero without a written reason.

## Options considered

- **Split the suite into a gate and a nightly run** (chosen): the gate stays
  under ten minutes a job and only a wrong change turns it red. The cost is
  that a Firefox-only or size-class-only regression shows up the next
  morning instead of on the pull request, and someone has to read the
  nightly result.
- **Keep everything on the pull request and add retries**: green at once,
  and every flake becomes invisible. The suite would decay without anyone
  noticing. Rejected.
- **Keep everything on the pull request and shard across more runners**:
  faster, and still load-sensitive, since each shard is a four-core runner
  with four workers. The perf projects would still miss their budgets. It
  also multiplies the runner minutes of every push.
- **Delete the load-sensitive tests**: the perf budgets caught a quadratic
  render path once (docs/testing.md), and the size classes are the only
  check of ADR 0052's layout. Rejected.

## Consequences

- A contributor reads a red pull request as a fault in the change, which it
  almost always is. A flake that still reaches the gate is a bug with a
  Beads ticket, found by the nightly detector before it reaches a pull
  request.
- The nightly run needs a reader. `scripts/ci-flake-report.sh` turns the
  run history into a table of failing tests and whether the commit touched
  them, and `scripts/nightly-to-beads.sh` prints a ready `bd create` for a
  failed run with its failing tests and trace artifact. A nightly failure
  that is not a ticket within a day is the signal that this decision is not
  being kept.
- Perf budgets move: a budget regression shows up nightly, not on the pull
  request that caused it. The measure report and the perf numbers are
  artifacts of the nightly run, so a trend is readable from the run history.
- Firefox and Safari leave the gate. A change that breaks one of them only
  shows the next morning. That is the trade the numbers above justify: in
  40 runs, no real failure was Firefox-only or Safari-only, and one of the
  six flakes was.
- Re-evaluate when the gate takes more than ten minutes a job, when a
  nightly-only project catches a real regression more than once a month
  (then it belongs in the gate), or when GitHub runners get enough cores
  that the perf projects can share them.
