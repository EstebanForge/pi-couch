package quotas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	antigravityTokenURL = "https://oauth2.googleapis.com/token"
	antigravityQuotaURL = "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
)

var (
	agyMu     sync.Mutex
	agyToken  string
	agyExpiry time.Time
	// agyCLIQuotaURL and agyBin are test seams; fetchAntigravityQuotaAt
	// takes them as parameters so tests never mutate the vars.
	agyBin = "agy"
)

type agyTokenFile struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

type googleTokenResp struct {
	AccessToken string  `json:"access_token"`
	ExpiresIn   float64 `json:"expires_in"`
}

type agyQuotaResp struct {
	Groups []struct {
		Buckets []struct {
			DisplayName       string  `json:"displayName"`
			Window            string  `json:"window"`
			ResetTime         string  `json:"resetTime"`
			RemainingFraction float64 `json:"remainingFraction"`
		} `json:"buckets"`
	} `json:"groups"`
}

func fetchAntigravityQuota(client *http.Client) ProviderQuota {
	return fetchAntigravityQuotaAt(client, antigravityQuotaURL, antigravityTokenURL, agyBin)
}

// fetchAntigravityQuotaAt prefers the agy CLI's own quota report: the
// HTTP summary endpoint serves only the third-party pool on both known
// hosts (the Gemini and Argon pools are omitted), while the CLI carries
// every group. The CLI failing (missing binary, cold language server)
// falls back to the summary.
func fetchAntigravityQuotaAt(client *http.Client, quotaURL, tokenURL, bin string) ProviderQuota {
	q := ProviderQuota{
		ID:        "antigravity",
		Name:      "Antigravity",
		Plan:      "Pro",
		UpdatedAt: time.Now(),
	}
	if windows := runAgyQuota(bin); len(windows) > 0 {
		q.Windows = windows
		return q
	}
	return fetchAgySummaryQuota(client, quotaURL, tokenURL, q)
}

func runAgyQuota(bin string) []WindowQuota {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-p", "/quota", "--output-format", "json").Output()
	if err != nil {
		return nil
	}
	return parseAgyCLIQuota(out)
}

// agyGroups finds the quota report's groups array, top-level or nested.
func agyGroups(raw []byte) []byte {
	var top struct {
		Groups []json.RawMessage `json:"groups"`
	}
	if err := json.Unmarshal(raw, &top); err == nil && len(top.Groups) > 0 {
		arr, _ := json.Marshal(top.Groups)
		return arr
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	var found []byte
	var walk func(node any)
	walk = func(node any) {
		if found != nil {
			return
		}
		switch v := node.(type) {
		case map[string]any:
			if g, ok := v["groups"].([]any); ok && len(g) > 0 {
				found, _ = json.Marshal(g)
				return
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(probe)
	return found
}

// agyShortName collapses the report's group names for window labels.
func agyShortName(name string) string {
	switch name {
	case "Gemini Models":
		return "Gemini"
	case "Claude and GPT models":
		return "Claude/GPT"
	case "Gemini 4 Argon":
		return "Argon"
	}
	return name
}

func parseAgyCLIQuota(raw []byte) []WindowQuota {
	var groups []struct {
		Name    string `json:"name"`
		Buckets []struct {
			Window            string  `json:"window"`
			RemainingFraction float64 `json:"remaining_fraction"`
			ResetTime         string  `json:"reset_time"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(agyGroups(raw), &groups); err != nil {
		return nil
	}
	var windows []WindowQuota
	for _, g := range groups {
		short := agyShortName(g.Name)
		for _, b := range g.Buckets {
			var resets time.Time
			if b.ResetTime != "" {
				resets, _ = time.Parse(time.RFC3339, b.ResetTime)
			}
			windows = append(windows, WindowQuota{
				Label:    short + " " + b.Window,
				Percent:  (1.0 - b.RemainingFraction) * 100.0,
				Unit:     "%",
				ResetsAt: resets,
			})
		}
	}
	return windows
}

func fetchAgySummaryQuota(client *http.Client, quotaURL, tokenURL string, q ProviderQuota) ProviderQuota {
	tok, err := getAntigravityTokenAt(client, tokenURL)
	if err != nil {
		q.Error = err.Error()
		return q
	}

	req, err := http.NewRequest("POST", quotaURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		q.Error = err.Error()
		return q
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "antigravity")

	resp, err := client.Do(req)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		q.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return q
	}

	var u agyQuotaResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
		return q
	}

	for _, g := range u.Groups {
		for _, b := range g.Buckets {
			var resets time.Time
			if b.ResetTime != "" {
				resets, _ = time.Parse(time.RFC3339Nano, b.ResetTime)
			}
			usedPct := (1.0 - b.RemainingFraction) * 100.0
			label := b.DisplayName
			if label == "" {
				label = b.Window
			}
			q.Windows = append(q.Windows, WindowQuota{
				Label:    label,
				Percent:  usedPct,
				Unit:     "%",
				ResetsAt: resets,
			})
		}
	}

	return q
}

func getAntigravityTokenAt(client *http.Client, tokenURL string) (string, error) {
	agyMu.Lock()
	defer agyMu.Unlock()

	if agyToken != "" && time.Now().Before(agyExpiry) {
		return agyToken, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	tokPath := filepath.Join(home, ".gemini", "antigravity-acp", "acp_token.json")
	data, err := os.ReadFile(tokPath)
	if err != nil {
		return "", fmt.Errorf("read acp_token: %w", err)
	}

	var at agyTokenFile
	if err := json.Unmarshal(data, &at); err != nil {
		return "", fmt.Errorf("parse acp_token: %w", err)
	}

	form := url.Values{}
	form.Set("client_id", at.ClientID)
	form.Set("client_secret", at.ClientSecret)
	form.Set("refresh_token", at.RefreshToken)
	form.Set("grant_type", "refresh_token")

	resp, err := client.PostForm(tokenURL, form)
	if err != nil {
		return "", fmt.Errorf("refresh google token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google refresh HTTP %d", resp.StatusCode)
	}

	var g googleTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}

	agyToken = g.AccessToken
	agyExpiry = time.Now().Add(time.Duration(g.ExpiresIn-60) * time.Second)
	return agyToken, nil
}
