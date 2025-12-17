//go:build darwin

package host

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	launchDaemonServerLabel = "com.imsg.server.%s"
	launchDaemonFRPCLabel   = "com.imsg.frpc.%s"
	launchDaemonsDir        = "/Library/LaunchDaemons"
)

// LaunchDaemon plist template for iMessage server.
// Uses UserName key to run as specific user at boot without login.
const serverLaunchDaemonTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.imsg.server.%s</string>
    <key>UserName</key>
    <string>%s</string>
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
      <key>HOME</key>
      <string>%s</string>
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

// LaunchDaemon plist template for frpc tunnel.
const frpcLaunchDaemonTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.imsg.frpc.%s</string>
    <key>UserName</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
      <string>-c</string>
      <string>%s</string>
    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>EnvironmentVariables</key>
    <dict>
      <key>HOME</key>
      <string>%s</string>
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

// UserLaunchDaemonConfig holds configuration for creating per-user LaunchDaemons.
type UserLaunchDaemonConfig struct {
	Username   string
	HomeDir    string
	ServiceDir string
	ServerBin  string
	FRPCBin    string
	FRPCConfig string
	LocalPort  int
	MachineID  string
	NexusAddr  string
}

// EnsureUserLaunchDaemons creates LaunchDaemon plist files in /Library/LaunchDaemons/.
// Uses UserName key to run services as specific user at boot without login.
func EnsureUserLaunchDaemons(cfg UserLaunchDaemonConfig) error {
	log.Printf("[launch_daemons] creating for %s", cfg.Username)

	logsDir := filepath.Join(cfg.HomeDir, "Library", "Logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}
	if err := chownRecursive(cfg.Username, logsDir); err != nil {
		return fmt.Errorf("chown logs dir: %w", err)
	}

	serverPlist := filepath.Join(launchDaemonsDir, fmt.Sprintf(launchDaemonServerLabel+".plist", cfg.Username))
	serverContent := fmt.Sprintf(serverLaunchDaemonTemplate,
		cfg.Username, cfg.Username, cfg.ServerBin, cfg.ServiceDir,
		cfg.LocalPort, cfg.MachineID, strings.TrimRight(cfg.NexusAddr, "/"), cfg.HomeDir,
		filepath.Join(logsDir, "imsg-server.log"), filepath.Join(logsDir, "imsg-server.err"),
	)
	if err := os.WriteFile(serverPlist, []byte(serverContent), 0o644); err != nil {
		return fmt.Errorf("write server plist: %w", err)
	}

	frpcPlist := filepath.Join(launchDaemonsDir, fmt.Sprintf(launchDaemonFRPCLabel+".plist", cfg.Username))
	frpcContent := fmt.Sprintf(frpcLaunchDaemonTemplate,
		cfg.Username, cfg.Username, cfg.FRPCBin, cfg.FRPCConfig, cfg.ServiceDir, cfg.HomeDir,
		filepath.Join(logsDir, "frpc.log"), filepath.Join(logsDir, "frpc.err"),
	)
	if err := os.WriteFile(frpcPlist, []byte(frpcContent), 0o644); err != nil {
		return fmt.Errorf("write frpc plist: %w", err)
	}

	return nil
}

// BootstrapUserLaunchDaemons loads LaunchDaemons into system domain.
// Includes retry logic for boot-time when launchd may not be fully ready.
func BootstrapUserLaunchDaemons(username string) error {
	serverPlist := filepath.Join(launchDaemonsDir, fmt.Sprintf(launchDaemonServerLabel+".plist", username))
	frpcPlist := filepath.Join(launchDaemonsDir, fmt.Sprintf(launchDaemonFRPCLabel+".plist", username))

	if _, err := os.Stat(frpcPlist); err == nil {
		if err := bootstrapWithRetry(frpcPlist, 3); err != nil {
			return fmt.Errorf("bootstrap frpc: %w", err)
		}
	}

	if _, err := os.Stat(serverPlist); err == nil {
		if err := bootstrapWithRetry(serverPlist, 3); err != nil {
			return fmt.Errorf("bootstrap server: %w", err)
		}
	}

	log.Printf("[launch_daemons] bootstrapped for %s", username)
	return nil
}

// RemoveUserLaunchDaemons unloads and deletes LaunchDaemon files for a user.
func RemoveUserLaunchDaemons(username string) error {
	serverLabel := fmt.Sprintf(launchDaemonServerLabel, username)
	frpcLabel := fmt.Sprintf(launchDaemonFRPCLabel, username)

	_ = exec.Command("launchctl", "bootout", "system/"+serverLabel).Run()
	_ = exec.Command("launchctl", "bootout", "system/"+frpcLabel).Run()
	_ = os.Remove(filepath.Join(launchDaemonsDir, serverLabel+".plist"))
	_ = os.Remove(filepath.Join(launchDaemonsDir, frpcLabel+".plist"))

	return nil
}

// RestartUserDaemons restarts both server and frpc daemons for a user.
func RestartUserDaemons(username string) error {
	serverLabel := fmt.Sprintf(launchDaemonServerLabel, username)
	frpcLabel := fmt.Sprintf(launchDaemonFRPCLabel, username)

	var errs []string
	if out, err := exec.Command("launchctl", "kickstart", "-k", "system/"+frpcLabel).CombinedOutput(); err != nil {
		errs = append(errs, fmt.Sprintf("frpc: %v (%s)", err, strings.TrimSpace(string(out))))
	}
	if out, err := exec.Command("launchctl", "kickstart", "-k", "system/"+serverLabel).CombinedOutput(); err != nil {
		errs = append(errs, fmt.Sprintf("server: %v (%s)", err, strings.TrimSpace(string(out))))
	}

	if len(errs) > 0 {
		return fmt.Errorf("restart failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func bootstrapWithRetry(plistPath string, retries int) error {
	var lastErr error
	for i := 0; i <= retries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}
		if err := bootstrapDaemon(plistPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func bootstrapDaemon(plistPath string) error {
	out, err := exec.Command("launchctl", "bootstrap", "system", plistPath).CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if strings.Contains(output, "already bootstrapped") || strings.Contains(output, "EEXIST") {
			return nil
		}
		return fmt.Errorf("%s: %w (%s)", filepath.Base(plistPath), err, output)
	}

	label := strings.TrimSuffix(filepath.Base(plistPath), ".plist")
	_ = exec.Command("launchctl", "enable", "system/"+label).Run()
	return nil
}
