// Package report handles output formatting and PNG generation.
package report

import (
	"fmt"
	"image"
	"image/color"
	"math"
	_ "image/png"
	"os"
	"regexp"
	"strconv"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	vgdraw "gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// intensityScale is the divisor applied to raw 16-bit pixel values when
// extracting light curve data.
const intensityScale = 4000

// ExtractRow loads a 16-bit PNG and returns the scaled intensity values from
// the row at the vertical center plus offsetFromCenter.
func ExtractRow(imagePath string, offsetFromCenter int) ([]float64, error) {
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
	values := make([]float64, bounds.Dx())
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		r, _, _, _ := src.At(x, row).RGBA()
		values[x-bounds.Min.X] = float64(r) / intensityScale
	}
	return values, nil
}

// ParsePixelScale extracts fundamental_plane_width_km and
// fundamental_plane_width_num_points from a JSON5 parameters string
// and returns the km-per-pixel scale factor.
func ParsePixelScale(json5Text string) (float64, error) {
	widthKm, err := extractFloat(json5Text, "fundamental_plane_width_km")
	if err != nil {
		return 0, err
	}
	numPoints, err := extractFloat(json5Text, "fundamental_plane_width_num_points")
	if err != nil {
		return 0, err
	}
	if numPoints == 0 {
		return 0, fmt.Errorf("fundamental_plane_width_num_points is zero")
	}
	return widthKm / numPoints, nil
}

// ParseShadowSpeed extracts dX_km_per_sec and dY_km_per_sec from a JSON5
// parameters string and returns the shadow speed in km/s as
// sqrt(dX^2 + dY^2).
func ParseShadowSpeed(json5Text string) (float64, error) {
	dx, err := extractFloat(json5Text, "dX_km_per_sec")
	if err != nil {
		return 0, err
	}
	dy, err := extractFloat(json5Text, "dY_km_per_sec")
	if err != nil {
		return 0, err
	}
	return math.Sqrt(dx*dx + dy*dy), nil
}

// extractFloat finds a key in JSON5 text and returns its numeric value.
func extractFloat(text, key string) (float64, error) {
	re := regexp.MustCompile(key + `\s*:\s*([0-9.eE+-]+)`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, fmt.Errorf("%s not found in parameters", key)
	}
	return strconv.ParseFloat(m[1], 64)
}

// PlotLightCurve renders a line plot of the given intensity values using
// gonum/plot with grid, axis labels, and tick marks. If edges is non-empty,
// full-height red vertical lines are drawn at those data-index positions.
// Returns the plot as an RGBA image.
func PlotLightCurve(values []float64, width, height int, edges []int, yMax, kmPerPixel float64) *image.RGBA {
	p := plot.New()
	p.Title.Text = "Light Curve"
	if kmPerPixel > 0 {
		p.X.Label.Text = "Distance (km)"
	} else {
		p.X.Label.Text = "Pixel"
		kmPerPixel = 1
	}
	p.Y.Label.Text = "Intensity"
	p.Y.Min = -0.1
	p.Y.Max = yMax
	p.Add(plotter.NewGrid())

	if len(values) >= 2 {
		pts := make(plotter.XYs, len(values))
		for i, v := range values {
			pts[i].X = float64(i) * kmPerPixel
			pts[i].Y = v
		}
		line, _ := plotter.NewLine(pts)
		line.Color = color.RGBA{B: 255, A: 255}
		p.Add(line)

		// Draw edge markers as red vertical lines from 0 to Y max.
		for _, ei := range edges {
			if ei < 0 || ei >= len(values) {
				continue
			}
			edgeLine, _ := plotter.NewLine(plotter.XYs{
				{X: float64(ei) * kmPerPixel, Y: 0},
				{X: float64(ei) * kmPerPixel, Y: p.Y.Max},
			})
			edgeLine.Color = color.RGBA{R: 255, A: 255}
			edgeLine.Width = vg.Points(1.5)
			p.Add(edgeLine)
		}
	}

	// Render to an in-memory image.
	c := vgimg.New(vg.Length(width), vg.Length(height))
	p.Draw(vgdraw.New(c))

	src := c.Image()
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.Set(x, y, src.At(x, y))
		}
	}
	return dst
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

