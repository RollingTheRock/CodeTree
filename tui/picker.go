package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/RollingTheRock/CodeTree/core"
)

// fileEntry is one row in the picker.
type fileEntry struct {
	path       string // relative to project root
	classCount int    // classes/interfaces/structs/enums defined inside

	// render caches, filled once by pickerEntries
	styled   string // path with file style
	styledBG string // path with file style + cursor-row background
	count    string // "(N)"
	plainW   int    // display width of the row content (no cursor prefix)
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
		fe := fileEntry{path: f.Path, classCount: n}
		fe.styled = styleFile.Render(f.Path)
		fe.styledBG = styleFile.Background(pickerRowBG).Render(f.Path)
		fe.count = fmt.Sprintf("(%d)", n)
		fe.plainW = 3 + 1 + lipgloss.Width(f.Path) + 1 + len(fe.count) // mark + path + count
		out = append(out, fe)
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
				m.cc.dirty = true // mark box + header count changed
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
			m.cc.dirty = true // visible set changed with the query
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
			m.cc.dirty = true
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

// pickerRowBG is the cursor row's full-width highlight color.
var pickerRowBG = lipgloss.Color("237")

// mark box variants, rendered once (cursor row needs the background version).
var (
	markOff   = styleDim.Render("[ ]")
	markOn    = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Render("[x]")
	markOffBG = styleDim.Background(pickerRowBG).Render("[ ]")
	markOnBG  = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Background(pickerRowBG).Render("[x]")
)

// pickerRow renders one picker row from cached parts. The cursor row costs a
// few lipgloss renders (one row per cursor move); other rows are pure concat.
func (m *model) pickerRow(fe *fileEntry, cursor bool) string {
	if !cursor {
		mark := markOff
		if m.marked[fe.path] {
			mark = markOn
		}
		return "  " + mark + " " + fe.styled + " " + fe.count
	}
	row := lipgloss.NewStyle().Background(pickerRowBG)
	mark := markOffBG
	if m.marked[fe.path] {
		mark = markOnBG
	}
	// full-row highlight: pad to viewport width on the row background
	line := row.Render("▶ ") + mark + row.Render(" ") + fe.styledBG + row.Render(" "+fe.count)
	if pad := m.treeVP.Width - (2 + fe.plainW); pad > 0 {
		line += row.Render(strings.Repeat(" ", pad))
	}
	return line
}

// syncPicker maintains the picker's line cache: full rebuild when dirty
// (filter/mark/scope change), otherwise patches just the old/new cursor
// rows, then re-joins. Called from View via the shared cache pointer.
func (m *model) syncPicker() {
	cc := m.cc
	if cc.mode != modePicker {
		cc.mode, cc.dirty = modePicker, true
	}
	vis := m.pickerVisible()
	if cc.dirty || cc.visLen != len(vis) {
		header := styleTitle.Render("Select files") +
			styleDim.Render(fmt.Sprintf("  %d marked", len(m.marked)))
		cc.lines = make([]string, 0, len(vis)+3)
		cc.lines = append(cc.lines, header, "")
		for i := range vis {
			cc.lines = append(cc.lines, m.pickerRow(&vis[i], i == m.pCursor))
		}
		if len(vis) == 0 {
			cc.lines = append(cc.lines, styleDim.Render("  (no matching files)"))
		}
		cc.dirty = false
		cc.cursor = m.pCursor
		cc.visLen = len(vis)
	} else if cc.cursor != m.pCursor {
		// patch old+new cursor rows (offset by the 2 header lines)
		if cc.cursor >= 0 && cc.cursor < len(vis) {
			cc.lines[cc.cursor+2] = m.pickerRow(&vis[cc.cursor], false)
		}
		if m.pCursor < len(vis) {
			cc.lines[m.pCursor+2] = m.pickerRow(&vis[m.pCursor], true)
		}
		cc.cursor = m.pCursor
	} else {
		return // nothing changed: keep the joined content as-is
	}
	cc.content = strings.Join(cc.lines, "\n")
	cc.gen++
}
