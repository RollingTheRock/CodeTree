package lsp

import (
	"strings"
	"testing"

	"codetree/core"
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
						{Name: "tricks"},                    // untyped → fillable
						{Name: "name", Type: "str"},         // annotated → static wins
					}},
			}},
		},
	}
}

func TestApplyBaseBinding(t *testing.T) {
	p := projFixture()
	d := Apply(p, []BaseBinding{{
		File: "models/dog.py", ClassLine: 3, BaseIndex: 0,
		TargetFile: "models/base.py", TargetLine: 5,
	}}, nil, nil)

	dog := p.AllSymbols()[1]
	if len(dog.BaseRefs) != 1 || dog.BaseRefs[0].File != "models/base.py" || dog.BaseRefs[0].Line != 5 {
		t.Fatalf("BaseRefs = %+v", dog.BaseRefs)
	}
	if len(d.ReboundBases) != 1 || !strings.Contains(d.ReboundBases[0], "Dog.Animal") {
		t.Errorf("diff = %+v", d)
	}

	// idempotent: applying again produces no new diff
	d2 := Apply(p, []BaseBinding{{
		File: "models/dog.py", ClassLine: 3, BaseIndex: 0,
		TargetFile: "models/base.py", TargetLine: 5,
	}}, nil, nil)
	if !d2.Empty() {
		t.Errorf("re-apply should be empty, got %v", d2)
	}
}

func TestApplyBaseBindingMisses(t *testing.T) {
	p := projFixture()
	d := Apply(p, []BaseBinding{
		{File: "models/dog.py", ClassLine: 999, BaseIndex: 0, TargetFile: "x", TargetLine: 1}, // no class at line 999
		{File: "models/dog.py", ClassLine: 3, BaseIndex: 5, TargetFile: "x", TargetLine: 1},    // index out of range
	}, nil, nil)
	if !d.Empty() {
		t.Errorf("expected empty diff, got %v", d)
	}
	if len(p.AllSymbols()[1].BaseRefs) != 0 {
		t.Error("BaseRefs should stay empty on misses")
	}
}

func TestApplyFieldTypes(t *testing.T) {
	p := projFixture()
	d := Apply(p, nil, []FieldType{
		{File: "models/dog.py", Line: 10, Col: 4, Type: "int"},  // no field at this pos
		{File: "models/dog.py", Line: 0, Col: 0, Type: "list"},  // tricks has 0/0 → this fills it
		{File: "models/dog.py", Line: 0, Col: 0, Type: "Other"}, // already filled → no double
	}, nil)

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
	d := Apply(p, nil, []FieldType{{File: "models/dog.py", Line: 7, Col: 8, Type: "bytes"}}, nil)
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
	d := Apply(p, nil, nil, added)

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
