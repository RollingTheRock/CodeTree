// Package langs is the language registry. Language implementations
// (langs/python, langs/golang) register themselves from their init().
package langs

import (
	"strings"

	"codetree/core"
)

// Language is a pluggable source parser.
type Language interface {
	core.LangParser
	// Extensions returns handled file extensions, each with leading dot
	// (e.g. ".py"). Used for auto-detection during scans.
	Extensions() []string
}

var (
	byExt  = map[string]Language{}
	byName = map[string]Language{}
)

// Register adds a language to the registry. Called from init() of each
// language package.
func Register(l Language) {
	byName[strings.ToLower(l.Name())] = l
	for _, ext := range l.Extensions() {
		byExt[strings.ToLower(ext)] = l
	}
}

// ByExt implements core.LangProvider.
func ByExt(ext string) core.LangParser {
	return byExt[strings.ToLower(ext)]
}

// ByName implements core.LangProvider.
func ByName(name string) core.LangParser {
	return byName[strings.ToLower(name)]
}

// Registry is a core.LangProvider backed by the global registry.
type Registry struct{}

func (Registry) ByExt(ext string) core.LangParser   { return ByExt(ext) }
func (Registry) ByName(name string) core.LangParser { return ByName(name) }

// Names returns registered language names.
func Names() []string {
	out := make([]string, 0, len(byName))
	seen := map[string]bool{}
	for _, l := range byExt {
		if !seen[l.Name()] {
			seen[l.Name()] = true
			out = append(out, l.Name())
		}
	}
	return out
}

// Extensions returns every registered file extension (with leading dot).
func Extensions() map[string]bool {
	out := make(map[string]bool, len(byExt))
	for ext := range byExt {
		out[ext] = true
	}
	return out
}
