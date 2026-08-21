package diagram_test

import (
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/diagram"
)

var update = flag.Bool("update", false, "update golden files")

func cls(name string, bases ...string) *core.Symbol {
	s := &core.Symbol{Name: name, Kind: core.KindClass, SuperTypes: bases}
	return s
}

func withMembers(s *core.Symbol, fields []core.Field, methods ...string) *core.Symbol {
	s.Fields = fields
	for _, m := range methods {
		s.Children = append(s.Children, &core.Symbol{Name: m, Kind: core.KindMethod, Detail: "(self)"})
	}
	return s
}

func proj(syms ...*core.Symbol) *core.Project {
	return &core.Project{
		Root:  "/p",
		Files: []*core.File{{Path: "a.py", Lang: "python", Symbols: syms}},
	}
}

func golden(t *testing.T, name string, p *core.Project, opts diagram.Options) {
	t.Helper()
	d, err := diagram.Build(p, opts)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	path := "testdata/" + name + ".golden"
	if *update {
		if err := os.WriteFile(path, []byte(d.Text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if d.Text != string(want) {
		t.Errorf("%s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, d.Text, want)
	}
}

func TestChain(t *testing.T) {
	golden(t, "chain", proj(cls("A"), cls("B", "A"), cls("C", "B")), diagram.DefaultOptions())
}

func TestFanout(t *testing.T) {
	golden(t, "fanout", proj(
		cls("P"), cls("C1", "P"), cls("C2", "P"), cls("C3", "P"),
	), diagram.DefaultOptions())
}

func TestTwoTrees(t *testing.T) {
	golden(t, "two_trees", proj(
		cls("A"), cls("B", "A"),
		cls("X"), cls("Y", "X"), cls("Z", "X"),
	), diagram.DefaultOptions())
}

func TestMultiLevelFanout(t *testing.T) {
	golden(t, "multilevel", proj(
		cls("Root"),
		cls("Left", "Root"), cls("Right", "Root"),
		cls("LL", "Left"), cls("LR", "Left"), cls("RR", "Right"),
	), diagram.DefaultOptions())
}

func TestMultipleInheritance(t *testing.T) {
	golden(t, "multi_inherit", proj(
		cls("Base1"), cls("Base2"),
		cls("Child", "Base1", "Base2"),
		cls("GrandChild", "Child"),
	), diagram.DefaultOptions())
}

func TestExternalBase(t *testing.T) {
	golden(t, "external_base", proj(
		cls("Local"), cls("Mine", "requests.Session"),
	), diagram.DefaultOptions())
}

func TestOrphans(t *testing.T) {
	golden(t, "orphans", proj(
		cls("A"), cls("B", "A"),
		cls("Lonely1"), cls("Lonely2"), cls("Lonely3"),
	), diagram.DefaultOptions())
}

func TestMembers(t *testing.T) {
	opts := diagram.DefaultOptions()
	opts.Members = true
	golden(t, "members", proj(
		withMembers(cls("Animal"), []core.Field{{Name: "legs", Type: "int"}}, "speak"),
		withMembers(cls("Dog", "Animal"),
			[]core.Field{{Name: "name", Type: "str"}, {Name: "tricks", Type: "list", ClassVar: true}},
			"fetch", "bark"),
	), opts)
}

func TestFocus(t *testing.T) {
	p := proj(
		cls("A"), cls("B", "A"), cls("C", "B"), cls("D", "C"),
		cls("B2", "A"), // sibling branch
		cls("Unrelated"),
	)
	opts := diagram.DefaultOptions()
	opts.Focus = "B"
	golden(t, "focus", p, opts)

	opts.Siblings = true
	golden(t, "focus_siblings", p, opts)

	opts = diagram.DefaultOptions()
	opts.Focus = "B"
	opts.Up = 1
	opts.Down = 1
	golden(t, "focus_up1_down1", p, opts)
}

// TestOrphansMembers covers the regression where orphan cards were sized
// with collapsed width but drawn expanded, tearing the border.
func TestOrphansMembers(t *testing.T) {
	opts := diagram.DefaultOptions()
	opts.Members = true
	golden(t, "orphans_members", proj(
		cls("A"), cls("B", "A"),
		withMembers(cls("Lonely"),
			[]core.Field{{Name: "count", Type: "int"}},
			"a_very_long_method_name", "conv"),
	), opts)
}

func TestImplementsEdge(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: "zoo.java", Lang: "java", Symbols: []*core.Symbol{
			{Name: "Entity", Kind: core.KindInterface},
			{Name: "Animal", Kind: core.KindClass, Implements: []string{"Entity"}},
			{Name: "Dog", Kind: core.KindClass, SuperTypes: []string{"Animal"}, Implements: []string{"Comparable"}},
		}}},
	}
	d, err := diagram.Build(p, diagram.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Text, "┆") && !strings.Contains(d.Text, "┄") {
		t.Errorf("expected dashed implements edge in:\n%s", d.Text)
	}
	golden(t, "implements", p, diagram.DefaultOptions())
}

// TestFileScope covers scope mode: protagonists from the given files,
// context = full ancestor chain + direct subclasses, marked and dimmed;
// no unrelated partition.
func TestFileScope(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{
			{Path: "models/base.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Module", Kind: core.KindClass, File: "models/base.py"},
			}},
			{Path: "models/net.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Network", Kind: core.KindClass, SuperTypes: []string{"Module"}, File: "models/net.py"},
			}},
			{Path: "models/resnet.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "ResNet", Kind: core.KindClass, SuperTypes: []string{"Network"}, File: "models/resnet.py"},
				{Name: "ResNet18", Kind: core.KindClass, SuperTypes: []string{"ResNet"}, File: "models/resnet.py"},
			}},
			{Path: "data/ds.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Dataset", Kind: core.KindClass, File: "data/ds.py"},
			}},
		},
	}
	opts := diagram.DefaultOptions()
	opts.Files = []string{"models/resnet.py"}

	d, err := diagram.Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	// protagonists: ResNet, ResNet18; context: Module, Network
	// out: Dataset
	ctx := map[string]bool{}
	for _, n := range d.Nodes {
		ctx[n.Name] = n.Context
	}
	want := map[string]bool{
		"ResNet": false, "ResNet18": false,
		"Module": true, "Network": true,
	}
	for name, isCtx := range want {
		got, ok := ctx[name]
		if !ok {
			t.Errorf("%s missing from scoped diagram", name)
		} else if got != isCtx {
			t.Errorf("%s context = %v, want %v", name, got, isCtx)
		}
	}
	if _, ok := ctx["Dataset"]; ok {
		t.Error("Dataset must not appear in resnet.py scope")
	}
	if strings.Contains(d.Text, "unrelated") {
		t.Error("file scope must not show the unrelated partition")
	}
	if !strings.Contains(d.Text, "·models/base.py") {
		t.Error("context node should carry its source file suffix")
	}
	golden(t, "scope", p, opts)
}

