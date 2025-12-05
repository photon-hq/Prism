package root

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"prism/internal/control/host"
)

// Model is the root TUI model.
type Model struct {
	cursor      int
	status      string
	initRunning bool
	initResult  *host.Result
	initErr     error

	awaitUserCount       bool
	userCountInput       string
	provisionRunning     bool
	provisionResult      *host.ProvisionResult
	provisionErr         error
	provisionKind        provisionKind
	awaitRemoveSelection bool
	removeIndex          int
	lastRemovedUser      string

	servicesRunning bool
	servicesErr     error
	services        []host.ServiceStatus
}

type provisionKind int

const (
	provisionKindNone provisionKind = iota
	provisionKindInitial
	provisionKindAdd
	provisionKindView
	provisionKindUpdate
	provisionKindRemove
)

type initDoneMsg struct {
	result host.Result
	err    error
}

type provisionDoneMsg struct {
	result host.ProvisionResult
	err    error
}

type servicesDoneMsg struct {
	statuses []host.ServiceStatus
	err      error
}

// New creates a new root model.
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
	case initDoneMsg:
		return m.updateForInitDoneMsg(msg)
	case provisionDoneMsg:
		return m.updateForProvisionDoneMsg(msg)
	case servicesDoneMsg:
		return m.updateForServicesDoneMsg(msg)
	default:
		return m, nil
	}
}

