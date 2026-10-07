//go:build unix

package cli

import (
	"os"
	"syscall"
)

func setStdinNonblock(v bool) error {
	return syscall.SetNonblock(int(os.Stdin.Fd()), v)
}
