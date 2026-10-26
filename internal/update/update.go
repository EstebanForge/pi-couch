// Package update keeps the server binary current: once a day it
// compares the running version against the latest GitHub release and,
// when a newer one exists, downloads the platform asset, proves it
// runs, swaps it in atomically and re-executes itself. Auto-update is
// the default; auto_update = false in the config turns it off.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	latestAPI      = "https://api.github.com/repos/EstebanForge/pi-couch/releases/latest"
	latestDownload = "https://github.com/EstebanForge/pi-couch/releases/latest/download/"

	// binaryPath is the seam tests point at a temp file; the real one
	// is the running executable.
	binaryPath = func() string {
		p, err := os.Executable()
		if err != nil {
			return ""
		}
		return p
	}

	// restart replaces the process image with the swapped binary; the
	// listener socket dies with the old image and the fresh one binds
	// within ~100ms, the same blip as a manual service restart.
	restart = func(path string) error {
		return syscall.Exec(path, os.Args, os.Environ())
	}
)

type Updater struct {
	logger  *slog.Logger
	current string
	token   string
}

func New(logger *slog.Logger, current, token string) *Updater {
	return &Updater{logger: logger, current: current, token: token}
}

// Run settles briefly after start (the binary is fresh at boot), then
// checks once a day until the context closes.
func (u *Updater) Run(ctx context.Context) {
	select {
	case <-time.After(2 * time.Minute):
	case <-ctx.Done():
		return
	}
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	u.checkAndMaybeRestart()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.checkAndMaybeRestart()
		}
	}
}

func (u *Updater) checkAndMaybeRestart() {
	tag, err := u.check()
	if err != nil {
		u.logger.Warn("update check failed", "err", err)
		return
	}
	if tag == "" {
		return
	}
	u.logger.Info("updated binary, restarting", "from", u.current, "to", tag)
	if err := restart(binaryPath()); err != nil {
		u.logger.Error("restart into new binary", "err", err)
	}
}

// check downloads and swaps a newer binary when one exists, returning
// its tag; an empty return means already current.
func (u *Updater) check() (string, error) {
	tag, err := u.latestTag()
	if err != nil {
		return "", err
	}
	if cmpVersion(tag, u.current) <= 0 {
		return "", nil
	}

	tmp, err := u.download(tag)
	if err != nil {
		return "", err
	}

	cur := binaryPath()
	if cur == "" {
		return "", errors.New("cannot resolve the running binary path")
	}
	bak := cur + ".bak"
	if err := copyFile(cur, bak); err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	// One atomic rename: the running process keeps the old inode and
	// the next exec picks the new file.
	if err := os.Rename(tmp, cur); err != nil {
		return "", fmt.Errorf("swap: %w", err)
	}
	return tag, nil
}

func (u *Updater) latestTag() (string, error) {
	req, err := http.NewRequest("GET", latestAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound && u.token == "" {
			return "", errors.New("release lookup needs a token while the repo is private (set GITHUB_TOKEN)")
		}
		return "", fmt.Errorf("release lookup HTTP %d", resp.StatusCode)
	}
	var out struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimPrefix(out.TagName, "v"), nil
}

// download fetches the platform asset, marks it executable and
// verifies it by running -version; the printed version must match the
// release tag, which proves executability and architecture in one go.
func (u *Updater) download(tag string) (string, error) {
	url := latestDownload + "pi-couch-" + runtime.GOOS + "-" + runtime.GOARCH
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	cur := binaryPath()
	tmp, err := os.CreateTemp(filepath.Dir(cur), ".couch-pi-new-*")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fail(err)
	}

	out, err := exec.Command(tmp.Name(), "-version").Output()
	if err != nil {
		return fail(fmt.Errorf("probe new binary: %w", err))
	}
	if got := strings.TrimSpace(string(out)); got != tag {
		return fail(fmt.Errorf("probe printed %q, want %q", got, tag))
	}
	return tmp.Name(), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// cmpVersion compares dotted numeric versions; unparsable input sorts
// oldest so a junk tag never triggers a downgrade.
func cmpVersion(a, b string) int {
	pa, poka := parseVersion(a)
	pb, pokb := parseVersion(b)
	if !poka || !pokb {
		if poka {
			return 1
		}
		if pokb {
			return -1
		}
		return 0
	}
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parseVersion(s string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
