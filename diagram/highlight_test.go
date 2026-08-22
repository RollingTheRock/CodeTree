package diagram_test

import (
	"testing"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/diagram"
)

// TestSetHighlightMatchesRebuild proves the cheap re-highlight path produces
// exactly the same text as a full Build with the highlight set — including
// moving the highlight back and forth (restoring the original styles).
func TestSetHighlightMatchesRebuild(t *testing.T) {
	p := proj(
		withMembers(cls("A"), []core.Field{{Name: "x", Type: "int"}}, "m1"),
		cls("B", "A"),
		cls("C", "B"),
		cls("Ext", "Missing"),
	)
	opts := diagram.DefaultOptions()
	opts.Color = true
	opts.Members = true

	base, err := diagram.Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	plain := base.Text

	for _, name := range []string{"A", "B", "C", "Ext"} {
		wantOpts := opts
		wantOpts.Highlight = name
		want, err := diagram.Build(p, wantOpts)
		if err != nil {
			t.Fatal(err)
		}
		base.SetHighlight(name)
		if base.Text != want.Text {
			t.Errorf("SetHighlight(%q) differs from rebuild:\n--- set ---\n%s\n--- want ---\n%s", name, base.Text, want.Text)
		}
	}

	// moving the highlight back to empty restores the plain rendering
	base.SetHighlight("")
	if base.Text != plain {
		t.Error("SetHighlight(\"\") should restore the unhighlighted text")
	}
}

// TestSetHighlightDuplicateNames: two classes sharing a name are both
// highlighted (same as Build), and both restored on move.
func TestSetHighlightDuplicateNames(t *testing.T) {
	p := proj(cls("Dup"), cls("Dup"), cls("Other"))
	opts := diagram.DefaultOptions()
	opts.Color = true

	d, err := diagram.Build(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	d.SetHighlight("Dup")
	wantOpts := opts
	wantOpts.Highlight = "Dup"
	want, err := diagram.Build(p, wantOpts)
	if err != nil {
		t.Fatal(err)
	}
	if d.Text != want.Text {
		t.Error("SetHighlight with duplicate names differs from rebuild")
	}
}
