package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/langs"
	_ "github.com/RollingTheRock/CodeTree/langs/python"
)

var testExts = map[string]bool{".py": true, ".go": true}

func TestRelevantEvent(t *testing.T) {
	root := "/proj"
	cases := []struct {
		name string
		op   fsnotify.Op
		path string
		want bool
	}{
		{"write source", fsnotify.Write, "/proj/a.py", true},
		{"create source", fsnotify.Create, "/proj/pkg/b.go", true},
		{"rename source", fsnotify.Rename, "/proj/a.py", true},
		{"remove source", fsnotify.Remove, "/proj/a.py", true},
		{"chmod only", fsnotify.Chmod, "/proj/a.py", false},
		{"non-source", fsnotify.Write, "/proj/README.md", false},
		{"skipped dir", fsnotify.Write, "/proj/__pycache__/a.py", false},
		{"vendor", fsnotify.Write, "/proj/vendor/x/a.py", false},
		{"dot dir", fsnotify.Write, "/proj/.git/a.py", false},
	}
	for _, c := range cases {
		ev := fsnotify.Event{Name: c.path, Op: c.op}
		if got := relevantEvent(root, ev, testExts, nil); got != c.want {
			t.Errorf("%s: relevantEvent = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWatchProjectDebounce(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	root := t.TempDir()
	writeT(t, root, "a.py", "class A:\n    pass\n")

	ch := watchProject(root)
	if ch == nil {
		t.Skip("watcher unavailable")
	}

	// burst of writes (editor save simulation) → exactly one notification
	time.Sleep(100 * time.Millisecond)
	writeT(t, root, "a.py", "class A:\n    pass\n\nclass B:\n    pass\n")
	writeT(t, root, "b.py", "class C:\n    pass\n")

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no reload notification")
	}
	select {
	case <-ch:
		t.Fatal("burst produced more than one notification")
	case <-time.After(debounceInterval + 200*time.Millisecond):
	}
}

func TestReloadPreservesState(t *testing.T) {
	root := t.TempDir()
	writeT(t, root, "a.py", "class A:\n    pass\n")
	writeT(t, root, "b.py", "class B(A):\n    pass\n")

	proj, err := core.Scan(root, langs.Registry{}, core.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(proj)
	m.projRoot = root
	m.scanOpts = core.ScanOptions{}
	m.width, m.height = 120, 40
	m.layout()
	m.ready = true

	// simulate user state: marked a.py, scoped to a.py, focus set
	m.marked["a.py"] = true
	m.dopts.Files = []string{"a.py"}
	m.dopts.Members = true

	// external change: b.py deleted, a.py gains a class
	if err := os.Remove(filepath.Join(root, "b.py")); err != nil {
		t.Fatal(err)
	}
	writeT(t, root, "a.py", "class A:\n    pass\n\nclass A2:\n    pass\n")

	m.reload(nil)

	if !m.marked["a.py"] {
		t.Error("mark on a.py lost")
	}
	if len(m.dopts.Files) != 1 || m.dopts.Files[0] != "a.py" {
		t.Errorf("scope = %v", m.dopts.Files)
	}
	if !m.dopts.Members {
		t.Error("members flag lost")
	}
	if m.lastReload.IsZero() {
		t.Error("lastReload not set")
	}
	// picker reflects the change
	var paths []string
	for _, fe := range m.pickerFiles {
		paths = append(paths, fe.path)
	}
	if len(paths) != 1 || paths[0] != "a.py" {
		t.Errorf("picker files = %v", paths)
	}
}

func TestReloadPrunesDeletedMarks(t *testing.T) {
	root := t.TempDir()
	writeT(t, root, "a.py", "class A:\n    pass\n")
	writeT(t, root, "gone.py", "class G:\n    pass\n")

	proj, _ := core.Scan(root, langs.Registry{}, core.ScanOptions{})
	m := newModel(proj)
	m.projRoot = root
	m.marked["gone.py"] = true
	m.marked["a.py"] = true

	if err := os.Remove(filepath.Join(root, "gone.py")); err != nil {
		t.Fatal(err)
	}
	m.reload(nil)

	if m.marked["gone.py"] {
		t.Error("mark on deleted file should be pruned")
	}
	if !m.marked["a.py"] {
		t.Error("mark on surviving file lost")
	}
}

// Incremental reload must produce exactly the same project as a full rescan.
func TestIncrementalReloadMatchesFullRescan(t *testing.T) {
	root := t.TempDir()
	writeT(t, root, "a.py", "class A:\n    pass\n")
	writeT(t, root, "pkg/b.py", "class B:\n    pass\n")
	writeT(t, root, "c.py", "class C:\n    pass\n")

	proj, err := core.Scan(root, langs.Registry{}, core.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(proj)
	m.projRoot = root

	// change a.py, delete pkg/b.py, add d.py
	writeT(t, root, "a.py", "class A:\n    pass\n\nclass A2:\n    pass\n")
	if err := os.Remove(filepath.Join(root, "pkg", "b.py")); err != nil {
		t.Fatal(err)
	}
	writeT(t, root, "d.py", "class D:\n    pass\n")

	m.reload([]string{"a.py", "pkg/b.py", "d.py"})

	full, err := core.Scan(root, langs.Registry{}, core.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.proj.Files) != len(full.Files) {
		t.Fatalf("incremental %d files, full %d", len(m.proj.Files), len(full.Files))
	}
	for i, f := range full.Files {
		got := m.proj.Files[i]
		if got.Path != f.Path {
			t.Fatalf("file %d: %q vs full %q", i, got.Path, f.Path)
		}
		var gotSyms, wantSyms []string
		for _, s := range got.AllSymbols() {
			gotSyms = append(gotSyms, s.Label())
		}
		for _, s := range f.AllSymbols() {
			wantSyms = append(wantSyms, s.Label())
		}
		if strings.Join(gotSyms, ",") != strings.Join(wantSyms, ",") {
			t.Errorf("%s: symbols %v vs full %v", f.Path, gotSyms, wantSyms)
		}
	}

	// picker reflects the same file set
	if len(m.pickerFiles) != len(full.Files) {
		t.Errorf("picker files = %d, want %d", len(m.pickerFiles), len(full.Files))
	}
}

func writeT(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
