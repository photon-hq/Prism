package user

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const footerHint = "↑/k up  •  ↓/j down  •  Enter select  •  q quit"

// View renders the user-mode TUI.
func (m Model) View() string {
	titleStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#E0D39C")). // Soft Yellow
		Foreground(lipgloss.Color("#575279")). // Deep Purple Gray
		Bold(true).
		Padding(0, 1)

	subtleText := lipgloss.NewStyle().Foreground(lipgloss.Color("#908CAA")) // Muted Lavender Gray
	accentColor := lipgloss.Color("#EF9F76")                                // Warm Orange
	accentBorder := lipgloss.NewStyle().Foreground(accentColor)
	activeTitle := lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	inactiveTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#C4C1D0")) // Dimmed Warm White
	activeDesc := lipgloss.NewStyle().Foreground(lipgloss.Color("#F472B6"))    // Soft Pink
	inactiveDesc := subtleText
	statusStyle := subtleText.MarginTop(1).PaddingLeft(2)
	footerStyle := subtleText.MarginTop(1).PaddingLeft(2)

	items := []struct {
		title string
		desc  string
	}{
		{"Prewarm permissions", "Prewarm local permissions (Messages/System Events/Automation)"},
		{"Get API key", "Request a one-time API key from Nexus (displayed once)"},
		{"Deploy / start services", "Deploy or start the local Prism server and frpc"},
		{"Stop all services", "Stop the local Prism server and frpc"},
		{"Start all services", "Start the local Prism server and frpc (after stop)"},
		{"Restart server", "Restart the local Prism server"},
		{"Restart frpc", "Restart the local frpc"},
		{"Rename friendly name", "Update the friendly name and restart frpc"},
		{"Quit", "Exit Prism (does not change current service state)"},
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(" Prism – User mode ") + "\n\n")
	b.WriteString(subtleText.Render(fmt.Sprintf("  %d items", len(items))) + "\n\n")

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

	if m.status != "" {
		b.WriteString(statusStyle.Render(m.status) + "\n")
	}
	if m.renaming {
		prompt := subtleText.Render("  Current input: ")
		val := m.renameInput
		if val == "" {
			val = "_"
		}
		input := activeDesc.Render(val)
		b.WriteString(prompt + input + "\n")
	}

	b.WriteString("\n")
	b.WriteString(footerStyle.Render(footerHint) + "\n")

	return b.String()
}
