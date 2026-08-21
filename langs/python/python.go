// Package python implements Python support via tree-sitter queries (cgo).
package python

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"

	"codetree/core"
	"codetree/langs"
)

const querySource = `
(class_definition
  name: (identifier) @name
  superclasses: (argument_list)? @bases) @class

(function_definition
  name: (identifier) @name
  parameters: (parameters) @params) @func

(assignment
  left: (identifier) @name
  type: (type)? @var.type
  right: (_)? @var.value) @var

(assignment
  left: (attribute
    object: (identifier) @attr.obj
    attribute: (identifier) @attr.name)
  type: (type)? @attr.type
  right: (_)? @attr.value) @attr
`

type lang struct{}

func (lang) Name() string         { return "python" }
func (lang) Extensions() []string { return []string{".py", ".pyi"} }

func init() { langs.Register(lang{}) }

type item struct {
	node *sitter.Node
	sym  *core.Symbol
	typ  string // annotation text (var items only)
	val  string // value-inferred type (var items only)
	pos  core.Pos // name token position (var items only)
}

// attrItem is a self.x assignment captured for instance-field extraction.
type attrItem struct {
	node *sitter.Node // the assignment node
	obj  string       // receiver name, e.g. "self"
	name string
	typ  string
	val  string
	pos  core.Pos // attribute name token position
}

// Parse extracts the symbol tree of one Python source file.
func (lang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()

	q, err := sitter.NewQuery([]byte(querySource), python.GetLanguage())
	if err != nil {
		return nil, err
	}
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)

	var items []*item
	var attrs []*attrItem
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		var defNode, nameNode, auxNode, typeNode, valNode *sitter.Node
		var defKind string
		var attr attrItem
		for _, c := range m.Captures {
			capName := q.CaptureNameForId(c.Index)
			switch capName {
			case "class", "func", "var":
				defNode, defKind = c.Node, capName
			case "attr":
				defNode, defKind = c.Node, capName
				attr.node = c.Node
			case "name":
				nameNode = c.Node
			case "bases", "params":
				auxNode = c.Node
			case "var.type", "attr.type":
				typeNode = c.Node
			case "var.value", "attr.value":
				valNode = c.Node
			case "attr.obj":
				attr.obj = c.Node.Content(src)
			case "attr.name":
				attr.name = c.Node.Content(src)
				attr.pos = core.Pos{Line: int(c.Node.StartPoint().Row) + 1, Col: int(c.Node.StartPoint().Column)}
			}
		}
		if defNode == nil {
			continue
		}
		typ, val := "", ""
		if typeNode != nil {
			typ = compactWS(typeNode.Content(src))
		}
		if valNode != nil {
			val = inferType(valNode, src)
		}
		if defKind == "attr" {
			if attr.obj == "self" && attr.name != "" {
				attr.typ, attr.val = typ, val
				attrs = append(attrs, &attr)
			}
			continue
		}
		if nameNode == nil {
			continue
		}
		sym := buildSymbol(defKind, defNode, nameNode, auxNode, src)
		sym.File = path
		it := &item{node: defNode, sym: sym, typ: typ, val: val}
		if defKind == "var" {
			it.pos = core.Pos{Line: int(nameNode.StartPoint().Row) + 1, Col: int(nameNode.StartPoint().Column)}
		}
		items = append(items, it)
	}

	return assemble(root, items, attrs, opts), nil
}

// nodeKey uniquely identifies a syntax node by its byte range.
func nodeKey(n *sitter.Node) string {
	return fmt.Sprintf("%s:%d:%d", n.Type(), n.StartByte(), n.EndByte())
}

func buildSymbol(kind string, defNode, nameNode, auxNode *sitter.Node, src []byte) *core.Symbol {
	sym := &core.Symbol{
		Name: nameNode.Content(src),
		Line: int(defNode.StartPoint().Row) + 1,
	}
	switch kind {
	case "class":
		sym.Kind = core.KindClass
		if auxNode != nil {
			sym.Detail = compactWS(auxNode.Content(src)) // e.g. "(Animal, Mixin)"
			sym.SuperTypes, sym.BasePos = baseNames(auxNode, src)
		}
		sym.Kind = classifyClass(sym.SuperTypes)
		sym.Doc = docstring(defNode, src)
	case "func":
		sym.Kind = core.KindFunction // promoted to Method during assembly
		var parts []string
		if isAsync(defNode) {
			parts = append(parts, "async")
		}
		if auxNode != nil {
			parts = append(parts, compactWS(auxNode.Content(src))) // e.g. "(self, x)"
		}
		if decs := decorators(defNode, src); len(decs) > 0 {
			parts = append(parts, strings.Join(decs, " "))
		}
		sym.Detail = strings.Join(parts, " ")
		sym.Doc = docstring(defNode, src)
	case "var":
		if isConstName(sym.Name) {
			sym.Kind = core.KindConstant
		} else {
			sym.Kind = core.KindVariable
		}
	}
	return sym
}

