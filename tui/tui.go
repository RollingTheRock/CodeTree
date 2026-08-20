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
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"codetree/core"
	"codetree/diagram"
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

type viewMode int

const (
	modePicker viewMode = iota // entry point: file picker
	modeDiagram
	modeTree
)

// fileEntry is one row in the picker.
type fileEntry struct {
	path       string // relative to project root
	classCount int    // classes/interfaces/structs/enums defined inside
}

type model struct {
	root     *node
	projRoot string
	proj     *core.Project

	rows   []*node // visible rows (flattened)
	cursor int

	// class diagram view
	mode   viewMode
	dopts  diagram.Options
	diag   *diagram.Diagram
	sel    string // selected class name (persists across rebuilds)
	selIdx int    // selected node index in diag.Nodes (per-build identity)
	xoff   int    // horizontal scroll mirror for the diagram viewport

	// picker view
	pickerFiles []fileEntry
	marked      map[string]bool
	pCursor     int

	// live reload
	watchCh    chan struct{} // nil = watching disabled (silent degrade)
	lastReload time.Time
	scanOpts   core.ScanOptions

	treeVP    viewport.Model
	filter    textinput.Model
	filtering bool // tree mode only; picker's filter is always live

	width, height int
	ready         bool
	quitting      bool
	showHelp      bool
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
	m.scanOpts = opts
	m.watchCh = watchProject(abs) // nil on failure: no auto-reload, no crash
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func newModel(p *core.Project) model {
	ti := textinput.New()
	ti.Placeholder = "filter files…"
	ti.Prompt = "/ "
	ti.Focus()
	dopts := diagram.DefaultOptions()
	dopts.Color = true
	m := model{
		root: buildTree(p), projRoot: p.Root, proj: p, filter: ti, dopts: dopts,
		mode: modePicker, pickerFiles: pickerEntries(p), marked: map[string]bool{},
	}
	m.reflow()
	return m
}

// pickerEntries lists project source files with their class counts
// (files without classes included, shown as (0)).
func pickerEntries(p *core.Project) []fileEntry {
	var out []fileEntry
	for _, f := range p.Files {
		n := 0
		for _, s := range f.AllSymbols() {
			switch s.Kind {
			case core.KindClass, core.KindInterface, core.KindStruct, core.KindEnum:
				n++
			}
		}
		out = append(out, fileEntry{path: f.Path, classCount: n})
	}
	return out
}

// pickerVisible returns the filtered picker rows.
func (m *model) pickerVisible() []fileEntry {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.pickerFiles
	}
	var out []fileEntry
	for _, fe := range m.pickerFiles {
		if strings.Contains(strings.ToLower(fe.path), q) {
			out = append(out, fe)
		}
	}
	return out
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

// reloadedMsg signals a debounced project file change.
type reloadedMsg struct{}

// waitForChange blocks until the watcher fires; nil channel = never fires.
func waitForChange(ch chan struct{}) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		<-ch
		return reloadedMsg{}
	}
}

func (m model) Init() tea.Cmd { return waitForChange(m.watchCh) }

