// ct — codetree: a code structure map for the terminal.
//
// Bare `ct` on a TTY opens the TUI browser; with arguments or piped stdout
// it behaves as a pure CLI and prints a text tree.
package main

import (
	"flag"
	"fmt"
	"os"

	"codetree/core"
	"codetree/langs"
	_ "codetree/langs/golang"
	_ "codetree/langs/python"
	"codetree/render/json"
	"codetree/render/mermaid"
	"codetree/render/text"
	"codetree/tui"
)

func main() {
	var (
		all    bool
		depth  int
		format string
		lang   string
	)
	flag.BoolVar(&all, "a", false, "show all symbols including variables/constants")
	flag.BoolVar(&all, "all", false, "show all symbols including variables/constants")
	flag.IntVar(&depth, "d", 0, "limit tree depth (1=files, 2=classes, 3=methods)")
	flag.IntVar(&depth, "depth", 0, "limit tree depth (1=files, 2=classes, 3=methods)")
	flag.StringVar(&format, "f", "text", "output format: text|json|mermaid")
	flag.StringVar(&format, "format", "text", "output format: text|json|mermaid")
	flag.StringVar(&lang, "l", "", "force language (python|go); default: auto by extension")
	flag.StringVar(&lang, "lang", "", "force language (python|go); default: auto by extension")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "ct — code structure map for the terminal\n\nusage: ct [flags] [path]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	path := "."
	if flag.NArg() > 0 {
		path = flag.Arg(0)
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
	default:
		fmt.Fprintf(os.Stderr, "ct: unknown format %q (want text|json|mermaid)\n", format)
		os.Exit(2)
	}
	fmt.Print(out)
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
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
