package ui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vanderheijden86/b9s/pkg/model"
)

func closeCommentModel(t *testing.T, exitCode int) (Model, string) {
	t.Helper()
	recordPath := filepath.Join(t.TempDir(), "calls")
	m := NewModel([]model.Issue{
		{ID: "bd-123", Title: "Close this ticket", Status: model.StatusOpen},
	}, "")
	m.issueWriter = &IssueWriter{bdPath: writeFakeBdRecorder(t, recordPath, exitCode), available: true, checkout: testCheckout(t)}
	return m, recordPath
}

func closeKeys(t *testing.T, m Model, keys ...tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var updated tea.Model
		updated, cmd = m.Update(k)
		m = updated.(Model)
	}
	return m, cmd
}

func closeRunes(s string) []tea.KeyMsg {
	var keys []tea.KeyMsg
	for _, r := range s {
		keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return keys
}

func TestCloseDialogOffersCommentForSingleIssue(t *testing.T) {
	m, _ := closeCommentModel(t, 0)
	m, _ = closeKeys(t, m, closeRunes("K")...)
	if !strings.Contains(m.View(), "[C] Comment and close") {
		t.Fatalf("single close dialog must offer a comment, got:\n%s", m.View())
	}
}

func TestCloseWithCommentAddsCommentThenCloses(t *testing.T) {
	m, recordPath := closeCommentModel(t, 0)
	keys := append(closeRunes("Kc"), closeRunes("yes, done")...)
	m, _ = closeKeys(t, m, keys...)
	if calls := readBdCallsUI(t, recordPath); len(calls) != 0 {
		t.Fatalf("typing the comment must not run bd, calls = %q", calls)
	}
	if !strings.Contains(m.View(), "then closes the issue") || !strings.Contains(m.View(), "[Enter] Comment and close") {
		t.Fatalf("comment step must say the issue closes after the comment, got:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "yes, done") {
		t.Fatalf("comment input must show the typed text, got:\n%s", m.View())
	}
	m, cmd := closeKeys(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter must close the issue")
	}
	result, ok := cmd().(BdResultMsg)
	if !ok || result.Operation != BdOpClose || result.IssueID != "bd-123" || !result.Success {
		t.Fatalf("unexpected result: %#v", result)
	}
	want := [][]string{{"comments", "add", "bd-123", "--", "yes, done"}, {"close", "bd-123"}}
	if got := readBdCallsUI(t, recordPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("bd calls = %q, want %q", got, want)
	}
	if strings.Contains(m.View(), "Close issue?") {
		t.Fatal("dialog must dismiss after Enter")
	}
}

func TestCloseWithEmptyCommentClosesWithoutComment(t *testing.T) {
	m, recordPath := closeCommentModel(t, 0)
	m, cmd := closeKeys(t, m, append(closeRunes("Kc"), tea.KeyMsg{Type: tea.KeyEnter})...)
	if cmd == nil {
		t.Fatal("Enter must close the issue")
	}
	cmd()
	want := [][]string{{"close", "bd-123"}}
	if got := readBdCallsUI(t, recordPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("bd calls = %q, want %q", got, want)
	}
}

func TestCloseCommentFailureLeavesIssueOpen(t *testing.T) {
	m, recordPath := closeCommentModel(t, 1)
	_, cmd := closeKeys(t, m, append(closeRunes("Kcnote"), tea.KeyMsg{Type: tea.KeyEnter})...)
	result, _ := cmd().(BdResultMsg)
	if result.Success || result.Operation != BdOpComment {
		t.Fatalf("a failed comment must be reported, got %#v", result)
	}
	if calls := readBdCallsUI(t, recordPath); len(calls) != 1 {
		t.Fatalf("close must not run after a failed comment, calls = %q", calls)
	}
}

func TestCloseCommentEscCancels(t *testing.T) {
	m, recordPath := closeCommentModel(t, 0)
	m, cmd := closeKeys(t, m, append(closeRunes("Kcabc"), tea.KeyMsg{Type: tea.KeyEsc})...)
	if cmd != nil {
		t.Fatal("Esc must not run bd")
	}
	if strings.Contains(m.View(), "Close issue?") {
		t.Fatal("Esc must dismiss the dialog")
	}
	if calls := readBdCallsUI(t, recordPath); len(calls) != 0 {
		t.Fatalf("no bd call expected, got %q", calls)
	}
}

func TestBulkCloseDialogHasNoComment(t *testing.T) {
	m := NewModel([]model.Issue{
		{ID: "bd-1", Title: "One", Status: model.StatusOpen},
		{ID: "bd-2", Title: "Two", Status: model.StatusOpen},
	}, "")
	m.issueConfirm = issueConfirmation{action: issueConfirmClose, ids: []string{"bd-1", "bd-2"}}
	if strings.Contains(m.View(), "[C] Comment and close") {
		t.Fatal("bulk close must not offer a comment")
	}
}
