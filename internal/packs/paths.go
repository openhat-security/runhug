package packs

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
)

const (
	EnvPacksRepo   = "RUNHUG_PACKS_REPO"
	EnvIndexLimit  = "RUNHUG_INDEX_LIMIT"
	DefaultRepo    = "openhat-security/runhug"
	packsSubdir    = "packs"
	installedName  = "installed.json"
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

// ReleaseRepo returns owner/name for pack downloads.
func ReleaseRepo() string {
	if v := strings.TrimSpace(os.Getenv(EnvPacksRepo)); v != "" {
		return strings.TrimPrefix(strings.TrimPrefix(v, "https://github.com/"), "/")
	}
	return DefaultRepo
}