func (m Model) updateForKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.initRunning || m.provisionRunning || m.servicesRunning {
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if m.awaitUserCount {
		key := msg.String()
		switch key {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			input := strings.TrimSpace(m.userCountInput)
			if input == "" {
				if m.provisionKind == provisionKindAdd {
					m.status = "Please enter the number of Prism users to add (at least 1)."
				} else {
					m.status = "Please enter the number of Prism users to create (at least 1)."
				}
				return m, nil
			}
			n, err := strconv.Atoi(input)
			if err != nil || n <= 0 {
				m.status = "Invalid count. Please enter a number greater than 0."
				return m, nil
			}
			m.awaitUserCount = false
			m.provisionRunning = true
			m.provisionErr = nil
			m.provisionResult = nil
			if m.provisionKind == provisionKindAdd {
				m.status = fmt.Sprintf("Adding %d Prism users to this host. Please wait...", n)
				return m, runAddUsersCmd(n)
			}
			m.status = fmt.Sprintf("Creating Prism runtime for %d users. Please wait...", n)
			return m, runProvisionCmd(n)
		case "backspace", "ctrl+h":
			if len(m.userCountInput) > 0 {
				m.userCountInput = m.userCountInput[:len(m.userCountInput)-1]
			}
			return m, nil
		default:
			if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
				m.userCountInput += key
				return m, nil
			}
		}
	}

	if m.provisionKind == provisionKindRemove && m.provisionResult != nil && m.awaitRemoveSelection {
		key := msg.String()
		switch key {
		case "q", "esc", "ctrl+c":
			m.status = "Prism user deletion cancelled."
			m.awaitRemoveSelection = false
			m.provisionKind = provisionKindNone
			return m, nil
		case "up", "k":
			if m.removeIndex > 0 {
				m.removeIndex--
			}
			return m, nil
		case "down", "j":
			if m.provisionResult != nil {
				max := len(m.provisionResult.State.Users) - 1
				if max < 0 {
					return m, nil
				}
				if m.removeIndex < max {
					m.removeIndex++
				}
			}
			return m, nil
		case "enter", " ":
			if m.provisionResult == nil || len(m.provisionResult.State.Users) == 0 {
				m.status = "No Prism users found."
				m.awaitRemoveSelection = false
				return m, nil
			}
			if m.removeIndex < 0 || m.removeIndex >= len(m.provisionResult.State.Users) {
				return m, nil
			}
			u := m.provisionResult.State.Users[m.removeIndex]
			m.awaitRemoveSelection = false
			m.provisionRunning = true
			m.provisionErr = nil
			m.status = fmt.Sprintf("Removing Prism user %s and its services. Please wait...", u.Name)
			m.lastRemovedUser = u.Name
			return m, runRemoveUserCmd(u.Name)
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
		if m.cursor < 6 {
			m.cursor++
		}
		return m, nil
	case "enter", " ":
		switch m.cursor {
		case 0:
			m.status = "Initializing host environment (including preflight checks)..."
			m.initRunning = true
			m.initErr = nil
			m.initResult = nil
			m.provisionKind = provisionKindInitial
			return m, runInitCmd()
		case 1:
			m.status = "Enter the number of Prism users to add, then press Enter to start."
			m.awaitUserCount = true
			m.userCountInput = ""
			m.provisionKind = provisionKindAdd
			m.provisionErr = nil
			return m, nil
		case 2:
			m.status = "Loading current Prism user state..."
			m.provisionKind = provisionKindView
			m.provisionErr = nil
			return m, runViewUsersCmd()
		case 3:
			m.status = "Updating Prism user code for all users. Please wait..."
			m.provisionKind = provisionKindUpdate
			m.provisionRunning = true
			m.provisionErr = nil
			m.provisionResult = nil
			return m, runUpdateUsersCodeCmd()
		case 4:
			m.status = "Checking service status for all Prism users..."
			m.servicesRunning = true
			m.servicesErr = nil
			m.services = nil
			return m, runServicesCmd()
		case 5:
			m.status = "Loading current Prism user list to select a user to remove..."
			m.provisionKind = provisionKindRemove
			m.provisionErr = nil
			m.provisionResult = nil
			m.provisionRunning = true
			m.awaitRemoveSelection = false
			m.lastRemovedUser = ""
			return m, runViewUsersCmd()
		default:
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m Model) updateForInitDoneMsg(msg initDoneMsg) (tea.Model, tea.Cmd) {
	m.initRunning = false
	m.initResult = &msg.result
	m.initErr = msg.err

	if msg.err != nil {
		m.status = "Environment is not ready. Please follow the Preflight and Dependencies hints below, then retry."
		m.awaitUserCount = false
	} else if msg.result.AlreadyInitialized {
		m.status = "This host is already initialized; no further setup is required."
	} else {
		m.status = "Environment checks completed. Enter the number of Prism users to create, then press Enter to begin."
		m.awaitUserCount = true
		m.userCountInput = ""
		m.provisionKind = provisionKindInitial
	}

	return m, nil
}

func (m Model) updateForProvisionDoneMsg(msg provisionDoneMsg) (tea.Model, tea.Cmd) {
	m.provisionRunning = false
	m.provisionResult = &msg.result
	m.provisionErr = msg.err

	if msg.err != nil {
		m.status = "An error occurred while creating or updating Prism users. See the User provisioning section below for details."
	} else {
		n := len(msg.result.State.Users)
		if n == 0 {
			m.status = "No Prism users found."
		} else {
			switch m.provisionKind {
			case provisionKindAdd:
				m.status = fmt.Sprintf("Added %d Prism users to this host. Passwords are stored in %s.", n, msg.result.SecretsPath)
			case provisionKindView:
				m.status = fmt.Sprintf("There are currently %d Prism users. Password records are located at %s.", n, msg.result.SecretsPath)
			case provisionKindRemove:
				if m.lastRemovedUser == "" {
					m.awaitRemoveSelection = true
					if m.removeIndex >= n {
						m.removeIndex = n - 1
					}
					if m.removeIndex < 0 {
						m.removeIndex = 0
					}
					m.status = "Use ↑/↓ to select a Prism user to delete, then press Enter to confirm; press q to cancel."
				} else {
					m.awaitRemoveSelection = false
					m.status = fmt.Sprintf("Deleted Prism user %s. There are now %d users.", m.lastRemovedUser, n)
				}
			case provisionKindUpdate:
				m.status = fmt.Sprintf("Updated Prism user code for %d users.", n)
			default:
				m.status = fmt.Sprintf("🎉 Setup completed! Created %d users. Next: switch to each user and run './prism user'", n)
			}
		}
	}

	return m, nil
}

func (m Model) updateForServicesDoneMsg(msg servicesDoneMsg) (tea.Model, tea.Cmd) {
	m.servicesRunning = false
	m.services = msg.statuses
	m.servicesErr = msg.err

	if msg.err != nil {
		m.status = "An error occurred while checking Prism service status. See the Service status section below for details."
	} else if len(msg.statuses) == 0 {
		m.status = "There are currently no Prism users, so there are no service statuses to check."
	} else {
		m.status = "Service status for all Prism users has been updated."
	}

	return m, nil
}
