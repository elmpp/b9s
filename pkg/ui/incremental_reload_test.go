package ui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vanderheijden86/b9s/internal/datasource"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// scriptedReads answers each reload with the next read and records the start
// point every request asked for.
type scriptedReads struct {
	asked []time.Time
	reads []func(since time.Time) (datasource.LeanRead, error)
}

func (s *scriptedReads) load(req doltReloadRequest) (datasource.LeanRead, error) {
	s.asked = append(s.asked, req.since)
	next := s.reads[0]
	if len(s.reads) > 1 {
		s.reads = s.reads[1:]
	}
	return next(req.since)
}

func newIncrementalModel(t *testing.T, s *scriptedReads) Model {
	t.Helper()
	m := newDoltReloadModel(t, nil)
	m.reloadLoader = s.load
	return m
}

// reloadOnce runs one change notification through to the applied reply.
func reloadOnce(t *testing.T, m Model, trigger tea.Msg) Model {
	t.Helper()
	m, cmd := updateWithin(t, m, trigger, 2*time.Second)
	msgs := runCmdMsgs(t, cmd, 2*time.Second)
	reply, ok := findReloadMsg(msgs)
	if !ok {
		for _, msg := range msgs {
			if _, isChange := msg.(FileChangedMsg); isChange {
				return reloadOnce(t, m, msg)
			}
		}
		t.Fatal("expected a reload reply")
	}
	m, _ = updateWithin(t, m, reply, 2*time.Second)
	return m
}

var serverTime = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func fullRead(issues ...model.Issue) func(time.Time) (datasource.LeanRead, error) {
	return func(time.Time) (datasource.LeanRead, error) {
		return datasource.LeanRead{Full: true, Issues: issues, AsOf: serverTime}, nil
	}
}

func deltaRead(read datasource.LeanRead) func(time.Time) (datasource.LeanRead, error) {
	return func(time.Time) (datasource.LeanRead, error) {
		read.AsOf = serverTime.Add(time.Minute)
		return read, nil
	}
}

func TestDoltReloadReadsOnlyWhatChangedAfterAFullRead(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := &scriptedReads{reads: []func(time.Time) (datasource.LeanRead, error){
		fullRead(asyncReloadIssue("p-1", "First", base), asyncReloadIssue("p-2", "Second", base), asyncReloadIssue("p-3", "Third", base)),
		deltaRead(datasource.LeanRead{Issues: []model.Issue{asyncReloadIssue("p-2", "Renamed", base)}, Removed: []string{"p-3"}}),
	}}
	m := newIncrementalModel(t, s)
	m = reloadOnce(t, m, FileChangedMsg{})
	m = reloadOnce(t, m, FileChangedMsg{})

	if !s.asked[0].IsZero() {
		t.Fatalf("the first reload asked for changes since %s, want a full read", s.asked[0])
	}
	if !s.asked[1].Equal(serverTime) {
		t.Fatalf("the second reload asked since %s, want the first read's server time %s", s.asked[1], serverTime)
	}
	titles := map[string]string{}
	for _, issue := range m.issues {
		titles[issue.ID] = issue.Title
	}
	if len(titles) != 2 || titles["p-1"] != "First" || titles["p-2"] != "Renamed" {
		t.Fatalf("issues after the incremental reload = %v, want p-1 First and p-2 Renamed", titles)
	}
}

func TestDoltReloadReadsInFullAfterAFailedRead(t *testing.T) {
	s := &scriptedReads{reads: []func(time.Time) (datasource.LeanRead, error){
		fullRead(),
		func(time.Time) (datasource.LeanRead, error) {
			return datasource.LeanRead{}, errors.New("tunnel closed")
		},
		fullRead(),
	}}
	m := newIncrementalModel(t, s)
	for range 3 {
		m = reloadOnce(t, m, FileChangedMsg{})
	}
	if !s.asked[2].IsZero() {
		t.Fatalf("the reload after a failure asked since %s, want a full read", s.asked[2])
	}
}

func TestRefreshKeyReadsInFull(t *testing.T) {
	s := &scriptedReads{reads: []func(time.Time) (datasource.LeanRead, error){fullRead()}}
	m := newIncrementalModel(t, s)
	m = reloadOnce(t, m, FileChangedMsg{})
	m = reloadOnce(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if len(s.asked) != 2 || !s.asked[1].IsZero() {
		t.Fatalf("requests = %v, want Ctrl-R to read in full", s.asked)
	}
}

func TestDoltReloadReadsInFullAfterManyChanges(t *testing.T) {
	s := &scriptedReads{reads: []func(time.Time) (datasource.LeanRead, error){
		fullRead(),
		deltaRead(datasource.LeanRead{}),
	}}
	m := newIncrementalModel(t, s)
	m = reloadOnce(t, m, FileChangedMsg{})
	for range maxIncrementalReloads {
		m = reloadOnce(t, m, FileChangedMsg{})
	}
	m = reloadOnce(t, m, FileChangedMsg{})
	last := s.asked[len(s.asked)-1]
	if !last.IsZero() {
		t.Fatalf("after %d incremental reloads the next asked since %s, want a full read", maxIncrementalReloads, last)
	}
}

func TestDoltReloadReadsInFullWhenTheLastFullReadIsOld(t *testing.T) {
	s := &scriptedReads{reads: []func(time.Time) (datasource.LeanRead, error){fullRead()}}
	m := newIncrementalModel(t, s)
	m = reloadOnce(t, m, FileChangedMsg{})
	m.doltFullAt = time.Now().Add(-fullReloadEvery - time.Second)
	m = reloadOnce(t, m, FileChangedMsg{})
	if !s.asked[1].IsZero() {
		t.Fatalf("a reload long after the last full read asked since %s, want a full read", s.asked[1])
	}
}

func TestIncrementalReloadForgetsTheCachedTextOfChangedIssues(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	closed := asyncReloadIssue("p-1", "Closed", base)
	closed.Status = model.StatusClosed
	closed.Abridged = true
	s := &scriptedReads{reads: []func(time.Time) (datasource.LeanRead, error){
		fullRead(closed),
		// A comment that leaves updated_at alone: same row, new text.
		deltaRead(datasource.LeanRead{Issues: []model.Issue{closed}}),
	}}
	m := newIncrementalModel(t, s)
	m = reloadOnce(t, m, FileChangedMsg{})
	m.issueTexts = map[string]cachedIssueText{"p-1": {updatedAt: base, text: datasource.IssueText{Description: "old"}}}
	m = reloadOnce(t, m, FileChangedMsg{})
	if _, cached := m.issueTexts["p-1"]; cached {
		t.Fatal("an incremental reload kept the cached text of an issue it reports as changed")
	}
}
