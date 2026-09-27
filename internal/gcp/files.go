package gcp

import (
	"os"
	"path/filepath"
)

func writeFile(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if filepath.Base(path) == "entrypoint.sh" {
		mode = 0o755
	}
	return os.WriteFile(path, []byte(contents), mode)
}
