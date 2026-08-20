package diagram

import (
	"strings"
	"testing"
)

func TestExtractTypeRefs(t *testing.T) {
	cases := []struct {
		in   string
		want string // comma-joined
	}{
		{"Insect", "Insect"},
		{"list[Insect]", "Insect"},
		{"dict[str, Foo]", "Foo"},
		{"dict[str, Insect]", "Insect"},
		{"Map<String, Foo>", "Foo"},
		{"Optional[Foo]", "Foo"},
		{"Foo | None", "Foo"},
		{"Foo[]", "Foo"},
		{"[]Foo", "Foo"},
		{"[]*Foo", "Foo"},
		{"*Foo", "Foo"},
		{"List<Foo>", "Foo"},
		{"map[string]*Foo", "Foo"},
		{"map[string]Foo", "Foo"},
		{"std::vector<Foo>", "Foo"},
		{"models.Insect", "Insect"},
		{"zoo::Insect", "Insect"},
		{"tuple[Foo, Bar]", "Foo,Bar"},
		{"Callable[[Insect], Foo]", "Insect,Foo"},
		// filtered out
		{"str", ""},
		{"int", ""},
		{"list[int]", ""},
		{"x", ""},     // lowercase primitive-style
		{"myVar", ""}, // lowercase
		{"T", ""},     // hmm: template param T is uppercase — not filtered by rule
		{"", ""},
	}
	for _, c := range cases {
		got := strings.Join(ExtractTypeRefs(c.in), ",")
		want := c.want
		if c.in == "T" {
			// T is uppercase and not builtin: passes the name filter;
			// it only dies at byName matching. Document actual behavior.
			want = "T"
		}
		if got != want {
			t.Errorf("ExtractTypeRefs(%q) = %q, want %q", c.in, got, want)
		}
	}
}
