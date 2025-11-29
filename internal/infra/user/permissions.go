//go:build darwin

package userinfra

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PrewarmPermissions performs permission prewarm for the current macOS user.
func PrewarmPermissions() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Sprintf("Permission prewarm failed: unable to determine user home directory: %v", err)
	}

	var warns []string

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if out, err := exec.CommandContext(
		ctx,
		"defaults",
		"read",
		"/Library/Preferences/com.apple.security.libraryvalidation.plist",
		"DisableLibraryValidation",
	).CombinedOutput(); err != nil {
		warns = append(
			warns,
			"Unable to read DisableLibraryValidation; please set it to true on "+
				"the Host side following the Preflight instructions.",
		)
	} else {
		val := strings.ToLower(strings.TrimSpace(string(out)))
		if val != "1" && val != "true" {
			warns = append(
				warns,
				fmt.Sprintf(
					"DisableLibraryValidation is currently %q; it is recommended to set it to true/1.",
					val,
				),
			)
		}
	}

	msgDir := filepath.Join(home, "Library", "Messages")
	if fi, err := os.Stat(msgDir); err != nil || !fi.IsDir() {
		warns = append(
			warns,
			"Could not find ~/Library/Messages; it looks like Messages has not been used yet. "+
				"Please open Messages with this account and send at least one iMessage so Prism can later detect your phone number or email.",
		)
	} else {
		if _, err := os.ReadDir(msgDir); err != nil {
			warns = append(
				warns,
				"Unable to list ~/Library/Messages; please ensure the terminal/app "+
					"running Prism has Full Disk Access.",
			)
		}
		chatDB := filepath.Join(msgDir, "chat.db")
		if f, err := os.Open(chatDB); err != nil {
			warns = append(
				warns,
				"Could not open ~/Library/Messages/chat.db; Messages may not have been used yet. "+
					"If this is a new iMessage account, open Messages and send at least one iMessage so Prism can later detect your phone number or email.",
			)
		} else {
			buf := make([]byte, 4096)
			_, _ = f.Read(buf)
			_ = f.Close()
		}
	}

	runOSA := func(desc, script string) {
		cmd := exec.CommandContext(ctx, "osascript", "-e", script)
		if err := cmd.Run(); err != nil {
			warns = append(warns, fmt.Sprintf("%s may not be authorized yet (osascript failed).", desc))
		}
	}

	runOSA("Messages automation", "tell application \"Messages\"\nactivate\ntry\nget name of first chat\nend try\nend tell")
	runOSA("System Events accessibility", "tell application \"System Events\"\nset _ to name of first process\nend tell")

	markerDir := filepath.Join(home, ".prism")
	markerPath := filepath.Join(markerDir, "perms-prewarmed")
	_ = os.MkdirAll(markerDir, 0o700)
	_ = os.WriteFile(markerPath, []byte(time.Now().Format(time.RFC3339)), 0o600)

	if len(warns) == 0 {
		return "Permission prewarm completed: checked DisableLibraryValidation and attempted to access Messages and System Events. If you continue to see permission prompts, please grant access in System Settings."
	}

	return "Permission prewarm completed, but some items may require manual attention:\n- " + strings.Join(warns, "\n- ")
}
