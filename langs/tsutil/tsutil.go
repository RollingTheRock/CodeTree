// Package tsutil holds the boilerplate shared by the tree-sitter based
// language parsers (python/java/cpp/jsts/rust): parser construction, query
// compilation and match iteration, and node position/content helpers.
//
// Deliberately NOT shared: the query strings and the symbol-extraction
// logic (item assembly, nesting rules) stay in each language package —
// this package removes plumbing, not semantics.
package tsutil

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"codetree/core"
)

// Parse parses src with the given tree-sitter language and returns the root
// node of the syntax tree.
func Parse(language *sitter.Language, src []byte) (*sitter.Node, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(language)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	return tree.RootNode(), nil
}

// Captures maps capture names to nodes for one query match. When several
// captures share a name in one match, the last one wins (existing queries
// never do that).
type Captures map[string]*sitter.Node

// First returns the first non-nil node among the given capture names.
func (c Captures) First(names ...string) *sitter.Node {
	for _, n := range names {
		if c[n] != nil {
			return c[n]
		}
	}
	return nil
}

// Each compiles query against language, runs it on root and calls fn for
// every match.
func Each(language *sitter.Language, query string, root *sitter.Node, fn func(Captures)) error {
	q, err := sitter.NewQuery([]byte(query), language)
	if err != nil {
		return err
	}
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)
	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}
		caps := Captures{}
		for _, c := range m.Captures {
			caps[q.CaptureNameForId(c.Index)] = c.Node
		}
		fn(caps)
	}
	return nil
}

// NodeKey uniquely identifies a syntax node by type and byte range; used to
// match nodes across capture iterations and ancestor walks.
func NodeKey(n *sitter.Node) string {
	return fmt.Sprintf("%s:%d:%d", n.Type(), n.StartByte(), n.EndByte())
}

// Pos returns the node's start position (Line 1-based, Col 0-based).
func Pos(n *sitter.Node) core.Pos {
	return core.Pos{Line: Line(n), Col: int(n.StartPoint().Column)}
}

// Line returns the node's 1-based start line.
func Line(n *sitter.Node) int {
	return int(n.StartPoint().Row) + 1
}

// Content returns the node's source text.
func Content(n *sitter.Node, src []byte) string {
	return n.Content(src)
}

// CompactWS collapses all whitespace runs to single spaces.
func CompactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
