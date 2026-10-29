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
	// The download is pinned to the tag read from the API so the tag
	// and the asset come from the same release, not two moments.
	latestAPI      = "https://api.github.com/repos/EstebanForge/pi-couch/releases/latest"
	latestDownload = "https://github.com/EstebanForge/pi-couch/releases/download/"

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

	// probeBin is the seam the tests drive for probe failures.
	probeBin = func(path string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, "-version")
		cmd.Env = []string{} // empty env: the probe runs before any trust is granted
		return cmd.Output()
	}
)

const (
	httpTimeout   = 60 * time.Second
	probeTimeout  = 10 * time.Second
	maxBinarySize = 256 << 20
	// 12 hours: cadence the operator picked; releases are never urgent.
	updateInterval = 12 * time.Hour
)

// HealthyBoot is how long a boot must stay up before the update marker
// clears and the swap counts as good; a boot that still finds the marker
// armed rolls back instead.
const HealthyBoot = time.Minute

type Updater struct {
	logger  *slog.Logger
	current string
	token   string
	// path is resolved once at construction, before any swap, so a
	// replaced or unlinked executable never sends the restart to a
	// "(deleted)" proc path.
	path     string
	stateDir string
	client   *http.Client
}

func New(logger *slog.Logger, current, token, stateDir string) *Updater {
	return &Updater{
		logger:   logger,
		current:  current,
		token:    token,
		path:     binaryPath(),
		stateDir: stateDir,
		client:   &http.Client{Timeout: httpTimeout},
	}
}

// Run checks every 12 hours, the first look 12 hours after start:
// a fresh binary has nothing to update, so boot checks are skipped
// entirely. Stops when the context closes.
func (u *Updater) Run(ctx context.Context) {
	t := time.NewTicker(updateInterval)
	defer t.Stop()
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
	// Arm the rollback marker before the exec: if the new binary cannot
	// stay up, the next boot restores the backup.
	if err := armPending(u.stateDir, tag); err != nil {
		u.logger.Warn("rollback marker not written", "err", err)
	}
	if err := restart(u.path); err != nil {
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

	bak, err := atomicBackup(u.path)
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("backup: %w", err)
	}
	_ = bak
	// One atomic rename: the running process keeps the old inode and
	// the next exec picks the new file.
	if err := os.Rename(tmp, u.path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("swap: %w", err)
	}
	return tag, nil
}

// atomicBackup copies the current binary to <path>.bak via a temp file
// so a partial copy never destroys the previous good backup.
func atomicBackup(cur string) (string, error) {
	if cur == "" {
		return "", errors.New("cannot resolve the running binary path")
	}
	bak := cur + ".bak"
	tmpbak := bak + ".tmp"
	if err := copyFile(cur, tmpbak); err != nil {
		os.Remove(tmpbak)
		return "", err
	}
	if err := os.Rename(tmpbak, bak); err != nil {
		os.Remove(tmpbak)
		return "", err
	}
	return bak, nil
}

func (u *Updater) latestTag() (string, error) {
	tag, err := u.fetchTag(true)
	// A stale or wrongly-scoped token must not break a public lookup.
	if err != nil && u.token != "" && strings.Contains(err.Error(), "HTTP 401") {
		return u.fetchTag(false)
	}
	return tag, err
}

func (u *Updater) fetchTag(auth bool) (string, error) {
	req, err := http.NewRequest("GET", latestAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if auth && u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return "", errors.New("no release found - publish one (a private repo also needs GITHUB_TOKEN)")
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

// download fetches the tag's platform asset, marks it executable and
// verifies it by running -version; the printed version must match the
// release tag, which proves executability and architecture in one go.
func (u *Updater) download(tag string) (string, error) {
	url := latestDownload + tag + "/pi-couch-" + runtime.GOOS + "-" + runtime.GOARCH
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(u.path), ".couch-pi-new-*")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, maxBinarySize)); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return fail(err)
	}

	if err := probeWithRetries(tmp.Name(), tag); err != nil {
		return fail(err)
	}
	return tmp.Name(), nil
}

// probeWithRetries verifies the new binary and rides out ETXTBSY, the
// classic race where this server's own forks still hold the just-written
// file open for execution.
func probeWithRetries(path, tag string) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		out, err := probeBin(path)
		if err == nil {
			if got := strings.TrimSpace(string(out)); got != tag {
				return fmt.Errorf("probe printed %q, want %q", got, tag)
			}
			return nil
		}
		lastErr = err
		if !strings.Contains(err.Error(), "text file busy") {
			break
		}
	}
	if lastErr != nil {
		return fmt.Errorf("probe new binary: %w", lastErr)
	}
	return nil
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

// The pending marker records an update that was swapped in but not yet
// proven healthy. First boot arms it; a boot that still finds it armed
// means the new binary could not stay up, so RecoverBoot restores the
// backup.
type pending struct {
	Tag      string `json:"tag"`
	Attempts int    `json:"attempts"`
}

func pendingPath(stateDir string) string {
	return filepath.Join(stateDir, "update-pending.json")
}

func armPending(stateDir, tag string) error {
	return writePending(stateDir, pending{Tag: tag})
}

func writePending(stateDir string, p pending) error {
	if stateDir == "" {
		return errors.New("no state dir")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(pendingPath(stateDir), raw, 0o600)
}

func readPending(stateDir string) (pending, bool) {
	raw, err := os.ReadFile(pendingPath(stateDir))
	if err != nil {
		return pending{}, false
	}
	var p pending
	if err := json.Unmarshal(raw, &p); err != nil {
		return pending{}, false
	}
	return p, true
}

// RecoverBoot runs at every boot, before any config load: a crash could
// be the new binary refusing the config, so the check must precede it.
// It never returns when a rollback fires - the process execs into the
// restored backup.
func RecoverBoot(stateDir, binPath string, healthy time.Duration, log *slog.Logger, current string) {
	p, ok := readPending(stateDir)
	if !ok {
		return
	}
	if p.Tag != current {
		// The running binary is not the one the marker armed for; a
		// manual install superseded the update.
		os.Remove(pendingPath(stateDir))
		return
	}
	if p.Attempts == 0 {
		_ = writePending(stateDir, pending{Tag: p.Tag, Attempts: 1})
		time.AfterFunc(healthy, func() { os.Remove(pendingPath(stateDir)) })
		log.Info("update boot, arming rollback", "tag", p.Tag)
		return
	}
	bak := binPath + ".bak"
	if err := os.Rename(bak, binPath); err != nil {
		log.Error("rollback failed", "err", err)
		os.Remove(pendingPath(stateDir))
		return
	}
	os.Remove(pendingPath(stateDir))
	log.Info("update rolled back, restarting into the backup", "tag", p.Tag)
	if err := restart(binPath); err != nil {
		log.Error("rollback restart", "err", err)
	}
}

// cmpVersion compares dotted numeric versions of any part count so
// the date scheme (20261028, 20261028.3) orders against the old
// three-part semver; unparsable input sorts oldest so a junk tag
// never triggers a downgrade.
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
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var av, bv int
		if i < len(pa) {
			av = pa[i]
		}
		if i < len(pb) {
			bv = pb[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

func parseVersion(s string) ([]int, bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}
