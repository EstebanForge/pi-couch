package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher folds fsnotify events on the sessions tree into debounced ticks.
// fsnotify is not recursive: the root is watched for new workdirs, each
// workdir for session file writes.
type Watcher struct {
	ticks chan struct{}
}

// StartWatcher begins watching. Ticks coalesce bursts (a session write can
// fire several events) into one channel send; a slow consumer drops ticks.
func StartWatcher(ctx context.Context, root string) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fs.Add(root); err != nil {
		_ = fs.Close()
		return nil, err
	}
	for _, d := range workdirs(root) {
		_ = fs.Add(d) // a workdir vanishing between list and add is fine
	}

	w := &Watcher{ticks: make(chan struct{}, 1)}
	go w.loop(ctx, fs, root)
	return w, nil
}

// Events returns the debounced tick channel.
func (w *Watcher) Events() <-chan struct{} { return w.ticks }

func (w *Watcher) loop(ctx context.Context, fs *fsnotify.Watcher, root string) {
	defer fs.Close()
	var debounce *time.Timer
	fire := func() {
		if debounce == nil {
			debounce = time.AfterFunc(250*time.Millisecond, func() {
				select {
				case w.ticks <- struct{}{}:
				default: // consumer still busy; its next render is fresh enough
				}
			})
		} else {
			debounce.Reset(250 * time.Millisecond)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-fs.Events:
			if !ok {
				return
			}
			if isDir(ev.Name) {
				_ = fs.Add(ev.Name) // new workdir appeared; watch it
				fire()              // and its first session file is imminent
				continue
			}
			if strings.HasSuffix(ev.Name, ".jsonl") || ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
				fire()
			}
		case <-fs.Errors:
			// tolerant: files vanish mid-event on live trees
		}
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// workdirs lists direct subdirectories of the sessions root.
func workdirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out
}
