// Package macos holds the B9s.app bundle and DMG build. The tests run the
// app's setup script (b9s-app.sh) against a temporary home with stub b9s and
// bd binaries, so they need bash but no Mac, no screen and no real Beads.
package macos

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// appEnv is one isolated install: a home, a projects root whose name holds a
// space and a quote (as "Sample Project" and real home folders do), stub
// tools, and a CLI directory.
type appEnv struct {
	t        *testing.T
	home     string
	root     string
	bin      string
	cliDir   string
	callsLog string
	// userBin stands in for a directory on the user's login-shell PATH, such
	// as Homebrew's, where they may have installed bd or b9s themselves.
	userBin string
	extra   []string
}

const bdStub = `#!/bin/bash
# Records each call and its Beads environment, then acts like bd init.
printf '%s\n' "$*" >>"$STUB_CALLS"
env | grep -E '^(BEADS|BD|DOLT)_' >>"$STUB_CALLS.env" || true
[ -n "${STUB_BD_FAIL:-}" ] && { echo "bd: simulated failure" >&2; exit 1; }
mkdir -p .beads
printf '{\n  "dolt_mode": "embedded"\n}\n' >.beads/metadata.json
`

const b9sStub = `#!/bin/bash
# Acts like b9s web: prints the banner with a pairing link, then serves.
printf 'b9s %s\n' "$*" >>"$STUB_CALLS"
printf 'b9s web is serving shop\n\n  This machine  http://127.0.0.1:7997/pair?token=stub\n'
exec sleep 30
`

