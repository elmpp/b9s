package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/vanderheijden86/b9s/pkg/model"
)

func priorityFixture() model.Issue {
	return model.Issue{
		ID: "bd-p1", Title: "Priority fixture", Status: model.StatusOpen,
		IssueType: model.TypeTask, Priority: 1, CreatedAt: time.Now(),
	}
}

func TestTreeColumnPopup_ListsPriority(t *testing.T) {
	tree := NewTreeModel(newTreeTestTheme())
	tree.OpenColumnPopup()
	if popup := stripANSI(tree.RenderColumnPopup()); !strings.Contains(popup, "Priority") {
		t.Fatalf("column popup missing Priority: %q", popup)
	}
}

func TestTreePriorityColumn_HiddenByDefault(t *testing.T) {
	tree := NewTreeModel(newTreeTestTheme())
	tree.Build([]model.Issue{priorityFixture()})
	tree.SetSize(160, 10)
	if strings.Contains(stripANSI(tree.View()), "PRI") {
		t.Fatal("priority column should be hidden unless asked for")
	}
}

func TestTreePriorityColumn_ShowRendersPriority(t *testing.T) {
	tree := NewTreeModel(newTreeTestTheme())
	tree.Build([]model.Issue{priorityFixture()})
	tree.SetSize(160, 10)
	tree.SetColumnPreference(TreeColumnPriority, ColumnShow)
	view := stripANSI(tree.View())
	if !strings.Contains(view, "PRI") || !strings.Contains(view, "P1") {
		t.Fatalf("priority column not rendered:\n%s", view)
	}
}

func TestTreePriorityColumn_PopupResolvesAutoAsHidden(t *testing.T) {
	tree := NewTreeModel(newTreeTestTheme())
	tree.Build([]model.Issue{priorityFixture()})
	tree.SetSize(160, 10)
	tree.OpenColumnPopup()
	for _, line := range strings.Split(stripANSI(tree.RenderColumnPopup()), "\n") {
		if strings.Contains(line, "Priority") && !strings.Contains(line, "Auto (hidden)") {
			t.Fatalf("Priority should resolve to Auto (hidden): %q", line)
		}
	}
}
