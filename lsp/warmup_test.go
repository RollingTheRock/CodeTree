package lsp

import (
	"context"
	"testing"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"codetree/core"
)

// TestWarmEndToEnd drives a full pass against the in-process fake server:
// base binding + field type fill + class augmentation, merged into a project.
func TestWarmEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "models/dog.py", "from models.base import Animal\n\nclass Dog(Animal):\n    def __init__(self):\n        self.tricks = make_tricks()\n")

	proj := &core.Project{
		Root: root,
		Files: []*core.File{
			{Path: "models/base.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Animal", Kind: core.KindClass, File: "models/base.py", Line: 3},
			}},
			{Path: "models/dog.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Dog", Kind: core.KindClass, File: "models/dog.py", Line: 3,
					SuperTypes: []string{"Animal"},
					BasePos:    []core.Pos{{Line: 3, Col: 10}},
					Fields:     []core.Field{{Name: "tricks", Line: 5, Col: 13}}},
			}},
		},
	}

	baseURI := uri.File(root + "/models/base.py")
	// swap in the fake
	oldResolve, oldStarter := resolveServerFn, starter
	defer func() { resolveServerFn, starter = oldResolve, oldStarter }()
	resolveServerFn = func(lang string) (ServerConfig, bool) { return ServerConfig{Command: "fake"}, true }
	starter = func(ctx context.Context, cfg ServerConfig, r, langKey string) (*Client, error) {
		stream := fakeServer(t, func(method string, params jsonrpc2.RawMessage) any {
			switch method {
			case "initialize":
				return protocol.InitializeResult{}
			case "textDocument/definition":
				return protocol.LocationSlice{{
					URI:   baseURI,
					Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 6}},
				}}
			case "textDocument/hover":
				return &protocol.Hover{Contents: &protocol.MarkupContent{
					Kind: protocol.MarkupKindMarkdown, Value: "(variable) tricks: list[str]"}}
			case "textDocument/documentSymbol":
				return protocol.DocumentSymbolSlice{{
					Name: "Dog", Kind: protocol.SymbolKindClass,
					Range:          protocol.Range{Start: protocol.Position{Line: 2}},
					SelectionRange: protocol.Range{Start: protocol.Position{Line: 2}},
				}, {
					Name: "Trick", Kind: protocol.SymbolKindClass, // statically missed
					Range:          protocol.Range{Start: protocol.Position{Line: 9}},
					SelectionRange: protocol.Range{Start: protocol.Position{Line: 9}},
				}}
			}
			return nil
		})
		return newClient(ctx, stream, r)
	}

	out := Warm(context.Background(), root, proj)
	if out.Status != StatusReady {
		t.Fatalf("status = %v, err = %v", out.Status, out.Err)
	}
	if out.Requests < 3 {
		t.Errorf("requests = %d, want >= 3 (definition+hover+documentSymbol)", out.Requests)
	}

	dog := proj.Files[1].Symbols[0]
	if len(dog.BaseRefs) != 1 || dog.BaseRefs[0].File != "models/base.py" || dog.BaseRefs[0].Line != 3 {
		t.Errorf("BaseRefs = %+v", dog.BaseRefs)
	}
	if dog.Fields[0].Type != "list[str]" {
		t.Errorf("tricks.Type = %q", dog.Fields[0].Type)
	}
	var trick *core.Symbol
	for _, s := range proj.AllSymbols() {
		if s.Name == "Trick" {
			trick = s
		}
	}
	if trick == nil || trick.Source != "lsp" {
		t.Errorf("Trick not augmented: %+v", trick)
	}
	if out.Diff.Empty() {
		t.Error("diff should not be empty")
	}
	t.Logf("diff:\n%s", out.Diff)
}

func TestWarmAbsent(t *testing.T) {
	old := resolveServerFn
	defer func() { resolveServerFn = old }()
	resolveServerFn = func(lang string) (ServerConfig, bool) { return ServerConfig{}, false }

	out := Warm(context.Background(), t.TempDir(), &core.Project{})
	if out.Status != StatusAbsent {
		t.Errorf("status = %v, want absent", out.Status)
	}
}

