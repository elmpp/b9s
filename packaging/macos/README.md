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
   2. asks once: install the b9s and bd commands into /usr/local/bin?
   3. asks: open the project in the Browser or in Terminal?
        Browser  ─▶ b9s web on 127.0.0.1:7997, opened on its pairing link
        Terminal ─▶ Terminal.app running b9s in the sample folder
```

- **The sample** is the 52-issue web shop from
  [`examples/sample-project`](../../examples/sample-project/README.md), imported
  into an embedded Beads project, so edits in either view are real `bd` writes.
  B9s never touches `~/B9s/Sample Project` again once it exists.
- **The browser view** keeps pairing on: the app opens the pairing link that
  `b9s web` printed, so other local users cannot use the board without it. It
  listens on port 7997, not `b9s web`'s 7979, to stay clear of a server you
  started by hand. The server keeps running after the app quits; the next open
  reuses it. The project list shows every Beads project directly under `~/B9s`.
- **The commands** are links into the app bundle. The app never replaces a
  `b9s` or `bd` it did not install (from Homebrew, say), and it does not offer
  the links while it runs from the DMG, because they would point into a disk
  image that is ejected later.

Logs go to `~/Library/Logs/B9s`, state to `~/Library/Application Support/B9s`.
Delete `cli-asked` there to be asked about the commands again.

## Files

| File | Becomes | Role |
|---|---|---|
| `launcher.sh` | `Contents/MacOS/B9s` | The app's executable: the dialogs, through `osascript` |
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
reuse, quoting of paths with spaces and quotes, and the launcher's dialog flow.
They do not cover Gatekeeper or a real dialog; check those by hand on a Mac
before publishing a DMG.
