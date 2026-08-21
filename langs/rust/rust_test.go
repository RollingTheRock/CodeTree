package rust

import (
	"os"
	"strings"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
)

const fixture = `
trait Speak {
    fn speak(&self);
}

trait Named {
    fn name(&self) -> &str;
}

struct Dog {
    name: String,
    tricks: u32,
}

enum Color {
    Red,
    Green,
}

impl Speak for Dog {
    fn speak(&self) {}
}

impl Dog {
    fn new(name: String) -> Dog {
        Dog { name, tricks: 0 }
    }
}

fn free_fn(x: i32) {}
`

func parseFixture(t *testing.T) []*core.Symbol {
	t.Helper()
	syms, err := (lang{}).Parse("lib.rs", []byte(fixture), core.ParseOptions{})
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

func TestRustExtraction(t *testing.T) {
	top := parseFixture(t)

	speak := find(top, "Speak")
	if speak == nil || speak.Kind != core.KindInterface {
		t.Fatalf("Speak = %+v", speak)
	}
	if m := find(speak.Children, "speak"); m == nil || m.Kind != core.KindMethod {
		t.Errorf("Speak.speak = %+v", m)
	}

	dog := find(top, "Dog")
	if dog == nil || dog.Kind != core.KindStruct {
		t.Fatalf("Dog = %+v", dog)
	}
	// fields
	if len(dog.Fields) != 2 || dog.Fields[0].Name != "name" || dog.Fields[0].Type != "String" {
		t.Errorf("Dog fields = %+v", dog.Fields)
	}
	// impl Speak for Dog → Implements
	if len(dog.Implements) != 1 || dog.Implements[0] != "Speak" {
		t.Errorf("Dog.Implements = %v", dog.Implements)
	}
	if len(dog.ImplPos) != 1 || dog.ImplPos[0].Line != 20 {
		t.Errorf("Dog.ImplPos = %+v", dog.ImplPos)
	}
	// impl Dog { fn new } → method
	if m := find(dog.Children, "new"); m == nil || m.Kind != core.KindMethod {
		t.Errorf("Dog.new = %+v", m)
	}

	if c := find(top, "Color"); c == nil || c.Kind != core.KindEnum {
		t.Errorf("Color = %+v", c)
	}
	if f := find(top, "free_fn"); f == nil || f.Kind != core.KindFunction || f.Detail != "(x: i32)" {
		t.Errorf("free_fn = %+v", f)
	}
	if !strings.Contains(strings.Join(names(top), ","), "Named") {
		t.Errorf("top = %v", names(top))
	}
}

func names(syms []*core.Symbol) []string {
	var out []string
	for _, s := range syms {
		out = append(out, s.Name)
	}
	return out
}

var _ = os.ReadFile
