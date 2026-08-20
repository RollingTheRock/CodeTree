package cpp

import (
	"os"
	"strings"
	"testing"

	"codetree/core"
)

func parseFixture(t *testing.T) []*core.Symbol {
	t.Helper()
	src, err := os.ReadFile("testdata/fixture.cpp")
	if err != nil {
		t.Fatal(err)
	}
	syms, err := (lang{}).Parse("fixture.cpp", src, core.ParseOptions{})
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

func TestCppExtraction(t *testing.T) {
	top := parseFixture(t)

	// namespace contents are flattened to top level; free function too
	for _, want := range []string{"Animal", "Runnable", "Dog", "Box", "Point", "Color", "free_func"} {
		if find(top, want) == nil {
			t.Fatalf("%s missing, top = %v", want, names(top))
		}
	}

	// multiple public inheritance, access specifiers stripped
	dog := find(top, "Dog")
	if dog.Kind != core.KindClass {
		t.Errorf("Dog kind = %v", dog.Kind)
	}
	if strings.Join(dog.SuperTypes, ",") != "Animal,Runnable" {
		t.Errorf("Dog.SuperTypes = %v", dog.SuperTypes)
	}

	// ctor/dtor/methods attached; namespace-qualified out-of-class
	// definition (void zoo::Dog::bark()) attaches by bare class name
	for _, m := range []string{"Dog", "~Dog", "bark"} {
		if find(dog.Children, m) == nil {
			t.Errorf("Dog child %s missing, children = %v", m, names(dog.Children))
		}
	}

	// template class: name without parameters, template base stripped to name
	box := find(top, "Box")
	if box.Kind != core.KindClass {
		t.Errorf("Box kind = %v", box.Kind)
	}
	if strings.Join(box.SuperTypes, ",") != "Container" {
		t.Errorf("Box.SuperTypes = %v", box.SuperTypes)
	}
	if len(box.Fields) != 1 || box.Fields[0].Name != "value" || box.Fields[0].Type != "T" {
		t.Errorf("Box fields = %+v", box.Fields)
	}

	// struct → KindStruct
	if p := find(top, "Point"); p.Kind != core.KindStruct {
		t.Errorf("Point kind = %v", p.Kind)
	}

	// enum class → KindEnum
	if c := find(top, "Color"); c.Kind != core.KindEnum {
		t.Errorf("Color kind = %v", c.Kind)
	}

	// free function
	if f := find(top, "free_func"); f.Kind != core.KindFunction || f.Detail != "(int a)" {
		t.Errorf("free_func = %+v", f)
	}
}

func TestCppOutOfClassMethod(t *testing.T) {
	src := []byte("class Dog {\npublic:\n  void bark();\n};\n\nvoid Dog::bark() {}\n")
	top, err := (lang{}).Parse("t.cpp", src, core.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dog := find(top, "Dog")
	if dog == nil {
		t.Fatal("Dog missing")
	}
	var barkDecl, barkDef int
	for _, c := range dog.Children {
		if c.Name == "bark" {
			if c.Kind == core.KindMethod {
				barkDef++
			}
		}
	}
	_ = barkDecl
	if barkDef < 1 {
		t.Errorf("out-of-class bark() not attached, children = %v", names(dog.Children))
	}
}
