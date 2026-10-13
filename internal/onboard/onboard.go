// Package onboard checks whether a repository is set up for Beads and B9s,
// and writes the brief an AI agent or a developer follows to set it up.
//
// B9s never edits a repository itself. The brief hands the work to the
// person's own agent, and Check reads the folder to see how far it got, so
// the app and the agent agree on one definition of "done".
package onboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vanderheijden86/b9s/internal/bdrun"
)

// Agent names the AI coding agent the setup is written for. The value is the
// argument bd setup takes, except Other, which has no bd recipe.
type Agent string

const (
	Claude Agent = "claude"
	Codex  Agent = "codex"
	Cursor Agent = "cursor"
	Other  Agent = "other"
)

// Agents lists every agent in the order the app offers them.
var Agents = []Agent{Claude, Codex, Cursor, Other}

// ParseAgent accepts the agent names people type; empty means Claude Code.
func ParseAgent(s string) (Agent, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "claude", "claude-code", "claude code", "claudecode":
		return Claude, nil
	case "codex":
		return Codex, nil
	case "cursor":
		return Cursor, nil
	case "other":
		return Other, nil
	}
	return "", fmt.Errorf("unknown agent %q: use claude, codex, cursor or other", s)
}

// Label is the agent's name as the app shows it.
func (a Agent) Label() string {
	switch a {
	case Claude:
		return "Claude Code"
	case Codex:
		return "Codex"
	case Cursor:
		return "Cursor"
	}
	return "another AI"
}

// StepID identifies one step of the setup checklist.
type StepID string

const (
	StepGit          StepID = "git"
	StepBeads        StepID = "beads"
	StepInstructions StepID = "instructions"
	StepHooks        StepID = "hooks"
	StepIssues       StepID = "issues"
	StepCommitted    StepID = "committed"
)

