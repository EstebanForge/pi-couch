package quotas

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

type claudeCredsFile struct {
	ClaudeAIOAuth struct {
		AccessToken  string  `json:"accessToken"`
		RefreshToken string  `json:"refreshToken"`
		ExpiresAt    float64 `json:"expiresAt"` // ms
	} `json:"claudeAiOauth"`
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

	token, err := getClaudeToken()
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

// getClaudeToken is deliberately a read-only consumer of the CLI's
// credential file. Refreshing here rotates Anthropic's refresh token
// without a safe place to persist the replacement, which invalidates
// the token Claude Code holds and logs the user out. When the access
// token is expired the card degrades with an honest error instead.
func getClaudeToken() (string, error) {
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

	token := creds.ClaudeAIOAuth.AccessToken
	if token == "" {
		return "", errors.New("no claude access token - open claude once to log in")
	}
	exp := time.UnixMilli(int64(creds.ClaudeAIOAuth.ExpiresAt))
	if !time.Now().Before(exp.Add(-60 * time.Second)) {
		return "", errors.New("claude token expired - open claude once to refresh it")
	}
	return token, nil
}
