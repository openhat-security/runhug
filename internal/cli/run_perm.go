package cli

import (
	"fmt"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
)

const (
	permAsk   = "ask"
	permAllow = "allow"
	permDeny  = "deny"
)

func normalizeRunPerm(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case permAllow, "on", "yes":
		return permAllow
	case permDeny, "off", "no":
		return permDeny
	default:
		return permAsk
	}
}

func toolNeedsPermission(name string) bool {
	switch name {
	case "write_file", "append_file", "edit_file", "bash":
		return true
	default:
		return false
	}
}

func applyRunSettingsDefaults(s *runSession) {
	if s == nil {
		return
	}
	st := config.LoadSettings()
	if s.Perm == "" {
		s.Perm = normalizeRunPerm(st.RunPerm)
	}
	if !s.Plan && strings.EqualFold(strings.TrimSpace(st.RunMode), "plan") {
		s.Plan = true
	}
}

func saveRunModeSettings(s *runSession) {
	if s == nil {
		return
	}
	st := config.LoadSettings()
	if s.Plan {
		st.RunMode = "plan"
	} else {
		st.RunMode = "agent"
	}
	st.RunPerm = normalizeRunPerm(s.Perm)
	_ = config.SaveSettings(st)
}

func (s *runSession) decideToolPerm(name, argsJSON string) error {
	if s == nil || !toolNeedsPermission(name) {
		return nil
	}
	if s.Plan {
		return fmt.Errorf("plan mode — mutating tools are off; /agent to implement")
	}
	if s.PermNever[name] {
		return fmt.Errorf("permission denied (%s never)", name)
	}
	perm := normalizeRunPerm(s.Perm)
	if perm == permDeny {
		return fmt.Errorf("permissions are deny — /perm ask or /perm allow")
	}
	if perm == permAllow || s.PermAlways[name] {
		return nil
	}
	sum := summarizeToolArgs(name, argsJSON)
	title := dim("permission") + "  " + bold(name)
	if sum != "" {
		title += "  " + dim(sum)
	}
	id, err := promptChoice(s.tty(), nil, title, permChoiceOpts(), 0)
	if err != nil {
		return fmt.Errorf("permission cancelled: %w", err)
	}
	switch id {
	case "allow":
		return nil
	case "always":
		if s.PermAlways == nil {
			s.PermAlways = map[string]bool{}
		}
		s.PermAlways[name] = true
		delete(s.PermNever, name)
		return nil
	case "never":
		if s.PermNever == nil {
			s.PermNever = map[string]bool{}
		}
		s.PermNever[name] = true
		delete(s.PermAlways, name)
		return fmt.Errorf("permission denied (%s never)", name)
	default:
		return fmt.Errorf("permission denied")
	}
}
