package ui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func markRows(t *testing.T, m Model, ids ...string) Model {
	t.Helper()
	for _, id := range ids {
		m.tree.SelectByID(id)
		m, _ = pressBulkKey(t, m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	}
	return m
}

func TestLowercaseUUnmarksCursorRow(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1", "bd-2")
	m.tree.SelectByID("bd-1")

	m, _ = pressBulkKey(t, m, runeKey("u"))

	if got := m.tree.TreeMarkedIDs(); !reflect.DeepEqual(got, []string{"bd-2"}) {
		t.Fatalf("u must unmark only the cursor row, got %v", got)
	}
}

// u is unmark, not toggle: dired users press it over a region expecting every
// row to end up unmarked, whatever state each row started in.
func TestLowercaseUDoesNotMarkUnmarkedRow(t *testing.T) {
	m := newBulkMarkModel(t)

	m, _ = pressBulkKey(t, m, runeKey("u"))

	if got := m.tree.MarkedCount(); got != 0 {
		t.Fatalf("u on an unmarked row must leave it unmarked, %d marked", got)
	}
}

func TestUppercaseMPreservesMarks(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1", "bd-3")

	m, _ = pressBulkKey(t, m, runeKey("M"))

	if got := m.tree.MarkedCount(); got != 2 {
		t.Fatalf("M opens Memories and must preserve marks, %d remain", got)
	}
}

func TestUppercaseVMarksRangeFromPreviousMark(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1")
	m.tree.SelectByID("bd-3")

	m, _ = pressBulkKey(t, m, runeKey("V"))

	want := []string{"bd-1", "bd-2", "bd-3"}
	if got := m.tree.TreeMarkedIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("V must mark the range, want %v got %v", want, got)
	}
}

// Repeated m marks consecutive rows, as dired-mark does.
func TestLowercaseMMarksAndAdvances(t *testing.T) {
	m := newBulkMarkModel(t)

	m, _ = pressBulkKey(t, m, runeKey("m"))
	m, _ = pressBulkKey(t, m, runeKey("m"))

	if got := m.tree.TreeMarkedIDs(); !reflect.DeepEqual(got, []string{"bd-1", "bd-2"}) {
		t.Fatalf("m m must mark two consecutive rows, got %v", got)
	}
	if got := m.tree.GetSelectedID(); got != "bd-3" {
		t.Fatalf("m must move the cursor to the next row, cursor on %q", got)
	}
}

// m marks rather than toggles: pressed over a marked row it keeps the mark.
func TestLowercaseMKeepsExistingMark(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1")
	m.tree.SelectByID("bd-1")

	m, _ = pressBulkKey(t, m, runeKey("m"))

	if got := m.tree.TreeMarkedIDs(); !reflect.DeepEqual(got, []string{"bd-1"}) {
		t.Fatalf("m over a marked row must keep it marked, got %v", got)
	}
}

func TestLowercaseUUnmarksAndAdvances(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1", "bd-2", "bd-3")
	m.tree.SelectByID("bd-1")

	m, _ = pressBulkKey(t, m, runeKey("u"))
	m, _ = pressBulkKey(t, m, runeKey("u"))

	if got := m.tree.TreeMarkedIDs(); !reflect.DeepEqual(got, []string{"bd-3"}) {
		t.Fatalf("u u must unmark two consecutive rows, got %v", got)
	}
	if got := m.tree.GetSelectedID(); got != "bd-3" {
		t.Fatalf("u must move the cursor to the next row, cursor on %q", got)
	}
}

// Backspace is dired's DEL: move up one row and unmark it.
func TestBackspaceUnmarksRowAbove(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1", "bd-2")
	m.tree.SelectByID("bd-3")

	m, _ = pressBulkKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})

	if got := m.tree.TreeMarkedIDs(); !reflect.DeepEqual(got, []string{"bd-1"}) {
		t.Fatalf("backspace must unmark the row above, got %v", got)
	}
	if got := m.tree.GetSelectedID(); got != "bd-2" {
		t.Fatalf("backspace must move the cursor up, cursor on %q", got)
	}
}

func TestUppercaseUClearsMarks(t *testing.T) {
	m := markRows(t, newBulkMarkModel(t), "bd-1", "bd-3")
	m.updateAvailable = true
	m.updateTag = "v9.9.9"

	m, _ = pressBulkKey(t, m, runeKey("U"))

	if got := m.tree.MarkedCount(); got != 0 {
		t.Fatalf("U must clear every mark, %d remain", got)
	}
	if m.showUpdateModal {
		t.Fatalf("U must not open the update dialog")
	}
}
