package tracker

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"time"

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
	jpegQuality = 60
)

// CaptureScreenshot takes a screenshot of the primary display, compresses it
// to JPEG at 60% quality (max 720p), saves it to disk, and returns a base64
// encoded copy for AI analysis.
func CaptureScreenshot(dataDir string) (*ScreenshotResult, error) {
	// Capture primary display (display index 0)
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no active displays detected")
	}

	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("capture screen: %w", err)
	}

	// Resize to max 720p maintaining aspect ratio
	resized := resizeImage(img, maxWidth, maxHeight)

	// Encode to JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}

	jpegData := buf.Bytes()

	// Determine save path: dataDir/images/YYYY-MM-DD/HH-MM-SS.jpg
	now := time.Now()
	dateDir := filepath.Join(dataDir, "images", now.Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return nil, fmt.Errorf("create image dir: %w", err)
	}
	filename := now.Format("15-04-05") + ".jpg"
	filePath := filepath.Join(dateDir, filename)

	if err := os.WriteFile(filePath, jpegData, 0644); err != nil {
		return nil, fmt.Errorf("write screenshot: %w", err)
	}

	b := resized.Bounds()
	return &ScreenshotResult{
		FilePath:   filePath,
		Base64Data: base64.StdEncoding.EncodeToString(jpegData),
		Width:      b.Dx(),
		Height:     b.Dy(),
		SizeBytes:  len(jpegData),
	}, nil
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
