package quotas

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const copilotUsageURL = "https://api.github.com/copilot_internal/user"

type copilotUserResp struct {
	CopilotPlan    string `json:"copilot_plan"`
	QuotaResetDate string `json:"quota_reset_date_utc"`
	QuotaSnapshots struct {
		PremiumInteractions struct {
			PercentRemaining float64 `json:"percent_remaining"`
			Remaining        float64 `json:"remaining"`
			Entitlement      float64 `json:"entitlement"`
		} `json:"premium_interactions"`
		Chat struct {
			PercentRemaining float64 `json:"percent_remaining"`
			Unlimited        bool    `json:"unlimited"`
		} `json:"chat"`
	} `json:"quota_snapshots"`
}

func fetchCopilotQuota(client *http.Client) ProviderQuota {
	q := ProviderQuota{
		ID:        "copilot",
		Name:      "GitHub Copilot",
		Plan:      "Individual",
		UpdatedAt: time.Now(),
	}

	var token string
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		token = strings.TrimSpace(string(out))
	}

	if token == "" {
		if home, err := os.UserHomeDir(); err == nil {
			authPath := filepath.Join(home, ".pi", "agent", "auth.json")
			if data, err := os.ReadFile(authPath); err == nil {
				var authFile struct {
					GithubCopilot struct {
						Refresh string `json:"refresh"`
						Access  string `json:"access"`
					} `json:"github-copilot"`
				}
				if err := json.Unmarshal(data, &authFile); err == nil {
					if authFile.GithubCopilot.Refresh != "" {
						token = authFile.GithubCopilot.Refresh
					} else if authFile.GithubCopilot.Access != "" {
						token = authFile.GithubCopilot.Access
					}
				}
			}
		}
	}

	if token == "" {
		q.Error = "gh auth token or ~/.pi/agent/auth.json token unavailable"
		return q
	}

	req, err := http.NewRequest("GET", copilotUsageURL, nil)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.22.4")

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

	var u copilotUserResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
		return q
	}

	if u.CopilotPlan != "" {
		q.Plan = strings.ToUpper(u.CopilotPlan[:1]) + u.CopilotPlan[1:]
	}

	var resetTime time.Time
	if u.QuotaResetDate != "" {
		resetTime, _ = time.Parse(time.RFC3339Nano, u.QuotaResetDate)
	}

	prem := u.QuotaSnapshots.PremiumInteractions
	if prem.Entitlement > 0 {
		used := prem.Entitlement - prem.Remaining
		pct := 100.0 - prem.PercentRemaining
		q.Windows = append(q.Windows, WindowQuota{
			Label:     "Premium Requests",
			Percent:   pct,
			Used:      used,
			Limit:     prem.Entitlement,
			Unit:      "requests",
			ResetsAt:  resetTime,
			Remaining: fmt.Sprintf("%.0f / %.0f", prem.Remaining, prem.Entitlement),
		})
	}

	if u.QuotaSnapshots.Chat.Unlimited {
		q.Windows = append(q.Windows, WindowQuota{
			Label:     "Chat",
			Percent:   0,
			Unit:      "requests",
			Remaining: "Unlimited",
		})
	}

	return q
}
