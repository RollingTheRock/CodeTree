// chrome.go — status bar, help overlay, picker row highlight.
// All visual chrome styles are centralized here (ANSI 256 palette).
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/RollingTheRock/CodeTree/lsp"
)

// ---- status bar styles ----------------------------------------------------

var (
	stBar = lipgloss.NewStyle().Background(lipgloss.Color("234"))

	badgePicker  = lipgloss.NewStyle().Background(lipgloss.Color("78")).Foreground(lipgloss.Color("0")).Bold(true)
	badgeDiagram = lipgloss.NewStyle().Background(lipgloss.Color("51")).Foreground(lipgloss.Color("0")).Bold(true)
	badgeFocus   = lipgloss.NewStyle().Background(lipgloss.Color("227")).Foreground(lipgloss.Color("0")).Bold(true)
	badgeTree    = lipgloss.NewStyle().Background(lipgloss.Color("141")).Foreground(lipgloss.Color("0")).Bold(true)

	stKey     = lipgloss.NewStyle().Background(lipgloss.Color("234")).Foreground(lipgloss.Color("15")).Bold(true)
	stDesc    = lipgloss.NewStyle().Background(lipgloss.Color("234")).Foreground(lipgloss.Color("245"))
	stBarInfo = lipgloss.NewStyle().Background(lipgloss.Color("234")).Foreground(lipgloss.Color("250"))
)

// hint is one key-description pair in the status bar.
type hint struct{ key, desc string }

var (
	pickerHints = []hint{
		{"j/k", "move"}, {"space", "mark"}, {"enter", "open"},
		{"/", "filter"}, {"t", "tree"}, {"L", "lsp"}, {"?", "help"}, {"q", "quit"},
	}
	pickerFilterHints = []hint{
		{"↑↓", "move"}, {"tab", "mark"}, {"enter", "open"}, {"esc", "done"},
	}
	diagramHints = []hint{
		{"hjkl", "select"}, {"enter", "focus"}, {"+/-", "depth"},
		{"b", "siblings"}, {"m", "members"}, {"c", "assoc"}, {"A", "all"},
		{"esc", "picker"}, {"t", "tree"}, {"L", "lsp"}, {"?", "help"}, {"q", "quit"},
	}
	focusHints = []hint{
		{"hjkl", "select"}, {"esc", "unfocus"}, {"+/-", "depth"},
		{"b", "siblings"}, {"m", "members"}, {"c", "assoc"}, {"A", "all"},
		{"t", "tree"}, {"L", "lsp"}, {"?", "help"}, {"q", "quit"},
	}
	treeHints = []hint{
		{"j/k", "move"}, {"h/l", "fold"}, {"space", "toggle"},
		{"/", "filter"}, {"t", "picker"}, {"L", "lsp"}, {"?", "help"}, {"q", "quit"},
	}
)

