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
	// since is the server time of the previous read; zero asks for a full
	// read (docs/adr/0062-read-only-what-changed-since-the-last-reload.md).
	since time.Time
}

// maxIncrementalReloads and fullReloadEvery bound how long the snapshot
// lives on incremental reads alone. Labels carry no timestamp and a removed
// dependency leaves no row, so only a full read sees those changes.
const (
	maxIncrementalReloads = 20
	fullReloadEvery       = 5 * time.Minute
)

// doltReloadedMsg carries a finished Dolt reload back to the event loop.
type doltReloadedMsg struct {
	generation uint64
	read       datasource.LeanRead
	err        error
	started    time.Time
	loadTime   time.Duration
}

// firstPageShownMsg arrives once the first page is on screen and asks for the
// rest of a partial snapshot.
type firstPageShownMsg struct{}

// loadRestOfSnapshot starts the full read a partial snapshot owes. The sync
// state is still zero, so the read is full, and its reply replaces the first
// page. The project counts wait for that reply.
func (m Model) loadRestOfSnapshot() (Model, tea.Cmd) {
	m.partialSnapshot = false
	m.projectCountsDeferred = len(m.allProjects) > 1
	if !m.sourceType.IsDolt() {
		m.projectCountsDeferred = false
		return m, tea.Batch(m.projectCountsCmd(), func() tea.Msg { return FileChangedMsg{} })
	}
	m.resetDoltSync()
	return m.startDoltReload(nil)
}

// projectCountsCmd loads the project picker's counts, or nothing when there is
// no other project to count.
func (m Model) projectCountsCmd() tea.Cmd {
	if len(m.allProjects) <= 1 {
		return nil
	}
	return loadProjectCountsCmd(m.allProjects, m.startupDoltUser)
}

// loadDoltSnapshot reads a lean snapshot, or what changed since req.since.
// Every reader of an Abridged issue's text goes through Model.wholeIssue,
// which loads it first.
func loadDoltSnapshot(req doltReloadRequest) (datasource.LeanRead, error) {
	if req.fromSource {
		return datasource.ReadLeanFromSource(req.source, req.since)
	}
	return datasource.ReadLeanFromDir(req.beadsDir, req.since)
}

// doltReadSince returns the start point of the next read, or zero for a full
// read.
func (m Model) doltReadSince(now time.Time) time.Time {
	if m.doltForceFull || m.doltDeltaCount >= maxIncrementalReloads || now.Sub(m.doltFullAt) >= fullReloadEvery {
		return time.Time{}
	}
	return m.doltSyncedAt
}

// resetDoltSync makes the next read a full one.
func (m *Model) resetDoltSync() {
	m.doltSyncedAt = time.Time{}
	m.doltDeltaCount = 0
	m.doltFullAt = time.Time{}
}

// doltRequest names the active project's database.
func (m Model) doltRequest() doltReloadRequest {
	beadsDir, hasCheckout := projectBeadsDir(m.activeProjectPath)
	return doltReloadRequest{
		source:     m.doltSource,
		fromSource: m.sourceType == datasource.SourceTypeDolt && !hasCheckout,
		beadsDir:   beadsDir,
	}
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

	req := m.doltRequest()
	req.since = m.doltReadSince(time.Now())
	m.doltForceFull = false
	load := m.reloadLoader
	if load == nil {
		load = loadDoltSnapshot
	}
	generation := m.projectGeneration
	debug.Log("doltReload: start generation=%d fromSource=%t db=%s dir=%s since=%s", generation, req.fromSource, req.source.Database, req.beadsDir, req.since)
	cmds = append(cmds, func() tea.Msg {
		return runDoltReload(load, req, generation, doltReloadTimeout)
	})
	return m, tea.Batch(cmds...)
}

// runDoltReload runs load with a deadline. A load that outlives it keeps
// running in its goroutine and its result is discarded.
func runDoltReload(load func(doltReloadRequest) (datasource.LeanRead, error), req doltReloadRequest, generation uint64, limit time.Duration) doltReloadedMsg {
	type result struct {
		read datasource.LeanRead
		err  error
	}
	started := time.Now()
	done := make(chan result, 1)
	go func() {
		read, err := load(req)
		done <- result{read, err}
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case r := <-done:
		return doltReloadedMsg{generation: generation, read: r.read, err: r.err, started: started, loadTime: time.Since(started)}
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
	read := msg.read
	debug.Log("doltReload: done generation=%d full=%t issues=%d removed=%d load=%s err=%v", msg.generation, read.Full, len(read.Issues), len(read.Removed), msg.loadTime, msg.err)
	var issues []model.Issue
	if msg.err == nil || errors.Is(msg.err, datasource.ErrCommentsUnavailable) {
		issues = datasource.MergeLean(m.issues, read)
		if !read.Full {
			// A comment need not move updated_at, so text cached for a
			// changed issue may be stale even when its row looks the same.
			for _, issue := range read.Issues {
				delete(m.issueTexts, issue.ID)
			}
		}
	}
	switch {
	case msg.err != nil || read.AsOf.IsZero():
		m.resetDoltSync()
	case read.Full:
		m.doltSyncedAt, m.doltDeltaCount, m.doltFullAt = read.AsOf, 0, msg.started
	default:
		m.doltSyncedAt = read.AsOf
		m.doltDeltaCount++
	}
	m, cmds := m.applyReload(issues, msg.err, nil, msg.started, msg.loadTime, nil)
	if m.projectCountsDeferred {
		m.projectCountsDeferred = false
		cmds = append(cmds, m.projectCountsCmd())
	}
	if m.reloadPending {
		return m.startDoltReload(cmds)
	}
	return m, tea.Batch(cmds...)
}
