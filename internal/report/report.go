// Package report handles output formatting and PNG generation.
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

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

// ParseShadowVelocity extracts dX_km_per_sec and dY_km_per_sec from a JSON5
// parameters string and returns both components.
func ParseShadowVelocity(json5Text string) (dx, dy float64, err error) {
	dx, err = extractFloat(json5Text, "dX_km_per_sec")
	if err != nil {
		return 0, 0, err
	}
	dy, err = extractFloat(json5Text, "dY_km_per_sec")
	if err != nil {
		return 0, 0, err
	}
	return dx, dy, nil
}

// ParseShadowSpeed extracts dX_km_per_sec and dY_km_per_sec from a JSON5
// parameters string and returns the shadow speed in km/s as
// sqrt(dX^2 + dY^2).
func ParseShadowSpeed(json5Text string) (float64, error) {
	dx, dy, err := ParseShadowVelocity(json5Text)
	if err != nil {
		return 0, err
	}
	return math.Sqrt(dx*dx + dy*dy), nil
}

// ProjectedStarDiametersKm returns the projected diameter in km of star 1 and
// star 2 at the fundamental plane distance, as derived from the parameters
// text. A returned value of 0 means the star is absent or a point source
// (diameter parameter missing or zero). If any star has a non-zero diameter
// but no distance_au or parallax_arcsec is available, an error is returned.
func ProjectedStarDiametersKm(json5Text string) (d1Km, d2Km float64, err error) {
	mas1, mas1Err := extractFloat(json5Text, "star_diam_on_plane_mas")
	mas2, mas2Err := extractFloat(json5Text, "star2_diam_on_plane_mas")
	haveStar1 := mas1Err == nil && mas1 > 0
	haveStar2 := mas2Err == nil && mas2 > 0
	if !haveStar1 && !haveStar2 {
		return 0, 0, nil
	}
	distanceKm, err := parseDistanceKm(json5Text)
	if err != nil {
		return 0, 0, err
	}
	// 1 mas = (π / 648_000_000) rad; projected size = distance × angle.
	const masToRad = math.Pi / 648_000_000.0
	if haveStar1 {
		d1Km = distanceKm * mas1 * masToRad
	}
	if haveStar2 {
		d2Km = distanceKm * mas2 * masToRad
	}
	return d1Km, d2Km, nil
}

// StarSeparation returns the projected separation (in km) and position
// angle (in degrees CCW from North) of the two stars. ok is false when
// star_separation_mas is absent.
func StarSeparation(json5Text string) (sepKm, angleDeg float64, ok bool, err error) {
	mas, masErr := extractFloat(json5Text, "star_separation_mas")
	if masErr != nil {
		return 0, 0, false, nil
	}
	distanceKm, err := parseDistanceKm(json5Text)
	if err != nil {
		return 0, 0, false, err
	}
	const masToRad = math.Pi / 648_000_000.0
	sepKm = distanceKm * mas * masToRad
	angleDeg, _ = extractFloat(json5Text, "star_angle_degrees_ccw")
	return sepKm, angleDeg, true, nil
}

// parseDistanceKm returns the asteroid distance in km from the parameters
// text, using parallax_arcsec if present, otherwise distance_au. The
// parallax form is the equatorial horizontal parallax of the asteroid.
func parseDistanceKm(json5Text string) (float64, error) {
	const auKm = 149_597_870.7
	const earthRadiusKm = 6378.14
	const arcsecPerRadian = 648_000.0 / math.Pi
	if parallax, err := extractFloat(json5Text, "parallax_arcsec"); err == nil && parallax > 0 {
		return earthRadiusKm * arcsecPerRadian / parallax, nil
	}
	if au, err := extractFloat(json5Text, "distance_au"); err == nil && au > 0 {
		return au * auKm, nil
	}
	return 0, fmt.Errorf("no distance_au or parallax_arcsec in parameters")
}

// PathAngleFromVelocity computes the observation path angle in degrees
// measured counter-clockwise from the positive Y-axis, given the shadow
// velocity components dxKmPerSec and dyKmPerSec. The result is in [0, 359.99....].
func PathAngleFromVelocity(dxKmPerSec, dyKmPerSec float64) float64 {
	angle := math.Atan2(-dxKmPerSec, -dyKmPerSec) * 180.0 / math.Pi
	if angle < 0.0 {
		angle += 360.0
	}
	return angle
}

