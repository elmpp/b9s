package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vanderheijden86/b9s/internal/onboard"
	"github.com/vanderheijden86/b9s/pkg/config"
	"github.com/vanderheijden86/b9s/pkg/ui"
)

// OnboardHost runs the Add a project actions that reach outside the browser.
// B9s itself only ever runs bd init; every other change to a repository is
// left to the person's own agent, which the hand-off starts (ADR 0057).
type OnboardHost interface {
	// PickFolder shows the computer's folder picker and returns the chosen
	// folder, or "" when the person cancelled.
	PickFolder(ctx context.Context) (string, error)
	// StartAgent opens a terminal in dir running agent with prompt.
	StartAgent(ctx context.Context, dir string, agent onboard.Agent, prompt string) error
	// OpenTerminal opens a terminal in dir, running b9s there when b9s is set.
	OpenTerminal(ctx context.Context, dir string, b9s bool) error
	// InitBeads creates an embedded Beads project in dir, without agent
	// files or git hooks.
	InitBeads(ctx context.Context, dir string) error
	// Check reads dir's setup checklist.
	Check(ctx context.Context, dir string, agent onboard.Agent) onboard.Report
	// AgentInstalled reports whether agent's program is on this computer.
	AgentInstalled(agent onboard.Agent) bool
}

const repoListLimit = 30

func (s *Server) onboardRoutes() {
	s.mux.HandleFunc("GET /api/onboard/repos", s.paired(s.onboardOnly(s.handleOnboardRepos)))
	s.mux.HandleFunc("GET /api/onboard/check", s.paired(s.onboardOnly(s.handleOnboardCheck)))
	s.mux.HandleFunc("POST /api/onboard/plan-prompt", s.paired(s.csrf(s.onboardOnly(s.handlePlanPrompt))))
	s.mux.HandleFunc("POST /api/onboard/pick-folder", s.paired(s.csrf(s.onboardOnly(s.localOnly(s.handlePickFolder)))))
	s.mux.HandleFunc("POST /api/onboard/start-agent", s.paired(s.csrf(s.onboardOnly(s.localOnly(s.handleStartAgent)))))
	s.mux.HandleFunc("POST /api/onboard/terminal", s.paired(s.csrf(s.onboardOnly(s.localOnly(s.handleOpenTerminal)))))
	s.mux.HandleFunc("POST /api/onboard/init", s.paired(s.csrf(s.onboardOnly(s.handleOnboardInit))))
	s.mux.HandleFunc("POST /api/onboard/create-plan", s.paired(s.csrf(s.onboardOnly(s.handleCreatePlan))))
	s.mux.HandleFunc("POST /api/onboard/add", s.paired(s.csrf(s.onboardOnly(s.handleOnboardAdd))))
}

// onboardOnly refuses the flow on a public server, which must never reach
// the machine's folders, and on a server started without a Host.
func (s *Server) onboardOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Public {
			writeError(w, http.StatusForbidden, "this public server shows one project only")
			return
		}
		if s.opts.Host == nil {
			writeError(w, http.StatusNotImplemented, "adding projects is not available in this b9s web")
			return
		}
		next(w, r)
	}
}

// localOnly refuses actions that open a window on this computer when the
// browser is elsewhere, such as a paired phone: nobody there could answer
// the dialog.
func (s *Server) localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !fromLoopback(r) {
			writeError(w, http.StatusForbidden, "this opens a window on the computer that runs b9s web: use the browser there")
			return
		}
		next(w, r)
	}
}

func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) handleOnboardRepos(w http.ResponseWriter, r *http.Request) {
	roots := s.opts.RepoRoots
	if roots == nil {
		roots = onboard.DefaultRoots()
	}
	out := OnboardRepos{Repos: []OnboardRepo{}, Local: fromLoopback(r)}
	for _, repo := range onboard.FindRepos(roots, repoListLimit) {
		out.Repos = append(out.Repos, OnboardRepo{Name: repo.Name, Path: repo.Path, HasBeads: repo.HasBeads, Modified: repo.Modified.UTC().Format(time.RFC3339)})
	}
	for _, a := range onboard.Agents {
		out.Agents = append(out.Agents, OnboardAgent{ID: string(a), Label: a.Label(), Installed: s.opts.Host.AgentInstalled(a)})
	}
	writeJSON(w, r, out)
}

