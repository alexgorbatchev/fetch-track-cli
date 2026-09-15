package deps

import (
	"fmt"
	"os/exec"
	"strings"
)

// ResolveJSRuntimeArgs returns the appropriate yt-dlp arguments for JavaScript runtime execution.
func ResolveJSRuntimeArgs(preference string) []string {
	pref := strings.ToLower(strings.TrimSpace(preference))
	if pref == "" || pref == "auto" {
		for _, engine := range []string{"deno", "node", "bun", "quickjs"} {
			if path, err := exec.LookPath(engine); err == nil && path != "" {
				return []string{"--js-runtimes", fmt.Sprintf("%s:%s", engine, path)}
			}
		}
		return nil
	}

	if pref == "none" || pref == "off" || pref == "false" || pref == "disabled" {
		return nil
	}

	// Explicit engine specified
	if path, err := exec.LookPath(pref); err == nil && path != "" {
		return []string{"--js-runtimes", fmt.Sprintf("%s:%s", pref, path)}
	}
	return []string{"--js-runtimes", pref}
}
