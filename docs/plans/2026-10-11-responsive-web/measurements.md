---
plan_slug: 2026-10-11-responsive-web
phase: measurements
epic: b9m-vdp
task: b9m-vdp.1.1
status: partial
---

# Measurements: b9s web on local beads stores

The operator stopped this task on 2026-10-11 12:56 to start the implementation. Below is the raw summary of the last measure run (`npx playwright test --project measure`, `web/tests/measure.spec.ts`), copied from `.b9s-e2e/measure/report/summary.md` as the run was stopped. Local fixtures only (1000 and 5000 issues); beads.osen.co was not measured, by the operator's rule. The budgets for story 7 are set in the speed task (`b9m-vdp.1.2`) from these numbers.

## Raw summary

# b9s web measurements

Generated 2026-10-11T10:56:39.502Z on Apple M4 Max (16 cores), node v22.21.1, 5 runs per metric; median and p95 (nearest rank) in ms unless the field says otherwise.

## load

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | usable | 54 | 68 |
| pixel7 | firstRow | 39 | 49 |
| pixel7 | live | 62 | 88 |
| pixel7 | wallMs | 70 | 102 |
| pixel7 | ttfb | 0.9 | 3.2 |
| pixel7 | dcl | 20 | 21 |
| pixel7 | fcp | 40 | 44 |
| pixel7 | lcp | 0.0 | 0.0 |
| pixel7 | jsMs | 4.9 | 6.4 |
| pixel7 | jsKB | 41 | 41 |
| pixel7 | cssKB | 7.1 | 7.1 |
| pixel7 | sessionMs | 0.6 | 1.2 |
| pixel7 | snapshotMs | 5.6 | 9.1 |
| pixel7 | snapshotStart | 22 | 23 |
| pixel7 | snapshotEnd | 29 | 31 |
| pixel7 | snapshotKB | 32 | 32 |
| pixel7 | snapshotRawKB | 561 | 561 |
| pixel7 | renderMs | 16 | 25 |
| pixel7 | paintMs | 9.4 | 12 |
| pixel7 | rowsShown | 693 | 693 |
| pixel7 | issues | 1000 | 1000 |
| pixel7 | longTaskMs | 0.0 | 0.0 |
| pixel7 | hello | 55 | 72 |
| pixel7-cpu4x | usable | 172 | 186 |
| pixel7-cpu4x | firstRow | 111 | 127 |
| pixel7-cpu4x | live | 203 | 225 |
| pixel7-cpu4x | wallMs | 230 | 255 |
| pixel7-cpu4x | ttfb | 0.8 | 0.8 |
| pixel7-cpu4x | dcl | 58 | 72 |
| pixel7-cpu4x | fcp | 60 | 76 |
| pixel7-cpu4x | lcp | 60 | 264 |
| pixel7-cpu4x | jsMs | 4.8 | 5.0 |
| pixel7-cpu4x | jsKB | 41 | 41 |
| pixel7-cpu4x | cssKB | 7.1 | 7.1 |
| pixel7-cpu4x | sessionMs | 0.7 | 0.8 |
| pixel7-cpu4x | snapshotMs | 5.3 | 6.2 |
| pixel7-cpu4x | snapshotStart | 69 | 80 |
| pixel7-cpu4x | snapshotEnd | 74 | 86 |
| pixel7-cpu4x | snapshotKB | 32 | 32 |
| pixel7-cpu4x | snapshotRawKB | 561 | 561 |
| pixel7-cpu4x | renderMs | 63 | 65 |
| pixel7-cpu4x | paintMs | 36 | 41 |
| pixel7-cpu4x | rowsShown | 693 | 693 |
| pixel7-cpu4x | issues | 1000 | 1000 |
| pixel7-cpu4x | longTaskMs | 60 | 63 |
| pixel7-cpu4x | hello | 180 | 198 |
| pixel7-slow4g | usable | 1662 | 1859 |
| pixel7-slow4g | firstRow | 1644 | 1843 |
| pixel7-slow4g | live | 1982 | 2201 |
| pixel7-slow4g | wallMs | 1993 | 2210 |
| pixel7-slow4g | ttfb | 0.8 | 1.1 |
| pixel7-slow4g | dcl | 882 | 1018 |
| pixel7-slow4g | fcp | 700 | 964 |
| pixel7-slow4g | lcp | 1680 | 1880 |
| pixel7-slow4g | jsMs | 571 | 705 |
| pixel7-slow4g | jsKB | 41 | 41 |
| pixel7-slow4g | cssKB | 7.1 | 7.1 |
| pixel7-slow4g | sessionMs | 316 | 336 |
| pixel7-slow4g | snapshotMs | 468 | 477 |
| pixel7-slow4g | snapshotStart | 1208 | 1356 |
| pixel7-slow4g | snapshotEnd | 1632 | 1833 |
| pixel7-slow4g | snapshotKB | 32 | 32 |
| pixel7-slow4g | snapshotRawKB | 561 | 561 |
| pixel7-slow4g | renderMs | 19 | 27 |
| pixel7-slow4g | paintMs | 10 | 11 |
| pixel7-slow4g | rowsShown | 693 | 693 |
| pixel7-slow4g | issues | 1000 | 1000 |
| pixel7-slow4g | longTaskMs | 0.0 | 0.0 |
| pixel7-slow4g | hello | 1972 | 2194 |

