package main

import (
	"clowder/api"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

type Hypervisor struct {
	socketDir string
	cmd       *exec.Cmd
	client    *api.Client
}

func newHypervisor() (*Hypervisor, error) {
	socketDir, err := os.MkdirTemp("", "cloud-hypervisor-")
	if err != nil {
		return nil, fmt.Errorf("creating socket directory: %w", err)
	}

	socketPath := filepath.Join(socketDir, "api.sock")

	cmd := exec.Command("cloud-hypervisor", "--api-socket", "path="+socketPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cmd.Process.Kill()
		os.RemoveAll(socketDir)
		return nil, fmt.Errorf("starting Cloud Hypervisor: %w", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	client, err := api.NewClient("http://unix", api.WithHTTPClient(httpClient))
	if err != nil {
		cmd.Process.Kill()
		os.RemoveAll(socketDir)
		return nil, fmt.Errorf("creating API client: %w", err)
	}

	return &Hypervisor{socketDir, cmd, client}, nil
}

func (h *Hypervisor) Close() error {
	defer os.RemoveAll(h.socketDir)

	if err := h.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("killing Cloud Hypervisor: %w", err)
	}

	if err := h.cmd.Wait(); err != nil {
		return fmt.Errorf("waiting for Cloud Hypervisor: %w", err)
	}

	return nil
}