// assemble nests items under their nearest enclosing class/function and
// returns the top-level symbol list. Class-body assignments become Fields;
// self.x assignments inside __init__ become instance Fields.
func assemble(root *sitter.Node, items []*item, attrs []*attrItem, opts core.ParseOptions) []*core.Symbol {
	byKey := map[string]*item{}
	for _, it := range items {
		byKey[nodeKey(it.node)] = it
	}
	nearest := func(n *sitter.Node) *item {
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p == root {
				return nil
			}
			if pi, ok := byKey[nodeKey(p)]; ok {
				return pi
			}
		}
		return nil
	}

	var top []*core.Symbol
	for _, it := range items {
		parent := nearest(it.node)
		if parent == nil {
			top = append(top, it.sym)
			continue
		}
		switch {
		case parent.sym.Kind == core.KindClass && it.sym.Kind == core.KindFunction:
			it.sym.Kind = core.KindMethod
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case parent.sym.Kind == core.KindClass && it.sym.Kind == core.KindClass:
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case parent.sym.Kind == core.KindClass &&
			(it.sym.Kind == core.KindVariable || it.sym.Kind == core.KindConstant):
			// direct class-body assignment → class attribute
			parent.sym.Fields = append(parent.sym.Fields, core.Field{
				Name: it.sym.Name, Type: firstNonEmpty(it.typ, it.val), ClassVar: true,
				Line: it.pos.Line, Col: it.pos.Col,
			})
		default: // nested function/class inside a function
			parent.sym.Children = append(parent.sym.Children, it.sym)
		}
	}

	// instance attributes: self.x inside __init__ → Field on the owning class
	for _, a := range attrs {
		fn := nearest(a.node)
		if fn == nil || fn.sym.Name != "__init__" {
			continue
		}
		owner := nearest(fn.node)
		if owner == nil || owner.sym.Kind != core.KindClass {
			continue
		}
		dup := false
		for _, f := range owner.sym.Fields {
			if f.Name == a.name {
				dup = true
				break
			}
		}
		if !dup {
			owner.sym.Fields = append(owner.sym.Fields, core.Field{
				Name: a.name, Type: firstNonEmpty(a.typ, a.val),
				Line: a.pos.Line, Col: a.pos.Col,
			})
		}
	}

	if !opts.IncludeVars {
		var filterVars func(syms []*core.Symbol) []*core.Symbol
		filterVars = func(syms []*core.Symbol) []*core.Symbol {
			out := syms[:0]
			for _, s := range syms {
				if s.Kind == core.KindVariable || s.Kind == core.KindConstant {
					continue
				}
				s.Children = filterVars(s.Children)
				out = append(out, s)
			}
			return out
		}
		top = filterVars(top)
	}
	return top
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// inferType guesses a Python type from a value expression node.
func inferType(n *sitter.Node, src []byte) string {
	switch n.Type() {
	case "integer":
		return "int"
	case "float":
		return "float"
	case "string", "concatenated_string":
		return "str"
	case "true", "false":
		return "bool"
	case "list", "list_comprehension":
		return "list"
	case "dictionary", "dictionary_comprehension", "set":
		return "dict"
	case "tuple":
		return "tuple"
	case "call": // Foo() → Foo
		if f := n.ChildByFieldName("function"); f != nil {
			return compactWS(f.Content(src))
		}
	case "attribute": // self.x = module.CONST
		return compactWS(n.Content(src))
	}
	return ""
}

func isConstName(name string) bool {
	return name == strings.ToUpper(name) && strings.ContainsAny(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}

// classifyClass refines the class kind from its text-level bases:
// ABC/Protocol → interface, Enum family → enum, else plain class.
func classifyClass(bases []string) core.Kind {
	for _, b := range bases {
		if i := strings.LastIndex(b, "."); i >= 0 {
			b = b[i+1:]
		}
		switch b {
		case "ABC", "ABCMeta", "Protocol":
			return core.KindInterface
		case "Enum", "IntEnum", "StrEnum", "Flag", "IntFlag":
			return core.KindEnum
		}
	}
	return core.KindClass
}

func isAsync(fn *sitter.Node) bool {
	for i := 0; i < int(fn.ChildCount()); i++ {
		if fn.Child(i).Type() == "async" {
			return true
		}
	}
	return false
}

// decorators returns "@name" summaries when the def is wrapped in a
// decorated_definition node.
func decorators(defNode *sitter.Node, src []byte) []string {
	p := defNode.Parent()
	if p == nil || p.Type() != "decorated_definition" {
		return nil
	}
	var out []string
	for i := 0; i < int(p.ChildCount()); i++ {
		c := p.Child(i)
		if c.Type() != "decorator" {
			continue
		}
		text := strings.TrimSpace(c.Content(src)) // "@staticmethod" or "@app.route('/x')"
		if i := strings.IndexAny(text, "("); i >= 0 {
			text = text[:i] + "(…)"
		}
		out = append(out, text)
	}
	return out
}

// docstring extracts the first paragraph of the body's docstring.
func docstring(defNode *sitter.Node, src []byte) string {
	body := defNode.ChildByFieldName("body")
	if body == nil || body.NamedChildCount() == 0 {
		return ""
	}
	first := body.NamedChild(0)
	if first.Type() != "expression_statement" || first.NamedChildCount() == 0 {
		return ""
	}
	s := first.NamedChild(0)
	if s.Type() != "string" && s.Type() != "concatenated_string" {
		return ""
	}
	text := s.Content(src)
	text = strings.Trim(text, "\"'`) ")
	text = strings.TrimSpace(text)
	// first paragraph only
	if i := strings.Index(text, "\n\n"); i >= 0 {
		text = text[:i]
	}
	return strings.Join(strings.Fields(text), " ")
}

// baseNames extracts text-level base class names from an argument_list node,
// with the source position of each base token (for LSP definition requests).
func baseNames(args *sitter.Node, src []byte) ([]string, []core.Pos) {
	var out []string
	var pos []core.Pos
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c.Type() == "keyword_argument" {
			continue
		}
		out = append(out, c.Content(src))
		pos = append(pos, core.Pos{Line: int(c.StartPoint().Row) + 1, Col: int(c.StartPoint().Column)})
	}
	return out, pos
}

func compactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
