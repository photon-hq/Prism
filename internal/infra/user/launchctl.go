//go:build darwin

package userinfra

import (
	"context"
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

// StopAllServices stops the per-user server and frpc LaunchAgents.
func StopAllServices() string {
	username, domain, err := currentUserLaunchDomain()
	if err != nil {
		return fmt.Sprintf("Failed to stop services: %v", err)
	}
	serverLabel := fmt.Sprintf("%s/"+launchAgentServerLabelPattern, domain, username)
	frpcLabel := fmt.Sprintf("%s/"+launchAgentFRPCLabelPattern, domain, username)

	_ = runLaunchctl("bootout", serverLabel)
	_ = runLaunchctl("bootout", frpcLabel)

	return "Attempted to stop the local Prism server and frpc (ignoring if they were already stopped)."
}

// RestartServer restarts the per-user server LaunchAgent.
func RestartServer() string {
	username, domain, err := currentUserLaunchDomain()
	if err != nil {
		return fmt.Sprintf("Failed to restart server: %v", err)
	}
	label := fmt.Sprintf("%s/"+launchAgentServerLabelPattern, domain, username)
	if err := runLaunchctl("kickstart", "-k", label); err != nil {
		return fmt.Sprintf("Failed to restart server: %v", err)
	}
	return "Restarted the local Prism server."
}

// RestartFRPC restarts the per-user frpc LaunchAgent.
func RestartFRPC() string {
	username, domain, err := currentUserLaunchDomain()
	if err != nil {
		return fmt.Sprintf("Failed to restart frpc: %v", err)
	}
	label := fmt.Sprintf("%s/"+launchAgentFRPCLabelPattern, domain, username)
	if err := runLaunchctl("kickstart", "-k", label); err != nil {
		return fmt.Sprintf("Failed to restart frpc: %v", err)
	}
	return "Restarted the local frpc."
}

func currentUserLaunchDomain() (string, string, error) {
	u, err := user.Current()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(u.Uid) == "" {
		return "", "", fmt.Errorf("empty uid for current user")
	}
	return u.Username, fmt.Sprintf("gui/%s", u.Uid), nil
}

func runLaunchctl(args ...string) error {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "launchctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w (output=%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
