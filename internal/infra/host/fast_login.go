//go:build darwin

package host

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	fastLoginLabel          = "com.prism.fast-login"
	fastLoginScriptFilename = "prism-fast-login.sh"
)

// fastLoginScriptTemplate spawns VNC sessions for sub-users to activate their GUI.
const fastLoginScriptTemplate = `#!/bin/bash
# Prism Fast Login - activates sub-user GUI sessions via VNC loopback with SSH Tunnel
#
# PREREQUISITE: "Remote Login" must be enabled in System Settings -> General -> Sharing

ALL_USERS=(%s)
PASSWORD="%s"
TUNNEL_PORT=5901
LOG_FILE="/tmp/prism_tunnel.log"

# Function to start SSH tunnel
start_tunnel() {
    # Check if tunnel is already active
    # We check the BASE port 5901
    if lsof -i :$TUNNEL_PORT >/dev/null; then
        echo "Tunnel occupied on port $TUNNEL_PORT. Killing stale process..."
        lsof -ti :$TUNNEL_PORT | xargs kill -9
        sleep 1
    fi

    # Prerequisite: Kill any existing Screen Sharing app to avoid "No window" confusion
    killall "Screen Sharing" >/dev/null 2>&1 || true

    local tunnel_user="${ALL_USERS[0]}"

    # Construct multi-port forwarding args
    # Loop users to create -L 5901:localhost:5900 -L 5902:localhost:5900 ...
    local ssh_forwarding_opts=""
    local i=0
    for _ in "${ALL_USERS[@]}"; do
        local port=$((TUNNEL_PORT + i))
        ssh_forwarding_opts="$ssh_forwarding_opts -L $port:localhost:5900"
        ((i++))
    done

    echo "Starting SSH tunnel via $tunnel_user with opts: $ssh_forwarding_opts"
    echo " Debug log: $LOG_FILE"

    /usr/bin/expect <<EOF > "$LOG_FILE" 2>&1 &
      exp_internal 0
      # Set timeout to infinite so the tunnel stays open
      set timeout -1
      spawn ssh -N $ssh_forwarding_opts -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null $tunnel_user@localhost
      expect {
        "*ssword:" {
          send "$PASSWORD\r"
          expect eof
        }
        "refused" { exit 1 }
        eof { exit 1 }
      }
EOF
}

# Start the tunnel before looping users
start_tunnel
# Sleep 5s to allow SSH auth to complete
sleep 5

spawn_session() {
    local target_user=$1
    local port=$2
    echo "Spawning session for $target_user on port $port..."

    # Connect (No -n, reuse app to simplify scripting)
    open "vnc://127.0.0.1:$port"

    # Wait for app launch and connection handshake
    sleep 5

    osascript <<EOF
      tell application "Screen Sharing" to activate
      delay 1
      tell application "System Events"
        tell process "Screen Sharing"
          set frontmost to true

          -- Wait for the authentication window to appear (up to 10s)
          repeat 20 times
            if exists window 1 then exit repeat
            delay 0.5
          end repeat

          if exists window 1 then
             log "Found window: " & (get name of window 1)
             tell window 1
               -- Ensure we are typing into the window
               delay 0.5
               keystroke "${target_user}"
               delay 0.5
               keystroke tab
               delay 0.5
               keystroke "${PASSWORD}"
               delay 0.5
               keystroke return
             end tell
          else
             log "No window found. Visible windows: " & (get name of every window)
          end if
        end tell
      end tell
EOF

    # Extra delay to allow login to proceed before next iteration
    sleep 5

    osascript <<EOF
      -- Attempt to handle "Log in as..." or subsequent dialogs
      tell application "System Events"
        tell process "Screen Sharing"
           if exists window 1 then
              keystroke return
           end if
        end tell
      end tell

      -- Hide windows
      try
        tell application "Screen Sharing"
          set visible of every window to false
        end tell
      end try
EOF
}

i=0
for user in "${ALL_USERS[@]}"; do
    port=$((TUNNEL_PORT + i))
    spawn_session "$user" "$port"
    ((i++))
    sleep 5
done

# Final cleanup: Close Screen Sharing app to clean up the desktop
# The sub-user sessions will remain active in the background.
sleep 5
killall "Screen Sharing" || true
`

const fastLoginPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.prism.fast-login</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>
`

// FastLoginConfig holds configuration for the Fast Login spawner.
type FastLoginConfig struct {
	AdminUser   string
	TargetUsers []string
	Password    string
}

// EnsureFastLoginService installs the spawner script and LaunchAgent for the admin user.
func EnsureFastLoginService(cfg FastLoginConfig) error {
	homeDir := filepath.Join("/Users", cfg.AdminUser)
	scriptPath := filepath.Join(homeDir, fastLoginScriptFilename)
	launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	plistPath := filepath.Join(launchAgentsDir, fastLoginLabel+".plist")
	logsDir := filepath.Join(homeDir, "Library", "Logs")

	// If no users to login, clean up any existing artifacts to ensure we don't run stale scripts
	if len(cfg.TargetUsers) == 0 {
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove plist: %w", err)
		}
		if err := os.Remove(scriptPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove script: %w", err)
		}
		return nil
	}

	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := chownRecursive(cfg.AdminUser, launchAgentsDir); err != nil {
		return fmt.Errorf("chown LaunchAgents dir: %w", err)
	}
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return fmt.Errorf("create Logs dir: %w", err)
	}
	if err := chownRecursive(cfg.AdminUser, logsDir); err != nil {
		return fmt.Errorf("chown Logs dir: %w", err)
	}

	var usersStr string
	for _, u := range cfg.TargetUsers {
		usersStr += fmt.Sprintf("\"%s\" ", u)
	}

	scriptContent := fmt.Sprintf(fastLoginScriptTemplate, usersStr, cfg.Password)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o700); err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	if err := chownRecursive(cfg.AdminUser, scriptPath); err != nil {
		return fmt.Errorf("chown script: %w", err)
	}

	stdoutLog := filepath.Join(logsDir, "prism-fast-login.log")
	stderrLog := filepath.Join(logsDir, "prism-fast-login.err.log")
	plistContent := fmt.Sprintf(fastLoginPlistTemplate, scriptPath, stdoutLog, stderrLog)
	if err := os.WriteFile(plistPath, []byte(plistContent), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	if err := chownRecursive(cfg.AdminUser, plistPath); err != nil {
		return fmt.Errorf("chown plist: %w", err)
	}

	return nil
}
