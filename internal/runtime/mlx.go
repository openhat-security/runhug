package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func StartMLX(model string, port int) (*exec.Cmd, error) {
	if port <= 0 {
		port = MLXPort
	}
	argv, err := mlxArgs(model, port)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "mlx        %s\n", strings.Join(argv, " "))
	cmd := exec.Command(argv[0], argv[1:]...)
	cleanup, err := discardChildIO(cmd)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	cleanup()
	if err := WaitPort(port, 3*time.Minute); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mlx_lm.server: %w (first load downloads the Hub repo)", err)
	}
	return cmd, nil
}

func mlxArgs(model string, port int) ([]string, error) {
	if p := strings.TrimSpace(os.Getenv("MLX_SERVER")); p != "" {
		return []string{p, "--model", model, "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port)}, nil
	}
	if p, err := exec.LookPath("mlx_lm.server"); err == nil {
		return []string{p, "--model", model, "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port)}, nil
	}
	if p, err := exec.LookPath("python3"); err == nil {
		return []string{p, "-m", "mlx_lm.server", "--model", model, "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port)}, nil
	}
	return nil, fmt.Errorf("mlx_lm.server not found (pip install mlx-lm, or set MLX_SERVER)")
}