## switch

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | toFrame | 0.6 | 5.6 |
| pixel7 | toDom | 0.5 | 0.6 |
| pixel7 | eventDur | 32 | 40 |
| pixel7 | sheetToFrame | 75 | 76 |
| pixel7 | projectsMs | 61 | 70 |
| pixel7 | openMs | 55 | 56 |
| pixel7 | snapshotMs | 7.4 | 7.7 |
| pixel7 | snapshotEnd | 62 | 63 |
| pixel7 | snapshots | 1.0 | 1.0 |
| pixel7 | sseProject | 55 | 55 |
| pixel7-cpu4x | toFrame | 1.9 | 3.7 |
| pixel7-cpu4x | toDom | 0.6 | 3.5 |
| pixel7-cpu4x | eventDur | 24 | 88 |
| pixel7-cpu4x | sheetToFrame | 97 | 120 |
| pixel7-cpu4x | projectsMs | 62 | 69 |
| pixel7-cpu4x | openMs | 61 | 77 |
| pixel7-cpu4x | snapshotMs | 10 | 15 |
| pixel7-cpu4x | snapshotEnd | 72 | 85 |
| pixel7-cpu4x | snapshots | 1.0 | 1.0 |
| pixel7-cpu4x | sseProject | 64 | 77 |
| pixel7-slow4g | toFrame | 0.8 | 1.2 |
| pixel7-slow4g | toDom | 0.5 | 0.6 |
| pixel7-slow4g | eventDur | 32 | 40 |
| pixel7-slow4g | sheetToFrame | 74 | 88 |
| pixel7-slow4g | snapshots | 0.0 | 0.0 |
| pixel7-slow4g | sseProject | 65 | 72 |

## detail

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | toFrame | 24 | 31 |
| pixel7 | toDom | 15 | 21 |
| pixel7 | eventDur | 48 | 48 |
| pixel7 | fullMs | 29 | 33 |
| pixel7 | issueMs | 11 | 21 |
| pixel7-cpu4x | toFrame | 76 | 92 |
| pixel7-cpu4x | toDom | 42 | 60 |
| pixel7-cpu4x | eventDur | 88 | 96 |
| pixel7-cpu4x | fullMs | 84 | 102 |
| pixel7-cpu4x | issueMs | 37 | 62 |

## fold

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | toFrame | 19 | 23 |
| pixel7 | toDom | 8.9 | 11 |
| pixel7 | eventDur | 32 | 40 |
| pixel7-cpu4x | toFrame | 59 | 70 |
| pixel7-cpu4x | toDom | 28 | 31 |
| pixel7-cpu4x | eventDur | 72 | 80 |

## chip

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | toFrame | 21 | 30 |
| pixel7 | toDom | 12 | 20 |
| pixel7 | eventDur | 32 | 48 |
| pixel7 | rows | 933 | 933 |
| pixel7-cpu4x | toFrame | 67 | 74 |
| pixel7-cpu4x | toDom | 32 | 35 |
| pixel7-cpu4x | eventDur | 72 | 80 |
| pixel7-cpu4x | rows | 933 | 933 |

## board

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | toFrame | 23 | 29 |
| pixel7 | toDom | 14 | 19 |
| pixel7 | eventDur | 40 | 48 |
| pixel7 | cards | 451 | 451 |
| pixel7-cpu4x | toFrame | 75 | 79 |
| pixel7-cpu4x | toDom | 42 | 47 |
| pixel7-cpu4x | eventDur | 88 | 96 |
| pixel7-cpu4x | cards | 441 | 441 |

## swipe

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | toFrame | 191 | 199 |
| pixel7 | toDom | 180 | 187 |
| pixel7 | snapBack | 0.2 | 181 |
| pixel7 | eventDur | 24 | 24 |
| pixel7-cpu4x | toFrame | 221 | 226 |
| pixel7-cpu4x | toDom | 188 | 189 |
| pixel7-cpu4x | snapBack | 2.1 | 189 |
| pixel7-cpu4x | eventDur | 24 | 24 |
| pixel7-slow4g | toFrame | 184 | 219 |
| pixel7-slow4g | toDom | 175 | 209 |
| pixel7-slow4g | snapBack | 0.1 | 0.2 |
| pixel7-slow4g | eventDur | 24 | 24 |