// TestComposition covers ◆ edges and orphan promotion: Insect would be an
// orphan without the composition relation.
func TestComposition(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: "world.py", Lang: "python", Symbols: []*core.Symbol{
			{Name: "Insect", Kind: core.KindClass, File: "world.py"},
			{Name: "Place", Kind: core.KindClass, File: "world.py",
				Fields: []core.Field{{Name: "insects", Type: "list[Insect]"}}},
			{Name: "Garden", Kind: core.KindClass, SuperTypes: []string{"Place"}, File: "world.py"},
		}}},
	}
	d, err := diagram.Build(p, diagram.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Text, "◆") {
		t.Errorf("composition diamond missing:\n%s", d.Text)
	}
	if strings.Contains(d.Text, "unrelated") {
		t.Error("Insect should be promoted out of the orphan partition")
	}
	golden(t, "assoc", p, diagram.DefaultOptions())

	// --assoc=false: Insect falls back to the orphan partition
	opts := diagram.DefaultOptions()
	opts.Assoc = false
	d2, err := diagram.Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d2.Text, "◆") {
		t.Error("assoc=false must not draw composition edges")
	}
	if !strings.Contains(d2.Text, "unrelated classes (1)") {
		t.Errorf("assoc=false should orphan Insect:\n%s", d2.Text)
	}
}

// TestScopeWithAssoc: composition pulls neighbors into file scope as context.
func TestScopeWithAssoc(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{
			{Path: "place.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Place", Kind: core.KindClass, File: "place.py",
					Fields: []core.Field{{Name: "insects", Type: "list[Insect]"}}},
			}},
			{Path: "insect.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Insect", Kind: core.KindClass, File: "insect.py"},
			}},
			{Path: "other.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Other", Kind: core.KindClass, File: "other.py"},
			}},
		},
	}
	opts := diagram.DefaultOptions()
	opts.Files = []string{"place.py"}
	d, err := diagram.Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	var names map[string]bool = map[string]bool{}
	for _, n := range d.Nodes {
		names[n.Name] = n.Context
	}
	if ctx, ok := names["Place"]; !ok || ctx {
		t.Error("Place should be protagonist")
	}
	if ctx, ok := names["Insect"]; !ok || !ctx {
		t.Error("Insect should be pulled in as context")
	}
	if _, ok := names["Other"]; ok {
		t.Error("Other must not appear")
	}
	golden(t, "scope_assoc", p, opts)
}

