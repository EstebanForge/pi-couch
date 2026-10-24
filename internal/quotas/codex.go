package quotas

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

type codexAuthFile struct {
	AccessToken string `json:"access_token"`
	Tokens      struct {
		AccessToken string `json:"access_token"`
	} `json:"tokens"`
}

type codexUsageResp struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		PrimaryWindow struct {
			UsedPercent        float64 `json:"used_percent"`
			LimitWindowSeconds int64   `json:"limit_window_seconds"`
			ResetAfterSeconds  int64   `json:"reset_after_seconds"`
		} `json:"primary_window"`
		SecondaryWindow struct {
			UsedPercent        float64 `json:"used_percent"`
			LimitWindowSeconds int64   `json:"limit_window_seconds"`
			ResetAfterSeconds  int64   `json:"reset_after_seconds"`
		} `json:"secondary_window"`
	} `json:"rate_limit"`
}

func fetchCodexQuota(client *http.Client) ProviderQuota {
	q := ProviderQuota{
		ID:        "codex",
		Name:      "Codex",
		Plan:      "Plus",
		UpdatedAt: time.Now(),
	}

	home, err := os.UserHomeDir()
	if err != nil {
		q.Error = err.Error()
		return q
	}

	data, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		q.Error = err.Error()
		return q
	}

	var auth codexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		q.Error = err.Error()
		return q
	}

	tok := auth.Tokens.AccessToken
	if tok == "" {
		tok = auth.AccessToken
	}
	if tok == "" {
		q.Error = "no token found"
		return q
	}

	req, err := http.NewRequest("GET", codexUsageURL, nil)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex-cli")

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

	var u codexUsageResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
		return q
	}

	if u.PlanType != "" {
		q.Plan = u.PlanType
	}

	now := time.Now()
	pw := u.RateLimit.PrimaryWindow
	if pw.LimitWindowSeconds > 0 {
		label := "Session"
		if pw.LimitWindowSeconds >= 86400*7 {
			label = "Weekly"
		} else if pw.LimitWindowSeconds >= 86400 {
			label = "Daily"
		}
		q.Windows = append(q.Windows, WindowQuota{
			Label:    label,
			Percent:  pw.UsedPercent,
			Unit:     "%",
			ResetsAt: now.Add(time.Duration(pw.ResetAfterSeconds) * time.Second),
		})
	}

	sw := u.RateLimit.SecondaryWindow
	if sw.LimitWindowSeconds > 0 {
		label := "Weekly"
		if sw.LimitWindowSeconds >= 86400*25 {
			label = "Monthly"
		}
		q.Windows = append(q.Windows, WindowQuota{
			Label:    label,
			Percent:  sw.UsedPercent,
			Unit:     "%",
			ResetsAt: now.Add(time.Duration(sw.ResetAfterSeconds) * time.Second),
		})
	}

	return q
}
