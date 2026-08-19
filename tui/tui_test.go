package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"codetree/render/fixture"
)

// TestInitialView proves the browser renders without an interactive TTY.
func TestInitialView(t *testing.T) {
	m := newModel(fixture.Project())
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mi.(model)
	out := m.View()
	for _, want := range []string{"myproject/", "animal.py", "Animal (class)", "speak(self)", "j/k move"} {
		if !strings.Contains(out, want) {
			t.Errorf("initial view missing %q:\n%s", want, out)
		}
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
