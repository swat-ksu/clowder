package next

import (
	"fmt"
	"os"
)

func main() {
}

func run() error {
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

	return nil
}
