package tracker

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/chai2010/webp"
	"github.com/kbinani/screenshot"
	"golang.org/x/image/draw"
)

// ScreenshotResult holds the results of a screenshot capture.
type ScreenshotResult struct {
	FilePath   string `json:"file_path"`
	Base64Data string `json:"base64_data"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SizeBytes  int    `json:"size_bytes"`
}

const (
	maxWidth    = 1280
	maxHeight   = 720
	webpQuality = 60
)

// CaptureScreenshot takes a screenshot of all active displays, combines them into
// a single grid image, compresses it to WebP at 60% quality (max 720p), saves it
// to disk, and returns a base64 encoded copy for AI analysis.
func CaptureScreenshot(dataDir string) (*ScreenshotResult, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no active displays detected")
	}

	var displayImgs []image.Image
	for i := 0; i < n; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			log.Printf("[tracker] capture screen display %d: %v", i, err)
			continue
		}
		displayImgs = append(displayImgs, img)
	}

	if len(displayImgs) == 0 {
		return nil, fmt.Errorf("failed to capture any active display")
	}

	// Combine display images into a single grid image
	combined := stitchDisplayImages(displayImgs)

	// Resize to max 720p maintaining aspect ratio
	resized := resizeImage(combined, maxWidth, maxHeight)

	// Encode to WebP
	var buf bytes.Buffer
	if err := webp.Encode(&buf, resized, &webp.Options{Quality: webpQuality}); err != nil {
		return nil, fmt.Errorf("encode webp: %w", err)
	}

	webpData := buf.Bytes()

	// Determine save path: dataDir/images/YYYY-MM-DD/HH-MM-SS.webp
	now := time.Now()
	dateDir := filepath.Join(dataDir, "images", now.Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return nil, fmt.Errorf("create image dir: %w", err)
	}
	filename := now.Format("15-04-05") + ".webp"
	filePath := filepath.Join(dateDir, filename)

	if err := os.WriteFile(filePath, webpData, 0644); err != nil {
		return nil, fmt.Errorf("write screenshot: %w", err)
	}

	b := resized.Bounds()
	return &ScreenshotResult{
		FilePath:   filePath,
		Base64Data: base64.StdEncoding.EncodeToString(webpData),
		Width:      b.Dx(),
		Height:     b.Dy(),
		SizeBytes:  len(webpData),
	}, nil
}

// stitchDisplayImages combines multiple display images into a single grid image.
func stitchDisplayImages(imgs []image.Image) image.Image {
	if len(imgs) == 1 {
		return imgs[0]
	}

	cols := 2
	if len(imgs) < 2 {
		cols = len(imgs)
	}
	rows := (len(imgs) + cols - 1) / cols

	// Find max cell width and height among displays
	cellW := 0
	cellH := 0
	for _, img := range imgs {
		b := img.Bounds()
		if b.Dx() > cellW {
			cellW = b.Dx()
		}
		if b.Dy() > cellH {
			cellH = b.Dy()
		}
	}

	totalW := cellW * cols
	totalH := cellH * rows

	dst := image.NewRGBA(image.Rect(0, 0, totalW, totalH))

	for idx, img := range imgs {
		r := idx / cols
		c := idx % cols
		xOffset := c * cellW
		yOffset := r * cellH

		targetRect := image.Rect(xOffset, yOffset, xOffset+cellW, yOffset+cellH)
		draw.CatmullRom.Scale(dst, targetRect, img, img.Bounds(), draw.Over, nil)
	}

	return dst
}

// resizeImage resizes the image to fit within maxW×maxH, preserving aspect ratio.
func resizeImage(src image.Image, maxW, maxH int) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	if srcW <= maxW && srcH <= maxH {
		return src
	}

	// Calculate scale factor to fit within bounds
	scaleW := float64(maxW) / float64(srcW)
	scaleH := float64(maxH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, srcBounds, draw.Over, nil)
	return dst
}
