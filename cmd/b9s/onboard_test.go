package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/vanderheijden86/b9s/internal/onboard"
)

func TestRunOnboardPrintsTheBrief(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := runOnboard([]string{"--agent", "codex", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "bd setup codex") || !strings.Contains(out.String(), dir) {
		t.Fatalf("brief:\n%s", out.String())
	}
}

func TestRunOnboardCheckExitsNonZeroUntilComplete(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", "-q", dir).Run(); err != nil {
		t.Skip("git not available")
	}
	var out, errOut bytes.Buffer
	if code := runOnboard([]string{dir, "--check"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d, want 1: %s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "[x] Git repository") || !strings.Contains(out.String(), "[ ] Beads project") {
		t.Fatalf("checklist:\n%s", out.String())
	}
}

func TestRunOnboardJSON(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	runOnboard([]string{"--json", dir}, &out, &errOut)
	var r onboard.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if r.Agent != onboard.Claude || len(r.Steps) != 6 || r.Complete {
		t.Fatalf("report = %+v", r)
	}
}

func TestRunOnboardRejectsUnknownAgent(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runOnboard([]string{"--agent", "vim"}, &out, &errOut); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

func TestRunOnboardPrompt(t *testing.T) {
	var out, errOut bytes.Buffer
	runOnboard([]string{"--prompt", "--agent", "cursor"}, &out, &errOut)
	if !strings.Contains(out.String(), "b9s onboard --agent cursor") {
		t.Fatalf("prompt:\n%s", out.String())
	}
}
