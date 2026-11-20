package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"charm.land/log/v2"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("fatal error", "error", err)
	}
}

func run() error {
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	tmpdir, err := os.MkdirTemp("data", "vm-*")
	if err != nil {
		return fmt.Errorf("creating tempdir: %w", err)
	}

	defer os.RemoveAll(tmpdir)

	_, err = PullData(tmpdir)
	if err != nil {
		return fmt.Errorf("pulling data: %w", err)
	}

	hypervisor, err := NewHypervisor(tmpdir)
	if err != nil {
		return fmt.Errorf("creating hypervisor: %w", err)
	}

	defer hypervisor.Close()

	if err := hypervisor.Run(); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	<-done
	log.Info("Shutting down")

	return nil
}
