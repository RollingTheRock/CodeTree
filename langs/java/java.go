// Package java implements Java support via tree-sitter queries (cgo).
package java

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"

	"codetree/core"
	"codetree/langs"
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
	parser := sitter.NewParser()
	parser.SetLanguage(java.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()

	q, err := sitter.NewQuery([]byte(querySource), java.GetLanguage())
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
		var defNode, nameNode, superNode, ifacesNode, paramsNode, ftypeNode, fnameNode *sitter.Node
		var defKind string
		for _, c := range m.Captures {
			switch q.CaptureNameForId(c.Index) {
			case "class", "iface", "enum", "record", "method", "ctor", "field":
				defNode, defKind = c.Node, q.CaptureNameForId(c.Index)
			case "name":
				nameNode = c.Node
			case "super":
				superNode = c.Node
			case "ifaces", "extends":
				ifacesNode = c.Node
			case "params":
				paramsNode = c.Node
			case "field.type":
				ftypeNode = c.Node
			case "field.name":
				fnameNode = c.Node
			}
		}
		if defNode == nil {
			continue
		}
		if defKind == "field" {
			if fnameNode == nil {
				continue
			}
			sym := &core.Symbol{
				Name: fnameNode.Content(src),
				Kind: core.KindVariable,
				File: path,
				Line: int(defNode.StartPoint().Row) + 1,
			}
			it := &item{node: defNode, sym: sym}
			if ftypeNode != nil {
				it.fieldTyp = compactWS(ftypeNode.Content(src))
			}
			items = append(items, it)
			continue
		}
		if nameNode == nil {
			continue
		}
		sym := &core.Symbol{
			Name: nameNode.Content(src),
			Line: int(defNode.StartPoint().Row) + 1,
			File: path,
		}
		switch defKind {
		case "class":
			sym.Kind = core.KindClass
			if superNode != nil {
				sym.SuperTypes = typeNames(superNode, src)
				sym.Detail = "(" + strings.Join(sym.SuperTypes, ", ") + ")"
			}
			if ifacesNode != nil {
				sym.Implements = typeNames(ifacesNode, src)
			}
		case "iface":
			sym.Kind = core.KindInterface
			if ifacesNode != nil { // interface extends interface → inheritance
				sym.SuperTypes = typeNames(ifacesNode, src)
				sym.Detail = "(" + strings.Join(sym.SuperTypes, ", ") + ")"
			}
		case "enum":
			sym.Kind = core.KindEnum
		case "record":
			sym.Kind = core.KindClass
			sym.Detail = "record " + compactWS(paramsNode.Content(src))
		case "method", "ctor":
			sym.Kind = core.KindFunction // promoted to Method in assembly
			if paramsNode != nil {
				sym.Detail = compactWS(paramsNode.Content(src))
			}
			if anns := annotations(defNode, src); anns != "" {
				sym.Detail = strings.TrimSpace(sym.Detail + " " + anns)
			}
			if defKind == "ctor" {
				sym.Detail = "new " + sym.Detail
			}
		}
		items = append(items, &item{node: defNode, sym: sym})
	}

	return assemble(root, items, opts), nil
}

func nodeKey(n *sitter.Node) string {
	return fmt.Sprintf("%s:%d:%d", n.Type(), n.StartByte(), n.EndByte())
}

func isContainer(k core.Kind) bool {
	switch k {
	case core.KindClass, core.KindInterface, core.KindEnum:
		return true
	}
	return false
}

// assemble nests members under their nearest enclosing type declaration.
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

// typeNames extracts type names from superclass / super_interfaces /
// extends_interfaces nodes, stripping generics (Comparable<Dog> → Comparable).
func typeNames(n *sitter.Node, src []byte) []string {
	var out []string
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		switch n.Type() {
		case "type_identifier":
			out = append(out, n.Content(src))
			return
		case "generic_type", "scoped_type_identifier":
			// take the outermost name only
			if name := firstTypeIdentifier(n, src); name != "" {
				out = append(out, name)
			}
			return
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(n)
	return out
}

func firstTypeIdentifier(n *sitter.Node, src []byte) string {
	if n.Type() == "type_identifier" {
		return n.Content(src)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if s := firstTypeIdentifier(n.NamedChild(i), src); s != "" {
			return s
		}
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
					text := name.Content(src)
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

func compactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
