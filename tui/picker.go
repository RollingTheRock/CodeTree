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
