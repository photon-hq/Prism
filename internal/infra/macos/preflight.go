//go:build darwin

package macos

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Check represents the result of a single preflight check.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// PreflightResult aggregates all checks performed for a host.
type PreflightResult struct {
	Checks []Check `json:"checks"`
}

const (
	bootArgsCommand                 = "sudo nvram boot-args=\"amfi_get_out_of_my_way=1 amfi_allow_any_signature=1 -arm64e_preview_abi ipc_control_port_options=0\""
	disableLibraryValidationCommand = "sudo defaults write /Library/Preferences/com.apple.security.libraryvalidation.plist DisableLibraryValidation -bool true"
	sipDisableRecoverySteps         = "" +
		"1. Restart your Mac and hold Command+R to enter Recovery Mode.\n" +
		"2. Open Terminal from the Utilities menu.\n" +
		"3. Run: csrutil disable\n" +
		"4. Restart back into the normal system and retry."
)

func isSIPDisabled(output string) bool {
	return strings.Contains(strings.ToLower(output), "disabled")
}

func checkSIP(ctx context.Context) Check {
	check := Check{Name: "SIP disabled"}

	out, err := exec.CommandContext(ctx, "csrutil", "status").CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	if err != nil {
		check.OK = false
		check.Detail = fmt.Sprintf(
			"Failed to run `csrutil status` (%v). Please disable SIP in Recovery Mode:\n%s",
			err,
			sipDisableRecoverySteps,
		)
		return check
	}

	if !isSIPDisabled(outStr) {
		check.OK = false
		check.Detail = "System Integrity Protection (SIP) is currently enabled. " +
			"Please follow these steps to disable it:\n" +
			sipDisableRecoverySteps +
			"\n\n" +
			"Output of `csrutil status`:\n" + outStr
		return check
	}

	check.OK = true
	check.Detail = outStr
	return check
}

func checkBootArgs(ctx context.Context) Check {
	check := Check{Name: "boot-args required flags"}

	out, err := exec.CommandContext(ctx, "nvram", "boot-args").CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	if err != nil {
		check.OK = false
		check.Detail = fmt.Sprintf(
			"Failed to read boot-args (%v). Please run the following command in a normal system session, then reboot:\n\n"+
				"%s\n\n"+
				"Command output: %q",
			err,
			bootArgsCommand,
			outStr,
		)
		return check
	}

	required := []string{
		"amfi_get_out_of_my_way=1",
		"amfi_allow_any_signature=1",
		"-arm64e_preview_abi",
		"ipc_control_port_options=0",
	}
	var missing []string
	for _, flag := range required {
		if !strings.Contains(outStr, flag) {
			missing = append(missing, flag)
		}
	}

	if len(missing) > 0 {
		check.OK = false
		check.Detail = fmt.Sprintf(
			"boot-args is missing the following required flags: %s\n\n"+
				"Please run the following command in Terminal on a normal system, then reboot:\n\n"+
				"%s\n\n"+
				"Current boot-args output: %q",
			strings.Join(missing, ", "),
			bootArgsCommand,
			outStr,
		)
		return check
	}

	check.OK = true
	check.Detail = outStr
	return check
}

func checkDisableLibraryValidation(ctx context.Context) Check {
	check := Check{Name: "DisableLibraryValidation true"}

	out, err := exec.CommandContext(
		ctx,
		"defaults",
		"read",
		"/Library/Preferences/com.apple.security.libraryvalidation.plist",
		"DisableLibraryValidation",
	).CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	if err != nil {
		check.OK = false
		check.Detail = fmt.Sprintf(
			"Failed to read DisableLibraryValidation (%v). Please run the following command in Terminal on a normal system, then reboot:\n\n"+
				"%s\n\n"+
				"Command output: %q",
			err,
			disableLibraryValidationCommand,
			outStr,
		)
		return check
	}

	lower := strings.ToLower(outStr)
	if lower == "1" || lower == "true" {
		check.OK = true
		check.Detail = outStr
		return check
	}

	check.OK = false
	check.Detail = fmt.Sprintf(
		"DisableLibraryValidation is currently %q (expected true/1).\n\n"+
			"Please run the following command in Terminal on a normal system, then reboot:\n\n"+
			"%s\n\n",
		outStr,
		disableLibraryValidationCommand,
	)
	return check
}

// Preflight verifies SIP, boot-args, and DisableLibraryValidation.
func Preflight(ctx context.Context) (PreflightResult, error) {
	checks := []Check{
		checkSIP(ctx),
		checkBootArgs(ctx),
		checkDisableLibraryValidation(ctx),
	}

	var failed []string
	for _, check := range checks {
		if !check.OK {
			failed = append(failed, check.Name)
		}
	}

	res := PreflightResult{Checks: checks}
	if len(failed) > 0 {
		return res, fmt.Errorf("preflight failed: %s", strings.Join(failed, ", "))
	}

	return res, nil
}
