package quotas

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Package var so tests can pin the fetcher against a canned server.
var zaiQuotaURL = "https://api.z.ai/api/monitor/usage/quota/limit"

type zaiQuotaResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Level  string `json:"level"`
		Limits []struct {
			Type          string `json:"type"` // "TIME_LIMIT", "TOKENS_LIMIT"
			Percentage    int    `json:"percentage"`
			Remaining     int    `json:"remaining"`
			NextResetTime int64  `json:"nextResetTime"` // ms timestamp
		} `json:"limits"`
	} `json:"data"`
}

func fetchZaiQuota(client *http.Client) ProviderQuota {
	q := ProviderQuota{
		ID:        "zai",
		Name:      "Zai GLM",
		Plan:      "Pro",
		UpdatedAt: time.Now(),
	}

	apiKey := getAPIKey("ZAI_API_KEY", "CNSTR_ZAI_API_KEY")
	if apiKey == "" {
		q.Error = "no ZAI_API_KEY set"
		return q
	}

	req, err := http.NewRequest("GET", zaiQuotaURL, nil)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

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

	var u zaiQuotaResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
		return q
	}

	if u.Data.Level != "" {
		q.Plan = strings.ToUpper(u.Data.Level[:1]) + u.Data.Level[1:]
	}

	// The tokens limit is the 5-hour session allowance, the one that
	// runs out mid-session, so it reads first; the web-search
	// allowance sits below it.
	for _, tokensPass := range []bool{true, false} {
		for _, l := range u.Data.Limits {
			isTokens := l.Type == "TOKENS_LIMIT"
			if isTokens != tokensPass {
				continue
			}
			var resets time.Time
			if l.NextResetTime > 0 {
				resets = time.UnixMilli(l.NextResetTime)
			}
			label := "Web searches remaining"
			remaining := ""
			if isTokens {
				label = "Session remaining (5h)"
			} else if l.Remaining > 0 {
				remaining = fmt.Sprintf("%d left", l.Remaining)
			}
			q.Windows = append(q.Windows, WindowQuota{
				Label:     label,
				Percent:   float64(l.Percentage),
				Unit:      "%",
				ResetsAt:  resets,
				Remaining: remaining,
			})
		}
	}

	return q
}
