package user

import (
	tea "github.com/charmbracelet/bubbletea"

	userinfra "prism/internal/infra/user"
)

func runGetAPIKeyCmd() tea.Cmd {
	return func() tea.Msg {
		return getKeyDoneMsg{status: userinfra.GetAPIKey()}
	}
}

func runPrewarmPermissionsCmd() tea.Cmd {
	return func() tea.Msg {
		return prewarmDoneMsg{status: userinfra.PrewarmPermissions()}
	}
}

func runRenameFriendlyCmd(name string) tea.Cmd {
	return func() tea.Msg {
		return renameDoneMsg{status: userinfra.RenameFriendlyName(name)}
	}
}

func runDeployCmd() tea.Cmd {
	return func() tea.Msg {
		return deployDoneMsg{status: userinfra.Deploy()}
	}
}

func runStopAllServicesCmd() tea.Cmd {
	return func() tea.Msg {
		return stopDoneMsg{status: userinfra.StopAllServices()}
	}
}

func runStartAllServicesCmd() tea.Cmd {
	return func() tea.Msg {
		return stopDoneMsg{status: userinfra.StartAllServices()}
	}
}

func runRestartServerCmd() tea.Cmd {
	return func() tea.Msg {
		return stopDoneMsg{status: userinfra.RestartServer()}
	}
}

func runRestartFRPCCmd() tea.Cmd {
	return func() tea.Msg {
		return stopDoneMsg{status: userinfra.RestartFRPC()}
	}
}
