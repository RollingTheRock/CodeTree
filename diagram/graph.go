package diagram

import (
	"strings"

	"codetree/core"
)

// gnode is a node in the inheritance forest.
type gnode struct {
	name      string
	sym       *core.Symbol // nil for external base boxes
	external  bool
	parent    *gnode // primary parent: first resolved base
	children  []*gnode
	baseNodes []*gnode // all resolved bases (primary + secondary)
	secBases  []string // resolved non-primary base names (annotated on card)
	extBases  []string // unresolved external base names (annotation when hidden)

	implNodes    []*gnode // resolved implements targets (dashed edges)
	implementors []*gnode // reverse links: classes implementing this interface
	implExternal []string // unresolved implements names (~Iface annotation)

	assocTargets []*gnode // classes this node's fields reference (composition)
	assocFrom    []*gnode // reverse links: classes whose fields reference this node

	context bool // scope mode: pulled-in relative, not a protagonist
}

// buildGraph extracts the inheritance forest from all class-like symbols.
// Duplicate class names: first occurrence (scan order) wins.
func buildGraph(p *core.Project, opts Options) *graph {
	g := &graph{byName: map[string]*gnode{}}

	var syms []*core.Symbol
	for _, s := range p.AllSymbols() {
		switch s.Kind {
		case core.KindClass, core.KindStruct, core.KindInterface, core.KindEnum:
			syms = append(syms, s)
		}
	}
	for _, s := range syms {
		if _, dup := g.byName[s.Name]; !dup {
			n := &gnode{name: s.Name, sym: s}
			g.byName[s.Name] = n
			g.nodes = append(g.nodes, n)
		}
	}

	// resolve bases
	externals := map[string]*gnode{}
	var extOrder []string // first-seen order; map iteration is randomized
	newExternal := func(name string) *gnode {
		en, ok := externals[name]
		if !ok {
			en = &gnode{name: name, external: true}
			externals[name] = en
			extOrder = append(extOrder, name)
		}
		return en
	}
	for _, n := range g.nodes {
		for _, base := range n.sym.SuperTypes {
			name := baseKey(base)
			if name == "" || name == n.name {
				continue
			}
			if bn, ok := g.byName[name]; ok {
				n.baseNodes = append(n.baseNodes, bn)
			} else {
				n.extBases = append(n.extBases, name)
			}
		}
		// primary parent = first resolved base
		if len(n.baseNodes) > 0 {
			n.parent = n.baseNodes[0]
			for _, b := range n.baseNodes[1:] {
				n.secBases = append(n.secBases, b.name)
			}
		} else if opts.External && len(n.extBases) > 0 {
			// unresolved first base → gray external box, shared by name
			name := n.extBases[0]
			en := newExternal(name)
			n.parent = en
			n.extBases = n.extBases[1:]
		}
		// implements: dashed edges; unresolved first external interface
		// becomes a gray box like external bases
		for _, iname := range n.sym.Implements {
			name := baseKey(iname)
			if name == "" || name == n.name {
				continue
			}
			if in, ok := g.byName[name]; ok {
				n.implNodes = append(n.implNodes, in)
				in.implementors = append(in.implementors, n)
			} else {
				n.implExternal = append(n.implExternal, name)
			}
		}
		if opts.External && len(n.implExternal) > 0 {
			name := n.implExternal[0]
			en := newExternal(name)
			n.implNodes = append(n.implNodes, en)
			en.implementors = append(en.implementors, n)
			n.implExternal = n.implExternal[1:]
		}
		// composition: field types referencing project classes (deduped,
		// self-references skipped)
		if opts.Assoc && n.sym != nil {
			seen := map[*gnode]bool{}
			for _, f := range n.sym.Fields {
				for _, ref := range ExtractTypeRefs(f.Type) {
					tn, ok := g.byName[ref]
					if !ok || tn == n || seen[tn] {
						continue
					}
					seen[tn] = true
					n.assocTargets = append(n.assocTargets, tn)
					tn.assocFrom = append(tn.assocFrom, n)
				}
			}
		}
	}
	// guard against cycles (A(B), B(A)): drop back-edges
	for _, n := range g.nodes {
		if n.parent != nil && reaches(n.parent, n) {
			n.parent = nil
		}
	}

	for _, n := range g.nodes {
		if n.parent != nil {
			n.parent.children = append(n.parent.children, n)
		}
	}
	for _, name := range extOrder {
		g.externals = append(g.externals, externals[name])
	}
	g.computeForest()
	return g
}

