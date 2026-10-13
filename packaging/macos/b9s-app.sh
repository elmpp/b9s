#!/bin/bash
# b9s-app: the work behind B9s.app. The launcher (launcher.sh) owns the
# dialogs; this script owns everything a test can run without a screen:
#
#   b9s-app sample        create the sample project if it is missing, print its path
#   b9s-app web           start b9s web for the sample (or reuse it), print the URL to open
#   b9s-app terminal-cmd  print the shell command that opens b9s on the sample
#   b9s-app cli-status    print "installed", or "missing" when b9s or bd is not on
#                         the user's PATH
#   b9s-app install-cli   link b9s and bd into the CLI directory, unless the user
#                         has them (exit 3: needs admin, exit 4: runs from the DMG)
#
# Every location can be moved with an environment variable, which is how the
# tests run it against a temporary home with stub binaries.
set -eu

here=$(cd "$(dirname "$0")" && pwd -P)

BIN_DIR=${B9S_APP_BIN_DIR:-$here/bin}
SAMPLE_SRC=${B9S_APP_SAMPLE_SRC:-$here/sample-project}
PROJECTS_ROOT=${B9S_APP_PROJECTS_ROOT:-$HOME/B9s}
SAMPLE_DIR="$PROJECTS_ROOT/Sample Project"
SUPPORT_DIR=${B9S_APP_SUPPORT_DIR:-$HOME/Library/Application Support/B9s}
LOG_DIR=${B9S_APP_LOG_DIR:-$HOME/Library/Logs/B9s}
CLI_DIR=${B9S_APP_CLI_DIR:-/usr/local/bin}
# Not b9s web's default 7979, so the app never takes over a b9s web the user
# started by hand.
WEB_PORT=${B9S_APP_WEB_PORT:-7997}
WEB_WAIT_SECONDS=${B9S_APP_WEB_WAIT_SECONDS:-20}

# The bundled tools come first, so the app never runs an older bd or b9s the
# user happens to have installed. A Finder launch has a minimal PATH anyway.
PATH="$BIN_DIR:$PATH"
export PATH

die() {
	printf 'b9s-app: %s\n' "$*" >&2
	exit 1
}

# sample creates the editable sample project from the bundled JSONL. It never
# touches a folder that already exists: that is the user's project by now.
sample() {
	if [ -d "$SAMPLE_DIR/.beads" ]; then
		printf '%s\n' "$SAMPLE_DIR"
		return 0
	fi
	[ -f "$SAMPLE_SRC/.beads/issues.jsonl" ] || die "no sample issues at $SAMPLE_SRC/.beads/issues.jsonl"

	mkdir -p "$PROJECTS_ROOT"
	# Build in a sibling folder and rename at the end, so a failed import never
	# leaves a half-made project that the next launch would take as finished.
	staging=$(mktemp -d "$PROJECTS_ROOT/.sample.XXXXXX")
	trap 'rm -rf "$staging"' EXIT
	mkdir -p "$staging/.beads"
	cp "$SAMPLE_SRC/.beads/issues.jsonl" "$staging/.beads/issues.jsonl"
	[ -f "$SAMPLE_SRC/README.md" ] && cp "$SAMPLE_SRC/README.md" "$staging/README.md"

	mkdir -p "$LOG_DIR"
	# A shell profile or direnv may point bd at a Dolt server. The sample is a
	# local embedded project, so none of those settings may reach this init.
	if ! (
		cd "$staging"
		for name in $(env | awk -F= '/^(BEADS|BD|DOLT)_/ { print $1 }'); do
			unset "$name"
		done
		BD_NON_INTERACTIVE=1 bd init --from-jsonl --prefix shop --skip-hooks
	) >"$LOG_DIR/sample-init.log" 2>&1; then
		die "bd init failed, see $LOG_DIR/sample-init.log"
	fi
	grep -q '"dolt_mode": "embedded"' "$staging/.beads/metadata.json" ||
		die "bd did not create an embedded project, see $LOG_DIR/sample-init.log"

	mv "$staging" "$SAMPLE_DIR"
	trap - EXIT
	printf '%s\n' "$SAMPLE_DIR"
}

pid_file() { printf '%s/web.pid\n' "$SUPPORT_DIR"; }
web_log() { printf '%s/web.log\n' "$LOG_DIR"; }

# web_url reads the pairing link from the log b9s web wrote. Pairing stays on,
# so another local user or process cannot use the board without the link.
web_url() {
	awk '/This machine/ { print $NF; exit }' "$(web_log)" 2>/dev/null
}

web_running() {
	[ -f "$(pid_file)" ] || return 1
	pid=$(cat "$(pid_file)")
	[ -n "$pid" ] && kill -0 "$pid" 2>/dev/null
}

