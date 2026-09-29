package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func EnsureOllama(bin string) error {
	if bin == "" {
		p, err := exec.LookPath("ollama")
		if err != nil {
			return fmt.Errorf("ollama not on PATH")
		}
		bin = p
	}
	if PortOpen(OllamaPort) {
		return nil
	}
	cmd := exec.Command(bin, "serve")
	cleanup, err := discardChildIO(cmd)
	if err != nil {
		return fmt.Errorf("ollama serve: %w", err)
	}
	cmd.Env = append(os.Environ(), "OLLAMA_DEBUG=ERROR")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		cleanup()
		return fmt.Errorf("ollama serve: %w", err)
	}
	cleanup() // child inherits the fd; parent drops its copy
	if err := WaitPort(OllamaPort, 20*time.Second); err != nil {
		return fmt.Errorf("ollama serve started (pid %d) but %w", cmd.Process.Pid, err)
	}
	fmt.Fprintf(os.Stderr, "ollama serve  pid %d  %s\n", cmd.Process.Pid, OllamaURL)
	return nil
}

func PullOllama(bin, name string) error {
	if bin == "" {
		p, err := exec.LookPath("ollama")
		if err != nil {
			return err
		}
		bin = p
	}
	if err := EnsureOllama(bin); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ollama pull  %s\n", name)
	cmd := exec.Command(bin, "pull", name)
	// Pull progress is intentional user feedback.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ollama pull %s: %w", name, err)
	}
	return nil
}
