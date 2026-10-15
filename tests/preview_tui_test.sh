#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)"
timeout_bin="$(command -v timeout || command -v gtimeout)"

# A container isolates the absolute runtime paths from the operator's machine.
"$timeout_bin" 30s docker run --network none \
  -v "$ROOT/scripts/preview:/source:ro" --entrypoint sh alpine:3.22 -eu -c '
  mkdir -p /data/projects/other/.beads /test-bin
  printf "%s\n" /data/projects/other > /tmp/b9s-preview-project
  cat > /test-bin/b9s <<'"'"'STUB'"'"'
#!/bin/sh
set -eu
test "$PWD" = /data/projects/other
test "$BEADS_DIR" = "$PWD/.beads"
test "$HOME" = /tmp
test "$TERM" = xterm-256color
test "$BW_NO_BROWSER" = 1
test "$BW_TEST_MODE" = 1
test "$BEADS_DOLT_PASSWORD" = "$B9S_DOLT_READ_PASSWORD"
test "$B9S_TRUSTED_DOLT_ENDPOINTS" = dolt.example:3306
test "$#" = 2
test "$1" = "argument with spaces"
test "$2" = "literal; argument"
printf "PASS environment, selected project and literal arguments\n"
exit 37
STUB
  chmod +x /test-bin/b9s
  export PATH="/test-bin:$PATH" B9S_DOLT_HOST=dolt.example B9S_DOLT_PORT=3306
  export B9S_DOLT_READ_PASSWORD='"'"'literal $password; "with spaces"'"'"'
  export HOME=/wrong TERM=dumb BEADS_DIR=/wrong BEADS_DOLT_PASSWORD=wrong
  rc=0
  sh /source/preview-tui "argument with spaces" "literal; argument" || rc=$?
  test "$rc" = 37
  printf "PASS child exit status\n"
  if B9S_DOLT_READ_PASSWORD= sh /source/preview-tui >/tmp/missing-password.log 2>&1; then exit 1; fi
  grep -q B9S_DOLT_READ_PASSWORD /tmp/missing-password.log
  printf "PASS missing password fails before starting b9s\n"
  printf "%s\n" /data/projects/missing > /tmp/b9s-preview-project
  if sh /source/preview-tui >/tmp/missing-project.log 2>&1; then exit 1; fi
  printf "PASS missing project fails before starting b9s\n"
  printf "=== PREVIEW_TUI DONE pass=4 fail=0 ===\n"
'

if [[ $# -gt 0 ]]; then
  terminal_log="$(mktemp "${TMPDIR:-/tmp}/b9s-preview-tui.XXXXXX")"
  "$timeout_bin" 30s docker run -t --network none --user 65532:65532 \
    --tmpfs /data:mode=1777 -v "$ROOT/tests/testdata/minimal.jsonl:/fixture.jsonl:ro" \
    -e B9S_DOLT_HOST=unused.invalid -e B9S_DOLT_PORT=3306 \
    -e B9S_DOLT_READ_PASSWORD=fixture-only -e B9S_TUI_AUTOCLOSE_MS=2000 \
    --entrypoint sh "$1" -eu -c '
      mkdir -p /data/projects/fixture/.beads
      cp /fixture.jsonl /data/projects/fixture/.beads/issues.jsonl
      printf "%s\n" /data/projects/fixture > /tmp/b9s-preview-project
      test "$(cat /app/.commit-sha)" = "$1"
      exec timeout 15 preview-tui
    ' sh "${2:?expected image identity required}" >"$terminal_log" 2>&1
  grep -q Root "$terminal_log"
  printf '=== PREVIEW_TUI_IMAGE DONE pass=1 fail=0 ===\nEvidence: %s\n' "$terminal_log"
fi
