// Package cpp implements C++ support via tree-sitter queries (cgo).
package cpp

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/cpp"

	"codetree/core"
	"codetree/langs"
)

const querySource = `
(class_specifier
  name: (type_identifier) @name
  (base_class_clause)? @bases) @class

(struct_specifier
  name: (type_identifier) @name
  (base_class_clause)? @bases) @struct

(enum_specifier
  name: (type_identifier)? @name) @enum

(function_definition
  declarator: (function_declarator
    declarator: (_) @fname
    parameters: (parameter_list) @params)) @funcdef

(field_declaration
  declarator: (function_declarator
    declarator: (field_identifier) @mname
    parameters: (parameter_list) @mparams)) @methdecl

(field_declaration
  type: (_) @ftype
  declarator: (field_identifier) @fname2) @field
`

type lang struct{}

func (lang) Name() string         { return "cpp" }
func (lang) Extensions() []string { return []string{".cc", ".cpp", ".cxx", ".h", ".hpp", ".hxx"} }

func init() { langs.Register(lang{}) }

type item struct {
	node     *sitter.Node
	sym      *core.Symbol
	fieldTyp string
	scope    string // qualified out-of-class method: enclosing class name
	qname    string // full qualified name, e.g. "zoo::Animal::speak"
}

// Parse extracts the symbol tree of one C++ source/header file.
func (lang) Parse(path string, src []byte, opts core.ParseOptions) ([]*core.Symbol, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(cpp.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	root := tree.RootNode()

	q, err := sitter.NewQuery([]byte(querySource), cpp.GetLanguage())
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
		var defNode, nameNode, basesNode, paramsNode *sitter.Node
		var defKind, fname, ftyp string
		for _, c := range m.Captures {
			switch q.CaptureNameForId(c.Index) {
			case "class", "struct", "enum", "funcdef", "methdecl", "field":
				defNode, defKind = c.Node, q.CaptureNameForId(c.Index)
			case "name":
				nameNode = c.Node
			case "bases":
				basesNode = c.Node
			case "params", "mparams":
				paramsNode = c.Node
			case "fname", "mname":
				fname = c.Node.Content(src)
			case "fname2":
				fname = c.Node.Content(src)
			case "ftype":
				ftyp = compactWS(c.Node.Content(src))
			}
		}
		if defNode == nil {
			continue
		}
		it := &item{node: defNode}
		line := int(defNode.StartPoint().Row) + 1
		switch defKind {
		case "class", "struct":
			sym := &core.Symbol{Name: nameNode.Content(src), Line: line, File: path}
			if defKind == "class" {
				sym.Kind = core.KindClass
			} else {
				sym.Kind = core.KindStruct
			}
			if basesNode != nil {
				sym.SuperTypes = baseNames(basesNode, src)
				if len(sym.SuperTypes) > 0 {
					sym.Detail = "(" + strings.Join(sym.SuperTypes, ", ") + ")"
				}
			}
			it.sym = sym
		case "enum":
			name := "enum"
			if nameNode != nil {
				name = nameNode.Content(src)
			}
			it.sym = &core.Symbol{Name: name, Kind: core.KindEnum, Line: line, File: path}
		case "funcdef", "methdecl":
			// qualified name Dog::bark → out-of-class method definition
			scope, name := splitQualified(fname)
			sym := &core.Symbol{Name: name, Kind: core.KindFunction, Line: line, File: path}
			if paramsNode != nil {
				sym.Detail = compactWS(paramsNode.Content(src))
			}
			it.sym = sym
			it.scope = scope
			it.qname = fname
		case "field":
			it.sym = &core.Symbol{Name: fname, Kind: core.KindVariable, Line: line, File: path}
			it.fieldTyp = ftyp
		}
		if it.sym != nil {
			items = append(items, it)
		}
	}

	return assemble(root, items, opts), nil
}

func nodeKey(n *sitter.Node) string {
	return fmt.Sprintf("%s:%d:%d", n.Type(), n.StartByte(), n.EndByte())
}

func isContainer(k core.Kind) bool {
	switch k {
	case core.KindClass, core.KindStruct, core.KindEnum:
		return true
	}
	return false
}

// splitQualified splits "Dog::bark" → ("Dog", "bark"); plain names get "".
func splitQualified(name string) (scope, base string) {
	if i := strings.LastIndex(name, "::"); i >= 0 {
		return name[:i], name[i+2:]
	}
	return "", name
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

	// class index for out-of-class method definitions
	byClass := map[string]*item{}
	for _, it := range items {
		if isContainer(it.sym.Kind) {
			if _, ok := byClass[it.sym.Name]; !ok {
				byClass[it.sym.Name] = it
			}
		}
	}

	var top []*core.Symbol
	for _, it := range items {
		parent := nearest(it.node)
		// out-of-class definition: void Dog::bark() {}
		if it.scope != "" && parent == nil {
			owner, ok := byClass[it.scope]
			if !ok { // namespace-qualified: zoo::Dog → try bare name
				if i := strings.LastIndex(it.scope, "::"); i >= 0 {
					owner, ok = byClass[it.scope[i+2:]]
				}
			}
			if ok {
				it.sym.Kind = core.KindMethod
				owner.sym.Children = append(owner.sym.Children, it.sym)
				continue
			}
			// class declared in another file (header): keep the qualified
			// name so the definition is identifiable instead of a bare
			// duplicate-looking function name
			it.sym.Name = it.qname
		}
		if parent == nil || !isContainer(parent.sym.Kind) {
			top = append(top, it.sym)
			continue
		}
		switch it.sym.Kind {
		case core.KindFunction:
			it.sym.Kind = core.KindMethod
			parent.sym.Children = append(parent.sym.Children, it.sym)
		case core.KindVariable:
			parent.sym.Fields = append(parent.sym.Fields, core.Field{
				Name: it.sym.Name, Type: it.fieldTyp,
			})
		default: // nested class/struct/enum
			parent.sym.Children = append(parent.sym.Children, it.sym)
		}
	}
	return top
}

// baseNames extracts base classes from base_class_clause, skipping access
// specifiers and stripping template arguments (B<T> → B).
func baseNames(clause *sitter.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(clause.NamedChildCount()); i++ {
		c := clause.NamedChild(i)
		switch c.Type() {
		case "type_identifier":
			out = append(out, c.Content(src))
		case "template_type":
			if n := c.ChildByFieldName("name"); n != nil {
				out = append(out, n.Content(src))
			}
		case "qualified_identifier", "scoped_identifier":
			text := c.Content(src)
			if i := strings.LastIndex(text, "::"); i >= 0 {
				text = text[i+2:]
			}
			out = append(out, text)
		}
	}
	return out
}

func compactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
