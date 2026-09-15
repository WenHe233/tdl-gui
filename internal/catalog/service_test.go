package catalog

import (
	"encoding/json"
	"testing"
)

func TestParseDocumentMedia(t *testing.T) {
	raw := map[string]any{"date": json.Number("1726300000"), "message": "caption", "grouped_id": json.Number("99"), "media": map[string]any{"document": map[string]any{"id": json.Number("123"), "size": json.Number("456"), "mime_type": "video/mp4", "attributes": []any{map[string]any{"type": "documentAttributeFilename", "file_name": "clip.mp4"}, map[string]any{"type": "documentAttributeVideo", "duration": json.Number("12"), "w": json.Number("1920"), "h": json.Number("1080")}}}}}
	m, ok := parseMedia("a", "c", "7", "", 0, "", raw)
	if !ok {
		t.Fatal("media not parsed")
	}
	if m.MediaID != "123" || m.Kind != "video" || m.FileName != "clip.mp4" || m.Size != 456 || m.Duration != 12 || m.Width != 1920 || m.GroupedID != "99" {
		t.Fatalf("unexpected media: %+v", m)
	}
}

func TestJSONPayloadRemovesProgressNoise(t *testing.T) {
	b := []byte("progress\n\x1b[32mOK\x1b[0m\n[{\"id\":1}]\n")
	got := string(jsonPayload(b, '[', ']'))
	if got != "[{\"id\":1}]" {
		t.Fatalf("got %q", got)
	}
}

func TestParseProgressivePhotoSize(t *testing.T) {
	raw := map[string]any{"date": json.Number("1726300000"), "media": map[string]any{"photo": map[string]any{"id": json.Number("8"), "sizes": []any{map[string]any{"type": "x", "w": json.Number("1280"), "h": json.Number("720"), "sizes": []any{json.Number("100"), json.Number("900")}}}}}}
	m, ok := parseMedia("a", "c", "9", "", 0, "", raw)
	if !ok || m.Size != 900 || m.Kind != "photo" {
		t.Fatalf("unexpected media: %+v ok=%v", m, ok)
	}
}

func TestVoiceAttributeOverridesAudioMime(t *testing.T) {
	raw := map[string]any{"date": json.Number("1726300000"), "media": map[string]any{"document": map[string]any{"id": json.Number("8"), "size": json.Number("100"), "mime_type": "audio/ogg", "attributes": []any{map[string]any{"voice": true, "duration": json.Number("3")}}}}}
	m, ok := parseMedia("a", "c", "9", "", 0, "", raw)
	if !ok || m.Kind != "voice" {
		t.Fatalf("unexpected media: %+v", m)
	}
}

func TestParseVideoKindFromFileExtension(t *testing.T) {
	raw := map[string]any{"date": json.Number("1726300000"), "media": map[string]any{"document": map[string]any{"id": json.Number("8"), "size": json.Number("100")}}}
	m, ok := parseMedia("a", "c", "9", "clip.MP4", 0, "", raw)
	if !ok || m.Kind != "video" || m.Extension != ".mp4" {
		t.Fatalf("unexpected media: %+v ok=%v", m, ok)
	}
}
