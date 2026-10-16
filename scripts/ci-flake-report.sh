#!/usr/bin/env bash
# Lists every failed GitHub Actions run on a branch with the job and the tests
# that failed, and whether the commit touched the file or package the test
# lives in. A failure in a file the commit did not touch is a flake candidate.
#
#   scripts/ci-flake-report.sh [-b branch] [-n runs] [-w workflow]
#
# Defaults: branch main, 40 runs, workflow ci.yml. Needs gh (logged in) and jq.
# This is the analysis behind docs/adr/0059, as a command that runs in under a
# minute: bd-zoom.4.1.
set -euo pipefail

branch=main
count=40
workflow=ci.yml
while getopts "b:n:w:" opt; do
  case $opt in
    b) branch=$OPTARG ;;
    n) count=$OPTARG ;;
    w) workflow=$OPTARG ;;
    *) echo "usage: $0 [-b branch] [-n runs] [-w workflow]" >&2; exit 2 ;;
  esac
done

repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
runs=$(gh run list --workflow "$workflow" --branch "$branch" --limit "$count" \
  --json databaseId,conclusion,createdAt,headSha,displayTitle)

total=$(jq length <<<"$runs")
failed=$(jq '[.[] | select(.conclusion == "failure")] | length' <<<"$runs")
flaky=0

printf '%-12s %-10s %-12s %-9s %s\n' RUN DATE JOB TOUCHED TEST
while IFS=$'\t' read -r id date sha _title; do
  jobs=$(gh run view "$id" --json jobs -q '.jobs[] | select(.conclusion == "failure") | "\(.databaseId)\t\(.name)"')
  [ -n "$jobs" ] || continue
  files=$(gh api "repos/$repo/commits/$sha" -q '.files[].filename' 2>/dev/null || true)
  while IFS=$'\t' read -r jobid jobname; do
    log=$(gh api "repos/$repo/actions/jobs/$jobid/logs" 2>/dev/null || true)
    # Playwright lists each failure as "N) [project] › file:line › title";
    # go test prints "--- FAIL: TestName". Both lines carry a timestamp first.
    tests=$(grep -oE '[0-9]+\) \[[a-z0-9-]+\] › tests/[a-z-]+\.spec\.ts:[0-9]+:[0-9]+ › .*|--- FAIL: [A-Za-z0-9_/]+' <<<"$log" \
      | sed -E 's/^[0-9]+\) //; s/ ─+$//; s/ \([0-9.]+m?s\)$//' | sort -u)
    [ -n "$tests" ] || tests="(no test named: build or setup failure)"
    while IFS= read -r test; do
      # The spec file for a web failure, the package directory for a Go one.
      spec=$(grep -oE 'tests/[a-z-]+\.spec\.ts' <<<"$test" || true)
      touched=no
      if [ -n "$spec" ]; then
        grep -qx "web/$spec" <<<"$files" && touched=yes
        grep -qE '^web/(src|tests/harness|tests/fake-bd|playwright)' <<<"$files" && touched=web
      elif grep -q -- '--- FAIL' <<<"$test"; then
        grep -qE '\.go$' <<<"$files" && touched=go
      fi
      [ "$touched" = no ] && flaky=$((flaky + 1))
      printf '%-12s %-10s %-12s %-9s %s\n' "$id" "${date:0:10}" "${jobname:0:12}" "$touched" "${test:0:110}"
    done <<<"$tests"
  done <<<"$jobs"
done < <(jq -r '.[] | select(.conclusion == "failure") | "\(.databaseId)\t\(.createdAt)\t\(.headSha)\t\(.displayTitle)"' <<<"$runs")

echo
echo "$total runs on $branch ($workflow): $failed failed, $flaky failing tests in files the commit did not touch"
echo "=== CI_FLAKE_REPORT DONE runs=$total failed=$failed untouched=$flaky ==="