// Step is one line of the checklist.
type Step struct {
	ID     StepID `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Detail string `json:"detail"`
}

// Report is the checklist for one folder. Complete holds when every step is done.
type Report struct {
	Dir      string `json:"dir"`
	Agent    Agent  `json:"agent"`
	Steps    []Step `json:"steps"`
	Complete bool   `json:"complete"`
}

func (r Report) done(id StepID) bool {
	for _, s := range r.Steps {
		if s.ID == id {
			return s.Done
		}
	}
	return false
}

// Options replaces the commands Check runs, for tests.
type Options struct {
	// CountIssues returns how many issues the Beads project in dir holds.
	// Nil runs bd count.
	CountIssues func(ctx context.Context, dir string) (int, error)
	// Git is the git binary; empty finds git on PATH.
	Git string
}

// setupPaths are the files bd init and bd setup write. The committed step
// looks only at these: issue data under .beads changes with every write and
// would keep the step from ever settling.
var setupPaths = []string{".beads/metadata.json", ".beads/config.yaml", ".beads/.gitignore", "AGENTS.md", "CLAUDE.md", ".claude", ".codex", ".cursor", ".agents", ".gitignore"}

var mentionsBd = regexp.MustCompile(`(?i)\bbd\b|\bbeads\b`)

// Check reads dir and reports each setup step. It never changes the folder.
func Check(ctx context.Context, dir string, agent Agent, opts Options) Report {
	r := Report{Dir: dir, Agent: agent}
	add := func(id StepID, title string, done bool, detail string) {
		r.Steps = append(r.Steps, Step{ID: id, Title: title, Done: done, Detail: detail})
	}

	isRepo := exists(filepath.Join(dir, ".git"))
	add(StepGit, "Git repository", isRepo, pick(isRepo, "Found .git", "Not a git repository"))

	hasBeads := exists(filepath.Join(dir, ".beads", "metadata.json"))
	add(StepBeads, "Beads project", hasBeads, pick(hasBeads, ".beads/ is set up", "No .beads/ folder yet"))

	file := firstMatch(dir, mentionsBd, "AGENTS.md", "CLAUDE.md")
	add(StepInstructions, "AI instructions", file != "", pick(file != "", file+" explains bd", "AGENTS.md does not mention bd yet"))

	hooks, detail := checkHooks(dir, agent)
	add(StepHooks, "Agent hooks", hooks, detail)

	switch {
	case !hasBeads:
		add(StepIssues, "First issues", false, "Waits for the Beads project")
	default:
		count := opts.CountIssues
		if count == nil {
			count = bdCount
		}
		n, err := count(ctx, dir)
		switch {
		case err != nil:
			add(StepIssues, "First issues", false, "bd count failed: "+err.Error())
		case n == 0:
			add(StepIssues, "First issues", false, "No issues yet")
		default:
			add(StepIssues, "First issues", true, fmt.Sprintf("%d %s", n, pick(n == 1, "issue", "issues")))
		}
	}

	committed, detail := checkCommitted(ctx, dir, opts.Git, isRepo, hasBeads)
	add(StepCommitted, "Committed", committed, detail)

	r.Complete = true
	for _, s := range r.Steps {
		r.Complete = r.Complete && s.Done
	}
	return r
}

func checkHooks(dir string, agent Agent) (bool, string) {
	switch agent {
	case Claude:
		for _, f := range []string{".claude/settings.json", ".claude/settings.local.json"} {
			if fileMatches(filepath.Join(dir, f), regexp.MustCompile(`bd prime`)) {
				return true, f + " runs bd prime"
			}
		}
		return false, "No bd prime hook in .claude/settings.json"
	case Codex:
		if f := treeMatch(dir, ".codex", regexp.MustCompile(`bd prime`)); f != "" {
			return true, f + " runs bd prime"
		}
		return false, "No bd prime hook in .codex/"
	case Cursor:
		if f := treeMatch(dir, ".cursor", mentionsBd); f != "" {
			return true, f + " explains bd"
		}
		return false, "No bd rule in .cursor/"
	}
	return true, "This agent reads AGENTS.md; no hook to install"
}

func checkCommitted(ctx context.Context, dir, gitBin string, isRepo, hasBeads bool) (bool, string) {
	if !isRepo || !hasBeads {
		return false, "Waits for the Beads project"
	}
	if gitBin == "" {
		gitBin = "git"
	}
	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, gitBin, append([]string{"-C", dir}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	branch, _ := run("rev-parse", "--abbrev-ref", "HEAD")
	tracked, err := run("ls-files", "--", ".beads/metadata.json")
	if err != nil {
		return false, "git ls-files failed: " + err.Error()
	}
	if tracked == "" {
		return false, "Not committed yet"
	}
	dirty, err := run(append([]string{"status", "--porcelain", "--"}, setupPaths...)...)
	if err != nil {
		return false, "git status failed: " + err.Error()
	}
	if dirty != "" {
		return false, "Changes not committed yet on " + branch
	}
	return true, "Committed on " + branch
}

func bdCount(ctx context.Context, dir string) (int, error) {
	bd, ok := bdrun.Resolve()
	if !ok {
		return 0, errors.New("bd is not installed")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, _, err := bdrun.Output(ctx, bd, dir, nil, "count")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, errors.New("bd count printed nothing")
	}
	return strconv.Atoi(fields[len(fields)-1])
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}

func fileMatches(path string, re *regexp.Regexp) bool {
	b, err := os.ReadFile(path)
	return err == nil && re.Match(b)
}

func firstMatch(dir string, re *regexp.Regexp, names ...string) string {
	for _, n := range names {
		if fileMatches(filepath.Join(dir, n), re) {
			return n
		}
	}
	return ""
}

// treeMatch returns the first file under dir/sub, relative to dir, whose text
// matches re. Agent folders hold a handful of small files.
func treeMatch(dir, sub string, re *regexp.Regexp) string {
	found := ""
	_ = filepath.WalkDir(filepath.Join(dir, sub), func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		if !d.IsDir() && fileMatches(path, re) {
			found, _ = filepath.Rel(dir, path)
			return filepath.SkipAll
		}
		return nil
	})
	return filepath.ToSlash(found)
}

// BdVersion asks the installed bd for its version, or returns "".
func BdVersion(ctx context.Context) string {
	bd, ok := bdrun.Resolve()
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _, err := bdrun.Output(ctx, bd, "", nil, "version")
	if err != nil {
		return ""
	}
	return versionPattern.FindString(string(out))
}

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+[0-9A-Za-z.+-]*`)

