// Command print-settings-meta emits config.Settings() as JSON for the console.
// Used when GET /v1/settings is unavailable (binary not yet restarted).
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/agent0ai/spynel/internal/config"
)

func main() {
	items := make([]map[string]any, 0)
	for _, setting := range config.Settings(config.Config{}) {
		item := map[string]any{
			"key":         setting.Key,
			"section":     setting.Section,
			"description": setting.Description,
			"value":       setting.Value,
			"secret":      setting.Secret,
			"restart":     setting.Restart,
			"advanced":    setting.Advanced,
		}
		if len(setting.Choices) > 0 {
			item["choices"] = setting.Choices
		}
		items = append(items, item)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"settings": items}); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
}
