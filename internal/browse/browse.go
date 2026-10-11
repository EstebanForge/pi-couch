// Package browse lists and previews files under a repo root for the
// files tab. Read-only; previews never cross the root.
package browse

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// PreviewLimit caps a text preview at 2 MB on disk (09 spec cutoff);
// MaxPreviewRender caps what the page embeds.
const (
	PreviewLimit     = 2 << 20 // 09 spec cutoff on disk
	MaxPreviewRender = 256 << 10
)

// ErrEscape means the path leaves the root, or rides symlinks out.
var ErrEscape = errors.New("browse: path escapes root")

// Entry is one listing row.
type Entry struct {
	Name string
	Dir  bool
	Size int64
}

// resolve cleans rel against root and rejects escapes, following
// symlinks for the deepest existing path component so outbound links
// fail here even when the leaf does not exist.
func resolve(root, rel string) (string, error) {
	cleanRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(cleanRoot, filepath.FromSlash(path.Clean("/"+rel)))
	real := ""
	for p := target; ; p = filepath.Dir(p) {
		r, err := filepath.EvalSymlinks(p)
		if err == nil {
			real = r
			break
		}
		if p == string(filepath.Separator) {
			return "", err
		}
	}
	if real != cleanRoot && !strings.HasPrefix(real, cleanRoot+string(filepath.Separator)) {
		return "", ErrEscape
	}
	return target, nil
}

// List reads one directory level: the tree stays lazy by navigation.
func List(root, rel string) ([]Entry, error) {
	dir, err := resolve(root, rel)
	if err != nil {
		return nil, err
	}
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		e := Entry{Name: d.Name(), Dir: d.IsDir()}
		if !e.Dir {
			if info, err := d.Info(); err == nil {
				e.Size = info.Size()
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Preview returns file content for the page: text up to
// MaxPreviewRender, with binary and size cutoffs from the 09 spec.
// Over-limit and binary files return a reason instead of content.
func Preview(root, rel string) (text string, kind string, err error) {
	full, err := resolve(root, rel)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", errors.New("browse: not a file")
	}
	if info.Size() > PreviewLimit {
		return "", "toolarge", nil
	}
	f, err := os.Open(full)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, MaxPreviewRender+1))
	if err != nil {
		return "", "", err
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return "", "binary", nil
	}
	if len(content) > MaxPreviewRender {
		return string(content[:MaxPreviewRender]), "partial", nil
	}
	return string(content), "text", nil
}
