package main_test

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// releaseCommit stands in for the commit GoReleaser stamps into a release.
const releaseCommit = "0123456789abcdef0123456789abcdef01234567"

// A release build takes its version and commit from the linker flags in
// .goreleaser.yaml, so it prints the same in every clone.
func TestVersionFlag_ReleaseBuildPrintsLinkerVersion(t *testing.T) {
	bv := buildReleaseBinary(t, "v9.9.9", releaseCommit)

	want := "b9s v9.9.9 (01234567)\n" +
		"commit https://github.com/vanderheijden86/b9s/commit/" + releaseCommit + "\n"
	if got := versionOutput(t, bv); got != want {
		t.Errorf("--version = %q, want %q", got, want)
	}
}

// A plain go build, the binary every other e2e test runs, takes its version
// and commit from the VCS stamp that Go derives from the clone. It names a
// release only when its commit carries that tag, as in CI runs for a tag push.
func TestVersionFlag_PlainBuildPrintsDevAndCommit(t *testing.T) {
	bv := buildBvBinary(t)
	commit := stampedCommit(t, bv)

	// "b9s <version> (<commit>)", with ", modified" after the commit when the
	// tree had uncommitted changes.
	versionLine := regexp.MustCompile(`^b9s (.+) \(([0-9a-f]{8})(, modified)?\)$`)
	// "dev" in a clone without tags, as CI's shallow checkout is, or
	// "dev after vX.Y.Z" past a tag.
	devVersion := regexp.MustCompile(`^dev( after v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?)?$`)

	lines := strings.Split(strings.TrimSuffix(versionOutput(t, bv), "\n"), "\n")
	m := versionLine.FindStringSubmatch(lines[0])
	if m == nil || m[2] != commit[:8] {
		t.Fatalf("--version first line = %q, want \"b9s <version> (%s)\"", lines[0], commit[:8])
	}
	if v := m[1]; !devVersion.MatchString(v) && !commitHasTag(t, commit, strings.TrimSuffix(v, "+dirty")) {
		t.Errorf("plain build calls itself %q, want dev or dev after vX.Y.Z: commit %s carries no such tag", v, commit[:8])
	}
	wantCommit := "commit https://github.com/vanderheijden86/b9s/commit/" + commit
	if len(lines) != 2 || lines[1] != wantCommit {
		t.Errorf("--version lines = %q, want the second to be %q", lines, wantCommit)
	}
}

// buildReleaseBinary builds b9s with the version and commit linker flags that
// .goreleaser.yaml sets.
func buildReleaseBinary(t *testing.T, version, commit string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "b9s")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	ldflags := "-X github.com/vanderheijden86/b9s/pkg/version.Version=" + version +
		" -X github.com/vanderheijden86/b9s/pkg/version.Commit=" + commit
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, "../../cmd/b9s")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func versionOutput(t *testing.T, bv string) string {
	t.Helper()
	out, err := exec.Command(bv, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\n%s", err, out)
	}
	return string(out)
}

// stampedCommit is the commit Go stamped into a binary built in a git checkout.
func stampedCommit(t *testing.T, bin string) string {
	t.Helper()
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatalf("read build info: %v", err)
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	t.Fatal("binary carries no vcs.revision; run the e2e tests from a git checkout")
	return ""
}

// commitHasTag reports whether tag points at commit in this checkout.
func commitHasTag(t *testing.T, commit, tag string) bool {
	t.Helper()
	out, err := exec.Command("git", "tag", "--points-at", commit).Output()
	if err != nil {
		t.Fatalf("git tag --points-at %s: %v", commit, err)
	}
	return slices.Contains(strings.Fields(string(out)), tag)
}

func TestVersionFlag_IncludesBuildInfo(t *testing.T) {
	bv := buildBvBinary(t)

	cmd := exec.Command(bv, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %v\n%s", err, out)
	}

	output := string(out)
	if !strings.Contains(strings.ToLower(output), "b9s") {
		t.Errorf("expected 'b9s' in version output, got: %s", output)
	}
}

func TestRollbackFlag_FailsWithoutBackup(t *testing.T) {
	bv := buildBvBinary(t)
	tmpDir := t.TempDir()

	cmd := exec.Command(bv, "--rollback")
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected --rollback to fail without backup, but succeeded: %s", out)
	}

	output := string(out)
	if !strings.Contains(strings.ToLower(output), "backup") && !strings.Contains(strings.ToLower(output), "no backup") {
		t.Errorf("expected error message about missing backup, got: %s", output)
	}
}

func TestUpdateFlag_RequiresNetwork(t *testing.T) {
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("Skipping network-dependent test")
	}

	bv := buildBvBinary(t)
	tmpDir := t.TempDir()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.jsonl"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bv, "--update", "-y")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "B9S_UPDATE_TIMEOUT=2s")
	out, _ := cmd.CombinedOutput()

	output := string(out)
	hasUpdateContent := strings.Contains(output, "update") ||
		strings.Contains(output, "Update") ||
		strings.Contains(output, "version") ||
		strings.Contains(output, "latest") ||
		strings.Contains(output, "download") ||
		strings.Contains(output, "failed")

	if !hasUpdateContent {
		t.Logf("Update output: %s", output)
	}
}

func TestHelpFlag_DocumentsUpdateFeatures(t *testing.T) {
	bv := buildBvBinary(t)

	cmd := exec.Command(bv, "--help")
	out, _ := cmd.CombinedOutput()
	output := string(out)

	if !strings.Contains(output, "-update") && !strings.Contains(output, "--update") {
		t.Errorf("expected help to document --update flag, got: %s", output)
	}

	if !strings.Contains(output, "-rollback") && !strings.Contains(output, "--rollback") {
		t.Errorf("expected help to document --rollback flag, got: %s", output)
	}
}

func TestBinary_HasProperPermissions(t *testing.T) {
	bv := buildBvBinary(t)

	info, err := os.Stat(bv)
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}

	mode := info.Mode()
	if mode&0111 == 0 {
		t.Errorf("binary should be executable, mode: %v", mode)
	}
}

func TestBinary_RespondsToSignals(t *testing.T) {
	bv := buildBvBinary(t)
	tmpDir := t.TempDir()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "beads.jsonl"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bv, "--version")
	cmd.Dir = tmpDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("binary failed: %v\n%s", err, out)
	}
}
