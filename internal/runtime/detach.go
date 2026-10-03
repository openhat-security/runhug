package runtime

import "os/exec"

// DetachProcess puts cmd in a new session so a long-lived child (tunnel,
// ollama serve) is not tied to the parent's TTY and survives parent exit.
func DetachProcess(cmd *exec.Cmd) {
	detach(cmd)
}
