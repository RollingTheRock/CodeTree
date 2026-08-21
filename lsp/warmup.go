package lsp

import (
	"context"
	"time"

	"go.lsp.dev/protocol"

	"codetree/core"
)

// Status is the LSP layer's state for the status bar.
type Status int

const (
	StatusAbsent  Status = iota // no server configured/found — stay static
	StatusWarming               // server starting / requests in flight
	StatusReady                 // corrections applied
	StatusFailed                // server found but handshake/run failed
)

func (s Status) String() string {
	switch s {
	case StatusWarming:
		return "warming"
	case StatusReady:
		return "ready"
	case StatusFailed:
		return "failed"
	default:
		return "absent"
	}
}

// Outcome is the result of one LSP pass over the project.
type Outcome struct {
	Status    Status
	Diff      Diff
	Startup   time.Duration // initialize handshake latency
	Requests  int           // definition+hover+documentSymbol requests made
	Err       error         // non-nil when Status == StatusFailed
}

// maxFilesPerPass caps per-file work so huge projects stay snappy; the pass
// covers the files with the most class symbols first.
const maxFilesPerPass = 200

// test seams: the server probe and client factory are replaceable.
var (
	resolveServerFn = ResolveServer
	starter         = func(ctx context.Context, cfg ServerConfig, root string) (*Client, error) {
		return Start(ctx, cfg, root)
	}
)

// Warm runs one synchronous LSP pass and applies the corrections to proj in
// place. CLI use. The TUI uses Collect instead and applies on its own
// goroutine (the bubbletea main loop) to avoid racing the renderer.
func Warm(ctx context.Context, root string, proj *core.Project) Outcome {
	out, bases, fields, added := Collect(ctx, root, proj)
	if out.Status == StatusReady {
		out.Diff = Apply(proj, bases, fields, added)
	}
	return out
}

// Collect gathers LSP corrections without touching proj: probe server →
// handshake → didOpen + definition (base disambiguation) + hover (field
// types) + documentSymbol (class augmentation). proj is only read.
//
// serverURI/ports: none — stdio only.
func Collect(ctx context.Context, root string, proj *core.Project) (Outcome, []BaseBinding, []FieldType, []*core.Symbol) {
	cfg, ok := resolveServerFn("python")
	if !ok {
		return Outcome{Status: StatusAbsent}, nil, nil, nil
	}
	files := pythonFiles(proj)
	if len(files) == 0 {
		return Outcome{Status: StatusAbsent}, nil, nil, nil
	}

	t0 := time.Now()
	client, err := starter(ctx, cfg, root)
	if err != nil {
		return Outcome{Status: StatusFailed, Err: err}, nil, nil, nil
	}
	defer client.Shutdown(ctx)
	startup := time.Since(t0)

	out := Outcome{Status: StatusReady, Startup: startup}

	var bases []BaseBinding
	var fields []FieldType
	var added []*core.Symbol

	for _, f := range files {
		for _, s := range f.AllSymbols() {
			if !isClassLike(s) {
				continue
			}
			// base disambiguation: definition at each base token
			for i := range s.SuperTypes {
				if i >= len(s.BasePos) {
					break
				}
				pos := s.BasePos[i]
				locs, err := client.Definition(ctx, f.Path, pos)
				out.Requests++
				if err != nil || len(locs) == 0 {
					continue
				}
				loc := locs[0]
				bases = append(bases, BaseBinding{
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
				fields = append(fields, FieldType{File: f.Path, Line: fld.Line, Col: fld.Col, Type: typ})
			}
		}
		// class augmentation: documentSymbol catches dynamic class assignments
		syms, err := client.DocumentSymbols(ctx, f.Path)
		out.Requests++
		if err != nil {
			continue
		}
		added = append(added, convertSymbols(f.Path, syms)...)
	}

	return out, bases, fields, added
}

func pythonFiles(p *core.Project) []*core.File {
	var out []*core.File
	for _, f := range p.Files {
		if f.Lang == "python" {
			out = append(out, f)
		}
	}
	if len(out) > maxFilesPerPass {
		out = out[:maxFilesPerPass]
	}
	return out
}

func isClassLike(s *core.Symbol) bool {
	switch s.Kind {
	case core.KindClass, core.KindInterface, core.KindStruct, core.KindEnum:
		return true
	}
	return false
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
