package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"codetree/render/fixture"
)

func init() {
	// go test's stdout is not a TTY; force 256-color so chrome renders ANSI.
	lipgloss.SetColorProfile(termenv.ANSI256)
}

// TestInitialView proves the picker (entry view) renders without a TTY.
func TestInitialView(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)
	if m.mode != modePicker {
		t.Fatalf("entry mode = %v, want picker", m.mode)
	}
	out := m.View()
	for _, want := range []string{"Select files", "animal.py", "zoo.py", "PICKER", "▶"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker view missing %q:\n%s", want, out)
		}
	}
	// cursor row carries a full-width background highlight (ANSI 237)
	if !strings.Contains(out, "237") {
		t.Error("picker cursor row should have bg highlight")
	}
}

// TestTreeView switches to the tree view with t and checks rendering.
func TestTreeView(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = mi.(model)
	if m.mode != modeTree {
		t.Fatal("t should switch to tree mode")
	}
	out := m.View()
	for _, want := range []string{"myproject/", "animal.py", "Animal (class)", "speak(self)"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree view missing %q:\n%s", want, out)
		}
	}
}

// TestPickerFlow covers filter, mark, and confirm-into-diagram.
func TestPickerFlow(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)

	// filter narrows the list
	m.filter.SetValue("animal")
	if vis := m.pickerVisible(); len(vis) != 1 || vis[0].path != "models/animal.py" {
		t.Fatalf("filtered = %+v", vis)
	}

	// space marks the cursor file
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mi.(model)
	if !m.marked["models/animal.py"] {
		t.Fatal("space should mark cursor file")
	}

	// enter opens the diagram scoped to marked files
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if m.mode != modeDiagram {
		t.Fatal("enter should open diagram")
	}
	if len(m.dopts.Files) != 1 || m.dopts.Files[0] != "models/animal.py" {
		t.Fatalf("scope = %v", m.dopts.Files)
	}
	out := m.View()
	if !strings.Contains(out, "Animal") || !strings.Contains(out, "Dog") {
		t.Errorf("scoped diagram should show protagonists:\n%s", out)
	}
	if strings.Contains(out, "unrelated") {
		t.Error("file scope must not show the unrelated partition")
	}

	// esc returns to picker with marks preserved
	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(model)
	if m.mode != modePicker || !m.marked["models/animal.py"] {
		t.Error("esc should return to picker with marks preserved")
	}
}

func TestFilterAndFold(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)

	// fuzzy filter narrows visible rows to matches + ancestors
	m.filter.SetValue("fetch")
	m.reflow()
	var labels []string
	for _, r := range m.rows {
		labels = append(labels, r.label)
	}
	joined := strings.Join(labels, "|")
	if !strings.Contains(joined, "fetch(self)") || strings.Contains(joined, "zoo.py") {
		t.Errorf("filtered rows = %v", labels)
	}

	// collapse hides children
	m.filter.SetValue("")
	m.reflow()
	var dog *node
	for _, r := range m.rows {
		if strings.HasPrefix(r.label, "Dog") {
			dog = r
		}
	}
	if dog == nil {
		t.Fatal("Dog node not found")
	}
	dog.expanded = false
	m.reflow()
	count := 0
	for _, r := range m.rows {
		if r.label == "fetch(self)" {
			count++
		}
	}
	if count != 0 {
		t.Error("collapsed Dog should hide fetch")
	}
}
