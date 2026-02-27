package sitegen

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"log"
	"os"

	"github.com/chai2010/webp"
	xdraw "golang.org/x/image/draw"
)

func resizeImage(img image.Image, newWidth int) image.Image {
	bounds := img.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()
	newHeight := srcHeight * newWidth / srcWidth
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, xdraw.Over, nil)
	return dst
}

func (sg *SiteGen) processImage(src []byte, pubPath string, ext string) error {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return err
	}

	// Calculate if we need to resize
	bounds := img.Bounds()
	width := bounds.Dx()

	optimized := false

	// Resize if minify is turned on and width > 1920
	if sg.Minify != nil && width > 1920 {
		img = resizeImage(img, 1920)
		optimized = true
	}

	// Always write the original (or resized) image
	if optimized {
		out, err := os.Create(pubPath)
		if err != nil {
			return err
		}

		if ext == ".png" {
			err = png.Encode(out, img)
		} else {
			// Save using JPEG backend
			err = jpeg.Encode(out, img, &jpeg.Options{Quality: 85})
		}
		out.Close()
		if err != nil {
			return err
		}
	} else {
		// Just write original bytes if not resized to save quality & time
		if err := os.WriteFile(pubPath, src, os.ModePerm); err != nil {
			return err
		}
	}

	// Generate WebP if requested
	if sg.Webp {
		webpPath := pubPath[:len(pubPath)-len(ext)] + ".webp"
		outWebp, err := os.Create(webpPath)
		if err != nil {
			return err
		}
		defer outWebp.Close()

		err = webp.Encode(outWebp, img, &webp.Options{Lossless: false, Quality: 85})
		if err != nil {
			log.Println("failed to encode webp", webpPath, err)
			return err
		}
	}

	return nil
}
