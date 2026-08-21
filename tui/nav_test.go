package tui

import (
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
)

// navFixture builds: tree1 A→(B, C), tree2 X→Y, orphan O.
func navFixture() *core.Project {
	f := "a.py"
	return &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: f, Lang: "python", Symbols: []*core.Symbol{
			{Name: "A", Kind: core.KindClass, File: f},
			{Name: "B", Kind: core.KindClass, SuperTypes: []string{"A"}, File: f},
			{Name: "C", Kind: core.KindClass, SuperTypes: []string{"A"}, File: f},
			{Name: "X", Kind: core.KindClass, File: f},
			{Name: "Y", Kind: core.KindClass, SuperTypes: []string{"X"}, File: f},
			{Name: "O", Kind: core.KindClass, File: f},
		}}},
	}
}

func newNavModel(t *testing.T, p *core.Project) model {
	t.Helper()
	m := newModel(p)
	m.mode = modeDiagram
	m.dopts.Files = nil // all-project
	m.width, m.height = 120, 40
	m.rebuildDiagram()
	if m.diag == nil || len(m.diag.Nodes) == 0 {
		t.Fatal("empty diagram")
	}
	return m
}

func (m *model) selectName(name string) {
	for i, n := range m.diag.Nodes {
		if n.Name == name {
			m.selIdx = i
			m.sel = name
			return
		}
	}
	panic("no such node: " + name)
}

func TestStructuralNavigation(t *testing.T) {
	m := newNavModel(t, navFixture())

	seq := []struct {
		from, key, want string
	}{
		{"A", "j", "B"}, // down to leftmost child
		{"B", "l", "C"}, // sibling right
		{"C", "l", "X"}, // sibling edge → next tree root
		{"X", "j", "Y"}, // down tree2
		{"Y", "k", "X"}, // up
		{"X", "h", "A"}, // root: previous tree root
		{"A", "h", "O"}, // orphan partition sits at x=0: O is the leftmost root
		{"O", "h", "O"}, // forest edge: no-op
		{"A", "k", "A"}, // root: no-op
		{"B", "j", "B"}, // leaf: no-op
	}
	for _, s := range seq {
		m.selectName(s.from)
		switch s.key {
		case "j":
			m.moveSel(0, 1)
		case "k":
			m.moveSel(0, -1)
		case "h":
			m.moveSel(-1, 0)
		case "l":
			m.moveSel(1, 0)
		}
		if m.sel != s.want {
			t.Errorf("%s --%s--> %s, want %s", s.from, s.key, m.sel, s.want)
		}
	}

	// orphan is a root: k is a no-op; it joins the adjacent-root sequence
	m.selectName("O")
	m.moveSel(0, -1)
	if m.sel != "O" {
		t.Errorf("orphan root k → %s, want O", m.sel)
	}
	m.moveSel(1, 0) // O (x=0) → next root right: A
	if m.sel != "A" {
		t.Errorf("orphan O --l--> %s, want A (next tree root)", m.sel)
	}
}

func TestFocusModeNavigation(t *testing.T) {
	m := newNavModel(t, navFixture())
	m.dopts.Focus = "A"
	m.dopts.Up = -1
	m.dopts.Down = 1
	m.rebuildDiagram()

	// subgraph: A + B + C (X/Y/O filtered out)
	m.selectName("A")
	m.moveSel(0, 1) // j → leftmost child B
	if m.sel != "B" {
		t.Errorf("focus A --j--> %s, want B", m.sel)
	}
	m.moveSel(1, 0) // l → sibling C
	if m.sel != "C" {
		t.Errorf("focus B --l--> %s, want C", m.sel)
	}
	m.moveSel(1, 0) // sibling edge; no other tree in subgraph → no-op
	if m.sel != "C" {
		t.Errorf("focus C --l--> %s, want C (no adjacent tree)", m.sel)
	}
	m.moveSel(0, -1) // k → parent A
	if m.sel != "A" {
		t.Errorf("focus C --k--> %s, want A", m.sel)
	}
}