func (s *Server) handleOnboardCheck(w http.ResponseWriter, r *http.Request) {
	dir, err := cleanFolder(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, err := onboard.ParseAgent(r.URL.Query().Get("agent"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, s.onboardCheck(r.Context(), dir, agent))
}

func (s *Server) onboardCheck(ctx context.Context, dir string, agent onboard.Agent) OnboardCheck {
	report := s.opts.Host.Check(ctx, dir, agent)
	out := OnboardCheck{
		Path: dir, Name: filepath.Base(dir), Agent: string(agent), Complete: report.Complete,
		Prompt: onboard.Prompt(agent), Teammate: onboard.TeammateMessage(filepath.Base(dir), agent),
		Steps: make([]OnboardStep, 0, len(report.Steps)),
	}
	for _, st := range report.Steps {
		out.Steps = append(out.Steps, OnboardStep{ID: string(st.ID), Title: st.Title, Done: st.Done, Detail: st.Detail})
	}
	if s.opts.Projects != nil {
		for _, p := range s.opts.Projects() {
			if p.ResolvedPath() == dir {
				out.Key = ui.ProjectKey(p)
				break
			}
		}
	}
	return out
}

func (s *Server) handlePlanPrompt(w http.ResponseWriter, r *http.Request) {
	var req OnboardPlanRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Goal) == "" {
		writeError(w, http.StatusBadRequest, "write the goal first")
		return
	}
	writeJSON(w, r, OnboardText{Text: onboard.PlanPrompt(req.Goal, onboard.PlanOptions{PlanOnly: req.PlanOnly, AskFirst: req.AskFirst, Due: req.Due})})
}

func (s *Server) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	dir, err := s.opts.Host.PickFolder(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, OnboardText{Text: dir})
}

func (s *Server) handleStartAgent(w http.ResponseWriter, r *http.Request) {
	var req OnboardStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := cleanFolder(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agent, err := onboard.ParseAgent(req.Agent)
	if err != nil || agent == onboard.Other {
		writeError(w, http.StatusBadRequest, "B9s can start Claude Code, Codex or Cursor; paste the prompt into another AI yourself")
		return
	}
	prompt := req.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = onboard.Prompt(agent)
	}
	if err := s.opts.Host.StartAgent(r.Context(), dir, agent, prompt); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, OnboardText{Text: dir})
}

func (s *Server) handleOpenTerminal(w http.ResponseWriter, r *http.Request) {
	var req OnboardTerminalRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := req.Path
	if path == "" {
		if path = s.opts.Store.Target().Dir; path == "" {
			writeError(w, http.StatusBadRequest, "open one project first: all projects have no single folder")
			return
		}
	}
	dir, err := cleanFolder(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.opts.Host.OpenTerminal(r.Context(), dir, req.B9s); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, r, OnboardText{Text: dir})
}

// handleOnboardInit is "B9s, right now": an embedded Beads project in an
// existing folder, and nothing else.
func (s *Server) handleOnboardInit(w http.ResponseWriter, r *http.Request) {
	var req OnboardPathRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := cleanFolder(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if hasBeads(dir) {
		writeError(w, http.StatusConflict, filepath.Base(dir)+" already has a Beads project")
		return
	}
	if err := s.opts.Host.InitBeads(r.Context(), dir); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.addProject(w, r, dir)
}

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	var req OnboardCreatePlan
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.opts.PlansRoot == "" {
		writeError(w, http.StatusNotImplemented, "this b9s web has no folder for plans: start it with --projects-root")
		return
	}
	slug := onboard.PlanDirName(req.Name)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "give the plan a name with letters or digits")
		return
	}
	dir := filepath.Join(s.opts.PlansRoot, slug)
	if _, err := os.Stat(dir); err == nil {
		writeError(w, http.StatusConflict, dir+" already exists: choose another name")
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.opts.Host.InitBeads(r.Context(), dir); err != nil {
		// An empty folder left behind would block the same name next time.
		_ = os.RemoveAll(dir)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.addProject(w, r, dir)
}

func (s *Server) handleOnboardAdd(w http.ResponseWriter, r *http.Request) {
	var req OnboardPathRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := cleanFolder(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !hasBeads(dir) {
		writeError(w, http.StatusConflict, filepath.Base(dir)+" has no Beads project yet")
		return
	}
	s.addProject(w, r, dir)
}

// addProject opens dir and tells Opened, which puts it on the recent list,
// so it shows in the project sheet from now on.
func (s *Server) addProject(w http.ResponseWriter, r *http.Request, dir string) {
	p := config.Project{Name: filepath.Base(dir), Path: dir}
	key := ui.ProjectKey(p)
	if failure := s.opts.Store.Open(ui.ProjectOpenTarget(p, s.opts.StartupUser), key); failure != nil {
		writeError(w, http.StatusBadGateway, failure.Message())
		return
	}
	if s.opts.Opened != nil {
		s.opts.Opened(p)
	}
	writeJSON(w, r, s.opts.Store.Info())
}

func hasBeads(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".beads", "metadata.json"))
	return err == nil
}

// cleanFolder accepts an absolute path to an existing folder.
func cleanFolder(path string) (string, error) {
	if path == "" {
		return "", errors.New("choose a folder first")
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("the folder must be a full path")
	}
	dir := filepath.Clean(path)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New(dir + " is not a folder on this computer")
	}
	return dir, nil
}
