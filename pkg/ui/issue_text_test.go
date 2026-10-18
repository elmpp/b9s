package ui

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vanderheijden86/b9s/internal/datasource"
	"github.com/vanderheijden86/b9s/pkg/config"
	"github.com/vanderheijden86/b9s/pkg/model"
)

// A lean snapshot leaves closed issues Abridged; the detail pane, copy, the
// editor, attachments and the edit form load their text first (bd-mlet.5).

var textBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func abridgedIssue(id string, updated time.Time) model.Issue {
	return model.Issue{ID: id, Title: "Old " + id, Status: model.StatusClosed, IssueType: model.TypeTask,
		Priority: 2, CreatedAt: textBase, UpdatedAt: updated, Abridged: true}
}

func oldText(id string) datasource.IssueText {
	return datasource.IssueText{
		Description: "Full description of " + id,
		Notes:       "Notes of " + id,
		Comments:    []*model.Comment{{ID: id + "-c1", IssueID: id, Author: "Claude", Text: "Remark on " + id, CreatedAt: textBase}},
	}
}

// newAbridgedModel shows one abridged issue in a visible detail pane, with
// text answered by load. It returns the command of the keypress that showed
// the pane, which carries the text request.
func newAbridgedModel(t *testing.T, load func(doltReloadRequest, string) (datasource.IssueText, error)) (Model, tea.Cmd) {
	t.Helper()
	m := NewModel([]model.Issue{abridgedIssue("o-1", textBase)}, "").
		WithConfig(config.Config{}, "remote", "").
		WithSourceType(datasource.SourceTypeDolt)
	m.doltSource = datasource.DataSource{Type: datasource.SourceTypeDolt, Path: "127.0.0.1:1", Database: "remote", User: "reader"}
	m.textLoader = load
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)
	if !m.detailVisible() {
		t.Fatal("setup: detail pane should be visible")
	}
	return m, cmd
}

func fetchedTextMsg(t *testing.T, cmd tea.Cmd) issueTextLoadedMsg {
	t.Helper()
	for _, msg := range runCmdMsgs(t, cmd, 2*time.Second) {
		if loaded, ok := msg.(issueTextLoadedMsg); ok {
			return loaded
		}
	}
	t.Fatal("no issueTextLoadedMsg: the abridged issue's text was never requested")
	return issueTextLoadedMsg{}
}

func TestAbridgedDetailShowsPlaceholderThenText(t *testing.T) {
	var calls atomic.Int32
	m, cmd := newAbridgedModel(t, func(_ doltReloadRequest, id string) (datasource.IssueText, error) {
		calls.Add(1)
		return oldText(id), nil
	})
	if !strings.Contains(ansi.Strip(m.viewport.View()), "Loading") {
		t.Fatalf("abridged detail should show a loading placeholder, got:\n%s", m.viewport.View())
	}
	// Renders while the request runs must not start a second one.
	m = sendDetailKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m.updateViewportContent()

	loaded := fetchedTextMsg(t, cmd)
	next, _ := m.Update(loaded)
	m = next.(Model)

	view := ansi.Strip(m.viewport.View())
	if !strings.Contains(view, "Full description of o-1") || !strings.Contains(view, "Remark on o-1") {
		t.Fatalf("detail should show the fetched text and comment, got:\n%s", view)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("text loaded %d times, want 1: renders while a fetch runs must not start another", got)
	}
}

func TestEditWaitsForAbridgedText(t *testing.T) {
	m, _ := newAbridgedModel(t, func(_ doltReloadRequest, id string) (datasource.IssueText, error) {
		return oldText(id), nil
	})
	m.focused = focusTree
	// The first request is dropped, as if it were still running elsewhere:
	// the edit key must answer from the request it starts or joins.
	m.issueTextLoading = nil
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(Model)
	if m.showEditModal {
		t.Fatal("the edit form opened on an issue whose description is not loaded: saving would erase it")
	}
	if !strings.Contains(m.statusMsg, "Loading") {
		t.Fatalf("status should say the text is loading, got %q", m.statusMsg)
	}

	next, _ = m.Update(fetchedTextMsg(t, cmd))
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(Model)
	if !m.showEditModal {
		t.Fatal("edit form should open once the text is loaded")
	}
	if got := m.issueMap["o-1"].Description; got != "Full description of o-1" {
		t.Fatalf("issue description = %q, want the fetched one", got)
	}
}

