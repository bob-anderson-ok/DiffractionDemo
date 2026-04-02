// Package report handles output formatting and PNG generation.
package report

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"math"
	"os"
)

const plotMargin = 40

// ExtractRow loads a 16-bit PNG and returns the intensity values from the
// row at the vertical center plus offsetFromCenter.
func ExtractRow(imagePath string, offsetFromCenter int) ([]uint16, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, err
	}
	src, _, decErr := image.Decode(f)
	if closeErr := f.Close(); closeErr != nil {
		return nil, closeErr
	}
	if decErr != nil {
		return nil, decErr
	}
	bounds := src.Bounds()
	row := bounds.Min.Y + bounds.Dy()/2 + offsetFromCenter
	if row < bounds.Min.Y || row >= bounds.Max.Y {
		return nil, fmt.Errorf("row %d outside image bounds [%d, %d)", row, bounds.Min.Y, bounds.Max.Y)
	}
	values := make([]uint16, bounds.Dx())
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		r, _, _, _ := src.At(x, row).RGBA()
		values[x-bounds.Min.X] = uint16(r)
	}
	return values, nil
}

// PlotLightCurve renders a line plot of the given intensity values and
// returns the resulting image. The plot has a white background with a
// blue data line and simple axes.
func PlotLightCurve(values []uint16, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	if len(values) < 2 {
		return img
	}

	// Determine Y-axis range.
	minVal, maxVal := values[0], values[0]
	for _, v := range values[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	// Add 5% padding so the line doesn't touch the edges.
	valRange := float64(maxVal - minVal)
	if valRange == 0 {
		valRange = 1
	}
	padded := valRange * 0.05
	yMin := float64(minVal) - padded
	yMax := float64(maxVal) + padded

	plotW := width - 2*plotMargin
	plotH := height - 2*plotMargin

	toX := func(i int) int {
		return plotMargin + i*plotW/(len(values)-1)
	}
	toY := func(v uint16) int {
		frac := (float64(v) - yMin) / (yMax - yMin)
		return plotMargin + plotH - int(math.Round(frac*float64(plotH)))
	}

	// Draw axes.
	black := color.RGBA{A: 255}
	for x := plotMargin; x <= plotMargin+plotW; x++ {
		img.Set(x, plotMargin+plotH, black)
	}
	for y := plotMargin; y <= plotMargin+plotH; y++ {
		img.Set(plotMargin, y, black)
	}

	// Draw data line.
	blue := color.RGBA{B: 255, A: 255}
	prevX, prevY := toX(0), toY(values[0])
	for i := 1; i < len(values); i++ {
		cx, cy := toX(i), toY(values[i])
		bresenham(img, prevX, prevY, cx, cy, blue)
		prevX, prevY = cx, cy
	}

	return img
}

// FindEdges traverses the observation-path row of geometricShadow.png and
// returns the x positions where transitions occur. The first edge is the
// first white-to-black transition; after that every transition (black-to-white
// or white-to-black) is recorded.
func FindEdges(imagePath string, offsetFromCenter int) ([]int, error) {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil, err
	}
	src, _, decErr := image.Decode(f)
	if closeErr := f.Close(); closeErr != nil {
		return nil, closeErr
	}
	if decErr != nil {
		return nil, decErr
	}

	bounds := src.Bounds()
	row := bounds.Min.Y + bounds.Dy()/2 + offsetFromCenter
	if row < bounds.Min.Y || row >= bounds.Max.Y {
		return nil, fmt.Errorf("row %d outside image bounds [%d, %d)", row, bounds.Min.Y, bounds.Max.Y)
	}

	isWhite := func(x int) bool {
		r, _, _, _ := src.At(x, row).RGBA()
		return r > 0x7FFF
	}

	var edges []int
	foundFirst := false

	prev := isWhite(bounds.Min.X)
	for x := bounds.Min.X + 1; x < bounds.Max.X; x++ {
		cur := isWhite(x)
		if !foundFirst {
			// Looking for the first white-to-black transition.
			if prev && !cur {
				edges = append(edges, x)
				foundFirst = true
			}
		} else if cur != prev {
			edges = append(edges, x)
		}
		prev = cur
	}
	return edges, nil
}

// bresenham draws a line between two points using Bresenham's algorithm.
func bresenham(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	dy = -dy

	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		img.Set(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}
