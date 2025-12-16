package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"prism/internal/infra/env"
	infrahost "prism/internal/infra/host"
	"prism/internal/infra/paths"
	"prism/internal/ui/root"
	userui "prism/internal/ui/user"
)

// main is the Prism entrypoint. It supports three modes:
// 1) "host-autoboot" for the LaunchDaemon-managed headless host daemon.
// 2) "user" for the interactive TUI for a single local user.
// 3) default host-side root TUI for initializing the host and managing Prism users.
func main() {
	env.Load()

	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "host-autoboot":
		// First, bootstrap all user LaunchAgents
		infrahost.RunAutoboot(paths.StatePath())

		// Set up signal handling for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			log.Println("[host-autoboot] received shutdown signal")
			cancel()
		}()

		// Start the auto-update loop (runs forever until context is cancelled)
		auCfg := infrahost.AutoUpdateConfig{
			CheckInterval: 1 * time.Hour,
			OutputDir:     paths.OutputDir(),
			ConfigPath:    paths.ConfigPath(),
			StatePath:     paths.StatePath(),
		}
		infrahost.RunAutoUpdateLoop(ctx, auCfg)

		return

	case "user":
		model := userui.New()
		p := tea.NewProgram(model)

		if _, err := p.Run(); err != nil {
			log.New(os.Stderr, "", log.LstdFlags).Printf("Prism user UI exited with error: %v", err)
			os.Exit(1)
		}

		return

	default:
		model := root.New()
		p := tea.NewProgram(model)

		if _, err := p.Run(); err != nil {
			log.New(os.Stderr, "", log.LstdFlags).Printf("Prism TUI exited with error (see logs for details): %v", err)
			os.Exit(1)
		}

		return
	}
}
