//go:build darwin

package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	hostAutobootLabel      = "com.prism.host-autoboot"
	hostAutobootProgramArg = "host-autoboot"
	hostAutobootPlistPath  = "/Library/LaunchDaemons/" + hostAutobootLabel + ".plist"
	hostAutobootLogPath    = "/var/log/prism-host-autoboot.log"
	hostAutobootErrLogPath = "/var/log/prism-host-autoboot.err.log"
)

const hostAutobootPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
      <string>%s</string>
      <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
      <key>SuccessfulExit</key>
      <false/>
    </dict>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
  </dict>
</plist>
`

// EnsureHostAutobootDaemon installs the system-wide host-autoboot LaunchDaemon.
// workingDir should point to the directory containing .env for godotenv.Load().
func EnsureHostAutobootDaemon(ctx context.Context, prismPath, workingDir string) error {
	if strings.TrimSpace(prismPath) == "" {
		return errors.New("prismPath is empty")
	}

	if workingDir == "" {
		workingDir = filepath.Dir(prismPath)
	}

	plist := fmt.Sprintf(hostAutobootPlistTemplate,
		hostAutobootLabel,
		prismPath,
		hostAutobootProgramArg,
		workingDir,
		hostAutobootLogPath,
		hostAutobootErrLogPath,
	)

	if err := os.WriteFile(hostAutobootPlistPath, []byte(plist), 0o644); err != nil {
		if os.IsPermission(err) {
			return nil
		}
		return fmt.Errorf("write host-autoboot plist: %w", err)
	}

	// Try to bootout first to ensure we reload the config if it changed
	_ = exec.CommandContext(ctx, "launchctl", "bootout", "system", hostAutobootPlistPath).Run()

	cmd := exec.CommandContext(ctx, "launchctl", "bootstrap", "system", hostAutobootPlistPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" && (strings.Contains(output, "already bootstrapped") || strings.Contains(output, "EEXIST")) {
			return nil
		}
		if strings.Contains(strings.ToLower(output), "operation not permitted") || strings.Contains(strings.ToLower(output), "permission denied") {
			return nil
		}
		if strings.Contains(err.Error(), "exit status 5") {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap system: %w (output=%s)", err, output)
	}

	return nil
}
