package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"codetree/core"
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
}

func buildTree(p *core.Project) *node {
	root := &node{label: filepath.Base(p.Root) + "/", expanded: true, isDir: true}
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
				dirs[acc] = child
				parent.children = append(parent.children, child)
			}
			parent = child
		}
		fn := &node{label: parts[len(parts)-1], file: f.Path, expanded: true, isFile: true}
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

// reflow recomputes the visible row list from expansion + filter state.
func (m *model) reflow() {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.rows = m.rows[:0]
	var walk func(n *node, forceExpand bool)
	walk = func(n *node, forceExpand bool) {
		m.rows = append(m.rows, n)
		if !n.expanded && !forceExpand {
			return
		}
		for _, c := range n.children {
			if query != "" && !visibleUnder(c, query) {
				continue
			}
			walk(c, query != "")
		}
	}
	walk(m.root, false)
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
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

// treeContent renders the visible rows with guide lines.
func (m *model) treeContent() string {
	var b strings.Builder
	depths := m.rowDepths()
	for i, n := range m.rows {
		prefix := strings.Repeat("  ", max(depths[i]-1, 0))
		marker := "  "
		if len(n.children) > 0 {
			if n.expanded {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
		}
		label := n.label
		switch {
		case n.isDir:
			label = styleDir.Render(label)
		case n.isFile:
			label = styleFile.Render(label)
		case n.sym != nil:
			label = symIcon(n.sym.Kind) + " " + label
		}
		line := prefix + marker + label
		if i == m.cursor {
			line = styleCursor.Render("▶ ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	return b.String()
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

// rowDepths computes nesting depth per visible row by tracking ancestry
// through the flattened list.
func (m *model) rowDepths() []int {
	depths := make([]int, len(m.rows))
	var stack []*node
	for i, n := range m.rows {
		for len(stack) > 0 && !containsNode(stack[len(stack)-1], n) && stack[len(stack)-1] != n {
			stack = stack[:len(stack)-1]
		}
		depths[i] = len(stack)
		stack = append(stack, n)
	}
	return depths
}
