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

func (h *Hypervisor) Run() error {
	ctx := context.Background()

	ping, err := h.client.GetVmmPingWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("pinging Cloud Hypervisor: %w", err)
	}
	if ping.StatusCode() != http.StatusOK {
		return fmt.Errorf("pinging Cloud Hypervisor status: %v", ping.StatusCode())
	}

	log.Print("ping", "data", ping.JSON200)

	body := api.CreateVMJSONRequestBody{
		Console: &api.ConsoleConfig{
			Mode:   api.ConsoleModeSocket,
			Socket: new("console.sock"),
		},
		Cpus: &api.CpusConfig{
			BootVcpus: 1,
			MaxVcpus:  1,
		},
		Memory: &api.MemoryConfig{
			Size: 512 * 1024 * 1024,
		},
		Disks: &[]api.DiskConfig{
			{
				ImageType: new(api.ImageTypeRaw),
				Path:      new(os.Getenv("DISK")),
			},
		},
		Payload: api.PayloadConfig{
			Cmdline: new("console=hvc0 root=/dev/vda1 rw init=/bin/sh"),
			Kernel:  new(os.Getenv("KERNEL")),
		},
	}

	create, err := h.client.CreateVMWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("creating Cloud Hypervisor VM: %w", err)
	}
	if create.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("creating Cloud Hypervisor VM status: %v", create.StatusCode())
	}

	boot, err := h.client.BootVMWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("booting Cloud Hypervisor VM: %w", err)
	}
	if boot.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("booting Cloud Hypervisor VM status: %v", boot.StatusCode())
	}

	info, err := h.client.GetVmInfoWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("querying Cloud Hypervisor VM: %w", err)
	}
	if info.StatusCode() != http.StatusOK {
		return fmt.Errorf("querying Cloud Hypervisor VM status: %v", info.StatusCode())
	}

	log.Print("info", "data", info.JSON200)

	return nil
}

func (h *Hypervisor) Close() error {
	ctx := context.Background()

	shutdown, err := h.client.ShutdownVMMWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("shutting down Cloud Hypervisor: %w", err)
	}
	if shutdown.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("shutting down Cloud Hypervisor status: %v", shutdown.StatusCode())
	}

	if err := h.cmd.Wait(); err != nil {
		return fmt.Errorf("waiting for Cloud Hypervisor: %w", err)
	}

	return nil
}
