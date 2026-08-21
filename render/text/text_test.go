package text_test

import (
	"flag"
	"os"
	"testing"

	"github.com/RollingTheRock/CodeTree/render/fixture"
	"github.com/RollingTheRock/CodeTree/render/text"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderGolden(t *testing.T) {
	got := text.Render(fixture.Project(), text.Options{})
	golden := "testdata/tree.golden"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if got != string(want) {
		t.Errorf("text render mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderDepth(t *testing.T) {
	p := fixture.Project()

	d1 := text.Render(p, text.Options{Depth: 1})
	if want := "myproject/\n└── models/\n    ├── animal.py\n    └── zoo.py\n"; d1 != want {
		t.Errorf("depth 1:\n%s\nwant:\n%s", d1, want)
	}

	d2 := text.Render(p, text.Options{Depth: 2})
	if want := "myproject/\n└── models/\n    ├── animal.py\n    │   ├── Animal (class)\n    │   └── Dog(Animal) (class)\n    └── zoo.py\n        └── make_sound(a) (func)\n"; d2 != want {
		t.Errorf("depth 2:\n%s\nwant:\n%s", d2, want)
	}
}
