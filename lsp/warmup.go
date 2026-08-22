// Project：CodeTree
// Author：RollingTheRock
// Date: 2026.8.21

package lsp

import (
	"context"
	"time"

	"go.lsp.dev/protocol"

	"github.com/RollingTheRock/CodeTree/core"
)

// Status is the LSP layer's state for the status bar.
type Status int

const (
	StatusAbsent  Status = iota // no server configured/found — stay static
	StatusWarming               // server starting / requests in flight
	StatusReady                 // corrections applied
	StatusFailed                // server found but handshake/run failed
	StatusStale                 // corrections were applied, then files changed
)

func (s Status) String() string {
	switch s {
	case StatusWarming:
		return "warming"
	case StatusReady:
		return "ready"
	case StatusFailed:
		return "failed"
	case StatusStale:
		return "stale"
	default:
		return "absent"
	}
}

// Outcome is the result of one LSP pass over the project.
type Outcome struct {
	Status   Status
	Diff     Diff
	Startup  time.Duration // slowest server handshake
	Requests int           // definition+hover+documentSymbol+implementation requests made
	Langs    []string      // languages whose server became ready
	Err      error         // non-nil when Status == StatusFailed
}

// maxFilesPerPass caps per-language file work so huge projects stay snappy.
const maxFilesPerPass = 200

// MinFilesPerLang is the minimum number of files a language must have in the
// project before a server is started for it. It keeps stray files (fixtures,
// vendored snippets) from fanning out to servers for languages the project
// isn't really written in. The CLI lowers this for the explicit --lsp flag.
var MinFilesPerLang = 5

// langOrder is the deterministic processing order for Collect.
var langOrder = []string{"python", "go", "cpp", "java", "rust", "typescript", "javascript"}

// test seams: the server probe and client factory are replaceable.
var (
	resolveServerFn = ResolveServer
	starter         = func(ctx context.Context, cfg ServerConfig, root, langKey string) (*Client, error) {
		return Start(ctx, cfg, root, langKey)
	}
)

// Warm runs one synchronous LSP pass and applies the corrections to proj in
// place. CLI use. The TUI uses Collect instead and applies on its own
// goroutine (the bubbletea main loop) to avoid racing the renderer.
func Warm(ctx context.Context, root string, proj *core.Project) Outcome {
	out, corr := Collect(ctx, root, proj)
	if out.Status == StatusReady {
		out.Diff = Apply(proj, corr)
	}
	return out
}

// Collect gathers LSP corrections without touching proj. For each language
// present in the project that has a resolvable server, it starts one client
// and runs: definition (base disambiguation) + hover (field types) +
// documentSymbol (class augmentation) + implementation (Go interfaces).
// proj is only read.
func Collect(ctx context.Context, root string, proj *core.Project) (Outcome, Corrections) {
	var out Outcome
	var corr Corrections

	present := map[string]bool{}
	for _, f := range proj.Files {
		present[f.Lang] = true
	}

	anyFound := false
	for _, lang := range langOrder {
		if !present[lang] {
			continue
		}
		// skip stray-file languages (test fixtures etc.): starting a server
		// costs seconds of CPU and gains nothing for them
		if len(filesOfLang(proj, lang)) < MinFilesPerLang {
			continue
		}
		cfg, ok := resolveServerFn(lang)
		if !ok {
			continue
		}
		anyFound = true
		lo, lc := collectLang(ctx, root, proj, lang, cfg)
		if lo.Err != nil {
			out.Err = lo.Err
			continue
		}
		out.Langs = append(out.Langs, lang)
		out.Requests += lo.Requests
		if lo.Startup > out.Startup {
			out.Startup = lo.Startup
		}
		corr.Bases = append(corr.Bases, lc.Bases...)
		corr.Fields = append(corr.Fields, lc.Fields...)
		corr.Impls = append(corr.Impls, lc.Impls...)
		corr.Added = append(corr.Added, lc.Added...)
	}

	switch {
	case len(out.Langs) > 0:
		out.Status = StatusReady
	case anyFound:
		out.Status = StatusFailed
	default:
		out.Status = StatusAbsent
	}
	return out, corr
}

