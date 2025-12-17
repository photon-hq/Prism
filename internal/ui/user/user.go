package user

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Model is the per-user TUI model.
type Model struct {
	cursor int
	status string

	busy        bool
	renaming    bool
	renameInput string
}

// New creates a new user-mode model.
func New() Model {
	return Model{}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateForKeyMsg(msg)
	case stopDoneMsg:
		m.busy = false
		m.status = msg.status
		return m, nil
	case renameDoneMsg:
		m.busy = false
		m.status = msg.status
		return m, nil
	case prewarmDoneMsg:
		m.busy = false
		m.status = msg.status
		return m, nil
	case getKeyDoneMsg:
		m.busy = false
		m.status = msg.status
		return m, nil
	case deployDoneMsg:
		m.busy = false
		m.status = msg.status
		return m, nil
	}

	return m, nil
}

func (m Model) updateForKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if m.renaming {
		key := msg.String()
		switch key {
		case "esc":
			m.renaming = false
			m.renameInput = ""
			m.status = "Cancelled friendly name update."
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.renameInput)
			if name == "" {
				m.status = "Please enter a non-empty friendly name."
				return m, nil
			}
			m.renaming = false
			m.busy = true
			m.status = fmt.Sprintf("Updating friendly name to \"%s\" and restarting frpc...", name)
			return m, runRenameFriendlyCmd(name)
		case "backspace", "ctrl+h":
			if len(m.renameInput) > 0 {
				runes := []rune(m.renameInput)
				m.renameInput = string(runes[:len(runes)-1])
			}
			return m, nil
		default:
			r := []rune(key)
			if len(r) == 1 && r[0] >= ' ' {
				m.renameInput += key
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.cursor < 8 {
			m.cursor++
		}
		return m, nil
	case "enter", " ":
		switch m.cursor {
		case 0:
			m.busy = true
			m.status = "Prewarming local permissions; Messages/System Events prompts may appear, please click Allow..."
			return m, runPrewarmPermissionsCmd()
		case 1:
			m.busy = true
			m.status = "Requesting a one-time API key from Nexus..."
			return m, runGetAPIKeyCmd()
		case 2:
			m.busy = true
			m.status = "Deploying and starting the local Prism server and frpc..."
			return m, runDeployCmd()
		case 3:
			m.busy = true
			m.status = "Stopping the local Prism server and frpc..."
			return m, runStopAllServicesCmd()
		case 4:
			m.busy = true
			m.status = "Starting the local Prism server and frpc..."
			return m, runStartAllServicesCmd()
		case 5:
			m.busy = true
			m.status = "Restarting the local Prism server..."
			return m, runRestartServerCmd()
		case 6:
			m.busy = true
			m.status = "Restarting frpc..."
			return m, runRestartFRPCCmd()
		case 7:
			m.renaming = true
			m.renameInput = ""
			m.status = "Enter a new friendly name, then press Enter to confirm (Esc to cancel)."
			return m, nil
		case 8:
			return m, tea.Quit
		}
	}

	return m, nil
}

type stopDoneMsg struct {
	status string
}

type prewarmDoneMsg struct {
	status string
}

type getKeyDoneMsg struct {
	status string
}

type deployDoneMsg struct {
	status string
}

type renameDoneMsg struct {
	status string
}
