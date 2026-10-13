# B9s.app and the macOS DMG

One download that installs b9s and Beads together and opens a sample project,
for developers and product owners alike. A developer gets the `b9s` and `bd`
commands for their own repositories. A product owner gets the same issues in
the browser, without installing anything else or opening a terminal.

## What the user sees

```
 B9s-<version>.dmg
 ├── B9s.app          drag to Applications
 ├── Applications     (link)
 └── Read Me.txt

 first open of B9s.app
   1. creates ~/B9s/Sample Project        (bd init --from-jsonl, embedded Dolt)
   2. installs bd and b9s into /usr/local/bin, unless the user has them
   3. asks once: Quit, Open the sample, or Add your project…
        Open the sample    ─▶ b9s web on 127.0.0.1:7997, opened on its pairing link
        Add your project…  ─▶ the same, on #/add: the add-a-project flow

 every later open
   opens the browser view without a question
```

- **Adding a project** hands a repository to the user's own AI (Claude Code,
  Codex or Cursor) with a three-line prompt that runs `b9s onboard`, and ticks
  off each setup step it sees land; a plan without code becomes
  `~/B9s/<name>`. [ADR 0057](../../docs/adr/0057-onboard-a-project-through-the-users-own-ai.md)
  says why. Terminal users choose Open in Terminal on any project.

- **The sample** is the 52-issue web shop from
  [`examples/sample-project`](../../examples/sample-project/README.md), imported
  into an embedded Beads project, so edits in either view are real `bd` writes.
  B9s never touches `~/B9s/Sample Project` again once it exists.
- **The browser view** keeps pairing on: the app opens the pairing link that
  `b9s web` printed, so other local users cannot use the board without it. It
  listens on port 7997, not `b9s web`'s 7979, to stay clear of a server you
  started by hand. The server keeps running after the app quits; the next open
  reuses it. The project list shows every Beads project directly under `~/B9s`.
- **The commands** are links into the app bundle, made without a question
  because b9s needs Beads. macOS asks for the password when `/usr/local/bin`
  belongs to root. The app skips a tool the user already has anywhere on their
  shell's PATH (Homebrew, `~/.local/bin`, `go install`) and never replaces a
  file it did not create. The app itself always runs its bundled tools, so an
  older `bd` of the user's does no harm. From the DMG it links nothing, because
  the links would point into a disk image that is ejected later; it asks the
  user to drag B9s to Applications first. A launch that could not install the
  commands tries again on the next launch.

Logs go to `~/Library/Logs/B9s`, state to `~/Library/Application Support/B9s`.

## Files

| File | Becomes | Role |
|---|---|---|
| `stub/` | `Contents/MacOS/B9s` | The app's executable: a universal Go binary that runs the launcher. A script there makes macOS ask for Rosetta on Apple silicon, so the build refuses one |
| `launcher.sh` | `Contents/Resources/launcher` | The dialogs, through `osascript` |
| `b9s-app.sh` | `Contents/Resources/b9s-app` | Everything without a screen: `sample`, `web`, `terminal-cmd`, `cli-status`, `install-cli` |
| `Info.plist.in` | `Contents/Info.plist` | Bundle metadata, version filled in at build |
| `ReadMe.txt` | `Read Me.txt` in the DMG | Install steps for the person who downloaded it |
| `build-dmg.sh` | | Builds all of it |
| `app_test.go` | | Runs both scripts against a temporary home with stub `b9s`, `bd`, `osascript` and `open` |

## Building

```bash
make dmg                                  # dist/macos/B9s-<version>.dmg and its .sha256
BD_VERSION=1.3.1 VERSION=1.4.0 make dmg   # pin both versions
```

The build needs macOS (it uses `lipo`, `codesign`, `sips`, `iconutil` and
`hdiutil`), Go, and network access to download bd. It compiles b9s for arm64
and amd64 and joins them into one universal binary, downloads the bd release
tarballs from [gastownhall/beads](https://github.com/gastownhall/beads), checks
each against that release's `checksums.txt`, and joins those too. Downloads are
cached in `dist/macos/cache`.

Before raising `BD_VERSION`, run the bd contract test against that release
([docs/testing.md](../../docs/testing.md#bd-releases)).

### Signing and notarizing

Without credentials the app is signed ad hoc. macOS then refuses the first
open of a downloaded copy until the user right-clicks B9s and chooses Open.
For a release, sign with a Developer ID and notarize:

```bash
xcrun notarytool store-credentials b9s-notary   # once: Apple ID, team, app password
MACOS_SIGN_IDENTITY="Developer ID Application: Name (TEAMID)" \
MACOS_NOTARY_PROFILE=b9s-notary make dmg
```

## Testing

```bash
go test ./packaging/macos/ -v             # the scripts, on any Unix with bash
make dmg                                  # then mount the DMG and open B9s.app
```

The Go tests cover the sample import (once, never with the shell's Beads server
settings, nothing left behind on failure), the command links, the web server
reuse, quoting of paths with spaces and quotes, skipping tools the user already
has, and the launcher installing the commands without a question.
They do not cover Gatekeeper or a real dialog; check those by hand on a clean
Mac before publishing a DMG. A [Tart](https://tart.run) VM from
`ghcr.io/cirruslabs/macos-sequoia-vanilla` works: it has no Rosetta, Homebrew or
`/usr/local/bin`. Its Gatekeeper is off, so switch it on first
(`sudo spctl --global-enable`), and mark the copied DMG as downloaded with a
`com.apple.quarantine` attribute.
