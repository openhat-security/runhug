package cli

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adamsiwiec1/runhug/internal/packs"
	"github.com/adamsiwiec1/runhug/internal/version"
)

func cmdUpgrade(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		printUpgradeHelp(os.Stdout)
		return nil
	}
	fs := newFlagSet("upgrade")
	check := fs.Bool("check", false, "only report whether a newer release exists")
	force := fs.Bool("force", false, "reinstall even if already on latest")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	return upgradeCLI(*check, *force)
}

func printUpgradeHelp(w io.Writer) {
	helpUsage(w, "runhug upgrade")
	fmt.Fprintln(w, dim("Detect how this binary was installed, then upgrade that way"))
	fmt.Fprintln(w)

	helpSection(w, "flags")
	helpFlag(w, "--check", "report only (no install)")
	helpFlag(w, "--force", "reinstall even if already latest")
	fmt.Fprintln(w)

	helpSection(w, "methods")
	fmt.Fprintln(w, dim("  brew → brew upgrade --cask openhat-security/tap/runhug"))
	fmt.Fprintln(w, dim("  scoop → scoop update runhug"))
	fmt.Fprintln(w, dim("  winget → winget upgrade --id OpenHatSecurity.Runhug"))
	fmt.Fprintln(w, dim("  npm → npm install -g runhug@<latest>"))
	fmt.Fprintln(w, dim("  apt / dnf → sudo apt/dnf upgrade runhug"))
	fmt.Fprintln(w, dim("  aur → yay/paru -Syu runhug-bin (or pacman -Syu)"))
	fmt.Fprintln(w, dim("  go → go install github.com/adamsiwiec1/runhug/cmd/runhug@latest"))
	fmt.Fprintln(w, dim("  github → replace this binary from the latest Release asset"))
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%s %s\n", dim("alias:"), cyan("runhug update --cli")+" / "+cyan("runhug update self"))
}

func upgradeCLI(checkOnly, force bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	latest, assetURL, err := latestCLIAsset(ctx)
	if err != nil {
		return err
	}
	cur := strings.TrimPrefix(version.Version, "v")
	lat := strings.TrimPrefix(latest, "v")

	heading(os.Stdout, "Upgrade CLI")
	fmt.Fprintf(os.Stdout, "Current: %s %s\n", version.Name, version.Version)
	fmt.Fprintf(os.Stdout, "Latest:  %s\n", latest)
	fmt.Fprintf(os.Stdout, "Binary:  %s\n", exe)

	method := detectInstallMethod(exe)
	fmt.Fprintf(os.Stdout, "Install: %s\n", method)
	fmt.Fprintln(os.Stdout)

	if !force && cur == lat {
		fmt.Fprintln(os.Stdout, green("Already up to date."))
		return nil
	}
	if checkOnly {
		if cur != lat {
			fmt.Fprintf(os.Stdout, "%s %s → %s available\n", yellow("update:"), cur, lat)
			fmt.Fprintln(os.Stdout, dim("Run: runhug upgrade   (or: runhug update --cli)"))
		}
		return nil
	}

	switch method {
	case "brew":
		return runUpgradeCmd("brew", "upgrade", "--cask", "openhat-security/tap/runhug")
	case "scoop":
		return runUpgradeCmd("scoop", "update", "runhug")
	case "winget":
		return runUpgradeCmd("winget", "upgrade", "--id", "OpenHatSecurity.Runhug", "--accept-package-agreements", "--accept-source-agreements")
	case "npm":
		return runUpgradeCmd("npm", "install", "-g", "runhug@"+lat)
	case "apt":
		return runUpgradeCmd("sudo", "apt-get", "install", "-y", "--only-upgrade", "runhug")
	case "dnf":
		return runUpgradeCmd("sudo", "dnf", "upgrade", "-y", "runhug")
	case "aur":
		return upgradeAUR()
	case "go":
		return runUpgradeCmd("go", "install", "github.com/adamsiwiec1/runhug/cmd/runhug@latest")
	default:
		return replaceBinaryFromURL(ctx, exe, assetURL, latest)
	}
}

func detectInstallMethod(exe string) string {
	low := strings.ToLower(filepath.ToSlash(exe))
	switch {
	case strings.Contains(low, "/caskroom/runhug/"):
		return "brew"
	case brewCaskInstalled() && isBrewLinkedBin(exe):
		return "brew"
	case strings.Contains(low, "/scoop/apps/runhug/"):
		return "scoop"
	case strings.Contains(low, "/winget/packages/") && strings.Contains(low, "openhatsecurity.runhug"):
		return "winget"
	case strings.Contains(low, "/microsoft/winget/packages/") && strings.Contains(low, "runhug"):
		return "winget"
	case strings.Contains(low, "/node_modules/"):
		return "npm"
	case isGoInstallBin(exe):
		return "go"
	case strings.HasPrefix(exe, "/usr/bin/runhug") && fileExists("/etc/apt/sources.list.d/runhug.list"):
		return "apt"
	case strings.HasPrefix(exe, "/usr/bin/runhug") && fileExists("/etc/yum.repos.d/runhug.repo"):
		return "dnf"
	case strings.HasPrefix(exe, "/usr/bin/runhug") && pacmanOwnsRunhug():
		return "aur"
	default:
		return "github"
	}
}

func isGoInstallBin(exe string) bool {
	base := filepath.Base(exe)
	if base != "runhug" && base != "runhug.exe" {
		return false
	}
	dir := filepath.Clean(filepath.Dir(exe))
	candidates := goBinDirs()
	for _, c := range candidates {
		if c != "" && dir == filepath.Clean(c) {
			return true
		}
	}
	return false
}

