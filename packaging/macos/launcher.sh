#!/bin/bash
# B9s.app's executable. It asks the questions in native dialogs and leaves the
# work to b9s-app, so the behaviour stays testable without a screen.
set -u

resources=$(cd "$(dirname "$0")/../Resources" && pwd -P)
app=$resources/b9s-app
support=$HOME/Library/Application\ Support/B9s
mkdir -p "$support"

# dialog shows a message with buttons and prints the one clicked. Text reaches
# AppleScript as an argument, never spliced into the script, so quotes in an
# error message cannot break it.
dialog() {
	text=$1
	shift
	last=${!#}
	buttons=$(printf '"%s", ' "$@")
	buttons=${buttons%, }
	osascript - "$text" <<EOF 2>/dev/null
on run argv
	set r to display dialog (item 1 of argv) buttons {$buttons} default button "$last" with title "B9s" with icon note
	return button returned of r
end run
EOF
}

fail() {
	osascript - "$1" <<'EOF' >/dev/null 2>&1
on run argv
	display alert "B9s could not start" message (item 1 of argv) as critical
end run
EOF
	exit 1
}

out=$("$app" sample 2>&1) || fail "$out"

# Ask once about the command-line tools, and remember the answer. Run from the
# DMG, the links would point into a disk image that is ejected later, so the
# question waits until B9s runs from Applications.
case $resources in
/Volumes/* | */AppTranslocation/*) movable=1 ;;
*) movable=0 ;;
esac
if [ $movable = 0 ] && [ "$("$app" cli-status)" != linked ] && [ ! -f "$support/cli-asked" ]; then
	answer=$(dialog "Install the b9s and bd commands, so developers can use them in Terminal and in their own repositories? macOS asks for your password." "Not now" "Install")
	touch "$support/cli-asked"
	if [ "$answer" = Install ]; then
		if ! out=$("$app" install-cli 2>&1); then
			# /usr/local/bin belongs to root on a fresh Mac.
			quoted=$(printf '%s' "$app" | sed "s/'/'\\\\''/g")
			out=$(osascript - "'$quoted' install-cli" <<'EOF' 2>&1
on run argv
	do shell script (item 1 of argv) with administrator privileges
end run
EOF
			) || dialog "The commands were not installed: $out" "OK" >/dev/null
		fi
	fi
fi

choice=$(dialog "Open the sample project. The browser view suits planning and review, the terminal view suits developers." "Quit" "Terminal" "Browser")
case $choice in
Browser)
	url=$("$app" web 2>&1) || fail "$url"
	open "$url"
	;;
Terminal)
	cmd=$("$app" terminal-cmd 2>&1) || fail "$cmd"
	osascript - "$cmd" <<'EOF' >/dev/null
on run argv
	tell application "Terminal"
		activate
		do script (item 1 of argv)
	end tell
end run
EOF
	;;
esac
