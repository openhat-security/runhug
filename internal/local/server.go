package local

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func FindServer() (string, error) {
	if p := strings.TrimSpace(os.Getenv("LLAMA_SERVER")); p != "" {
		return p, nil
	}
	for _, name := range []string{"llama-server", "llama-cpp-server"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("llama-server not on PATH (install llama.cpp, or set LLAMA_SERVER)")
}

func Start(bin, gguf, alias string, port, ctx, threads int, embeddings bool) (*exec.Cmd, error) {
	if port <= 0 {
		port = 8081
	}
	if bin == "" {
		var err error
		bin, err = FindServer()
		if err != nil {
			return nil, err
		}
	}
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	args := []string{"-m", gguf, "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	if ctx > 0 {
		args = append(args, "-c", strconv.Itoa(ctx))
	}
	if threads > 0 {
		args = append(args, "-t", strconv.Itoa(threads))
	}
	if alias != "" {
		args = append(args, "--alias", alias)
	}
	if embeddings {
		args = append(args, "--embeddings")
	}
	cmd := exec.Command(bin, args...)
	cleanup, err := attachQuietIO(cmd)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	cleanup()
	deadline := time.Now().Add(45 * time.Second)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return cmd, nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return nil, fmt.Errorf("llama-server exited during startup")
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, fmt.Errorf("llama-server did not accept connections on %s", addr)
}

func attachQuietIO(cmd *exec.Cmd) (func(), error) {
	if strings.TrimSpace(os.Getenv("RUNHUG_VERBOSE")) != "" {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return func() {}, nil
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	return func() { _ = devNull.Close() }, nil
}

func DefaultURL(port int) string {
	if port <= 0 {
		port = 8081
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1", port)
}
