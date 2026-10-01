package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// helpModeFromArgs returns "short", "full", or "" if args are not a help request.
// Recognizes: -h | --help | help [| full|--full|--help-full]
//
//	--help-full | help-full
func helpModeFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	a0 := strings.ToLower(strings.TrimSpace(args[0]))
	if isHelpFullArg(a0) {
		return "full"
	}
	if isHelpArg(a0) {
		if wantsFullHelp(args[1:]) {
			return "full"
		}
		return "short"
	}
	return ""
}

func isHelpFullArg(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "--help-full", "help-full":
		return true
	default:
		return false
	}
}

// showCmdHelp prints short or full help when args explicitly request it.
// Short help includes a details hint pointing at cmd --help-full.
// Returns true if handled. Bare command invocations (no help flag) return false —
// callers that print usage on empty args should not add the details hint.
func showCmdHelp(args []string, cmd string, short, full func(io.Writer)) bool {
	mode := helpModeFromArgs(args)
	if mode == "" {
		return false
	}
	if mode == "full" {
		full(os.Stdout)
		return true
	}
	short(os.Stdout)
	printHelpDetailsHint(os.Stdout, cmd)
	return true
}

// printHelpDetailsHint points short help at the full page for this command.
// Only used when the user explicitly asked for --help / help.
func printHelpDetailsHint(w io.Writer, cmd string) {
	fmt.Fprintf(w, "%s %s\n", dim("details:"), cyan(strings.TrimSpace(cmd)+" --help-full"))
}
