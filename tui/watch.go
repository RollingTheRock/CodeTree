// watch.go — file watching for the TUI: recursive fsnotify watcher with
// ignore-rule-aware directory pruning, source-file filtering and debouncing.
package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	ignore "github.com/sabhiram/go-gitignore"

	"github.com/RollingTheRock/CodeTree/core"
	"github.com/RollingTheRock/CodeTree/langs"
)

// debounceInterval merges editor save bursts (write+chmod+rename) into one
// refresh.
const debounceInterval = 300 * time.Millisecond

// isSourceFile reports whether rel names a file of a registered language.
func isSourceFile(rel string, exts map[string]bool) bool {
	return exts[strings.ToLower(filepath.Ext(rel))]
}

// watchableDir reports whether a directory should be watched: not a default
// skip dir and not matched by .gitignore.
func watchableDir(rel, name string, ign *ignore.GitIgnore) bool {
	if core.IsSkippedDir(name) {
		return false
	}
	if ign != nil && rel != "." && ign.MatchesPath(rel) {
		return false
	}
	return true
}

// relevantEvent reports whether a filesystem event should trigger a reload:
// source-file changes only (create/write/remove/rename), never ignored paths.
func relevantEvent(root string, ev fsnotify.Event, exts map[string]bool, ign *ignore.GitIgnore) bool {
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, ev.Name)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	// any path component ignored → drop (cheap check: walk parents)
	for i, part := range strings.Split(rel, "/") {
		if core.IsSkippedDir(part) && i < len(strings.Split(rel, "/"))-1 {
			return false
		}
	}
	if ign != nil {
		if ign.MatchesPath(rel) {
			return false
		}
	}
	return isSourceFile(rel, exts)
}

// watcher watches a project tree and emits one signal per debounced burst.
type watcher struct {
	fs  *fsnotify.Watcher
	out chan struct{}
}

// watchProject starts a recursive watcher. Returns nil channel on any
// failure (inotify limits etc.) — the TUI then simply never auto-reloads.
func watchProject(root string) chan struct{} {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	var ign *ignore.GitIgnore
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err == nil {
		if gi, err := ignore.CompileIgnoreFile(filepath.Join(root, ".gitignore")); err == nil {
			ign = gi
		}
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !watchableDir(rel, d.Name(), ign) {
			return filepath.SkipDir
		}
		if err := w.Add(path); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		w.Close()
		return nil
	}

	wr := &watcher{fs: w, out: make(chan struct{}, 1)}
	go wr.run(root, ign)
	return wr.out
}

// run is the event loop: filter, debounce, notify. Newly created
// directories are added to the watch set (rename-based saves included).
func (w *watcher) run(root string, ign *ignore.GitIgnore) {
	exts := langs.Extensions()
	var timer *time.Timer
	var timerC <-chan time.Time
	notify := func() {
		select {
		case w.out <- struct{}{}:
		default: // a notification is already pending
		}
	}
	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			// pick up newly created directories
			if ev.Op&fsnotify.Create != 0 {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					rel, _ := filepath.Rel(root, ev.Name)
					if watchableDir(rel, filepath.Base(ev.Name), ign) {
						_ = w.fs.Add(ev.Name)
					}
				}
			}
			if !relevantEvent(root, ev, exts, ign) {
				continue
			}
			// (re)start the debounce window
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(debounceInterval)
			timerC = timer.C
		case <-timerC:
			notify()
			timerC = nil
		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *watcher) close() { w.fs.Close() }
