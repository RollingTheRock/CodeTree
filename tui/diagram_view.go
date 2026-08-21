package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"codetree/diagram"
)

func (m model) updateDiagram(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "t":
		m.mode = modeTree
		return m, nil
	case "h", "left":
		m.moveSel(-1, 0)
	case "l", "right":
		m.moveSel(1, 0)
	case "k", "up":
		m.moveSel(0, -1)
	case "j", "down":
		m.moveSel(0, 1)
	case "enter": // neighborhood mode on the selected class
		if m.sel != "" {
			m.dopts.Focus = m.sel
		}
	case "esc":
		if m.dopts.Focus != "" {
			m.dopts.Focus = "" // leave neighborhood first
		} else {
			m.mode = modePicker // back to picker, marks preserved
			return m, nil
		}
	case "A": // all-project scope
		m.dopts.Files = nil
	case "+", "=":
		m.dopts.Down++
	case "-":
		if m.dopts.Down > 0 {
			m.dopts.Down--
		}
	case "b":
		m.dopts.Siblings = !m.dopts.Siblings
	case "c": // toggle composition edges
		m.dopts.Assoc = !m.dopts.Assoc
	case "m":
		m.dopts.Members = !m.dopts.Members
	case "?":
		m.showHelp = true
		return m, nil
	case "o":
		if n := m.selNode(); n != nil && n.Sym != nil {
			return m, m.openEditorAt(n.Sym.File, n.Sym.Line)
		}
		return m, nil
	}
	m.rebuildDiagram()
	return m, nil
}

// rebuildDiagram re-renders the class diagram with current options and
// keeps the selection visible. Highlight is style-only (canvas.go), so a
// single Build suffices: layout and node set are identical with or without
// it. Only when the selection vanished and falls back to a default do we
// re-render once with the new highlight.
func (m *model) rebuildDiagram() {
	if m.proj == nil {
		return
	}
	opts := m.dopts
	opts.Color = true
	if m.width > 4 {
		opts.WrapWidth = m.width - 2
	}
	d, err := diagram.Build(m.proj, diagramOpts(opts, m.sel))
	if err != nil {
		return
	}
	// validate selection against the (possibly filtered) node set
	found := false
	for i, n := range d.Nodes {
		if n.Name == m.sel {
			m.selIdx = i
			found = true
			break
		}
	}
	if m.sel == "" || !found {
		if len(d.Nodes) > 0 {
			// default selection: first protagonist (not a dimmed context or
			// external box), falling back to the first node
			m.selIdx = 0
			for i, n := range d.Nodes {
				if !n.Context && !n.External {
					m.selIdx = i
					break
				}
			}
			if d.Nodes[m.selIdx].Name != m.sel {
				m.sel = d.Nodes[m.selIdx].Name
				d, _ = diagram.Build(m.proj, diagramOpts(opts, m.sel))
			}
		} else {
			m.selIdx = 0
			m.sel = ""
		}
	}
	m.diag = d
	m.treeVP.SetContent(m.diag.Text)
	m.scrollToSel()
}

func diagramOpts(o diagram.Options, highlight string) diagram.Options {
	o.Highlight = highlight
	return o
}

func (m *model) selNode() *diagram.PlacedNode {
	if m.diag == nil || m.selIdx < 0 || m.selIdx >= len(m.diag.Nodes) {
		return nil
	}
	return &m.diag.Nodes[m.selIdx]
}

// moveSel moves the selection along the layout skeleton (tidy-tree
// parent/child links only; implements/composition edges don't navigate):
//
//	j → leftmost child;   k → parent;   leaf/root: no-op
//	h/l → sibling by visual X; at sibling edge → adjacent tree's root;
//	forest edge: no-op. Orphan boxes are roots of single-node trees.
func (m *model) moveSel(dx, dy int) {
	d := m.diag
	if d == nil || len(d.Nodes) == 0 {
		return
	}
	cur := m.selIdx
	if cur < 0 || cur >= len(d.Nodes) {
		cur = 0
	}
	next := cur
	switch {
	case dy > 0: // j: down to leftmost child
		if kids := d.Nodes[cur].Children; len(kids) > 0 {
			next = kids[0]
		}
	case dy < 0: // k: up to parent
		if p := d.Nodes[cur].Parent; p >= 0 {
			next = p
		}
	case dx != 0: // h/l: siblings, then adjacent tree roots
		if p := d.Nodes[cur].Parent; p >= 0 {
			sibs := d.Nodes[p].Children
			i := indexOfInt(sibs, cur)
			if dx < 0 && i > 0 {
				next = sibs[i-1]
			} else if dx > 0 && i >= 0 && i < len(sibs)-1 {
				next = sibs[i+1]
			} else {
				next = m.adjacentRoot(cur, dx)
			}
		} else {
			next = m.adjacentRoot(cur, dx)
		}
	}
	if next != cur {
		m.selIdx = next
		m.sel = d.Nodes[next].Name
	}
}

// adjacentRoot returns the root of the next/previous tree in visual X order.
func (m *model) adjacentRoot(cur, dx int) int {
	d := m.diag
	var roots []int
	for i, n := range d.Nodes {
		if n.Parent == -1 {
			roots = append(roots, i)
		}
	}
	sort.Slice(roots, func(a, b int) bool { return d.Nodes[roots[a]].X < d.Nodes[roots[b]].X })
	myRoot := cur
	for d.Nodes[myRoot].Parent >= 0 {
		myRoot = d.Nodes[myRoot].Parent
	}
	i := indexOfInt(roots, myRoot)
	if dx < 0 && i > 0 {
		return roots[i-1]
	}
	if dx > 0 && i >= 0 && i < len(roots)-1 {
		return roots[i+1]
	}
	return cur
}

func indexOfInt(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func (m *model) scrollToSel() {
	n := m.selNode()
	if n == nil {
		return
	}
	xoff := m.xoff
	if n.X < xoff {
		xoff = n.X
	}
	if n.X+n.W > xoff+m.treeVP.Width {
		xoff = n.X + n.W - m.treeVP.Width
	}
	m.xoff = xoff
	m.treeVP.SetXOffset(xoff)
	if n.Y < m.treeVP.YOffset {
		m.treeVP.YOffset = n.Y
	}
	if n.Y+n.H > m.treeVP.YOffset+m.treeVP.Height {
		m.treeVP.YOffset = n.Y + n.H - m.treeVP.Height
	}
}
