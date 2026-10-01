package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/adamsiwiec1/runhug/internal/config"
	"github.com/adamsiwiec1/runhug/internal/version"
)

// bannerASCII is a compact ≤80-col block banner spelling RUNHUG.
const bannerASCII = ` ____  _   _ _   _ _   _ _   _  ____
|  _ \| | | | \ | | | | | | | |/ ___|
| |_) | | | |  \| | |_| | | | | |  _
|  _ <| |_| | |\  |  _  | |_| | |_| |
|_| \_\\___/|_| \_|_| |_|\___/ \____|`

// showBanner reports whether help/chrome should print the ASCII banner.
// Skipped for non-TTY (piped help) and when color/settings disable chrome.
func showBanner() bool {
	if !stdoutIsTTY() {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if config.LoadSettings().NoColor {
		return false
	}
	return true
}

func printBanner(w io.Writer) {
	if !showBanner() {
		return
	}
	fmt.Fprintln(w, cyan(bannerASCII))
	fmt.Fprintln(w)
}

func printTagline(w io.Writer) {
	fmt.Fprintf(w, "%s %s — %s\n",
		boldCyan(version.Name),
		yellow(version.Version),
		dim("find, deploy, and run Hugging Face models."))
	fmt.Fprintln(w)
}
