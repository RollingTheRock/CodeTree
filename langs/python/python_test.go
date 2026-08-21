package python

import (
	"os"
	"strings"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
)

func parseFixture(t *testing.T, includeVars bool) []*core.Symbol {
	t.Helper()
	src, err := os.ReadFile("testdata/fixture.py")
	if err != nil {
		t.Fatal(err)
	}
	syms, err := (lang{}).Parse("fixture.py", src, core.ParseOptions{IncludeVars: includeVars})
	if err != nil {
		t.Fatal(err)
	}
	return syms
}

func find(syms []*core.Symbol, name string) *core.Symbol {
	for _, s := range syms {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func names(syms []*core.Symbol) []string {
	var out []string
	for _, s := range syms {
		out = append(out, s.Name)
	}
	return out
}

func TestPythonExtraction(t *testing.T) {
	top := parseFixture(t, false)

	// top-level: classes + functions, variables filtered out
	wantTop := []string{"Animal", "Dog", "Puppy", "feed", "make_sound", "outer", "Drawable", "Color"}
	got := names(top)
	if strings.Join(got, ",") != strings.Join(wantTop, ",") {
		t.Fatalf("top-level = %v, want %v", got, wantTop)
	}

	// Protocol/ABC bases classify as interface; Enum bases as enum
	if d := find(top, "Drawable"); d.Kind != core.KindInterface {
		t.Errorf("Drawable kind = %v, want interface", d.Kind)
	}
	if c := find(top, "Color"); c.Kind != core.KindEnum {
		t.Errorf("Color kind = %v, want enum", c.Kind)
	}

	animal := find(top, "Animal")
	if animal.Kind != core.KindClass {
		t.Errorf("Animal kind = %v", animal.Kind)
	}
	if animal.Doc != "Base animal." {
		t.Errorf("Animal doc = %q", animal.Doc)
	}
	if find(animal.Children, "__init__") == nil || find(animal.Children, "speak") == nil {
		t.Errorf("Animal children = %v", names(animal.Children))
	}
	if find(animal.Children, "speak").Kind != core.KindMethod {
		t.Errorf("Animal.speak should be a method")
	}

	dog := find(top, "Dog")
	if len(dog.SuperTypes) != 1 || dog.SuperTypes[0] != "Animal" {
		t.Errorf("Dog.SuperTypes = %v", dog.SuperTypes)
	}
	if dog.Detail != "(Animal)" {
		t.Errorf("Dog.Detail = %q", dog.Detail)
	}

	// decorators land in Detail
	tricks := find(dog.Children, "tricks")
	if tricks == nil || !strings.Contains(tricks.Detail, "@property") {
		t.Errorf("tricks detail = %+v", tricks)
	}
	species := find(dog.Children, "species")
	if species == nil || !strings.Contains(species.Detail, "@staticmethod") {
		t.Errorf("species detail = %+v", species)
	}

	// nested class nested under Dog, with its own method
	puppyNested := find(dog.Children, "Puppy")
	if puppyNested == nil || puppyNested.Kind != core.KindClass {
		t.Fatalf("nested Puppy not found under Dog: %v", names(dog.Children))
	}
	if find(puppyNested.Children, "nap") == nil {
		t.Errorf("Puppy.nap missing")
	}

	// multiple inheritance, text level
	puppyTop := find(top, "Puppy")
	if strings.Join(puppyTop.SuperTypes, ",") != "Dog,Animal" {
		t.Errorf("Puppy supertypes = %v", puppyTop.SuperTypes)
	}

	// async marker
	feed := find(top, "feed")
	if !strings.Contains(feed.Detail, "async") {
		t.Errorf("feed detail = %q, want async marker", feed.Detail)
	}
	if feed.Kind != core.KindFunction {
		t.Errorf("feed kind = %v", feed.Kind)
	}

	// nested function attaches to its enclosing function
	outer := find(top, "outer")
	if find(outer.Children, "inner") == nil {
		t.Errorf("outer.inner missing, children = %v", names(outer.Children))
	}
}

func TestPythonIncludeVars(t *testing.T) {
	top := parseFixture(t, true)
	c := find(top, "MAX_ANIMALS")
	if c == nil || c.Kind != core.KindConstant {
		t.Errorf("MAX_ANIMALS = %+v, want constant", c)
	}
	v := find(top, "default_name")
	if v == nil || v.Kind != core.KindVariable {
		t.Errorf("default_name = %+v, want variable", v)
	}
}

func TestPythonFields(t *testing.T) {
	top := parseFixture(t, false)
	animal := find(top, "Animal")

	fields := map[string]core.Field{}
	for _, f := range animal.Fields {
		fields[f.Name] = f
	}
	// class attributes
	if f, ok := fields["kingdom"]; !ok || !f.ClassVar || f.Type != "str" {
		t.Errorf("kingdom field = %+v", fields["kingdom"])
	}
	if f, ok := fields["count"]; !ok || !f.ClassVar || f.Type != "int" {
		t.Errorf("count field = %+v", fields["count"])
	}
	// instance attributes from __init__
	if f, ok := fields["name"]; !ok || f.ClassVar {
		t.Errorf("name field = %+v, want instance attr", fields["name"])
	}
	if f, ok := fields["legs"]; !ok || f.ClassVar || f.Type != "int" {
		t.Errorf("legs field = %+v", fields["legs"])
	}
	// class-body assignments must not leak into children symbols
	for _, c := range animal.Children {
		if c.Name == "kingdom" || c.Name == "count" {
			t.Errorf("class attr %s leaked into children", c.Name)
		}
	}
}

func TestPythonLineNumbers(t *testing.T) {
	top := parseFixture(t, false)
	if find(top, "Animal").Line != 9 {
		t.Errorf("Animal line = %d, want 9", find(top, "Animal").Line)
	}
}
