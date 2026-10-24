package quotas

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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
	q := ProviderQuota{
		ID:        "antigravity",
		Name:      "Antigravity",
		Plan:      "Pro",
		UpdatedAt: time.Now(),
	}

	tok, err := getAntigravityToken(client)
	if err != nil {
		q.Error = err.Error()
		return q
	}

	req, err := http.NewRequest("POST", antigravityQuotaURL, bytes.NewReader([]byte("{}")))
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

func getAntigravityToken(client *http.Client) (string, error) {
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

	resp, err := client.PostForm(antigravityTokenURL, form)
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
