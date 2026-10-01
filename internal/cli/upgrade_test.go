package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIAssetName(t *testing.T) {
	name := cliAssetName("0.1.7")
	switch runtime.GOOS {
	case "windows":
		if name != "runhug_0.1.7_windows_"+runtime.GOARCH+".zip" &&
			!(runtime.GOARCH == "amd64" && name == "runhug_0.1.7_windows_amd64.zip") {
			t.Fatalf("windows asset = %q", name)
		}
	default:
		wantArch := runtime.GOARCH
		if wantArch == "x86_64" {
			wantArch = "amd64"
		}
		want := "runhug_0.1.7_" + runtime.GOOS + "_" + wantArch
		if name != want {
			t.Fatalf("asset = %q want %q", name, want)
		}
	}
}

func TestDetectInstallMethodCaskroom(t *testing.T) {
	got := detectInstallMethod(filepath.FromSlash("/opt/homebrew/Caskroom/runhug/0.1.7/runhug_0.1.7_darwin_arm64"))
	if got != "brew" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectInstallMethodGithub(t *testing.T) {
	got := detectInstallMethod(filepath.Join(t.TempDir(), "runhug"))
	if got != "github" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectInstallMethodWinget(t *testing.T) {
	p := filepath.FromSlash("/Users/x/AppData/Local/Microsoft/WinGet/Packages/OpenHatSecurity.Runhug_8wekyb3d8bbwe/runhug.exe")
	got := detectInstallMethod(p)
	if got != "winget" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectInstallMethodNPM(t *testing.T) {
	p := filepath.FromSlash("/usr/local/lib/node_modules/runhug/bin/runhug")
	got := detectInstallMethod(p)
	if got != "npm" {
		t.Fatalf("got %q", got)
	}
}

func TestDetectInstallMethodGo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOPATH", "")
	t.Setenv("GOBIN", "")
	bin := filepath.Join(home, "go", "bin", "runhug")
	if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
		t.Fatal(err)
	}
	got := detectInstallMethod(bin)
	if got != "go" {
		t.Fatalf("got %q want go", got)
	}
}

func TestDetectInstallMethodGoBin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOBIN", dir)
	t.Setenv("GOPATH", t.TempDir())
	bin := filepath.Join(dir, "runhug")
	got := detectInstallMethod(bin)
	if got != "go" {
		t.Fatalf("got %q", got)
	}
}

func TestUpgradeHelpMentionsMethods(t *testing.T) {
	var b strings.Builder
	printUpgradeHelp(&b)
	s := b.String()
	for _, want := range []string{"brew", "winget", "npm", "github", "update --cli"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