func TestResolveServer(t *testing.T) {
	// config entry wins
	cfg := fileConfig{LSP: map[string]ServerConfig{
		"python": {Command: "sh", Args: []string{"--stdio"}}, // sh always in PATH
	}}
	sc, ok := resolveServer("python", cfg)
	if !ok || sc.Command != "sh" {
		t.Errorf("config override failed: %+v %v", sc, ok)
	}

	// enabled=false disables
	off := false
	cfg = fileConfig{LSP: map[string]ServerConfig{
		"python": {Command: "sh", Enabled: &off},
	}}
	if _, ok := resolveServer("python", cfg); ok {
		t.Error("enabled=false should disable")
	}

	// configured but missing binary → absent
	cfg = fileConfig{LSP: map[string]ServerConfig{
		"python": {Command: "definitely-not-a-real-server-xyz"},
	}}
	if _, ok := resolveServer("python", cfg); ok {
		t.Error("missing binary should be absent")
	}

	// unknown language → absent
	if _, ok := resolveServer("cobol", fileConfig{}); ok {
		t.Error("cobol should be absent")
	}
}

// TestCollectMultiLang: python+go project, two fake servers, corrections merge.
func TestCollectMultiLang(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "a.py", "class A: pass\n")
	writeSource(t, root, "s.go", "package s\n\ntype Speaker interface{}\n\ntype Dog struct{}\n")

	proj := &core.Project{
		Root: root,
		Files: []*core.File{
			{Path: "a.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "A", Kind: core.KindClass, File: "a.py", Line: 1},
			}},
			{Path: "s.go", Lang: "go", Symbols: []*core.Symbol{
				{Name: "Speaker", Kind: core.KindInterface, File: "s.go", Line: 3, Col: 5},
				{Name: "Dog", Kind: core.KindStruct, File: "s.go", Line: 5, Col: 5},
			}},
		},
	}

	goURI := uri.File(root + "/s.go")
	oldResolve, oldStarter := resolveServerFn, starter
	defer func() { resolveServerFn, starter = oldResolve, oldStarter }()
	resolveServerFn = func(lang string) (ServerConfig, bool) { return ServerConfig{Command: "fake-" + lang}, true }
	starter = func(ctx context.Context, cfg ServerConfig, r, langKey string) (*Client, error) {
		stream := fakeServer(t, func(method string, params jsonrpc2.RawMessage) any {
			switch {
			case method == "initialize":
				return protocol.InitializeResult{}
			case method == "textDocument/implementation" && langKey == "go":
				return protocol.LocationSlice{{
					URI:   goURI,
					Range: protocol.Range{Start: protocol.Position{Line: 4, Character: 5}},
				}}
			case method == "textDocument/documentSymbol":
				return protocol.DocumentSymbolSlice{}
			}
			return nil
		})
		return newClient(ctx, stream, r)
	}

	out, corr := Collect(context.Background(), root, proj)
	if out.Status != StatusReady {
		t.Fatalf("status = %v err = %v", out.Status, out.Err)
	}
	if len(out.Langs) != 2 {
		t.Errorf("langs = %v, want [python go]", out.Langs)
	}
	if len(corr.Impls) != 1 || corr.Impls[0].ImplLine != 5 {
		t.Fatalf("impls = %+v", corr.Impls)
	}
	d := Apply(proj, corr)
	if len(d.AddedImpls) != 1 {
		t.Errorf("diff = %v", d)
	}
	if got := proj.Files[1].Symbols[1].Implements; len(got) != 1 || got[0] != "Speaker" {
		t.Errorf("Dog.Implements = %v", got)
	}
}

// TestCollectAbsentWhenNoServer: language present but server missing → absent.
func TestCollectAbsentWhenNoServer(t *testing.T) {
	old := resolveServerFn
	defer func() { resolveServerFn = old }()
	resolveServerFn = func(lang string) (ServerConfig, bool) { return ServerConfig{}, false }
	proj := &core.Project{Root: "/p", Files: []*core.File{{Path: "a.go", Lang: "go"}}}
	out, _ := Collect(context.Background(), t.TempDir(), proj)
	if out.Status != StatusAbsent {
		t.Errorf("status = %v", out.Status)
	}
}
