// Project：CodeTree
// Author：RollingTheRock
// Date: 2026.8.21

// Package tui is a thin bubbletea browser over core.Project: tree on the
// left, symbol detail on the right, fuzzy filter, open-in-editor.
package tui

import (
	"context"
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

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/diagram"
	"github.com/RollingTheRock/CodeTree/langs"
	"github.com/RollingTheRock/CodeTree/lsp"
)

// ---- node tree ------------------------------------------------------------

// ---- model ----------------------------------------------------------------

type viewMode int

const (
	modePicker viewMode = iota // entry point: file picker
	modeDiagram
	modeTree
)

// contentCache holds the rendered line cache shared by the tree and picker
// views. It lives behind a pointer so View (value receiver) can update it.
// Frames only re-render what changed: a full rebuild on dirty, else just the
// old/new cursor lines.
type contentCache struct {
	mode      viewMode // mode the cache was built for
	dirty     bool     // full rebuild needed (structure/filter/resize/mode)
	lines     []string // rendered lines (padding included for tree)
	cursor    int      // cursor value the lines were rendered against
	visLen    int      // picker: visible entry count at rebuild
	pad       int      // tree: centering pad
	padStr    string
	content   string // joined lines, handed to the viewport
	gen       int    // bumped whenever content changes
	pushedGen int    // gen last pushed via SetContent (skips redundant re-splits)
}

type model struct {
	root     *node
	projRoot string
	proj     *core.Project

	rows     []*node // visible rows (flattened)
	rowDepth []int   // per-row nesting depth, parallel to rows
	cursor   int
	cc       *contentCache

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
	watchCh    chan []string // nil = watching disabled (silent degrade)
	lastReload time.Time
	scanOpts   core.ScanOptions

	// LSP semantic layer (optional, async): corrections are collected in a
	// goroutine and applied on the main loop, then views rebuild
	lspStat lsp.Status

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
		lspStat: lsp.StatusWarming, // Init's warm pass decides the real status
		cc:      &contentCache{dirty: true},
	}
	m.reflow()
	return m
}

// ---- tea.Model ------------------------------------------------------------

// reloadedMsg signals a debounced batch of changed files. Nil paths means
// "too many changes — rescan everything".
type reloadedMsg struct {
	paths []string
}

// lspMsg carries one LSP pass's collected corrections to the main loop.
type lspMsg struct {
	out  lsp.Outcome
	corr lsp.Corrections
}

// waitForChange blocks until the watcher fires; nil channel = never fires.
// A closed channel (watcher setup failed in the background) yields a nil
// message, which bubbletea drops — watching then silently stops.
func waitForChange(ch chan []string) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		paths, ok := <-ch
		if !ok {
			return nil
		}
		return reloadedMsg{paths}
	}
}

// warmLSP runs an LSP pass off the main loop. proj is only read here; the
// corrections are applied when the returned message is handled.
func warmLSP(root string, proj *core.Project) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		out, corr := lsp.Collect(ctx, root, proj)
		return lspMsg{out, corr}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitForChange(m.watchCh), warmLSP(m.projRoot, m.proj))
}

// reload applies a debounced change batch and rebuilds all views, preserving
// UI state: marks (pruned of deleted files), scope, focus, selection, scroll
// offset. A nil batch (mass change) triggers a full rescan; otherwise only
// the changed files are re-parsed. A file that fails to parse mid-save is
// simply skipped; a total scan failure keeps the old project.
func (m *model) reload(paths []string) {
	if paths == nil {
		proj, err := core.Scan(m.projRoot, langs.Registry{}, m.scanOpts)
		if err != nil || len(proj.Files) == 0 {
			return
		}
		m.proj = proj
	} else {
		m.applyChanged(paths)
	}
	m.rebuildViews()
}

// applyChanged re-parses just the changed files, keeping proj.Files sorted
// by path. Deleted, unreadable, or symbol-free files drop out — exactly what
// a full rescan would produce.
func (m *model) applyChanged(paths []string) {
	reg := langs.Registry{}
	for _, rel := range paths {
		f := core.ScanFile(m.projRoot, rel, reg, m.scanOpts)
		i := sort.Search(len(m.proj.Files), func(i int) bool { return m.proj.Files[i].Path >= rel })
		exists := i < len(m.proj.Files) && m.proj.Files[i].Path == rel
		switch {
		case f == nil && exists:
			m.proj.Files = append(m.proj.Files[:i], m.proj.Files[i+1:]...)
		case f == nil:
			// not in the project, nothing to remove
		case exists:
			m.proj.Files[i] = f
		default:
			m.proj.Files = append(m.proj.Files, nil)
			copy(m.proj.Files[i+1:], m.proj.Files[i:])
			m.proj.Files[i] = f
		}
	}
}

// rebuildViews derives every view from m.proj after a reload.
func (m *model) rebuildViews() {
	proj := m.proj
	m.root = buildTree(proj)
	m.reflow()
	m.pickerFiles = pickerEntries(proj)
	m.cc.dirty = true // picker entries rebuilt

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
		m.reload(msg.paths)
		// the rescan dropped applied LSP corrections; servers are expensive
		// to restart, so don't re-warm automatically — mark stale, the user
		// refreshes manually with L
		if m.lspStat == lsp.StatusReady {
			m.lspStat = lsp.StatusStale
		}
		return m, waitForChange(m.watchCh)

	case lspMsg:
		m.lspStat = msg.out.Status
		if msg.out.Status == lsp.StatusReady {
			lsp.Apply(m.proj, msg.corr)
			m.root = buildTree(m.proj)
			m.reflow()
			m.pickerFiles = pickerEntries(m.proj)
			if m.diag != nil {
				m.rebuildDiagram()
			}
		}
		return m, nil

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
		// L refreshes the LSP pass manually in any non-filtering mode
		if msg.String() == "L" && !m.filtering {
			m.lspStat = lsp.StatusWarming
			return m, warmLSP(m.projRoot, m.proj)
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

// ---- class diagram view ---------------------------------------------------

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
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
	m.cc.dirty = true // width affects the centering pad
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

// centeredDiag pads the diagram text for horizontal centering using the
// known canvas width — no per-line width measurement.
func (m *model) centeredDiag() string {
	if m.diag == nil {
		return ""
	}
	pad := (m.width - m.diag.Width) / 2
	if pad <= 0 {
		return m.diag.Text
	}
	padStr := strings.Repeat(" ", pad)
	lines := strings.Split(m.diag.Text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = padStr + line
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
		m.syncPicker() // full-row highlight, no centering
	case modeDiagram:
		if m.diag == nil {
			break // no diagram yet: leave the viewport as-is
		}
	default:
		m.syncTree()
	}
	if m.mode == modeDiagram && m.diag == nil {
		// nothing to show
	} else if m.cc.gen != m.cc.pushedGen {
		// SetContent re-splits and re-measures every line — only pay for it
		// when the content actually changed
		m.treeVP.SetContent(m.cc.content)
		m.cc.pushedGen = m.cc.gen
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
