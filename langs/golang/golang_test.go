package golang

import (
	"os"
	"strings"
	"testing"

	"codetree/core"
)

func parseFixture(t *testing.T, includeVars bool) []*core.Symbol {
	t.Helper()
	src, err := os.ReadFile("testdata/fixture.go")
	if err != nil {
		t.Fatal(err)
	}
	syms, err := (lang{}).Parse("fixture.go", src, core.ParseOptions{IncludeVars: includeVars})
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

func TestGoExtraction(t *testing.T) {
	top := parseFixture(t, false)

	sp := find(top, "Speaker")
	if sp == nil || sp.Kind != core.KindInterface {
		t.Fatalf("Speaker = %+v, want interface", sp)
	}

	an := find(top, "Animal")
	if an == nil || an.Kind != core.KindStruct {
		t.Fatalf("Animal = %+v, want struct", an)
	}
	if an.Doc != "Animal is a base struct." {
		t.Errorf("Animal doc = %q", an.Doc)
	}

	// method attached to its receiver type
	speak := find(an.Children, "Speak")
	if speak == nil || speak.Kind != core.KindMethod {
		t.Fatalf("Animal.Speak = %+v, want method child", speak)
	}
	if speak.Detail != "() string" {
		t.Errorf("Speak detail = %q", speak.Detail)
	}

	// plain function with signature
	newAnimal := find(top, "NewAnimal")
	if newAnimal == nil || newAnimal.Kind != core.KindFunction {
		t.Fatalf("NewAnimal = %+v", newAnimal)
	}
	if newAnimal.Detail != "(name string) *Animal" {
		t.Errorf("NewAnimal detail = %q", newAnimal.Detail)
	}

	// vars/consts filtered by default
	if find(top, "MaxAnimals") != nil || find(top, "defaultName") != nil {
		t.Error("vars/consts should be filtered without IncludeVars")
	}
}

func TestGoIncludeVars(t *testing.T) {
	top := parseFixture(t, true)
	if c := find(top, "MaxAnimals"); c == nil || c.Kind != core.KindConstant {
		t.Errorf("MaxAnimals = %+v, want constant", c)
	}
	if v := find(top, "defaultName"); v == nil || v.Kind != core.KindVariable {
		t.Errorf("defaultName = %+v, want variable", v)
	}
}

func TestGoKinds(t *testing.T) {
	top := parseFixture(t, false)
	var kinds []string
	for _, s := range top {
		kinds = append(kinds, s.Name+":"+s.Kind.String())
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, "Speaker:interface") || !strings.Contains(joined, "Animal:struct") {
		t.Errorf("kinds = %s", joined)
	}
}
