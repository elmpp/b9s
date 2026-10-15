package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/vanderheijden86/b9s/pkg/model"
)

type advertisedShortcut struct {
	section, key, desc string
}

func advertisedShortcuts(t *testing.T) []advertisedShortcut {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "model.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var rows []advertisedShortcut
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "renderHelpOverlay" {
			return true
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			name, ok := assign.Lhs[0].(*ast.Ident)
			if !ok || !strings.HasSuffix(name.Name, "Section") || name.Name == "statusSection" {
				return true
			}
			list, ok := assign.Rhs[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range list.Elts {
				row := elt.(*ast.CompositeLit)
				key, _ := strconv.Unquote(row.Elts[0].(*ast.BasicLit).Value)
				desc, _ := strconv.Unquote(row.Elts[1].(*ast.BasicLit).Value)
				rows = append(rows, advertisedShortcut{name.Name, key, desc})
			}
			return true
		})
		return false
	})
	if len(rows) == 0 {
		t.Fatal("no help shortcuts found in renderHelpOverlay")
	}
	return rows
}

// Only literals compared with a key expression count. A status or config
// string in the same function cannot supply a missing shortcut handler.
func handlerKeys(t *testing.T, filename, function string) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	isKey := func(expr ast.Expr) bool {
		if id, ok := expr.(*ast.Ident); ok {
			return id.Name == "key"
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "String" {
			return false
		}
		id, ok := sel.X.(*ast.Ident)
		return ok && id.Name == "msg"
	}
	add := func(expr ast.Expr) {
		if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			key, err := strconv.Unquote(lit.Value)
			if err == nil {
				keys[key] = true
			}
		}
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.SwitchStmt:
				if isKey(node.Tag) {
					for _, stmt := range node.Body.List {
						for _, expr := range stmt.(*ast.CaseClause).List {
							add(expr)
						}
					}
				}
			case *ast.BinaryExpr:
				if node.Op == token.EQL {
					if isKey(node.X) {
						add(node.Y)
					}
					if isKey(node.Y) {
						add(node.X)
					}
				}
			}
			return true
		})
	}
	return keys
}

func helpKeyNames(label string) []string {
	if label == "/" {
		return []string{"/"}
	}
	if label == "F1-F4" {
		return []string{"f1", "f2", "f3", "f4"}
	}
	if strings.HasPrefix(label, ":") {
		return []string{":"}
	}
	aliases := map[string]string{
		"↓": "down", "↑": "up", "←": "left", "→": "right",
		"PgDn": "pgdown", "PgUp": "pgup", "Bksp": "backspace",
		"^Space": "ctrl+@", "^\\": "ctrl+\\",
	}
	var keys []string
	for _, key := range strings.Split(label, "/") {
		key = strings.TrimSpace(key)
		if alias, ok := aliases[key]; ok {
			key = alias
		} else if len(key) > 1 {
			key = strings.ToLower(key)
		}
		keys = append(keys, key)
	}
	return keys
}

func TestHelpEveryShortcutHasHandler(t *testing.T) {
	global := handlerKeys(t, "model.go", "dispatchMsg")
	tree := handlerKeys(t, "model.go", "handleTreeKeys")
	help := handlerKeys(t, "model.go", "handleHelpKeys")
	editor := handlerKeys(t, "edit_modal.go", "Update")
	for _, row := range advertisedShortcuts(t) {
		t.Run(row.section+"/"+row.key, func(t *testing.T) {
			for _, key := range helpKeyNames(row.key) {
				handled := global[key]
				if _, ok := typeForFunctionKey(key); ok {
					handled = true
				}
				switch row.section {
				case "treeSection", "filterSection", "navSection", "actionsSection":
					handled = handled || tree[key]
				case "editingSection":
					handled = handled || editor[key]
				case "globalSection":
					handled = handled || help[key]
				}
				if !handled {
					t.Errorf("model.go %s: %q (%s) has no key handler in its advertised context", row.section, key, row.desc)
				}
			}
		})
	}
}

func TestHelpHandlerGuardRejectsUnrelatedLiterals(t *testing.T) {
	global := handlerKeys(t, "model.go", "dispatchMsg")
	for _, key := range []string{"off", "B", "!", ";", "'"} {
		if global[key] {
			t.Errorf("%q has no global key handler", key)
		}
	}
	if !handlerKeys(t, "model.go", "handleTreeKeys")["B"] {
		t.Fatal("B must be collected only from the tree handler")
	}
}

func TestHelpViewsFollowModelRouting(t *testing.T) {
	for _, row := range advertisedShortcuts(t) {
		if row.section != "viewsSection" {
			continue
		}
		t.Run(row.key, func(t *testing.T) {
			m := NewModel([]model.Issue{{ID: "one", Title: "One", Status: model.StatusOpen}}, "")
			m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(row.key)})
			switch row.desc {
			case "Tree view":
				if m.focused != focusTree {
					t.Fatal("tree shortcut did not focus the tree")
				}
			case "Kanban board":
				if !m.isBoardView || m.focused != focusBoard {
					t.Fatal("board shortcut did not open the board")
				}
			case "Dependency graph":
				if !m.isGraphView || m.focused != focusGraph {
					t.Fatal("graph shortcut did not open the graph")
				}
			default:
				t.Fatalf("help advertises unsupported view %q", row.desc)
			}
		})
	}
}

