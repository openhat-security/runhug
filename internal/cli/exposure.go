package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adamsiwiec1/runhug/internal/netguard"
)

// exposure is the shared loopback-default / fail-closed-key policy every
// listener (`start <agent>` bridges, `proxy`) inherits. See internal/netguard.
type exposure struct {
	host        string
	allowRemote bool
	key         string
	keyFile     string // set when the key was generated
}

type exposureFlags struct {
	host        *string
	allowRemote *bool
	generateKey *bool
}

func addExposureFlags(fs *flag.FlagSet, hostName, hostHelp string) exposureFlags {
	return exposureFlags{
		host:        fs.String(hostName, "", hostHelp),
		allowRemote: fs.Bool("allow-remote", false, "allow binding/accepting non-loopback connections (also "+netguard.EnvAllowRemote+"=1); requires a strong key in "+netguard.EnvAPIKey+" or --generate-key"),
		generateKey: fs.Bool("generate-key", false, "generate a strong random API key (written to a 0600 file, never printed)"),
	}
}

func (f exposureFlags) resolve() (exposure, error) {
	e := exposure{
		host:        strings.TrimSpace(*f.host),
		allowRemote: *f.allowRemote || netguard.Truthy(os.Getenv(netguard.EnvAllowRemote)),
		key:         strings.TrimSpace(os.Getenv(netguard.EnvAPIKey)),
	}
	if *f.generateKey {
		k, err := netguard.GenerateKey()
		if err != nil {
			return e, fmt.Errorf("generate key: %w", err)
		}
		path, err := writeKeyFile(k)
		if err != nil {
			return e, err
		}
		e.key, e.keyFile = k, path
	}
	return e, nil
}

// remote reports whether the effective bind host is off-loopback.
func (e exposure) remote() bool {
	return e.host != "" && !netguard.IsLoopbackHost(e.host)
}

func writeKeyFile(key string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "runhug")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "api-key")
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