// collectLang runs one language's pass with its own client.
func collectLang(ctx context.Context, root string, proj *core.Project, lang string, cfg ServerConfig) (Outcome, Corrections) {
	var out Outcome
	var corr Corrections

	files := filesOfLang(proj, lang)
	if len(files) == 0 {
		return out, corr
	}

	t0 := time.Now()
	client, err := starter(ctx, cfg, root, lang)
	if err != nil {
		out.Err = err
		return out, corr
	}
	defer client.Shutdown(ctx)
	out.Startup = time.Since(t0)

	for _, f := range files {
		for _, s := range f.AllSymbols() {
			if !s.Kind.ClassLike() {
				continue
			}
			// base disambiguation: definition at each base token
			for i := range s.SuperTypes {
				if i >= len(s.BasePos) {
					break
				}
				locs, err := client.Definition(ctx, f.Path, s.BasePos[i])
				out.Requests++
				if err != nil || len(locs) == 0 {
					continue
				}
				loc := locs[0]
				corr.Bases = append(corr.Bases, BaseBinding{
					File: f.Path, ClassLine: s.Line, BaseIndex: i,
					TargetFile: URIToRel(root, loc.URI),
					TargetLine: int(loc.Range.Start.Line) + 1,
				})
			}
			// field types: hover where inference left the type empty
			for _, fld := range s.Fields {
				if fld.Type != "" || fld.Line == 0 {
					continue
				}
				typ, err := client.HoverType(ctx, f.Path, core.Pos{Line: fld.Line, Col: fld.Col})
				out.Requests++
				if err != nil || typ == "" {
					continue
				}
				corr.Fields = append(corr.Fields, FieldType{File: f.Path, Line: fld.Line, Col: fld.Col, Type: typ})
			}
			// Go: interface implementations
			if lang == "go" && s.Kind == core.KindInterface && s.Line > 0 {
				locs, err := client.Implementation(ctx, f.Path, core.Pos{Line: s.Line, Col: s.Col})
				out.Requests++
				if err != nil {
					continue
				}
				for _, loc := range locs {
					corr.Impls = append(corr.Impls, ImplBinding{
						InterfaceFile: f.Path, InterfaceLine: s.Line,
						ImplFile: URIToRel(root, loc.URI),
						ImplLine: int(loc.Range.Start.Line) + 1,
					})
				}
			}
		}
		// class augmentation: documentSymbol catches dynamic class assignments
		syms, err := client.DocumentSymbols(ctx, f.Path)
		out.Requests++
		if err != nil {
			continue
		}
		corr.Added = append(corr.Added, convertSymbols(f.Path, syms)...)
	}
	return out, corr
}

func filesOfLang(p *core.Project, lang string) []*core.File {
	var out []*core.File
	for _, f := range p.Files {
		if f.Lang == lang {
			out = append(out, f)
		}
	}
	if len(out) > maxFilesPerPass {
		out = out[:maxFilesPerPass]
	}
	return out
}

// convertSymbols maps LSP DocumentSymbols to core symbols (classes only).
func convertSymbols(file string, syms protocol.DocumentSymbolSlice) []*core.Symbol {
	var out []*core.Symbol
	for _, s := range syms {
		var kind core.Kind
		switch s.Kind {
		case protocol.SymbolKindClass:
			kind = core.KindClass
		case protocol.SymbolKindInterface:
			kind = core.KindInterface
		case protocol.SymbolKindStruct:
			kind = core.KindStruct
		case protocol.SymbolKindEnum:
			kind = core.KindEnum
		default:
			// still recurse: classes hide inside functions etc.
			out = append(out, convertSymbols(file, s.Children)...)
			continue
		}
		sym := &core.Symbol{
			Name:   s.Name,
			Kind:   kind,
			File:   file,
			Line:   int(s.Range.Start.Line) + 1,
			Source: "lsp",
		}
		out = append(out, sym)
		out = append(out, convertSymbols(file, s.Children)...)
	}
	return out
}
