package gcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/config"
)

// GenerateBearer returns a random opaque API key for llama-server --api-key.
// The key is managed by the CLI (tunnel / env), never baked into the image.
func GenerateBearer() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "rh_" + hex.EncodeToString(b[:]), nil
}

func tokenDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gcp"), nil
}

// TokenPath is where the CLI stores the per-instance Bearer (0600).
func TokenPath(instance string) (string, error) {
	instance = sanitizeName(instance)
	if instance == "" {
		return "", fmt.Errorf("empty instance name")
	}
	dir, err := tokenDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, instance+".token"), nil
}

// SaveBearer writes the Bearer for an instance (CLI-managed; not in the image).
func SaveBearer(instance, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("empty bearer")
	}
	path, err := TokenPath(instance)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// LoadBearer reads a previously saved Bearer for IAP tunnel / clients.
func LoadBearer(instance string) (string, error) {
	path, err := TokenPath(instance)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