// statusBar renders the airline-style status bar: colored mode badge on the
// left, key hints (bright bold key + dim description), scroll position on
// the right, all on a full-width dark background.
func (m *model) statusBar() string {
	// filter input takes over the bar (tree filter, or picker's filter mode
	// with fzf-style nav/mark hints)
	if m.filtering {
		if m.mode == modePicker {
			return badgePicker.Render(" PICKER ") + " " + m.filter.View() +
				" " + renderHints(pickerFilterHints, m.width)
		}
		return m.filter.View()
	}

	var badge string
	var hints []hint
	switch m.mode {
	case modePicker:
		badge = badgePicker.Render(" PICKER ")
		hints = pickerHints
	case modeDiagram:
		if m.dopts.Focus != "" {
			badge = badgeFocus.Render(" FOCUS ") + stBarInfo.Render(" "+m.dopts.Focus+" ")
			hints = focusHints
		} else {
			badge = badgeDiagram.Render(" DIAGRAM ")
			hints = diagramHints
		}
	case modeTree:
		badge = badgeTree.Render(" TREE ")
		hints = treeHints
	}

	// scope label right after the badge in diagram mode
	if m.mode == modeDiagram {
		scope := "all"
		if len(m.dopts.Files) > 0 {
			var names []string
			for _, f := range m.dopts.Files {
				names = append(names, baseName(f))
			}
			scope = strings.Join(names, ",")
		}
		badge += stBarInfo.Render(" " + scope + " ")
	}

	// picker: show the active filter query (input itself takes over the bar
	// while filtering)
	if m.mode == modePicker {
		if q := strings.TrimSpace(m.filter.Value()); q != "" {
			badge += stDesc.Render(" /" + q)
		}
	}

	right := m.scrollIndicator()
	if !m.lastReload.IsZero() {
		right = stBarInfo.Render("↻ "+m.lastReload.Format("15:04:05")+" ") + right
	}
	switch m.lspStat {
	case lsp.StatusWarming:
		right = stDesc.Render("lsp warming… ") + right
	case lsp.StatusReady:
		right = stBarInfo.Render("lsp ready ") + right
	case lsp.StatusFailed:
		right = stDesc.Render("lsp failed ") + right
	case lsp.StatusStale:
		right = stDesc.Render("lsp stale ·L ") + right
	}
	left := badge + " " + renderHints(hints, m.width)

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		// narrow terminal: drop descriptions, keep badge + keys
		left = badge + " " + renderHintKeys(hints)
		pad = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if pad < 1 {
		left = badge // extreme narrow: badge only
		pad = m.width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if pad < 0 {
		pad = 0
	}
	return left + stBar.Render(strings.Repeat(" ", pad)) + right
}

func renderHints(hints []hint, width int) string {
	sep := stDesc.Render(" · ")
	var parts []string
	for _, h := range hints {
		parts = append(parts, stKey.Render(h.key)+stDesc.Render(" "+h.desc))
	}
	return strings.Join(parts, sep)
}

// renderHintKeys renders keys only (narrow-terminal degrade).
func renderHintKeys(hints []hint) string {
	var parts []string
	for _, h := range hints {
		parts = append(parts, h.key)
	}
	return stKey.Render(strings.Join(parts, " · "))
}

// scrollIndicator renders the viewport position, e.g. "▲ 12/48 ▼".
func (m *model) scrollIndicator() string {
	total := m.treeVP.TotalLineCount()
	h := m.treeVP.Height
	if total <= h || h <= 0 {
		return stBar.Render("")
	}
	up, down := " ", " "
	if m.treeVP.YOffset > 0 {
		up = "▲"
	}
	if !m.treeVP.AtBottom() {
		down = "▼"
	}
	return stDesc.Render(fmt.Sprintf("%s %d/%d %s", up, m.treeVP.YOffset+1, total, down))
}

// ---- help overlay ---------------------------------------------------------

var helpGroups = []struct {
	name  string
	binds []hint
}{
	{"PICKER", pickerHints},
	{"PICKER (filter mode)", pickerFilterHints},
	{"DIAGRAM", diagramHints[:len(diagramHints)-2]}, // minus help/quit
	{"FOCUS (neighborhood)", focusHints[:len(focusHints)-2]},
	{"TREE", treeHints[:len(treeHints)-2]},
	{"GENERAL", []hint{{"o", "open in $EDITOR"}, {"?", "this help"}, {"q", "quit"}}},
}

// helpView renders the centered help overlay body (status bar stays visible).
func (m *model) helpView() string {
	keySty := lipgloss.NewStyle().Foreground(lipgloss.Color("51")).Bold(true)
	descSty := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	headSty := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)

	var b strings.Builder
	for _, g := range helpGroups {
		b.WriteString(headSty.Render(g.name) + "\n")
		for _, h := range g.binds {
			b.WriteString("  " + keySty.Render(fmt.Sprintf("%-8s", h.key)) + descSty.Render(h.desc) + "\n")
		}
		b.WriteString("\n")
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("51")).
		Padding(0, 2).
		Render(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render("ct key bindings") +
			"\n\n" + strings.TrimRight(b.String(), "\n"))
	h := m.height - 2
	if h < 3 {
		h = 3
	}
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
}

// baseName is filepath.Base without importing filepath again here.
func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
