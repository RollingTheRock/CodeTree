// Package jsts implements JavaScript and TypeScript support via tree-sitter
// queries (cgo). "javascript" covers .js/.jsx; "typescript" covers .ts/.tsx
// (the tsx grammar handles .tsx).
package jsts

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/langs"
	"github.com/RollingTheRock/CodeTree/langs/tsutil"
)

// JS grammar uses (identifier) for class names, TS uses (type_identifier).
// The bundled JS grammar has no field_definition node; JS fields are skipped.
const jsQuery = `
(class_declaration
  name: (identifier) @name
  (class_heritage)? @heritage) @class

(method_definition
  name: (property_identifier) @name
  parameters: (formal_parameters) @params) @method

(function_declaration
  name: (identifier) @name
  parameters: (formal_parameters) @params) @func
`

const tsQuery = `
(class_declaration
  name: (type_identifier) @name
  (class_heritage)? @heritage) @class

(interface_declaration
  name: (type_identifier) @name) @iface

(enum_declaration
  name: (identifier) @name) @enum

(method_definition
  name: (property_identifier) @name
  parameters: (formal_parameters) @params) @method

(public_field_definition
  name: (property_identifier) @field.name
  type: (type_annotation)? @field.type) @field

(function_declaration
  name: (identifier) @name
  parameters: (formal_parameters) @params) @func
`

type jsLang struct{}

func (jsLang) Name() string         { return "javascript" }
func (jsLang) Extensions() []string { return []string{".js", ".jsx"} }

type tsLang struct{}

func (tsLang) Name() string         { return "typescript" }
func (tsLang) Extensions() []string { return []string{".ts", ".tsx"} }

func init() {
	langs.Register(jsLang{})
	langs.Register(tsLang{})
}

// Parse implements javascript.
func (jsLang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	return parse(javascript.GetLanguage(), jsQuery, path, src)
}

// Parse implements typescript (picks the tsx grammar for .tsx).
func (tsLang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	lang := typescript.GetLanguage()
	if strings.HasSuffix(path, ".tsx") {
		lang = tsx.GetLanguage()
	}
	return parse(lang, tsQuery, path, src)
}

type item struct {
	node *sitter.Node
	sym  *core.Symbol
	typ  string // field type
	pos  core.Pos
}

func parse(language *sitter.Language, query, path string, src []byte) ([]*core.Symbol, error) {
	root, err := tsutil.Parse(language, src)
	if err != nil {
		return nil, err
	}

	var items []*item
	err = tsutil.Each(language, query, root, func(c tsutil.Captures) {
		defKind := ""
		for _, k := range []string{"class", "iface", "enum", "method", "func", "field"} {
			if c[k] != nil {
				defKind = k
				break
			}
		}
		defNode := c[defKind]
		var fieldTyp string
		var it item
		nameNode := c["name"]
		heritageNode := c["heritage"]
		paramsNode := c["params"]
		if fn := c["field.name"]; fn != nil {
			nameNode = fn // also serves as the field's name
			it.pos = tsutil.Pos(fn)
		}
		if ft := c["field.type"]; ft != nil {
			fieldTyp = stripAnnotation(tsutil.Content(ft, src))
		}
		if defNode == nil || nameNode == nil {
			return
		}
		sym := &core.Symbol{
			Name: tsutil.Content(nameNode, src),
			Line: tsutil.Line(defNode),
			Col:  tsutil.Pos(nameNode).Col,
			File: path,
		}
		switch defKind {
		case "class":
			sym.Kind = core.KindClass
			if heritageNode != nil {
				parseHeritage(sym, heritageNode, src)
			}
			if len(sym.SuperTypes) > 0 {
				sym.Detail = "(" + strings.Join(sym.SuperTypes, ", ") + ")"
			}
		case "iface":
			sym.Kind = core.KindInterface
		case "enum":
			sym.Kind = core.KindEnum
		case "method", "func":
			sym.Kind = core.KindFunction // promoted to Method in assembly
			if paramsNode != nil {
				sym.Detail = tsutil.CompactWS(paramsNode.Content(src))
			}
		case "field":
			sym.Kind = core.KindVariable
			it.typ = fieldTyp
		}
		it.node, it.sym = defNode, sym
		items = append(items, &it)
	})
	if err != nil {
		return nil, err
	}
	return assemble(root, items), nil
}

// parseHeritage fills SuperTypes (extends) and Implements (implements) with
// token positions. TS nests extends_clause/implements_clause under
// class_heritage; the bundled JS grammar puts a bare identifier there.
func parseHeritage(sym *core.Symbol, h *sitter.Node, src []byte) {
	putBase := func(v *sitter.Node) {
		sym.SuperTypes = append(sym.SuperTypes, bareName(tsutil.Content(v, src)))
		sym.BasePos = append(sym.BasePos, tsutil.Pos(v))
	}
	for i := 0; i < int(h.NamedChildCount()); i++ {
		clause := h.NamedChild(i)
		switch clause.Type() {
		case "extends_clause":
			if v := clause.ChildByFieldName("value"); v != nil {
				putBase(v)
			}
		case "implements_clause":
			for j := 0; j < int(clause.NamedChildCount()); j++ {
				t := clause.NamedChild(j)
				sym.Implements = append(sym.Implements, bareName(tsutil.Content(t, src)))
				sym.ImplPos = append(sym.ImplPos, tsutil.Pos(t))
			}
		case "identifier", "member_expression": // JS: class_heritage (identifier)
			putBase(clause)
		}
	}
}

func bareName(s string) string {
	if i := strings.Index(s, "<"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// stripAnnotation turns a type_annotation node into its type text.
func stripAnnotation(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), ":"))
}

func assemble(root *sitter.Node, items []*item) []*core.Symbol {
	byKey := map[string]*item{}
	for _, it := range items {
		byKey[tsutil.NodeKey(it.node)] = it
	}
	var top []*core.Symbol
	for _, it := range items {
		var parent *item
		for p := it.node.Parent(); p != nil && p != root; p = p.Parent() {
			if pi, ok := byKey[tsutil.NodeKey(p)]; ok {
				parent = pi
				break
			}
		}
		if parent == nil {
			top = append(top, it.sym)
			continue
		}
		isType := parent.sym.Kind == core.KindClass || parent.sym.Kind == core.KindInterface
		switch {
		case isType && it.sym.Kind == core.KindFunction:
			it.sym.Kind = core.KindMethod
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case isType && it.sym.Kind == core.KindVariable:
			parent.sym.Fields = append(parent.sym.Fields, core.Field{
				Name: it.sym.Name, Type: it.typ, Line: it.pos.Line, Col: it.pos.Col,
			})
		default:
			parent.sym.Children = append(parent.sym.Children, it.sym)
		}
	}
	return top
}
