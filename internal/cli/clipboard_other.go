//go:build !darwin

package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

func pasteFromClipboard() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("wl-paste"); err == nil {
			out, err := exec.Command("wl-paste", "-n").Output()
			return string(out), err
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
			return string(out), err
		}
	}
	return "", fmt.Errorf("clipboard paste not supported on %s", runtime.GOOS)
}

func copyToClipboard(text string) error {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd := exec.Command("wl-copy")
			in, err := cmd.StdinPipe()
			if err != nil {
				return err
			}
			if err := cmd.Start(); err != nil {
				return err
			}
			_, _ = in.Write([]byte(text))
			_ = in.Close()
			return cmd.Wait()
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd := exec.Command("xclip", "-selection", "clipboard")
			in, err := cmd.StdinPipe()
			if err != nil {
				return err
			}
			if err := cmd.Start(); err != nil {
				return err
			}
			_, _ = in.Write([]byte(text))
			_ = in.Close()
			return cmd.Wait()
		}
	}
	return fmt.Errorf("clipboard copy not supported on %s", runtime.GOOS)
}
