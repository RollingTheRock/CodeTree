// Package java implements Java support via tree-sitter queries (cgo).
package java

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/langs"
	"github.com/RollingTheRock/CodeTree/langs/tsutil"
)

const querySource = `
(class_declaration
  name: (identifier) @name
  superclass: (superclass)? @super
  interfaces: (super_interfaces)? @ifaces) @class

(interface_declaration
  name: (identifier) @name
  (extends_interfaces)? @extends) @iface

(enum_declaration
  name: (identifier) @name) @enum

(record_declaration
  name: (identifier) @name
  parameters: (formal_parameters) @params) @record

(method_declaration
  name: (identifier) @name
  parameters: (formal_parameters) @params) @method

(constructor_declaration
  name: (identifier) @name
  parameters: (formal_parameters) @params) @ctor

(field_declaration
  type: (_) @field.type
  declarator: (variable_declarator name: (identifier) @field.name)) @field
`

type lang struct{}

func (lang) Name() string         { return "java" }
func (lang) Extensions() []string { return []string{".java"} }

func init() { langs.Register(lang{}) }

type item struct {
	node     *sitter.Node
	sym      *core.Symbol
	fieldTyp string // field items only
}

// Parse extracts the symbol tree of one Java source file.
func (lang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	root, err := tsutil.Parse(java.GetLanguage(), src)
	if err != nil {
		return nil, err
	}

	var items []*item
	err = tsutil.Each(java.GetLanguage(), querySource, root, func(c tsutil.Captures) {
		defKind := ""
		for _, k := range []string{"class", "iface", "enum", "record", "method", "ctor", "field"} {
			if c[k] != nil {
				defKind = k
				break
			}
		}
		defNode := c[defKind]
		if defNode == nil {
			return
		}
		nameNode := c["name"]
		superNode := c["super"]
		ifacesNode := c.First("ifaces", "extends")
		paramsNode := c["params"]
		ftypeNode := c["field.type"]
		fnameNode := c["field.name"]
		if defKind == "field" {
			if fnameNode == nil {
				return
			}
			sym := &core.Symbol{
				Name: tsutil.Content(fnameNode, src),
				Kind: core.KindVariable,
				File: path,
				Line: tsutil.Line(defNode),
			}
			it := &item{node: defNode, sym: sym}
			if ftypeNode != nil {
				it.fieldTyp = tsutil.CompactWS(tsutil.Content(ftypeNode, src))
			}
			items = append(items, it)
			return
		}
		if nameNode == nil {
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
			if superNode != nil {
				sym.SuperTypes, sym.BasePos = typeNamesPos(superNode, src)
				sym.Detail = "(" + strings.Join(sym.SuperTypes, ", ") + ")"
			}
			if ifacesNode != nil {
				sym.Implements, sym.ImplPos = typeNamesPos(ifacesNode, src)
			}
		case "iface":
			sym.Kind = core.KindInterface
			if ifacesNode != nil { // interface extends interface → inheritance
				sym.SuperTypes, sym.BasePos = typeNamesPos(ifacesNode, src)
				sym.Detail = "(" + strings.Join(sym.SuperTypes, ", ") + ")"
			}
		case "enum":
			sym.Kind = core.KindEnum
		case "record":
			sym.Kind = core.KindClass
			sym.Detail = "record " + tsutil.CompactWS(paramsNode.Content(src))
		case "method", "ctor":
			sym.Kind = core.KindFunction // promoted to Method in assembly
			if paramsNode != nil {
				sym.Detail = tsutil.CompactWS(paramsNode.Content(src))
			}
			if anns := annotations(defNode, src); anns != "" {
				sym.Detail = strings.TrimSpace(sym.Detail + " " + anns)
			}
			if defKind == "ctor" {
				sym.Detail = "new " + sym.Detail
			}
		}
		items = append(items, &item{node: defNode, sym: sym})
	})
	if err != nil {
		return nil, err
	}

	return assemble(root, items, opts), nil
}

// isContainer reports whether members nest under this kind. Includes
// KindStruct for uniformity (Java never produces it, so no behavior change).
func isContainer(k core.Kind) bool {
	return k.ClassLike()
}

// assemble nests members under their nearest enclosing type declaration.
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

	var top []*core.Symbol
	for _, it := range items {
		parent := nearest(it.node)
		if parent == nil || !isContainer(parent.sym.Kind) {
			top = append(top, it.sym)
			continue
		}
		switch it.sym.Kind {
		case core.KindFunction:
			it.sym.Kind = core.KindMethod
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case core.KindVariable:
			// field directly in a type body → Fields
			parent.sym.Fields = append(parent.sym.Fields, core.Field{
				Name: it.sym.Name, Type: it.fieldTyp,
			})
		default: // nested class/interface/enum/record
			parent.sym.Children = append(parent.sym.Children, it.sym)
		}
	}
	return top
}

// typeNamesPos extracts type names from superclass / super_interfaces /
// extends_interfaces nodes, stripping generics (Comparable<Dog> →
// Comparable), with the source position of each type token.
func typeNamesPos(n *sitter.Node, src []byte) ([]string, []core.Pos) {
	var out []string
	var pos []core.Pos
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Type() {
		case "type_identifier":
			out = append(out, tsutil.Content(n, src))
			pos = append(pos, nodePos(n))
			return
		case "generic_type", "scoped_type_identifier":
			// take the outermost name only
			if ti := firstTypeIdentifierNode(n); ti != nil {
				out = append(out, tsutil.Content(ti, src))
				pos = append(pos, nodePos(ti))
			}
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(n)
	return out, pos
}

func nodePos(n *sitter.Node) core.Pos { return tsutil.Pos(n) }

func firstTypeIdentifierNode(n *sitter.Node) *sitter.Node {
	if n.Type() == "type_identifier" {
		return n
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if s := firstTypeIdentifierNode(n.NamedChild(i)); s != nil {
			return s
		}
	}
	return nil
}

func firstTypeIdentifier(n *sitter.Node, src []byte) string {
	if ti := firstTypeIdentifierNode(n); ti != nil {
		return tsutil.Content(ti, src)
	}
	return ""
}

// annotations summarizes marker annotations (@Override) on a declaration.
func annotations(defNode *sitter.Node, src []byte) string {
	for i := 0; i < int(defNode.ChildCount()); i++ {
		c := defNode.Child(i)
		if c.Type() != "modifiers" {
			continue
		}
		var out []string
		for j := 0; j < int(c.NamedChildCount()); j++ {
			a := c.NamedChild(j)
			if a.Type() == "marker_annotation" || a.Type() == "annotation" {
				if name := a.ChildByFieldName("name"); name != nil {
					text := tsutil.Content(name, src)
					if i := strings.LastIndex(text, "."); i >= 0 {
						text = text[i+1:]
					}
					out = append(out, "@"+text)
				}
			}
		}
		return strings.Join(out, " ")
	}
	return ""
}
