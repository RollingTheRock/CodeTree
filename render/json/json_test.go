package json_test

import (
	"flag"
	"os"
	"testing"

	"github.com/RollingTheRock/CodeTree/render/fixture"
	"github.com/RollingTheRock/CodeTree/render/json"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderGolden(t *testing.T) {
	got, err := json.Render(fixture.Project(), json.Options{})
	if err != nil {
		t.Fatal(err)
	}
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
		t.Errorf("json render mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
