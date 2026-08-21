// Package rust implements Rust support via tree-sitter queries (cgo):
// struct/enum/trait, impl blocks (methods attach to their type,
// `impl Trait for Type` → Implements).
package rust

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"

	"codetree/core"
	"codetree/langs"
)

const querySource = `
(struct_item
  name: (type_identifier) @name) @struct

(enum_item
  name: (type_identifier) @name) @enum

(trait_item
  name: (type_identifier) @name) @trait

(impl_item
  trait: (type_identifier)? @impl.trait
  type: (_) @impl.type) @impl

(function_item
  name: (identifier) @name
  parameters: (parameters) @params) @func

(function_signature_item
  name: (identifier) @name
  parameters: (parameters) @params) @sigfn

(field_declaration
  name: (field_identifier) @field.name
  type: (_) @field.type) @field
`

type lang struct{}

func (lang) Name() string         { return "rust" }
func (lang) Extensions() []string { return []string{".rs"} }

func init() { langs.Register(lang{}) }

type item struct {
	node      *sitter.Node
	sym       *core.Symbol
	implType  string   // impl blocks: target type name (no own symbol)
	implTrait string   // impl blocks: trait name if `impl Trait for Type`
	traitPos  core.Pos // trait token position
	typ, val  string   // fields
	pos       core.Pos
}

// Parse extracts the symbol tree of one Rust source file.
func (lang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(rust.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()

	q, err := sitter.NewQuery([]byte(querySource), rust.GetLanguage())
	if err != nil {
		return nil, err
	}
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)

	var items []*item
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		var defNode, nameNode, paramsNode, implTypeNode *sitter.Node
		var defKind, fieldName, fieldTyp string
		var it item
		for _, c := range m.Captures {
			switch q.CaptureNameForId(c.Index) {
			case "struct", "enum", "trait", "func", "sigfn", "field":
				defNode, defKind = c.Node, q.CaptureNameForId(c.Index)
			case "impl":
				defNode, defKind = c.Node, q.CaptureNameForId(c.Index)
				it.node = c.Node
			case "name":
				nameNode = c.Node
			case "params":
				paramsNode = c.Node
			case "impl.trait":
				it.implTrait = c.Node.Content(src)
				it.traitPos = core.Pos{Line: int(c.Node.StartPoint().Row) + 1, Col: int(c.Node.StartPoint().Column)}
			case "impl.type":
				implTypeNode = c.Node
			case "field.name":
				fieldName = c.Node.Content(src)
				it.pos = core.Pos{Line: int(c.Node.StartPoint().Row) + 1, Col: int(c.Node.StartPoint().Column)}
			case "field.type":
				fieldTyp = compactWS(c.Node.Content(src))
			}
		}
		if defNode == nil {
			continue
		}
		line := int(defNode.StartPoint().Row) + 1
		switch defKind {
		case "struct", "enum", "trait":
			sym := &core.Symbol{
				Name: nameNode.Content(src), Line: line, File: path,
				Col: int(nameNode.StartPoint().Column),
			}
			switch defKind {
			case "struct":
				sym.Kind = core.KindStruct
			case "enum":
				sym.Kind = core.KindEnum
			case "trait":
				sym.Kind = core.KindInterface
			}
			it.sym = sym
			it.node = defNode
		case "impl":
			if implTypeNode == nil {
				continue
			}
			it.implType = bareType(implTypeNode.Content(src))
			it.node = defNode
		case "func", "sigfn":
			it.sym = &core.Symbol{
				Name: nameNode.Content(src), Kind: core.KindFunction, Line: line, File: path,
				Detail: compactWS(paramsNode.Content(src)),
			}
			it.node = defNode
		case "field":
			it.sym = &core.Symbol{Name: fieldName, Kind: core.KindVariable, Line: line, File: path}
			it.typ = fieldTyp
			it.node = defNode
		}
		if it.sym != nil || it.implType != "" {
			items = append(items, &it)
		}
	}

	return assemble(root, items, opts), nil
}

func nodeKey(n *sitter.Node) string {
	return fmt.Sprintf("%s:%d:%d", n.Type(), n.StartByte(), n.EndByte())
}

// bareType strips generics/references from an impl target ("&'a Foo<T>" → "Foo").
func bareType(s string) string {
	if i := strings.IndexAny(s, "<&"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func assemble(root *sitter.Node, items []*item, opts core.ParseOptions) []*core.Symbol {
	byKey := map[string]*item{}
	for _, it := range items {
		byKey[nodeKey(it.node)] = it
	}
	nearest := func(n *sitter.Node) *item {
		for p := n.Parent(); p != nil && p != root; p = p.Parent() {
			if pi, ok := byKey[nodeKey(p)]; ok {
				return pi
			}
		}
		return nil
	}
	// impl target lookup: type symbols in this file
	byType := map[string]*item{}
	for _, it := range items {
		if it.sym != nil && (it.sym.Kind == core.KindStruct || it.sym.Kind == core.KindEnum) {
			if _, ok := byType[it.sym.Name]; !ok {
				byType[it.sym.Name] = it
			}
		}
	}

	var top []*core.Symbol
	for _, it := range items {
		// impl blocks: register trait on the type, no own symbol
		if it.implType != "" {
			if it.implTrait != "" {
				if owner, ok := byType[it.implType]; ok {
					owner.sym.Implements = append(owner.sym.Implements, it.implTrait)
					owner.sym.ImplPos = append(owner.sym.ImplPos, it.traitPos)
				}
			}
			continue
		}
		parent := nearest(it.node)
		switch {
		case parent == nil:
			top = append(top, it.sym)
		case parent.implType != "":
			// inside an impl block → method of the target type
			if owner, ok := byType[parent.implType]; ok && it.sym.Kind == core.KindFunction {
				it.sym.Kind = core.KindMethod
				owner.sym.Children = append(owner.sym.Children, it.sym)
			} else {
				top = append(top, it.sym)
			}
		case parent.sym.Kind == core.KindStruct && it.sym.Kind == core.KindVariable:
			parent.sym.Fields = append(parent.sym.Fields, core.Field{
				Name: it.sym.Name, Type: it.typ, Line: it.pos.Line, Col: it.pos.Col,
			})
		case parent.sym.Kind == core.KindInterface && it.sym.Kind == core.KindFunction:
			it.sym.Kind = core.KindMethod
			parent.sym.Children = append(parent.sym.Children, it.sym)
		default:
			parent.sym.Children = append(parent.sym.Children, it.sym)
		}
	}
	return top
}

func compactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