func TestCopyWaitsForAbridgedText(t *testing.T) {
	m, cmd := newAbridgedModel(t, func(_ doltReloadRequest, id string) (datasource.IssueText, error) {
		return oldText(id), nil
	})
	var copied []string
	m.clipboardWrite = func(text string) error { copied = append(copied, text); return nil }
	m.copyIssueToClipboard()
	if len(copied) != 0 {
		t.Fatal("copied an issue whose text is not loaded")
	}
	if !strings.Contains(m.statusMsg, "Loading") {
		t.Fatalf("status should say the text is loading, got %q", m.statusMsg)
	}
	next, _ := m.Update(fetchedTextMsg(t, cmd))
	m = next.(Model)
	m.copyIssueToClipboard()
	if len(copied) != 1 || !strings.Contains(copied[0], "Full description of o-1") {
		t.Fatalf("copy should carry the fetched text, got %q", copied)
	}
}

func TestReloadKeepsFetchedTextWhileIssueIsUnchanged(t *testing.T) {
	m, cmd := newAbridgedModel(t, func(_ doltReloadRequest, id string) (datasource.IssueText, error) {
		return oldText(id), nil
	})
	next, _ := m.Update(fetchedTextMsg(t, cmd))
	m = next.(Model)

	m, _ = m.applyReload([]model.Issue{abridgedIssue("o-1", textBase)}, nil, nil, time.Now(), 0, nil)
	if got := m.issueMap["o-1"]; got.Abridged || got.Description != "Full description of o-1" {
		t.Fatalf("unchanged issue lost its fetched text on reload: abridged=%t description=%q", got.Abridged, got.Description)
	}

	m, _ = m.applyReload([]model.Issue{abridgedIssue("o-1", textBase.Add(time.Hour))}, nil, nil, time.Now(), 0, nil)
	if got := m.issueMap["o-1"]; !got.Abridged || got.Description != "" {
		t.Fatalf("changed issue kept outdated text: abridged=%t description=%q", got.Abridged, got.Description)
	}
}

func TestTextFromAnEarlierProjectIsDropped(t *testing.T) {
	m, cmd := newAbridgedModel(t, func(_ doltReloadRequest, id string) (datasource.IssueText, error) {
		return oldText(id), nil
	})
	loaded := fetchedTextMsg(t, cmd)
	loaded.generation = m.projectGeneration + 1
	next, _ := m.Update(loaded)
	m = next.(Model)
	if m.issueMap["o-1"].Description != "" {
		t.Fatal("text from another project generation was applied")
	}
}

func TestFailedTextLoadIsShownAndNotRetriedInALoop(t *testing.T) {
	var calls atomic.Int32
	m, cmd := newAbridgedModel(t, func(doltReloadRequest, string) (datasource.IssueText, error) {
		calls.Add(1)
		return datasource.IssueText{}, errors.New("tunnel down")
	})
	next, cmd := m.Update(fetchedTextMsg(t, cmd))
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.viewport.View()), "tunnel down") {
		t.Fatalf("detail should show why the text is missing, got:\n%s", m.viewport.View())
	}
	for _, msg := range runCmdMsgs(t, cmd, 200*time.Millisecond) {
		if _, ok := msg.(issueTextLoadedMsg); ok {
			t.Fatal("a failed text load was retried at once")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("text loaded %d times, want 1", got)
	}
}

func TestOpenEditModalNeverOpensOnAnAbridgedIssue(t *testing.T) {
	m, _ := newAbridgedModel(t, func(_ doltReloadRequest, id string) (datasource.IssueText, error) {
		return oldText(id), nil
	})
	issue := abridgedIssue("o-1", textBase)
	m.openEditModal(&issue)
	if m.showEditModal {
		t.Fatal("openEditModal opened the form on an Abridged issue")
	}
}
