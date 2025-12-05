package root

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const footerHint = "↑/k up  •  ↓/j down  •  Enter select  •  q quit"

// View implements tea.Model.
func (m Model) View() string {
	titleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#E0D39C")). // Soft Yellow (Warm)
		Foreground(lipgloss.Color("#575279")). // Deep Purple Gray
		Bold(true).
		Padding(0, 1)

	subtleText := lipgloss.NewStyle().Foreground(lipgloss.Color("#908CAA")) // Muted Lavender Gray
	countStyle := subtleText
	accentColor := lipgloss.Color("#EF9F76") // Warm Orange
	accentBorder := lipgloss.NewStyle().Foreground(accentColor)
	activeTitle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	inactiveTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C4C1D0")) // Dimmed Warm White
	activeDesc := lipgloss.NewStyle().Foreground(lipgloss.Color("#F472B6"))    // Soft Pink
	inactiveDesc := subtleText
	statusStyle := subtleText.MarginTop(1).PaddingLeft(2)
	checkOKStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))   // Bright Green
	checkFailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EB6F92")) // Rose (Low Sat Red)
	footerStyle := subtleText.MarginTop(1).PaddingLeft(2)

	items := []struct {
		title string
		desc  string
	}{
		{
			title: "Setup",
			desc:  "Initialize this Mac and prepare the Prism runtime",
		},
		{
			title: "Add users",
			desc:  "Add additional Prism users on this host",
		},
		{
			title: "View users",
			desc:  "View current Prism users and their state",
		},
		{
			title: "Update user code",
			desc:  "Download latest service bundle and update all Prism users",
		},
		{
			title: "Services status",
			desc:  "Check service status for each Prism user",
		},
		{
			title: "Remove user",
			desc:  "Remove a Prism user and its services",
		},
		{
			title: "Quit",
			desc:  "Exit Prism",
		},
	}

	var b strings.Builder

	// Title capsule
	b.WriteString(titleStyle.Render(" Prism ") + "\n\n")

	b.WriteString(countStyle.Render(fmt.Sprintf("  %d items", len(items))) + "\n\n")

	// Menu items
	for i, it := range items {
		selected := i == m.cursor

		border := "  "
		if selected {
			border = accentBorder.Render("│ ")
		}

		title := inactiveTitle.Render(it.title)
		desc := inactiveDesc.Render(it.desc)
		if selected {
			title = activeTitle.Render(it.title)
			desc = activeDesc.Render(it.desc)
		}

		b.WriteString(border + title + "\n")
		b.WriteString("  " + desc + "\n\n")
	}

	// Status line
	if m.status != "" {
		b.WriteString(statusStyle.Render(m.status) + "\n")
	}

	// Show errors prominently first, before technical details
	if m.provisionErr != nil {
		b.WriteString("\n")
		title := "[x] Setup failed"
		switch m.provisionKind {
		case provisionKindAdd:
			title = "[x] Add users failed"
		case provisionKindView:
			title = "[x] Failed to load users"
		case provisionKindRemove:
			title = "[x] Remove user failed"
		case provisionKindUpdate:
			title = "[x] Update user code failed"
		}
		b.WriteString(checkFailStyle.Render("  "+title) + "\n")

		// Show a user-friendly error message instead of technical stack trace
		errorMsg := m.provisionErr.Error()
		if strings.Contains(errorMsg, "permission denied") {
			b.WriteString("  " + subtleText.Render("Permission denied. Please run with sudo:") + "\n")
			b.WriteString("  " + accentBorder.Render("sudo ./prism") + "\n")
		} else if strings.Contains(errorMsg, "download archive") {
			b.WriteString("  " + subtleText.Render("Failed to download service bundle. Check your internet connection and GitHub token.") + "\n")
		} else if strings.Contains(errorMsg, "user") && strings.Contains(errorMsg, "already exists") {
			b.WriteString("  " + subtleText.Render("User already exists. Use 'Add users' instead of 'Setup'.") + "\n")
		} else {
			// For other errors, show a simplified version
			lines := strings.Split(errorMsg, "\n")
			mainError := lines[0]
			if len(mainError) > 80 {
				mainError = mainError[:77] + "..."
			}
			b.WriteString("  " + subtleText.Render(mainError) + "\n")
		}
		b.WriteString("\n")
	}

	// Render Preflight / Dependencies results (if present), but only when not showing errors
	if m.initResult != nil && m.provisionErr == nil {
		checks := m.initResult.Preflight.Checks
		if len(checks) > 0 {
			b.WriteString("\n")

			total := len(checks)
			passed := 0
			for _, c := range checks {
				if c.OK {
					passed++
				}
			}

			header := fmt.Sprintf("Preflight checks – %d/%d passed", passed, total)
			headerStyle := subtleText
			if passed == total {
				headerStyle = checkOKStyle
			}
			b.WriteString("  " + headerStyle.Render(header) + "\n")

			// Use the first failing step as the focus. Expand its details while
			// keeping other failing steps summarized and passed steps minimal.
			focus := -1
			for i, c := range checks {
				if !c.OK {
					focus = i
					break
				}
			}
			if focus != -1 {
				b.WriteString("  " + subtleText.Render("The first failing step below is blocking setup.") + "\n")
			}

			for i, c := range checks {
				stepLabel := fmt.Sprintf("Step %d: %s", i+1, c.Name)

				if c.OK {
					line := checkOKStyle.Render(fmt.Sprintf("  [✓] %s", stepLabel))
					b.WriteString(line + "\n")
					continue
				}

				// Failing step
				if i == focus {
					// Focus step: expand details to draw user attention.
					line := checkFailStyle.Render(fmt.Sprintf("  [!] %s", stepLabel))
					b.WriteString(line + "\n")
					if c.Detail != "" {
						for _, line := range strings.Split(c.Detail, "\n") {
							b.WriteString("    " + subtleText.Render(line) + "\n")
						}
					}
					continue
				}

				// Non-focus failing step: show only a short summary to avoid
				// overwhelming the user with information.
				short := "Blocked by previous steps"
				line := subtleText.Render(fmt.Sprintf("  [ ] %s (%s)", stepLabel, short))
				b.WriteString(line + "\n")
			}
		}

		depsRes := m.initResult.Deps
		if len(depsRes.Items) > 0 {
			b.WriteString("\n")
			b.WriteString("  " + activeTitle.Render("Dependencies") + "\n")

			totalDeps := len(depsRes.Items)
			readyDeps := 0
			for _, it := range depsRes.Items {
				if it.OK {
					readyDeps++
				}
			}

			depsHeader := fmt.Sprintf("Dependencies — %d/%d ready", readyDeps, totalDeps)
			depsHeaderStyle := subtleText
			if readyDeps == totalDeps {
				depsHeaderStyle = checkOKStyle
			}
			b.WriteString("  " + depsHeaderStyle.Render(depsHeader) + "\n")

			if readyDeps != totalDeps {
				b.WriteString("  " + subtleText.Render("Some dependencies below are blocking setup.") + "\n")
			}

			for _, it := range depsRes.Items {
				name := string(it.Name)
				detail := strings.TrimSpace(it.Detail)
				if idx := strings.Index(detail, "\n"); idx >= 0 {
					detail = detail[:idx]
				}
				if it.OK {
					if detail == "" {
						detail = "Ready"
					}
					line := checkOKStyle.Render(fmt.Sprintf("  [✓] %s – %s", name, detail))
					b.WriteString(line + "\n")
					continue
				}

				line := checkFailStyle.Render(fmt.Sprintf("  [!] %s", name))
				b.WriteString(line + "\n")
				if detail != "" {
					for _, l := range strings.Split(it.Detail, "\n") {
						b.WriteString("    " + subtleText.Render(l) + "\n")
					}
				}
			}
		}
	}

	// Service status view.
	if m.servicesRunning || m.servicesErr != nil || len(m.services) > 0 {
		b.WriteString("\n")
		b.WriteString("  " + activeTitle.Render("Service status") + "\n")

		if m.servicesRunning {
			b.WriteString("  " + subtleText.Render("Checking Prism user service status. Please wait...") + "\n")
		} else if m.servicesErr != nil {
			line := checkFailStyle.Render("  [!] Failed to check service status")
			b.WriteString(line + "\n")
			for _, l := range strings.Split(m.servicesErr.Error(), "\n") {
				b.WriteString("    " + subtleText.Render(l) + "\n")
			}
		} else if len(m.services) == 0 {
			b.WriteString("  " + subtleText.Render("There are currently no Prism users.") + "\n")
		} else {
			total := len(m.services)
			healthy := 0
			for _, s := range m.services {
				if s.ServiceDirOK && s.PortListening {
					healthy++
				}
			}
			header := fmt.Sprintf("Service status — %d/%d healthy", healthy, total)
			headerStyle := subtleText
			switch healthy {
			case total:
				headerStyle = checkOKStyle
			case 0:
				headerStyle = checkFailStyle
			}
			b.WriteString("  " + headerStyle.Render(header) + "\n")

			for _, s := range m.services {
				ok := s.ServiceDirOK && s.PortListening
				var line string
				base := fmt.Sprintf("%s • port %d • subdomain %s", s.Name, s.Port, s.Subdomain)
				if ok {
					line = checkOKStyle.Render("  [✓] " + base)
				} else {
					line = checkFailStyle.Render("  [!] " + base)
				}
				b.WriteString("  " + line + "\n")
				if !ok && strings.TrimSpace(s.Detail) != "" {
					for _, l := range strings.Split(s.Detail, ";") {
						b.WriteString("    " + subtleText.Render(strings.TrimSpace(l)) + "\n")
					}
				}
			}
		}
	}

	// User provisioning section (simplified - errors are shown above now)
	if m.awaitUserCount || m.provisionRunning || (m.provisionResult != nil && m.provisionErr == nil) {
		b.WriteString("\n")
		b.WriteString("  " + activeTitle.Render("User provisioning") + "\n")

		switch m.provisionKind {
		case provisionKindInitial:
			b.WriteString("  " + activeTitle.Render("Setup") + "\n")
		case provisionKindAdd:
			b.WriteString("  " + activeTitle.Render("Add users") + "\n")
		case provisionKindView:
			b.WriteString("  " + activeTitle.Render("View users") + "\n")
		case provisionKindUpdate:
			b.WriteString("  " + activeTitle.Render("Update user code") + "\n")
		case provisionKindRemove:
			b.WriteString("  " + activeTitle.Render("Remove user") + "\n")
		}

		switch {
		case m.awaitUserCount && !m.provisionRunning:
			prompt := "Enter the number of Prism users to create (positive integer), then press Enter."
			if m.provisionKind == provisionKindAdd {
				prompt = "Enter the number of Prism users to add (positive integer), then press Enter."
			}
			b.WriteString("  " + subtleText.Render(prompt) + "\n")
			val := strings.TrimSpace(m.userCountInput)
			if val == "" {
				val = "_"
			}
			label := subtleText.Render("  Users: ")
			input := accentBorder.Render(" " + val + " ")
			b.WriteString(label + input + "\n")

		case m.provisionRunning:
			msg := "Creating users and provisioning services. Please wait..."
			switch m.provisionKind {
			case provisionKindAdd:
				msg = "Adding users and provisioning services. Please wait..."
			case provisionKindRemove:
				msg = "Removing user and cleaning up services. Please wait..."
			case provisionKindUpdate:
				msg = "Updating Prism user code for all users. Please wait..."
			}
			b.WriteString("  " + subtleText.Render(msg) + "\n")

		case m.provisionResult != nil && m.provisionErr == nil:
			n := len(m.provisionResult.State.Users)

			switch m.provisionKind {
			case provisionKindAdd:
				b.WriteString("  " + checkOKStyle.Render("🎉 Successfully added users!") + "\n")
				b.WriteString("  " + subtleText.Render(fmt.Sprintf("There are now %d users total. Passwords saved to: %s", n, m.provisionResult.SecretsPath)) + "\n")
			case provisionKindView:
				b.WriteString("  " + checkOKStyle.Render(fmt.Sprintf("📋 Current users (%d total)", n)) + "\n")
				b.WriteString("  " + subtleText.Render(fmt.Sprintf("Password records: %s", m.provisionResult.SecretsPath)) + "\n")
			case provisionKindRemove:
				if m.lastRemovedUser != "" && !m.awaitRemoveSelection {
					b.WriteString("  " + checkOKStyle.Render(fmt.Sprintf("🎉 Deleted user %s", m.lastRemovedUser)) + "\n")
					b.WriteString("  " + subtleText.Render(fmt.Sprintf("%d users remaining. Passwords: %s", n, m.provisionResult.SecretsPath)) + "\n")
				} else {
					b.WriteString("  " + checkOKStyle.Render(fmt.Sprintf("📋 Select user to remove (%d total)", n)) + "\n")
					b.WriteString("  " + subtleText.Render("Use ↑/↓ to select, Enter to confirm, q to cancel") + "\n")
				}
			case provisionKindUpdate:
				b.WriteString("  " + checkOKStyle.Render("🎉 Successfully updated user code!") + "\n")
				b.WriteString("  " + subtleText.Render(fmt.Sprintf("Updated code for %d Prism users.", n)) + "\n")
			default:
				// Initial setup success
				b.WriteString("  " + checkOKStyle.Render("🎉 Setup completed successfully!") + "\n")
				b.WriteString("  " + subtleText.Render(fmt.Sprintf("Created %d users. Passwords saved to: %s", n, m.provisionResult.SecretsPath)) + "\n")
				b.WriteString("\n")
				b.WriteString("  " + activeTitle.Render("Next steps:") + "\n")
				b.WriteString("  " + subtleText.Render("1. Switch to each user account to start their services") + "\n")
				b.WriteString("  " + subtleText.Render("2. Run: ./prism user") + "\n")
			}

			b.WriteString("\n")
			for idx, u := range m.provisionResult.State.Users {
				prefix := "  • "
				if m.provisionKind == provisionKindRemove && m.awaitRemoveSelection && idx == m.removeIndex {
					prefix = "  ▶ "
				}
				line := fmt.Sprintf("%s%s (port %d, subdomain: %s)", prefix, u.Name, u.Port, u.Subdomain)
				style := subtleText
				if m.provisionKind == provisionKindRemove && m.awaitRemoveSelection && idx == m.removeIndex {
					style = activeTitle
				}
				b.WriteString(style.Render(line) + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(footerStyle.Render(footerHint) + "\n")

	return b.String()
}
