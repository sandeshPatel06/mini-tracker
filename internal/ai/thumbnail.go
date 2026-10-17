package ai

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/chai2010/webp"
	"golang.org/x/image/draw"
)

const thumbnailMaxDim = 512

// ThumbnailForImage returns a path to a cached, downscaled WebP thumbnail (max 512px on the longest side, quality 40)
// for the given source image, stored under cacheDir/thumbnails/. Returns the original imagePath when
// thumbnailing is unnecessary or fails. Never returns an error.
func ThumbnailForImage(imagePath, cacheDir string) string {
	if imagePath == "" || cacheDir == "" {
		return imagePath
	}

	info, err := os.Stat(imagePath)
	if err != nil || info.IsDir() {
		return imagePath
	}

	absPath, err := filepath.Abs(imagePath)
	if err != nil {
		return imagePath
	}

	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", absPath, info.Size(), info.ModTime().UnixNano())))
	thumbDir := filepath.Join(cacheDir, "thumbnails")
	cachePath := filepath.Join(thumbDir, hex.EncodeToString(sum[:])+".webp")

	if _, err := os.Stat(cachePath); err == nil {
		return cachePath
	}

	img, err := decodeThumbnail(imagePath)
	if err != nil {
		return imagePath
	}

	bounds := img.Bounds()
	if bounds.Dx() <= thumbnailMaxDim && bounds.Dy() <= thumbnailMaxDim {
		return imagePath
	}

	resized := resizeThumbnail(img, thumbnailMaxDim)

	var buf bytes.Buffer
	if err := webp.Encode(&buf, resized, &webp.Options{Quality: 40}); err != nil {
		return imagePath
	}

	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return imagePath
	}
	if err := os.WriteFile(cachePath, buf.Bytes(), 0644); err != nil {
		return imagePath
	}

	return cachePath
}

func decodeThumbnail(imagePath string) (image.Image, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if strings.EqualFold(filepath.Ext(imagePath), ".webp") {
		return webp.Decode(f)
	}

	img, _, err := image.Decode(f)
	return img, err
}

func resizeThumbnail(src image.Image, maxDim int) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	scale := float64(maxDim) / float64(srcW)
	if s := float64(maxDim) / float64(srcH); s < scale {
		scale = s
	}

	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, srcBounds, draw.Over, nil)
	return dst
}
