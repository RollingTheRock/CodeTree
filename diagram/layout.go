package diagram

import (
	"math"
)

// Tidy-tree layout (Walker/Buchheim algorithm) in character cells.
// Each lnode's x is its box's horizontal center; y is derived per level
// afterwards because box heights vary when compartments are expanded.

type lnode struct {
	g        *gnode
	parent   *lnode
	children []*lnode
	w, h     int // box size in cells

	prelim, mod      float64
	shift, change    float64
	thread, ancestor *lnode
	number           int

	x float64 // final center x
}

// box is a placed card on the canvas.
type box struct {
	ln       *lnode
	x, y     int // top-left
	w, h     int
	external bool
}

func (b *box) cx() int     { return b.x + b.w/2 }
func (b *box) bottom() int { return b.y + b.h - 1 }

func buildLayoutTree(g *gnode, opts Options) *lnode {
	ln := &lnode{g: g}
	ln.w, ln.h = cardSize(g, opts)
	for _, c := range g.children {
		cl := buildLayoutTree(c, opts)
		cl.parent = ln
		cl.number = len(ln.children)
		ln.children = append(ln.children, cl)
	}
	return ln
}

func spacing(a, b *lnode) float64 {
	return float64(a.w+b.w)/2 + HSep
}

func leftSibling(v *lnode) *lnode {
	if v.parent == nil || v.number == 0 {
		return nil
	}
	return v.parent.children[v.number-1]
}

func leftmostSibling(v *lnode) *lnode {
	if v.parent == nil {
		return v
	}
	return v.parent.children[0]
}

// nextLeft / nextRight walk left/right contours, following threads.
func nextLeft(v *lnode) *lnode {
	if len(v.children) > 0 {
		return v.children[0]
	}
	return v.thread
}

func nextRight(v *lnode) *lnode {
	if len(v.children) > 0 {
		return v.children[len(v.children)-1]
	}
	return v.thread
}

// layout runs Buchheim's algorithm: firstWalk assigns preliminary x,
// secondWalk accumulates modifiers into final center coordinates.
func layout(root *lnode) {
	firstWalk(root)
	secondWalk(root, -root.prelim)
}

func firstWalk(v *lnode) {
	if len(v.children) == 0 {
		if ls := leftSibling(v); ls != nil {
			v.prelim = ls.prelim + spacing(ls, v)
		}
		return
	}
	defaultAncestor := v.children[0]
	for _, w := range v.children {
		firstWalk(w)
		defaultAncestor = apportion(w, defaultAncestor)
	}
	executeShifts(v)
	midpoint := (v.children[0].prelim + v.children[len(v.children)-1].prelim) / 2
	if ls := leftSibling(v); ls != nil {
		v.prelim = ls.prelim + spacing(ls, v)
		v.mod = v.prelim - midpoint
	} else {
		v.prelim = midpoint
	}
}

func apportion(v, defaultAncestor *lnode) *lnode {
	ls := leftSibling(v)
	if ls == nil {
		return defaultAncestor
	}
	vir, vor := v, v
	vil, vol := ls, leftmostSibling(v)
	sir, sor := v.mod, v.mod
	sil, sol := ls.mod, leftmostSibling(v).mod

	for nextRight(vil) != nil && nextLeft(vir) != nil {
		vil = nextRight(vil)
		vir = nextLeft(vir)
		vol = nextLeft(vol)
		vor = nextRight(vor)
		vor.ancestor = v
		shift := (vil.prelim + sil) - (vir.prelim + sir) + spacing(vil, vir)
		if shift > 0 {
			moveSubtree(ancestorOf(vil, v, defaultAncestor), v, shift)
			sir += shift
			sor += shift
		}
		sil += vil.mod
		sir += vir.mod
		sol += vol.mod
		sor += vor.mod
	}
	if nextRight(vil) != nil && nextRight(vor) == nil {
		vor.thread = nextRight(vil)
		vor.mod += sil - sor
	}
	if nextLeft(vir) != nil && nextLeft(vol) == nil {
		vol.thread = nextLeft(vir)
		vol.mod += sir - sol
	}
	return v
}

func ancestorOf(vil, v, defaultAncestor *lnode) *lnode {
	if vil.ancestor != nil && vil.ancestor.parent == v.parent {
		return vil.ancestor
	}
	return defaultAncestor
}

func moveSubtree(wl, wr *lnode, shift float64) {
	subtrees := float64(wr.number - wl.number)
	if subtrees == 0 {
		return
	}
	wr.change -= shift / subtrees
	wr.shift += shift
	wl.change += shift / subtrees
	wr.prelim += shift
	wr.mod += shift
}

func executeShifts(v *lnode) {
	var shift, change float64
	for i := len(v.children) - 1; i >= 0; i-- {
		w := v.children[i]
		w.prelim += shift
		w.mod += shift
		change += w.change
		shift += w.shift + change
	}
}

func secondWalk(v *lnode, m float64) {
	v.x = v.prelim + m
	for _, c := range v.children {
		secondWalk(c, m+v.mod)
	}
}

// place converts float centers to integer box positions and stacks levels
// vertically. Returns all boxes, tree width, tree height.
func place(root *lnode, xOff float64) ([]*box, float64, int) {
	var nodes []*lnode
	var walk func(n *lnode, depth int)
	maxDepth := 0
	walk = func(n *lnode, depth int) {
		nodes = append(nodes, n)
		if depth > maxDepth {
			maxDepth = depth
		}
		for _, c := range n.children {
			walk(c, depth+1)
		}
	}
	walk(root, 0)

	// per-level heights (boxes are bottom-aligned within their level)
	levelH := make([]int, maxDepth+1)
	depthOf := map[*lnode]int{}
	var mark func(n *lnode, depth int)
	mark = func(n *lnode, depth int) {
		depthOf[n] = depth
		if n.h > levelH[depth] {
			levelH[depth] = n.h
		}
		for _, c := range n.children {
			mark(c, depth+1)
		}
	}
	mark(root, 0)

	levelY := make([]int, maxDepth+1)
	for d := 1; d <= maxDepth; d++ {
		levelY[d] = levelY[d-1] + levelH[d-1] + VGap
	}

	// normalize: leftmost box edge at xOff
	minX := nodes[0].x - float64(nodes[0].w)/2
	for _, n := range nodes {
		if l := n.x - float64(n.w)/2; l < minX {
			minX = l
		}
	}

	var boxes []*box
	maxRight := 0.0
	maxBottom := 0
	for _, n := range nodes {
		left := int(math.Round(n.x - float64(n.w)/2 - minX + xOff))
		d := depthOf[n]
		top := levelY[d] + (levelH[d] - n.h) // bottom-aligned in level
		b := &box{ln: n, x: left, y: top, w: n.w, h: n.h, external: n.g.external}
		boxes = append(boxes, b)
		if r := float64(left + n.w); r > maxRight {
			maxRight = r
		}
		if top+n.h > maxBottom {
			maxBottom = top + n.h
		}
	}
	return boxes, maxRight - xOff, maxBottom
}

func findBox(boxes []*box, ln *lnode) *box {
	for _, b := range boxes {
		if b.ln == ln {
			return b
		}
	}
	return nil
}

func findBoxByG(boxes []*box, g *gnode) *box {
	for _, b := range boxes {
		if b.ln.g == g {
			return b
		}
	}
	return nil
}
