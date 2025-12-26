//go:build darwin

package macos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Check represents the result of a single preflight check.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// PreflightResult aggregates all checks performed for a host.
type PreflightResult struct {
	Checks        []Check `json:"checks"`
	NeedsReboot   bool    `json:"needs_reboot"`
	RebootSkipped bool    `json:"reboot_skipped"`
}

var (
	requiredBootArgs = []string{
		"amfi_get_out_of_my_way=1",
		"amfi_allow_any_signature=1",
		"-arm64e_preview_abi",
		"ipc_control_port_options=0",
	}
	bootArgsValue = strings.Join(requiredBootArgs, " ")

	sipDisableSteps = "1. Restart and hold Command+R to enter Recovery Mode.\n" +
		"2. Open Terminal from Utilities menu.\n" +
		"3. Run: csrutil disable\n" +
		"4. Restart and retry."
)

// containsAll returns missing items from required that are not in s.
func containsAll(s string, required []string) []string {
	var missing []string
	for _, r := range required {
		if !strings.Contains(s, r) {
			missing = append(missing, r)
		}
	}
	return missing
}

func checkSIP(ctx context.Context) Check {
	out, err := exec.CommandContext(ctx, "csrutil", "status").CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	if err != nil || !strings.Contains(strings.ToLower(outStr), "disabled") {
		return Check{
			Name:   "SIP disabled",
			OK:     false,
			Detail: "SIP is enabled. " + sipDisableSteps + "\n\nOutput: " + outStr,
		}
	}
	return Check{Name: "SIP disabled", OK: true, Detail: outStr}
}

func checkAndFixBootArgs(ctx context.Context) (Check, bool) {
	out, _ := exec.CommandContext(ctx, "nvram", "boot-args").CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	if missing := containsAll(outStr, requiredBootArgs); len(missing) == 0 {
		return Check{Name: "boot-args", OK: true, Detail: outStr}, false
	}

	// Auto-fix
	fmt.Printf("\n[preflight] Setting boot-args: %s\n", bootArgsValue)
	if out, err := exec.CommandContext(ctx, "nvram", "boot-args="+bootArgsValue).CombinedOutput(); err != nil {
		return Check{Name: "boot-args", OK: false, Detail: fmt.Sprintf("Failed: %v\n%s", err, out)}, false
	}

	// Verify
	out, _ = exec.CommandContext(ctx, "nvram", "boot-args").CombinedOutput()
	outStr = strings.TrimSpace(string(out))
	if missing := containsAll(outStr, requiredBootArgs); len(missing) > 0 {
		return Check{Name: "boot-args", OK: false, Detail: "Verification failed: " + outStr}, false
	}

	return Check{Name: "boot-args", OK: true, Detail: "Auto-configured: " + outStr}, true
}

func checkAndFixLibraryValidation(ctx context.Context) (Check, bool) {
	const plist = "/Library/Preferences/com.apple.security.libraryvalidation.plist"
	const key = "DisableLibraryValidation"

	out, err := exec.CommandContext(ctx, "defaults", "read", plist, key).CombinedOutput()
	if err == nil && strings.TrimSpace(strings.ToLower(string(out))) == "1" {
		return Check{Name: key, OK: true, Detail: "1"}, false
	}

	// Auto-fix
	fmt.Printf("\n[preflight] Setting %s: true\n", key)
	if out, err := exec.CommandContext(ctx, "defaults", "write", plist, key, "-bool", "true").CombinedOutput(); err != nil {
		return Check{Name: key, OK: false, Detail: fmt.Sprintf("Failed: %v\n%s", err, out)}, false
	}

	// Verify
	out, _ = exec.CommandContext(ctx, "defaults", "read", plist, key).CombinedOutput()
	if strings.TrimSpace(strings.ToLower(string(out))) != "1" {
		return Check{Name: key, OK: false, Detail: "Verification failed"}, false
	}

	return Check{Name: key, OK: true, Detail: "Auto-configured: 1"}, true
}

func rebootWithCountdown() bool {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("Settings changed. Rebooting in 10s...")
	fmt.Println("After reboot, run `sudo ./prism` again.")
	fmt.Println(strings.Repeat("=", 50) + "\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	for i := 10; i > 0; i-- {
		select {
		case <-sigCh:
			fmt.Println("\n\nReboot cancelled. Please reboot manually.")
			return true
		default:
			fmt.Printf("\r  %d seconds... (Ctrl+C to cancel)", i)
			time.Sleep(time.Second)
		}
	}

	fmt.Println("\n\nRebooting...")
	_ = exec.Command("shutdown", "-r", "now").Run()
	return false
}

// Preflight verifies SIP, boot-args, and DisableLibraryValidation.
func Preflight(ctx context.Context) (PreflightResult, error) {
	sipCheck := checkSIP(ctx)
	bootCheck, bootReboot := checkAndFixBootArgs(ctx)
	libCheck, libReboot := checkAndFixLibraryValidation(ctx)

	res := PreflightResult{
		Checks:      []Check{sipCheck, bootCheck, libCheck},
		NeedsReboot: bootReboot || libReboot,
	}

	// Collect failures
	var failed []string
	for _, c := range res.Checks {
		if !c.OK {
			failed = append(failed, c.Name)
		}
	}
	if len(failed) > 0 {
		return res, fmt.Errorf("preflight failed: %s", strings.Join(failed, ", "))
	}

	// Trigger reboot if needed
	if res.NeedsReboot {
		res.RebootSkipped = rebootWithCountdown()
		if !res.RebootSkipped {
			os.Exit(0)
		}
		return res, fmt.Errorf("reboot cancelled - please reboot manually")
	}

	return res, nil
}
