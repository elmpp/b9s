package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vanderheijden86/b9s/internal/onboard"
	"github.com/vanderheijden86/b9s/pkg/config"
	"github.com/vanderheijden86/b9s/pkg/ui"
)

// fakeHost records what the flow asked of the computer and writes a JSONL
// Beads project where bd init would create an embedded one.
type fakeHost struct {
	mu       sync.Mutex
	picked   string
	started  []string
	terminal []string
	b9s      []bool
	inits    []string
	initErr  error
}

func (h *fakeHost) PickFolder(context.Context) (string, error) { return h.picked, nil }
func (h *fakeHost) StartAgent(_ context.Context, dir string, a onboard.Agent, prompt string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.started = append(h.started, dir+"|"+string(a)+"|"+prompt)
	return nil
}
func (h *fakeHost) OpenTerminal(_ context.Context, dir string, b9s bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminal = append(h.terminal, dir)
	h.b9s = append(h.b9s, b9s)
	return nil
}
func (h *fakeHost) InitBeads(_ context.Context, dir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.initErr != nil {
		return h.initErr
	}
	h.inits = append(h.inits, dir)
	writeBeads(dir)
	return nil
}
func (h *fakeHost) Check(ctx context.Context, dir string, a onboard.Agent) onboard.Report {
	return onboard.Check(ctx, dir, a, onboard.Options{CountIssues: func(context.Context, string) (int, error) { return 2, nil }})
}
func (h *fakeHost) AgentInstalled(a onboard.Agent) bool { return a == onboard.Claude }

func writeBeads(dir string) {
	_ = os.MkdirAll(filepath.Join(dir, ".beads"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".beads", "issues.jsonl"), []byte(fixtureIssues), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".beads", "metadata.json"), []byte(`{"backend":"jsonl"}`), 0o644)
}

type onboardServer struct {
	srv    *Server
	host   *fakeHost
	store  *Store
	opened []config.Project
	root   string
	plans  string
}

