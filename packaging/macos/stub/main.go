// Command stub is B9s.app's main executable (Contents/MacOS/B9s). It runs the
// launcher script in Contents/Resources and does nothing else.
//
// The executable cannot be the script itself: on Apple silicon, macOS takes an
// app whose main executable is not a native binary for an Intel app and asks
// the user to install Rosetta before it opens. build-dmg.sh compiles this for
// arm64 and amd64 and refuses a bundle whose executable lacks either slice.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func main() {
	self, err := os.Executable()
	if err == nil {
		self, err = filepath.EvalSymlinks(self)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "B9s: cannot find its own bundle:", err)
		os.Exit(1)
	}
	launcher := filepath.Join(filepath.Dir(self), "..", "Resources", "launcher")
	launcher = filepath.Clean(launcher)
	argv := append([]string{"/bin/bash", launcher}, os.Args[1:]...)
	err = syscall.Exec("/bin/bash", argv, os.Environ())
	fmt.Fprintln(os.Stderr, "B9s: cannot run", launcher+":", err)
	os.Exit(1)
}
