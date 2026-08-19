// Package mermaid renders a Project as a Mermaid classDiagram, including
// inheritance edges (Animal <|-- Dog).
package mermaid

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"codetree/core"
)

// Render emits a valid Mermaid classDiagram. Classes (and Go structs /
// interfaces) become class blocks with their methods; SuperTypes become
// inheritance edges.
func Render(p *core.Project) string {
	var b strings.Builder
	b.WriteString("classDiagram\n")

	classes := collectClasses(p)
	edges := inheritanceEdges(p)

	for _, c := range classes {
		if len(c.methods) == 0 {
			fmt.Fprintf(&b, "    class %s\n", c.name)
		} else {
			fmt.Fprintf(&b, "    class %s {\n", c.name)
			for _, m := range c.methods {
				fmt.Fprintf(&b, "        %s\n", m)
			}
			b.WriteString("    }\n")
		}
		switch c.kind {
		case core.KindInterface:
			fmt.Fprintf(&b, "    <<interface>> %s\n", c.name)
		case core.KindStruct:
			fmt.Fprintf(&b, "    <<struct>> %s\n", c.name)
		}
	}
	for _, e := range edges {
		fmt.Fprintf(&b, "    %s <|-- %s\n", e.base, e.derived)
	}
	return b.String()
}

type classInfo struct {
	name    string
	kind    core.Kind
	methods []string
}

type edge struct{ base, derived string }

var mermaidNameSanitizer = regexp.MustCompile(`[^A-Za-z0-9_]`)

// sanitize makes an identifier safe for Mermaid classDiagram.
func sanitize(name string) string {
	name = mermaidNameSanitizer.ReplaceAllString(name, "_")
	if name == "" {
		return "_"
	}
	return name
}

func collectClasses(p *core.Project) []*classInfo {
	var out []*classInfo
	seen := map[string]bool{}
	var walk func(syms []*core.Symbol)
	walk = func(syms []*core.Symbol) {
		for _, s := range syms {
			switch s.Kind {
			case core.KindClass, core.KindStruct, core.KindInterface:
				name := sanitize(s.Name)
				if !seen[name] {
					seen[name] = true
					ci := &classInfo{name: name, kind: s.Kind}
					for _, ch := range s.Children {
						if ch.Kind == core.KindMethod {
							ci.methods = append(ci.methods, methodLabel(ch))
						}
					}
					out = append(out, ci)
				}
			}
			walk(s.Children)
		}
	}
	for _, f := range p.Files {
		walk(f.Symbols)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// inheritanceEdges derives "Base <|-- Derived" edges from text-level
// SuperTypes. Base names are sanitized and stripped of generic arguments.
func inheritanceEdges(p *core.Project) []edge {
	var edges []edge
	seen := map[string]bool{}
	var walk func(syms []*core.Symbol)
	walk = func(syms []*core.Symbol) {
		for _, s := range syms {
			for _, base := range s.SuperTypes {
				b := sanitize(stripGenerics(base))
				key := b + "|" + sanitize(s.Name)
				if b != sanitize(s.Name) && !seen[key] {
					seen[key] = true
					edges = append(edges, edge{base: b, derived: sanitize(s.Name)})
				}
			}
			walk(s.Children)
		}
	}
	for _, f := range p.Files {
		walk(f.Symbols)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].base != edges[j].base {
			return edges[i].base < edges[j].base
		}
		return edges[i].derived < edges[j].derived
	})
	return edges
}

func stripGenerics(s string) string {
	if i := strings.IndexAny(s, "[("); i >= 0 {
		s = s[:i]
	}
	// dotted path: keep last component (mod.Base -> Base)
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

func methodLabel(s *core.Symbol) string {
	sig := strings.TrimSpace(s.Name + s.Detail)
	// strip decorator summary and async marker for diagram cleanliness
	if i := strings.Index(sig, " @"); i >= 0 {
		sig = sig[:i]
	}
	sig = strings.ReplaceAll(sig, "async ", "")
	// parens are valid member syntax in classDiagram; only drop chars
	// Mermaid's member parser cannot take
	return strings.NewReplacer("{", "(", "}", ")", "~", "").Replace(sig)
}
