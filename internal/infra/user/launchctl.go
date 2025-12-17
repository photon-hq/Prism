//go:build darwin

package userinfra

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

const (
	launchDaemonServerLabel = "com.imsg.server.%s"
	launchDaemonFRPCLabel   = "com.imsg.frpc.%s"
)

// StopAllServices stops the per-user LaunchDaemons.
// Disables and boots out services so they won't restart via KeepAlive.
func StopAllServices() string {
	username, err := currentUsername()
	if err != nil {
		return fmt.Sprintf("Failed to stop services: %v", err)
	}

	serverLabel := fmt.Sprintf(launchDaemonServerLabel, username)
	frpcLabel := fmt.Sprintf(launchDaemonFRPCLabel, username)

	if _, err := os.Stat("/Library/LaunchDaemons/" + serverLabel + ".plist"); err != nil {
		return "No LaunchDaemons found. Please run Host setup first (sudo ./prism)."
	}

	_ = launchctl("disable", "system/"+serverLabel)
	_ = launchctl("disable", "system/"+frpcLabel)
	_ = launchctl("bootout", "system/"+serverLabel)
	_ = launchctl("bootout", "system/"+frpcLabel)

	return "Stopped the Prism server and frpc. Use 'Start all services' to restart them."
}

// StartAllServices enables and starts the per-user LaunchDaemons.
func StartAllServices() string {
	username, err := currentUsername()
	if err != nil {
		return fmt.Sprintf("Failed to start services: %v", err)
	}

	serverLabel := fmt.Sprintf(launchDaemonServerLabel, username)
	frpcLabel := fmt.Sprintf(launchDaemonFRPCLabel, username)
	serverPlist := "/Library/LaunchDaemons/" + serverLabel + ".plist"
	frpcPlist := "/Library/LaunchDaemons/" + frpcLabel + ".plist"

	if _, err := os.Stat(serverPlist); err != nil {
		return "No LaunchDaemons found. Please run Host setup first (sudo ./prism)."
	}

	_ = launchctl("enable", "system/"+serverLabel)
	_ = launchctl("enable", "system/"+frpcLabel)
	_ = launchctlBootstrap("system", frpcPlist)
	_ = launchctlBootstrap("system", serverPlist)
	_ = launchctl("kickstart", "-k", "system/"+frpcLabel)
	_ = launchctl("kickstart", "-k", "system/"+serverLabel)

	return "Started the Prism server and frpc."
}

// RestartServer restarts the server LaunchDaemon.
func RestartServer() string {
	username, err := currentUsername()
	if err != nil {
		return fmt.Sprintf("Failed to restart server: %v", err)
	}
	if err := launchctl("kickstart", "-k", "system/"+fmt.Sprintf(launchDaemonServerLabel, username)); err != nil {
		return fmt.Sprintf("Failed to restart server: %v", err)
	}
	return "Restarted the Prism server."
}

// RestartFRPC restarts the frpc LaunchDaemon.
func RestartFRPC() string {
	username, err := currentUsername()
	if err != nil {
		return fmt.Sprintf("Failed to restart frpc: %v", err)
	}
	if err := launchctl("kickstart", "-k", "system/"+fmt.Sprintf(launchDaemonFRPCLabel, username)); err != nil {
		return fmt.Sprintf("Failed to restart frpc: %v", err)
	}
	return "Restarted frpc."
}

func currentUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(u.Username) == "" {
		return "", fmt.Errorf("empty username for current user")
	}
	return u.Username, nil
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w (output=%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// launchctlBootstrap wraps launchctl bootstrap and tolerates "already bootstrapped" errors.
func launchctlBootstrap(domain, plistPath string) error {
	out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if strings.Contains(output, "already bootstrapped") || strings.Contains(output, "EEXIST") {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap %s %s: %w (output=%s)", domain, plistPath, err, output)
	}
	return nil
}
