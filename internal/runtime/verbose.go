package runtime

import (
	"os"
	"strings"
)

// Verbose reports whether RUNHUG_VERBOSE is set (child logs + extra CLI chrome).
func Verbose() bool {
	return strings.TrimSpace(os.Getenv("RUNHUG_VERBOSE")) != ""
}
