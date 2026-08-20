// Package diagram renders a core.Project's class inheritance forest as a
// character-canvas UML class diagram. Pure logic: no TUI dependencies.
//
// Pipeline: build inheritance graph → (optional) neighborhood filter →
// Buchheim tidy-tree layout in character cells → elbow edge routing with
// direction-bitmask junction resolution → box/card drawing → styled output.
package diagram

import (
	"fmt"
	"sort"
	"strings"

	"codetree/core"
)

// Layout constants (character cells).
const (
	HSep    = 4 // horizontal gap between sibling boxes
	VGap    = 3 // vertical channel rows between levels
	TreeGap = 6 // gap between separate inheritance trees
)

// Options controls diagram rendering.
type Options struct {
	Members   bool     // expand field/method compartments (default collapsed)
	Focus     string   // neighborhood mode: focus class name
	Up        int      // ancestor levels to show; <0 = all (default)
	Down      int      // descendant levels to show (default 2)
	Siblings  bool     // include focus class's siblings
	External  bool     // render unresolved (external) bases as gray boxes
	Color     bool     // emit ANSI colors
	Highlight string   // class name to highlight (TUI selection)
	WrapWidth int      // min width for orphan-grid wrapping (0 = content width)
	Files     []string // file scope: protagonists defined in these files (empty = whole project)
	Assoc     bool     // draw composition edges from field types
}

// DefaultOptions returns the defaults.
func DefaultOptions() Options {
	return Options{Up: -1, Down: 2, External: true, Assoc: true}
}

// Diagram is the rendered result.
type Diagram struct {
	Text   string
	Width  int // canvas width in cells
	Height int
	Nodes  []PlacedNode // box placements, for TUI hit-testing/navigation
	Focus  string       // resolved focus name, if any
}

// PlacedNode is one rendered box's position.
type PlacedNode struct {
	Name     string
	Sym      *core.Symbol // nil for external base nodes
	External bool
	Context  bool // scope mode: pulled-in relative, rendered dimmed
	X, Y     int  // top-left cell
	W, H     int

	// Layout-skeleton relations (tidy-tree only — implements/composition
	// edges do not create navigation links). Indexes into Diagram.Nodes.
	Parent   int   // -1 for roots
	Children []int // ordered by visual X (left to right)
}

// ErrFocusNotFound is returned when Options.Focus matches no class.
type ErrFocusNotFound struct {
	Focus     string
	Available []string
}

func (e *ErrFocusNotFound) Error() string {
	return fmt.Sprintf("class %q not found (available: %s)", e.Focus, strings.Join(e.Available, ", "))
}

// Build renders the project into a character-canvas diagram.
func Build(p *core.Project, opts Options) (*Diagram, error) {
	g := buildGraph(p, opts)
	var focusName string
	switch {
	case opts.Focus != "":
		// focus (neighborhood) takes priority over file scope
		fg, name, err := filterNeighborhood(g, opts)
		if err != nil {
			return nil, err
		}
		g = fg
		focusName = name
	case len(opts.Files) > 0:
		g = filterScope(g, opts.Files)
	}

	d := &Diagram{Focus: focusName}
	if len(g.roots) == 0 && len(g.orphans) == 0 {
		d.Text = "(no classes found)\n"
		return d, nil
	}

	var trees []*lnode
	for _, r := range g.roots {
		trees = append(trees, buildLayoutTree(r, opts))
	}

	// layout each tree, place side by side
	var boxes []*box
	xOff := 0.0
	maxY := 0
	for _, t := range trees {
		layout(t) // Buchheim: sets x centers (float)
		bb, w, h := place(t, xOff)
		boxes = append(boxes, bb...)
		xOff += w + TreeGap
		if h > maxY {
			maxY = h
		}
	}

	canvasW := int(xOff) - TreeGap
	if canvasW < 0 {
		canvasW = 0
	}
	canvasH := maxY

	c := newCanvas(canvasW, canvasH)
	// reserve box cells so edges never cross them
	for _, b := range boxes {
		c.reserve(b)
	}
	// edges first (skipping box cells), then boxes, then arrowheads
	for _, b := range boxes {
		if b.ln.g.parent == nil {
			continue
		}
		pb := findBox(boxes, b.ln.parent)
		if pb != nil {
			sty := stEdgeSolid
			if pb.external || b.external {
				sty = stEdgeExt // edges touching external bases are dimmed
			}
			c.edge(pb.cx(), pb.bottom(), b.cx(), b.y, sty, false)
		}
	}
	for _, b := range boxes {
		c.drawBox(b, opts)
	}
	for _, b := range boxes {
		if b.ln.g.parent == nil {
			continue
		}
		sty := stEdgeSolid
		if pb := findBox(boxes, b.ln.parent); pb != nil && (pb.external || b.external) {
			sty = stEdgeExt
		}
		c.arrow(b.cx(), b.y, sty)
	}

	// dashed implements edges (best-effort, drawn last: box cells are
	// occupied so the line can never enter a card; crossings render as ┼)
	for _, b := range boxes {
		for _, in := range b.ln.g.implNodes {
			if ib := findBoxByG(boxes, in); ib != nil && ib != b {
				c.implementsEdge(ib, b)
			}
		}
	}

	// amber composition edges: ◆ on the owner side, no arrowhead
	if opts.Assoc {
		for _, b := range boxes {
			for _, tn := range b.ln.g.assocTargets {
				if tb := findBoxByG(boxes, tn); tb != nil && tb != b {
					c.assocEdge(b, tb)
				}
			}
		}
	}

	// orphan partition below the trees
	if opts.Focus == "" && len(g.orphans) > 0 {
		header := fmt.Sprintf("── unrelated classes (%d) ", len(g.orphans))
		c.appendOrphans(g.orphans, opts, header)
	}

	d.Width = c.width
	d.Height = c.height
	d.Text = c.render(opts)

	// sort boxes by (Y, X), then wire layout-skeleton relations as indexes
	// into the sorted Nodes slice (immune to duplicate class names)
	boxesAll := c.allBoxes
	order := make([]int, len(boxesAll))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := boxesAll[order[i]], boxesAll[order[j]]
		if a.y != b.y {
			return a.y < b.y
		}
		return a.x < b.x
	})
	idxOf := make(map[*box]int, len(boxesAll))
	nodes := make([]PlacedNode, len(boxesAll))
	for rank, bi := range order {
		b := boxesAll[bi]
		idxOf[b] = rank
		nodes[rank] = PlacedNode{
			Name: b.ln.g.name, Sym: b.ln.g.sym, External: b.external, Context: b.ln.g.context,
			X: b.x, Y: b.y, W: b.w, H: b.h, Parent: -1,
		}
	}
	for _, b := range boxesAll {
		i := idxOf[b]
		if p := b.ln.parent; p != nil {
			if pb := findBox(boxesAll, p); pb != nil {
				nodes[i].Parent = idxOf[pb]
			}
		}
		var kids []int
		for _, cl := range b.ln.children {
			if kb := findBox(boxesAll, cl); kb != nil {
				kids = append(kids, idxOf[kb])
			}
		}
		sort.Slice(kids, func(a, b2 int) bool { return nodes[kids[a]].X < nodes[kids[b2]].X })
		nodes[i].Children = kids
	}
	d.Nodes = nodes
	return d, nil
}
