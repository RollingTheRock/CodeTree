package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"codetree/core"
	"codetree/render/fixture"
)

// enterDiagram drives the picker: confirm with no marks = cursor file scope.
func enterDiagram(t *testing.T, m model) model {
	t.Helper()
	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if m.mode != modeDiagram {
		t.Fatal("enter should switch to diagram mode")
	}
	return m
}

// TestDiagramView proves the class diagram mode renders and focus works.
func TestDiagramView(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)

	m = enterDiagram(t, m) // scope: models/animal.py (first picker row)
	out := m.View()
	for _, want := range []string{"┌", "Animal", "▲", "DIAGRAM", "animal.py"} {
		if !strings.Contains(out, want) {
			t.Errorf("diagram view missing %q:\n%s", want, out)
		}
	}
	if m.sel == "" {
		t.Fatal("expected an initial selection")
	}

	// move selection and enter neighborhood mode
	m.sel = "Dog"
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if m.dopts.Focus != "Dog" {
		t.Fatalf("enter should focus selected class, got %q", m.dopts.Focus)
	}
	out = m.View()
	if strings.Contains(out, "unrelated") {
		t.Error("focus mode should hide orphans")
	}

	// members toggle
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = mi.(model)
	if !m.dopts.Members {
		t.Error("m should toggle members")
	}

	// A switches to all-project scope
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = mi.(model)
	if m.dopts.Files != nil {
		t.Error("A should clear file scope")
	}

	// t goes to tree view
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = mi.(model)
	if m.mode != modeTree {
		t.Error("t should switch to tree mode")
	}
}

// TestHelpOverlay covers the ? overlay: opens over any mode, closes on any key.
func TestHelpOverlay(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)

	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = mi.(model)
	if !m.showHelp {
		t.Fatal("? should open help")
	}
	out := m.View()
	for _, want := range []string{"ct key bindings", "PICKER", "DIAGRAM", "TREE", "GENERAL"} {
		if !strings.Contains(out, want) {
			t.Errorf("help overlay missing %q:\n%s", want, out)
		}
	}

	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = mi.(model)
	if m.showHelp {
		t.Error("any key should close help")
	}
}

// TestFocusBadge: entering neighborhood mode switches the status bar badge.
func TestFocusBadge(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)
	m = enterDiagram(t, m)
	if !strings.Contains(m.View(), " DIAGRAM ") {
		t.Fatal("diagram badge missing")
	}
	m.sel = "Dog"
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	out := m.View()
	if !strings.Contains(out, " FOCUS ") || !strings.Contains(out, "Dog") {
		t.Errorf("focus badge missing:\n%s", out)
	}
}

// TestPickerFilterMode covers fzf-style keys inside filter mode:
// arrows move, tab marks (and advances), esc keeps the query, enter confirms.
func TestPickerFilterMode(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)

	key := func(s string) {
		mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = mi.(model)
	}
	special := func(k tea.KeyType) {
		mi, _ = m.Update(tea.KeyMsg{Type: k})
		m = mi.(model)
	}

	key("/") // enter filter mode
	if !m.filtering {
		t.Fatal("/ should enter filter mode")
	}
	// arrows move within filtered results
	special(tea.KeyDown)
	if m.pCursor != 1 {
		t.Fatalf("down in filter: pCursor = %d", m.pCursor)
	}
	special(tea.KeyUp)
	if m.pCursor != 0 {
		t.Fatalf("up in filter: pCursor = %d", m.pCursor)
	}
	// tab marks current row and advances
	special(tea.KeyTab)
	if !m.marked["models/animal.py"] {
		t.Fatalf("tab should mark cursor row, marked = %v", m.marked)
	}
	if m.pCursor != 1 {
		t.Fatalf("tab should advance cursor, pCursor = %d", m.pCursor)
	}
	special(tea.KeyTab) // mark zoo.py too
	if len(m.marked) != 2 {
		t.Fatalf("marked = %v", m.marked)
	}
	// space types a space into the query (not a mark)
	key(" ")
	if m.filter.Value() != " " {
		t.Errorf("space should go to filter, got %q", m.filter.Value())
	}
	m.filter.SetValue("")
	// enter confirms with the marked set
	special(tea.KeyEnter)
	if m.mode != modeDiagram {
		t.Fatal("enter should open diagram")
	}
	if len(m.dopts.Files) != 2 {
		t.Errorf("scope = %v, want both marked files", m.dopts.Files)
	}
	if m.filtering {
		t.Error("filter mode should exit on confirm")
	}
}

func TestDiagramNavigation(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: "a.py", Symbols: []*core.Symbol{
			{Name: "A", Kind: core.KindClass, File: "a.py"},
			{Name: "B", Kind: core.KindClass, SuperTypes: []string{"A"}, File: "a.py"},
			{Name: "C", Kind: core.KindClass, SuperTypes: []string{"A"}, File: "a.py"},
		}}},
	}
	m := newModel(p)
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)
	m = enterDiagram(t, m)

	m.sel = "A"
	m.moveSel(0, 1) // down to B or C
	if m.sel != "B" && m.sel != "C" {
		t.Errorf("down from A → %q, want B or C", m.sel)
	}
	from := m.sel
	m.moveSel(0, -1) // back up to A
	if m.sel != "A" {
		t.Errorf("up from %s → %q, want A", from, m.sel)
	}
}