web() {
	dir=$(sample)
	mkdir -p "$SUPPORT_DIR" "$LOG_DIR"
	if web_running && url=$(web_url) && [ -n "$url" ]; then
		printf '%s\n' "$url"
		return 0
	fi

	(
		cd "$dir"
		nohup b9s web --listen "127.0.0.1:$WEB_PORT" --projects-root "$PROJECTS_ROOT" \
			>"$(web_log)" 2>&1 </dev/null &
		echo $! >"$(pid_file)"
	)

	deadline=$(($(date +%s) + WEB_WAIT_SECONDS))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		url=$(web_url)
		if [ -n "$url" ]; then
			printf '%s\n' "$url"
			return 0
		fi
		web_running || die "b9s web stopped: $(tail -n 3 "$(web_log)" | tr '\n' ' ')"
		sleep 0.2
	done
	die "b9s web printed no link within ${WEB_WAIT_SECONDS}s, see $(web_log)"
}

# quote wraps $1 in single quotes for a shell, so a path with spaces or quotes
# survives being typed into Terminal.
quote() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

terminal_cmd() {
	dir=$(sample)
	# $PATH expands in the Terminal session, not here.
	# shellcheck disable=SC2016
	printf 'cd %s && PATH=%s:"$PATH" exec b9s\n' "$(quote "$dir")" "$(quote "$BIN_DIR")"
}

# ours reports whether $1 is a link into this app's bin directory.
ours() {
	[ -L "$1" ] && [ "$(readlink "$1")" = "$BIN_DIR/$(basename "$1")" ]
}

# user_path prints the PATH of the user's own shell. A Finder launch inherits
# only /usr/bin:/bin:/usr/sbin:/sbin, which misses Homebrew and ~/.local/bin.
# The common install folders follow, for a shell profile that fails to load.
user_path() {
	if [ -n "${B9S_APP_USER_PATH:-}" ]; then
		printf '%s\n' "$B9S_APP_USER_PATH"
		return
	fi
	# -i reads .zshrc too, where ~/.local/bin is often added. The alarm stops a
	# profile that waits for input.
	login=$(perl -e 'alarm 10; exec @ARGV' "${SHELL:-/bin/zsh}" -lic 'printf "\n%s\n" "$PATH"' \
		</dev/null 2>/dev/null | tail -n 1)
	printf '%s:/opt/homebrew/bin:/usr/local/bin:%s/.local/bin:%s/go/bin:%s/bin\n' \
		"$login" "$HOME" "$HOME" "$HOME"
}

# installed_elsewhere prints the first $1 on the user's PATH that this app did
# not link, and fails when there is none. The PATH is read once per run:
# starting the user's shell costs a second or more.
installed_elsewhere() {
	[ -n "${user_path_cache:-}" ] || user_path_cache=$(user_path)
	IFS=: read -r -a dirs <<<"$user_path_cache"
	for d in "${dirs[@]}"; do
		[ -n "$d" ] && [ "$d" != "$BIN_DIR" ] || continue
		if [ -x "$d/$1" ] && [ ! -d "$d/$1" ] && ! ours "$d/$1"; then
			printf '%s\n' "$d/$1"
			return 0
		fi
	done
	return 1
}

# needs_link reports whether $1 is neither linked by this app nor installed by
# the user some other way.
needs_link() {
	! ours "$CLI_DIR/$1" && ! installed_elsewhere "$1" >/dev/null
}

cli_status() {
	for tool in b9s bd; do
		if needs_link "$tool"; then
			echo missing
			return
		fi
	done
	echo installed
}

# movable_bundle reports whether the app runs from a place that goes away: the
# mounted DMG, or the random read-only copy macOS runs a quarantined app from
# (App Translocation) until the user moves it.
movable_bundle() {
	case $BIN_DIR in
	/Volumes/* | */AppTranslocation/*) return 0 ;;
	*) return 1 ;;
	esac
}

# install_cli links each tool the user does not have yet. A bd from Homebrew
# or a b9s from go install belongs to the user: linking a second copy would
# shadow it or be shadowed by it, depending on PATH order. The app itself
# always runs its bundled tools, so an older copy of the user's does no harm.
install_cli() {
	if movable_bundle; then
		echo "b9s-app: move B9s to Applications first: the tools would be linked into $BIN_DIR, which disappears" >&2
		exit 4
	fi
	# Filled here, because each $(installed_elsewhere) below is a subshell.
	user_path_cache=$(user_path)
	todo=()
	for tool in b9s bd; do
		target=$CLI_DIR/$tool
		if ours "$target"; then
			echo "$tool: already linked"
		elif existing=$(installed_elsewhere "$tool"); then
			echo "$tool: already installed at $existing"
		elif [ -e "$target" ] || [ -L "$target" ]; then
			echo "$tool: kept the existing $target"
		else
			todo+=("$tool")
		fi
	done
	[ ${#todo[@]} -gt 0 ] || return 0

	if [ ! -d "$CLI_DIR" ] || [ ! -w "$CLI_DIR" ]; then
		mkdir -p "$CLI_DIR" 2>/dev/null || true
		[ -w "$CLI_DIR" ] || exit 3
	fi
	for tool in "${todo[@]}"; do
		ln -s "$BIN_DIR/$tool" "$CLI_DIR/$tool"
		echo "$tool: linked $CLI_DIR/$tool"
	done
}

case ${1:-} in
sample) sample ;;
web) web ;;
terminal-cmd) terminal_cmd ;;
cli-status) cli_status ;;
install-cli) install_cli ;;
*)
	echo "usage: b9s-app sample|web|terminal-cmd|cli-status|install-cli" >&2
	exit 2
	;;
esac
