package tui

import (
	"fmt"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/diagram"
)

// perfProject builds a synthetic project: nFiles files, each with nSyms
// classes carrying nMeths methods.
func perfProject(nFiles, nSyms, nMeths int) *core.Project {
	p := &core.Project{Root: "/bench"}
	for i := 0; i < nFiles; i++ {
		f := &core.File{Path: fmt.Sprintf("pkg%d/mod%d/file%d.go", i%10, i%50, i), Lang: "go"}
		for j := 0; j < nSyms; j++ {
			s := &core.Symbol{Kind: core.KindClass, Name: fmt.Sprintf("Class%d", j), File: f.Path}
			for k := 0; k < nMeths; k++ {
				s.Children = append(s.Children, &core.Symbol{Kind: core.KindMethod, Name: fmt.Sprintf("Method%d", k), File: f.Path})
			}
			f.Symbols = append(f.Symbols, s)
		}
		p.Files = append(p.Files, f)
	}
	return p
}

// BenchmarkViewTree measures one frame in tree mode with ~12k visible rows.
func BenchmarkViewTree(b *testing.B) {
	m := newModel(perfProject(500, 3, 8))
	m.mode = modeTree
	m.width, m.height = 120, 40
	m.ready = true
	m.layout()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkViewPicker measures one frame in picker mode with 20k files.
func BenchmarkViewPicker(b *testing.B) {
	m := newModel(perfProject(20000, 1, 0))
	m.width, m.height = 120, 40
	m.ready = true
	m.layout()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkDiagramMove measures one selection move in diagram mode
// (~1000 classes) via the highlight-only fast path.
func BenchmarkDiagramMove(b *testing.B) {
	m := newModel(perfProject(500, 2, 4))
	m.width, m.height = 120, 40
	m.ready = true
	m.layout()
	m.mode = modeDiagram
	m.rebuildDiagram()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.moveSel(1, 0)
		m.applySelection()
	}
}

// BenchmarkDiagramBuild measures a full diagram render (~1000 classes).
func BenchmarkDiagramBuild(b *testing.B) {
	p := perfProject(500, 2, 4)
	opts := diagram.DefaultOptions()
	opts.Color = true
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := diagram.Build(p, opts); err != nil {
			b.Fatal(err)
		}
	}
}