// ValidateParams converts JSON5 parameters text to standard JSON and
// validates it with encoding/json. Returns nil if the parameters are valid,
// or an error describing the problem with the source line shown.
func ValidateParams(json5Text string) error {
	jsonText := json5ToJSON(json5Text)
	var v interface{}
	if err := json.Unmarshal([]byte(jsonText), &v); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			lineNum := offsetToLine(jsonText, int(syntaxErr.Offset))
			origLines := strings.Split(json5Text, "\n")
			if lineNum >= 1 && lineNum <= len(origLines) {
				return fmt.Errorf("line %d: %s\n   %s", lineNum, syntaxErr.Error(), strings.TrimSpace(origLines[lineNum-1]))
			}
		}
		return fmt.Errorf("JSON parse error: %w", err)
	}
	return nil
}

// offsetToLine returns the 1-based line number at the given byte offset.
// json.SyntaxError.Offset is the count of bytes read (one past the error
// character), so we subtract one to land on the offending byte itself.
func offsetToLine(text string, offset int) int {
	if offset > 0 {
		offset--
	}
	line := 1
	for i := 0; i < offset && i < len(text); i++ {
		if text[i] == '\n' {
			line++
		}
	}
	return line
}

// json5ToJSON converts JSON5 text to standard JSON by stripping comments,
// quoting bare keys, removing trailing commas, and stripping + prefixes
// on numbers.
func json5ToJSON(json5Text string) string {
	src := stripJSON5Comments(json5Text)
	var out strings.Builder
	out.Grow(len(src))

	i := 0
	for i < len(src) {
		ch := src[i]

		// Pass through string literals unchanged (normalize to double quotes).
		if ch == '"' || ch == '\'' {
			quote := ch
			out.WriteByte('"')
			i++
			for i < len(src) && src[i] != quote {
				if src[i] == '\\' {
					out.WriteByte(src[i])
					i++
					if i < len(src) {
						out.WriteByte(src[i])
						i++
					}
					continue
				}
				out.WriteByte(src[i])
				i++
			}
			out.WriteByte('"')
			if i < len(src) {
				i++ // skip closing quote
			}
			continue
		}

		// Bare identifier — quote it if followed by ':' (a JSON5 key).
		if isIdentStart(ch) {
			start := i
			for i < len(src) && isIdentChar(src[i]) {
				i++
			}
			ident := src[start:i]
			// Look ahead past whitespace for ':'.
			j := i
			for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			if j < len(src) && src[j] == ':' {
				out.WriteByte('"')
				out.WriteString(ident)
				out.WriteByte('"')
			} else {
				// Bare value (true/false/null) — write as-is.
				out.WriteString(ident)
			}
			continue
		}

		// Strip leading '+' on numeric values.
		if ch == '+' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9' {
			i++
			continue
		}

		// Remove trailing commas before '}' or ']'.
		if ch == ',' {
			j := i + 1
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
				j++
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				i++
				continue
			}
		}

		out.WriteByte(ch)
		i++
	}
	return out.String()
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '$'
}

