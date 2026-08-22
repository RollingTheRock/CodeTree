package diagram

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/RollingTheRock/CodeTree/core"
)

// ---- styles ---------------------------------------------------------------
//
// All diagram styling is centralized here. ANSI is emitted only when
// Options.Color is set (CLI: TTY; TUI: always on).

type style uint8

const (
	stNone style = iota
	stBorder
	stTitle     // class name: bold bright
	stBase      // base-class names: cyan
	stIconClass // C class icon: bright cyan
	stIconIface // I interface icon: green
	stIconStruct
	stIconEnum
	stField     // P field icon: orange
	stMethod    // m method icon: magenta
	stExtern    // external bases: gray
	stEdgeSolid // inheritance edges: bright blue
	stEdgeDash  // implements edges: green dashed
	stEdgeAssoc // composition edges: amber solid with ◆
	stEdgeExt   // edges to external bases: dim gray
	stHeader    // section headers
	stDim
	stContext   // scope-mode context nodes: readable but dimmed
	stHighlight // TUI selection / focus class: bright yellow
)

var styleMap = map[style]lipgloss.Style{
	stBorder:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	stTitle:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
	stBase:       lipgloss.NewStyle().Foreground(lipgloss.Color("51")),
	stIconClass:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51")),
	stIconIface:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("78")),
	stIconStruct: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
	stIconEnum:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220")),
	stField:      lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	stMethod:     lipgloss.NewStyle().Foreground(lipgloss.Color("213")),
	stExtern:     lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
	stEdgeSolid:  lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	stEdgeDash:   lipgloss.NewStyle().Foreground(lipgloss.Color("78")),
	stEdgeAssoc:  lipgloss.NewStyle().Foreground(lipgloss.Color("208")),
	stEdgeExt:    lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	stHeader:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	stDim:        lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	stContext:    lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	stHighlight:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("227")),
}

// ---- canvas ---------------------------------------------------------------

// direction bit mask for edge cells; resolved to box chars at render time.
const (
	dirUp = 1 << iota
	dirDown
	dirLeft
	dirRight
)

var dirChar = map[int]rune{
	dirUp: '│', dirDown: '│', dirLeft: '─', dirRight: '─',
	dirUp | dirDown: '│', dirLeft | dirRight: '─',
	dirUp | dirRight: '└', dirUp | dirLeft: '┘',
	dirDown | dirRight: '┌', dirDown | dirLeft: '┐',
	dirUp | dirDown | dirRight: '├', dirUp | dirDown | dirLeft: '┤',
	dirUp | dirLeft | dirRight: '┴', dirDown | dirLeft | dirRight: '┬',
	dirUp | dirDown | dirLeft | dirRight: '┼',
}

// dashChar resolves dashed (implements) edges. Corners/junctions have no
// dashed Unicode variants, so they fall back to the solid glyph with the
// dashed edge's color.
func dashChar(mask int) rune {
	switch mask {
	case dirUp, dirDown, dirUp | dirDown:
		return '┆'
	case dirLeft, dirRight, dirLeft | dirRight:
		return '┄'
	default:
		return dirChar[mask]
	}
}

type cell struct {
	ch       rune // box/text glyph; 0 = edge or empty
	sty      style
	dir      int  // edge direction mask (when ch == 0)
	dash     bool // edge is dashed (implements)
	occupied bool // reserved by a box: edges must not cross
	border   bool // part of a box border/separator: restyled on highlight
}

type canvas struct {
	width, height int
	cells         [][]cell
	allBoxes      []*box // every box incl. orphans (for PlacedNode export)
}

func newCanvas(w, h int) *canvas {
	c := &canvas{width: w, height: h}
	c.cells = make([][]cell, h)
	for i := range c.cells {
		c.cells[i] = make([]cell, w)
	}
	return c
}

func (c *canvas) growTo(w, h int) {
	if w <= c.width && h <= c.height {
		return
	}
	nw, nh := max(w, c.width), max(h, c.height)
	rows := make([][]cell, nh)
	for i := range rows {
		rows[i] = make([]cell, nw)
		if i < c.height {
			copy(rows[i], c.cells[i])
		}
	}
	c.cells, c.width, c.height = rows, nw, nh
}

