// Package json renders a Project as structured JSON for scripting.
package json

import (
	"encoding/json"

	"codetree/core"
)

// Options controls JSON rendering.
type Options struct {
	Depth int // 0 = unlimited; same semantics as text renderer
	All   bool
}

// Render serializes the project to indented JSON. Kind is emitted as a
// string; depth limiting trims the Children hierarchy.
func Render(p *core.Project, opts Options) (string, error) {
	v := view(p, opts)
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// viewProject mirrors core.Project with depth-trimmed symbols.
type viewProject struct {
	Root  string      `json:"root"`
	Files []*viewFile `json:"files"`
}

type viewFile struct {
	Path    string        `json:"path"`
	Lang    string        `json:"lang"`
	Symbols []*viewSymbol `json:"symbols,omitempty"`
}

type viewSymbol struct {
	Name       string        `json:"name"`
	Kind       string        `json:"kind"`
	Detail     string        `json:"detail,omitempty"`
	Doc        string        `json:"doc,omitempty"`
	File       string        `json:"file"`
	Line       int           `json:"line"`
	SuperTypes []string      `json:"supertypes,omitempty"`
	Implements []string      `json:"implements,omitempty"`
	Fields     []core.Field  `json:"fields,omitempty"`
	Children   []*viewSymbol `json:"children,omitempty"`
}

func view(p *core.Project, opts Options) *viewProject {
	vp := &viewProject{Root: p.Root}
	for _, f := range p.Files {
		vf := &viewFile{Path: f.Path, Lang: f.Lang}
		if opts.Depth == 0 || opts.Depth >= 2 {
			vf.Symbols = viewSymbols(f.Symbols, opts, 2)
		}
		vp.Files = append(vp.Files, vf)
	}
	return vp
}

func viewSymbols(syms []*core.Symbol, opts Options, depth int) []*viewSymbol {
	var out []*viewSymbol
	for _, s := range syms {
		if !opts.All && (s.Kind == core.KindVariable || s.Kind == core.KindConstant) {
			continue
		}
		vs := &viewSymbol{
			Name: s.Name, Kind: s.Kind.String(), Detail: s.Detail,
			Doc: s.Doc, File: s.File, Line: s.Line, SuperTypes: s.SuperTypes,
			Implements: s.Implements,
			Fields:     s.Fields,
		}
		if opts.Depth == 0 || depth < opts.Depth {
			vs.Children = viewSymbols(s.Children, opts, depth+1)
		}
		out = append(out, vs)
	}
	return out
}
