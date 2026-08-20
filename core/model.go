// Package core defines the language-agnostic symbol model and the project
// scanner. It contains pure logic only: no rendering, no UI.
package core

import "strings"

// Kind classifies a symbol.
type Kind int

const (
	KindModule Kind = iota
	KindClass
	KindMethod
	KindFunction
	KindInterface
	KindStruct
	KindEnum
	KindConstant
	KindVariable
)

func (k Kind) String() string {
	switch k {
	case KindModule:
		return "module"
	case KindClass:
		return "class"
	case KindMethod:
		return "method"
	case KindFunction:
		return "func"
	case KindInterface:
		return "interface"
	case KindStruct:
		return "struct"
	case KindEnum:
		return "enum"
	case KindConstant:
		return "const"
	case KindVariable:
		return "variable"
	default:
		return "unknown"
	}
}

// KindLabel returns the short suffix shown after a symbol in text output,
// e.g. "Animal (class)". Methods and variables render bare.
func (k Kind) KindLabel() string {
	switch k {
	case KindMethod, KindVariable:
		return ""
	default:
		return "(" + k.String() + ")"
	}
}

// Field is a member variable of a class/struct.
type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`     // from annotation or value inference; may be empty
	ClassVar bool   `json:"classvar,omitempty"` // Python: class-body attribute vs self.x instance attribute
	Embedded bool   `json:"embedded,omitempty"` // Go: embedded struct field
}

// Symbol is a node in the code structure map.
type Symbol struct {
	Name     string    `json:"name"`
	Kind     Kind      `json:"-"`
	KindName string    `json:"kind"`
	Detail   string    `json:"detail,omitempty"` // Python: "(Animal)" bases, decorator summary; Go: receiver, signature
	Doc      string    `json:"doc,omitempty"`    // first paragraph of docstring / doc comment
	File     string    `json:"file"`             // path relative to project root
	Line     int       `json:"line"`             // 1-based
	Children []*Symbol `json:"children,omitempty"`
	Fields   []Field   `json:"fields,omitempty"` // class/struct member variables

	// SuperTypes is the text-level base class list in v1 (static analysis).
	// v2 will fill precise resolved types from LSP.
	SuperTypes []string `json:"supertypes,omitempty"`

	// Implements lists interfaces this type implements (Java/C#-style).
	// Distinct from SuperTypes so diagrams can render dashed implements
	// edges vs solid extends edges. Empty for Python/Go (v2 LSP may fill).
	Implements []string `json:"implements,omitempty"`
}

// Label renders one symbol line: "Dog(Animal) (class)", "speak(self)",
// "feed async (animal)".
func (s *Symbol) Label() string {
	name := s.Name
	if s.Detail != "" {
		sep := ""
		if !strings.HasPrefix(s.Detail, "(") {
			sep = " "
		}
		name += sep + s.Detail
	}
	if k := s.Kind.KindLabel(); k != "" {
		name += " " + k
	}
	return name
}

// File is one source file with its top-level symbols.
type File struct {
	Path    string    `json:"path"` // relative to project root
	Lang    string    `json:"lang"`
	Symbols []*Symbol `json:"symbols,omitempty"`
}

// AllSymbols flattens every symbol in the file (depth-first).
func (f *File) AllSymbols() []*Symbol {
	var out []*Symbol
	var walk func(syms []*Symbol)
	walk = func(syms []*Symbol) {
		for _, s := range syms {
			out = append(out, s)
			walk(s.Children)
		}
	}
	walk(f.Symbols)
	return out
}

// Project is the scanned result for a directory tree.
type Project struct {
	Root  string  `json:"root"`
	Files []*File `json:"files"`
}

// AllSymbols flattens every symbol in the project (depth-first).
func (p *Project) AllSymbols() []*Symbol {
	var out []*Symbol
	var walk func(syms []*Symbol)
	walk = func(syms []*Symbol) {
		for _, s := range syms {
			out = append(out, s)
			walk(s.Children)
		}
	}
	for _, f := range p.Files {
		walk(f.Symbols)
	}
	return out
}
