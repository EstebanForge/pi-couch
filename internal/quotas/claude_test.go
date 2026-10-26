package quotas

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Claude fetcher must stay a read-only consumer of the CLI's
// credential file: rotating the refresh token without persisting the
// replacement invalidates the token Claude Code holds and logs the
// user out. These tests pin that no network call happens at all.

func setClaudeCreds(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func formatMs(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func noNetworkClient() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network call made")
	})}
}

func TestClaudeTokenUsesUnexpiredAccessToken(t *testing.T) {
	setClaudeCreds(t, `{"claudeAiOauth":{"accessToken":"tok-1","refreshToken":"rt","expiresAt":`+
		formatMs(time.Now().Add(time.Hour))+"}}")
	tok, err := getClaudeToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok != "tok-1" {
		t.Fatalf("token = %q, want tok-1", tok)
	}
}

func TestClaudeTokenExpiredRefusesToRefresh(t *testing.T) {
	setClaudeCreds(t, `{"claudeAiOauth":{"accessToken":"old","refreshToken":"rt","expiresAt":`+
		formatMs(time.Now().Add(-time.Hour))+"}}")
	_, err := getClaudeToken()
	if err == nil {
		t.Fatal("want error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want expired notice", err)
	}
}
