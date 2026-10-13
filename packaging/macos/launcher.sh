#!/bin/bash
# B9s.app's launcher, run by the bundle's executable (stub/main.go) from
# Contents/Resources. It asks the questions in native dialogs and leaves the
# work to b9s-app, so the behaviour stays testable without a screen.
set -u

resources=$(cd "$(dirname "$0")" && pwd -P)
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

# Beads is what b9s runs on, so the bd and b9s commands are installed on every
# launch that finds one missing from the user's PATH, without a question. A tool
# the user installed some other way (Homebrew, say) stays as it is. Run from the
# DMG, the links would point into a disk image that is ejected later, so the
# install waits until B9s runs from Applications.
case $resources in
/Volumes/* | */AppTranslocation/*)
	dialog "Drag B9s to Applications and open it from there. B9s then installs the Beads (bd) and b9s commands." "Continue" >/dev/null
	;;
*)
	if [ "$("$app" cli-status)" != installed ] && ! out=$("$app" install-cli 2>&1); then
		# /usr/local/bin belongs to root on a fresh Mac.
		quoted=$(printf '%s' "$app" | sed "s/'/'\\\\''/g")
		out=$(osascript - "'$quoted' install-cli" <<'EOF' 2>&1
on run argv
	do shell script (item 1 of argv) with prompt "B9s installs the Beads (bd) and b9s commands." with administrator privileges
end run
EOF
		) || dialog "B9s could not install the Beads (bd) and b9s commands, and tries again on the next launch. $out" "OK" >/dev/null
	fi
	;;
esac

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
