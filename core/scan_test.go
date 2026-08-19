package core_test

import (
	"os"
	"path/filepath"
	"testing"

	"codetree/core"
	"codetree/langs"
	_ "codetree/langs/golang"
	_ "codetree/langs/python"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanMixedProject(t *testing.T) {
	root := t.TempDir()
	write(t, root, "models/animal.py", "class Animal:\n    def speak(self):\n        pass\n")
	write(t, root, "main.go", "package main\n\nfunc main() {}\n")
	write(t, root, "README.md", "# not code\n")
	write(t, root, "vendor/skip.py", "class Skipped:\n    pass\n")
	write(t, root, ".gitignore", "ignored/\n")
	write(t, root, "ignored/hidden.py", "class Hidden:\n    pass\n")

	proj, err := core.Scan(root, langs.Registry{}, core.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var paths []string
	for _, f := range proj.Files {
		paths = append(paths, f.Path)
	}
	want := map[string]string{
		"main.go":          "go",
		"models/animal.py": "python",
	}
	if len(paths) != len(want) {
		t.Fatalf("scanned files = %v", paths)
	}
	for _, f := range proj.Files {
		if want[f.Path] != f.Lang {
			t.Errorf("file %s lang = %s", f.Path, f.Lang)
		}
	}
}

func TestScanForcedLang(t *testing.T) {
	root := t.TempDir()
	write(t, root, "main.go", "package main\n\nfunc main() {}\n")
	write(t, root, "a.py", "def f():\n    pass\n")

	proj, err := core.Scan(root, langs.Registry{}, core.ScanOptions{Lang: "python"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range proj.Files {
		if f.Lang != "python" {
			t.Errorf("forced lang: got %s file %s", f.Lang, f.Path)
		}
	}
	if len(proj.Files) != 1 {
		t.Fatalf("expected only python files, got %d", len(proj.Files))
	}
}