func TestHelpReachableListClaims(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "one", Title: "One", Status: model.StatusOpen}}, "")
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if m.focused != focusTree || !m.tree.IsFlatMode() {
		t.Fatal("t must reach the tree's flat list")
	}
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if m.currentFilter != "closed" {
		t.Fatal("C must filter closed issues in the reachable list")
	}
	for _, row := range advertisedShortcuts(t) {
		if row.key == "b/B" || row.desc == "Copy to clipboard" ||
			strings.Contains(row.desc, "(list)") {
			t.Errorf("unreachable help claim: %q %s", row.key, row.desc)
		}
	}
}

func TestHelpCopyDescriptionsMatchContext(t *testing.T) {
	issue := model.Issue{
		ID: "bd-copy-help", Title: "Copy help", Status: model.StatusOpen,
		Description: "Full description", Notes: "Progress notes",
		Comments: []*model.Comment{{Author: "alex", Text: "A useful comment"}},
	}
	for _, tc := range []struct {
		name, section, description string
		detail                     bool
	}{
		{"tree", "treeSection", "Copy ID and title", false},
		{"detail", "actionsSection", "Copy full Markdown (detail)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var row *advertisedShortcut
			for _, candidate := range advertisedShortcuts(t) {
				if candidate.section == tc.section && candidate.key == "c" {
					row = &candidate
					break
				}
			}
			if row == nil {
				t.Fatalf("%s has no c copy shortcut", tc.section)
			}
			m := NewModel([]model.Issue{issue}, "")
			if tc.detail {
				m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
				if m.focused != focusDetail {
					t.Fatal("Enter must focus the detail view")
				}
			}
			var copied string
			m.clipboardWrite = func(text string) error {
				copied = text
				return nil
			}
			m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(row.key)})
			if tc.detail {
				for _, want := range []string{"### Description\nFull description", "### Notes\nProgress notes", "### Comments (1)", "A useful comment"} {
					if !strings.Contains(copied, want) {
						t.Errorf("detail copy must contain %q, got %q", want, copied)
					}
				}
			} else if want := issue.ID + " " + issue.Title; copied != want {
				t.Errorf("tree copy = %q, want %q", copied, want)
			}
			if row.desc != tc.description {
				t.Errorf("%s copy help = %q, want %q for this clipboard content", tc.name, row.desc, tc.description)
			}
		})
	}
}

func openHelpOverlay(t *testing.T) Model {
	t.Helper()
	m := NewModel([]model.Issue{{ID: "1", Title: "One", Status: model.StatusOpen}}, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 120})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(Model)
	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("expected help overlay shown")
	}
	return m
}

func sendKeys(m Model, msgs ...tea.KeyMsg) Model {
	for _, msg := range msgs {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

func typeText(m Model, text string) Model {
	for _, r := range text {
		m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestHelpSearch_CtrlSOpensSearchInput(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("ctrl+s must keep the help overlay open")
	}
	if !m.helpSearching {
		t.Fatalf("ctrl+s must start help search")
	}
}

func TestHelpSearch_FiltersShortcutsByDescription(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "graph")

	view := m.View()
	if !strings.Contains(view, "Dependency graph") {
		t.Fatalf("expected matching shortcut in view, got:\n%s", view)
	}
	if strings.Contains(view, "Kanban board") {
		t.Fatalf("expected non-matching shortcut hidden, got:\n%s", view)
	}
}

func TestHelpSearch_FiltersShortcutsByKey(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "ctrl+r")

	view := m.View()
	if !strings.Contains(view, "Force refresh") {
		t.Fatalf("expected shortcut matched by key in view, got:\n%s", view)
	}
	if strings.Contains(view, "Dependency graph") {
		t.Fatalf("expected non-matching shortcut hidden, got:\n%s", view)
	}
}

func TestHelpSearch_TypingHelpToggleKeyDoesNotCloseHelp(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "?`:x")

	if !m.showHelp || m.focused != focusHelp {
		t.Fatalf("keys typed into the search must not close help")
	}
	if got := m.helpSearchInput.Value(); got != "?`:x" {
		t.Fatalf("expected query %q, got %q", "?`:x", got)
	}
}

func TestHelpSearch_NoMatchesSaysSo(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "zzzqqq")

	if view := m.View(); !strings.Contains(view, "No shortcuts match") {
		t.Fatalf("expected no-match notice, got:\n%s", view)
	}
}

func TestHelpSearch_EnterKeepsFilterAndStopsTyping(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "graph")
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.helpSearching {
		t.Fatalf("enter must stop editing the search")
	}
	if !m.showHelp {
		t.Fatalf("enter must keep help open")
	}
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if !m.showHelp {
		t.Fatalf("j must scroll, not dismiss, a filtered help overlay")
	}
	if view := m.View(); strings.Contains(view, "Kanban board") {
		t.Fatalf("filter must survive enter, got:\n%s", view)
	}
}

func TestHelpSearch_EscClearsFilterBeforeClosingHelp(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "graph")

	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.showHelp || m.helpSearching || m.helpSearchInput.Value() != "" {
		t.Fatalf("first esc must clear the search and keep help open")
	}
	if view := m.View(); !strings.Contains(view, "Kanban board") {
		t.Fatalf("clearing the search must show every shortcut again")
	}

	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.showHelp {
		t.Fatalf("second esc must close help")
	}
}

func TestHelpSearch_ReopeningHelpStartsUnfiltered(t *testing.T) {
	m := openHelpOverlay(t)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = typeText(m, "graph")
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.showHelp {
		t.Fatalf("? must close a help overlay that is not being searched")
	}

	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.helpSearchInput.Value() != "" {
		t.Fatalf("reopened help must start without a filter")
	}
}