// reload rescans the project and rebuilds all views, preserving UI state:
// marks (pruned of deleted files), scope, focus, selection, scroll offset.
// A file that fails to parse mid-save is simply skipped by the scanner;
// a total scan failure keeps the old project.
func (m *model) reload() {
	proj, err := core.Scan(m.projRoot, langs.Registry{}, m.scanOpts)
	if err != nil || len(proj.Files) == 0 {
		return
	}
	m.proj = proj
	m.root = buildTree(proj)
	m.reflow()
	m.pickerFiles = pickerEntries(proj)

	// prune marks and scope of vanished files
	valid := map[string]bool{}
	for _, fe := range m.pickerFiles {
		valid[fe.path] = true
	}
	for p := range m.marked {
		if !valid[p] {
			delete(m.marked, p)
		}
	}
	var files []string
	for _, f := range m.dopts.Files {
		if valid[f] {
			files = append(files, f)
		}
	}
	m.dopts.Files = files

	// focus may reference a deleted class; drop it if gone (diagram rebuild
	// would error otherwise)
	if m.dopts.Focus != "" {
		found := false
		for _, s := range proj.AllSymbols() {
			if s.Name == m.dopts.Focus {
				found = true
				break
			}
		}
		if !found {
			m.dopts.Focus = ""
		}
	}
	if m.diag != nil {
		m.rebuildDiagram() // falls back to first node if selection vanished
	}
	m.lastReload = time.Now()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case reloadedMsg:
		m.reload()
		return m, waitForChange(m.watchCh) // re-arm

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		if m.diag != nil {
			m.rebuildDiagram()
		}
		return m, nil

	case tea.KeyMsg:
		// help overlay: any key closes
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		// rapid key repeats arrive as one coalesced message ("jj"); split
		// into per-rune messages so movement keys still work
		if len(msg.Runes) > 1 {
			for _, r := range msg.Runes {
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				m = mm.(model)
			}
			return m, nil
		}
		if m.mode == modePicker {
			return m.updatePicker(msg)
		}
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

		if m.mode == modeDiagram {
			return m.updateDiagram(msg)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "t":
			m.mode = modePicker
			return m, nil
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
		case "?":
			m.showHelp = true
			return m, nil
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

// ---- file picker ------------------------------------------------------------

func (m model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.pickerVisible()

	// filter mode: input owns printable keys; navigation/marking stay live
	// (fzf-style): arrows & ctrl+n/p move, tab marks, space types a space
	if m.filtering {
		switch msg.String() {
		case "esc": // exit filter, keep the query
			m.filtering = false
			m.filter.Blur()
			return m, nil
		case "enter": // confirm straight from the filter
			return m.pickerConfirm(vis)
		case "up", "ctrl+p":
			if m.pCursor > 0 {
				m.pCursor--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.pCursor < len(vis)-1 {
				m.pCursor++
			}
			return m, nil
		case "tab": // mark/unmark current row, advance
			if m.pCursor < len(vis) {
				p := vis[m.pCursor].path
				if m.marked[p] {
					delete(m.marked, p)
				} else {
					m.marked[p] = true
				}
				if m.pCursor < len(vis)-1 {
					m.pCursor++
				}
			}
			return m, nil
		case "shift+tab": // move up without marking
			if m.pCursor > 0 {
				m.pCursor--
			}
			return m, nil
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.pCursor = 0
			return m, cmd
		}
	}

	switch msg.String() {
	case "q", "esc", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "j", "down":
		if m.pCursor < len(vis)-1 {
			m.pCursor++
		}
		return m, nil
	case "k", "up":
		if m.pCursor > 0 {
			m.pCursor--
		}
		return m, nil
	case " ": // mark/unmark the file under the cursor
		if m.pCursor < len(vis) {
			p := vis[m.pCursor].path
			if m.marked[p] {
				delete(m.marked, p)
			} else {
				m.marked[p] = true
			}
		}
		return m, nil
	case "?":
		m.showHelp = true
		return m, nil
	case "/": // enter filter mode
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "enter":
		return m.pickerConfirm(vis)
	case "t":
		m.mode = modeTree
		return m, nil
	}
	return m, nil
}

// pickerConfirm opens the diagram scoped to the marked files, or the file
// under the cursor when nothing is marked.
func (m model) pickerConfirm(vis []fileEntry) (tea.Model, tea.Cmd) {
	var files []string
	if len(m.marked) > 0 {
		for p := range m.marked {
			files = append(files, p)
		}
		sort.Strings(files)
	} else if m.pCursor < len(vis) {
		files = []string{vis[m.pCursor].path}
	}
	m.dopts.Files = files
	m.dopts.Focus = ""
	m.sel = ""
	m.mode = modeDiagram
	m.filtering = false
	m.filter.Blur()
	m.rebuildDiagram()
	return m, nil
}

// pickerContent renders the file picker list.
func (m *model) pickerContent() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Select files") +
		styleDim.Render(fmt.Sprintf("  %d marked", len(m.marked))) + "\n\n")
	vis := m.pickerVisible()
	rowBG := lipgloss.Color("237") // cursor row highlight
	for i, fe := range vis {
		cursor := i == m.pCursor
		mkStyle := styleDim
		if m.marked[fe.path] {
			mkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
		}
		pathStyle := styleFile
		if cursor {
			mkStyle = mkStyle.Background(rowBG)
			pathStyle = pathStyle.Background(rowBG)
		}
		mark := mkStyle.Render("[x]")
		if !m.marked[fe.path] {
			mark = mkStyle.Render("[ ]")
		}
		count := fmt.Sprintf("(%d)", fe.classCount)
		prefix := "  "
		if cursor {
			prefix = "▶ "
		}
		line := prefix + mark + " " + pathStyle.Render(fe.path) + " " + count
		if cursor {
			// full-row highlight: pad to viewport width on the row background
			row := lipgloss.NewStyle().Background(rowBG)
			line = row.Render(prefix) + mark + row.Render(" ") + pathStyle.Render(fe.path) + row.Render(" "+count)
			if pad := m.treeVP.Width - lipgloss.Width(line); pad > 0 {
				line += row.Render(strings.Repeat(" ", pad))
			}
		}
		b.WriteString(line + "\n")
	}
	if len(vis) == 0 {
		b.WriteString(styleDim.Render("  (no matching files)") + "\n")
	}
	return b.String()
}

// ---- class diagram view ---------------------------------------------------

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

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
	return m.openEditorAt(n.file, n.line)
}

func (m *model) openEditorAt(rel string, line int) tea.Cmd {
	if rel == "" {
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
	file := filepath.Join(m.projRoot, rel)
	var args []string
	base := filepath.Base(editor)
	switch {
	case strings.Contains(base, "hx"):
		args = []string{fmt.Sprintf("%s:%d", file, max(line, 1))}
	case strings.Contains(base, "vim"), strings.Contains(base, "nvim"):
		args = []string{fmt.Sprintf("+%d", max(line, 1)), file}
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
	m.treeVP = viewport.New(w, h)
	m.treeVP.SetContent(m.centered(m.treeContent()))
	m.syncScroll()
}

// centered 把内容块整体水平居中（块比视口窄时左侧统一补 padding），
// 比视口宽时不处理，交由横向滚动。
func (m *model) centered(s string) string {
	if m.width <= 0 {
		return s
	}
	maxw := 0
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > maxw {
			maxw = w
		}
	}
	pad := (m.width - maxw) / 2
	if pad <= 0 {
		return s
	}
	padding := strings.Repeat(" ", pad)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = padding + line
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "loading…"
	}
	switch m.mode {
	case modePicker:
		m.treeVP.SetContent(m.pickerContent()) // full-row highlight, no centering
	case modeDiagram:
		if m.diag != nil {
			m.treeVP.SetContent(m.centered(m.diag.Text))
		}
	default:
		m.treeVP.SetContent(m.centered(m.treeContent()))
	}

	body := m.treeVP.View()
	if m.showHelp {
		body = m.helpView()
	}
	return body + "\n" + m.statusBar()
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
