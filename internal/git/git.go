// Package git reads working-tree state for the changes tab. Read-only:
// status, branch, and diffs only; Couch Pi never writes to a repo.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// DiffLimit caps a per-file diff at 2000 lines (R4 spec).
const DiffLimit = 2000

// ErrNotRepo means the path is not inside a git work tree.
var ErrNotRepo = errors.New("git: not a repository")

// FileChange is one working-tree entry from status plus numstat counts.
type FileChange struct {
	Path    string
	Staged  string // index letter (porcelain X)
	Work    string // worktree letter (porcelain Y)
	Added   int    // numstat +
	Deleted int    // numstat -
	Binary  bool
}

// Label collapses X and Y into one chip letter for the card list.
func (f FileChange) Label() string {
	x, y := f.Staged, f.Work
	switch {
	case x == "?" || y == "?":
		return "new"
	case x == "D" || y == "D":
		return "del"
	case x == "R" || y == "R":
		return "ren"
	case x == "M" || y == "M" || x == "A":
		return "mod"
	default:
		if x != " " && x != "" {
			return string(x)
		}
		return string(y)
	}
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if strings.Contains(msg, "not a git repository") {
			return "", ErrNotRepo
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// Toplevel returns the repo root for cwd, or ErrNotRepo.
func Toplevel(ctx context.Context, cwd string) (string, error) {
	out, err := run(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Branch returns the checked-out branch name; detached HEAD reads "HEAD".
func Branch(ctx context.Context, repo string) string {
	out, err := run(ctx, repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Status lists worktree changes. Porcelain v1 -z entries are
// NUL-terminated: one "XY PATH" field per entry, renames add the old
// path as a second field.
func Status(ctx context.Context, repo string) ([]FileChange, error) {
	out, err := run(ctx, repo, "status", "--porcelain=v1", "-z")
	if err != nil {
		return nil, err
	}
	added, deleted, binary := numstat(ctx, repo, false)
	sadd, sdel, sbinary := numstat(ctx, repo, true)
	var files []FileChange
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		f := FileChange{Staged: string(entry[0]), Work: string(entry[1]), Path: entry[3:]}
		if f.Staged == "R" || f.Work == "R" {
			if i+1 < len(fields) {
				i++ // consume the old path
			}
		}
		a, d, b := added[f.Path], deleted[f.Path], binary[f.Path] || sbinary[f.Path]
		if sa, ok := sadd[f.Path]; ok {
			a, d = sa, sdel[f.Path]
		}
		f.Added, f.Deleted, f.Binary = a, d, b
		files = append(files, f)
	}
	return files, nil
}

func numstat(ctx context.Context, repo string, staged bool) (map[string]int, map[string]int, map[string]bool) {
	args := []string{"diff", "--numstat"}
	if staged {
		args = append(args, "--cached")
	}
	out, err := run(ctx, repo, args...)
	added := map[string]int{}
	deleted := map[string]int{}
	bin := map[string]bool{}
	if err != nil {
		return added, deleted, bin
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		path := parts[2]
		if parts[0] == "-" {
			bin[path] = true
			continue
		}
		n, _ := strconv.Atoi(parts[0])
		added[path] = n
		d, _ := strconv.Atoi(parts[1])
		deleted[path] = d
	}
	return added, deleted, bin
}

// Diff returns the diff for one path; staged selects --cached. Truncated
// reports hitting DiffLimit; binary files return empty text.
func Diff(ctx context.Context, repo, path string, staged bool) (text string, truncated bool, err error) {
	args := []string{"diff", "--"}
	if staged {
		args = []string{"diff", "--cached", "--"}
	}
	args = append(args, path)
	out, err := run(ctx, repo, args...)
	if err != nil {
		return "", false, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) > DiffLimit {
		return strings.Join(lines[:DiffLimit], "\n"), true, nil
	}
	return out, false, nil
}
