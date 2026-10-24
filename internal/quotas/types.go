package quotas

import "time"

// ProviderQuota holds normalized quota and limits for one AI provider.
type ProviderQuota struct {
	ID        string        `json:"id"`      // "claude", "codex", "copilot", "antigravity", "zai", "minimax"
	Name      string        `json:"name"`    // Display name
	Plan      string        `json:"plan"`    // Tier / plan (e.g. "Pro", "Free", "Individual")
	Windows   []WindowQuota `json:"windows"` // Tracked windows (session, daily, weekly, monthly)
	UpdatedAt time.Time     `json:"updated_at"`
	Error     string        `json:"error,omitempty"`
}

// WindowQuota represents one limit window (e.g., 5-hour session or 7-day weekly).
type WindowQuota struct {
	Label     string    `json:"label"`     // "Session (5h)", "Weekly", "Tokens"
	Percent   float64   `json:"percent"`   // Percent used (0 to 100)
	Used      float64   `json:"used"`      // Amount used if known
	Limit     float64   `json:"limit"`     // Total limit if known
	Unit      string    `json:"unit"`      // "requests", "tokens", "%"
	ResetsAt  time.Time `json:"resets_at"` // When this window resets
	Remaining string    `json:"remaining"` // Human readable remaining or balance
}

// QuotasSnapshot is served by GET /api/quotas.
type QuotasSnapshot struct {
	Providers []ProviderQuota `json:"providers"`
	UpdatedAt time.Time       `json:"updated_at"`
}
