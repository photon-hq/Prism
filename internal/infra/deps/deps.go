package deps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Name is a symbolic name for a dependency.
type Name string

const (
	NameHomebrew Name = "Homebrew"
	NameNode     Name = "Node.js"
	NameFRPC     Name = "frpc"
)

type Action string

const (
	ActionAlreadyInstalled Action = "already-installed"
	ActionInstallFailed    Action = "install-failed"
	ActionInstallUncertain Action = "install-uncertain"
	ActionBlockedNoBrew    Action = "blocked-no-brew"
	ActionInstalled        Action = "installed"
)

// Item describes the status of a single dependency.
type Item struct {
	Name   Name   `json:"name"`
	OK     bool   `json:"ok"`
	Action Action `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// Result aggregates all dependency checks.
type Result struct {
	Items []Item `json:"items"`
}

// Runner abstracts command execution for testing.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// RunnerFunc adapts a function to the Runner interface.
type RunnerFunc func(ctx context.Context, name string, args ...string) (string, error)

func (f RunnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

type cmdRunner struct {
	sudoUser string // The original non-root user when running under sudo
}

func newCmdRunner() cmdRunner {
	sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if sudoUser == "root" {
		sudoUser = ""
	}
	return cmdRunner{sudoUser: sudoUser}
}

func (r cmdRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	// If sudoUser is set and command needs user context, run via su
	if r.sudoUser != "" && needsUserContext(name) {
		return r.runAsUser(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// needsUserContext returns true for commands that must run as non-root.
func needsUserContext(name string) bool {
	switch name {
	case "brew", "node", "/bin/bash":
		return true
	default:
		return false
	}
}

func (r cmdRunner) runAsUser(ctx context.Context, name string, args ...string) (string, error) {
	cmdStr := shellQuote(name)
	for _, arg := range args {
		cmdStr += " " + shellQuote(arg)
	}
	cmd := exec.CommandContext(ctx, "su", "-", r.sudoUser, "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// Ensure checks and installs required dependencies (Homebrew, Node, frpc).
func Ensure(ctx context.Context) (Result, error) {
	return EnsureWithRunner(ctx, newCmdRunner())
}

func EnsureWithRunner(ctx context.Context, r Runner) (Result, error) {
	var res Result

	brewItem, hasBrew := ensureHomebrew(ctx, r)
	res.Items = append(res.Items, brewItem)

	nodeItem := ensureNode(ctx, r, hasBrew)
	res.Items = append(res.Items, nodeItem)

	frpcItem := ensureFRPC(ctx, r, hasBrew)
	res.Items = append(res.Items, frpcItem)

	var missing []string
	for _, it := range res.Items {
		if !it.OK {
			missing = append(missing, string(it.Name))
		}
	}

	if len(missing) > 0 {
		return res, fmt.Errorf("dependencies not satisfied: %s", strings.Join(missing, ", "))
	}

	return res, nil
}

func ensureHomebrew(ctx context.Context, r Runner) (Item, bool) {
	out, err := r.Run(ctx, "brew", "--version")
	if err == nil {
		return Item{
			Name:   NameHomebrew,
			OK:     true,
			Action: ActionAlreadyInstalled,
			Detail: out,
		}, true
	}

	// If running as root without SUDO_USER, we cannot install Homebrew
	if os.Geteuid() == 0 && os.Getenv("SUDO_USER") == "" {
		return Item{
			Name:   NameHomebrew,
			OK:     false,
			Action: ActionInstallFailed,
			Detail: "Homebrew is not installed and cannot be installed as root.\n\n" +
				"Please install Homebrew manually as a non-root user:\n\n" +
				"  /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\"\n\n" +
				"Then run `sudo ./prism` again.",
		}, false
	}

	const installScript = "NONINTERACTIVE=1 /bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
	installOut, installErr := r.Run(ctx, "/bin/bash", "-c", installScript)
	if installErr != nil {
		return Item{
			Name:   NameHomebrew,
			OK:     false,
			Action: ActionInstallFailed,
			Detail: fmt.Sprintf("Failed to install Homebrew: %v\nOutput: %s", installErr, installOut),
		}, false
	}

	out, err = r.Run(ctx, "brew", "--version")
	if err != nil {
		return Item{
			Name:   NameHomebrew,
			OK:     false,
			Action: ActionInstallUncertain,
			Detail: fmt.Sprintf("Homebrew installation attempted, but `brew --version` failed: %v\nOutput: %s", err, out),
		}, false
	}

	return Item{
		Name:   NameHomebrew,
		OK:     true,
		Action: ActionInstalled,
		Detail: out,
	}, true
}

func ensureNode(ctx context.Context, r Runner, hasBrew bool) Item {
	out, err := r.Run(ctx, "node", "--version")
	if err == nil && out != "" {
		return Item{
			Name:   NameNode,
			OK:     true,
			Action: ActionAlreadyInstalled,
			Detail: out,
		}
	}

	if !hasBrew {
		return Item{
			Name:   NameNode,
			OK:     false,
			Action: ActionBlockedNoBrew,
			Detail: "Node.js is not installed and Homebrew is missing. Please install Homebrew first.",
		}
	}

	installOut, installErr := r.Run(ctx, "brew", "install", "node@18")
	if installErr != nil {
		return Item{
			Name:   NameNode,
			OK:     false,
			Action: ActionInstallFailed,
			Detail: fmt.Sprintf("Failed to install node@18: %v\nOutput: %s", installErr, installOut),
		}
	}

	linkOut, linkErr := r.Run(ctx, "brew", "link", "--overwrite", "--force", "node@18")
	if linkErr != nil {
		return Item{
			Name:   NameNode,
			OK:     false,
			Action: ActionInstallUncertain,
			Detail: fmt.Sprintf("node@18 installed, but brew link failed: %v\nOutput: %s", linkErr, linkOut),
		}
	}

	out, err = r.Run(ctx, "node", "--version")
	if err != nil {
		return Item{
			Name:   NameNode,
			OK:     false,
			Action: ActionInstallUncertain,
			Detail: fmt.Sprintf("node@18 installed, but `node --version` failed: %v\nOutput: %s", err, out),
		}
	}

	return Item{
		Name:   NameNode,
		OK:     true,
		Action: ActionInstalled,
		Detail: out,
	}
}

func ensureFRPC(ctx context.Context, r Runner, hasBrew bool) Item {
	out, err := r.Run(ctx, "frpc", "-v")
	if err == nil && out != "" {
		return Item{
			Name:   NameFRPC,
			OK:     true,
			Action: ActionAlreadyInstalled,
			Detail: out,
		}
	}

	if !hasBrew {
		return Item{
			Name:   NameFRPC,
			OK:     false,
			Action: ActionBlockedNoBrew,
			Detail: "frpc is not installed and Homebrew is missing. Please install Homebrew first.",
		}
	}

	installOut, installErr := r.Run(ctx, "brew", "install", "frpc")
	if installErr != nil {
		return Item{
			Name:   NameFRPC,
			OK:     false,
			Action: ActionInstallFailed,
			Detail: fmt.Sprintf("Failed to install frpc: %v\nOutput: %s", installErr, installOut),
		}
	}

	out, err = r.Run(ctx, "frpc", "-v")
	if err != nil {
		return Item{
			Name:   NameFRPC,
			OK:     false,
			Action: ActionInstallUncertain,
			Detail: fmt.Sprintf("frpc installed, but `frpc -v` failed: %v\nOutput: %s", err, out),
		}
	}

	return Item{
		Name:   NameFRPC,
		OK:     true,
		Action: ActionInstalled,
		Detail: out,
	}
}
