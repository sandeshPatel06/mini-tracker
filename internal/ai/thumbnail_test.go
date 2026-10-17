package ai

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/chai2010/webp"
)

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()

	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

func TestThumbnailForImageCreatesWebPThumbnail(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.png")
	writeTestPNG(t, src, 1000, 500)

	thumb := ThumbnailForImage(src, dir)
	if thumb == src {
		t.Fatalf("expected thumbnail path, got original source path %q", src)
	}

	info, err := os.Stat(thumb)
	if err != nil {
		t.Fatalf("thumbnail file does not exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("thumbnail file is empty")
	}

	f, err := os.Open(thumb)
	if err != nil {
		t.Fatalf("open thumbnail: %v", err)
	}
	defer f.Close()

	img, err := webp.Decode(f)
	if err != nil {
		t.Fatalf("decode thumbnail as webp: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() > 512 || bounds.Dy() > 512 {
		t.Fatalf("thumbnail too large: %dx%d", bounds.Dx(), bounds.Dy())
	}
	if bounds.Dx() != 512 || bounds.Dy() != 256 {
		t.Fatalf("unexpected thumbnail dimensions: %dx%d, want 512x256", bounds.Dx(), bounds.Dy())
	}
}

func TestThumbnailForImageSecondCallReturnsSamePath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.png")
	writeTestPNG(t, src, 1000, 500)

	first := ThumbnailForImage(src, dir)
	second := ThumbnailForImage(src, dir)

	if first != second {
		t.Fatalf("second call returned %q, want %q", second, first)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("cached thumbnail missing: %v", err)
	}
}

func TestThumbnailForImageEmptyImagePath(t *testing.T) {
	if got := ThumbnailForImage("", t.TempDir()); got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

func TestThumbnailForImageMissingSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "missing.png")

	if got := ThumbnailForImage(src, dir); got != src {
		t.Fatalf("got %q, want original path %q", got, src)
	}
}

func TestThumbnailForImageSmallImageUnchanged(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "small.png")
	writeTestPNG(t, src, 100, 100)

	if got := ThumbnailForImage(src, dir); got != src {
		t.Fatalf("got %q, want original path %q", got, src)
	}
	if _, err := os.Stat(filepath.Join(dir, "thumbnails")); err == nil {
		t.Fatalf("thumbnails directory should not be created for small images")
	}
}