// Versions are the tool versions the brief was written for.
type Versions struct {
	B9s string
	Bd  string
}

// Brief is the step-by-step setup an agent or a developer follows in dir.
// r is the current Check result; an empty Report means nothing is known yet.
func Brief(dir string, agent Agent, v Versions, r Report) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("# Set up Beads for B9s")
	w("")
	w("Repository: %s", dir)
	if v.B9s != "" || v.Bd != "" {
		w("Written for b9s %s and bd %s.", orUnknown(v.B9s), orUnknown(v.Bd))
	}
	w("")
	w("You are setting up Beads (the `bd` issue tracker) in this repository, so the")
	w("owner can follow the work in B9s. Work through the steps in order. Do not")
	w("change application code. Ask the owner when a step needs a decision.")
	w("")
	w("1. Check the repository and start a branch.")
	w("   It must be a git repository. Run `git status`. If there are uncommitted")
	w("   changes, ask the owner whether to continue before you touch anything.")
	w("   Then run `git switch -c chore/beads-setup`. bd init commits its files")
	w("   on the current branch, so the branch must exist first.")
	w("")
	if r.done(StepBeads) {
		w("2. This folder already has a Beads project (.beads/metadata.json).")
		w("   Do not run bd init. Run `bd list`. If it fails because the database is")
		w("   missing, run `bd bootstrap --yes`. It never deletes data.")
	} else {
		w("2. Create the Beads project.")
		w("   Run `bd init --non-interactive`. If .beads/ exists but bd cannot open")
		w("   it, run `bd bootstrap --yes` instead. It never deletes data.")
	}
	w("")
	if agent == Other {
		w("3. Connect your agent.")
		w("   Run `bd setup --list` and install the recipe for your agent, if there is")
		w("   one. Make sure AGENTS.md tells agents to use bd for all task tracking.")
	} else {
		w("3. Connect %s.", agent.Label())
		w("   Run `bd setup %s`. It adds the instructions and the hook that loads bd", agent)
		w("   context at the start of each session. Running it twice is safe.")
	}
	w("")
	w("4. Propose the first issues.")
	w("   Read the README, the open TODOs and the recent commits. Propose 3 to 8")
	w("   issues: an epic for the main piece of ongoing work, with tasks under it.")
	w("   Ask the owner which to create, then create them with `bd create`")
	w("   (always with --description). Never invent deadlines or owners.")
	w("")
	w("5. Check the result.")
	w("   Run `b9s onboard --check --agent %s`. Fix every step it reports as open,", agent)
	w("   except Committed, which the next step handles.")
	w("")
	w("6. Commit.")
	w("   On chore/beads-setup, commit what bd changed after its own commit")
	w("   (AGENTS.md, CLAUDE.md, .beads/ and the agent folders) with the message")
	w("   \"chore: set up Beads issue tracking\". Run the check again.")
	w("   Ask the owner before you push or open a pull request.")
	w("")
	w("When `b9s onboard --check` exits 0, tell the owner the project is ready.")
	w("B9s shows the same checklist and adds the project by itself.")
	return b.String()
}

func orUnknown(s string) string { return pick(s == "", "(unknown)", s) }

// Prompt is the short message the owner pastes into their agent. It points
// at the brief instead of carrying it, so the steps match the installed b9s.
func Prompt(agent Agent) string {
	return "Set up Beads issue tracking in this repository so I can follow the work in B9s.\n" +
		fmt.Sprintf("Run `b9s onboard --agent %s` and follow the brief it prints, step by step.\n", agent) +
		"Ask me before you push or open a pull request."
}

