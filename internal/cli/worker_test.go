package cli

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/tdl-gui/internal/domain"
)

func TestImageData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thumb.jpg")
	want := []byte{0xff, 0xd8, 0xff, 0xd9}
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got := imageData(path)
	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("imageData() = %q, want JPEG data URI", got)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(want) {
		t.Fatalf("decoded bytes = %v, want %v", decoded, want)
	}
}

func TestMediaWithImageDataDoesNotExposeMissingPath(t *testing.T) {
	items := []domain.Media{{MessageID: "1", ThumbPath: filepath.Join(t.TempDir(), "missing.jpg")}}
	got := mediaWithImageData(items)
	if got[0].ThumbPath != "" {
		t.Fatalf("ThumbPath = %q, want empty", got[0].ThumbPath)
	}
}
