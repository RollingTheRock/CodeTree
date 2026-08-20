package diagram

import (
	"strings"
	"unicode"
)

// builtinNames are scalar/container type names that never become graph
// nodes. Lowercase-starting names are also filtered (primitive-style).
var builtinNames = map[string]bool{
	"str": true, "string": true, "bool": true, "byte": true, "rune": true,
	"char": true, "void": true, "int": true, "long": true, "short": true,
	"unsigned": true, "double": true, "float": true, "size_t": true,
	"None": true, "nil": true, "true": true, "false": true,
	"list": true, "dict": true, "set": true, "tuple": true, "frozenset": true,
	"map": true, "slice": true, "array": true, "object": true,
	"List": true, "Dict": true, "Set": true, "Tuple": true, "Map": true,
	"Optional": true, "Union": true, "Sequence": true, "Iterable": true,
	"Callable": true, "Type": true, "ClassVar": true,
	"Any": true, "Object": true, "Self": true,
	"String": true, "Integer": true, "Boolean": true, "Double": true, "Long": true,
	"int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "complex64": true, "complex128": true,
	"error": true, "any": true,
}

// ExtractTypeRefs pulls candidate referenced type names out of a field type
// expression. It unwraps containers/generics/optionals/pointers/slices and
// drops builtins, primitives and lowercase (primitive-style) names:
//
//	list[Insect] → [Insect]          dict[str, Foo] → [Foo]
//	Map<String, Foo> → [Foo]         Optional[Foo] / Foo | None → [Foo]
//	Foo[] / []Foo / []*Foo / *Foo → [Foo]
//	map[string]*Foo → [Foo]          std::vector<Foo> → [Foo]
//
// Names are returned unqualified (last component) for byName matching.
func ExtractTypeRefs(typ string) []string {
	var out []string
	seen := map[string]bool{}
	var rec func(s string)
	rec = func(s string) {
		s = strings.TrimSpace(s)
		// leading Go pointer/slice markers
		for {
			switch {
			case strings.HasPrefix(s, "*"):
				s = strings.TrimSpace(s[1:])
			case strings.HasPrefix(s, "[]"):
				s = strings.TrimSpace(s[2:])
			default:
				goto leadDone
			}
		}
	leadDone:
		// trailing Java/C++ array markers
		for strings.HasSuffix(s, "[]") {
			s = strings.TrimSpace(s[:len(s)-2])
		}
		if s == "" {
			return
		}
		// top-level union: Foo | None
		if parts := splitTopLevel(s, '|'); len(parts) > 1 {
			for _, p := range parts {
				rec(p)
			}
			return
		}
		// generics / subscripts: name[...] or name<...>
		if i := strings.IndexAny(s, "[<"); i >= 0 {
			rec(s[:i]) // container name itself (usually filtered as builtin)
			open := s[i]
			close := byte(']')
			if open == '<' {
				close = '>'
			}
			inner, trailing := splitBracket(s[i+1:], close)
			for _, p := range splitTopLevel(inner, ',') {
				rec(p)
			}
			if trailing != "" { // Go map value: map[string]Foo
				rec(trailing)
			}
			return
		}
		// dotted / namespaced: last component
		if i := strings.LastIndex(s, "::"); i >= 0 {
			s = s[i+2:]
		}
		if i := strings.LastIndex(s, "."); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || builtinNames[s] {
			return
		}
		if r := []rune(s)[0]; unicode.IsLower(r) || !unicode.IsLetter(r) {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	rec(typ)
	return out
}

// splitTopLevel splits s on sep at bracket depth zero.
func splitTopLevel(s string, sep byte) []string {
	depth := 0
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '<', '(':
			depth++
		case ']', '>', ')':
			depth--
		default:
			if s[i] == sep && depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// splitBracket splits at the matching close bracket: returns the inner
// content and whatever follows it.
func splitBracket(s string, close byte) (inner, trailing string) {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', '<':
			depth++
		case ']', '>':
			depth--
			if depth == 0 {
				return s[:i], strings.TrimSpace(s[i+1:])
			}
		}
	}
	return s, ""
}
