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
  left: (identifier) @name) @var
`

type lang struct{}

func (lang) Name() string         { return "python" }
func (lang) Extensions() []string { return []string{".py", ".pyi"} }

func init() { langs.Register(lang{}) }

type item struct {
	node *sitter.Node
	sym  *core.Symbol
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
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		var defNode, nameNode, auxNode *sitter.Node
		var defKind string
		for _, c := range m.Captures {
			capName := q.CaptureNameForId(c.Index)
			switch capName {
			case "class", "func", "var":
				defNode, defKind = c.Node, capName
			case "name":
				nameNode = c.Node
			case "bases", "params":
				auxNode = c.Node
			}
		}
		if defNode == nil || nameNode == nil {
			continue
		}
		sym := buildSymbol(defKind, defNode, nameNode, auxNode, src)
		sym.File = path
		items = append(items, &item{node: defNode, sym: sym})
	}

	return assemble(root, items, src, opts), nil
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
			sym.SuperTypes = baseNames(auxNode, src)
		}
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
// returns the top-level symbol list.
func assemble(root *sitter.Node, items []*item, src []byte, opts core.ParseOptions) []*core.Symbol {
	byKey := map[string]*item{}
	for _, it := range items {
		byKey[nodeKey(it.node)] = it
	}

	var top []*core.Symbol
	for _, it := range items {
		// nearest captured ancestor
		var parent *item
		for p := it.node.Parent(); p != nil && p != root.Parent(); p = p.Parent() {
			if p == root {
				break
			}
			if pi, ok := byKey[nodeKey(p)]; ok && (pi.sym.Kind == core.KindClass || pi.sym.Kind == core.KindFunction || pi.sym.Kind == core.KindMethod) {
				parent = pi
				break
			}
		}
		if parent == nil {
			top = append(top, it.sym)
			continue
		}
		switch {
		case parent.sym.Kind == core.KindClass && (it.sym.Kind == core.KindFunction):
			it.sym.Kind = core.KindMethod
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case parent.sym.Kind == core.KindClass && it.sym.Kind == core.KindClass:
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case parent.sym.Kind == core.KindClass: // var inside class body
			parent.sym.Children = append(parent.sym.Children, it.sym)
		default: // nested function/class inside a function
			parent.sym.Children = append(parent.sym.Children, it.sym)
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
	_ = src
	return top
}

func isConstName(name string) bool {
	return name == strings.ToUpper(name) && strings.ContainsAny(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
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

// baseNames extracts text-level base class names from an argument_list node.
func baseNames(args *sitter.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(args.NamedChildCount()); i++ {
		c := args.NamedChild(i)
		if c.Type() == "keyword_argument" {
			continue
		}
		out = append(out, c.Content(src))
	}
	return out
}

func compactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
