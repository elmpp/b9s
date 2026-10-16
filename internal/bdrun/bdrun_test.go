package bdrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// writeFakeBd installs a shell script named bd in a fresh PATH-only
// directory that echoes its args (one per line, so a caller can tell a
// space-containing argument was received intact) and exits with code.
func writeFakeBd(t *testing.T, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\nexit " + itoa(code) + "\n"
	path := filepath.Join(dir, "bd")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func TestResolve_FindsBdOnPath(t *testing.T) {
	binDir := writeFakeBd(t, 0)
	t.Setenv("PATH", binDir)

	path, ok := Resolve()

	if !ok {
		t.Fatal("Resolve() ok = false, want true")
	}
	if filepath.Dir(path) != binDir {
		t.Errorf("Resolve() path = %q, want it under %q", path, binDir)
	}
}

func TestResolve_NotFoundWhenBdIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, ok := Resolve()

	if ok {
		t.Fatal("Resolve() ok = true, want false: no bd on PATH")
	}
}

func TestRun_PassesEachArgIntact(t *testing.T) {
	binDir := writeFakeBd(t, 0)
	bdPath := filepath.Join(binDir, "bd")
	dir := t.TempDir()

	out, err := Run(context.Background(), bdPath, dir, "comments", "add", "bd-1", "line one\nline two with spaces")

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "comments\nadd\nbd-1\nline one\nline two with spaces"
	if out != want {
		t.Errorf("Run output = %q, want %q", out, want)
	}
}

func TestRun_RunsInDir(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\npwd\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	out, err := Run(context.Background(), filepath.Join(binDir, "bd"), dir, "list")

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedOut, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	if err != nil {
		t.Fatal(err)
	}
	if resolvedOut != resolvedDir {
		t.Errorf("Run pwd = %q, want %q", resolvedOut, resolvedDir)
	}
}

func TestRun_ReturnsErrorOnNonZeroExit(t *testing.T) {
	binDir := writeFakeBd(t, 1)
	bdPath := filepath.Join(binDir, "bd")

	_, err := Run(context.Background(), bdPath, t.TempDir(), "close", "bd-1")

	if err == nil {
		t.Fatal("Run() err = nil, want an error for exit code 1")
	}
}

// TestRun_TimesOutOnAHungProcess proves the deadline itself, not only the
// process-group kill below: a fake bd that sleeps past ctx's deadline must
// return a clear timeout error within the deadline plus WaitDelay, never
// hang for the sleep's full duration.
func TestRun_TimesOutOnAHungProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	binDir := t.TempDir()
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Run(ctx, filepath.Join(binDir, "bd"), t.TempDir(), "list")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run() err = nil, want a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want it to name the timeout clearly", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %v, want it bounded by the 200ms deadline plus WaitDelay, not the 30s sleep", elapsed)
	}
}

// TestRun_KillsWholeProcessGroupOnTimeout proves the process-group half of
// the fix: a fake bd that forks a grandchild holding stdout open (a
// backgrounded sh that never exits) must not block Run past its deadline.
// Killing only the direct bd process would leave that grandchild holding the
// CombinedOutput pipe open, and Wait would then block on it indefinitely.
func TestRun_KillsWholeProcessGroupOnTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	binDir := t.TempDir()
	// The backgrounded child inherits bd's stdout pipe and outlives bd
	// itself; only a process-group kill reaches it.
	script := "#!/bin/sh\n(sleep 2) &\nsleep 2\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Run(ctx, filepath.Join(binDir, "bd"), t.TempDir(), "list")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run() err = nil, want a timeout error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %v, want the grandchild's pipe hold to be bounded by WaitDelay, not the 3s sleep", elapsed)
	}
}

