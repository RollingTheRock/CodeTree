package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/lsp"
)

func lspTestProj() *core.Project {
	return &core.Project{Root: "/p", Files: []*core.File{
		{Path: "a.py", Lang: "python", Symbols: []*core.Symbol{
			{Name: "Base", Kind: core.KindClass, File: "a.py", Line: 1},
			{Name: "A", Kind: core.KindClass, File: "a.py", Line: 5,
				SuperTypes: []string{"Base"}},
		}},
	}}
}

// lspMsg handling: corrections apply on the main loop, status flips to
// ready, augmented classes reach the picker, and the diagram rebuilds.
func TestLSPMsgAppliesCorrections(t *testing.T) {
	m := newModel(lspTestProj())
	m.mode = modeDiagram
	m.rebuildDiagram()

	added := &core.Symbol{Name: "Color", Kind: core.KindEnum, File: "a.py", Line: 40, Source: "lsp"}
	mm, _ := m.Update(lspMsg{
		out:  lsp.Outcome{Status: lsp.StatusReady},
		corr: lsp.Corrections{Added: []*core.Symbol{added}},
	})
	m = mm.(model)

	if m.lspStat != lsp.StatusReady {
		t.Errorf("lspStat = %v, want ready", m.lspStat)
	}
	found := false
	for _, fe := range m.pickerFiles {
		if fe.path == "a.py" && fe.classCount == 3 {
			found = true
		}
	}
	if !found {
		t.Errorf("picker should count the augmented class: %+v", m.pickerFiles)
	}
	if findClassInProj(m.proj, "Color") == nil {
		t.Error("augmented class Color not merged into the project")
	}
}

// Absent/failed servers never touch the model.
func TestLSPMsgAbsentKeepsStatic(t *testing.T) {
	m := newModel(lspTestProj())
	before := len(m.proj.AllSymbols())
	mm, _ := m.Update(lspMsg{out: lsp.Outcome{Status: lsp.StatusAbsent}})
	m = mm.(model)
	if m.lspStat != lsp.StatusAbsent {
		t.Errorf("lspStat = %v, want absent", m.lspStat)
	}
	if len(m.proj.AllSymbols()) != before {
		t.Error("absent LSP must not mutate the project")
	}
}

// After a reload the applied corrections are gone and the layer goes stale
// instead of re-warming; L re-warms manually.
func TestLSPStaleAfterReload(t *testing.T) {
	m := newModel(lspTestProj())
	mm, _ := m.Update(lspMsg{out: lsp.Outcome{Status: lsp.StatusReady}})
	m = mm.(model)
	if m.lspStat != lsp.StatusReady {
		t.Fatalf("lspStat = %v, want ready", m.lspStat)
	}
	mm, _ = m.Update(reloadedMsg{})
	m = mm.(model)
	if m.lspStat != lsp.StatusStale {
		t.Fatalf("lspStat after reload = %v, want stale", m.lspStat)
	}
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	m = mm.(model)
	if m.lspStat != lsp.StatusWarming || cmd == nil {
		t.Errorf("L should re-warm: stat=%v cmd=%v", m.lspStat, cmd)
	}
}

func findClassInProj(p *core.Project, name string) *core.Symbol {
	for _, s := range p.AllSymbols() {
		if s.Name == name {
			return s
		}
	}
	return nil
}
