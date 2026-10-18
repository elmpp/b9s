package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vanderheijden86/b9s/pkg/config"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// A model started on the first page alone owes the rest of the snapshot, so
// Init reads it in full without waiting for the database to change.
func TestFirstPageStartupReadsTheRestInFull(t *testing.T) {
	requests := make(chan doltReloadRequest, 4)
	m := newDoltReloadModel(t, func(req doltReloadRequest) ([]model.Issue, error) {
		requests <- req
		return nil, nil
	}).WithPartialSnapshot(true)

	_, cmd := updateWithin(t, m, firstPageShownMsg{}, 2*time.Second)
	if _, ok := findReloadMsg(runCmdMsgs(t, cmd, 2*time.Second)); !ok {
		t.Fatal("the first page shown must start a reload of the rest")
	}
	select {
	case req := <-requests:
		if !req.since.IsZero() {
			t.Fatalf("the read after the first page started at %s, want a full read", req.since)
		}
	default:
		t.Fatal("no read ran")
	}
}

func TestFirstPageStartupInitAsksForTheRest(t *testing.T) {
	m := newDoltReloadModel(t, nil).WithPartialSnapshot(true)
	if !hasMsgOfType[firstPageShownMsg](runCmdMsgs(t, m.Init(), 500*time.Millisecond)) {
		t.Fatal("Init of a partial snapshot must ask for the rest")
	}

	whole := newDoltReloadModel(t, nil)
	if hasMsgOfType[firstPageShownMsg](runCmdMsgs(t, whole.Init(), 500*time.Millisecond)) {
		t.Fatal("Init of a complete snapshot asked for the rest")
	}
}

// Project counts read every project through the same tunnel; on a partial
// start they wait until the rest of the active project has loaded.
func TestFirstPageStartupDefersProjectCounts(t *testing.T) {
	m := newDoltReloadModel(t, func(doltReloadRequest) ([]model.Issue, error) {
		return nil, nil
	})
	m.allProjects = []config.Project{{Name: "a", Path: t.TempDir()}, {Name: "b", Path: t.TempDir()}}
	m = m.WithPartialSnapshot(true)

	if hasMsgOfType[projectCountsLoadedMsg](runCmdMsgs(t, m.Init(), time.Second)) {
		t.Fatal("Init of a partial snapshot loaded project counts before the rest of the project")
	}
	m, cmd := updateWithin(t, m, firstPageShownMsg{}, 2*time.Second)
	reply, ok := findReloadMsg(runCmdMsgs(t, cmd, 2*time.Second))
	if !ok {
		t.Fatal("expected a reload reply")
	}
	_, cmd = updateWithin(t, m, reply, 2*time.Second)
	if !hasMsgOfType[projectCountsLoadedMsg](runCmdMsgs(t, cmd, 2*time.Second)) {
		t.Fatal("the read of the rest must load the deferred project counts")
	}
}

func hasMsgOfType[T any](msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			return true
		}
	}
	return false
}
