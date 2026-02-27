package sitegen

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/tdewolff/minify/v2"
)

func TestResizeImage(t *testing.T) {
	// Create a 3000x2000 test image
	src := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 3000; x++ {
			src.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	t.Run("resize_to_1920", func(t *testing.T) {
		result := resizeImage(src, 1920)
		bounds := result.Bounds()
		if bounds.Dx() != 1920 {
			t.Errorf("width = %d, want 1920", bounds.Dx())
		}
		// Aspect ratio: 2000/3000 * 1920 = 1280
		if bounds.Dy() != 1280 {
			t.Errorf("height = %d, want 1280 (aspect ratio preserved)", bounds.Dy())
		}
	})

	t.Run("resize_to_800", func(t *testing.T) {
		result := resizeImage(src, 800)
		bounds := result.Bounds()
		if bounds.Dx() != 800 {
			t.Errorf("width = %d, want 800", bounds.Dx())
		}
		// 2000/3000 * 800 = 533
		if bounds.Dy() != 533 {
			t.Errorf("height = %d, want 533", bounds.Dy())
		}
	})
}

func createTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	f, err := os.CreateTemp("", "test*.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(f.Name())
	return data
}

func createTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	f, err := os.CreateTemp("", "test*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(f.Name())
	return data
}

func TestProcessImage_NoResize(t *testing.T) {
	tmpDir := t.TempDir()
	pubPath := filepath.Join(tmpDir, "photo.jpg")

	sg := &SiteGen{Minify: nil, Webp: false}
	src := createTestJPEG(t, 800, 600)

	if err := sg.processImage(src, pubPath, ".jpg"); err != nil {
		t.Fatal(err)
	}

	// Should write original bytes
	written, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != len(src) {
		t.Errorf("expected original bytes (%d), got %d bytes", len(src), len(written))
	}
}

func TestProcessImage_Resize(t *testing.T) {
	tmpDir := t.TempDir()
	pubPath := filepath.Join(tmpDir, "photo.jpg")

	sg := &SiteGen{Minify: &minify.M{}, Webp: false}
	src := createTestJPEG(t, 2500, 1500)

	if err := sg.processImage(src, pubPath, ".jpg"); err != nil {
		t.Fatal(err)
	}

	// Should have written a resized file
	info, err := os.Stat(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestProcessImage_ResizePNG(t *testing.T) {
	tmpDir := t.TempDir()
	pubPath := filepath.Join(tmpDir, "icon.png")

	sg := &SiteGen{Minify: &minify.M{}, Webp: false}
	src := createTestPNG(t, 2500, 1500)

	if err := sg.processImage(src, pubPath, ".png"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("output PNG file is empty")
	}
}

func TestProcessImage_WebpGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	pubPath := filepath.Join(tmpDir, "photo.jpg")

	sg := &SiteGen{Minify: nil, Webp: true}
	src := createTestJPEG(t, 800, 600)

	if err := sg.processImage(src, pubPath, ".jpg"); err != nil {
		t.Fatal(err)
	}

	// Check both files exist
	if _, err := os.Stat(pubPath); err != nil {
		t.Errorf("original file missing: %v", err)
	}
	webpPath := filepath.Join(tmpDir, "photo.webp")
	info, err := os.Stat(webpPath)
	if err != nil {
		t.Errorf("webp file missing: %v", err)
	} else if info.Size() == 0 {
		t.Error("webp file is empty")
	}
}

func TestProcessImage_ResizeAndWebp(t *testing.T) {
	tmpDir := t.TempDir()
	pubPath := filepath.Join(tmpDir, "big.jpg")

	sg := &SiteGen{Minify: &minify.M{}, Webp: true}
	src := createTestJPEG(t, 3000, 2000)

	if err := sg.processImage(src, pubPath, ".jpg"); err != nil {
		t.Fatal(err)
	}

	// Both files should exist
	if _, err := os.Stat(pubPath); err != nil {
		t.Errorf("resized file missing: %v", err)
	}
	webpPath := filepath.Join(tmpDir, "big.webp")
	if _, err := os.Stat(webpPath); err != nil {
		t.Errorf("webp file missing: %v", err)
	}
}
