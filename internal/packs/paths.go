package packs

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
)

const (
	EnvPacksRepo = "RUNHUG_PACKS_REPO"
	EnvCLIRepo   = "RUNHUG_REPO"

	// DefaultRepo is the pack download source (hfpacks Releases).
	DefaultRepo = "openhat-security/hfpacks"
	// DefaultCLIRepo hosts CLI binaries and gpudb release assets.
	DefaultCLIRepo = "openhat-security/runhug"

	packsSubdir   = "packs"
	installedName = "installed.json"
)

// PacksDir is ~/.config/runhug/packs (or under RUNHUG_CONFIG parent).
func PacksDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, packsSubdir), nil
}

// PackDBPath is packs/<id>.db under the config dir.
func PackDBPath(id string) (string, error) {
	dir, err := PacksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".db"), nil
}

// InstalledPath is packs/installed.json (selected categories + watermarks).
func InstalledPath() (string, error) {
	dir, err := PacksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, installedName), nil
}

// ReleaseRepo returns owner/name for pack downloads (hfpacks by default).
func ReleaseRepo() string {
	if v := strings.TrimSpace(os.Getenv(EnvPacksRepo)); v != "" {
		return strings.TrimPrefix(strings.TrimPrefix(v, "https://github.com/"), "/")
	}
	return DefaultRepo
}

// CLIRepo returns owner/name for CLI binary / gpudb release assets.
func CLIRepo() string {
	if v := strings.TrimSpace(os.Getenv(EnvCLIRepo)); v != "" {
		return strings.TrimPrefix(strings.TrimPrefix(v, "https://github.com/"), "/")
	}
	return DefaultCLIRepo
}
