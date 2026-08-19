// Package text renders a Project as an indented tree with ├── └── │ guides.
package text

import (
	"path/filepath"
	"sort"
	"strings"

	"codetree/core"
)

// Options controls text rendering.
type Options struct {
	Depth int  // 0 = unlimited; 1 = files only; 2 = top-level symbols; 3 = their children…
	All   bool // show variables/constants (they are already filtered by scan opts)
}

// dirNode is an intermediate directory in the rendered tree.
type dirNode struct {
	name  string
	dirs  map[string]*dirNode
	files []*core.File
}

// Render writes the project tree to a string.
func Render(p *core.Project, opts Options) string {
	root := &dirNode{name: displayRoot(p.Root), dirs: map[string]*dirNode{}}
	for _, f := range p.Files {
		parts := strings.Split(f.Path, "/")
		n := root
		for _, d := range parts[:len(parts)-1] {
			if n.dirs[d] == nil {
				n.dirs[d] = &dirNode{name: d, dirs: map[string]*dirNode{}}
			}
			n = n.dirs[d]
		}
		n.files = append(n.files, f)
	}

	var b strings.Builder
	b.WriteString(root.name + "/\n")
	renderDir(&b, root, "", opts, 1)
	return b.String()
}

func displayRoot(root string) string {
	base := filepath.Base(filepath.Clean(root))
	if base == "." || base == "/" || base == "" {
		return "project"
	}
	return base
}

func renderDir(b *strings.Builder, n *dirNode, prefix string, opts Options, depth int) {
	// directories first, then files, both sorted
	var names []string
	for name := range n.dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	files := append([]*core.File(nil), n.files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	total := len(names) + len(files)
	i := 0
	connector := func() (string, string) {
		i++
		if i == total {
			return "└── ", "    "
		}
		return "├── ", "│   "
	}

	for _, name := range names {
		c, childPrefix := connector()
		b.WriteString(prefix + c + name + "/\n")
		// directories don't consume depth: depth limits symbols, not folders
		renderDir(b, n.dirs[name], prefix+childPrefix, opts, depth)
	}
	for _, f := range files {
		c, childPrefix := connector()
		b.WriteString(prefix + c + filepath.Base(f.Path) + "\n")
		// symbol depth: files sit at current depth; symbols are one deeper
		if opts.Depth == 0 || depth < opts.Depth {
			renderSymbols(b, f.Symbols, prefix+childPrefix, opts, depth+1)
		}
	}
}

func renderSymbols(b *strings.Builder, syms []*core.Symbol, prefix string, opts Options, depth int) {
	visible := filterKinds(syms, opts)
	for i, s := range visible {
		last := i == len(visible)-1
		c, childPrefix := "├── ", "│   "
		if last {
			c, childPrefix = "└── ", "    "
		}
		b.WriteString(prefix + c + s.Label() + "\n")
		if opts.Depth == 0 || depth < opts.Depth {
			renderSymbols(b, s.Children, prefix+childPrefix, opts, depth+1)
		}
	}
}

func filterKinds(syms []*core.Symbol, opts Options) []*core.Symbol {
	if opts.All {
		return syms
	}
	out := make([]*core.Symbol, 0, len(syms))
	for _, s := range syms {
		if s.Kind == core.KindVariable || s.Kind == core.KindConstant {
			continue
		}
		out = append(out, s)
	}
	return out
}

// label renders one symbol line, delegating to core.Symbol.Label.
func label(s *core.Symbol) string { return s.Label() }
