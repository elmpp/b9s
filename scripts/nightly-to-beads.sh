#!/usr/bin/env bash
# Turns a failed GitHub Actions run into a ready bd create command: the failing
# jobs, the tests they failed on, the run URL and the artifacts that hold the
# traces. It prints the command and never runs it, so the reader checks the
# title and the parent before filing (docs/adr/0059: a nightly failure is a
# Beads bug, not a retry).
#
#   scripts/nightly-to-beads.sh <run-id> [--parent <issue-id>]
#
# Needs gh (logged in) and jq. The run id is the number in the run's URL or in
# `gh run list --workflow nightly.yml`.
set -euo pipefail

run=${1:-}
parent=""
if [ -z "$run" ]; then
  echo "usage: $0 <run-id> [--parent <issue-id>]" >&2
  exit 2
fi
shift
while [ $# -gt 0 ]; do
  case $1 in
    --parent) parent=$2; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

repo=$(gh repo view --json nameWithOwner -q .nameWithOwner)
info=$(gh run view "$run" --json name,url,headSha,createdAt,conclusion,jobs)
conclusion=$(jq -r .conclusion <<<"$info")
if [ "$conclusion" != "failure" ]; then
  echo "run $run concluded $conclusion, nothing to file" >&2
  exit 1
fi

body=""
title_tests=""
while IFS=$'\t' read -r jobid jobname; do
  log=$(gh api "repos/$repo/actions/jobs/$jobid/logs" 2>/dev/null || true)
  tests=$(grep -oE '[0-9]+\) \[[a-z0-9-]+\] › tests/[a-z-]+\.spec\.ts:[0-9]+:[0-9]+ › .*|--- FAIL: [A-Za-z0-9_/]+|FLAKY \(.*|  \[[a-z0-9-]+\] › .*\([0-9]+/[0-9]+ failed\)' <<<"$log" \
    | sed -E 's/^[0-9]+\) //; s/ ─+$//; s/ \([0-9.]+m?s\)$//; s/^ +//' | sort -u)
  [ -n "$tests" ] || tests="(no test named: read the job log)"
  body+="Job $jobname:"$'\n'"$tests"$'\n\n'
  first=$(head -1 <<<"$tests" | sed -E 's/^\[[a-z0-9-]+\] › //; s/ › .*//')
  title_tests+="${title_tests:+, }$first"
done < <(jq -r '.jobs[] | select(.conclusion == "failure") | "\(.databaseId)\t\(.name)"' <<<"$info")

artifacts=$(gh api "repos/$repo/actions/runs/$run/artifacts" -q '.artifacts[] | "\(.name) (\(.expires_at[:10]))"' | paste -sd ', ' - || true)
name=$(jq -r .name <<<"$info")
url=$(jq -r .url <<<"$info")
sha=$(jq -r .headSha <<<"$info")
date=$(jq -r '.createdAt[:10]' <<<"$info")

description="$name run of $date failed on commit ${sha:0:9}: $url

$body"
if [ -n "$artifacts" ]; then
  description+="Artifacts: $artifacts. Download with gh run download $run."
else
  description+="No artifacts were uploaded."
fi
description+="

Triage: run the failing test locally with --repeat-each 20 (B9S_E2E_CPU_THROTTLE=10 for a Chromium race, docs/testing.md), find the race, and fix it without a retry or a longer timeout."

title="$name failure $date: ${title_tests:0:80}"
printf 'bd create --type=bug --priority=2'
[ -n "$parent" ] && printf ' --parent=%q' "$parent"
printf ' \\\n  --title=%q \\\n  --description=%q\n' "$title" "$description"
