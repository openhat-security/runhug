package cli

import (
	"path/filepath"
	"runtime"
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