func goBinDirs() []string {
	var out []string
	if gopath := strings.TrimSpace(os.Getenv("GOPATH")); gopath != "" {
		for _, p := range filepath.SplitList(gopath) {
			out = append(out, filepath.Join(p, "bin"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "go", "bin"))
	}
	if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
		out = append(out, gobin)
	}
	return out
}

func pacmanOwnsRunhug() bool {
	if _, err := exec.LookPath("pacman"); err != nil {
		return false
	}
	return exec.Command("pacman", "-Q", "runhug-bin").Run() == nil ||
		exec.Command("pacman", "-Q", "runhug").Run() == nil
}

func upgradeAUR() error {
	for _, helper := range []string{"yay", "paru"} {
		if _, err := exec.LookPath(helper); err == nil {
			pkg := "runhug-bin"
			if exec.Command("pacman", "-Q", "runhug").Run() == nil &&
				exec.Command("pacman", "-Q", "runhug-bin").Run() != nil {
				pkg = "runhug"
			}
			return runUpgradeCmd(helper, "-Syu", "--noconfirm", pkg)
		}
	}
	fmt.Fprintln(os.Stdout, yellow("AUR helper not found (yay/paru)."))
	fmt.Fprintln(os.Stdout, dim("Update with: yay -Syu runhug-bin   or   paru -Syu runhug-bin"))
	return fmt.Errorf("aur: install yay or paru, then re-run runhug upgrade")
}

func brewCaskInstalled() bool {
	return exec.Command("brew", "list", "--cask", "runhug").Run() == nil
}

func isBrewLinkedBin(exe string) bool {
	out, err := exec.Command("brew", "--prefix").Output()
	if err != nil {
		return false
	}
	prefix := strings.TrimSpace(string(out))
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(exe, filepath.Join(prefix, "bin")+string(os.PathSeparator)) ||
		exe == filepath.Join(prefix, "bin", "runhug")
}

func runUpgradeCmd(name string, args ...string) error {
	fmt.Fprintf(os.Stdout, "→ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w\nHint: install/upgrade manually — see https://github.com/openhat-security/runhug#install", name, err)
	}
	fmt.Fprintln(os.Stdout, green("OK"))
	return nil
}

func latestCLIAsset(ctx context.Context) (tag, downloadURL string, err error) {
	repo := packs.ReleaseRepo()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return "", "", err
	}
	if res.StatusCode >= 300 {
		return "", "", fmt.Errorf("github releases: HTTP %d", res.StatusCode)
	}
	var parsed struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", err
	}
	ver := strings.TrimPrefix(parsed.TagName, "v")
	name := cliAssetName(ver)
	candidates := []string{name}
	if strings.HasSuffix(name, ".zip") {
		candidates = append(candidates, strings.TrimSuffix(name, ".zip")+".exe")
	}
	candidates = append(candidates, strings.Replace(name, "runhug_", "runhug-cli_", 1))
	for _, want := range candidates {
		for _, a := range parsed.Assets {
			if a.Name == want && a.URL != "" {
				return parsed.TagName, a.URL, nil
			}
		}
	}
	return "", "", fmt.Errorf("release %s has no asset matching %q", parsed.TagName, name)
}

func cliAssetName(ver string) string {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	switch goarch {
	case "x86_64":
		goarch = "amd64"
	case "aarch64":
		goarch = "arm64"
	}
	switch goos {
	case "windows":
		return fmt.Sprintf("runhug_%s_%s_%s.zip", ver, goos, goarch)
	default:
		return fmt.Sprintf("runhug_%s_%s_%s", ver, goos, goarch)
	}
}

func replaceBinaryFromURL(ctx context.Context, exe, url, tag string) error {
	fmt.Fprintf(os.Stdout, "→ downloading %s\n", url)
	tmpDir, err := os.MkdirTemp("", "runhug-upgrade-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	rawPath := filepath.Join(tmpDir, "download")
	if err := downloadToFile(ctx, url, rawPath); err != nil {
		return err
	}

	newBin := rawPath
	if strings.HasSuffix(strings.ToLower(url), ".zip") {
		extracted, err := extractRunhugFromZip(rawPath, tmpDir)
		if err != nil {
			return err
		}
		newBin = extracted
	}
	if err := os.Chmod(newBin, 0o755); err != nil {
		return err
	}

	dest := exe
	backup := exe + ".bak"
	staged := exe + ".new"
	if err := copyFile(newBin, staged); err != nil {
		return err
	}
	_ = os.Chmod(staged, 0o755)
	_ = os.Remove(backup)
	if err := os.Rename(dest, backup); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("replace binary (need write access to %s): %w", dest, err)
	}
	if err := os.Rename(staged, dest); err != nil {
		_ = os.Rename(backup, dest)
		return err
	}
	_ = os.Remove(backup)
	fmt.Fprintf(os.Stdout, "%s  %s → %s\n", green("OK"), dest, tag)
	fmt.Fprintln(os.Stdout, dim("Restart any long-running shells if PATH caches the old inode."))
	return nil
}

func downloadToFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", version.Name+"/"+version.Version)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("download: HTTP %d", res.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, res.Body)
	return err
}

func extractRunhugFromZip(zipPath, destDir string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()
	want := "runhug.exe"
	if runtime.GOOS != "windows" {
		want = "runhug"
	}
	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if base != want && base != "runhug" && base != "runhug.exe" {
			continue
		}
		out := filepath.Join(destDir, base)
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(w, rc)
		rc.Close()
		w.Close()
		if copyErr != nil {
			return "", copyErr
		}
		return out, nil
	}
	return "", fmt.Errorf("zip has no runhug binary")
}
