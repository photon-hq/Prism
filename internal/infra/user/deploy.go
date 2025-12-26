//go:build darwin

package userinfra

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	inframacos "prism/internal/infra/host"
)

type userServiceConfig struct {
	Username   string `json:"username"`
	MachineID  string `json:"machine_id"`
	LocalPort  int    `json:"local_port"`
	FullDomain string `json:"full_domain"`
	NexusAddr  string `json:"nexus_addr"`
	FRPCConfig string `json:"frpc_config"`
}

// Deploy verifies configuration, ensures friendly name, and performs health check.
// LaunchDaemons should already be created by Host provisioning.
func Deploy() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Sprintf("Deploy failed: unable to determine user home directory: %v", err)
	}
	serviceDir := filepath.Join(home, "services", "imsg")

	cfg, errMsg := loadUserServiceConfig(serviceDir)
	if errMsg != "" {
		return errMsg
	}

	friendlyNote, errMsg := ensureFRPCFriendlyName(cfg.FRPCConfig)
	if errMsg != "" {
		return errMsg
	}

	nodeNote := nodeVersionNote()

	u, err := user.Current()
	if err != nil {
		return fmt.Sprintf("Deploy failed: unable to get current user: %v", err)
	}

	// Check if LaunchDaemons exist
	serverLabel := fmt.Sprintf(launchDaemonServerLabel, u.Username)
	frpcLabel := fmt.Sprintf(launchDaemonFRPCLabel, u.Username)
	serverPlistPath := filepath.Join("/Library/LaunchDaemons", serverLabel+".plist")

	if _, err := os.Stat(serverPlistPath); err != nil {
		return fmt.Sprintf("Deploy failed: LaunchDaemon not found: %s\n\nPlease run the Host setup first (sudo ./prism) to create LaunchDaemons.", serverPlistPath)
	}

	// Kickstart the services to ensure they're running
	if err := launchctl("kickstart", "-k", "system/"+frpcLabel); err != nil {
		return fmt.Sprintf("Deploy failed: could not start frpc: %v", err)
	}
	if err := launchctl("kickstart", "-k", "system/"+serverLabel); err != nil {
		return fmt.Sprintf("Deploy failed: could not start server: %v", err)
	}

	healthURL := fmt.Sprintf("http://localhost:%d/health", cfg.LocalPort)
	if err := waitForHealth(healthURL, 10*time.Second); err != nil {
		return fmt.Sprintf("Deploy failed: local health check %s did not succeed: %v", healthURL, err)
	}

	// Deploy keepalive service (now that we know GUI is available)
	var keepaliveNote string
	if err := inframacos.EnsureKeepaliveService(u.Username); err != nil {
		keepaliveNote = fmt.Sprintf("\nWarning: failed to deploy keepalive: %v", err)
	} else {
		keepaliveNote = "\nKeepalive service deployed."
	}

	frpcLog := filepath.Join(home, "Library", "Logs", "frpc.log")
	serverLog := filepath.Join(home, "Library", "Logs", "imsg-server.log")

	return fmt.Sprintf(
		"Deploy succeeded: Prism server and frpc are running.\nLocal health OK: %s%s%s\n\nTo view logs:\n- tail -100 %s\n- tail -100 %s%s",
		healthURL,
		friendlyNote,
		keepaliveNote,
		frpcLog,
		serverLog,
		nodeNote,
	)
}

func waitForHealth(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		resp, err := client.Get(url) // #nosec G107 -- health endpoint is fixed, not user-controlled
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}

		if time.Now().After(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("health check %s returned status %s", url, resp.Status)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func loadUserServiceConfig(serviceDir string) (userServiceConfig, string) {
	configPath := filepath.Join(serviceDir, "config.json")
	frpcConfigPath := filepath.Join(serviceDir, "frpc.toml")

	if _, err := os.Stat(configPath); err != nil {
		return userServiceConfig{}, fmt.Sprintf("Deploy failed: config.json not found: %v", err)
	}
	if _, err := os.Stat(frpcConfigPath); err != nil {
		return userServiceConfig{}, fmt.Sprintf("Deploy failed: frpc.toml not found: %v", err)
	}

	var cfg userServiceConfig
	data, err := os.ReadFile(configPath)
	if err != nil {
		return userServiceConfig{}, fmt.Sprintf("Deploy failed: error reading config.json: %v", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return userServiceConfig{}, fmt.Sprintf("Deploy failed: error parsing config.json: %v", err)
	}

	if cfg.LocalPort <= 0 {
		return userServiceConfig{}, "Deploy failed: invalid local_port in config.json."
	}
	if strings.TrimSpace(cfg.FullDomain) == "" {
		return userServiceConfig{}, "Deploy failed: full_domain is empty in config.json."
	}
	if strings.TrimSpace(cfg.MachineID) == "" {
		return userServiceConfig{}, "Deploy failed: machine_id is empty in config.json."
	}
	if strings.TrimSpace(cfg.Username) == "" {
		u, _ := user.Current()
		cfg.Username = u.Username
	}
	if strings.TrimSpace(cfg.FRPCConfig) == "" {
		cfg.FRPCConfig = frpcConfigPath
	}

	return cfg, ""
}

func ensureFRPCFriendlyName(path string) (string, string) {
	hasFriendly := hasNonEmptyFriendlyName(path)
	friendly := ""
	friendlyNote := ""
	if !hasFriendly {
		friendly = strings.TrimSpace(autoDetectFriendlyName())
		if friendly != "" {
			if err := setFRPCFriendlyName(path, friendly); err != nil {
				return "", fmt.Sprintf("Deploy failed: unable to update frpc friendly name: %v", err)
			}
			friendlyNote = fmt.Sprintf("\nDetected friendly name: %s", friendly)
		}
	}

	if !hasFriendly && friendly == "" {
		return "", "Deploy failed: could not determine a friendly name (phone number or email).\n\n" +
			"To continue, please either:\n" +
			"1. Open Messages with this account and send at least one iMessage, then try \"Deploy / start services\" again, or\n" +
			"2. Open './prism user' and use \"Rename friendly name\" to set your phone number or email manually, then rerun Deploy."
	}

	return friendlyNote, ""
}

func nodeVersionNote() string {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return ""
	}

	cmd := exec.Command(nodeBin, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	ver := strings.TrimSpace(string(out))
	if !strings.HasPrefix(ver, "v") {
		return ""
	}

	parts := strings.SplitN(ver[1:], ".", 2)
	if len(parts) == 0 {
		return ""
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil || major == 18 {
		return ""
	}

	return fmt.Sprintf("\nNote: detected Node version %s; Node v18 is recommended for best compatibility.", ver)
}
