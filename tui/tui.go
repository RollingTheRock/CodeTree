// Package tui is a thin bubbletea browser over core.Project: tree on the
// left, symbol detail on the right, fuzzy filter, open-in-editor.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codetree/core"
	"codetree/langs"
)

// ---- node tree ------------------------------------------------------------

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

// ---- model ----------------------------------------------------------------

type model struct {
	root     *node
	projRoot string

	rows   []*node // visible rows (flattened)
	cursor int

	treeVP    viewport.Model
	detailVP  viewport.Model
	filter    textinput.Model
	filtering bool

	width, height int
	ready         bool
	quitting      bool
}

// Run scans path and starts the TUI program.
func Run(path string, opts core.ScanOptions) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	proj, err := core.Scan(abs, langs.Registry{}, opts)
	if err != nil {
		return err
	}
	m := newModel(proj)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newModel(p *core.Project) model {
	ti := textinput.New()
	ti.Placeholder = "filter by name…"
	ti.Prompt = "/"
	m := model{root: buildTree(p), projRoot: p.Root, filter: ti}
	m.reflow()
	return m
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

// ---- tea.Model ------------------------------------------------------------

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch msg.String() {
			case "esc":
				m.filtering = false
				m.filter.Blur()
				m.filter.SetValue("")
				m.reflow()
				return m, nil
			case "enter":
				m.filtering = false
				m.filter.Blur()
				m.reflow()
				return m, nil
			default:
				var cmd tea.Cmd
				m.filter, cmd = m.filter.Update(msg)
				m.reflow()
				m.cursor = 0
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "h", "left":
			m.collapse()
		case "l", "right", "enter":
			m.expand()
		case " ":
			m.toggle()
		case "/":
			m.filtering = true
			m.filter.Focus()
			return m, textinput.Blink
		case "o":
			return m, m.openEditor()
		}
		m.syncScroll()
	}

	return m, tea.Batch(cmds...)
}

func (m *model) current() *node {
	if len(m.rows) == 0 {
		return nil
	}
	return m.rows[m.cursor]
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

// openEditor suspends the TUI, opens $EDITOR (fallback hx, vim) at the
// symbol's file:line, then resumes.
func (m *model) openEditor() tea.Cmd {
	n := m.current()
	if n == nil || n.file == "" {
		return nil
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		for _, cand := range []string{"hx", "vim"} {
			if _, err := exec.LookPath(cand); err == nil {
				editor = cand
				break
			}
		}
	}
	if editor == "" {
		return nil
	}
	file := filepath.Join(m.projRoot, n.file)
	var args []string
	base := filepath.Base(editor)
	switch {
	case strings.Contains(base, "hx"):
		args = []string{fmt.Sprintf("%s:%d", file, max(n.line, 1))}
	case strings.Contains(base, "vim"), strings.Contains(base, "nvim"):
		args = []string{fmt.Sprintf("+%d", max(n.line, 1)), file}
	default:
		args = []string{file}
	}
	fields := strings.Fields(editor)
	c := exec.Command(fields[0], append(fields[1:], args...)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return nil })
}

// ---- view -----------------------------------------------------------------

var (
	styleCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	styleDir    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	styleFile   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	styleDetail = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func (m *model) layout() {
	w := m.width
	h := m.height - 2 // status + filter line
	if h < 3 {
		h = 3
	}
	if w >= 100 {
		tw := w * 2 / 5
		m.treeVP = viewport.New(tw, h)
		m.detailVP = viewport.New(w-tw-3, h)
	} else {
		th := h * 3 / 5
		m.treeVP = viewport.New(w, th)
		m.detailVP = viewport.New(w, h-th-1)
	}
	m.treeVP.SetContent(m.treeContent())
	m.detailVP.SetContent(m.detailContent())
	m.syncScroll()
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "loading…"
	}
	m.treeVP.SetContent(m.treeContent())
	m.detailVP.SetContent(m.detailContent())

	var body string
	if m.width >= 100 {
		sep := styleBorder.Render(strings.Repeat("│\n", m.treeVP.Height))
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.treeVP.View(), sep, m.detailVP.View())
	} else {
		sep := styleBorder.Render(strings.Repeat("─", m.width))
		body = lipgloss.JoinVertical(lipgloss.Left, m.treeVP.View(), sep, m.detailVP.View())
	}

	status := styleDim.Render("j/k move · h/l fold · space toggle · / filter · o open · q quit")
	if m.filtering {
		status = m.filter.View()
	}
	return body + "\n" + status
}

func (m *model) syncScroll() {
	if m.cursor < m.treeVP.YOffset {
		m.treeVP.YOffset = m.cursor
	}
	if m.cursor >= m.treeVP.YOffset+m.treeVP.Height {
		m.treeVP.YOffset = m.cursor - m.treeVP.Height + 1
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

func (m *model) detailContent() string {
	n := m.current()
	if n == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(n.label) + "\n\n")
	if n.sym != nil {
		s := n.sym
		fmt.Fprintf(&b, "%s  %s:%d\n\n", styleDim.Render(s.Kind.String()), n.file, s.Line)
		if s.Detail != "" {
			fmt.Fprintf(&b, "%s %s\n\n", styleDim.Render("signature:"), styleDetail.Render(s.Name+s.Detail))
		}
		if len(s.SuperTypes) > 0 {
			fmt.Fprintf(&b, "%s %s\n\n", styleDim.Render("inherits:"), strings.Join(s.SuperTypes, ", "))
		}
		if s.Doc != "" {
			fmt.Fprintf(&b, "%s\n", styleDetail.Render(s.Doc))
		}
	} else if n.isFile {
		fmt.Fprintf(&b, "%s\n", styleDim.Render(n.file))
	}
	return b.String()
}
