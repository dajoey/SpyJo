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
	items := config.SettingsJSON(config.Settings(config.Config{}))
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"settings": items}); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
}
