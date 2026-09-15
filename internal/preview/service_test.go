package preview

import (
	"github.com/gotd/td/tg"
	"testing"
)

func TestPhotoThumbChoosesLargestPreviewBelowLimit(t *testing.T) {
	p := &tg.Photo{ID: 1, AccessHash: 2, DCID: 3, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "s", W: 90, H: 90, Size: 100}, &tg.PhotoSize{Type: "m", W: 320, H: 320, Size: 500}, &tg.PhotoSize{Type: "x", W: 1280, H: 1280, Size: 5000}}}
	m, ok := photoThumb(p)
	if !ok {
		t.Fatal("thumbnail not selected")
	}
	loc, ok := m.InputFileLoc.(*tg.InputPhotoFileLocation)
	if !ok || loc.ThumbSize != "m" || m.Size != 500 {
		t.Fatalf("unexpected thumbnail: %+v", m)
	}
}
