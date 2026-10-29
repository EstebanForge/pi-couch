package quotas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const minimaxRemainsURL = "https://www.minimax.io/v1/token_plan/remains"

// minimax.io answers slower than the shared client budget from some
// networks (5s+ to first byte), so this fetcher widens its own
// deadline; the service-level client stays tight for the fast
// providers.
const minimaxTimeout = 30 * time.Second

type minimaxRemainsResp struct {
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	ModelRemains []struct {
		ModelName                       string `json:"model_name"` // "general", "video"
		CurrentIntervalRemainingPercent int    `json:"current_interval_remaining_percent"`
		EndTime                         int64  `json:"end_time"` // ms timestamp
		WeeklyEndTime                   int64  `json:"weekly_end_time"`
		CurrentWeeklyRemainingPercent   int    `json:"current_weekly_remaining_percent"`
	} `json:"model_remains"`
}

func fetchMinimaxQuota(client *http.Client) ProviderQuota {
	return fetchMinimaxQuotaAt(client, minimaxRemainsURL)
}

func fetchMinimaxQuotaAt(client *http.Client, url string) ProviderQuota {
	q := ProviderQuota{
		ID:        "minimax",
		Name:      "MiniMax",
		Plan:      "Standard",
		UpdatedAt: time.Now(),
	}

	apiKey := getAPIKey("MINIMAX_API_KEY", "CNSTR_MINIMAX_API_KEY")
	if apiKey == "" {
		q.Error = "no MINIMAX_API_KEY set"
		return q
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	c := *client
	c.Timeout = minimaxTimeout
	resp, err := c.Do(req)
	if err != nil {
		q.Error = minimaxDoError(err)
		return q
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		q.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return q
	}

	var u minimaxRemainsResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
		return q
	}
	if u.BaseResp.StatusCode != 0 {
		if u.BaseResp.StatusMsg != "" {
			q.Error = u.BaseResp.StatusMsg
		} else {
			q.Error = fmt.Sprintf("minimax API error %d", u.BaseResp.StatusCode)
		}
		return q
	}

	for _, m := range u.ModelRemains {
		if m.ModelName == "general" {
			usedInterval := 100.0 - float64(m.CurrentIntervalRemainingPercent)
			var resetInterval time.Time
			if m.EndTime > 0 {
				resetInterval = time.UnixMilli(m.EndTime)
			}
			q.Windows = append(q.Windows, WindowQuota{
				Label:    "Interval",
				Percent:  usedInterval,
				Unit:     "%",
				ResetsAt: resetInterval,
			})

			usedWeekly := 100.0 - float64(m.CurrentWeeklyRemainingPercent)
			var resetWeekly time.Time
			if m.WeeklyEndTime > 0 {
				resetWeekly = time.UnixMilli(m.WeeklyEndTime)
			}
			q.Windows = append(q.Windows, WindowQuota{
				Label:    "Weekly",
				Percent:  usedWeekly,
				Unit:     "%",
				ResetsAt: resetWeekly,
			})
			break
		}
	}

	return q
}

// The phone renders q.Error verbatim, so a deadline miss reads short
// instead of Go's wrapped url.Error with the endpoint URL inside.
func minimaxDoError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return err.Error()
}
