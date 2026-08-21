package lsp

import (
	"strings"
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
)

func projFixture() *core.Project {
	return &core.Project{
		Root: "/p",
		Files: []*core.File{
			{Path: "models/base.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Animal", Kind: core.KindClass, File: "models/base.py", Line: 5},
			}},
			{Path: "models/dog.py", Lang: "python", Symbols: []*core.Symbol{
				{Name: "Dog", Kind: core.KindClass, File: "models/dog.py", Line: 3,
					SuperTypes: []string{"Animal"},
					BasePos:    []core.Pos{{Line: 3, Col: 10}},
					Fields: []core.Field{
						{Name: "tricks"},            // untyped → fillable
						{Name: "name", Type: "str"}, // annotated → static wins
					}},
			}},
		},
	}
}

func TestApplyBaseBinding(t *testing.T) {
	p := projFixture()
	d := Apply(p, Corrections{Bases: []BaseBinding{{
		File: "models/dog.py", ClassLine: 3, BaseIndex: 0,
		TargetFile: "models/base.py", TargetLine: 5,
	}}})

	dog := p.AllSymbols()[1]
	if len(dog.BaseRefs) != 1 || dog.BaseRefs[0].File != "models/base.py" || dog.BaseRefs[0].Line != 5 {
		t.Fatalf("BaseRefs = %+v", dog.BaseRefs)
	}
	if len(d.ReboundBases) != 1 || !strings.Contains(d.ReboundBases[0], "Dog.Animal") {
		t.Errorf("diff = %+v", d)
	}

	// idempotent: applying again produces no new diff
	d2 := Apply(p, Corrections{Bases: []BaseBinding{{
		File: "models/dog.py", ClassLine: 3, BaseIndex: 0,
		TargetFile: "models/base.py", TargetLine: 5,
	}}})
	if !d2.Empty() {
		t.Errorf("re-apply should be empty, got %v", d2)
	}
}

func TestApplyBaseBindingMisses(t *testing.T) {
	p := projFixture()
	d := Apply(p, Corrections{Bases: []BaseBinding{
		{File: "models/dog.py", ClassLine: 999, BaseIndex: 0, TargetFile: "x", TargetLine: 1}, // no class at line 999
		{File: "models/dog.py", ClassLine: 3, BaseIndex: 5, TargetFile: "x", TargetLine: 1},   // index out of range
	}})
	if !d.Empty() {
		t.Errorf("expected empty diff, got %v", d)
	}
	if len(p.AllSymbols()[1].BaseRefs) != 0 {
		t.Error("BaseRefs should stay empty on misses")
	}
}

func TestApplyFieldTypes(t *testing.T) {
	p := projFixture()
	d := Apply(p, Corrections{Fields: []FieldType{
		{File: "models/dog.py", Line: 10, Col: 4, Type: "int"},  // no field at this pos
		{File: "models/dog.py", Line: 0, Col: 0, Type: "list"},  // tricks has 0/0 → this fills it
		{File: "models/dog.py", Line: 0, Col: 0, Type: "Other"}, // already filled → no double
	}})

	dog := p.AllSymbols()[1]
	if dog.Fields[0].Type != "list" {
		t.Errorf("tricks.Type = %q, want list", dog.Fields[0].Type)
	}
	if dog.Fields[1].Type != "str" {
		t.Errorf("name.Type = %q, want str (static wins)", dog.Fields[1].Type)
	}
	if len(d.FilledFields) != 1 {
		t.Errorf("FilledFields = %v", d.FilledFields)
	}
}

func TestApplyFieldTypeStaticWins(t *testing.T) {
	p := projFixture()
	// annotate position of the annotated field: give it a position first
	dog := p.AllSymbols()[1]
	dog.Fields[1].Line, dog.Fields[1].Col = 7, 8
	d := Apply(p, Corrections{Fields: []FieldType{{File: "models/dog.py", Line: 7, Col: 8, Type: "bytes"}}})
	if dog.Fields[1].Type != "str" {
		t.Errorf("annotated field overwritten: %q", dog.Fields[1].Type)
	}
	if !d.Empty() {
		t.Errorf("diff = %v", d)
	}
}

func TestApplyAddedClasses(t *testing.T) {
	p := projFixture()
	added := []*core.Symbol{
		{Name: "Color", Kind: core.KindEnum, File: "models/base.py", Line: 12},  // new
		{Name: "Animal", Kind: core.KindClass, File: "models/base.py", Line: 5}, // dup → skip
	}
	d := Apply(p, Corrections{Added: added})

	if len(d.AddedClasses) != 1 || !strings.Contains(d.AddedClasses[0], "Color") {
		t.Fatalf("AddedClasses = %v", d.AddedClasses)
	}
	var color *core.Symbol
	for _, s := range p.AllSymbols() {
		if s.Name == "Color" {
			color = s
		}
	}
	if color == nil || color.Source != "lsp" {
		t.Fatalf("Color = %+v", color)
	}
	// only one Animal survives
	n := 0
	for _, s := range p.AllSymbols() {
		if s.Name == "Animal" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("Animal count = %d", n)
	}
}

func TestApplyImplBindings(t *testing.T) {
	p := &core.Project{
		Root: "/p",
		Files: []*core.File{{Path: "shape.go", Lang: "go", Symbols: []*core.Symbol{
			{Name: "Speaker", Kind: core.KindInterface, File: "shape.go", Line: 3, Col: 5},
			{Name: "Dog", Kind: core.KindStruct, File: "shape.go", Line: 10, Col: 5},
			{Name: "Cat", Kind: core.KindStruct, File: "shape.go", Line: 15, Col: 5},
		}}},
	}
	d := Apply(p, Corrections{Impls: []ImplBinding{
		{InterfaceFile: "shape.go", InterfaceLine: 3, ImplFile: "shape.go", ImplLine: 10},
		{InterfaceFile: "shape.go", InterfaceLine: 3, ImplFile: "shape.go", ImplLine: 15},
		{InterfaceFile: "shape.go", InterfaceLine: 3, ImplFile: "shape.go", ImplLine: 10}, // dup
		{InterfaceFile: "shape.go", InterfaceLine: 3, ImplFile: "shape.go", ImplLine: 99}, // no symbol there
	}})
	syms := p.AllSymbols()
	dog, cat := syms[1], syms[2]
	if len(dog.Implements) != 1 || dog.Implements[0] != "Speaker" {
		t.Errorf("Dog.Implements = %v", dog.Implements)
	}
	if len(cat.Implements) != 1 || cat.Implements[0] != "Speaker" {
		t.Errorf("Cat.Implements = %v", cat.Implements)
	}
	if len(d.AddedImpls) != 2 {
		t.Errorf("AddedImpls = %v (dup and miss should be excluded)", d.AddedImpls)
	}
}
