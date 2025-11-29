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
)

const (
	launchAgentServerLabelPattern = "com.imsg.server.%s"
	launchAgentFRPCLabelPattern   = "com.imsg.frpc.%s"
)

const userServerLaunchAgentPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.imsg.server.%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>EnvironmentVariables</key>
    <dict>
      <key>NODE_ENV</key>
      <string>production</string>
      <key>PORT</key>
      <string>%d</string>
      <key>MACHINE_ID</key>
      <string>%s</string>
      <key>NEXUS_BASE_URL</key>
      <string>%s</string>
      <key>PATH</key>
      <string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/opt/homebrew/opt/node@18/bin</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
  </dict>
</plist>
`

const userFRPCLaunchAgentPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.imsg.frpc.%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
      <string>-c</string>
      <string>%s</string>
    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
  </dict>
</plist>
`

type userServiceConfig struct {
	Username   string `json:"username"`
	MachineID  string `json:"machine_id"`
	LocalPort  int    `json:"local_port"`
	FullDomain string `json:"full_domain"`
	NexusAddr  string `json:"nexus_addr"`
	FRPCConfig string `json:"frpc_config"`
}

// Deploy creates per-user LaunchAgents for the server and frpc, then starts them.
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

	imsgBin, errMsg := ensureServerWrapperBinary(serviceDir)
	if errMsg != "" {
		return errMsg
	}

	frpcBin, errMsg := locateFRPCBinary()
	if errMsg != "" {
		return errMsg
	}

	nodeNote := nodeVersionNote()

	username, domain, err := currentUserLaunchDomain()
	if err != nil {
		return fmt.Sprintf("Deploy failed: unable to get current user info: %v", err)
	}

	frpcPlistPath, serverPlistPath, errMsg := writeUserLaunchAgents(home, serviceDir, username, cfg, frpcBin, imsgBin)
	if errMsg != "" {
		return errMsg
	}

	if errMsg := restartUserLaunchAgents(domain, username, frpcPlistPath, serverPlistPath); errMsg != "" {
		return errMsg
	}

	healthURL := fmt.Sprintf("http://localhost:%d/health", cfg.LocalPort)
	if err := waitForHealth(healthURL, 10*time.Second); err != nil {
		return fmt.Sprintf("Deploy failed: local health check %s did not succeed: %v", healthURL, err)
	}

	frpcLog := filepath.Join(home, "Library", "Logs", "frpc.log")
	serverLog := filepath.Join(home, "Library", "Logs", "imsg-server.log")

	return fmt.Sprintf(
		"Deploy succeeded: Prism server and frpc have been started.\nLocal health OK: %s%s\n\nTo view logs:\n- tail -100 %s\n- tail -100 %s%s",
		healthURL,
		friendlyNote,
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

func ensureServerWrapperBinary(serviceDir string) (string, string) {
	imsgBin := filepath.Join(serviceDir, "iMessageKitServer.app", "Contents", "MacOS", "iMessageKitServer")
	if fi, err := os.Stat(imsgBin); err != nil || fi.Mode()&0o111 == 0 {
		return "", fmt.Sprintf("Deploy failed: iMessageKitServer wrapper not found or not executable: %s", imsgBin)
	}
	return imsgBin, ""
}

func locateFRPCBinary() (string, string) {
	frpcBin, err := exec.LookPath("frpc")
	if err != nil {
		return "", "Deploy failed: frpc not found; please complete dependency installation from the Host TUI first."
	}
	return frpcBin, ""
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

func writeUserLaunchAgents(home, serviceDir, username string, cfg userServiceConfig, frpcBin, imsgBin string) (string, string, string) {
	agentsDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return "", "", fmt.Sprintf("Deploy failed: unable to create LaunchAgents directory: %v", err)
	}

	serverPlistPath := filepath.Join(agentsDir, fmt.Sprintf(launchAgentServerLabelPattern+".plist", username))
	serverPlist := fmt.Sprintf(
		userServerLaunchAgentPlistTemplate,
		username,
		imsgBin,
		serviceDir,
		cfg.LocalPort,
		cfg.MachineID,
		strings.TrimRight(cfg.NexusAddr, "/"),
		filepath.Join(home, "Library", "Logs", "imsg-server.log"),
		filepath.Join(home, "Library", "Logs", "imsg-server.err"),
	)

	if err := os.WriteFile(serverPlistPath, []byte(serverPlist), 0o644); err != nil {
		return "", "", fmt.Sprintf("Deploy failed: error writing server LaunchAgent: %v", err)
	}

	frpcPlistPath := filepath.Join(agentsDir, fmt.Sprintf(launchAgentFRPCLabelPattern+".plist", username))
	frpcPlist := fmt.Sprintf(
		userFRPCLaunchAgentPlistTemplate,
		username,
		frpcBin,
		cfg.FRPCConfig,
		serviceDir,
		filepath.Join(home, "Library", "Logs", "frpc.log"),
		filepath.Join(home, "Library", "Logs", "frpc.err"),
	)

	if err := os.WriteFile(frpcPlistPath, []byte(frpcPlist), 0o644); err != nil {
		return "", "", fmt.Sprintf("Deploy failed: error writing frpc LaunchAgent: %v", err)
	}

	return frpcPlistPath, serverPlistPath, ""
}

func restartUserLaunchAgents(domain, username, frpcPlistPath, serverPlistPath string) string {
	frpcLabelFull := fmt.Sprintf("%s/"+launchAgentFRPCLabelPattern, domain, username)
	_ = runLaunchctl("bootout", frpcLabelFull)
	if err := runLaunchctl("bootstrap", domain, frpcPlistPath); err != nil {
		return fmt.Sprintf("Deploy failed: failed to bootstrap frpc LaunchAgent: %v", err)
	}

	serverLabelFull := fmt.Sprintf("%s/"+launchAgentServerLabelPattern, domain, username)
	_ = runLaunchctl("bootout", serverLabelFull)
	if err := runLaunchctl("bootstrap", domain, serverPlistPath); err != nil {
		return fmt.Sprintf("Deploy failed: failed to bootstrap server LaunchAgent: %v", err)
	}

	return ""
}