// TestPlacedNodeRelations verifies the exported layout skeleton: parent
// indexes, left-to-right children order, orphan boxes as roots.
func TestPlacedNodeRelations(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: "a.py", Lang: "python", Symbols: []*core.Symbol{
			{Name: "Root", Kind: core.KindClass, File: "a.py"},
			{Name: "Mid", Kind: core.KindClass, SuperTypes: []string{"Root"}, File: "a.py"},
			{Name: "Right", Kind: core.KindClass, SuperTypes: []string{"Root"}, File: "a.py"},
			{Name: "Leaf", Kind: core.KindClass, SuperTypes: []string{"Mid"}, File: "a.py"},
			{Name: "Lonely", Kind: core.KindClass, File: "a.py"},
		}}},
	}
	d, err := diagram.Build(p, diagram.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	for i, n := range d.Nodes {
		byName[n.Name] = i
	}
	root, mid, right, leaf, lonely := byName["Root"], byName["Mid"], byName["Right"], byName["Leaf"], byName["Lonely"]

	// parent links
	if d.Nodes[root].Parent != -1 || d.Nodes[lonely].Parent != -1 {
		t.Error("Root and Lonely should be roots (Parent=-1)")
	}
	if d.Nodes[mid].Parent != root || d.Nodes[right].Parent != root {
		t.Errorf("Mid/Right parents = %d/%d, want %d", d.Nodes[mid].Parent, d.Nodes[right].Parent, root)
	}
	if d.Nodes[leaf].Parent != mid {
		t.Errorf("Leaf parent = %d, want %d (Mid)", d.Nodes[leaf].Parent, mid)
	}
	// children ordered by visual X
	kids := d.Nodes[root].Children
	if len(kids) != 2 || kids[0] != mid || kids[1] != right {
		t.Errorf("Root children = %v, want [Mid Right] in X order", kids)
	}
	if d.Nodes[lonely].Children != nil {
		t.Error("Lonely should have no children")
	}
	// every child points back
	for i, n := range d.Nodes {
		for _, k := range n.Children {
			if d.Nodes[k].Parent != i {
				t.Errorf("node %d child %d back-pointer mismatch", i, k)
			}
		}
	}
}

func TestFocusNotFound(t *testing.T) {
	_, err := diagram.Build(proj(cls("A")), diagram.Options{Focus: "Nope", Up: -1, Down: 2})
	if err == nil {
		t.Fatal("want error for unknown focus")
	}
	var nf *diagram.ErrFocusNotFound
	if !strings.Contains(err.Error(), "Nope") {
		t.Errorf("error = %v", err)
	}
	_ = nf
}

// TestBaseRefsDisambiguate: two files each define Base; name matching alone
// picks the first-seen one, an LSP BaseRefs binding must win.
func TestBaseRefsDisambiguate(t *testing.T) {
	mk := func(withRefs bool) *core.Project {
		a := &core.Symbol{Name: "A", Kind: core.KindClass, File: "m2.py", Line: 5,
			SuperTypes: []string{"Base"}}
		if withRefs {
			a.BaseRefs = []core.Ref{{File: "m2.py", Line: 1}}
		}
		return &core.Project{Root: "/p", Files: []*core.File{
			{Path: "m1.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Base", Kind: core.KindClass, File: "m1.py", Line: 1}}},
			{Path: "m2.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Base", Kind: core.KindClass, File: "m2.py", Line: 1}, a}},
		}}
	}
	parentFile := func(p *core.Project) string {
		d, err := diagram.Build(p, diagram.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		for i, n := range d.Nodes {
			if n.Name == "A" {
				if n.Parent < 0 {
					t.Fatalf("A has no parent; nodes: %v", d.Nodes)
				}
				_ = i
				return d.Nodes[n.Parent].Sym.File
			}
		}
		t.Fatal("A not found")
		return ""
	}
	if got := parentFile(mk(false)); got != "m1.py" {
		t.Errorf("name-only match parent = %s, want m1.py (first-seen)", got)
	}
	if got := parentFile(mk(true)); got != "m2.py" {
		t.Errorf("BaseRefs-bound parent = %s, want m2.py (LSP wins)", got)
	}
}

// TestBaseRefsOutsideProject: an LSP ref pointing outside the project
// (typeshed etc.) keeps the base as an external box, not a wrong link.
func TestBaseRefsOutsideProject(t *testing.T) {
	a := &core.Symbol{Name: "A", Kind: core.KindClass, File: "a.py", Line: 3,
		SuperTypes: []string{"Base"},
		BaseRefs:   []core.Ref{{File: "../typeshed/stdlib/base.pyi", Line: 10}}}
	p := &core.Project{Root: "/p", Files: []*core.File{
		{Path: "a.py", Lang: "python", Symbols: []*core.Symbol{
			{Name: "Base", Kind: core.KindClass, File: "a.py", Line: 1}, a}},
	}}
	d, err := diagram.Build(p, diagram.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range d.Nodes {
		if n.Name == "A" {
			// gray external box: External, Sym nil, and NOT the in-project
			// same-name Base that plain name matching would have picked
			if n.Parent < 0 || !d.Nodes[n.Parent].External {
				t.Errorf("A's parent should be the gray external Base box, got %+v",
					d.Nodes[n.Parent])
			}
			return
		}
	}
	t.Fatal("A not found")
}
