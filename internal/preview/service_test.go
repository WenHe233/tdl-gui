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

func TestDocumentThumbSupportsProgressivePreview(t *testing.T) {
	d := &tg.Document{ID: 1, AccessHash: 2, DCID: 3, Thumbs: []tg.PhotoSizeClass{
		&tg.PhotoStrippedSize{Type: "i", Bytes: []byte{1, 2, 3}},
		&tg.PhotoSizeProgressive{Type: "m", W: 320, H: 180, Sizes: []int{100, 700}},
	}}
	m, ok := documentThumb(d)
	if !ok {
		t.Fatal("document thumbnail not selected")
	}
	loc, ok := m.InputFileLoc.(*tg.InputDocumentFileLocation)
	if !ok || loc.ThumbSize != "m" || m.Size != 700 {
		t.Fatalf("unexpected thumbnail: %+v", m)
	}
}

func TestThumbnailMediaPrefersVideoCover(t *testing.T) {
	cover := &tg.Photo{ID: 4, AccessHash: 5, DCID: 6, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "m", W: 320, H: 180, Size: 500}}}
	doc := &tg.Document{ID: 1, AccessHash: 2, DCID: 3}
	thumb, ok := thumbnailMedia(&tg.Message{Media: &tg.MessageMediaDocument{Document: doc, VideoCover: cover}})
	if !ok {
		t.Fatal("video cover not selected")
	}
	loc, ok := thumb.InputFileLoc.(*tg.InputPhotoFileLocation)
	if !ok || loc.ID != cover.ID {
		t.Fatalf("unexpected cover location: %+v", thumb.InputFileLoc)
	}
}
