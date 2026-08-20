// Package golang implements Go support using the standard library go/ast —
// no cgo, proving the language registry is pluggable.
package golang

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"

	"codetree/core"
	"codetree/langs"
)

type lang struct{}

func (lang) Name() string         { return "go" }
func (lang) Extensions() []string { return []string{".go"} }

func init() { langs.Register(lang{}) }

// Parse extracts the symbol tree of one Go source file.
func (lang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var top []*core.Symbol
	types := map[string]*core.Symbol{} // type name -> symbol, for method attachment

	line := func(pos token.Pos) int { return fset.Position(pos).Line }

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					sym := typeSymbol(sp, src, fset)
					if sym == nil {
						continue
					}
					sym.File = path
					sym.Line = line(sp.Pos())
					sym.Doc = firstPara(docText(sp.Doc, d.Doc))
					top = append(top, sym)
					types[sym.Name] = sym
				case *ast.ValueSpec:
					if !opts.IncludeVars {
						continue
					}
					kind := core.KindVariable
					if d.Tok == token.CONST {
						kind = core.KindConstant
					}
					for _, name := range sp.Names {
						if !name.IsExported() && !opts.IncludeVars {
							continue
						}
						top = append(top, &core.Symbol{
							Name: name.Name, Kind: kind,
							File: path, Line: line(name.Pos()),
						})
					}
				}
			}
		case *ast.FuncDecl:
			sig := funcSignature(fset, d)
			if d.Recv == nil || len(d.Recv.List) == 0 {
				top = append(top, &core.Symbol{
					Name: d.Name.Name, Kind: core.KindFunction,
					Detail: sig, Doc: firstPara(docText(d.Doc)),
					File: path, Line: line(d.Pos()),
				})
				continue
			}
			recvType := recvTypeName(d.Recv.List[0].Type)
			sym := &core.Symbol{
				Name: d.Name.Name, Kind: core.KindMethod,
				Detail: sig, Doc: firstPara(docText(d.Doc)),
				File: path, Line: line(d.Pos()),
			}
			if owner, ok := types[recvType]; ok {
				owner.Children = append(owner.Children, sym)
			} else {
				// Type declared elsewhere (other file or generated): keep the
				// method visible, annotated with its receiver.
				sym.Detail = "(" + recvType + ") " + sig
				top = append(top, sym)
			}
		}
	}
	return top, nil
}

func typeSymbol(sp *ast.TypeSpec, src []byte, fset *token.FileSet) *core.Symbol {
	sym := &core.Symbol{Name: sp.Name.Name}
	switch t := sp.Type.(type) {
	case *ast.StructType:
		sym.Kind = core.KindStruct
		sym.Fields = structFields(fset, t)
	case *ast.InterfaceType:
		sym.Kind = core.KindInterface
	default:
		_ = t
		return nil // type aliases and other decls: out of v1 scope
	}
	return sym
}

// structFields extracts field name+type pairs; embedded fields are marked.
func structFields(fset *token.FileSet, st *ast.StructType) []core.Field {
	var out []core.Field
	for _, f := range st.Fields.List {
		typ := exprString(fset, f.Type)
		if len(f.Names) == 0 { // embedded field
			out = append(out, core.Field{Name: typ, Embedded: true})
			continue
		}
		for _, name := range f.Names {
			out = append(out, core.Field{Name: name.Name, Type: typ})
		}
	}
	return out
}

func exprString(fset *token.FileSet, e ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		return ""
	}
	return buf.String()
}

// funcSignature renders "(a int, b string) error" from a FuncDecl.
func funcSignature(fset *token.FileSet, d *ast.FuncDecl) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, d.Type); err != nil {
		return ""
	}
	s := buf.String() // "func(a int) error"
	return strings.TrimSpace(strings.TrimPrefix(s, "func"))
}

// recvTypeName extracts the base type name from a receiver expression,
// handling *T, T, and generic T[P].
func recvTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr: // generic: T[P]
		return recvTypeName(e.X)
	case *ast.IndexListExpr:
		return recvTypeName(e.X)
	}
	return ""
}

func docText(groups ...*ast.CommentGroup) string {
	for _, g := range groups {
		if g != nil {
			return g.Text()
		}
	}
	return ""
}

func firstPara(s string) string {
	if i := strings.Index(s, "\n\n"); i >= 0 {
		s = s[:i]
	}
	return strings.Join(strings.Fields(s), " ")
}
