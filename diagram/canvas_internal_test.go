package diagram

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"codetree/core"
)

func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// TestDashedEdge exercises the reserved implements-edge rendering: dashed
// ┄/┆ glyphs for straight runs, solid corner/junction glyphs where Unicode
// has no dashed variants.
func TestDashedEdge(t *testing.T) {
	opts := DefaultOptions()

	mk := func(name string) *lnode {
		g := &gnode{name: name, sym: &core.Symbol{Name: name, Kind: core.KindClass}}
		w, h := cardSize(g, opts)
		return &lnode{g: g, w: w, h: h}
	}
	iface := mk("Reader")
	impl := mk("FileReader")
	// place impl directly below iface
	bx := &box{ln: iface, x: 0, y: 0, w: iface.w, h: iface.h}
	by := &box{ln: impl, x: (iface.w - impl.w) / 2, y: iface.h + VGap, w: impl.w, h: impl.h}
	if by.x < 0 {
		by.x = 0
	}

	c := newCanvas(max(iface.w, impl.w), iface.h+VGap+impl.h)
	c.reserve(bx)
	c.reserve(by)
	c.edge(bx.cx(), bx.bottom(), by.cx(), by.y, stEdgeDash, true)
	c.drawBox(bx, opts)
	c.drawBox(by, opts)
	c.arrow(by.cx(), by.y, stEdgeDash)
	out := c.render(opts)

	golden := "testdata/dashed_edge.golden"
	if updating() {
		if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if out != string(want) {
		t.Errorf("dashed edge mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}
	if !strings.Contains(out, "┆") && !strings.Contains(out, "┄") {
		t.Error("expected dashed glyphs in output")
	}
}

// TestANSIColors asserts that Color mode emits semantic ANSI sequences
// (class icon, interface icon, inheritance edge) and piped mode emits none.
func TestANSIColors(t *testing.T) {
	// go test's stdout is not a TTY; force 256-color profile so lipgloss
	// actually emits sequences.
	lipgloss.SetColorProfile(termenv.ANSI256)

	p := &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: "a.py", Symbols: []*core.Symbol{
			{Name: "Shape", Kind: core.KindInterface},
			{Name: "Circle", Kind: core.KindClass, SuperTypes: []string{"Shape"}},
		}}},
	}
	opts := DefaultOptions()
	opts.Color = true
	d, err := Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Text, "\x1b[") {
		t.Fatal("color mode must emit ANSI escapes")
	}
	for _, code := range []string{
		"38;5;51", // bright-cyan class icon (stIconClass)
		"38;5;78", // green interface icon (stIconIface)
		"38;5;39", // bright blue inheritance edge (stEdgeSolid)
	} {
		if !strings.Contains(d.Text, code) {
			t.Errorf("missing ANSI color %q in:\n%q", code, d.Text)
		}
	}

	opts.Color = false
	d2, err := Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d2.Text, "\x1b[") {
		t.Error("non-color mode must not emit ANSI escapes")
	}
}
