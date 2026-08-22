package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/RollingTheRock/CodeTree/core"
)

type node struct {
	label    string
	sym      *core.Symbol // nil for dir/file nodes
	file     string       // relative path for open-in-editor
	line     int
	children []*node
	expanded bool
	isDir    bool
	isFile   bool

	// render caches, filled once at build time: the styled label (icon
	// included for symbols) and its display width. Frames never re-style.
	rendered string
	plainW   int
}

// renderLabel fills the render caches according to the node kind.
func (n *node) renderLabel() {
	switch {
	case n.isDir:
		n.rendered = styleDir.Render(n.label)
		n.plainW = lipgloss.Width(n.label)
	case n.isFile:
		n.rendered = styleFile.Render(n.label)
		n.plainW = lipgloss.Width(n.label)
	case n.sym != nil:
		n.rendered = symIcon(n.sym.Kind) + " " + n.label
		n.plainW = 2 + lipgloss.Width(n.label)
	default:
		n.rendered = n.label
		n.plainW = lipgloss.Width(n.label)
	}
}

func buildTree(p *core.Project) *node {
	root := &node{label: filepath.Base(p.Root) + "/", expanded: true, isDir: true}
	root.renderLabel()
	dirs := map[string]*node{"": root}
	for _, f := range p.Files {
		parts := strings.Split(f.Path, "/")
		parent := root
		acc := ""
		for _, d := range parts[:len(parts)-1] {
			acc += d + "/"
			child, ok := dirs[acc]
			if !ok {
				child = &node{label: d + "/", expanded: true, isDir: true}
				child.renderLabel()
				dirs[acc] = child
				parent.children = append(parent.children, child)
			}
			parent = child
		}
		fn := &node{label: parts[len(parts)-1], file: f.Path, expanded: true, isFile: true}
		fn.renderLabel()
		for _, s := range f.Symbols {
			fn.children = append(fn.children, symNode(f.Path, s))
		}
		parent.children = append(parent.children, fn)
	}
	sortTree(root)
	return root
}

func symNode(file string, s *core.Symbol) *node {
	n := &node{label: label(s), sym: s, file: file, line: s.Line, expanded: true}
	n.renderLabel()
	for _, c := range s.Children {
		n.children = append(n.children, symNode(file, c))
	}
	return n
}

func label(s *core.Symbol) string { return s.Label() }

func sortTree(n *node) {
	sort.SliceStable(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		rank := func(x *node) int {
			if x.isDir {
				return 0
			}
			if x.isFile {
				return 1
			}
			return 2
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		return a.label < b.label
	})
	for _, c := range n.children {
		sortTree(c)
	}
}

// reflow recomputes the visible row list (and per-row depth) from expansion
// + filter state. Any structural change marks the line cache dirty.
func (m *model) reflow() {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.rows = m.rows[:0]
	m.rowDepth = m.rowDepth[:0]
	var walk func(n *node, depth int, forceExpand bool)
	walk = func(n *node, depth int, forceExpand bool) {
		m.rows = append(m.rows, n)
		m.rowDepth = append(m.rowDepth, depth)
		if !n.expanded && !forceExpand {
			return
		}
		for _, c := range n.children {
			if query != "" && !visibleUnder(c, query) {
				continue
			}
			walk(c, depth+1, query != "")
		}
	}
	walk(m.root, 0, false)
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.cc.dirty = true
}

// visibleUnder reports whether n or any descendant matches the query.
func visibleUnder(n *node, query string) bool {
	if strings.Contains(strings.ToLower(n.label), query) {
		return true
	}
	for _, c := range n.children {
		if visibleUnder(c, query) {
			return true
		}
	}
	return false
}

func (m *model) expand() {
	n := m.current()
	if n == nil {
		return
	}
	if len(n.children) > 0 && !n.expanded {
		n.expanded = true
		m.reflow()
	}
}

