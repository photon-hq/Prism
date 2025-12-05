package root

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"prism/internal/control/host"
	"prism/internal/infra/paths"
	"prism/internal/infra/state"
)

// runInitCmd runs the host initialization in a separate goroutine and returns
// a Bubble Tea command that yields an initDoneMsg when complete.
func runInitCmd() tea.Cmd {
	return func() tea.Msg {
		init := host.NewInitializer(paths.ConfigPath(), paths.StatePath())
		res, err := init.Run(context.Background())
		return initDoneMsg{result: res, err: err}
	}
}

// runProvisionCmd runs the user provisioning flow in a separate goroutine and
// returns a Bubble Tea command that yields a provisionDoneMsg when complete.
func runProvisionCmd(userCount int) tea.Cmd {
	return func() tea.Msg {
		init := host.NewInitializer(paths.ConfigPath(), paths.StatePath())
		prismPath, _ := os.Executable()
		res, err := init.Provision(context.Background(), userCount, prismPath)
		return provisionDoneMsg{result: res, err: err}
	}
}

// runAddUsersCmd runs the "add users" flow in a separate goroutine and
// returns a Bubble Tea command that yields a provisionDoneMsg when complete.
func runAddUsersCmd(userCount int) tea.Cmd {
	return func() tea.Msg {
		init := host.NewInitializer(paths.ConfigPath(), paths.StatePath())
		prismPath, _ := os.Executable()
		res, err := init.AddUsers(context.Background(), userCount, prismPath)
		return provisionDoneMsg{result: res, err: err}
	}
}

// runViewUsersCmd only loads the current state and wraps it into a
// ProvisionResult so that the User provisioning section can present the user
// list in a uniform way.
func runViewUsersCmd() tea.Cmd {
	return func() tea.Msg {
		st, err := state.Load(paths.StatePath())
		if err != nil {
			return provisionDoneMsg{err: err}
		}
		return provisionDoneMsg{result: host.ProvisionResult{State: st, SecretsPath: paths.SecretsPath()}}
	}
}

func runUpdateUsersCodeCmd() tea.Cmd {
	return func() tea.Msg {
		init := host.NewInitializer(paths.ConfigPath(), paths.StatePath())
		res, err := init.UpdateUserCode(context.Background())
		return provisionDoneMsg{result: res, err: err}
	}
}

// runServicesCmd runs the services status inspection and returns a
// servicesDoneMsg for the UI to render.
func runServicesCmd() tea.Cmd {
	return func() tea.Msg {
		init := host.NewInitializer(paths.ConfigPath(), paths.StatePath())
		statuses, err := init.UserServiceStatuses(context.Background())
		return servicesDoneMsg{statuses: statuses, err: err}
	}
}

// runRemoveUserCmd removes a single Prism user account and its service
// directory, then returns the updated state wrapped in a ProvisionResult so
// that the User provisioning section can render it consistently.
func runRemoveUserCmd(username string) tea.Cmd {
	return func() tea.Msg {
		init := host.NewInitializer(paths.ConfigPath(), paths.StatePath())
		st, err := init.RemoveUser(context.Background(), username)
		if err != nil {
			return provisionDoneMsg{err: err}
		}
		return provisionDoneMsg{result: host.ProvisionResult{State: st, SecretsPath: paths.SecretsPath()}}
	}
}
