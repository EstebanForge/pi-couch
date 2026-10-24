package quotas

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const minimaxRemainsURL = "https://www.minimax.io/v1/token_plan/remains"

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

	req, err := http.NewRequest("GET", minimaxRemainsURL, nil)
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

	var u minimaxRemainsResp
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		q.Error = err.Error()
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