func (c *canvas) set(x, y int, ch rune, sty style) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	c.cells[y][x] = cell{ch: ch, sty: sty, occupied: true}
}

// setBorder is set for box border/separator cells: it additionally marks the
// cell so restyleBox can restyle exactly the cells drawBox styled with
// borderSty.
func (c *canvas) setBorder(x, y int, ch rune, sty style) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	c.cells[y][x] = cell{ch: ch, sty: sty, occupied: true, border: true}
}

func (c *canvas) writeString(x, y int, s string, sty style) {
	for i, r := range []rune(s) {
		c.set(x+i, y, r, sty)
	}
}

func (c *canvas) setBit(x, y, bit int, sty style, dash bool) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	cl := &c.cells[y][x]
	if cl.occupied { // never draw edges through boxes
		return
	}
	if cl.dir == 0 { // first edge touching this cell sets its style
		cl.sty = sty
		cl.dash = dash
	}
	cl.dir |= bit
}

func (c *canvas) vline(x, y0, y1 int, sty style, dash bool) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		if y > y0 {
			c.setBit(x, y, dirUp, sty, dash)
		}
		if y < y1 {
			c.setBit(x, y, dirDown, sty, dash)
		}
	}
}

func (c *canvas) hline(x0, x1, y int, sty style, dash bool) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		if x > x0 {
			c.setBit(x, y, dirLeft, sty, dash)
		}
		if x < x1 {
			c.setBit(x, y, dirRight, sty, dash)
		}
	}
}

// edge draws an elbow connector: parent bottom center → child top center.
// Vertical channel row is the midpoint of the inter-level gap. Lines can
// cross each other (junctions resolved via dir bits) but never enter boxes
// (reserve marks them occupied first). sty picks the semantic color;
// dashed selects the ┄/┆ glyph set (implements edges, data layer pending).
func (c *canvas) edge(px, pBot, cx, cTop int, sty style, dashed bool) {
	if cTop <= pBot+1 {
		return
	}
	midY := pBot + (cTop-pBot)/2
	c.vline(px, pBot+1, midY, sty, dashed)
	c.hline(min(px, cx), max(px, cx), midY, sty, dashed)
	c.vline(cx, midY, cTop-1, sty, dashed)
	c.setBit(px, pBot+1, dirUp, sty, dashed)   // visually touch the parent's bottom border
	c.setBit(cx, cTop-1, dirDown, sty, dashed) // lead into the arrowhead
}

// arrow overwrites the child's top border midpoint with an arrowhead.
func (c *canvas) arrow(cx, top int, sty style) {
	if cx < 0 || top < 0 || cx >= c.width || top >= c.height {
		return
	}
	c.cells[top][cx].ch = '▲'
	c.cells[top][cx].sty = sty
}

// edgeBetween draws an elbow between two boxes, direction-agnostic, and
// returns a's anchor cell (border row/col facing b) for glyph placement.
// Best-effort: may cross other edges but never enters boxes (reserve marks
// them occupied). Same-row pairs dip one row below both boxes.
func (c *canvas) edgeBetween(a, b *box, sty style, dashed bool) (ax, ay int, aAbove bool) {
	ax, bx := a.cx(), b.cx()

	var aAnchor, bAnchor int // border rows of each box
	var aEnd, bEnd int       // edge rows just outside each box
	var aBit, bBit int       // dir bits pointing back toward each box
	var midY int
	switch {
	case a.bottom() < b.y: // a strictly above
		aAbove = true
		aAnchor, bAnchor = a.bottom(), b.y
		aEnd, bEnd = aAnchor+1, bAnchor-1
		aBit, bBit = dirUp, dirDown
		midY = (aAnchor + bAnchor) / 2
		if midY <= aAnchor {
			midY = aAnchor + 1
		}
	case b.bottom() < a.y: // a strictly below
		aAnchor, bAnchor = a.y, b.bottom()
		aEnd, bEnd = aAnchor-1, bAnchor+1
		aBit, bBit = dirDown, dirUp
		midY = (aAnchor + bAnchor) / 2
		if midY >= aAnchor {
			midY = aAnchor - 1
		}
	default: // same row band: dip one row below both boxes
		aAbove = true // glyph on a's bottom border points up into a
		aAnchor, bAnchor = a.bottom(), b.bottom()
		aEnd, bEnd = aAnchor+1, bAnchor+1
		aBit, bBit = dirUp, dirUp
		midY = max(aAnchor, bAnchor) + 1
	}

	// the dip-below channel may extend past the current canvas
	if midY >= c.height {
		c.growTo(c.width, midY+1)
	}
	c.vline(ax, min(aEnd, midY), max(aEnd, midY), sty, dashed)
	c.hline(min(ax, bx), max(ax, bx), midY, sty, dashed)
	c.vline(bx, min(bEnd, midY), max(bEnd, midY), sty, dashed)
	c.setBit(ax, aEnd, aBit, sty, dashed)
	c.setBit(bx, bEnd, bBit, sty, dashed)
	return ax, aAnchor, aAbove
}

