// ct — codetree: a code structure map for the terminal.
//
// Bare `ct` on a TTY opens the TUI browser; with arguments or piped stdout
// it behaves as a pure CLI and prints a text tree.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/term"

	"codetree/core"
	"codetree/diagram"
	"codetree/langs"
	_ "codetree/langs/cpp"
	_ "codetree/langs/golang"
	_ "codetree/langs/java"
	_ "codetree/langs/python"
	"codetree/render/json"
	"codetree/render/mermaid"
	"codetree/render/text"
	"codetree/tui"
)

func main() {
	var (
		all      bool
		depth    int
		format   string
		lang     string
		members  bool
		focus    string
		up       int
		down     int
		siblings bool
		external bool
		assoc    bool
	)
	flag.BoolVar(&all, "a", false, "show all symbols including variables/constants")
	flag.BoolVar(&all, "all", false, "show all symbols including variables/constants")
	flag.IntVar(&depth, "d", 0, "limit tree depth (1=files, 2=classes, 3=methods)")
	flag.IntVar(&depth, "depth", 0, "limit tree depth (1=files, 2=classes, 3=methods)")
	flag.StringVar(&format, "f", "text", "output format: text|json|mermaid|diagram")
	flag.StringVar(&format, "format", "text", "output format: text|json|mermaid|diagram")
	flag.StringVar(&lang, "l", "", "force language (python|go|java|cpp); default: auto by extension")
	flag.StringVar(&lang, "lang", "", "force language (python|go|java|cpp); default: auto by extension")
	flag.BoolVar(&members, "members", false, "diagram: expand field/method compartments")
	flag.StringVar(&focus, "focus", "", "diagram: neighborhood mode, focus on this class")
	flag.IntVar(&up, "up", -1, "diagram: ancestor levels in focus mode (-1 = all)")
	flag.IntVar(&down, "down", 2, "diagram: descendant levels in focus mode")
	flag.BoolVar(&siblings, "siblings", false, "diagram: include focus class's siblings")
	flag.BoolVar(&external, "external", true, "diagram: show unresolved bases as gray boxes")
	flag.BoolVar(&assoc, "assoc", true, "diagram: draw composition edges from field types (◆)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "ct — code structure map for the terminal\n\nusage: ct [flags] [path]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	path := "."
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}

	// Diagram + file arguments: the file is the scope, but relatives may
	// live anywhere in the project — find the project root (upward via
	// root markers), scan it, and scope to the given file(s).
	var scopeFiles []string
	if format == "diagram" && flag.NArg() > 0 {
		var files []string
		allFiles := true
		for _, a := range flag.Args() {
			if st, err := os.Stat(a); err == nil && !st.IsDir() {
				files = append(files, a)
			} else {
				allFiles = false
			}
		}
		if allFiles && len(files) > 0 {
			root := findProjectRoot(files[0])
			for _, f := range files {
				abs, err := filepath.Abs(f)
				if err != nil {
					continue
				}
				rel, err := filepath.Rel(root, abs)
				if err != nil {
					continue
				}
				scopeFiles = append(scopeFiles, filepath.ToSlash(rel))
			}
			path = root
		}
	}

	// Bare `ct` on a TTY with no arguments opens the TUI browser.
	if flag.NFlag() == 0 && flag.NArg() == 0 && isTTY(os.Stdout) {
		if err := tui.Run(path, core.ScanOptions{Lang: lang, IncludeVars: all}); err != nil {
			fmt.Fprintln(os.Stderr, "ct:", err)
			os.Exit(1)
		}
		return
	}

	// Non-TTY output is forced to text unless a format was explicitly given.
	if !isTTY(os.Stdout) && !flagPassed("f", "format") {
		format = "text"
	}

	proj, err := core.Scan(path, langs.Registry{}, core.ScanOptions{Lang: lang, IncludeVars: all})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ct:", err)
		os.Exit(1)
	}

	var out string
	switch format {
	case "text", "":
		out = text.Render(proj, text.Options{Depth: depth, All: all})
	case "json":
		out, err = json.Render(proj, json.Options{Depth: depth, All: all})
		if err != nil {
			fmt.Fprintln(os.Stderr, "ct:", err)
			os.Exit(1)
		}
	case "mermaid":
		out = mermaid.Render(proj)
	case "diagram":
		dopts := diagram.DefaultOptions()
		dopts.Members = members
		dopts.Focus = focus
		dopts.Up = up
		dopts.Down = down
		dopts.Siblings = siblings
		dopts.External = external
		dopts.Assoc = assoc
		dopts.Files = scopeFiles
		dopts.Color = isTTY(os.Stdout)
		if dopts.Color {
			if w, _, terr := term.GetSize(int(os.Stdout.Fd())); terr == nil {
				dopts.WrapWidth = w - 2
			}
		}
		d, derr := diagram.Build(proj, dopts)
		if derr != nil {
			fmt.Fprintln(os.Stderr, "ct:", derr)
			os.Exit(1)
		}
		out = d.Text
		if w, _, terr := term.GetSize(int(os.Stdout.Fd())); terr == nil && d.Width > w {
			fmt.Fprintf(os.Stderr, "ct: diagram is %d cells wide, terminal is %d; consider --focus <Class>\n", d.Width, w)
		}
	default:
		fmt.Fprintf(os.Stderr, "ct: unknown format %q (want text|json|mermaid|diagram)\n", format)
		os.Exit(2)
	}
	fmt.Print(out)
}

func isTTY(f *os.File) bool {
	// /dev/null is a char device; only a real terminal counts
	return term.IsTerminal(int(f.Fd()))
}

// rootMarkers identify a project root directory.
var rootMarkers = []string{".git", "pyproject.toml", "go.mod", "package.json", "setup.py", "Cargo.toml"}

// findProjectRoot walks up from a file looking for root markers; falls back
// to the file's directory.
func findProjectRoot(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return filepath.Dir(file)
	}
	dir := filepath.Dir(abs)
	for {
		for _, m := range rootMarkers {
			if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Dir(abs)
		}
		dir = parent
	}
}

func flagPassed(names ...string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		for _, n := range names {
			if f.Name == n {
				found = true
			}
		}
	})
	return found
}
