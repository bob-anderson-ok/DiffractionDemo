package report

import (
	"image"
	"image/color"
	"math"
)

// RotateGrayBilinear rotates an 8-bit grayscale image by angleRad counterclockwise.
// The destination is expanded so the entire rotated image fits.
// Pixels outside the source image are filled with bg.
func RotateGrayBilinear(src *image.Gray, angleRad float64, bg uint8) *image.Gray {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	dw, dh := rotatedBounds(sw, sh, angleRad)
	dst := image.NewGray(image.Rect(0, 0, dw, dh))

	srcCx := float64(sw-1) / 2.0
	srcCy := float64(sh-1) / 2.0
	dstCx := float64(dw-1) / 2.0
	dstCy := float64(dh-1) / 2.0

	sinA := math.Sin(angleRad)
	cosA := math.Cos(angleRad)

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			fx := float64(x) - dstCx
			fy := float64(y) - dstCy

			// Inverse map from destination to source
			sx := cosA*fx + sinA*fy + srcCx
			sy := -sinA*fx + cosA*fy + srcCy

			dst.Pix[y*dst.Stride+x] = sampleGrayBilinear(src, sx, sy, bg)
		}
	}

	return dst
}

// RotateTranslateGrayBilinear rotates src by angleRad CCW after first shifting
// its content by (txSrc, tySrc) pixels in the source (pre-rotation) frame.
// Pixels outside the source image are filled with bg.
func RotateTranslateGrayBilinear(src *image.Gray, angleRad, txSrc, tySrc float64, bg uint8) *image.Gray {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	dw, dh := rotatedBounds(sw, sh, angleRad)
	dst := image.NewGray(image.Rect(0, 0, dw, dh))

	srcCx := float64(sw-1) / 2.0
	srcCy := float64(sh-1) / 2.0
	dstCx := float64(dw-1) / 2.0
	dstCy := float64(dh-1) / 2.0

	sinA := math.Sin(angleRad)
	cosA := math.Cos(angleRad)

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			fx := float64(x) - dstCx
			fy := float64(y) - dstCy

			// Inverse rotation back to the translated-source frame, then
			// undo the source-frame translation to reach the original source.
			sx := cosA*fx + sinA*fy + srcCx - txSrc
			sy := -sinA*fx + cosA*fy + srcCy - tySrc

			dst.Pix[y*dst.Stride+x] = sampleGrayBilinear(src, sx, sy, bg)
		}
	}

	return dst
}

// RotateGray16Bilinear rotates a 16-bit grayscale image by angleRad counterclockwise.
// The destination is expanded so the entire rotated image fits.
// Pixels outside the source image are filled with bg.
func RotateGray16Bilinear(src *image.Gray16, angleRad float64, bg uint16) *image.Gray16 {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()

	dw, dh := rotatedBounds(sw, sh, angleRad)
	dst := image.NewGray16(image.Rect(0, 0, dw, dh))

	srcCx := float64(sw-1) / 2.0
	srcCy := float64(sh-1) / 2.0
	dstCx := float64(dw-1) / 2.0
	dstCy := float64(dh-1) / 2.0

	sinA := math.Sin(angleRad)
	cosA := math.Cos(angleRad)

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			fx := float64(x) - dstCx
			fy := float64(y) - dstCy

			sx := cosA*fx + sinA*fy + srcCx
			sy := -sinA*fx + cosA*fy + srcCy

			v := sampleGray16Bilinear(src, sx, sy, bg)
			i := y*dst.Stride + 2*x
			dst.Pix[i+0] = uint8(v >> 8)
			dst.Pix[i+1] = uint8(v & 0xFF)
		}
	}

	return dst
}

func sampleGrayBilinear(src *image.Gray, x, y float64, bg uint8) uint8 {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()

	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := x0 + 1
	y1 := y0 + 1

	fx := x - float64(x0)
	fy := y - float64(y0)

	p00 := grayAtOrBG(src, x0, y0, w, h, bg)
	p10 := grayAtOrBG(src, x1, y0, w, h, bg)
	p01 := grayAtOrBG(src, x0, y1, w, h, bg)
	p11 := grayAtOrBG(src, x1, y1, w, h, bg)

	v0 := (1-fx)*p00 + fx*p10
	v1 := (1-fx)*p01 + fx*p11
	v := (1-fy)*v0 + fy*v1

	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(math.Round(v))
}

func sampleGray16Bilinear(src *image.Gray16, x, y float64, bg uint16) uint16 {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()

	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := x0 + 1
	y1 := y0 + 1

	fx := x - float64(x0)
	fy := y - float64(y0)

	p00 := gray16AtOrBG(src, x0, y0, w, h, bg)
	p10 := gray16AtOrBG(src, x1, y0, w, h, bg)
	p01 := gray16AtOrBG(src, x0, y1, w, h, bg)
	p11 := gray16AtOrBG(src, x1, y1, w, h, bg)

	v0 := (1-fx)*p00 + fx*p10
	v1 := (1-fx)*p01 + fx*p11
	v := (1-fy)*v0 + fy*v1

	if v < 0 {
		v = 0
	}
	if v > 65535 {
		v = 65535
	}
	return uint16(math.Round(v))
}

func grayAtOrBG(src *image.Gray, x, y, w, h int, bg uint8) float64 {
	if x < 0 || x >= w || y < 0 || y >= h {
		return float64(bg)
	}
	return float64(src.Pix[y*src.Stride+x])
}

func gray16AtOrBG(src *image.Gray16, x, y, w, h int, bg uint16) float64 {
	if x < 0 || x >= w || y < 0 || y >= h {
		return float64(bg)
	}
	i := y*src.Stride + 2*x
	return float64(uint16(src.Pix[i])<<8 | uint16(src.Pix[i+1]))
}

// rotatedBounds returns the output width and height needed to fully contain
// the rotated source image.
func rotatedBounds(w, h int, angleRad float64) (int, int) {
	cx := float64(w-1) / 2.0
	cy := float64(h-1) / 2.0

	corners := [4][2]float64{
		{-cx, -cy},
		{float64(w-1) - cx, -cy},
		{-cx, float64(h-1) - cy},
		{float64(w-1) - cx, float64(h-1) - cy},
	}

	sinA := math.Sin(angleRad)
	cosA := math.Cos(angleRad)

	minX := math.Inf(1)
	maxX := math.Inf(-1)
	minY := math.Inf(1)
	maxY := math.Inf(-1)

	for _, c := range corners {
		x := c[0]
		y := c[1]

		rx := cosA*x - sinA*y
		ry := sinA*x + cosA*y

		if rx < minX {
			minX = rx
		}
		if rx > maxX {
			maxX = rx
		}
		if ry < minY {
			minY = ry
		}
		if ry > maxY {
			maxY = ry
		}
	}

	dw := int(math.Ceil(maxX-minX)) + 1
	dh := int(math.Ceil(maxY-minY)) + 1
	return dw, dh
}

// ToGray converts any image.Image to *image.Gray.
func ToGray(src image.Image) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x, y, color.GrayModel.Convert(src.At(x, y)))
		}
	}
	return dst
}

// ToGray16 converts any image.Image to *image.Gray16.
func ToGray16(src image.Image) *image.Gray16 {
	b := src.Bounds()
	dst := image.NewGray16(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x, y, color.Gray16Model.Convert(src.At(x, y)))
		}
	}
	return dst
}
