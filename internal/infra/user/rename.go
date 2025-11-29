//go:build darwin

package userinfra

import (
	"fmt"
	"os"
	"path/filepath"
)

// RenameFriendlyName updates the friendlyName in frpc.toml and restarts frpc.
func RenameFriendlyName(name string) string {
	if msg := validateFriendlyName(name); msg != "" {
		return fmt.Sprintf("Failed to update friendly name: %s", msg)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Sprintf("Failed to update friendly name: unable to determine user home directory: %v", err)
	}
	frpcPath := filepath.Join(home, "services", "imsg", "frpc.toml")
	if err := setFRPCFriendlyName(frpcPath, name); err != nil {
		return fmt.Sprintf("Failed to update friendly name: %v", err)
	}

	username, domain, err := currentUserLaunchDomain()
	if err != nil {
		return fmt.Sprintf("Friendly name updated, but failed to restart frpc: %v", err)
	}
	label := fmt.Sprintf("%s/"+launchAgentFRPCLabelPattern, domain, username)
	if err := runLaunchctl("kickstart", "-k", label); err != nil {
		return fmt.Sprintf("Friendly name updated, but failed to restart frpc: %v", err)
	}

	return fmt.Sprintf("Updated friendly name to \"%s\" and restarted frpc.", name)
}
