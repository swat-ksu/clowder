package main

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var choices = []string{"Yes, I agree", "No, disconnect"}

type keymap = struct {
	up, down, confirm, quit key.Binding
}

type model struct {
	cursor int
	choice string
	keymap keymap
	help   help.Model
}

func initialModel() model {
	return model{
		cursor: 0,
		choice: "",
		keymap: keymap{
			up: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("↑/k", "up"),
			),
			down: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("↓/j", "down"),
			),
			confirm: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "confirm"),
			),
			quit: key.NewBinding(
				key.WithKeys("esc", "ctrl+c"),
				key.WithHelp("esc/ctrl+c", "quit"),
			),
		},
		help: help.New(),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keymap.quit):
			return m, tea.Quit
		case key.Matches(msg, m.keymap.confirm):
			m.choice = choices[m.cursor]
			return m, tea.Quit
		case key.Matches(msg, m.keymap.up):
			m.cursor--
			if m.cursor < 0 {
				m.cursor = len(choices) - 1
			}
		case key.Matches(msg, m.keymap.down):
			m.cursor++
			if m.cursor >= len(choices) {
				m.cursor = 0
			}
		}
	}

	return m, nil
}

var (
	blockStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Bold(true).
			Italic(true).
			Background(lipgloss.Color("#00AFA9")).
			Foreground(lipgloss.Black)

	titleStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Italic(true).
			Background(lipgloss.Color("#FFC629")).
			Foreground(lipgloss.Black)

	questionStyle = lipgloss.NewStyle().
			Width(80).
			MarginBottom(1).
			Foreground(lipgloss.Color("#FFC629"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Underline(true).
			Foreground(lipgloss.Color("#00AFA9"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))
)

func (m model) View() tea.View {
	header := lipgloss.JoinHorizontal(lipgloss.Top, blockStyle.Render(">"), titleStyle.Render("Software Acceleration Team"))

	options := make([]string, len(choices))

	for i, choice := range choices {
		if m.cursor == i {
			options[i] = selectedStyle.Render("> " + choice)
		} else {
			options[i] = normalStyle.Render("  " + choice)
		}
	}

	main := lipgloss.JoinVertical(
		lipgloss.Left,
		questionStyle.Render("By using this Virtual Machine, you acknowledge that your activity will be monitored and that you agree to comply with all acceptable use policies."),
		lipgloss.JoinVertical(lipgloss.Left, options...),
	)

	footer := m.help.ShortHelpView([]key.Binding{
		m.keymap.up,
		m.keymap.down,
		m.keymap.confirm,
		m.keymap.quit,
	})

	all := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		lipgloss.NewStyle().Margin(1, 0).Render(main),
		footer,
	)

	v := tea.NewView(all)
	v.AltScreen = true

	return v
}
