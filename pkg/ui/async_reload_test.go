package ui

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vanderheijden86/b9s/internal/datasource"
	"github.com/vanderheijden86/b9s/pkg/config"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// runCmdMsgs runs every leaf command of cmd concurrently and returns the
// messages that arrive before the deadline. A command that blocks past the
// deadline (a watcher wait, a loader held by the test) contributes nothing.
func runCmdMsgs(t *testing.T, cmd tea.Cmd, wait time.Duration) []tea.Msg {
	t.Helper()
	out := make(chan tea.Msg, 64)
	var pending atomic.Int32
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		pending.Add(1)
		go func() {
			defer pending.Add(-1)
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			if msg != nil {
				out <- msg
			}
		}()
	}
	run(cmd)
	deadline := time.After(wait)
	var msgs []tea.Msg
	for {
		select {
		case msg := <-out:
			msgs = append(msgs, msg)
		case <-deadline:
			return msgs
		}
		if pending.Load() == 0 && len(out) == 0 {
			return msgs
		}
	}
}

func findReloadMsg(msgs []tea.Msg) (doltReloadedMsg, bool) {
	for _, msg := range msgs {
		if r, ok := msg.(doltReloadedMsg); ok {
			return r, true
		}
	}
	return doltReloadedMsg{}, false
}

func asyncReloadIssue(id, title string, created time.Time) model.Issue {
	return model.Issue{ID: id, Title: title, Status: model.StatusOpen, IssueType: model.TypeTask, Priority: 2, CreatedAt: created, UpdatedAt: created}
}

// newDoltReloadModel builds a Dolt-backed model whose reload goes through
// load, so a test controls when and what the database answers.
func newDoltReloadModel(t *testing.T, load func(doltReloadRequest) ([]model.Issue, error)) Model {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issues := []model.Issue{
		asyncReloadIssue("p-1", "First", base.Add(2*time.Hour)),
		asyncReloadIssue("p-2", "Second", base.Add(time.Hour)),
		asyncReloadIssue("p-3", "Third", base),
	}
	m := NewModel(issues, "").
		WithConfig(config.Config{}, "remote", "").
		WithSourceType(datasource.SourceTypeDolt)
	m.doltSource = datasource.DataSource{Type: datasource.SourceTypeDolt, Path: "127.0.0.1:1", Database: "remote", User: "reader"}
	m.beadsPath = "stands-in-for-watcher.jsonl"
	if load != nil {
		m.reloadLoader = func(req doltReloadRequest) (datasource.LeanRead, error) {
			issues, err := load(req)
			return datasource.LeanRead{Full: true, Issues: issues}, err
		}
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return updated.(Model)
}

// updateWithin fails the test when Update does not return within limit, which
// is what a reload run inside the event handler looks like.
func updateWithin(t *testing.T, m Model, msg tea.Msg, limit time.Duration) (Model, tea.Cmd) {
	t.Helper()
	type result struct {
		m   tea.Model
		cmd tea.Cmd
	}
	done := make(chan result, 1)
	go func() {
		next, cmd := m.Update(msg)
		done <- result{next, cmd}
	}()
	select {
	case r := <-done:
		return r.m.(Model), r.cmd
	case <-time.After(limit):
		t.Fatalf("Update(%T) did not return within %s: the reload blocks the event loop", msg, limit)
		return m, nil
	}
}

func TestDoltReloadRunsOutsideTheEventLoop(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		<-release
		return nil, nil
	})
	before := m.TreeSelectedID()

	m, _ = updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	m, _ = updateWithin(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, 2*time.Second)

	if got := m.TreeSelectedID(); got == before {
		t.Fatalf("cursor stayed on %q while the reload was loading; navigation must proceed", got)
	}
}

func TestDoltReloadReplyAppliesTheNewSnapshot(t *testing.T) {
	newer := asyncReloadIssue("p-9", "Arrived by reload", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		return []model.Issue{newer}, nil
	})

	m, cmd := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	reply, ok := findReloadMsg(runCmdMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("FileChangedMsg on a Dolt project must return a command that delivers the reloaded snapshot")
	}
	m, _ = updateWithin(t, m, reply, 2*time.Second)

	if len(m.issues) != 1 || m.issues[0].ID != "p-9" {
		t.Fatalf("issues after the reload reply = %v, want only p-9", issueIDs(m.issues))
	}
}

func TestDoltReloadReplyFromAnEarlierProjectIsDropped(t *testing.T) {
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		return []model.Issue{asyncReloadIssue("old-1", "Previous project", time.Now())}, nil
	})

	m, cmd := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	reply, ok := findReloadMsg(runCmdMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("expected a reload reply")
	}
	m.projectGeneration++ // a project switch happened while the reload ran
	m, _ = updateWithin(t, m, reply, 2*time.Second)

	for _, issue := range m.issues {
		if issue.ID == "old-1" {
			t.Fatal("a reload reply for an earlier project replaced the current snapshot")
		}
	}
}

func TestDoltReloadCoalescesChangesThatArriveWhileLoading(t *testing.T) {
	var calls atomic.Int32
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		calls.Add(1)
		return nil, nil
	})

	m, first := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	m, second := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	m, third := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	runCmdMsgs(t, second, 500*time.Millisecond)
	runCmdMsgs(t, third, 500*time.Millisecond)
	reply, ok := findReloadMsg(runCmdMsgs(t, first, 2*time.Second))
	if !ok {
		t.Fatal("expected a reload reply")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("loads started while one was in flight = %d, want 1", got)
	}

	_, followUp := updateWithin(t, m, reply, 2*time.Second)
	if _, ok := findReloadMsg(runCmdMsgs(t, followUp, 2*time.Second)); !ok {
		t.Fatal("changes during a reload must start exactly one follow-up reload")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("total loads = %d, want 2 (the first plus one coalesced follow-up)", got)
	}
}

func TestDoltReloadErrorKeepsTheCurrentSnapshot(t *testing.T) {
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		return nil, errors.New("tunnel closed")
	})
	m, cmd := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	reply, ok := findReloadMsg(runCmdMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("expected a reload reply")
	}
	m, _ = updateWithin(t, m, reply, 2*time.Second)
	if len(m.issues) != 3 {
		t.Fatalf("issues after a failed reload = %d, want the 3 already shown", len(m.issues))
	}
	if !m.statusIsError {
		t.Fatal("a failed reload must show an error in the status line")
	}
}

func TestFooterShowsReloadingWhileADoltReloadRuns(t *testing.T) {
	release := make(chan struct{})
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		<-release
		return nil, nil
	})
	m.sourceInfo = "dolt://127.0.0.1:1/remote ✓"
	m.projectPicker.SetSourceInfo(m.sourceInfo)

	m, cmd := updateWithin(t, m, FileChangedMsg{}, 2*time.Second)
	if !strings.Contains(m.View(), "reloading") {
		t.Fatal("the footer must show that a reload is running")
	}
	close(release)
	reply, ok := findReloadMsg(runCmdMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("expected a reload reply")
	}
	m, _ = updateWithin(t, m, reply, 2*time.Second)
	if strings.Contains(m.View(), "reloading") {
		t.Fatal("the reloading marker must clear when the reload finishes")
	}
}
