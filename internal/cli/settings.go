package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
)

func cmdConfig(args []string) error {
	if showCmdHelp(args, "runhug config", printConfigHelp, printConfigHelpFull) {
		return nil
	}
	if len(args) == 0 {
		return printConfigInfo()
	}
	switch strings.ToLower(args[0]) {
	case "get":
		return cmdConfigGet(args[1:])
	case "set":
		return cmdConfigSet(args[1:])
	case "path", "paths", "show":
		return printConfigInfo()
	case "connect":
		return cmdConnect(args[1:])
	case "disconnect":
		return cmdDisconnect(args[1:])
	default:
		printConfigHelp(os.Stderr)
		return fmt.Errorf("unknown config command %q\nRun `runhug config --help`", args[0])
	}
}

func printConfigHelp(w io.Writer) {
	helpUsage(w, "runhug config [command]")
	helpSection(w, "commands")
	helpCmd(w, "(none)", "show config dir, credentials status, settings")
	helpCmd(w, "get <key>", "print one setting")
	helpCmd(w, "set <key> <value>", "write one setting")
	helpCmd(w, "connect [hf]", "save RunPod key or HF token")
	helpCmd(w, "disconnect [hf]", "clear saved credentials")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s %s · %s\n",
		dim("aliases:"), cyan("runhug connect"), cyan("runhug disconnect"),
	)
}

func printConfigHelpFull(w io.Writer) {
	helpUsage(w, "runhug config [command]")
	fmt.Fprintln(w, dim("Settings and credentials. Short list:"), cyan("runhug config --help"))
	fmt.Fprintln(w)
	helpFullSection(w, "commands",
		helpFullEntry{
			Cmd:  "(none) / show / path",
			What: "Print config dir, RunPod/HF credential status, and current settings.",
			When: "Checking where tokens live or what update_limit / advisor URL is set.",
		},
		helpFullEntry{
			Cmd:  "get <key> · set <key> <value>",
			What: "Read or write settings.json keys.",
			When: "Toggling color, Hub update limits, or the recommend advisor endpoint.",
			More: "Keys: no_color, update_limit, advisor_base_url, advisor_model",
		},
		helpFullEntry{
			Cmd:  "connect [hf]",
			What: "Save a RunPod API key (default) or Hugging Face token (connect hf).",
			When: "Before deploy / heretic (RunPod) or gated Hub downloads (HF).",
			More: "Also: runhug connect [hf] (top-level alias).",
		},
		helpFullEntry{
			Cmd:  "disconnect [hf]",
			What: "Remove the saved RunPod key or HF token from disk.",
			When: "Rotating credentials or clearing a machine.",
			More: "Also: runhug disconnect [hf] (top-level alias).",
		},
	)
}

func printConfigInfo() error {
	heading(os.Stdout, "Config")
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	printKV(os.Stdout, "dir", dir)
	if override := config.ConfigPathOverride(); override != "" {
		printKV(os.Stdout, "override", override+"  (RUNHUG_CONFIG / RVP_CONFIG)")
	}
	if p, err := config.StoredKeyPath(); err == nil {
		status := "missing"
		if config.HasStoredKey() {
			status = "present (0600, never printed)"
		}
		printKV(os.Stdout, "runpod", p+"  — "+status)
	}
	if p, err := config.StoredHFTokenPath(); err == nil {
		status := "missing"
		if config.HasStoredHFToken() {
			status = "present (0600, never printed)"
		}
		printKV(os.Stdout, "hf.token", p+"  — "+status)
	}
	if p, err := config.SettingsPath(); err == nil {
		printKV(os.Stdout, "settings", p)
	}
	s := config.LoadSettings()
	printKV(os.Stdout, "no_color", strconv.FormatBool(s.NoColor || config.ColorDisabled()))
	if s.NoColor {
		printKV(os.Stdout, "source", "settings.json")
	} else if os.Getenv("NO_COLOR") != "" {
		printKV(os.Stdout, "source", "NO_COLOR env")
	} else {
		printKV(os.Stdout, "source", "default (colors when TTY)")
	}
	printKV(os.Stdout, "update_limit", strconv.Itoa(config.EffectiveUpdateLimit())+"  (0=unlimited; flag>env>settings>2000)")
	if s.AdvisorBaseURL != "" {
		printKV(os.Stdout, "advisor_base_url", s.AdvisorBaseURL)
	} else {
		printKV(os.Stdout, "advisor_base_url", "http://127.0.0.1:11434/v1  (default Ollama)")
	}
	if s.AdvisorModel != "" {
		printKV(os.Stdout, "advisor_model", s.AdvisorModel)
	} else {
		printKV(os.Stdout, "advisor_model", "(unset — pass --model or set advisor_model)")
	}
	fmt.Fprintln(os.Stdout)
	commands(os.Stdout, "Credentials:",
		"runhug config connect",
		"runhug config connect hf",
		"runhug config disconnect",
	)
	commands(os.Stdout, "Examples:",
		"runhug config set no_color true",
		"runhug config set update_limit 5000",
		"runhug config set advisor_base_url http://127.0.0.1:11434/v1",
		"runhug config get update_limit",
	)
	return nil
}

func cmdConfigGet(args []string) error {
	if len(args) == 0 {
		return printConfigInfo()
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))
	s := config.LoadSettings()
	switch key {
	case "no_color", "nocolor":
		fmt.Println(strconv.FormatBool(s.NoColor))
		return nil
	case "update_limit", "updatelimit":
		fmt.Println(config.EffectiveUpdateLimit())
		return nil
	case "advisor_base_url", "advisor_url":
		if s.AdvisorBaseURL == "" {
			fmt.Println("http://127.0.0.1:11434/v1")
		} else {
			fmt.Println(s.AdvisorBaseURL)
		}
		return nil
	case "advisor_model":
		fmt.Println(s.AdvisorModel)
		return nil
	case "dir", "path":
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		fmt.Println(dir)
		return nil
	default:
		return fmt.Errorf("unknown setting %q — known: no_color, update_limit, advisor_base_url, advisor_model, dir", args[0])
	}
}

func cmdConfigSet(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: runhug config set <key> <value>")
	}
	key := strings.ToLower(strings.TrimSpace(args[0]))
	val := strings.TrimSpace(args[1])
	s := config.LoadSettings()
	switch key {
	case "no_color", "nocolor":
		b, err := parseBoolArg(strings.ToLower(val))
		if err != nil {
			return err
		}
		s.NoColor = b
	case "update_limit", "updatelimit":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("update_limit must be an integer >= 0 (0=unlimited)")
		}
		s.UpdateLimit = &n
	case "advisor_base_url", "advisor_url":
		s.AdvisorBaseURL = strings.TrimRight(val, "/")
	case "advisor_model":
		s.AdvisorModel = val
	default:
		return fmt.Errorf("unknown setting %q — known: no_color, update_limit, advisor_base_url, advisor_model", args[0])
	}
	if err := config.SaveSettings(s); err != nil {
		return err
	}
	path, _ := config.SettingsPath()
	fmt.Fprintf(os.Stdout, "%s  %s=%s\n", green("Saved"), key, val)
	if path != "" {
		printKV(os.Stdout, "file", path)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func parseBoolArg(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected true|false, got %q", s)
	}
}
