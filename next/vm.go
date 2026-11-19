package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"charm.land/log/v2"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"clowder/api"
)

type VmData struct {
	rootfs string
	cfg    *v1.ConfigFile
}

func PullData(tmpdir string) (*VmData, error) {
	ref, err := name.ParseReference("alpine:latest")
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, fmt.Errorf("fetching image: %w", err)
	}

	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("getting config: %w", err)
	}

	reader := mutate.Extract(img)
	defer reader.Close()

	dir := filepath.Join(tmpdir, "rootfs")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating rootfs: %w", err)
	}

	cmd := exec.Command("tar", "-xf", "-", "-C", dir)
	cmd.Stdin = reader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("extracting rootfs: %w", err)
	}

	return &VmData{rootfs: dir, cfg: cfg}, nil
}

type Hypervisor struct {
	cmd    *exec.Cmd
	client *api.ClientWithResponses
}

func (h *Hypervisor) Run() error {
	ctx := context.Background()

	res, err := h.client.GetVmmPingWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("pinging Cloud Hypervisor: %w", err)
	}

	if res.JSON200 == nil {
		return fmt.Errorf("pinging Cloud Hypervisor status: %v", res.StatusCode())
	}

	log.Print("ping", "data", res.JSON200)

	return nil
}

func NewHypervisor(tmpdir string) (hypervisor *Hypervisor, err error) {
	socketPath := filepath.Join(tmpdir, "cloud-hypervisor.sock")

	cmd := exec.Command("cloud-hypervisor", "--api-socket", "path="+socketPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting Cloud Hypervisor: %w", err)
	}

	log.Info("Cloud Hypervisor started", "pid", cmd.Process.Pid)

	defer func() {
		if err != nil {
			cmd.Process.Kill()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var d net.Dialer

	for {
		conn, err := d.DialContext(ctx, "unix", socketPath)
		if err == nil {
			conn.Close()
			break
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for Cloud Hypervisor socket: %w", ctx.Err())
		case <-ticker.C:
		}
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	client, err := api.NewClientWithResponses("http://localhost/api/v1", api.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("creating API client: %w", err)
	}

	return &Hypervisor{cmd, client}, nil
}

func (h *Hypervisor) Close() error {
	if err := h.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("killing Cloud Hypervisor: %w", err)
	}

	if err := h.cmd.Wait(); err != nil {
		return fmt.Errorf("waiting for Cloud Hypervisor: %w", err)
	}

	return nil
}
