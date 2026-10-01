package platform

import (
	"image"
	"image/draw"
)

// Reuse the canonical tray/executable mark, including its antialiasing. The
// opaque 512px background meets maskable-icon requirements without a new logo.
func PWAIcon(size int) []byte {
	if size != 512 {
		return BrandIconPNG(size)
	}
	im := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(im, im.Bounds(), &image.Uniform{C: brandBlack}, image.Point{}, draw.Src)
	draw.Draw(im, im.Bounds(), brandIconImage(size, brandBlack, brandWhite), image.Point{}, draw.Over)
	return encodePNG(im)
}