// implementsEdge draws a dashed green implements edge; the arrowhead sits on
// the interface's border facing the implementor (▲ above, ▼ below).
func (c *canvas) implementsEdge(iface, impl *box) {
	ax, ay, aAbove := c.edgeBetween(iface, impl, stEdgeDash, true)
	arrow := '▲'
	if !aAbove {
		arrow = '▼'
	}
	if ax >= 0 && ay >= 0 && ax < c.width && ay < c.height {
		c.cells[ay][ax].ch = arrow
		c.cells[ay][ax].sty = stEdgeDash
	}
}

// assocEdge draws a solid amber composition edge: diamond ◆ on the owner's
// border facing the target, plain line to the target, no arrowhead.
func (c *canvas) assocEdge(owner, target *box) {
	ax, ay, _ := c.edgeBetween(owner, target, stEdgeAssoc, false)
	if ax >= 0 && ay >= 0 && ax < c.width && ay < c.height {
		c.cells[ay][ax].ch = '◆'
		c.cells[ay][ax].sty = stEdgeAssoc
	}
}

// reserve marks a box's rect as occupied so edges skip it.
func (c *canvas) reserve(b *box) {
	for y := b.y; y < b.y+b.h; y++ {
		for x := b.x; x < b.x+b.w; x++ {
			if x >= 0 && y >= 0 && x < c.width && y < c.height {
				c.cells[y][x].occupied = true
			}
		}
	}
}