func (m *model) collapse() {
	n := m.current()
	if n == nil {
		return
	}
	if n.expanded && len(n.children) > 0 {
		n.expanded = false
		m.reflow()
		return
	}
	// already collapsed: jump to parent
	for i := m.cursor - 1; i >= 0; i-- {
		if containsNode(m.rows[i], n) {
			m.cursor = i
			return
		}
	}
}

func containsNode(root, target *node) bool {
	for _, c := range root.children {
		if c == target || containsNode(c, target) {
			return true
		}
	}
	return false
}

func (m *model) toggle() {
	n := m.current()
	if n != nil && len(n.children) > 0 {
		n.expanded = !n.expanded
		m.reflow()
	}
}

// ---- cached line rendering --------------------------------------------------

// cursorOn/cursorOff are the fixed-width (2 cells) cursor prefixes.
var (
	cursorOn  = styleCursor.Render("▶ ")
	cursorOff = "  "
)

// treeLine renders row i from cached parts: concatenation only, no lipgloss.
func (m *model) treeLine(i int) string {
	n := m.rows[i]
	d := m.rowDepth[i] - 1
	if d < 0 {
		d = 0
	}
	marker := "  "
	if len(n.children) > 0 {
		if n.expanded {
			marker = "▾ "
		} else {
			marker = "▸ "
		}
	}
	cur := cursorOff
	if i == m.cursor {
		cur = cursorOn
	}
	return m.cc.padStr + cur + strings.Repeat("  ", d) + marker + n.rendered
}

// treeLineW computes the display width of row i from cached widths.
func (m *model) treeLineW(i int) int {
	d := m.rowDepth[i] - 1
	if d < 0 {
		d = 0
	}
	return 2 + 2*d + 2 + m.rows[i].plainW // cursor + indent + marker + label
}

// syncTree rebuilds the line cache when dirty, otherwise patches just the
// old/new cursor lines, then re-joins the content string. Called from View
// via the shared cache pointer.
func (m *model) syncTree() {
	cc := m.cc
	if cc.mode != modeTree {
		cc.mode, cc.dirty = modeTree, true
	}
	if cc.dirty || len(cc.lines) != len(m.rows) {
		// full rebuild: compute the centering pad from cached widths
		maxw := 0
		for i := range m.rows {
			if w := m.treeLineW(i); w > maxw {
				maxw = w
			}
		}
		cc.pad = (m.width - maxw) / 2
		if cc.pad < 0 {
			cc.pad = 0
		}
		cc.padStr = strings.Repeat(" ", cc.pad)
		cc.lines = make([]string, len(m.rows))
		for i := range m.rows {
			cc.lines[i] = m.treeLine(i)
		}
		cc.dirty = false
		cc.cursor = m.cursor
	} else if cc.cursor != m.cursor {
		if cc.cursor >= 0 && cc.cursor < len(cc.lines) {
			cc.lines[cc.cursor] = m.treeLine(cc.cursor)
		}
		if m.cursor < len(cc.lines) {
			cc.lines[m.cursor] = m.treeLine(m.cursor)
		}
		cc.cursor = m.cursor
	} else {
		return // nothing changed: keep the joined content as-is
	}
	cc.content = strings.Join(cc.lines, "\n")
	cc.gen++
}

// symIcon returns the colored single-character kind icon used in the tree
// view, matching the diagram card icons (C/I/S/E/m/f).
func symIcon(k core.Kind) string {
	type iconStyle struct {
		icon  string
		color lipgloss.Color
	}
	icons := map[core.Kind]iconStyle{
		core.KindClass:     {"C", "51"},
		core.KindInterface: {"I", "78"},
		core.KindStruct:    {"S", "39"},
		core.KindEnum:      {"E", "220"},
		core.KindMethod:    {"m", "213"},
		core.KindFunction:  {"f", "67"},
		core.KindConstant:  {"k", "214"},
		core.KindVariable:  {"v", "245"},
	}
	if is, ok := icons[k]; ok {
		return lipgloss.NewStyle().Foreground(is.color).Bold(k == core.KindClass).Render(is.icon)
	}
	return " "
}
