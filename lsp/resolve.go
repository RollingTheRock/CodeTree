// Package lsp is the optional semantic layer: it warms up a language server
// (Python only for now), corrects static-analysis facts and augments the
// model. The static path stays fully intact; when no server is installed the
// layer is silently absent.
package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"codetree/core"
)

// BaseBinding is a resolved base-class mention: the class declared at
// File:ClassLine, its BaseIndex-th SuperTypes entry binds to the definition
// at TargetFile:TargetLine.
type BaseBinding struct {
	File       string // class declaration file (project-relative)
	ClassLine  int    // class declaration line (1-based)
	BaseIndex  int    // index into Symbol.SuperTypes
	TargetFile string
	TargetLine int
}

// FieldType is a hover-resolved field type at File:Line:Col.
type FieldType struct {
	File      string
	Line, Col int
	Type      string
}

// ImplBinding is a resolved interface implementation: the interface declared
// at InterfaceFile:InterfaceLine is implemented by the type at
// ImplFile:ImplLine (Go textDocument/implementation).
type ImplBinding struct {
	InterfaceFile string
	InterfaceLine int
	ImplFile      string
	ImplLine      int
}

// Corrections is everything one LSP pass learned about the project.
type Corrections struct {
	Bases  []BaseBinding
	Fields []FieldType
	Impls  []ImplBinding
	Added  []*core.Symbol
}

// Diff summarizes what the LSP pass changed — printed by `ct --lsp`.
type Diff struct {
	ReboundBases []string // "Dog base Animal → models/base.py:8"
	FilledFields []string // "Network.stem: ConvBlock"
	AddedImpls   []string // "Dog implements Speaker"
	AddedClasses []string // "Color (models/base.py)"
}

// Empty reports whether the pass changed nothing.
func (d Diff) Empty() bool {
	return len(d.ReboundBases) == 0 && len(d.FilledFields) == 0 &&
		len(d.AddedImpls) == 0 && len(d.AddedClasses) == 0
}

func (d Diff) String() string {
	var b strings.Builder
	for _, s := range d.ReboundBases {
		fmt.Fprintf(&b, "~ base %s\n", s)
	}
	for _, s := range d.FilledFields {
		fmt.Fprintf(&b, "~ field %s\n", s)
	}
	for _, s := range d.AddedImpls {
		fmt.Fprintf(&b, "~ impl %s\n", s)
	}
	for _, s := range d.AddedClasses {
		fmt.Fprintf(&b, "+ class %s\n", s)
	}
	if d.Empty() {
		b.WriteString("(no corrections)\n")
	}
	return b.String()
}

// Apply merges LSP corrections into the project (in place) and returns the
// diff. Pure and server-free: unit tests drive it directly.
func Apply(p *core.Project, c Corrections) Diff {
	var d Diff

	for _, b := range c.Bases {
		// bindings outside the project (stdlib etc.) can't be matched to
		// graph nodes; skip them rather than printing ../.. paths
		if strings.HasPrefix(b.TargetFile, "..") {
			continue
		}
		s := findSymbolAt(p, b.File, b.ClassLine)
		if s == nil || b.BaseIndex >= len(s.SuperTypes) {
			continue
		}
		for len(s.BaseRefs) < len(s.SuperTypes) {
			s.BaseRefs = append(s.BaseRefs, core.Ref{})
		}
		ref := core.Ref{File: b.TargetFile, Line: b.TargetLine}
		if s.BaseRefs[b.BaseIndex] != ref {
			s.BaseRefs[b.BaseIndex] = ref
			d.ReboundBases = append(d.ReboundBases, fmt.Sprintf("%s.%s → %s:%d",
				s.Name, s.SuperTypes[b.BaseIndex], b.TargetFile, b.TargetLine))
		}
	}

	for _, f := range c.Fields {
		if f.Type == "" {
			continue
		}
		sym := findFieldOwner(p, f.File, f.Line, f.Col)
		if sym == nil {
			continue
		}
		for i := range sym.Fields {
			// static annotation wins; only fill what inference left empty
			if sym.Fields[i].Line == f.Line && sym.Fields[i].Col == f.Col && sym.Fields[i].Type == "" {
				sym.Fields[i].Type = f.Type
				d.FilledFields = append(d.FilledFields, fmt.Sprintf("%s.%s: %s", sym.Name, sym.Fields[i].Name, f.Type))
			}
		}
	}

	for _, b := range c.Impls {
		iface := findSymbolAt(p, b.InterfaceFile, b.InterfaceLine)
		impl := findSymbolAt(p, b.ImplFile, b.ImplLine)
		if iface == nil || impl == nil {
			continue
		}
		dup := false
		for _, in := range impl.Implements {
			if in == iface.Name {
				dup = true
			}
		}
		if !dup {
			impl.Implements = append(impl.Implements, iface.Name)
			d.AddedImpls = append(d.AddedImpls, fmt.Sprintf("%s implements %s", impl.Name, iface.Name))
		}
	}

	for _, s := range c.Added {
		if findClassInFile(p, s.File, s.Name) != nil {
			continue // already known statically
		}
		f := findFile(p, s.File)
		if f == nil {
			f = &core.File{Path: s.File, Lang: langOfFile(p, s.File)}
			p.Files = append(p.Files, f)
		}
		s.Source = "lsp"
		f.Symbols = append(f.Symbols, s)
		d.AddedClasses = append(d.AddedClasses, fmt.Sprintf("%s (%s:%d)", s.Name, s.File, s.Line))
	}
	return d
}

// findSymbolAt locates a symbol declared exactly at file:line (1-based).
func findSymbolAt(p *core.Project, file string, line int) *core.Symbol {
	for _, s := range p.AllSymbols() {
		if s.File == file && s.Line == line {
			return s
		}
	}
	return nil
}

// findFieldOwner locates the class-like symbol whose Fields contain the
// position file:line:col.
func findFieldOwner(p *core.Project, file string, line, col int) *core.Symbol {
	for _, s := range p.AllSymbols() {
		if s.File != file {
			continue
		}
		for _, f := range s.Fields {
			if f.Line == line && f.Col == col {
				return s
			}
		}
	}
	return nil
}

func findClassInFile(p *core.Project, file, name string) *core.Symbol {
	for _, s := range p.AllSymbols() {
		if s.File == file && s.Name == name &&
			(s.Kind == core.KindClass || s.Kind == core.KindInterface ||
				s.Kind == core.KindStruct || s.Kind == core.KindEnum) {
			return s
		}
	}
	return nil
}

func findFile(p *core.Project, path string) *core.File {
	for _, f := range p.Files {
		if f.Path == path {
			return f
		}
	}
	return nil
}

// langOfFile infers the language for a file added by the server (rare:
// usually the file already exists in the project).
func langOfFile(p *core.Project, path string) string {
	for _, f := range p.Files {
		if filepath.Ext(f.Path) == filepath.Ext(path) {
			return f.Lang
		}
	}
	return ""
}