type graph struct {
	nodes     []*gnode // project classes, first-seen order
	byName    map[string]*gnode
	roots     []*gnode
	orphans   []*gnode
	externals []*gnode
}

// computeForest splits roots into trees (have children) and orphans (no
// relatives at all). An interface with implementors but no extends-children
// stays a root (single-node tree) so dashed implements edges have a placed
// endpoint; likewise classes with any composition relation are promoted out
// of the orphan partition.
func (g *graph) computeForest() {
	g.roots = g.roots[:0]
	g.orphans = g.orphans[:0]
	seen := map[*gnode]bool{}
	var addRoots func(n *gnode)
	addRoots = func(n *gnode) {
		if n.parent != nil || seen[n] {
			return
		}
		seen[n] = true
		related := len(n.children) > 0 || len(n.implementors) > 0 ||
			len(n.assocTargets) > 0 || len(n.assocFrom) > 0
		if !related {
			if !n.external {
				g.orphans = append(g.orphans, n)
			}
		} else {
			g.roots = append(g.roots, n)
		}
	}
	for _, n := range g.nodes {
		addRoots(n)
	}
	for _, n := range g.externals {
		addRoots(n)
	}
}

// reaches reports whether `from` can reach `target` walking parent links.
func reaches(from, target *gnode) bool {
	for n := from; n != nil; n = n.parent {
		if n == target {
			return true
		}
	}
	return false
}

