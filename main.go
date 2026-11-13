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

	tea "charm.land/bubbletea/v2"
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
