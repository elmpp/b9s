#!/bin/bash
# Builds B9s.app and B9s-<version>.dmg: one download that holds b9s, bd and
# the sample project, for macOS 12 or later on Apple silicon and Intel.
#
#   packaging/macos/build-dmg.sh            # or: make dmg
#
# Environment:
#   VERSION                 b9s version (default: git describe)
#   BD_VERSION              Beads release to bundle (default below)
#   OUT_DIR                 output folder (default: dist/macos)
#   MACOS_SIGN_IDENTITY     "Developer ID Application: ..." to sign for release;
#                           without it the app is signed ad hoc, and Gatekeeper
#                           asks the user to confirm the first open
#   MACOS_NOTARY_PROFILE    notarytool keychain profile; notarizes and staples
#                           the DMG (needs MACOS_SIGN_IDENTITY)
set -euo pipefail

BD_VERSION=${BD_VERSION:-1.3.1}
BD_REPO=gastownhall/beads

root=$(cd "$(dirname "$0")/../.." && pwd -P)
src=$root/packaging/macos
VERSION=${VERSION:-$(git -C "$root" describe --tags --always --dirty)}
VERSION=${VERSION#v}
OUT_DIR=${OUT_DIR:-$root/dist/macos}
SIGN_IDENTITY=${MACOS_SIGN_IDENTITY:-}
NOTARY_PROFILE=${MACOS_NOTARY_PROFILE:-}

[ "$(uname -s)" = Darwin ] || {
	echo "build-dmg: needs macOS (lipo, codesign, hdiutil)" >&2
	exit 1
}
if [ -n "$NOTARY_PROFILE" ] && [ -z "$SIGN_IDENTITY" ]; then
	echo "build-dmg: MACOS_NOTARY_PROFILE needs MACOS_SIGN_IDENTITY" >&2
	exit 1
fi

work=$OUT_DIR/work
cache=$OUT_DIR/cache/bd-$BD_VERSION
app=$work/B9s.app
dmg=$OUT_DIR/B9s-$VERSION.dmg
rm -rf "$work"
mkdir -p "$work" "$cache"

step() { printf '==> %s\n' "$*"; }

step "b9s $VERSION, universal"
# The SQLite driver is pure Go, so both architectures cross-compile without cgo.
for arch in arm64 amd64; do
	CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go -C "$root" build -trimpath \
		-ldflags "-s -w -X github.com/vanderheijden86/b9s/pkg/version.Version=v$VERSION -X github.com/vanderheijden86/b9s/pkg/version.Commit=$(git -C "$root" rev-parse HEAD)" \
		-o "$work/b9s-$arch" ./cmd/b9s
done
lipo -create -output "$work/b9s" "$work/b9s-arm64" "$work/b9s-amd64"

step "bd $BD_VERSION from $BD_REPO, universal"
base=https://github.com/$BD_REPO/releases/download/v$BD_VERSION
[ -f "$cache/checksums.txt" ] || curl -fsSL -o "$cache/checksums.txt" "$base/checksums.txt"
for arch in arm64 amd64; do
	tarball=beads_${BD_VERSION}_darwin_$arch.tar.gz
	[ -f "$cache/$tarball" ] || curl -fsSL -o "$cache/$tarball" "$base/$tarball"
	# A cached or downloaded tarball is used only when it matches the release's
	# own checksum list; a mismatch stops the build rather than ship it.
	(cd "$cache" && grep " $tarball\$" checksums.txt | shasum -a 256 -c -) >/dev/null || {
		echo "build-dmg: $tarball does not match $BD_REPO's checksums.txt" >&2
		rm -f "$cache/$tarball"
		exit 1
	}
	mkdir -p "$work/bd-$arch"
	tar -xzf "$cache/$tarball" -C "$work/bd-$arch"
done
lipo -create -output "$work/bd" "$work/bd-arm64/bd" "$work/bd-amd64/bd"

step "B9s.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources/bin" \
	"$app/Contents/Resources/sample-project/.beads" "$app/Contents/Resources/licenses"
short=${VERSION%%-*}
sed -e "s/@VERSION@/$VERSION/" -e "s/@SHORT_VERSION@/$short/" \
	"$src/Info.plist.in" >"$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist" >/dev/null
install -m 0755 "$src/launcher.sh" "$app/Contents/MacOS/B9s"
install -m 0755 "$src/b9s-app.sh" "$app/Contents/Resources/b9s-app"
install -m 0755 "$work/b9s" "$work/bd" "$app/Contents/Resources/bin/"
cp "$root/examples/sample-project/.beads/issues.jsonl" "$app/Contents/Resources/sample-project/.beads/"
cp "$root/examples/sample-project/README.md" "$app/Contents/Resources/sample-project/"
cp "$root/LICENSE" "$app/Contents/Resources/licenses/b9s-LICENSE"
cp "$work/bd-arm64/LICENSE" "$app/Contents/Resources/licenses/beads-LICENSE"

iconset=$work/AppIcon.iconset
mkdir -p "$iconset"
for size in 16 32 128 256 512; do
	sips -z $size $size "$root/pkg/web/dist/icon-512.png" --out "$iconset/icon_${size}x${size}.png" >/dev/null
	double=$((size * 2))
	if [ $double -le 512 ]; then
		sips -z $double $double "$root/pkg/web/dist/icon-512.png" \
			--out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
	fi
done
iconutil -c icns -o "$app/Contents/Resources/AppIcon.icns" "$iconset"

step "signing ($([ -n "$SIGN_IDENTITY" ] && echo "$SIGN_IDENTITY" || echo "ad hoc"))"
if [ -n "$SIGN_IDENTITY" ]; then
	sign=(codesign --force --timestamp --options runtime --sign "$SIGN_IDENTITY")
else
	sign=(codesign --force --sign -)
fi
# Inside out: the binaries first, then the bundle that seals them.
"${sign[@]}" "$app/Contents/Resources/bin/b9s" "$app/Contents/Resources/bin/bd"
"${sign[@]}" "$app"
codesign --verify --deep --strict "$app"

step "$(basename "$dmg")"
stage=$work/dmg
mkdir -p "$stage"
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"
cp "$src/ReadMe.txt" "$stage/Read Me.txt"
rm -f "$dmg"
hdiutil create -quiet -volname "B9s $VERSION" -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg"

if [ -n "$SIGN_IDENTITY" ]; then
	codesign --force --timestamp --sign "$SIGN_IDENTITY" "$dmg"
fi
if [ -n "$NOTARY_PROFILE" ]; then
	step "notarizing"
	xcrun notarytool submit "$dmg" --keychain-profile "$NOTARY_PROFILE" --wait
	xcrun stapler staple "$dmg"
fi

(cd "$OUT_DIR" && shasum -a 256 "$(basename "$dmg")" >"$(basename "$dmg").sha256")
step "done: $dmg"
