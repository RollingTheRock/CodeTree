package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// LangProvider abstracts the language registry so core does not import langs
// (langs imports core). Registered by the caller (cmd/ct) at startup.
type LangProvider interface {
	// ByExt returns a parser for the given file extension (with dot), or nil.
	ByExt(ext string) LangParser
	// ByName returns a parser by language name, or nil.
	ByName(name string) LangParser
}

// LangParser parses one source file into top-level symbols.
type LangParser interface {
	Name() string
	Parse(path string, src []byte, opts ParseOptions) ([]*Symbol, error)
}

// ParseOptions controls extraction detail.
type ParseOptions struct {
	IncludeVars bool // collect variables/constants (default off, noise reduction)
}

// ScanOptions controls project scanning.
type ScanOptions struct {
	Lang        string // force a single language by name; "" = auto by extension
	IncludeVars bool
}

// defaultSkipDirs are always pruned regardless of .gitignore.
var defaultSkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"vendor": true, "node_modules": true,
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true,
	".venv": true, "venv": true,
	".idea": true, ".vscode": true,
	"dist": true, "build": true, "target": true,
}

// Scan walks root, parses every recognized source file and returns the
// project's symbol map. It honors the root .gitignore and always skips
// well-known noise directories.
func Scan(root string, lp LangProvider, opts ScanOptions) (*Project, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	var ign *ignore.GitIgnore
	if _, err := os.Stat(filepath.Join(abs, ".gitignore")); err == nil {
		if gi, err := ignore.CompileIgnoreFile(filepath.Join(abs, ".gitignore")); err == nil {
			ign = gi
		}
	}

	proj := &Project{Root: abs}

	// Single-file mode: ct path/to/file.py
	if !info.IsDir() {
		if f := scanFile(abs, filepath.Dir(abs), lp, opts); f != nil {
			proj.Files = append(proj.Files, f)
		}
		return proj, nil
	}

	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(abs, path)
		if rerr != nil {
			return rerr
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if defaultSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			if ign != nil && ign.MatchesPath(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if ign != nil && ign.MatchesPath(rel) {
			return nil
		}
		if f := scanFile(path, abs, lp, opts); f != nil {
			proj.Files = append(proj.Files, f)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(proj.Files, func(i, j int) bool { return proj.Files[i].Path < proj.Files[j].Path })
	return proj, nil
}

func scanFile(path, root string, lp LangProvider, opts ScanOptions) *File {
	var parser LangParser
	if opts.Lang != "" {
		parser = lp.ByName(opts.Lang)
	} else {
		parser = lp.ByExt(strings.ToLower(filepath.Ext(path)))
	}
	if parser == nil {
		return nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil
	}
	syms, err := parser.Parse(rel, src, ParseOptions{IncludeVars: opts.IncludeVars})
	if err != nil || len(syms) == 0 {
		return nil
	}
	return &File{Path: filepath.ToSlash(rel), Lang: parser.Name(), Symbols: syms}
}
