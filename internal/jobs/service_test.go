package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/tdl-gui/internal/domain"
)

func TestManifestAndVerification(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	items := []domain.JobItem{{ChatID: "123", MessageID: "7", TargetPath: `C:\downloads\clip.mp4`, Size: 3}}
	if err := writeManifest(manifest, "123", items); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(manifest)
	if !strings.Contains(string(b), `"id":7`) {
		t.Fatalf("manifest=%s", b)
	}
	file := filepath.Join(dir, "7_clip.mp4")
	if err := os.WriteFile(file, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findDownload(dir, "7", 3); got != file {
		t.Fatalf("got %q", got)
	}
}
func TestUniqueTargetDoesNotOverwriteDifferentFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.mp4")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := uniqueTarget(p, "42", 99)
	if got == p || !strings.HasSuffix(got, "x-42.mp4") {
		t.Fatalf("got %s", got)
	}
}

func TestDiskCheckUsesExistingAncestor(t *testing.T) {
	target := filepath.Join(t.TempDir(), "new", "nested", "file.bin")
	if err := checkDisk([]domain.JobItem{{TargetPath: target, Size: 1}}, 0); err != nil {
		t.Fatal(err)
	}
}