// render produces the final text. With Color, styles become ANSI sequences
// via lipgloss (which strips them automatically when output is not a TTY).
func (c *canvas) render(opts Options) string {
	var b strings.Builder
	for y := 0; y < c.height; y++ {
		var line strings.Builder
		row := c.cells[y]
		// trim trailing blanks
		end := len(row)
		for end > 0 && row[end-1].ch == 0 && row[end-1].dir == 0 {
			end--
		}
		resolve := func(cl cell) (rune, style) {
			if cl.ch != 0 {
				return cl.ch, cl.sty
			}
			if cl.dir != 0 {
				if cl.dash {
					return dashChar(cl.dir), cl.sty
				}
				return dirChar[cl.dir], cl.sty
			}
			return ' ', stNone
		}
		// batch consecutive cells sharing a style into one run; advance
		// unconditionally — skipping only styled runs made long blank
		// stretches O(n²) in color mode
		for x := 0; x < end; {
			ch, sty := resolve(row[x])
			j := x + 1
			for j < end {
				if _, sty2 := resolve(row[j]); sty2 != sty {
					break
				}
				j++
			}
			var sb strings.Builder
			sb.WriteRune(ch)
			for k := x + 1; k < j; k++ {
				ch2, _ := resolve(row[k])
				sb.WriteRune(ch2)
			}
			if opts.Color && sty != stNone {
				if st, ok := styleMap[sty]; ok {
					line.WriteString(st.Render(sb.String()))
				} else {
					line.WriteString(sb.String())
				}
			} else {
				line.WriteString(sb.String())
			}
			x = j
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// ---- cards ----------------------------------------------------------------

type segment struct {
	text string
	sty  style
}

// cardLines returns the styled content lines (without borders) and the
// card's inner width.
func cardContent(g *gnode, opts Options) (title []segment, fields, methods [][]segment) {
	icon, iconSty := kindIcon(g)
	nameSty := pick(stTitle, stExtern, g.external)
	if g.context && !g.external {
		nameSty = stContext
		iconSty = stContext
	}
	title = append(title,
		segment{icon, iconSty},
		segment{" ", iconSty},
		segment{g.name, nameSty},
	)
	for _, b := range g.secBases {
		title = append(title, segment{" +" + b, stBase})
	}
	for _, b := range g.extBases {
		title = append(title, segment{" ?" + b, stBase})
	}
	for _, b := range g.implExternal {
		title = append(title, segment{" ~" + b, stEdgeDash})
	}
	// context nodes show where they come from
	if g.context && g.sym != nil && g.sym.File != "" {
		title = append(title, segment{" ·" + g.sym.File, stDim})
	}
	if !opts.Members || g.sym == nil {
		return title, nil, nil
	}
	for _, f := range g.sym.Fields {
		text := f.Name
		if f.Type != "" && !f.Embedded {
			text += ": " + f.Type
		}
		if f.ClassVar {
			text += " ="
		}
		fields = append(fields, []segment{{"P ", stField}, {text, stNone}})
	}
	for _, m := range g.sym.Children {
		if m.Kind != core.KindMethod {
			continue
		}
		methods = append(methods, []segment{{"m ", stMethod}, {m.Label(), stNone}})
	}
	return title, fields, methods
}

// kindIcon maps a node to its single-character IDEA-style type icon.
func kindIcon(g *gnode) (string, style) {
	if g.external || g.sym == nil {
		return "C", stExtern
	}
	switch g.sym.Kind {
	case core.KindInterface:
		return "I", stIconIface
	case core.KindStruct:
		return "S", stIconStruct
	case core.KindEnum:
		return "E", stIconEnum
	default:
		return "C", stIconClass
	}
}

func segmentsWidth(segs []segment) int {
	w := 0
	for _, s := range segs {
		w += len([]rune(s.text))
	}
	return w
}

// cardSize computes box dimensions. The top border is always a plain line
// so arrowheads have a clean landing strip; the title is a centered body
// row. Collapsed = border + title + border; expanded adds compartments.
func cardSize(g *gnode, opts Options) (w, h int) {
	title, fields, methods := cardContent(g, opts)
	w = segmentsWidth(title) + 4 // "│ " + title + " │"
	if w%2 != 0 {
		w++ // even widths keep parent/child centers aligned (no 1-cell jogs)
	}
	if len(fields) == 0 && len(methods) == 0 {
		return w, 3
	}
	for _, f := range fields {
		if fw := segmentsWidth(f) + 4; fw > w {
			w = fw
		}
	}
	for _, m := range methods {
		if mw := segmentsWidth(m) + 4; mw > w {
			w = mw
		}
	}
	if w%2 != 0 {
		w++
	}
	h = 3 + len(fields) + len(methods) // borders + title row
	if len(fields) > 0 && len(methods) > 0 {
		h++ // compartment separator
	}
	return w, h
}

// ---- highlight restyling ----------------------------------------------------

// baseStyles returns the box's border and title-text styles without
// highlight, mirroring drawBox/cardContent.
func baseStyles(g *gnode) (border, title style) {
	switch {
	case g.external:
		return stExtern, stExtern
	case g.context:
		return stDim, stContext
	default:
		return stBorder, stTitle
	}
}

// restyleBox flips the highlight on a drawn box in place: border and
// separator cells (marked at draw time) plus title-row text cells. Runes and
// layout are untouched.
func (c *canvas) restyleBox(b *box, on bool) {
	border, title := baseStyles(b.ln.g)
	for y := b.y; y < b.y+b.h && y < c.height; y++ {
		for x := b.x; x < b.x+b.w && x < c.width; x++ {
			ce := &c.cells[y][x]
			if on {
				switch {
				case ce.border && ce.sty == border:
					ce.sty = stHighlight
				case y == b.y+1 && !ce.border && ce.sty == title:
					ce.sty = stHighlight
				}
			} else {
				switch {
				case ce.border && ce.sty == stHighlight:
					ce.sty = border
				case y == b.y+1 && !ce.border && ce.sty == stHighlight:
					ce.sty = title
				}
			}
		}
	}
}

// drawBox renders one card onto the canvas:
//
//	┌────────────────────┐
//	│        Dog         │   title (centered)
//	│ ▣ field1: int      │   field compartment (--members)
//	├────────────────────┤
//	│ ƒ fetch(self, item)│   method compartment
//	└────────────────────┘
func (c *canvas) drawBox(b *box, opts Options) {
	c.allBoxes = append(c.allBoxes, b)
	g := b.ln.g
	title, fields, methods := cardContent(g, opts)

	borderSty := stBorder
	if g.external {
		borderSty = stExtern
	}
	if g.context {
		borderSty = stDim
	}
	if opts.Highlight != "" && g.name == opts.Highlight {
		borderSty = stHighlight
	}

	x, y := b.x, b.y
	// plain top border (arrowhead lands at its midpoint)
	c.setBorder(x, y, '┌', borderSty)
	for i := x + 1; i < x+b.w-1; i++ {
		c.setBorder(i, y, '─', borderSty)
	}
	c.setBorder(x+b.w-1, y, '┐', borderSty)

	// bottom border
	by := b.y + b.h - 1
	c.setBorder(x, by, '└', borderSty)
	for i := x + 1; i < x+b.w-1; i++ {
		c.setBorder(i, by, '─', borderSty)
	}
	c.setBorder(x+b.w-1, by, '┘', borderSty)

	row := y + 1
	writeRow := func(segs []segment, center bool) {
		c.setBorder(x, row, '│', borderSty)
		tx := x + 2
		if center {
			tx = x + (b.w-segmentsWidth(segs))/2
		}
		for _, s := range segs {
			sty := s.sty
			// selected card: brighten the whole title (name, context-dimmed
			// or external-gray alike); keep base-name cyan readable
			if borderSty == stHighlight && (sty == stTitle || sty == stExtern || sty == stContext) {
				sty = stHighlight
			}
			c.writeString(tx, row, s.text, sty)
			tx += len([]rune(s.text))
		}
		c.setBorder(x+b.w-1, row, '│', borderSty)
		row++
	}
	writeRow(title, true)
	for _, f := range fields {
		writeRow(f, false)
	}
	if len(fields) > 0 && len(methods) > 0 {
		c.setBorder(x, row, '├', borderSty)
		for i := x + 1; i < x+b.w-1; i++ {
			c.setBorder(i, row, '─', borderSty)
		}
		c.setBorder(x+b.w-1, row, '┤', borderSty)
		row++
	}
	for _, m := range methods {
		writeRow(m, false)
	}
}

// appendOrphans adds the orphan partition below the trees: a header plus
// cards wrapped in rows. Card size honors opts.Members, same as the trees.
func (c *canvas) appendOrphans(orphans []*gnode, opts Options, header string) {
	startY := c.height + 1
	wrapW := max(c.width, 60)
	if opts.WrapWidth > wrapW {
		wrapW = opts.WrapWidth
	}
	c.growTo(wrapW, startY+2)
	c.writeString(0, startY, header+strings.Repeat("─", max(wrapW-len([]rune(header)), 0)), stHeader)

	x, y := 0, startY+1
	rowH := 0
	for _, g := range orphans {
		w, h := cardSize(g, opts)
		if x > 0 && x+w > wrapW {
			x = 0
			y += rowH + 1
			rowH = 0
		}
		c.growTo(x+w, y+h)
		ln := &lnode{g: g, w: w, h: h}
		b := &box{ln: ln, x: x, y: y, w: w, h: h}
		c.drawBox(b, opts)
		x += w + HSep
		if h > rowH {
			rowH = h
		}
	}
	_ = rowH
}

func pick(a, b style, cond bool) style {
	if cond {
		return b
	}
	return a
}
