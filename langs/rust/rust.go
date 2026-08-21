// Package rust implements Rust support via tree-sitter queries (cgo):
// struct/enum/trait, impl blocks (methods attach to their type,
// `impl Trait for Type` → Implements).
package rust

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/rust"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/langs"
	"github.com/RollingTheRock/CodeTree/langs/tsutil"
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
	root, err := tsutil.Parse(rust.GetLanguage(), src)
	if err != nil {
		return nil, err
	}

	var items []*item
	err = tsutil.Each(rust.GetLanguage(), querySource, root, func(c tsutil.Captures) {
		defKind := ""
		for _, k := range []string{"struct", "enum", "trait", "func", "sigfn", "field", "impl"} {
			if c[k] != nil {
				defKind = k
				break
			}
		}
		defNode := c[defKind]
		var fieldName, fieldTyp string
		var it item
		nameNode := c["name"]
		paramsNode := c["params"]
		var implTypeNode *sitter.Node
		if im := c["impl"]; im != nil {
			it.node = im
		}
		if tn := c["impl.trait"]; tn != nil {
			it.implTrait = tsutil.Content(tn, src)
			it.traitPos = tsutil.Pos(tn)
		}
		if tn := c["impl.type"]; tn != nil {
			implTypeNode = tn
		}
		if fn := c["field.name"]; fn != nil {
			fieldName = tsutil.Content(fn, src)
			it.pos = tsutil.Pos(fn)
		}
		if ft := c["field.type"]; ft != nil {
			fieldTyp = tsutil.CompactWS(tsutil.Content(ft, src))
		}
		if defNode == nil {
			return
		}
		line := tsutil.Line(defNode)
		switch defKind {
		case "struct", "enum", "trait":
			sym := &core.Symbol{
				Name: tsutil.Content(nameNode, src), Line: line, File: path,
				Col: tsutil.Pos(nameNode).Col,
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
				return
			}
			it.implType = bareType(tsutil.Content(implTypeNode, src))
			it.node = defNode
		case "func", "sigfn":
			it.sym = &core.Symbol{
				Name: tsutil.Content(nameNode, src), Kind: core.KindFunction, Line: line, File: path,
				Detail: tsutil.CompactWS(paramsNode.Content(src)),
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
	})
	if err != nil {
		return nil, err
	}

	return assemble(root, items, opts), nil
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
		byKey[tsutil.NodeKey(it.node)] = it
	}
	nearest := func(n *sitter.Node) *item {
		for p := n.Parent(); p != nil && p != root; p = p.Parent() {
			if pi, ok := byKey[tsutil.NodeKey(p)]; ok {
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
