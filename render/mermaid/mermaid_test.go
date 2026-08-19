package mermaid_test

import (
	"strings"
	"testing"

	"codetree/render/fixture"
	"codetree/render/mermaid"
)

func TestRender(t *testing.T) {
	out := mermaid.Render(fixture.Project())
	for _, want := range []string{
		"classDiagram",
		"class Animal {",
		"speak(self)",
		"class Dog {",
		"Animal <|-- Dog",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mermaid output missing %q:\n%s", want, out)
		}
	}
}
