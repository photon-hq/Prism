//go:build darwin

package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	hostAutobootLabel      = "com.prism.host-autoboot"
	hostAutobootProgramArg = "host-autoboot"
	hostAutobootPlistPath  = "/Library/LaunchDaemons/" + hostAutobootLabel + ".plist"
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
  </dict>
</plist>
`

// EnsureHostAutobootDaemon installs the system-wide host-autoboot LaunchDaemon.
func EnsureHostAutobootDaemon(ctx context.Context, prismPath string) error {
	if strings.TrimSpace(prismPath) == "" {
		return errors.New("prismPath is empty")
	}

	plist := fmt.Sprintf(hostAutobootPlistTemplate, hostAutobootLabel, prismPath, hostAutobootProgramArg)
	if err := os.WriteFile(hostAutobootPlistPath, []byte(plist), 0o644); err != nil {
		if os.IsPermission(err) {
			return nil
		}
		return fmt.Errorf("write host-autoboot plist: %w", err)
	}

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
