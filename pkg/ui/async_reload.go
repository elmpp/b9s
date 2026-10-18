package ui

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vanderheijden86/b9s/internal/datasource"
	"github.com/vanderheijden86/b9s/pkg/debug"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// doltReloadTimeout bounds one snapshot read. Through a saturated tunnel a
// full read has taken 28 s; past this bound the UI reports a timeout and keeps
// the snapshot it has rather than waiting on a connection that may never answer.
const doltReloadTimeout = 60 * time.Second

// errDoltReloadTimeout reports a reload that outlived doltReloadTimeout.
var errDoltReloadTimeout = errors.New("reload timed out")

// doltReloadRequest names the database a reload reads.
type doltReloadRequest struct {
	source datasource.DataSource
	// fromSource reads source directly. A project opened without a checkout
	// has only its database; every other project reads through beadsDir, so
	// BEADS_DIR cannot redirect a switched project back to the startup one.
	fromSource bool
	beadsDir   string
}

// doltReloadedMsg carries a finished Dolt reload back to the event loop.
type doltReloadedMsg struct {
	generation uint64
	issues     []model.Issue
	err        error
	started    time.Time
	loadTime   time.Duration
}

func loadDoltSnapshot(req doltReloadRequest) ([]model.Issue, error) {
	if req.fromSource {
		return datasource.LoadFromSource(req.source)
	}
	return datasource.LoadIssuesFromDir(req.beadsDir)
}

// startDoltReload reads the active Dolt project off the event loop. While one
// read runs, further changes only mark the snapshot stale: the reply of the
// running read starts one follow-up, however many commits landed meanwhile.
func (m Model) startDoltReload(cmds []tea.Cmd) (Model, tea.Cmd) {
	if m.reloadInFlight {
		m.reloadPending = true
		return m, tea.Batch(cmds...)
	}
	m.reloadInFlight = true
	m.reloadPending = false

	beadsDir, hasCheckout := projectBeadsDir(m.activeProjectPath)
	req := doltReloadRequest{
		source:     m.doltSource,
		fromSource: m.sourceType == datasource.SourceTypeDolt && !hasCheckout,
		beadsDir:   beadsDir,
	}
	load := m.reloadLoader
	if load == nil {
		load = loadDoltSnapshot
	}
	generation := m.projectGeneration
	debug.Log("doltReload: start generation=%d fromSource=%t db=%s dir=%s", generation, req.fromSource, req.source.Database, req.beadsDir)
	cmds = append(cmds, func() tea.Msg {
		return runDoltReload(load, req, generation, doltReloadTimeout)
	})
	return m, tea.Batch(cmds...)
}

// runDoltReload runs load with a deadline. A load that outlives it keeps
// running in its goroutine and its result is discarded.
func runDoltReload(load func(doltReloadRequest) ([]model.Issue, error), req doltReloadRequest, generation uint64, limit time.Duration) doltReloadedMsg {
	type result struct {
		issues []model.Issue
		err    error
	}
	started := time.Now()
	done := make(chan result, 1)
	go func() {
		issues, err := load(req)
		done <- result{issues, err}
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case r := <-done:
		return doltReloadedMsg{generation: generation, issues: r.issues, err: r.err, started: started, loadTime: time.Since(started)}
	case <-timer.C:
		return doltReloadedMsg{generation: generation, err: fmt.Errorf("%w after %s", errDoltReloadTimeout, limit), started: started, loadTime: limit}
	}
}

// finishDoltReload applies a reload reply. A reply stamped with an earlier
// project generation belongs to a project no longer on screen and is dropped:
// the switch already reset the in-flight state and armed its own watcher.
func (m Model) finishDoltReload(msg doltReloadedMsg) (Model, tea.Cmd) {
	if msg.generation != m.projectGeneration {
		debug.Log("doltReload: drop stale reply generation=%d current=%d", msg.generation, m.projectGeneration)
		return m, nil
	}
	m.reloadInFlight = false
	debug.Log("doltReload: done generation=%d issues=%d load=%s err=%v", msg.generation, len(msg.issues), msg.loadTime, msg.err)
	m, cmds := m.applyReload(msg.issues, msg.err, nil, msg.started, msg.loadTime, nil)
	if m.reloadPending {
		return m.startDoltReload(cmds)
	}
	return m, tea.Batch(cmds...)
}