## write

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | roundTrip | 105 | 213 |
| pixel7 | postMs | 83 | 192 |
| pixel7 | postStart | 180 | 186 |
| pixel7 | sseMs | 83 | 191 |
| pixel7 | snapshotStart | 83 | 191 |
| pixel7 | snapshotMs | 6.2 | 7.2 |
| pixel7 | renderMs | 8.1 | 8.4 |
| pixel7 | paintMs | 7.3 | 7.8 |
| pixel7 | snapshots | 1.0 | 1.0 |
| pixel7-cpu4x | roundTrip | 132 | 149 |
| pixel7-cpu4x | postMs | 70 | 73 |
| pixel7-cpu4x | postStart | 188 | 188 |
| pixel7-cpu4x | sseMs | 69 | 72 |
| pixel7-cpu4x | snapshotStart | 69 | 72 |
| pixel7-cpu4x | snapshotMs | 7.6 | 8.4 |
| pixel7-cpu4x | renderMs | 29 | 33 |
| pixel7-cpu4x | paintMs | 31 | 35 |
| pixel7-cpu4x | snapshots | 1.0 | 1.0 |
| pixel7-slow4g | roundTrip | 576 | 634 |
| pixel7-slow4g | postMs | 336 | 424 |
| pixel7-slow4g | postStart | 174 | 209 |
| pixel7-slow4g | sseMs | 120 | 195 |
| pixel7-slow4g | snapshotStart | 120 | 195 |
| pixel7-slow4g | snapshotMs | 248 | 258 |
| pixel7-slow4g | renderMs | 189 | 320 |
| pixel7-slow4g | paintMs | 11 | 15 |
| pixel7-slow4g | snapshots | 1.0 | 2.0 |

## live

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | latency | 284 | 293 |
| pixel7 | toDom | 276 | 286 |
| pixel7 | sseMs | 262 | 270 |
| pixel7 | snapshotStart | 262 | 270 |
| pixel7 | snapshotEnd | 268 | 278 |
| pixel7 | snapshotMs | 5.8 | 7.4 |
| pixel7-cpu4x | latency | 329 | 336 |
| pixel7-cpu4x | toDom | 297 | 301 |
| pixel7-cpu4x | sseMs | 263 | 265 |
| pixel7-cpu4x | snapshotStart | 263 | 265 |
| pixel7-cpu4x | snapshotEnd | 269 | 271 |
| pixel7-cpu4x | snapshotMs | 6.0 | 9.2 |
| pixel7-slow4g | latency | 759 | 791 |
| pixel7-slow4g | toDom | 751 | 778 |
| pixel7-slow4g | sseMs | 268 | 271 |
| pixel7-slow4g | snapshotStart | 268 | 271 |
| pixel7-slow4g | snapshotEnd | 587 | 597 |
| pixel7-slow4g | snapshotMs | 319 | 329 |

## scroll

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | frames | 170 | 179 |
| pixel7 | fps | 85 | 88 |
| pixel7 | frameP95 | 18 | 19 |
| pixel7 | frameMax | 26 | 83 |
| pixel7 | slowFrames | 0.0 | 1.0 |
| pixel7 | scrolledPx | 2528 | 2541 |
| pixel7 | longTasks | 0.0 | 0.0 |
| pixel7 | longTaskMs | 0.0 | 0.0 |
| pixel7 | traceMs | 2030 | 2074 |
| pixel7 | traceBeginFrames | 246 | 250 |
| pixel7 | traceDroppedFrames | 69 | 74 |
| pixel7 | traceMainBusyMs | 184 | 206 |
| pixel7 | traceLongTasks | 0.0 | 0.0 |
| pixel7-cpu4x | frames | 163 | 201 |
| pixel7-cpu4x | fps | 83 | 97 |
| pixel7-cpu4x | frameP95 | 18 | 23 |
| pixel7-cpu4x | frameMax | 26 | 42 |
| pixel7-cpu4x | slowFrames | 0.0 | 1.0 |
| pixel7-cpu4x | scrolledPx | 2512 | 2520 |
| pixel7-cpu4x | longTasks | 0.0 | 0.0 |
| pixel7-cpu4x | longTaskMs | 0.0 | 0.0 |
| pixel7-cpu4x | traceMs | 1986 | 2066 |
| pixel7-cpu4x | traceBeginFrames | 240 | 248 |
| pixel7-cpu4x | traceDroppedFrames | 70 | 75 |
| pixel7-cpu4x | traceMainBusyMs | 264 | 288 |
| pixel7-cpu4x | traceLongTasks | 0.0 | 0.0 |

## drag

| variant | field | 1000 median | 1000 p95 |
|---|---|---:|---:|
| pixel7 | frames | 69 | 82 |
| pixel7 | fps | 99 | 103 |
| pixel7 | frameP95 | 17 | 17 |
| pixel7 | frameMax | 18 | 18 |
| pixel7 | slowFrames | 0.0 | 0.0 |
| pixel7-cpu4x | frames | 71 | 91 |
| pixel7-cpu4x | fps | 96 | 100 |
| pixel7-cpu4x | frameP95 | 17 | 17 |
| pixel7-cpu4x | frameMax | 18 | 111 |
| pixel7-cpu4x | slowFrames | 0.0 | 2.0 |