// TestRun_CancelledBeforeStart_ReturnsCancelledError guards the error-shaping
// branch specifically: errors.Is(ctx.Err(), context.DeadlineExceeded) must be
// the check Run uses for a timeout, so a context that was already cancelled
// before bd ever started (not a deadline) is reported as "bd cancelled", not
// "bd timed out".
func TestRun_CancelledBeforeStart_ReturnsCancelledError(t *testing.T) {
	binDir := writeFakeBd(t, 0)
	bdPath := filepath.Join(binDir, "bd")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Run(ctx, bdPath, t.TempDir(), "list")

	if err == nil {
		t.Fatal("Run() err = nil, want an error for an already-cancelled context")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want cancellation (not a deadline) to not claim a timeout", err)
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %v, want it to say the run was cancelled", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want errors.Is(err, context.Canceled)", err)
	}
}

// TestRun_CancelledDuringRun_ReturnsCancelledError guards the same "bd
// cancelled" shaping when the caller cancels ctx while bd is actually
// running, not only when ctx was already done before Start ever ran.
func TestRun_CancelledDuringRun_ReturnsCancelledError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	binDir := t.TempDir()
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	_, err := Run(ctx, filepath.Join(binDir, "bd"), t.TempDir(), "list")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run() err = nil, want an error for a cancellation during the run")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want cancellation (not a deadline) to not claim a timeout", err)
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %v, want it to say the run was cancelled", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want errors.Is(err, context.Canceled)", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %v, want it bounded by the cancellation plus WaitDelay, not the 30s sleep", elapsed)
	}
}

// The lingering-child tests need a ctx that expires after the fake bd exits
// and before Run returns, which is WaitDelay after that exit because a child
// still holds the pipe. A wall-clock deadline cannot promise the first half:
// starting a fresh temp-dir script takes 150-200ms on macOS and seconds
// under a parallel full-suite run, and a deadline that lands before the exit
// kills bd and turns the test into a timeout test (bd-iylb, bd-j545,
// bd-v3vs). So the deadline is bd's own exit: the script writes its pid to a
// marker file, and the context expires once that pid is gone.

// exitMarkerContext expires when done is closed, and reports
// context.DeadlineExceeded from then on, like a deadline that elapsed.
type exitMarkerContext struct {
	context.Context
	done chan struct{}
}

func (c *exitMarkerContext) Done() <-chan struct{} { return c.done }

func (c *exitMarkerContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// contextExpiringWhenScriptExits returns a context that expires once the
// process whose pid the script wrote to marker has exited, and the script
// to prepend: `echo $$ > marker`. Reading the pid and polling kill(pid, 0)
// is bounded by markerWait so a script that never starts fails the test
// rather than hanging it.
func contextExpiringWhenScriptExits(t *testing.T, marker string) context.Context {
	t.Helper()
	const markerWait = 20 * time.Second
	ctx := &exitMarkerContext{Context: context.Background(), done: make(chan struct{})}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		deadline := time.Now().Add(markerWait)
		pid := 0
		for time.Now().Before(deadline) {
			select {
			case <-stop:
				return
			default:
			}
			if pid == 0 {
				if b, err := os.ReadFile(marker); err == nil {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
				}
			}
			// Run reaps bd the moment it exits, so a signal-0 probe fails for a pid that is gone.
			if pid != 0 && syscall.Kill(pid, 0) != nil {
				close(ctx.done)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Errorf("fake bd never wrote its pid to %s within %s", marker, markerWait)
	}()
	return ctx
}

func pidMarker(t *testing.T) (string, string) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "pid")
	return marker, "echo $$ > " + marker + "\n"
}

