package java

import (
	"os"
	"strings"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
)

func parseFixture(t *testing.T) []*core.Symbol {
	t.Helper()
	src, err := os.ReadFile("testdata/fixture.java")
	if err != nil {
		t.Fatal(err)
	}
	syms, err := (lang{}).Parse("fixture.java", src, core.ParseOptions{})
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

func TestJavaExtraction(t *testing.T) {
	top := parseFixture(t)

	wantTop := []string{"Drawable", "Named", "Entity", "Animal", "Dog", "Color", "Point"}
	if strings.Join(names(top), ",") != strings.Join(wantTop, ",") {
		t.Fatalf("top-level = %v", names(top))
	}

	// interface extends interface → SuperTypes (inheritance edge)
	ent := find(top, "Entity")
	if ent.Kind != core.KindInterface {
		t.Errorf("Entity kind = %v", ent.Kind)
	}
	if strings.Join(ent.SuperTypes, ",") != "Drawable,Named" {
		t.Errorf("Entity.SuperTypes = %v", ent.SuperTypes)
	}

	// class implements interface → Implements, not SuperTypes
	animal := find(top, "Animal")
	if animal.Kind != core.KindClass {
		t.Errorf("Animal kind = %v", animal.Kind)
	}
	if strings.Join(animal.Implements, ",") != "Entity" {
		t.Errorf("Animal.Implements = %v", animal.Implements)
	}
	if len(animal.SuperTypes) != 0 {
		t.Errorf("Animal.SuperTypes = %v", animal.SuperTypes)
	}

	// fields with types
	fmap := map[string]string{}
	for _, f := range animal.Fields {
		fmap[f.Name] = f.Type
	}
	if fmap["name"] != "String" || fmap["legs"] != "int" {
		t.Errorf("Animal fields = %v", fmap)
	}

	// constructor annotated, nested class under Animal
	if find(animal.Children, "Animal") == nil {
		t.Errorf("constructor missing, children = %v", names(animal.Children))
	}
	if c := find(animal.Children, "Animal"); c != nil && !strings.HasPrefix(c.Detail, "new ") {
		t.Errorf("ctor detail = %q", c.Detail)
	}
	if find(animal.Children, "Collar") == nil {
		t.Error("nested class Collar missing")
	}

	// extends + generic implements stripped to bare name
	dog := find(top, "Dog")
	if strings.Join(dog.SuperTypes, ",") != "Animal" {
		t.Errorf("Dog.SuperTypes = %v", dog.SuperTypes)
	}
	if strings.Join(dog.Implements, ",") != "Comparable" {
		t.Errorf("Dog.Implements = %v", dog.Implements)
	}
	// @Override annotation lands in Detail
	speak := find(dog.Children, "speak")
	if speak == nil || !strings.Contains(speak.Detail, "@Override") {
		t.Errorf("speak detail = %+v", speak)
	}
	if create := find(dog.Children, "create"); create == nil || create.Kind != core.KindMethod {
		t.Errorf("create = %+v", create)
	}

	// enum + record
	if c := find(top, "Color"); c.Kind != core.KindEnum {
		t.Errorf("Color kind = %v", c.Kind)
	}
	if p := find(top, "Point"); p.Kind != core.KindClass || !strings.HasPrefix(p.Detail, "record ") {
		t.Errorf("Point = %+v", p)
	}
}
