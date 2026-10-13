package onboard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "README.md"), "shop\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

// setUp writes what bd init and bd setup claude leave in a repository.
func setUp(t *testing.T, dir string) {
	t.Helper()
	write(t, filepath.Join(dir, ".beads", "metadata.json"), `{"backend":"dolt","dolt_mode":"embedded"}`)
	write(t, filepath.Join(dir, "AGENTS.md"), "This project uses **bd** (beads) for issue tracking. Run `bd prime`.\n")
	write(t, filepath.Join(dir, ".claude", "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"command":"bd prime --hook-json"}]}]}}`)
}

func count(n int) func(context.Context, string) (int, error) {
	return func(context.Context, string) (int, error) { return n, nil }
}

func done(r Report) map[StepID]bool {
	m := map[StepID]bool{}
	for _, s := range r.Steps {
		m[s.ID] = s.Done
	}
	return m
}

func TestCheckReportsEveryStepInOrder(t *testing.T) {
	r := Check(context.Background(), repo(t), Claude, Options{CountIssues: count(0)})
	var ids []StepID
	for _, s := range r.Steps {
		ids = append(ids, s.ID)
	}
	want := []StepID{StepGit, StepBeads, StepInstructions, StepHooks, StepIssues, StepCommitted}
	if strings.Join(stepStrings(ids), ",") != strings.Join(stepStrings(want), ",") {
		t.Fatalf("steps = %v, want %v", ids, want)
	}
	if r.Complete {
		t.Fatal("a bare repository is not complete")
	}
}

func stepStrings(ids []StepID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func TestCheckBareRepository(t *testing.T) {
	got := done(Check(context.Background(), repo(t), Claude, Options{CountIssues: count(0)}))
	if !got[StepGit] {
		t.Error("git step should be done in a git repository")
	}
	for _, id := range []StepID{StepBeads, StepInstructions, StepHooks, StepIssues, StepCommitted} {
		if got[id] {
			t.Errorf("%s should not be done in a bare repository", id)
		}
	}
}

func TestCheckNotARepository(t *testing.T) {
	r := Check(context.Background(), t.TempDir(), Claude, Options{CountIssues: count(0)})
	if done(r)[StepGit] {
		t.Fatal("a plain folder is not a git repository")
	}
}

func TestCheckSetUpButUncommitted(t *testing.T) {
	dir := repo(t)
	setUp(t, dir)
	got := done(Check(context.Background(), dir, Claude, Options{CountIssues: count(3)}))
	for _, id := range []StepID{StepGit, StepBeads, StepInstructions, StepHooks, StepIssues} {
		if !got[id] {
			t.Errorf("%s should be done", id)
		}
	}
	if got[StepCommitted] {
		t.Error("uncommitted setup files must not count as committed")
	}
}

func TestCheckCompleteAfterCommitOnBranch(t *testing.T) {
	dir := repo(t)
	git(t, dir, "checkout", "-qb", "chore/beads-setup")
	setUp(t, dir)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "chore: set up beads")
	r := Check(context.Background(), dir, Claude, Options{CountIssues: count(4)})
	if !r.Complete {
		t.Fatalf("expected complete, got %+v", r.Steps)
	}
	for _, s := range r.Steps {
		if s.ID == StepCommitted && !strings.Contains(s.Detail, "chore/beads-setup") {
			t.Errorf("committed detail %q should name the branch", s.Detail)
		}
		if s.ID == StepIssues && !strings.Contains(s.Detail, "4") {
			t.Errorf("issues detail %q should give the count", s.Detail)
		}
	}
}

func TestCheckHooksDependOnTheAgent(t *testing.T) {
	dir := repo(t)
	setUp(t, dir)
	if done(Check(context.Background(), dir, Codex, Options{CountIssues: count(1)}))[StepHooks] {
		t.Error("Claude hooks must not count for Codex")
	}
	write(t, filepath.Join(dir, ".codex", "hooks.json"), `{"SessionStart":[{"command":"bd prime"}]}`)
	if !done(Check(context.Background(), dir, Codex, Options{CountIssues: count(1)}))[StepHooks] {
		t.Error("Codex hooks should count once .codex/hooks.json runs bd prime")
	}
	write(t, filepath.Join(dir, ".cursor", "rules", "beads.mdc"), "Use bd for issues.\n")
	if !done(Check(context.Background(), dir, Cursor, Options{CountIssues: count(1)}))[StepHooks] {
		t.Error("Cursor rules that mention bd should count")
	}
}

func TestCheckOtherAgentNeedsNoHooks(t *testing.T) {
	dir := repo(t)
	setUp(t, dir)
	_ = os.RemoveAll(filepath.Join(dir, ".claude"))
	if !done(Check(context.Background(), dir, Other, Options{CountIssues: count(1)}))[StepHooks] {
		t.Error("an agent without a bd recipe has no hooks to wait for")
	}
}

func TestCheckInstructionsInClaudeMD(t *testing.T) {
	dir := repo(t)
	write(t, filepath.Join(dir, "CLAUDE.md"), "<!-- BEGIN BEADS INTEGRATION -->\nRun `bd prime`.\n")
	if !done(Check(context.Background(), dir, Claude, Options{CountIssues: count(0)}))[StepInstructions] {
		t.Error("CLAUDE.md with the beads block should count as instructions")
	}
	write(t, filepath.Join(dir, "CLAUDE.md"), "Build with make. Run the tests before you commit.\n")
	if done(Check(context.Background(), dir, Claude, Options{CountIssues: count(0)}))[StepInstructions] {
		t.Error("a CLAUDE.md that never mentions bd must not count")
	}
}

func TestCheckCountErrorLeavesIssuesOpen(t *testing.T) {
	dir := repo(t)
	setUp(t, dir)
	r := Check(context.Background(), dir, Claude, Options{CountIssues: func(context.Context, string) (int, error) {
		return 0, errors.New("database locked")
	}})
	for _, s := range r.Steps {
		if s.ID == StepIssues && (s.Done || !strings.Contains(s.Detail, "database locked")) {
			t.Errorf("issues step = %+v, want not done with the error", s)
		}
	}
}

func TestParseAgent(t *testing.T) {
	for in, want := range map[string]Agent{"": Claude, "claude": Claude, "Claude-Code": Claude, "codex": Codex, "cursor": Cursor, "other": Other} {
		got, err := ParseAgent(in)
		if err != nil || got != want {
			t.Errorf("ParseAgent(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseAgent("vim"); err == nil {
		t.Error("an unknown agent should be an error")
	}
}

func TestBriefNamesTheRightCommands(t *testing.T) {
	dir := repo(t)
	b := Brief(dir, Claude, Versions{B9s: "1.3.4", Bd: "1.3.1"}, Report{})
	for _, want := range []string{dir, "b9s 1.3.4", "bd 1.3.1", "bd init --non-interactive", "bd bootstrap --yes", "bd setup claude", "b9s onboard --check", "chore/beads-setup", "Ask"} {
		if !strings.Contains(b, want) {
			t.Errorf("brief misses %q:\n%s", want, b)
		}
	}
	if o := Brief(dir, Other, Versions{}, Report{}); strings.Contains(o, "bd setup other") || !strings.Contains(o, "bd setup --list") {
		t.Errorf("brief for another agent should point at bd setup --list:\n%s", o)
	}
}

func TestBriefSkipsInitWhenBeadsExists(t *testing.T) {
	dir := repo(t)
	setUp(t, dir)
	r := Check(context.Background(), dir, Claude, Options{CountIssues: count(0)})
	b := Brief(dir, Claude, Versions{}, r)
	if !strings.Contains(b, "already has a Beads project") {
		t.Errorf("brief should say the project exists:\n%s", b)
	}
}

func TestPromptIsShortAndNamesTheCommand(t *testing.T) {
	p := Prompt(Codex)
	if n := strings.Count(p, "\n"); n > 2 {
		t.Errorf("prompt has %d lines, want at most 3:\n%s", n+1, p)
	}
	if !strings.Contains(p, "b9s onboard --agent codex") {
		t.Errorf("prompt should name the command:\n%s", p)
	}
}

func TestPlanPromptCarriesTheGoal(t *testing.T) {
	p := PlanPrompt("Pay with iDEAL", PlanOptions{PlanOnly: true, AskFirst: true, Due: "2026-11-27"})
	for _, want := range []string{"Pay with iDEAL", "epic", "Do not write code", "Ask me", "2026-11-27"} {
		if !strings.Contains(p, want) {
			t.Errorf("plan prompt misses %q:\n%s", want, p)
		}
	}
	if p := PlanPrompt("x", PlanOptions{}); strings.Contains(p, "Do not write code") || strings.Contains(p, "Ask me the open") {
		t.Errorf("unchecked options must stay out:\n%s", p)
	}
}

func TestTeammateMessage(t *testing.T) {
	m := TeammateMessage("mobile-app", Claude)
	for _, want := range []string{"mobile-app", "b9s onboard --agent claude", "brew install"} {
		if !strings.Contains(m, want) {
			t.Errorf("message misses %q:\n%s", want, m)
		}
	}
}

func TestFindReposSortsByRecentUseAndFlagsBeads(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, age time.Duration, beads bool) {
		dir := filepath.Join(root, rel)
		write(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")
		if beads {
			write(t, filepath.Join(dir, ".beads", "metadata.json"), "{}")
		}
		when := time.Now().Add(-age)
		_ = os.Chtimes(filepath.Join(dir, ".git"), when, when)
	}
	mk("old", 48*time.Hour, false)
	mk("new", time.Hour, true)
	mk("group/nested", 2*time.Hour, false)
	mk("group/nested/node_modules/dep", 0, false) // inside a repo: skipped
	write(t, filepath.Join(root, "notes", "a.txt"), "x")
	repos := FindRepos([]string{root, filepath.Join(root, "missing")}, 20)
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "new,nested,old" {
		t.Fatalf("repos = %v, want new,nested,old", names)
	}
	if !repos[0].HasBeads || repos[2].HasBeads {
		t.Errorf("HasBeads wrong: %+v", repos)
	}
}

func TestPlanDirName(t *testing.T) {
	for in, want := range map[string]string{"Q1 roadmap": "q1-roadmap", "  Café Plan!! ": "caf-plan", "../etc": "etc"} {
		if got := PlanDirName(in); got != want {
			t.Errorf("PlanDirName(%q) = %q, want %q", in, got, want)
		}
	}
	if PlanDirName("!!!") != "" {
		t.Error("a name with no letters gives no folder")
	}
}

// bd init commits its files on the current branch, so a brief that creates
// the branch afterwards lands the setup on main.
func TestBriefCreatesTheBranchBeforeBdInit(t *testing.T) {
	b := Brief(t.TempDir(), Claude, Versions{}, Report{})
	branch, init := strings.Index(b, "git switch -c chore/beads-setup"), strings.Index(b, "bd init --non-interactive")
	if branch < 0 || init < 0 || branch > init {
		t.Fatalf("branch at %d, bd init at %d:\n%s", branch, init, b)
	}
}