// PlanOptions are the choices on the plan form.
type PlanOptions struct {
	PlanOnly bool
	AskFirst bool
	Due      string
}

// PlanPrompt turns a goal in plain words into a planning prompt for an agent.
func PlanPrompt(goal string, o PlanOptions) string {
	var b strings.Builder
	b.WriteString("Read this repository, then plan the goal below in Beads.\n")
	b.WriteString("Make one epic, a feature per capability, and tasks under them.\n")
	if o.AskFirst {
		b.WriteString("Ask me the open questions before you create anything.\n")
	}
	if o.PlanOnly {
		b.WriteString("Do not write code. Show me the plan with bd show.\n")
	}
	if due := strings.TrimSpace(o.Due); due != "" {
		fmt.Fprintf(&b, "It must be done by %s. Give the epic that due date.\n", due)
	}
	fmt.Fprintf(&b, "\nGoal: %s\n", strings.TrimSpace(goal))
	return b.String()
}

// TeammateMessage asks a developer to run the setup for the owner.
func TeammateMessage(repo string, agent Agent) string {
	return fmt.Sprintf(`Hi! I want to follow our work in B9s. Could you set up Beads in %s? It takes about five minutes.

1. Install: brew install vanderheijden86/tap/b9s
2. In the repo, give your AI this, or run the steps yourself:
   b9s onboard --agent %s
3. Push the branch and tell me when it is merged.

Thanks!`, repo, agent)
}

// Repo is a git repository found on this computer.
type Repo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	HasBeads bool      `json:"hasBeads"`
	Modified time.Time `json:"modified"`
}

const findDepth = 3

var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "Library": true, "Applications": true, "Pictures": true, "Music": true, "Movies": true}

// FindRepos lists git repositories up to three folders below each root, most
// recently changed first. Missing roots are skipped.
func FindRepos(roots []string, limit int) []Repo {
	seen := map[string]bool{}
	var repos []Repo
	visited := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if visited > 20000 {
			return
		}
		visited++
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if !seen[dir] {
				seen[dir] = true
				repos = append(repos, Repo{
					Name:     filepath.Base(dir),
					Path:     dir,
					HasBeads: exists(filepath.Join(dir, ".beads", "metadata.json")),
					Modified: info.ModTime(),
				})
			}
			return
		}
		if depth >= findDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || skipDirs[e.Name()] {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth+1)
		}
	}
	for _, root := range roots {
		if exists(root) {
			walk(filepath.Clean(root), 0)
		}
	}
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].Modified.After(repos[j].Modified) })
	if limit > 0 && len(repos) > limit {
		repos = repos[:limit]
	}
	return repos
}

// DefaultRoots are the folders people usually keep code in, or the list in
// B9S_REPO_ROOTS, separated like PATH.
func DefaultRoots() []string {
	if env := os.Getenv("B9S_REPO_ROOTS"); env != "" {
		return filepath.SplitList(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var roots []string
	for _, d := range []string{"code", "Code", "Developer", "dev", "src", "Projects", "projects", "git", "GitHub", "repos", "Documents", "Desktop"} {
		roots = append(roots, filepath.Join(home, d))
	}
	return roots
}

// PlanDirName turns a plan name into a folder name: lowercase ASCII letters
// and digits joined by single dashes. A name with neither gives "".
func PlanDirName(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	return b.String()
}

// FindAgent looks for an agent's command line program on PATH and in the
// places installers put it. Apps started from Finder get a short PATH, so
// PATH alone misses most installs.
func FindAgent(agent Agent) (string, bool) {
	name := map[Agent]string{Claude: "claude", Codex: "codex", Cursor: "cursor-agent"}[agent]
	if name == "" {
		return "", false
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".claude", "local"),
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".bun", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
	} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}
