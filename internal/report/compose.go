package report

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	captionPadding = 10
	separatorH     = 10
)

// ComposeResultImage combines the plot and diffraction images side by side,
// scaling the plot to match the diffraction image height. If caption is
// non-empty, it is drawn as text below the images at the given fontSize.
// Returns the composed image dimensions.
func ComposeResultImage(plotPath, diffPath, outputPath, caption string, fontSize float64, leftJustify bool) (int, int, error) {
	plotImg, err := loadPNG(plotPath)
	if err != nil {
		return 0, 0, fmt.Errorf("loading plot: %w", err)
	}
	diffImg, err := loadPNG(diffPath)
	if err != nil {
		return 0, 0, fmt.Errorf("loading diffraction image: %w", err)
	}

	diffBounds := diffImg.Bounds()
	plotBounds := plotImg.Bounds()
	targetH := diffBounds.Dy()

	// Scale plot to match diffraction image height.
	scale := float64(targetH) / float64(plotBounds.Dy())
	scaledPlotW := int(float64(plotBounds.Dx()) * scale)
	scaledPlot := image.NewRGBA(image.Rect(0, 0, scaledPlotW, targetH))
	xdraw.CatmullRom.Scale(scaledPlot, scaledPlot.Bounds(), plotImg, plotBounds, draw.Over, nil)

	// Determine caption area height.
	captionH := 0
	lineH := int(fontSize * 1.5)
	var lines []string
	var face font.Face

	trimmed := strings.TrimSpace(caption)
	if trimmed != "" {
		lines = strings.Split(trimmed, "\n")
		captionH = separatorH + captionPadding*2 + len(lines)*lineH

		ttFont, parseErr := opentype.Parse(goregular.TTF)
		if parseErr != nil {
			return 0, 0, fmt.Errorf("parsing font: %w", parseErr)
		}
		face, err = opentype.NewFace(ttFont, &opentype.FaceOptions{
			Size: fontSize,
			DPI:  72,
		})
		if err != nil {
			return 0, 0, fmt.Errorf("creating font face: %w", err)
		}
	}

	// Create the output canvas.
	totalW := scaledPlotW + diffBounds.Dx()
	totalH := targetH + captionH
	output := image.NewRGBA(image.Rect(0, 0, totalW, totalH))

	// White background.
	draw.Draw(output, output.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	// Draw scaled plot on the left, diffraction image on the right.
	draw.Draw(output, image.Rect(0, 0, scaledPlotW, targetH), scaledPlot, image.Point{}, draw.Over)
	draw.Draw(output, image.Rect(scaledPlotW, 0, totalW, targetH), diffImg, diffBounds.Min, draw.Over)

	// Draw brown separator line and centered caption lines.
	if face != nil {
		brown := color.RGBA{R: 139, G: 69, B: 19, A: 255}
		draw.Draw(output, image.Rect(0, targetH, totalW, targetH+separatorH), image.NewUniform(brown), image.Point{}, draw.Src)

		d := &font.Drawer{
			Dst:  output,
			Src:  image.NewUniform(color.Black),
			Face: face,
		}
		textTop := targetH + separatorH + captionPadding + int(fontSize)
		for i, line := range lines {
			var x fixed.Int26_6
			if leftJustify {
				x = fixed.I(captionPadding)
			} else {
				lineWidth := d.MeasureString(line)
				x = (fixed.I(totalW) - lineWidth) / 2
			}
			d.Dot = fixed.Point26_6{X: x, Y: fixed.I(textTop + i*lineH)}
			d.DrawString(line)
		}
	}

	// Write the output PNG.
	outFile, err := os.Create(outputPath)
	if err != nil {
		return 0, 0, fmt.Errorf("creating output file: %w", err)
	}
	if err := png.Encode(outFile, output); err != nil {
		outFile.Close()
		return 0, 0, fmt.Errorf("encoding PNG: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return 0, 0, fmt.Errorf("closing output file: %w", err)
	}
	return totalW, totalH, nil
}

// loadPNG opens and decodes a PNG file.
func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	img, _, decErr := image.Decode(f)
	if closeErr := f.Close(); closeErr != nil {
		return nil, closeErr
	}
	if decErr != nil {
		return nil, decErr
	}
	return img, nil
}