// TestRun_QuickNonZeroExitWithLingeringSameGroupChild_ReturnsExitError guards
// the other half of trusting bd's own exit over ctx: bd itself exits 3 well
// within the deadline, but a child it backgrounds keeps the stdout pipe open
// past that deadline and past WaitDelay, so ctx.Err() reads DeadlineExceeded
// by the time CombinedOutput returns even though bd already chose its own
// exit code. Run must report bd's exit, not a timeout.
func TestRun_QuickNonZeroExitWithLingeringSameGroupChild_ReturnsExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	binDir := t.TempDir()
	marker, writePid := pidMarker(t)
	script := "#!/bin/sh\n" + writePid + "sleep 3 &\nprintf 'boom'\nexit 3\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := contextExpiringWhenScriptExits(t, marker)

	start := time.Now()
	out, err := Run(ctx, filepath.Join(binDir, "bd"), t.TempDir(), "list")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run() err = nil, want bd's own exit status 3")
	}
	if strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "cancelled") {
		t.Errorf("err = %v, want bd's own exit, not a timeout or cancellation", err)
	}
	if !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("err = %v, want it to contain %q", err, "exit status 3")
	}
	if out != "boom" {
		t.Errorf("Run() out = %q, want %q", out, "boom")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %v, want it bounded by WaitDelay, not the 3s sleep", elapsed)
	}
}

// TestRun_QuickSuccessWithLingeringSameGroupChild_ReturnsOutputNoError guards
// against reporting a timeout from a stale ctx.Err() check alone: bd itself
// exits successfully well within the deadline, but a child it backgrounds (in
// bd's own process group, the common case) keeps the stdout pipe open past
// that deadline and past WaitDelay. By the time CombinedOutput returns,
// ctx.Err() is already DeadlineExceeded even though the run succeeded, so Run
// must judge success from runErr, not from ctx.
func TestRun_QuickSuccessWithLingeringSameGroupChild_ReturnsOutputNoError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	binDir := t.TempDir()
	marker, writePid := pidMarker(t)
	script := "#!/bin/sh\n" + writePid + "sleep 3 &\nprintf 'written'\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := contextExpiringWhenScriptExits(t, marker)

	start := time.Now()
	out, err := Run(ctx, filepath.Join(binDir, "bd"), t.TempDir(), "list")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run() err = %v, want nil: bd exited 0, only a lingering child held stdout open", err)
	}
	if out != "written" {
		t.Errorf("Run() out = %q, want %q", out, "written")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %v, want it bounded by WaitDelay, not the 3s sleep", elapsed)
	}
}

// TestRun_ErrWaitDelay_QuickSuccessWithLingeringDetachedChild_ReturnsOutputNoError
// guards the exec.ErrWaitDelay branch specifically: it means bd itself
// exited successfully and only a lingering descendant kept a pipe open,
// never a real failure, whether or not ctx has a deadline at all. The child
// detaches into its own process group before bd exits, so Run's
// process-group kill (which only fires on cancellation, and never reaches a
// different group in any case) plays no part in this scenario: WaitDelay's
// forced pipe close is what unblocks Wait.
func TestRun_ErrWaitDelay_QuickSuccessWithLingeringDetachedChild_ReturnsOutputNoError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script needs a POSIX shell")
	}
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl not on PATH: needed to detach the lingering child into its own process group")
	}
	binDir := t.TempDir()
	marker, writePid := pidMarker(t)
	script := "#!/bin/sh\n" + writePid + "perl -e 'setpgrp(0,0); exec(\"sleep\",\"3\")' &\nprintf 'written'\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	bdPath := filepath.Join(binDir, "bd")

	cases := []struct {
		name string
		ctx  func(t *testing.T) context.Context
	}{
		{"background", func(*testing.T) context.Context { return context.Background() }},
		{"with deadline", func(t *testing.T) context.Context { return contextExpiringWhenScriptExits(t, marker) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case runs the script once, so each wants its own marker.
			if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			ctx := tc.ctx(t)

			start := time.Now()
			out, err := Run(ctx, bdPath, t.TempDir(), "list")
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("Run() err = %v, want nil", err)
			}
			if out != "written" {
				t.Errorf("Run() out = %q, want %q", out, "written")
			}
			if elapsed > 5*time.Second {
				t.Fatalf("Run took %v, want it bounded by WaitDelay, not the 3s sleep", elapsed)
			}
		})
	}
}