func newOnboardServer(t *testing.T, public bool) *onboardServer {
	t.Helper()
	store := NewStore(50 * time.Millisecond)
	t.Cleanup(store.Close)
	dir := newFixtureProject(t)
	if failure := store.Open(ui.ProjectOpenTarget(config.Project{Name: "fixture", Path: dir}, ""), dir); failure != nil {
		t.Fatal(failure.Message())
	}
	o := &onboardServer{host: &fakeHost{}, store: store, root: t.TempDir(), plans: t.TempDir()}
	srv, err := NewServer(Options{
		Store: store, Assets: testAssets, Public: public, Host: o.host,
		RepoRoots: []string{o.root}, PlansRoot: o.plans,
		Projects: func() []config.Project { return o.opened },
		Opened:   func(p config.Project) { o.opened = append(o.opened, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	o.srv = srv
	return o
}

func (o *onboardServer) do(t *testing.T, method, path string, body any, remote string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	o.srv.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const local = "127.0.0.1:50000"

func TestOnboardReposListsRepositoriesAndAgents(t *testing.T) {
	o := newOnboardServer(t, false)
	_ = os.MkdirAll(filepath.Join(o.root, "webshop", ".git"), 0o755)
	rec, _ := o.do(t, "GET", "/api/onboard/repos", nil, local)
	var got OnboardRepos
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(got.Repos) != 1 || got.Repos[0].Name != "webshop" || !got.Local {
		t.Fatalf("repos = %+v", got)
	}
	if len(got.Agents) != 4 || !got.Agents[0].Installed || got.Agents[1].Installed {
		t.Fatalf("agents = %+v", got.Agents)
	}
}

func TestOnboardCheckReportsTheChecklistAndTexts(t *testing.T) {
	o := newOnboardServer(t, false)
	dir := t.TempDir()
	rec, _ := o.do(t, "GET", "/api/onboard/check?agent=codex&path="+dir, nil, local)
	var got OnboardCheck
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(got.Steps) != 6 || got.Complete || got.Agent != "codex" || got.Key != "" {
		t.Fatalf("check = %+v", got)
	}
	if !strings.Contains(got.Prompt, "b9s onboard --agent codex") || !strings.Contains(got.Teammate, filepath.Base(dir)) {
		t.Fatalf("texts = %q / %q", got.Prompt, got.Teammate)
	}
}

func TestOnboardCheckRefusesRelativeAndMissingFolders(t *testing.T) {
	o := newOnboardServer(t, false)
	for _, p := range []string{"", "code/webshop", "/no/such/folder"} {
		if rec, _ := o.do(t, "GET", "/api/onboard/check?path="+p, nil, local); rec.Code != http.StatusBadRequest {
			t.Errorf("path %q: %d", p, rec.Code)
		}
	}
}

func TestOnboardWindowActionsNeedTheLocalBrowser(t *testing.T) {
	o := newOnboardServer(t, false)
	dir := t.TempDir()
	for _, c := range []struct {
		path string
		body any
	}{
		{"/api/onboard/pick-folder", map[string]string{}},
		{"/api/onboard/start-agent", OnboardStartRequest{Path: dir, Agent: "claude"}},
		{"/api/onboard/terminal", OnboardTerminalRequest{Path: dir}},
	} {
		if rec, _ := o.do(t, "POST", c.path, c.body, "192.168.1.20:5555"); rec.Code != http.StatusForbidden {
			t.Errorf("%s from a phone: %d", c.path, rec.Code)
		}
	}
	if len(o.host.started)+len(o.host.terminal) != 0 {
		t.Fatal("no window may open for a remote browser")
	}
}

func TestOnboardStartAgentSendsTheSetupPrompt(t *testing.T) {
	o := newOnboardServer(t, false)
	dir := t.TempDir()
	if rec, _ := o.do(t, "POST", "/api/onboard/start-agent", OnboardStartRequest{Path: dir, Agent: "claude"}, local); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(o.host.started) != 1 || !strings.Contains(o.host.started[0], dir+"|claude|Set up Beads") {
		t.Fatalf("started = %q", o.host.started)
	}
	if rec, _ := o.do(t, "POST", "/api/onboard/start-agent", OnboardStartRequest{Path: dir, Agent: "other"}, local); rec.Code != http.StatusBadRequest {
		t.Fatalf("another AI cannot be started: %d", rec.Code)
	}
}

func TestOnboardPickFolder(t *testing.T) {
	o := newOnboardServer(t, false)
	o.host.picked = "/Users/sam/code/webshop"
	_, out := o.do(t, "POST", "/api/onboard/pick-folder", map[string]string{}, local)
	if out["text"] != "/Users/sam/code/webshop" {
		t.Fatalf("picked = %v", out)
	}
}

func TestOnboardPlanPrompt(t *testing.T) {
	o := newOnboardServer(t, false)
	_, out := o.do(t, "POST", "/api/onboard/plan-prompt", OnboardPlanRequest{Goal: "Pay with iDEAL", PlanOnly: true}, local)
	if text, _ := out["text"].(string); !strings.Contains(text, "Goal: Pay with iDEAL") || !strings.Contains(text, "Do not write code") {
		t.Fatalf("prompt = %v", out)
	}
	if rec, _ := o.do(t, "POST", "/api/onboard/plan-prompt", OnboardPlanRequest{Goal: "  "}, local); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty goal: %d", rec.Code)
	}
}

func TestOnboardAddOpensAndRemembersTheProject(t *testing.T) {
	o := newOnboardServer(t, false)
	dir := t.TempDir()
	if rec, _ := o.do(t, "POST", "/api/onboard/add", OnboardPathRequest{Path: dir}, local); rec.Code != http.StatusConflict {
		t.Fatalf("a folder without Beads: %d", rec.Code)
	}
	writeBeads(dir)
	rec, out := o.do(t, "POST", "/api/onboard/add", OnboardPathRequest{Path: dir}, local)
	if rec.Code != 200 || out["key"] != dir {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(o.opened) != 1 || o.opened[0].Path != dir {
		t.Fatalf("opened = %+v", o.opened)
	}
	_, check := o.do(t, "GET", "/api/onboard/check?path="+dir, nil, local)
	if check["key"] != dir {
		t.Fatalf("a known folder carries its key: %v", check["key"])
	}
}

func TestOnboardInitRunsBdOnceAndOpens(t *testing.T) {
	o := newOnboardServer(t, false)
	dir := t.TempDir()
	if rec, _ := o.do(t, "POST", "/api/onboard/init", OnboardPathRequest{Path: dir}, local); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec, _ := o.do(t, "POST", "/api/onboard/init", OnboardPathRequest{Path: dir}, local); rec.Code != http.StatusConflict {
		t.Fatalf("second init: %d", rec.Code)
	}
	if len(o.host.inits) != 1 || o.store.Info().Key != dir {
		t.Fatalf("inits = %v, open = %+v", o.host.inits, o.store.Info())
	}
}

func TestOnboardCreatePlanMakesAFolderUnderThePlansRoot(t *testing.T) {
	o := newOnboardServer(t, false)
	rec, out := o.do(t, "POST", "/api/onboard/create-plan", OnboardCreatePlan{Name: "Q1 roadmap"}, local)
	want := filepath.Join(o.plans, "q1-roadmap")
	if rec.Code != 200 || out["key"] != want {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec, _ := o.do(t, "POST", "/api/onboard/create-plan", OnboardCreatePlan{Name: "Q1 Roadmap"}, local); rec.Code != http.StatusConflict {
		t.Fatalf("same folder twice: %d", rec.Code)
	}
	if rec, _ := o.do(t, "POST", "/api/onboard/create-plan", OnboardCreatePlan{Name: "!!"}, local); rec.Code != http.StatusBadRequest {
		t.Fatalf("no letters: %d", rec.Code)
	}
}

func TestOnboardCreatePlanRemovesTheFolderWhenBdFails(t *testing.T) {
	o := newOnboardServer(t, false)
	o.host.initErr = os.ErrPermission
	if rec, _ := o.do(t, "POST", "/api/onboard/create-plan", OnboardCreatePlan{Name: "launch"}, local); rec.Code != http.StatusBadGateway {
		t.Fatalf("%d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(o.plans, "launch")); !os.IsNotExist(err) {
		t.Fatal("a failed plan must not leave its folder behind")
	}
}

func TestOnboardIsRefusedOnAPublicServer(t *testing.T) {
	o := newOnboardServer(t, true)
	for _, path := range []string{"/api/onboard/repos", "/api/onboard/check?path=/tmp"} {
		if rec, _ := o.do(t, "GET", path, nil, local); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
	if rec, _ := o.do(t, "POST", "/api/onboard/create-plan", OnboardCreatePlan{Name: "x"}, local); rec.Code != http.StatusForbidden {
		t.Errorf("create-plan: %d", rec.Code)
	}
}

func TestOnboardPostsNeedTheCSRFHeader(t *testing.T) {
	ts := newTestServer(t, testAuth(t), nil)
	c := ts.pairedClient(t)
	resp, err := c.Post(ts.URL+"/api/onboard/create-plan", "application/json", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF: %d", resp.StatusCode)
	}
}

func TestEmbeddedEnvDropsDoltSettings(t *testing.T) {
	got := embeddedEnv([]string{"HOME=/h", "BEADS_DOLT_PASSWORD=x", "BD_ACTOR=a", "DOLT_HOST=h", "PATH=/bin"})
	if strings.Join(got, " ") != "HOME=/h PATH=/bin" {
		t.Fatalf("env = %v", got)
	}
}

func TestShellAndAppleQuoting(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("shellQuote = %s", got)
	}
	if got := appleString(`say "hi" \ bye`); got != `"say \"hi\" \\ bye"` {
		t.Errorf("appleString = %s", got)
	}
}

func TestLocalHostOpensNoWindowInTestMode(t *testing.T) {
	t.Setenv("B9S_TEST_MODE", "1")
	if _, err := (LocalHost{}).PickFolder(context.Background()); !errors.Is(err, errTestMode) {
		t.Fatalf("PickFolder = %v, want errTestMode", err)
	}
	if err := (LocalHost{}).OpenTerminal(context.Background(), t.TempDir(), false); !errors.Is(err, errTestMode) {
		t.Fatalf("OpenTerminal = %v, want errTestMode", err)
	}
}

func TestOnboardTerminalWithoutAPathOpensTheOpenProject(t *testing.T) {
	o := newOnboardServer(t, false)
	rec, _ := o.do(t, "POST", "/api/onboard/terminal", OnboardTerminalRequest{B9s: true}, local)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if want := o.store.Target().Dir; len(o.host.terminal) != 1 || o.host.terminal[0] != want || !o.host.b9s[0] {
		t.Fatalf("terminal %v b9s %v, want %s running b9s", o.host.terminal, o.host.b9s, want)
	}
}
