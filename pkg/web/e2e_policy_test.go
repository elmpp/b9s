package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The web suite's reliability policy (docs/adr/0059) lives in files Go never
// compiles: the Playwright config, the specs and the workflows. These tests
// are what makes the policy unwritable to break.

var repoRoot = filepath.Join("..", "..")

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

// A retry turns a flake into a green run nobody investigates. The config
// keeps retries at zero and no workflow passes --retries (docs/testing.md,
// "Fix the race, never with a retry or a longer timeout").
func TestPlaywrightRetriesStayAtZero(t *testing.T) {
	cfg := readRepoFile(t, filepath.Join("web", "playwright.config.ts"))
	if m := regexp.MustCompile(`\bretries:\s*([^,\n]+)`).FindStringSubmatch(cfg); m != nil && strings.TrimSpace(m[1]) != "0" {
		t.Errorf("web/playwright.config.ts sets retries: %s; the suite runs with retries: 0, and a flake is a bug to fix (docs/adr/0059)", m[1])
	}
	workflows, err := filepath.Glob(filepath.Join(repoRoot, ".github", "workflows", "*.yml"))
	if err != nil || len(workflows) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	for _, wf := range workflows {
		b, err := os.ReadFile(wf)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "--retries") {
			t.Errorf("%s passes --retries to Playwright; the suite runs with retries: 0 (docs/adr/0059)", filepath.Base(wf))
		}
	}
}

// A one-shot read of the page followed by a plain matcher fails when the
// app has not rendered yet, which on a loaded runner it often has not.
// Playwright's retrying forms wait: expect(locator).toHaveAttribute(...),
// expect(locator).toHaveCount(...), or expect.poll(() => ...) for a value
// the matchers cannot reach (bd-zoom.1.3).
func TestWebSpecsReadThePageThroughRetryingAssertions(t *testing.T) {
	oneShot := regexp.MustCompile(`\bexpect\(await [^;]*\.(evaluateAll|getAttribute|count|textContent|innerText|isVisible|isHidden|inputValue|allTextContents|allInnerTexts)\(`)
	specs, err := filepath.Glob(filepath.Join(repoRoot, "web", "tests", "*.spec.ts"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("no specs found: %v", err)
	}
	for _, spec := range specs {
		b, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(b), "\n") {
			if oneShot.MatchString(line) {
				t.Errorf("web/tests/%s:%d reads the page once and asserts on the value; use expect(locator).toHave*/toBe* or await expect.poll(() => ...) so the assertion retries until the app has rendered:\n%s", filepath.Base(spec), n+1, strings.TrimSpace(line))
			}
		}
	}
}

// Every project in the Playwright config runs in CI somewhere: the pull
// request gate (ci.yml) or the nightly matrix (nightly.yml). A project added
// to the config and to neither workflow would run only on a laptop.
func TestEveryPlaywrightProjectRunsInAWorkflow(t *testing.T) {
	cfg := readRepoFile(t, filepath.Join("web", "playwright.config.ts"))
	var projects []string
	for _, m := range regexp.MustCompile(`\bname: "([a-z0-9-]+)"`).FindAllStringSubmatch(cfg, -1) {
		projects = append(projects, m[1])
	}
	// The three desktop browsers are named from their device: "desktop" for Chrome, and the lowercased device with a dash for the rest.
	if m := regexp.MustCompile(`\(\[((?:"Desktop [A-Za-z]+",? ?)+)\] as const\)\.map`).FindStringSubmatch(cfg); m != nil {
		for _, d := range regexp.MustCompile(`"Desktop ([A-Za-z]+)"`).FindAllStringSubmatch(m[1], -1) {
			if d[1] == "Chrome" {
				projects = append(projects, "desktop")
			} else {
				projects = append(projects, "desktop-"+strings.ToLower(d[1]))
			}
		}
	}
	sort.Strings(projects)
	if len(projects) < 15 {
		t.Fatalf("found only %d projects in web/playwright.config.ts: %v; the regexes no longer match the config", len(projects), projects)
	}

	ci := readRepoFile(t, filepath.Join(".github", "workflows", "ci.yml")) + readRepoFile(t, "Makefile")
	nightly := readRepoFile(t, filepath.Join(".github", "workflows", "nightly.yml"))
	for _, p := range projects {
		flag := "--project " + p
		if !strings.Contains(ci, flag+" ") && !strings.Contains(ci, flag+"\n") && !strings.Contains(nightly, flag+" ") && !strings.Contains(nightly, flag+"\n") {
			t.Errorf("project %q is in web/playwright.config.ts but neither the PR gate (ci.yml via make web-gate) nor nightly.yml runs it (docs/adr/0059)", p)
		}
	}
	// And the other way round: a flag naming a project the config does not have runs nothing.
	known := map[string]bool{}
	for _, p := range projects {
		known[p] = true
	}
	for _, m := range regexp.MustCompile(`--project ([a-z0-9-]+)`).FindAllStringSubmatch(ci+nightly, -1) {
		if !known[m[1]] {
			t.Errorf("a workflow passes --project %s, which web/playwright.config.ts does not define", m[1])
		}
	}
}
