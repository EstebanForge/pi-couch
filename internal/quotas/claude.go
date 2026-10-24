package quotas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	claudeClientID   = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeRefreshURL = "https://platform.claude.com/v1/oauth/token"
	claudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
)

var (
	claudeMu    sync.Mutex
	cachedToken string
	tokenExpiry time.Time
)

type claudeCredsFile struct {
	ClaudeAIOAuth struct {
		AccessToken  string  `json:"accessToken"`
		RefreshToken string  `json:"refreshToken"`
		ExpiresAt    float64 `json:"expiresAt"` // ms
	} `json:"claudeAiOauth"`
}

type claudeTokenResp struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresIn    float64 `json:"expires_in"` // seconds
}

type claudeUsageResp struct {
	FiveHour struct {
		Utilization float64 `json:"utilization"` // percentage
		ResetsAt    string  `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay struct {
		Utilization float64 `json:"utilization"` // percentage
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day"`
}

func fetchClaudeQuota(client *http.Client) ProviderQuota {
	q := ProviderQuota{
		ID:        "claude",
		Name:      "Claude",
		Plan:      "Pro",
		UpdatedAt: time.Now(),
	}

	token, err := getClaudeToken(client)
	if err != nil {
		q.Error = err.Error()
		return q
	}

	req, err := http.NewRequest("GET", claudeUsageURL, nil)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-code/2.1.69")

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

	var u claudeUsageResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
		return q
	}

	if t5, err := time.Parse(time.RFC3339Nano, u.FiveHour.ResetsAt); err == nil {
		q.Windows = append(q.Windows, WindowQuota{
			Label:    "Session (5h)",
			Percent:  u.FiveHour.Utilization,
			Unit:     "%",
			ResetsAt: t5,
		})
	}
	if t7, err := time.Parse(time.RFC3339Nano, u.SevenDay.ResetsAt); err == nil {
		q.Windows = append(q.Windows, WindowQuota{
			Label:    "Weekly",
			Percent:  u.SevenDay.Utilization,
			Unit:     "%",
			ResetsAt: t7,
		})
	}

	return q
}

func getClaudeToken(client *http.Client) (string, error) {
	claudeMu.Lock()
	defer claudeMu.Unlock()

	if cachedToken != "" && time.Now().Before(tokenExpiry) {
		return cachedToken, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(credPath)
	if err != nil {
		return "", fmt.Errorf("read claude creds: %w", err)
	}

	var creds claudeCredsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse claude creds: %w", err)
	}

	rf := creds.ClaudeAIOAuth.RefreshToken
	if rf == "" {
		if creds.ClaudeAIOAuth.AccessToken != "" {
			return creds.ClaudeAIOAuth.AccessToken, nil
		}
		return "", fmt.Errorf("missing refresh token")
	}

	// Refresh token via Anthropic platform
	payload, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     claudeClientID,
		"refresh_token": rf,
	})
	resp, err := client.Post(claudeRefreshURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("refresh claude token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude refresh HTTP %d", resp.StatusCode)
	}

	var tr claudeTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode refresh: %w", err)
	}

	cachedToken = tr.AccessToken
	tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return cachedToken, nil
}
