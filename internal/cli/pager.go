package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// pageText opens body in a read-only viewer when stdout is a TTY (vim -R, else less).
// Non-TTY callers dump plain text. Override with PAGER; --stdout callers should print themselves.
func pageText(body string) error {
	if !stdoutIsTTY() || !stdinIsTTY() {
		_, err := io.WriteString(os.Stdout, body)
		if !strings.HasSuffix(body, "\n") && err == nil {
			_, err = io.WriteString(os.Stdout, "\n")
		}
		return err
	}

	f, err := os.CreateTemp("", "runhug-page-*.txt")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if _, err := io.WriteString(f, body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	pager, args := resolvePager(path)
	cmd := exec.Command(pager, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_, werr := io.WriteString(os.Stdout, body)
		if werr != nil {
			return werr
		}
		return fmt.Errorf("pager %s: %w (content printed to stdout)", pager, err)
	}
	return nil
}

func resolvePager(path string) (string, []string) {
	if p := strings.TrimSpace(os.Getenv("PAGER")); p != "" {
		fields := strings.Fields(p)
		if len(fields) == 1 {
			switch fields[0] {
			case "less":
				return "less", []string{"-R", "-S", "-X", path}
			case "vim", "view", "nvim":
				return fields[0], vimReadonlyArgs(fields[0], path)
			default:
				return fields[0], []string{path}
			}
		}
		return fields[0], append(fields[1:], path)
	}
	// Prefer vim -R so :q! works like a readonly buffer (man-style browse).
	for _, bin := range []string{"vim", "view", "nvim"} {
		if _, err := exec.LookPath(bin); err == nil {
			return bin, vimReadonlyArgs(bin, path)
		}
	}
	if _, err := exec.LookPath("less"); err == nil {
		return "less", []string{"-R", "-S", "-X", path}
	}
	if _, err := exec.LookPath("more"); err == nil {
		return "more", []string{path}
	}
	return "cat", []string{path}
}

func vimReadonlyArgs(bin, path string) []string {
	switch bin {
	case "view":
		// view is already vim -R on most installs.
		return []string{"-n", "-c", "set nomodifiable", path}
	case "nvim":
		return []string{"-R", "-n", "-c", "set nomodifiable", path}
	default:
		return []string{"-R", "-n", "-c", "set nomodifiable", path}
	}
}
