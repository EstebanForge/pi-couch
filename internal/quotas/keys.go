package quotas

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// getAPIKey looks up an API key from environment variables,
// falling back to ~/.config/couch-pi/keys.json.
func getAPIKey(envVars ...string) string {
	for _, env := range envVars {
		if val := os.Getenv(env); val != "" {
			return val
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	keysPath := filepath.Join(home, ".config", "couch-pi", "keys.json")
	data, err := os.ReadFile(keysPath)
	if err != nil {
		return ""
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}

	for _, env := range envVars {
		if val, ok := m[env]; ok && val != "" {
			return val
		}
	}

	return ""
}
