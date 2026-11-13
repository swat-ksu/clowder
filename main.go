package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	"charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"

	"github.com/gorilla/websocket"
	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
)

const (
	host = "localhost"
	port = "23234"
)

func main() {
	server, err := incus.ConnectIncusUnix("", nil)
	if err != nil {
		log.Error("Could not connect to Incus socket", "error", err)
	}

	srv, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		wish.WithMiddleware(
			vmMiddleware(server),
			activeterm.Middleware(),
			logging.Middleware(),
		),
	)

	if err != nil {
		log.Error("Could not start server", "error", err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("Starting SSH server", "host", host, "port", port)

		if err = srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done

	log.Info("Stopping SSH server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() { cancel() }()

	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		log.Error("Could not stop server", "error", err)
	}
}

func vmMiddleware(incus incus.InstanceServer) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(s ssh.Session) {
			vmConnection(incus, s)
		}
	}
}

func vmConnection(server incus.InstanceServer, sess ssh.Session) {
	pty, resizeChannel, ok := sess.Pty()
	if !ok {
		panic("middleware should've enforced PTY")
	}

	opts := bubbletea.MakeOptions(sess)
	opts = append(opts, tea.WithContext(sess.Context()))

	// TODO: Handle resizing (look into `bubbletea.Middleware()`)
	program := tea.NewProgram(initialModel(), opts...)

	result, err := program.Run()
	if err != nil {
		log.Error("Bubble Tea failed", "error", err)

		if err := sess.Exit(1); err != nil {
			log.Error("Could not exit connection", "error", err)
		}

		return
	}

	if model, ok := result.(model); ok && model.choice != "Yes, I agree" {
		if err := sess.Exit(0); err != nil {
			log.Error("Could not exit connection", "error", err)
		}

		return
	}

	name := "vm"

	req := api.InstanceExecPost{
		Command:     []string{"bash", "-l"},
		WaitForWS:   true,
		Interactive: true,
		Width:       pty.Window.Width,
		Height:      pty.Window.Height,
	}

	args := incus.InstanceExecArgs{
		Stdin:  sess,
		Stdout: sess,
		Stderr: sess.Stderr(),
		Control: func(conn *websocket.Conn) {
			for {
				select {
				case size, ok := <-resizeChannel:
					if !ok {
						return
					}

					msg := api.InstanceExecControl{
						Command: "window-resize",
						Args: map[string]string{
							"width":  strconv.Itoa(size.Width),
							"height": strconv.Itoa(size.Height),
						},
					}

					if err := conn.WriteJSON(msg); err != nil {
						log.Error("Could not write resize message", "error", err)
					}
				case <-sess.Context().Done():
					return
				}
			}
		},
	}

	op, err := server.ExecInstance(name, req, &args)
	if err != nil {
		log.Error("Could not execute command on VM", "error", err)
	}

	if err := op.WaitContext(sess.Context()); err != nil {
		log.Error("Could not wait for VM", "error", err)
	}

	if err := sess.Exit(0); err != nil {
		log.Error("Could not exit connection", "error", err)
	}
}

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

func (m model) View() tea.View {
	blockStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Bold(true).
		Italic(true).
		Background(lipgloss.Color("#00AFA9")).
		Foreground(lipgloss.Black)

	titleStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Italic(true).
		Background(lipgloss.Color("#FFC629")).
		Foreground(lipgloss.Black)

	header := lipgloss.JoinHorizontal(lipgloss.Top, blockStyle.Render(">"), titleStyle.Render("Software Acceleration Team"))

	questionStyle := lipgloss.NewStyle().
		Width(80).
		MarginBottom(1).
		Foreground(lipgloss.Color("#FFC629"))

	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Underline(true).
		Foreground(lipgloss.Color("#00AFA9"))

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AAAAAA"))

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
