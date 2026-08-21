package jsts

import (
	"strings"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
)

const tsFixture = `interface Shape {
  area(): number;
}

enum Color {
  Red,
  Green,
}

class Animal {
  name: string;
}

class Dog extends Animal implements Shape {
  tricks: number;
  constructor(name: string) {
    super();
  }
  area(): number {
    return 0;
  }
  speak(): void {}
}

function freeFn(a: number) {}
`

const jsFixture = `class Animal {
}

class Dog extends Animal {
  speak() {}
}
`

func parseTS(t *testing.T) []*core.Symbol {
	t.Helper()
	syms, err := (tsLang{}).Parse("shapes.ts", []byte(tsFixture), core.ParseOptions{})
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

func TestTypeScriptExtraction(t *testing.T) {
	top := parseTS(t)

	if s := find(top, "Shape"); s == nil || s.Kind != core.KindInterface {
		t.Errorf("Shape = %+v", s)
	}
	if c := find(top, "Color"); c == nil || c.Kind != core.KindEnum {
		t.Errorf("Color = %+v", c)
	}

	dog := find(top, "Dog")
	if dog == nil || dog.Kind != core.KindClass {
		t.Fatalf("Dog = %+v", dog)
	}
	if strings.Join(dog.SuperTypes, ",") != "Animal" {
		t.Errorf("Dog.SuperTypes = %v", dog.SuperTypes)
	}
	if len(dog.BasePos) != 1 || dog.BasePos[0].Line != 14 {
		t.Errorf("Dog.BasePos = %+v", dog.BasePos)
	}
	if strings.Join(dog.Implements, ",") != "Shape" {
		t.Errorf("Dog.Implements = %v", dog.Implements)
	}
	if len(dog.Fields) != 1 || dog.Fields[0].Name != "tricks" || dog.Fields[0].Type != "number" {
		t.Errorf("Dog fields = %+v", dog.Fields)
	}
	for _, m := range []string{"constructor", "area", "speak"} {
		if find(dog.Children, m) == nil {
			t.Errorf("Dog.%s missing", m)
		}
	}
	if f := find(top, "freeFn"); f == nil || f.Kind != core.KindFunction {
		t.Errorf("freeFn = %+v", f)
	}
}

func TestJavaScriptExtraction(t *testing.T) {
	syms, err := (jsLang{}).Parse("a.js", []byte(jsFixture), core.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dog := find(syms, "Dog")
	if dog == nil || strings.Join(dog.SuperTypes, ",") != "Animal" {
		t.Fatalf("Dog = %+v", dog)
	}
	if find(dog.Children, "speak") == nil {
		t.Error("Dog.speak missing")
	}
}