func isIdentChar(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

// stripJSON5Comments removes // line comments from JSON5 text, respecting
// string literals so that "//" inside a quoted value is preserved.
func stripJSON5Comments(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		inStr := false
		var strCh byte
		for j := 0; j < len(line); j++ {
			ch := line[j]
			if inStr {
				if ch == '\\' {
					j++
					continue
				}
				if ch == strCh {
					inStr = false
				}
				continue
			}
			if ch == '"' || ch == '\'' {
				inStr = true
				strCh = ch
				continue
			}
			if ch == '/' && j+1 < len(line) && line[j+1] == '/' {
				lines[i] = line[:j]
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

// extractFloat finds a key in JSON5 text and returns its numeric value.
// Comments are stripped first so that commented-out lines are not matched.
func extractFloat(text, key string) (float64, error) {
	re := regexp.MustCompile(key + `\s*:\s*([0-9.eE+-]+)`)
	m := re.FindStringSubmatch(stripJSON5Comments(text))
	if m == nil {
		return 0, fmt.Errorf("%s not found in parameters", key)
	}
	return strconv.ParseFloat(m[1], 64)
}

// ApplyExposure returns a new slice where each value is the average of a
// sliding window of the given width centered on the original data. When the
// window extends past the right edge of the data, missing values are
// substituted with 1.0 so the output length matches the input length.
func ApplyExposure(values []float64, windowSize int) []float64 {
	n := len(values)
	if windowSize <= 1 || n == 0 {
		return values
	}
	result := make([]float64, n)
	for i := range result {
		sum := 0.0
		for j := 0; j < windowSize; j++ {
			idx := i + j
			if idx < n {
				sum += values[idx]
			} else {
				sum += 1.0
			}
		}
		result[i] = sum / float64(windowSize)
	}
	return result
}

// CurveData holds all data needed to plot a single observation-path light curve.
type CurveData struct {
	Values          []float64  // raw intensity values
	Integrated      []float64  // exposure-integrated values (maybe nil)
	Edges           []int      // geometric shadow edge positions
	CurveColor      color.RGBA // color for the raw curve
	IntegratedColor color.RGBA // color for the integrated curve
}

// PlotLightCurves renders one or more light curves overlaid on a single plot.
// Each CurveData entry produces a raw line and optional integrated line.
// Edge markers are drawn in red for each curve.
func PlotLightCurves(curves []CurveData, width, height int, yMax, kmPerPixel float64) *image.RGBA {
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
	p.Y.Tick.Marker = fixedIntervalTicker{Interval: 0.1, Min: p.Y.Min, Max: p.Y.Max}
	p.Add(plotter.NewGrid())

	for _, cd := range curves {
		if len(cd.Values) >= 2 {
			pts := make(plotter.XYs, len(cd.Values))
			for i, v := range cd.Values {
				pts[i].X = float64(i) * kmPerPixel
				pts[i].Y = v
			}
			line, _ := plotter.NewLine(pts)
			line.Color = cd.CurveColor
			p.Add(line)
		}

		if len(cd.Integrated) >= 2 {
			iPts := make(plotter.XYs, len(cd.Integrated))
			for i, v := range cd.Integrated {
				iPts[i].X = float64(i) * kmPerPixel
				iPts[i].Y = v
			}
			iLine, _ := plotter.NewLine(iPts)
			iLine.Color = cd.IntegratedColor
			iLine.Width = vg.Points(1.5)
			p.Add(iLine)
		}

		// Draw edge markers using whichever data length is available.
		dataLen := len(cd.Values)
		if len(cd.Integrated) > dataLen {
			dataLen = len(cd.Integrated)
		}
		for _, ei := range cd.Edges {
			if ei < 0 || ei >= dataLen {
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

// PlotLightCurve renders a line plot of the given intensity values using
// gonum/plot with grid, axis labels, and tick marks. If edges is non-empty,
// full-height red vertical lines are drawn at those data-index positions.
// Returns the plot as an RGBA image.
func PlotLightCurve(values []float64, width, height int, edges []int, yMax, kmPerPixel float64, integrated []float64) *image.RGBA {
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
	p.Y.Tick.Marker = fixedIntervalTicker{Interval: 0.1, Min: p.Y.Min, Max: p.Y.Max}
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

	// Overlay exposure-integrated light curve in green.
	if len(integrated) >= 2 {
		iPts := make(plotter.XYs, len(integrated))
		for i, v := range integrated {
			iPts[i].X = float64(i) * kmPerPixel
			iPts[i].Y = v
		}
		iLine, _ := plotter.NewLine(iPts)
		iLine.Color = color.RGBA{G: 180, A: 255}
		iLine.Width = vg.Points(1.5)
		p.Add(iLine)
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

// fixedIntervalTicker generates tick marks at a fixed interval.
type fixedIntervalTicker struct {
	Interval float64
	Min, Max float64
}

// Ticks returns tick marks from Min to Max at the configured Interval.
func (t fixedIntervalTicker) Ticks(min, max float64) []plot.Tick {
	var ticks []plot.Tick
	// Start at the first interval multiple at or above min.
	start := math.Ceil(min/t.Interval) * t.Interval
	for v := start; v <= max; v += t.Interval {
		ticks = append(ticks, plot.Tick{
			Value: v,
			Label: fmt.Sprintf("%.1f", v),
		})
	}
	return ticks
}
