package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelectReleaseAssets(t *testing.T) {
	name := "tdl_Linux_64bit.tar.gz"
	if runtime.GOOS == "windows" {
		name = "tdl_Windows_64bit.zip"
	}
	pkg, checks := selectAssets([]asset{{Name: name, BrowserDownloadURL: "package"}, {Name: "checksums.txt", BrowserDownloadURL: "checksum"}})
	if runtime.GOOS == "windows" && pkg.BrowserDownloadURL != "package" {
		t.Fatalf("package not selected: %+v", pkg)
	}
	if checks.BrowserDownloadURL != "checksum" {
		t.Fatal("checksum not selected")
	}
}
func TestChecksumFor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(p, []byte("abcdef  *tdl_Windows_64bit.zip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := checksumFor(p, "tdl_Windows_64bit.zip")
	if err != nil || got != "abcdef" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