func newAppEnv(t *testing.T) *appEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("B9s.app is a macOS bundle")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	dir := t.TempDir()
	e := &appEnv{
		t:        t,
		home:     filepath.Join(dir, "home"),
		root:     filepath.Join(dir, "home", "B9s it's mine"),
		bin:      filepath.Join(dir, "app bin"),
		cliDir:   filepath.Join(dir, "cli"),
		callsLog: filepath.Join(dir, "calls"),
		userBin:  filepath.Join(dir, "user bin"),
	}
	for _, d := range []string{e.home, e.bin, e.cliDir, e.userBin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(t, filepath.Join(e.bin, "bd"), bdStub)
	writeExec(t, filepath.Join(e.bin, "b9s"), b9sStub)
	t.Cleanup(func() {
		// Stop a stub server the web tests started.
		if pid, err := os.ReadFile(filepath.Join(e.home, "support", "web.pid")); err == nil {
			_ = exec.Command("kill", strings.TrimSpace(string(pid))).Run()
		}
	})
	return e
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// run calls b9s-app with a clean environment, plus a Beads setting that must
// never reach the sample's bd init.
func (e *appEnv) run(args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command("bash", append([]string{"b9s-app.sh"}, args...)...)
	cmd.Env = append([]string{
		"HOME=" + e.home,
		"PATH=/usr/bin:/bin",
		"B9S_APP_BIN_DIR=" + e.bin,
		"B9S_APP_SAMPLE_SRC=" + filepath.Join("..", "..", "examples", "sample-project"),
		"B9S_APP_PROJECTS_ROOT=" + e.root,
		"B9S_APP_SUPPORT_DIR=" + filepath.Join(e.home, "support"),
		"B9S_APP_LOG_DIR=" + filepath.Join(e.home, "logs"),
		"B9S_APP_CLI_DIR=" + e.cliDir,
		"B9S_APP_USER_PATH=" + e.userBin + ":" + e.cliDir,
		"B9S_APP_WEB_WAIT_SECONDS=5",
		"BEADS_DOLT_SERVER_HOST=10.0.0.1",
		"STUB_CALLS=" + e.callsLog,
	}, e.extra...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (e *appEnv) calls() []string {
	b, _ := os.ReadFile(e.callsLog)
	return strings.Fields(strings.ReplaceAll(string(b), " ", "_"))
}

func TestSampleCreatesAnEmbeddedProjectOnce(t *testing.T) {
	e := newAppEnv(t)
	out, err := e.run("sample")
	if err != nil {
		t.Fatalf("sample: %v\n%s", err, out)
	}
	want := filepath.Join(e.root, "Sample Project")
	if out != want {
		t.Fatalf("sample printed %q, want %q", out, want)
	}
	if _, err := os.Stat(filepath.Join(want, ".beads", "issues.jsonl")); err != nil {
		t.Fatalf("issues not copied: %v", err)
	}
	if got := e.calls(); len(got) != 1 || got[0] != "init_--from-jsonl_--prefix_shop_--skip-hooks" {
		t.Fatalf("bd calls = %v", got)
	}
	if env, _ := os.ReadFile(e.callsLog + ".env"); strings.Contains(string(env), "BEADS_DOLT_SERVER_HOST") {
		t.Fatalf("a Beads server setting reached bd init:\n%s", env)
	}

	if out, err := e.run("sample"); err != nil || out != want {
		t.Fatalf("second sample: %v %q", err, out)
	}
	if got := e.calls(); len(got) != 1 {
		t.Fatalf("second launch ran bd again: %v", got)
	}
}

func TestFailedSampleImportLeavesNothingBehind(t *testing.T) {
	e := newAppEnv(t)
	e.extra = []string{"STUB_BD_FAIL=1"}
	if out, err := e.run("sample"); err == nil {
		t.Fatalf("sample succeeded with a failing bd: %s", out)
	}
	entries, _ := os.ReadDir(e.root)
	if len(entries) != 0 {
		t.Fatalf("projects root kept %v after a failed import", entries)
	}

	e.extra = nil
	if out, err := e.run("sample"); err != nil {
		t.Fatalf("retry after a failure: %v\n%s", err, out)
	}
}

func TestInstallCLILinksBothToolsOnACleanMac(t *testing.T) {
	e := newAppEnv(t)
	if out, _ := e.run("cli-status"); out != "missing" {
		t.Fatalf("cli-status before install = %q", out)
	}
	if out, err := e.run("install-cli"); err != nil {
		t.Fatalf("install-cli: %v\n%s", err, out)
	}
	for _, tool := range []string{"b9s", "bd"} {
		if target, err := os.Readlink(filepath.Join(e.cliDir, tool)); err != nil || target != filepath.Join(e.bin, tool) {
			t.Fatalf("%s link = %q, %v", tool, target, err)
		}
	}
	if out, _ := e.run("cli-status"); out != "installed" {
		t.Fatalf("cli-status after install = %q", out)
	}
	if out, err := e.run("install-cli"); err != nil || !strings.Contains(out, "bd: already linked") {
		t.Fatalf("second install-cli: %v %q", err, out)
	}
}

func TestInstallCLISkipsAToolTheUserAlreadyHas(t *testing.T) {
	e := newAppEnv(t)
	writeExec(t, filepath.Join(e.userBin, "bd"), "#!/bin/sh\n")

	out, err := e.run("install-cli")
	if err != nil {
		t.Fatalf("install-cli: %v\n%s", err, out)
	}
	if _, err := os.Lstat(filepath.Join(e.cliDir, "bd")); err == nil {
		t.Fatal("linked a second bd beside the one the user already has")
	}
	if !strings.Contains(out, "bd: already installed at "+filepath.Join(e.userBin, "bd")) {
		t.Fatalf("install-cli output does not name the existing bd: %q", out)
	}
	if _, err := os.Readlink(filepath.Join(e.cliDir, "b9s")); err != nil {
		t.Fatalf("b9s not linked: %v", err)
	}
	if out, _ := e.run("cli-status"); out != "installed" {
		t.Fatalf("cli-status = %q", out)
	}
}

func TestInstallCLIKeepsAFileItDidNotPutThere(t *testing.T) {
	e := newAppEnv(t)
	theirs := filepath.Join(e.cliDir, "bd")
	if err := os.WriteFile(theirs, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := e.run("install-cli"); err != nil {
		t.Fatalf("install-cli: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(theirs); string(b) != "not a program" {
		t.Fatal("install-cli replaced a file it did not create")
	}
}

func TestInstallCLIAsksForAdminOnlyWhenItMustLink(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	e := newAppEnv(t)
	if err := os.Chmod(e.cliDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.cliDir, 0o755) })
	_, err := e.run("install-cli")
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 {
		t.Fatalf("install-cli on a read-only directory: %v, want exit 3", err)
	}

	writeExec(t, filepath.Join(e.userBin, "bd"), "#!/bin/sh\n")
	writeExec(t, filepath.Join(e.userBin, "b9s"), "#!/bin/sh\n")
	if out, err := e.run("install-cli"); err != nil {
		t.Fatalf("install-cli with nothing to link: %v %q, want no admin prompt", err, out)
	}
}

func TestWebStartsOneServerAndReusesIt(t *testing.T) {
	e := newAppEnv(t)
	start := time.Now()
	url, err := e.run("web")
	if err != nil {
		t.Fatalf("web: %v\n%s", err, url)
	}
	if url != "http://127.0.0.1:7997/pair?token=stub" {
		t.Fatalf("web printed %q", url)
	}
	if again, err := e.run("web"); err != nil || again != url {
		t.Fatalf("second web: %v %q", err, again)
	}
	var starts []string
	for _, c := range e.calls() {
		if strings.HasPrefix(c, "b9s_web") {
			starts = append(starts, c)
		}
	}
	if len(starts) != 1 {
		t.Fatalf("b9s web started %d times: %v", len(starts), starts)
	}
	if !strings.Contains(starts[0], "--listen_127.0.0.1:7997") {
		t.Fatalf("b9s web not on loopback port 7997: %s", starts[0])
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("web waited for its deadline instead of the banner")
	}
}

func TestTerminalCommandSurvivesQuotesInPaths(t *testing.T) {
	e := newAppEnv(t)
	cmd, err := e.run("terminal-cmd")
	if err != nil {
		t.Fatalf("terminal-cmd: %v\n%s", err, cmd)
	}
	// Run the printed command with b9s swapped for pwd, as Terminal would run it.
	probe := strings.Replace(cmd, "exec b9s", "pwd", 1)
	out, err := exec.Command("bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", probe, err, out)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(e.root, "Sample Project"))
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
	if got != want {
		t.Fatalf("terminal-cmd opened %q, want %q", got, want)
	}
}

func TestInstallCLIRefusesToLinkIntoADiskImage(t *testing.T) {
	for _, bin := range []string{
		"/Volumes/B9s 1.4.0/B9s.app/Contents/Resources/bin",
		"/private/var/folders/xy/T/AppTranslocation/0A1B/d/B9s.app/Contents/Resources/bin",
	} {
		e := newAppEnv(t)
		e.extra = []string{"B9S_APP_BIN_DIR=" + bin}
		out, err := e.run("install-cli")
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 4 {
			t.Fatalf("install-cli from %s: %v %q, want exit 4", bin, err, out)
		}
		if entries, _ := os.ReadDir(e.cliDir); len(entries) != 0 {
			t.Fatalf("install-cli from %s linked %v", bin, entries)
		}
	}
}

// osascriptStub answers each dialog with the button named in STUB_ANSWER_<n>,
// in order, and records every script it was handed.
const osascriptStub = `#!/bin/bash
script=$(cat)
printf '%s\n---\n' "$script" >>"$STUB_CALLS.osascript"
n=$(grep -c . "$STUB_CALLS.answers" 2>/dev/null || echo 0)
case $script in
*"display dialog"*)
	n=$((n + 1)); echo x >>"$STUB_CALLS.answers"
	eval "printf '%s\n' \"\${STUB_ANSWER_$n:-}\""
	;;
esac
`

var (
	stubOnce sync.Once
	stubPath string
	stubErr  error
)

// buildStub compiles the app's main executable once per test run.
func buildStub(t *testing.T) string {
	t.Helper()
	stubOnce.Do(func() {
		dir, err := os.MkdirTemp("", "b9s-app-stub")
		if err != nil {
			stubErr = err
			return
		}
		stubPath = filepath.Join(dir, "B9s")
		out, err := exec.Command("go", "build", "-o", stubPath, "./stub").CombinedOutput()
		if err != nil {
			stubErr = fmt.Errorf("go build ./stub: %v\n%s", err, out)
		}
	})
	if stubErr != nil {
		t.Fatal(stubErr)
	}
	return stubPath
}

// launchApp lays out a B9s.app around the real launcher and b9s-app scripts
// and runs it with osascript and open stubbed.
func (e *appEnv) launchApp(answers ...string) (string, error) {
	e.t.Helper()
	app := filepath.Join(filepath.Dir(e.home), "Applications", "B9s.app", "Contents")
	res := filepath.Join(app, "Resources")
	for _, d := range []string{filepath.Join(app, "MacOS"), filepath.Join(res, "bin"), filepath.Join(res, "sample-project", ".beads")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			e.t.Fatal(err)
		}
	}
	copyExec := func(from, to string) {
		b, err := os.ReadFile(from)
		if err != nil {
			e.t.Fatal(err)
		}
		writeExec(e.t, to, string(b))
	}
	copyExec("launcher.sh", filepath.Join(res, "launcher"))
	stub, err := os.ReadFile(buildStub(e.t))
	if err != nil {
		e.t.Fatal(err)
	}
	writeExec(e.t, filepath.Join(app, "MacOS", "B9s"), string(stub))
	copyExec("b9s-app.sh", filepath.Join(res, "b9s-app"))
	copyExec(filepath.Join(e.bin, "bd"), filepath.Join(res, "bin", "bd"))
	copyExec(filepath.Join(e.bin, "b9s"), filepath.Join(res, "bin", "b9s"))
	copyExec(filepath.Join("..", "..", "examples", "sample-project", ".beads", "issues.jsonl"),
		filepath.Join(res, "sample-project", ".beads", "issues.jsonl"))

	stubs := filepath.Join(filepath.Dir(e.home), "stubs")
	_ = os.MkdirAll(stubs, 0o755)
	writeExec(e.t, filepath.Join(stubs, "osascript"), osascriptStub)
	writeExec(e.t, filepath.Join(stubs, "open"), "#!/bin/bash\nprintf 'open %s\\n' \"$*\" >>\"$STUB_CALLS\"\n")

	cmd := exec.Command(filepath.Join(app, "MacOS", "B9s"))
	cmd.Env = []string{
		"HOME=" + e.home,
		"PATH=" + stubs + ":/usr/bin:/bin",
		"B9S_APP_PROJECTS_ROOT=" + e.root,
		"B9S_APP_SUPPORT_DIR=" + filepath.Join(e.home, "support"),
		"B9S_APP_LOG_DIR=" + filepath.Join(e.home, "logs"),
		"B9S_APP_CLI_DIR=" + e.cliDir,
		"B9S_APP_USER_PATH=" + e.userBin + ":" + e.cliDir,
		"B9S_APP_WEB_WAIT_SECONDS=5",
		"STUB_CALLS=" + e.callsLog,
	}
	for i, a := range answers {
		cmd.Env = append(cmd.Env, "STUB_ANSWER_"+string(rune('1'+i))+"="+a)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestLauncherInstallsTheToolsWithoutAskingThenOpensTheBrowser(t *testing.T) {
	e := newAppEnv(t)
	if out, err := e.launchApp("Browser"); err != nil {
		t.Fatalf("first launch: %v\n%s", err, out)
	}
	calls := strings.Join(e.calls(), "\n")
	if !strings.Contains(calls, "open_http://127.0.0.1:7997/pair?token=stub") {
		t.Fatalf("the browser was not opened on the pairing link:\n%s", calls)
	}
	for _, tool := range []string{"b9s", "bd"} {
		if _, err := os.Readlink(filepath.Join(e.cliDir, tool)); err != nil {
			t.Fatalf("launch did not link %s: %v", tool, err)
		}
	}
	scripts, _ := os.ReadFile(e.callsLog + ".osascript")
	if n := strings.Count(string(scripts), "display dialog"); n != 1 {
		t.Fatalf("first launch showed %d dialogs, want only the project one:\n%s", n, scripts)
	}
}

// The bundle's main executable must be a native binary: macOS asks for
// Rosetta before it opens an app whose executable is a script.
func TestStubRunsTheLauncherWithItsArguments(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := filepath.Join(tmp, "B9s's App.app", "Contents")
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(contents, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub, err := os.ReadFile(buildStub(t))
	if err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(contents, "MacOS", "B9s"), string(stub))
	writeExec(t, filepath.Join(contents, "Resources", "launcher"),
		"#!/bin/bash\nprintf '%s|' \"$0\" \"$@\"\n")

	out, err := exec.Command(filepath.Join(contents, "MacOS", "B9s"), "-psn_0_1", "two words").CombinedOutput()
	if err != nil {
		t.Fatalf("stub: %v\n%s", err, out)
	}
	want := filepath.Join(contents, "Resources", "launcher") + "|-psn_0_1|two words|"
	if string(out) != want {
		t.Fatalf("stub ran %q, want %q", out, want)
	}
}
