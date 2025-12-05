//go:build darwin

package host

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"prism/internal/infra/state"
)

const (
	userAutobootLabel      = "com.prism.autoboot"
	userAutobootPlistName  = userAutobootLabel + ".plist"
	userFrpcPlistPattern   = "com.imsg.frpc.%s.plist"
	userServerPlistPattern = "com.imsg.server.%s.plist"

	launchctlAlreadyBootstrapped = "already bootstrapped"
	launchctlEexist              = "EEXIST"
)

// RunAutoboot loads per-user LaunchAgents for all Prism-managed users.
func RunAutoboot(statePath string) {
	st, err := state.Load(statePath)
	if err != nil {
		log.Printf("[host-autoboot] load state: %v", err)
		return
	}

	if len(st.Users) == 0 {
		log.Printf("[host-autoboot] no users in state; nothing to do")
		return
	}

	for _, u := range st.Users {
		if err := bootstrapUserAutoboot(u.Name); err != nil {
			log.Printf("[host-autoboot] user %s: %v", u.Name, err)
		}
	}
}

func bootstrapUserAutoboot(username string) error {
	sysUser, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", username, err)
	}
	uid := sysUser.Uid
	domain := fmt.Sprintf("gui/%s", uid)
	homeDir := filepath.Join("/Users", username)
	agentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")

	autobootPlist := filepath.Join(agentsDir, userAutobootPlistName)
	if err := bootstrapPlist(domain, autobootPlist); err != nil {
		return fmt.Errorf("bootstrap com.prism.autoboot for %s: %w", username, err)
	}

	frpcPlist := filepath.Join(agentsDir, fmt.Sprintf(userFrpcPlistPattern, username))
	if _, err := os.Stat(frpcPlist); err == nil {
		if err := bootstrapPlist(domain, frpcPlist); err != nil {
			return fmt.Errorf("bootstrap com.imsg.frpc.%s: %w", username, err)
		}
	}

	serverPlist := filepath.Join(agentsDir, fmt.Sprintf(userServerPlistPattern, username))
	if _, err := os.Stat(serverPlist); err == nil {
		if err := bootstrapPlist(domain, serverPlist); err != nil {
			return fmt.Errorf("bootstrap com.imsg.server.%s: %w", username, err)
		}
	}

	return nil
}

func bootstrapPlist(domain, plistPath string) error {
	cmd := exec.Command("launchctl", "bootstrap", domain, plistPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" && (strings.Contains(output, launchctlAlreadyBootstrapped) || strings.Contains(output, launchctlEexist)) {
			return nil
		}
		return fmt.Errorf("launchctl bootstrap %s %s: %w (output=%s)", domain, plistPath, err, output)
	}

	label := fmt.Sprintf("%s/%s", domain, strings.TrimSuffix(filepath.Base(plistPath), ".plist"))
	_ = exec.Command("launchctl", "enable", label).Run()

	return nil
}

func restartUserLaunchAgents(username string) error {
	sysUser, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", username, err)
	}
	uid := sysUser.Uid
	domain := fmt.Sprintf("gui/%s", uid)
	homeDir := filepath.Join("/Users", username)
	agentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")

	frpcPlist := filepath.Join(agentsDir, fmt.Sprintf(userFrpcPlistPattern, username))
	if _, err := os.Stat(frpcPlist); err == nil {
		if err := kickstartPlist(domain, frpcPlist); err != nil {
			return fmt.Errorf("kickstart com.imsg.frpc.%s: %w", username, err)
		}
	}

	serverPlist := filepath.Join(agentsDir, fmt.Sprintf(userServerPlistPattern, username))
	if _, err := os.Stat(serverPlist); err == nil {
		if err := kickstartPlist(domain, serverPlist); err != nil {
			return fmt.Errorf("kickstart com.imsg.server.%s: %w", username, err)
		}
	}

	return nil
}

func kickstartPlist(domain, plistPath string) error {
	label := fmt.Sprintf("%s/%s", domain, strings.TrimSuffix(filepath.Base(plistPath), ".plist"))
	cmd := exec.Command("launchctl", "kickstart", "-k", label)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		return fmt.Errorf("launchctl kickstart %s: %w (output=%s)", label, err, output)
	}

	return nil
}