// baseKey normalizes a text-level base name: strips generics/template args
// and dotted/namespaced paths ("module.Base[T]", "std::vector<T>" → "Base",
// "vector").
func baseKey(s string) string {
	if i := strings.IndexAny(s, "[(<"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	return strings.TrimSpace(s)
}

// filterNeighborhood keeps only the focus class, its ancestor chain (Up
// levels, <0 = all), its descendants (Down levels), and optionally its
// siblings. Returns the filtered graph and the resolved focus name.
func filterNeighborhood(g *graph, opts Options) (*graph, string, error) {
	focus := g.byName[opts.Focus]
	if focus == nil {
		// try case-insensitive
		for name, n := range g.byName {
			if strings.EqualFold(name, opts.Focus) {
				focus = n
				break
			}
		}
	}
	if focus == nil {
		var avail []string
		for _, n := range g.nodes {
			avail = append(avail, n.name)
		}
		return nil, "", &ErrFocusNotFound{Focus: opts.Focus, Available: avail}
	}

	keep := map[*gnode]bool{focus: true}

	// ancestors via all resolved bases + implemented interfaces (both are
	// "upward" relations in UML); composing classes (assocFrom) too
	up := opts.Up
	var walkUp func(n *gnode, d int)
	walkUp = func(n *gnode, d int) {
		if up >= 0 && d >= up {
			return
		}
		for _, b := range n.baseNodes {
			if !keep[b] {
				keep[b] = true
				walkUp(b, d+1)
			}
		}
		for _, b := range n.implNodes {
			if !b.external && !keep[b] {
				keep[b] = true
				walkUp(b, d+1)
			}
		}
		for _, b := range n.assocFrom {
			if !keep[b] {
				keep[b] = true
				walkUp(b, d+1)
			}
		}
	}
	walkUp(focus, 0)

	// descendants via primary-parent children + composed classes
	var walkDown func(n *gnode, d int)
	walkDown = func(n *gnode, d int) {
		if d >= opts.Down {
			return
		}
		for _, c := range n.children {
			if !keep[c] {
				keep[c] = true
				walkDown(c, d+1)
			}
		}
		for _, t := range n.assocTargets {
			if !keep[t] {
				keep[t] = true
				walkDown(t, d+1)
			}
		}
	}
	walkDown(focus, 0)

	// siblings: same primary parent
	if opts.Siblings && focus.parent != nil {
		for _, sib := range focus.parent.children {
			keep[sib] = true
		}
	}

	fg := &graph{byName: map[string]*gnode{}}
	for _, n := range g.nodes {
		if keep[n] {
			fg.nodes = append(fg.nodes, n)
			fg.byName[n.name] = n
		}
	}
	// re-link: drop parent/child edges to nodes outside the keep set
	for _, n := range fg.nodes {
		if n.parent != nil && !keep[n.parent] && !n.parent.external {
			n.parent = nil
		}
		var kept []*gnode
		for _, c := range n.children {
			if keep[c] {
				kept = append(kept, c)
			}
		}
		n.children = kept
	}
	for _, en := range g.externals {
		hasChild := false
		var kept []*gnode
		for _, c := range en.children {
			if keep[c] {
				kept = append(kept, c)
				hasChild = true
			}
		}
		en.children = kept
		for _, im := range en.implementors {
			if keep[im] {
				hasChild = true
			}
		}
		if hasChild {
			fg.externals = append(fg.externals, en)
		}
	}
	fg.computeForest()
	return fg, focus.name, nil
}

// filterScope implements file scope: protagonists are classes defined in the
// given files; context nodes are their full ancestor chains (extends and
// implements, any file) plus direct subclasses one level down. Context nodes
// are flagged for dimmed rendering. Everything else is dropped.
func filterScope(g *graph, files []string) *graph {
	inScope := map[string]bool{}
	for _, f := range files {
		inScope[f] = true
	}

	keep := map[*gnode]bool{}
	var mains []*gnode
	for _, n := range g.nodes {
		if n.sym != nil && inScope[n.sym.File] {
			keep[n] = true
			mains = append(mains, n)
		}
	}

	for _, n := range mains {
		// full ancestor chain: extends + implements
		var walkUp func(x *gnode)
		walkUp = func(x *gnode) {
			for _, b := range x.baseNodes {
				if !keep[b] {
					keep[b] = true
					b.context = true
					walkUp(b)
				}
			}
			for _, b := range x.implNodes {
				if !b.external && !keep[b] {
					keep[b] = true
					b.context = true
					walkUp(b)
				}
			}
		}
		walkUp(n)
		// direct subclasses (one level down)
		for _, c := range n.children {
			if !keep[c] {
				keep[c] = true
				c.context = true
			}
		}
		// composition neighbors, one level, both directions
		for _, t := range n.assocTargets {
			if !keep[t] {
				keep[t] = true
				t.context = true
			}
		}
		for _, o := range n.assocFrom {
			if !keep[o] {
				keep[o] = true
				o.context = true
			}
		}
	}

	fg := &graph{byName: map[string]*gnode{}}
	for _, n := range g.nodes {
		if keep[n] {
			fg.nodes = append(fg.nodes, n)
			fg.byName[n.name] = n
		}
	}
	for _, n := range fg.nodes {
		if n.parent != nil && !keep[n.parent] && !n.parent.external {
			n.parent = nil
		}
		var kept []*gnode
		for _, c := range n.children {
			if keep[c] {
				kept = append(kept, c)
			}
		}
		n.children = kept
	}
	for _, en := range g.externals {
		hasRel := false
		var kept []*gnode
		for _, c := range en.children {
			if keep[c] {
				kept = append(kept, c)
				hasRel = true
			}
		}
		en.children = kept
		for _, im := range en.implementors {
			if keep[im] {
				hasRel = true
			}
		}
		if hasRel {
			en.context = true // externals are always context
			fg.externals = append(fg.externals, en)
		}
	}
	fg.computeForest()
	// scope mode: no unrelated partition — a protagonist without relatives
	// is still the point of the diagram, so it stays as a single-node tree
	fg.roots = append(fg.roots, fg.orphans...)
	fg.orphans = nil
	return fg
}
