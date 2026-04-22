// Package ui constructs and manages the DiffractionDemo Fyne application window.
package ui

import (
	"DiffractionDemo/internal/report"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Version is set by main before calling Run.
var Version string

const (
	appID = "com.iota.diffractiondemo"

	prefWindowWidth  = "window_width"
	prefWindowHeight = "window_height"
	prefWindowX      = "window_x"
	prefWindowY      = "window_y"
	prefWindowPosSet = "window_pos_set"
	prefHSplitOffset = "hsplit_offset"
	prefVSplitOffset = "vsplit_offset"

	defaultWidth  float64 = 1200
	defaultHeight float64 = 800
	defaultYMax   float64 = 1.5
	defaultHSplit float64 = 0.4
	defaultVSplit float64 = 0.55
)

// focusLostEntry extends widget.Entry to call a callback when focus is lost.
type focusLostEntry struct {
	widget.Entry
	OnFocusLost func()
}

// newFocusLostEntry creates a focusLostEntry and registers it as an extended widget.
func newFocusLostEntry() *focusLostEntry {
	e := &focusLostEntry{}
	e.ExtendBaseWidget(e)
	return e
}

// FocusLost is called by Fyne when the entry loses keyboard focus.
func (e *focusLostEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.OnFocusLost != nil {
		e.OnFocusLost()
	}
}

// Run creates the application window and enters the Fyne event loop.
func Run() {
	a := app.NewWithID(appID)
	w := a.NewWindow("DiffractionDemo " + Version)

	paramsEntry, paramsScroll := buildParamsPanel(a, "Open a parameters file to view its contents...")
	imagePanel := buildImagePanel("No image loaded — run diffraction to generate one.")
	lightCurvePanel := buildImagePanel("No light curve — run diffraction to generate one.")

	var sourceDir string
	var paramsFilePath string
	var diffImagePath string
	var paramsDirty bool
	var yMaxEntry *focusLostEntry
	var exposureEntry *focusLostEntry
	var exposure2Entry *focusLostEntry
	var pathOffset2Entry *focusLostEntry
	var diffCmd *exec.Cmd
	var view1Check, view2Check *widget.Check
	var edges1Check, edges2Check *widget.Check
	var darkModeCheck *widget.Check
	var geomEdgesCheck *widget.Check
	var thicknessEntry *focusLostEntry

	starDiam1Label := widget.NewLabel("")
	starDiam2Label := widget.NewLabel("")
	starSepLabel := widget.NewLabel("")
	starAngleLabel := widget.NewLabel("")
	updateStarDiameterLabels := func() {
		d1, d2, err := report.ProjectedStarDiametersKm(paramsEntry.Text)
		if err != nil {
			starDiam1Label.SetText("")
			starDiam2Label.SetText("")
		} else {
			if d1 > 0 {
				starDiam1Label.SetText(fmt.Sprintf("Star 1 proj. diameter: %.4f km", d1))
			} else {
				starDiam1Label.SetText("")
			}
			if d2 > 0 {
				starDiam2Label.SetText(fmt.Sprintf("Star 2 proj. diameter: %.4f km", d2))
			} else {
				starDiam2Label.SetText("")
			}
		}
		sepKm, angleDeg, hasSep, sepErr := report.StarSeparation(paramsEntry.Text)
		if hasSep && sepErr == nil {
			starSepLabel.SetText(fmt.Sprintf("Star separation: %.4f km", sepKm))
			starAngleLabel.SetText(fmt.Sprintf("Position angle: %.2f° CCW from N", angleDeg))
		} else {
			starSepLabel.SetText("")
			starAngleLabel.SetText("")
		}
	}

	paramsEntry.OnChanged = func(_ string) {
		paramsDirty = true
		updateStarDiameterLabels()
	}

	saveFileBtn := widget.NewButton("Save", func() {
		saveParameters(w, paramsEntry, paramsFilePath)
		paramsDirty = false
		if err := report.ValidateParams(paramsEntry.Text); err != nil {
			dialog.ShowError(fmt.Errorf("parameter file error:\n%s", err), w)
		}
	})
	saveFileBtn.Disable()

	saveAsBtn := widget.NewButton("Save As", func() {
		saveParametersAs(w, paramsEntry, sourceDir)
	})
	saveAsBtn.Disable()

	runBtn := widget.NewButton("Run IOTAdiffraction", nil)
	runBtn.Importance = widget.HighImportance

	openBtn := widget.NewButton("Open Parameters File", func() {
		openParametersFile(w, paramsEntry, &sourceDir, &paramsFilePath, saveFileBtn, saveAsBtn, runBtn, &paramsDirty)
	})
	openBtn.Importance = widget.WarningImportance
	saveFileBtn.Importance = widget.SuccessImportance
	saveAsBtn.Importance = widget.SuccessImportance

	pathOffsetEntry := newFocusLostEntry()
	pathOffsetEntry.SetText("0")
	pathOffsetEntry.OnChanged = func(text string) {
		if text == "" || text == "-" {
			return
		}
		if _, err := strconv.Atoi(text); err != nil {
			// Strip the last character that made it invalid.
			pathOffsetEntry.SetText(text[:len(text)-1])
		}
	}
	// stepPathOffset increments the current value by delta and triggers the
	// same update logic as losing focus on the entry.
	stepPathOffset := func(delta int) {
		cur, _ := strconv.Atoi(pathOffsetEntry.Text)
		pathOffsetEntry.SetText(strconv.Itoa(cur + delta))
		if pathOffsetEntry.OnFocusLost != nil {
			pathOffsetEntry.OnFocusLost()
		}
	}
	// kmPerPixel returns the pixel-to-km scale from the current parameters,
	// or 0 if the parameters cannot be parsed.
	kmPerPixel := func() float64 {
		scale, _ := report.ParsePixelScale(paramsEntry.Text)
		return scale
	}

	// exposurePixels returns the camera exposure time in pixels for path 1.
	exposurePixels := func() int {
		return calcExposurePixels(paramsEntry.Text, exposureEntry.Text, kmPerPixel())
	}
	// exposurePixels2 returns the camera exposure time in pixels for path 2.
	exposurePixels2 := func() int {
		return calcExposurePixels(paramsEntry.Text, exposure2Entry.Text, kmPerPixel())
	}

	// refreshImage redraws the upper-right diffraction image with path
	// overlay lines for both paths (when their View checkbox is checked).
	refreshImage := func() {
		if diffImagePath == "" {
			return
		}
		appDir := filepath.Dir(diffImagePath)
		var paths []pathOverlay
		if view1Check != nil && view1Check.Checked {
			offset := parsePathOffset(pathOffsetEntry.Text)
			edges := findEdgesForOffset(w, appDir, offset)
			paths = append(paths, pathOverlay{
				offset:    offset,
				edges:     edges,
				lineColor: color.RGBA{R: 255, A: 255},
			})
		}
		if view2Check != nil && view2Check.Checked {
			offset2 := parsePathOffset(pathOffset2Entry.Text)
			edges2 := findEdgesForOffset(w, appDir, offset2)
			paths = append(paths, pathOverlay{
				offset:    offset2,
				edges:     edges2,
				lineColor: color.RGBA{R: 255, G: 128, A: 255},
			})
		}
		if len(paths) == 0 {
			// No paths to show — just display the base rotated image.
			displayImage(w, imagePanel, filepath.Join(appDir, "diffractionImage8bitRotated.png"))
			return
		}
		drawPerimeter := geomEdgesCheck != nil && geomEdgesCheck.Checked
		thickness, _ := strconv.Atoi(thicknessEntry.Text)
		drawPathLines(w, imagePanel, diffImagePath, paths, drawPerimeter, thickness)
	}

	// refreshPlot redraws the light curve panel based on which View
	// checkboxes are checked, overlaying both curves when both are active.
	refreshPlot := func() {
		if diffImagePath == "" {
			return
		}
		appDir := filepath.Dir(diffImagePath)
		scale := kmPerPixel()
		yMax := parseYMax(yMaxEntry.Text)

		var curves []report.CurveData
		if view1Check != nil && view1Check.Checked {
			offset := parsePathOffset(pathOffsetEntry.Text)
			edges := findEdgesForOffset(w, appDir, offset)
			targetPath := filepath.Join(appDir, "targetImage16bitRotated.png")
			values, err := report.ExtractRow(targetPath, offset)
			if err != nil {
				dialog.ShowError(fmt.Errorf("cannot extract light curve: %w", err), w)
				return
			}
			cd := report.CurveData{
				Values:          values,
				CurveColor:      color.RGBA{B: 255, A: 255},
				IntegratedColor: color.RGBA{G: 180, A: 255},
			}
			if edges1Check != nil && edges1Check.Checked {
				cd.Edges = edges
			}
			if ep := exposurePixels(); ep > 1 {
				cd.Integrated = report.ApplyExposure(values, ep)
				cd.Values = nil // show only the camera curve
			}
			curves = append(curves, cd)
		}
		if view2Check != nil && view2Check.Checked {
			offset2 := parsePathOffset(pathOffset2Entry.Text)
			edges2 := findEdgesForOffset(w, appDir, offset2)
			targetPath := filepath.Join(appDir, "targetImage16bitRotated.png")
			values2, err := report.ExtractRow(targetPath, offset2)
			if err != nil {
				dialog.ShowError(fmt.Errorf("cannot extract light curve (path 2): %w", err), w)
				return
			}
			cd2 := report.CurveData{
				Values:          values2,
				CurveColor:      color.RGBA{R: 255, G: 128, A: 255},
				IntegratedColor: color.RGBA{R: 200, G: 100, B: 50, A: 255},
			}
			if edges2Check != nil && edges2Check.Checked {
				cd2.Edges = edges2
			}
			if ep2 := exposurePixels2(); ep2 > 1 {
				cd2.Integrated = report.ApplyExposure(values2, ep2)
				cd2.Values = nil // show only the camera curve
			}
			curves = append(curves, cd2)
		}
		if len(curves) == 0 {
			lightCurvePanel.Objects = nil
			lightCurvePanel.Refresh()
			return
		}
		plotImg := report.PlotLightCurves(curves, 1200, 400, yMax, scale)
		img := canvas.NewImageFromImage(plotImg)
		img.FillMode = canvas.ImageFillContain
		lightCurvePanel.Layout = layout.NewStackLayout()
		lightCurvePanel.Objects = []fyne.CanvasObject{img}
		lightCurvePanel.Refresh()
	}

	pathOffsetKmLabel := widget.NewLabel("")
	pathOffsetEntry.OnFocusLost = func() {
		if scale := kmPerPixel(); scale > 0 {
			offsetKm := float64(-parsePathOffset(pathOffsetEntry.Text)) * scale
			pathOffsetKmLabel.SetText(fmt.Sprintf("(%.3f km)", offsetKm))
		}
		refreshImage()
		refreshPlot()
	}

	yMaxEntry = newFocusLostEntry()
	yMaxEntry.SetText(strconv.FormatFloat(defaultYMax, 'f', -1, 64))
	yMaxEntry.OnChanged = func(text string) {
		if text == "" || text == "-" || text == "." || text == "-." {
			return
		}
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			yMaxEntry.SetText(text[:len(text)-1])
		}
	}
	yMaxEntry.OnFocusLost = func() {
		refreshPlot()
	}

	showPlotsCheck := widget.NewCheck("Show IOTAdiffraction plots", nil)

	statusLabel := widget.NewLabel("")
	pathOffsetLabel := widget.NewLabel("Path 1 offset (rows):")
	entryMinSize := pathOffsetEntry.MinSize()
	decBtn := widget.NewButton("-", func() { stepPathOffset(-1) })
	incBtn := widget.NewButton("+", func() { stepPathOffset(1) })
	spinnerEntry := container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), pathOffsetEntry)
	pathOffsetSpinner := container.NewHBox(decBtn, spinnerEntry, incBtn)
	// Preallocate space for the km label so it doesn't push adjacent widgets.
	pathOffsetKmLabel.SetText("                              ")
	kmLabelMinSize := pathOffsetKmLabel.MinSize()
	pathOffsetKmLabel.SetText("")
	pathOffsetBox := container.NewHBox(pathOffsetLabel, pathOffsetSpinner,
		container.NewGridWrap(kmLabelMinSize, pathOffsetKmLabel))

	// Second path offset spinner.
	pathOffset2Entry = newFocusLostEntry()
	pathOffset2Entry.SetText("0")
	pathOffset2Entry.OnChanged = func(text string) {
		if text == "" || text == "-" {
			return
		}
		if _, err := strconv.Atoi(text); err != nil {
			pathOffset2Entry.SetText(text[:len(text)-1])
		}
	}
	stepPathOffset2 := func(delta int) {
		cur, _ := strconv.Atoi(pathOffset2Entry.Text)
		pathOffset2Entry.SetText(strconv.Itoa(cur + delta))
		if pathOffset2Entry.OnFocusLost != nil {
			pathOffset2Entry.OnFocusLost()
		}
	}
	pathOffset2KmLabel := widget.NewLabel("")
	pathOffset2Entry.OnFocusLost = func() {
		if scale := kmPerPixel(); scale > 0 {
			offsetKm := float64(-parsePathOffset(pathOffset2Entry.Text)) * scale
			pathOffset2KmLabel.SetText(fmt.Sprintf("(%.3f km)", offsetKm))
		}
		refreshImage()
		refreshPlot()
	}
	pathOffset2Label := widget.NewLabel("Path 2 offset (rows):")
	dec2Btn := widget.NewButton("-", func() { stepPathOffset2(-1) })
	inc2Btn := widget.NewButton("+", func() { stepPathOffset2(1) })
	spinner2Entry := container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), pathOffset2Entry)
	pathOffset2Spinner := container.NewHBox(dec2Btn, spinner2Entry, inc2Btn)
	pathOffset2KmLabel.SetText("                              ")
	km2LabelMinSize := pathOffset2KmLabel.MinSize()
	pathOffset2KmLabel.SetText("")
	pathOffset2Box := container.NewHBox(pathOffset2Label, pathOffset2Spinner,
		container.NewGridWrap(km2LabelMinSize, pathOffset2KmLabel))
	yMaxLabel := widget.NewLabel("Y max:")
	yMaxBox := container.NewHBox(yMaxLabel,
		container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), yMaxEntry))

	exposureEntry = newFocusLostEntry()
	exposureEntry.SetText("0")
	exposureEntry.OnChanged = func(text string) {
		if text == "" || text == "." {
			return
		}
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			exposureEntry.SetText(text[:len(text)-1])
		}
	}
	exposureEntry.OnFocusLost = func() {
		refreshPlot()
	}
	exposureLabel := widget.NewLabel("Exposure 1 (secs):")
	exposureBox := container.NewHBox(exposureLabel,
		container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), exposureEntry))

	exposure2Entry = newFocusLostEntry()
	exposure2Entry.SetText("0")
	exposure2Entry.OnChanged = func(text string) {
		if text == "" || text == "." {
			return
		}
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			exposure2Entry.SetText(text[:len(text)-1])
		}
	}
	exposure2Entry.OnFocusLost = func() {
		refreshPlot()
	}
	exposure2Label := widget.NewLabel("Exposure 2 (secs):")
	exposure2Box := container.NewHBox(exposure2Label,
		container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), exposure2Entry))

	view1Check = widget.NewCheck("View", func(_ bool) { refreshImage(); refreshPlot() })
	view1Check.SetChecked(true)
	edges1Check = widget.NewCheck("Show edges", func(_ bool) { refreshPlot() })
	edges1Check.SetChecked(true)
	view2Check = widget.NewCheck("View", func(_ bool) { refreshImage(); refreshPlot() })
	edges2Check = widget.NewCheck("Show edges", func(_ bool) { refreshPlot() })
	edges2Check.SetChecked(true)

	angleEntry := newFocusLostEntry()
	angleEntry.SetText("0")
	angleEntry.OnChanged = func(text string) {
		if text == "" || text == "-" || text == "." || text == "-." {
			return
		}
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			angleEntry.SetText(text[:len(text)-1])
		}
	}
	// rotateImages rotates all three output images by the given angle in
	// degrees and displays the rotated diffraction image in the panel.
	rotateImages := func(deg float64) {
		deg = math.Mod(deg, 360)
		if deg < 0 {
			deg += 360
		}
		rotAngleRadians := deg * math.Pi / 180.0

		appDir, err := os.Getwd()
		if err != nil {
			dialog.ShowError(fmt.Errorf("cannot determine app directory: %w", err), w)
			return
		}
		imgPath := filepath.Join(appDir, "diffractionImage8bit.png")
		f, err := os.Open(imgPath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("cannot open diffractionImage8bit.png: %w", err), w)
			return
		}
		src, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			dialog.ShowError(fmt.Errorf("cannot decode diffractionImage8bit.png: %w", err), w)
			return
		}
		gray, ok := src.(*image.Gray)
		if !ok {
			gray = report.ToGray(src)
		}
		// Use the upper-right corner pixel as the background fill value.
		bg := gray.GrayAt(gray.Bounds().Max.X-1, gray.Bounds().Min.Y).Y
		rotated := report.RotateGrayBilinear(gray, rotAngleRadians, bg)

		outPath := filepath.Join(appDir, "diffractionImage8bitRotated.png")
		if out, err := os.Create(outPath); err != nil {
			dialog.ShowError(fmt.Errorf("cannot create diffractionImage8bitRotated.png: %w", err), w)
		} else {
			if err := png.Encode(out, rotated); err != nil {
				dialog.ShowError(fmt.Errorf("cannot write diffractionImage8bitRotated.png: %w", err), w)
			}
			out.Close()
		}

		// Rotate geometricShadow.png with bg=255.
		geoPath := filepath.Join(appDir, "geometricShadow.png")
		star1Out := filepath.Join(appDir, "geometricShadowRotated_star1.png")
		star2Out := filepath.Join(appDir, "geometricShadowRotated_star2.png")
		if gf, err := os.Open(geoPath); err != nil {
			dialog.ShowError(fmt.Errorf("cannot open geometricShadow.png: %w", err), w)
		} else {
			geoSrc, _, decErr := image.Decode(gf)
			gf.Close()
			if decErr != nil {
				dialog.ShowError(fmt.Errorf("cannot decode geometricShadow.png: %w", decErr), w)
			} else {
				geoGray, ok := geoSrc.(*image.Gray)
				if !ok {
					geoGray = report.ToGray(geoSrc)
				}
				geoRotated := report.RotateGrayBilinear(geoGray, rotAngleRadians, 255)
				if out, err := os.Create(filepath.Join(appDir, "geometricShadowRotated.png")); err != nil {
					dialog.ShowError(fmt.Errorf("cannot create geometricShadowRotated.png: %w", err), w)
				} else {
					if err := png.Encode(out, geoRotated); err != nil {
						dialog.ShowError(fmt.Errorf("cannot write geometricShadowRotated.png: %w", err), w)
					}
					out.Close()
				}

				// Write per-star offset shadows when two stars are present,
				// otherwise remove any stale copies from a prior run.
				sepKm, angleDeg, hasSep, sepErr := report.StarSeparation(paramsEntry.Text)
				scale := kmPerPixel()
				if sepErr == nil && hasSep && scale > 0 {
					sepPx := sepKm / scale
					theta := angleDeg * math.Pi / 180.0
					// Angle is CCW from N (image-up). In pixel coords +Y is
					// down, so the star-2 offset direction from star 1 is
					// (-sin θ, -cos θ) × sepPx. Each star's shadow is placed
					// at ±halfOffset relative to the image center.
					hx := -0.5 * math.Sin(theta) * sepPx
					hy := -0.5 * math.Cos(theta) * sepPx
					writeOffsetShadow := func(path string, tx, ty float64) {
						rot := report.RotateTranslateGrayBilinear(geoGray, rotAngleRadians, tx, ty, 255)
						out, err := os.Create(path)
						if err != nil {
							dialog.ShowError(fmt.Errorf("cannot create %s: %w", filepath.Base(path), err), w)
							return
						}
						defer out.Close()
						if err := png.Encode(out, rot); err != nil {
							dialog.ShowError(fmt.Errorf("cannot write %s: %w", filepath.Base(path), err), w)
						}
					}
					writeOffsetShadow(star1Out, -hx, -hy)
					writeOffsetShadow(star2Out, hx, hy)
				} else {
					os.Remove(star1Out)
					os.Remove(star2Out)
				}
			}
		}

		// Rotate targetImage16bit.png with bg=4000.
		tgtPath := filepath.Join(appDir, "targetImage16bit.png")
		if tf, err := os.Open(tgtPath); err != nil {
			dialog.ShowError(fmt.Errorf("cannot open targetImage16bit.png: %w", err), w)
		} else {
			tgtSrc, _, decErr := image.Decode(tf)
			tf.Close()
			if decErr != nil {
				dialog.ShowError(fmt.Errorf("cannot decode targetImage16bit.png: %w", decErr), w)
			} else {
				tgtGray16, ok := tgtSrc.(*image.Gray16)
				if !ok {
					tgtGray16 = report.ToGray16(tgtSrc)
				}
				tgtRotated := report.RotateGray16Bilinear(tgtGray16, rotAngleRadians, 4000)
				if out, err := os.Create(filepath.Join(appDir, "targetImage16bitRotated.png")); err != nil {
					dialog.ShowError(fmt.Errorf("cannot create targetImage16bitRotated.png: %w", err), w)
				} else {
					if err := png.Encode(out, tgtRotated); err != nil {
						dialog.ShowError(fmt.Errorf("cannot write targetImage16bitRotated.png: %w", err), w)
					}
					out.Close()
				}
			}
		}

		img := canvas.NewImageFromImage(rotated)
		img.FillMode = canvas.ImageFillContain
		imagePanel.Layout = layout.NewStackLayout()
		imagePanel.Objects = []fyne.CanvasObject{img}
		imagePanel.Refresh()
	}

	angleEntry.OnFocusLost = func() {
		deg, err := strconv.ParseFloat(angleEntry.Text, 64)
		if err != nil {
			return
		}
		rotateImages(deg)
		if diffImagePath != "" {
			refreshImage()
			refreshPlot()
		}
	}

	runBtn.OnTapped = func() {
		if paramsDirty && paramsFilePath != "" {
			saveParameters(w, paramsEntry, paramsFilePath)
			paramsDirty = false
		}
		if err := report.ValidateParams(paramsEntry.Text); err != nil {
			dialog.ShowError(fmt.Errorf("parameter file error:\n%s", err), w)
			return
		}
		// Clear any previous light curve plot and image from an earlier run.
		lightCurvePanel.Objects = []fyne.CanvasObject{
			container.NewCenter(widget.NewLabel("No light curve — run diffraction to generate one.")),
		}
		lightCurvePanel.Refresh()
		imagePanel.Objects = []fyne.CanvasObject{
			container.NewCenter(widget.NewLabel("No image loaded — run diffraction to generate one.")),
		}
		imagePanel.Refresh()
		runDiffraction(w, runBtn, statusLabel, paramsFilePath, imagePanel, &diffImagePath, showPlotsCheck.Checked, &diffCmd, func() {
			// Display the IOTAdiffraction-produced image with path overlay as-is.
			appDir, err := os.Getwd()
			if err != nil {
				dialog.ShowError(fmt.Errorf("cannot determine app directory: %w", err), w)
				return
			}
			displayImage(w, imagePanel, filepath.Join(appDir, "diffractionImageWithPath.png"))
			// Automatically rotate so path is horizontal left-to-right.
			dx, dy, rotErr := report.ParseShadowVelocity(paramsEntry.Text)
			if rotErr != nil {
				dialog.ShowError(fmt.Errorf("cannot compute standard angle: %w", rotErr), w)
				return
			}
			pathAngle := report.PathAngleFromVelocity(dx, dy)
			stdAngle := pathAngle - 270.0
			angleEntry.SetText(strconv.FormatFloat(stdAngle, 'f', 2, 64))
			rotateImages(stdAngle)
			if diffImagePath != "" {
				refreshImage()
				refreshPlot()
			}
			dialog.ShowInformation("IOTAdiffraction complete",
				"Images have been rotated so that the observation path is horizontal (left to right).\n\n"+
					"The light curve has been extracted and plotted.", w)
		})
	}
	leftPanelBg := canvas.NewRectangle(color.Transparent)
	geomEdgesCheck = widget.NewCheck("Shadow perimeter", func(_ bool) { refreshImage() })

	// makeIntSpinner builds a labeled `- [entry] +` integer spinner that calls
	// onChange whenever the value is stepped or the entry loses focus.
	makeIntSpinner := func(label string, initial, minVal int, onChange func()) (*focusLostEntry, fyne.CanvasObject) {
		e := newFocusLostEntry()
		e.SetText(strconv.Itoa(initial))
		e.OnChanged = func(text string) {
			if text == "" {
				return
			}
			if _, err := strconv.Atoi(text); err != nil {
				e.SetText(text[:len(text)-1])
			}
		}
		e.OnFocusLost = func() {
			v, err := strconv.Atoi(e.Text)
			if err != nil || v < minVal {
				e.SetText(strconv.Itoa(minVal))
			}
			onChange()
		}
		step := func(delta int) {
			v, _ := strconv.Atoi(e.Text)
			v += delta
			if v < minVal {
				v = minVal
			}
			e.SetText(strconv.Itoa(v))
			onChange()
		}
		dec := widget.NewButton("-", func() { step(-1) })
		inc := widget.NewButton("+", func() { step(1) })
		box := container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), e)
		return e, container.NewHBox(widget.NewLabel(label), dec, box, inc)
	}
	var thicknessSpinner fyne.CanvasObject
	thicknessEntry, thicknessSpinner = makeIntSpinner("Thickness:", 1, 1, refreshImage)
	darkModeCheck = widget.NewCheck("Dark mode", func(checked bool) {
		if checked {
			leftPanelBg.FillColor = color.RGBA{R: 30, G: 30, B: 30, A: 255}
		} else {
			leftPanelBg.FillColor = color.Transparent
		}
		leftPanelBg.Refresh()
	})
	toolbarRow1 := container.NewHBox(openBtn, saveFileBtn, saveAsBtn, widget.NewSeparator(), runBtn, showPlotsCheck)
	toolbar := container.NewVBox(toolbarRow1)
	hSplit := container.NewHSplit(paramsScroll, imagePanel)
	lightCurveLeftContent := container.NewVBox(
		yMaxBox, widget.NewSeparator(),
		container.NewHBox(view1Check, edges1Check), pathOffsetBox, exposureBox, widget.NewSeparator(),
		container.NewHBox(view2Check, edges2Check), pathOffset2Box, exposure2Box,
		widget.NewSeparator(),
		container.NewHBox(darkModeCheck, geomEdgesCheck, thicknessSpinner),
		starDiam1Label, starDiam2Label,
		starSepLabel, starAngleLabel,
	)
	lightCurveLeftPanel := container.NewStack(leftPanelBg, lightCurveLeftContent)
	lightCurveWithControls := container.NewBorder(nil, nil, lightCurveLeftPanel, nil, lightCurvePanel)
	vSplit := container.NewVSplit(hSplit, lightCurveWithControls)

	restorePreferences(a.Preferences(), w, hSplit, vSplit)

	w.SetContent(container.NewBorder(toolbar, nil, nil, nil, vSplit))
	w.SetCloseIntercept(func() {
		savePreferences(a.Preferences(), w, hSplit, vSplit)
		if diffCmd != nil && diffCmd.Process != nil {
			diffCmd.Process.Kill()
		}
		w.Close()
	})

	w.ShowAndRun()
}

// bigFontTheme wraps the default theme, increasing the text size by 50%.
type bigFontTheme struct {
	fyne.Theme
}

func (t *bigFontTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return t.Theme.Size(name) * 1.5
	}
	return t.Theme.Size(name)
}

// buildParamsPanel creates an editable multiline text area with a horizontal
// scroll container and enlarged font. Returns the entry (for accessing text)
// and a themed container (for layout).
func buildParamsPanel(a fyne.App, placeholder string) (*widget.Entry, fyne.CanvasObject) {
	entry := widget.NewMultiLineEntry()
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	entry.SetPlaceHolder(placeholder)
	scroll := container.NewHScroll(entry)
	themed := container.NewThemeOverride(scroll, &bigFontTheme{Theme: a.Settings().Theme()})
	return entry, themed
}

// buildImagePanel creates a panel with a centered placeholder label, suitable
// for later replacement with an image canvas.
func buildImagePanel(placeholder string) *fyne.Container {
	label := widget.NewLabel(placeholder)
	label.Alignment = fyne.TextAlignCenter
	return container.NewCenter(label)
}

// openParametersFile shows a file-open dialog filtered to .json and .json5
// files, loads the selected file contents into the entry widget, records the
// source directory, and enables the Save As button.
func openParametersFile(w fyne.Window, entry *widget.Entry, sourceDir *string, paramsFilePath *string, saveFileBtn, saveAsBtn, runBtn *widget.Button, dirty *bool) {
	fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		filePath := reader.URI().Path()
		*sourceDir = filepath.Dir(filePath)
		*paramsFilePath = filePath

		data, readErr := io.ReadAll(reader)
		if closeErr := reader.Close(); closeErr != nil {
			dialog.ShowError(closeErr, w)
			return
		}
		if readErr != nil {
			dialog.ShowError(readErr, w)
			return
		}
		// Normalize line endings so the editor always works with
		// consistent text and re-saves will produce valid CRLF files.
		entry.SetText(ensureCRLF(string(data)))
		*dirty = false
		saveAsBtn.Enable()

		// Check if the file is writable.
		if f, err := os.OpenFile(filePath, os.O_WRONLY, 0); err != nil {
			saveFileBtn.Disable()
			runBtn.Disable()
			dialog.ShowInformation("Read-Only File",
				"This file is read-only, so the Save and Run Diffraction buttons have been disabled.\nUse Save As to create an editable copy.", w)
		} else {
			f.Close()
			saveFileBtn.Enable()
			runBtn.Enable()
		}

		w.SetTitle("DiffractionDemo " + Version + " — " + filePath)
	}, w)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".json", ".json5"}))
	fd.Resize(fyne.NewSize(800, 600))
	fd.Show()
}

// ensureCRLF normalises line endings to CRLF. It first strips any existing
// CR characters so that mixed or pure-CR files are handled, then inserts
// CR before every LF.
func ensureCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// saveParameters writes the current editor contents back to the originally
// opened parameters file, ensuring CRLF line endings.
func saveParameters(w fyne.Window, entry *widget.Entry, paramsFile string) {
	if err := os.WriteFile(paramsFile, []byte(ensureCRLF(entry.Text)), 0644); err != nil {
		dialog.ShowError(err, w)
	}
}

// saveParametersAs prompts for a new filename and writes the current editor
// contents to the same directory the parameters file was loaded from.
func saveParametersAs(w fyne.Window, entry *widget.Entry, sourceDir string) {
	if entry.Text == "" {
		dialog.ShowError(fmt.Errorf("no parameters to save"), w)
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("new_parameters.json5")

	items := []*widget.FormItem{
		widget.NewFormItem("Directory", widget.NewLabel(sourceDir)),
		widget.NewFormItem("File name", nameEntry),
	}

	dialog.ShowForm("Save Parameters As", "Save", "Cancel", items, func(ok bool) {
		if !ok || nameEntry.Text == "" {
			return
		}
		baseName := strings.TrimSuffix(nameEntry.Text, filepath.Ext(nameEntry.Text))
		savePath := filepath.Join(sourceDir, baseName+".json5")
		if err := os.WriteFile(savePath, []byte(ensureCRLF(entry.Text)), 0644); err != nil {
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowInformation("Saved", "File saved to:\n"+savePath, w)
		if err := report.ValidateParams(entry.Text); err != nil {
			dialog.ShowError(fmt.Errorf("parameter file error:\n%s", err), w)
		}
	}, w)
}

// runDiffraction launches IOTAdiffraction.exe from the application directory
// in a background goroutine. The button is disabled while running. On success,
// diffractionImage8bit.png is displayed in the provided image panel with a
// path-offset line drawn via the onImageReady callback.
func runDiffraction(w fyne.Window, btn *widget.Button, status *widget.Label, paramsFile string, imagePanel *fyne.Container, diffImagePath *string, showPlots bool, diffCmd **exec.Cmd, onImageReady func()) {
	if paramsFile == "" {
		dialog.ShowError(fmt.Errorf("no parameters file has been opened"), w)
		return
	}

	appDir, err := os.Getwd()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot determine app directory: %w", err), w)
		return
	}
	diffExe := filepath.Join(appDir, "IOTAdiffraction.exe")

	if _, err := os.Stat(diffExe); err != nil {
		dialog.ShowError(fmt.Errorf("IOTAdiffraction.exe not found in %s", appDir), w)
		return
	}

	btn.Disable()
	status.SetText("Running IOTAdiffraction...")

	// Delete any previous error log before launching.
	errLogPath := filepath.Join(appDir, "IOTAdiffractionError.log")
	os.Remove(errLogPath)

	progress := widget.NewProgressBarInfinite()
	dlg := dialog.NewCustomWithoutButtons("Running IOTAdiffraction...", progress, w)
	dlg.Show()

	// Record the current mod times of output images so we can detect when
	// IOTAdiffraction has written new ones.
	outputPath := filepath.Join(appDir, "diffractionImage8bit.png")
	targetPath := filepath.Join(appDir, "targetImage16bit.png")
	withPathPath := filepath.Join(appDir, "diffractionImageWithPath.png")
	var prevModTime time.Time
	if info, err := os.Stat(outputPath); err == nil {
		prevModTime = info.ModTime()
	}
	var prevTargetModTime time.Time
	if info, err := os.Stat(targetPath); err == nil {
		prevTargetModTime = info.ModTime()
	}
	var prevWithPathModTime time.Time
	if info, err := os.Stat(withPathPath); err == nil {
		prevWithPathModTime = info.ModTime()
	}

	go func() {
		plotsArg := "False"
		if showPlots {
			plotsArg = "True"
		}
		cmd := exec.Command(diffExe, paramsFile, plotsArg)
		cmd.Dir = appDir
		*diffCmd = cmd
		if err := cmd.Start(); err != nil {
			*diffCmd = nil
			fyne.Do(func() {
				dlg.Hide()
				progress.Stop()
				btn.Enable()
				status.SetText("Failed")
				dialog.ShowError(fmt.Errorf("IOTAdiffraction failed to start: %w", err), w)
			})
			return
		}

		// Poll until all output images are written (mod times change),
		// an error log appears, or the process exits.
		diffReady := false
		targetReady := false
		withPathReady := false
		fatalError := false
		for !diffReady || !targetReady || !withPathReady {
			time.Sleep(500 * time.Millisecond)

			// Check for fatal error log from IOTAdiffraction.
			if _, statErr := os.Stat(errLogPath); statErr == nil {
				fatalError = true
				break
			}

			if !diffReady {
				if info, err := os.Stat(outputPath); err == nil && info.ModTime().After(prevModTime) && pngFullyReadable(outputPath) {
					diffReady = true
				}
			}
			if !targetReady {
				if info, err := os.Stat(targetPath); err == nil && info.ModTime().After(prevTargetModTime) && pngFullyReadable(targetPath) {
					targetReady = true
				}
			}
			if !withPathReady {
				if info, err := os.Stat(withPathPath); err == nil && info.ModTime().After(prevWithPathModTime) && pngFullyReadable(withPathPath) {
					withPathReady = true
				}
			}
			// Also stop polling if the process has already exited.
			if cmd.ProcessState != nil {
				break
			}
		}

		// If IOTAdiffraction wrote an error log, show its contents and abort.
		if fatalError {
			errContents, readErr := os.ReadFile(errLogPath)
			errMsg := string(errContents)
			if readErr != nil {
				errMsg = fmt.Sprintf("IOTAdiffraction reported an error (could not read log: %v)", readErr)
			}
			cmd.Wait()
			*diffCmd = nil
			fyne.Do(func() {
				dlg.Hide()
				progress.Stop()
				btn.Enable()
				status.SetText("Failed")
				dialog.ShowError(fmt.Errorf("%s", errMsg), w)
			})
			return
		}

		outputReady := diffReady && targetReady && withPathReady

		// Check for early process exit with error (no output produced).
		if !outputReady {
			waitErr := cmd.Wait()
			fyne.Do(func() {
				dlg.Hide()
				progress.Stop()
				btn.Enable()
				status.SetText("Failed")
				dialog.ShowError(fmt.Errorf("IOTAdiffraction failed: %w", waitErr), w)
			})
			return
		}

		fyne.Do(func() {
			dlg.Hide()
			progress.Stop()
			btn.Enable()
			status.SetText("Completed")
			*diffImagePath = outputPath
			displayImage(w, imagePanel, *diffImagePath)
			onImageReady()
		})

		// Let the process finish in the background (plot windows still open).
		cmd.Wait()
		*diffCmd = nil
	}()
}

// pngFullyReadable returns true if the file at path can be opened and fully
// decoded as a PNG. This guards against reading a file that is still being
// written by an external process.
func pngFullyReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = png.Decode(f)
	return err == nil
}

// displayImage replaces the contents of a panel container with the image at
// the given path, scaled to fill the available space. The file is fully
// decoded into memory before handing it to Fyne so that a partially-written
// file does not cause a render error.
func displayImage(w fyne.Window, panel *fyne.Container, path string) {
	f, err := os.Open(path)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open image %s: %w", filepath.Base(path), err), w)
		return
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot decode image %s: %w", filepath.Base(path), err), w)
		return
	}
	img := canvas.NewImageFromImage(src)
	img.FillMode = canvas.ImageFillContain
	panel.Layout = layout.NewStackLayout()
	panel.Objects = []fyne.CanvasObject{img}
	panel.Refresh()
}

// pathOverlay holds the offset and edge data for one observation path.
type pathOverlay struct {
	offset    int
	edges     []int
	lineColor color.RGBA
}

// drawPathLines loads the rotated diffraction image, draws horizontal path
// lines and vertical edge markers for each overlay, and displays the result.
// When drawShadowPerimeter is true, the perimeter of the geometric shadow
// (or both per-star shadows when present) is traced in solid red at the
// supplied line thickness.
func drawPathLines(w fyne.Window, panel *fyne.Container, imagePath string, paths []pathOverlay, drawShadowPerimeter bool, thickness int) {
	appDir := filepath.Dir(imagePath)
	rotatedPath := filepath.Join(appDir, "diffractionImage8bitRotated.png")
	f, err := os.Open(rotatedPath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open rotated image: %w", err), w)
		return
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot decode rotated image: %w", err), w)
		return
	}

	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	green := color.RGBA{G: 255, A: 255}
	red := color.RGBA{R: 255, A: 255}
	for _, po := range paths {
		// Draw observation path as a 4-pixel horizontal line.
		lineY := bounds.Min.Y + bounds.Dy()/2 + po.offset
		for dy := 0; dy < 4; dy++ {
			y := lineY + dy
			if y < bounds.Min.Y || y >= bounds.Max.Y {
				continue
			}
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				dst.Set(x, y, po.lineColor)
			}
		}

		// Draw edge positions as full-height green vertical lines, 3 pixels wide.
		for _, ex := range po.edges {
			for dx := -1; dx <= 1; dx++ {
				px := ex + dx
				if px < bounds.Min.X || px >= bounds.Max.X {
					continue
				}
				for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
					dst.Set(px, y, green)
				}
			}
		}
	}

	if drawShadowPerimeter {
		star1Path := filepath.Join(appDir, "geometricShadowRotated_star1.png")
		star2Path := filepath.Join(appDir, "geometricShadowRotated_star2.png")
		_, err1 := os.Stat(star1Path)
		_, err2 := os.Stat(star2Path)
		if err1 == nil && err2 == nil {
			overlayShadowPerimeter(dst, star1Path, red, thickness)
			overlayShadowPerimeter(dst, star2Path, red, thickness)
		} else {
			overlayShadowPerimeter(dst, filepath.Join(appDir, "geometricShadowRotated.png"), red, thickness)
		}
	}

	img := canvas.NewImageFromImage(dst)
	img.FillMode = canvas.ImageFillContain
	panel.Layout = layout.NewStackLayout()
	panel.Objects = []fyne.CanvasObject{img}
	panel.Refresh()
}

// overlayShadowPerimeter reads a binary geometric-shadow PNG and paints a
// solid-color outline of the shadow region onto dst. A perimeter pixel is
// any shadow (dark) pixel that has at least one 4-neighbor outside the
// shadow; thickness controls how many pixels wide the outline is drawn.
func overlayShadowPerimeter(dst *image.RGBA, imagePath string, c color.RGBA, thickness int) {
	if thickness < 1 {
		thickness = 1
	}
	f, err := os.Open(imagePath)
	if err != nil {
		return
	}
	src, _, decErr := image.Decode(f)
	f.Close()
	if decErr != nil {
		return
	}
	sb := src.Bounds()
	db := dst.Bounds()
	isShadow := func(x, y int) bool {
		if x < sb.Min.X || x >= sb.Max.X || y < sb.Min.Y || y >= sb.Max.Y {
			return false
		}
		r, _, _, _ := src.At(x, y).RGBA()
		return r < 0x7FFF
	}
	startOff := -(thickness / 2)
	endOff := (thickness - 1) / 2
	for y := sb.Min.Y; y < sb.Max.Y; y++ {
		for x := sb.Min.X; x < sb.Max.X; x++ {
			if !isShadow(x, y) {
				continue
			}
			if isShadow(x-1, y) && isShadow(x+1, y) && isShadow(x, y-1) && isShadow(x, y+1) {
				continue
			}
			for dy := startOff; dy <= endOff; dy++ {
				for dx := startOff; dx <= endOff; dx++ {
					px, py := x+dx, y+dy
					if px < db.Min.X || px >= db.Max.X || py < db.Min.Y || py >= db.Max.Y {
						continue
					}
					dst.Set(px, py, c)
				}
			}
		}
	}
}

// parsePathOffset parses the path offset entry text as an integer,
// returning 0 for empty or invalid input. The sign is negated for
// internal use so that positive user input moves the path downward
// in the image coordinate system.
func parsePathOffset(text string) int {
	n, _ := strconv.Atoi(text)
	return -n
}

// parseYMax parses the Y max entry text as a float, returning the
// default value for empty or invalid input.
func parseYMax(text string) float64 {
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return defaultYMax
	}
	return v
}

// calcExposurePixels returns the camera exposure time rounded to the nearest
// integer number of pixels, or 0 if the exposure is zero or parameters are
// unavailable.
func calcExposurePixels(paramsText, exposureText string, kmPerPx float64) int {
	exposure, err := strconv.ParseFloat(exposureText, 64)
	if err != nil || exposure == 0 || kmPerPx == 0 {
		return 0
	}
	speed, err := report.ParseShadowSpeed(paramsText)
	if err != nil {
		return 0
	}
	return int(math.Round(exposure * speed / kmPerPx))
}

// findEdgesForOffset returns the geometric shadow edge positions for the
// given path offset, or nil if the shadow image cannot be read. When the
// per-star offset shadows exist (double-star case), it returns the combined
// edges from both.
func findEdgesForOffset(w fyne.Window, appDir string, offset int) []int {
	star1Path := filepath.Join(appDir, "geometricShadowRotated_star1.png")
	star2Path := filepath.Join(appDir, "geometricShadowRotated_star2.png")
	_, err1 := os.Stat(star1Path)
	_, err2 := os.Stat(star2Path)
	if err1 == nil && err2 == nil {
		e1, e1Err := report.FindEdges(star1Path, offset)
		if e1Err != nil {
			dialog.ShowError(fmt.Errorf("cannot find edges (star 1): %w", e1Err), w)
		}
		e2, e2Err := report.FindEdges(star2Path, offset)
		if e2Err != nil {
			dialog.ShowError(fmt.Errorf("cannot find edges (star 2): %w", e2Err), w)
		}
		return append(e1, e2...)
	}
	shadowPath := filepath.Join(appDir, "geometricShadowRotated.png")
	edges, err := report.FindEdges(shadowPath, offset)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot find edges: %w", err), w)
	}
	return edges
}

// showResultsWindow opens a new window displaying lightCurvePlot.png and
// diffractionImageWithPath.png side by side, with the plot scaled to match
// the diffraction image height.
func showResultsWindow(w fyne.Window, appDir string) {
	diffPath := filepath.Join(appDir, "diffractionImageWithPath.png")
	plotPath := filepath.Join(appDir, "lightCurvePlot.png")

	diffW, diffH, err := getImageSize(diffPath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot read diffractionImageWithPath.png: %w", err), w)
		return
	}
	plotW, plotH, err := getImageSize(plotPath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot read lightCurvePlot.png: %w", err), w)
		return
	}

	// Scale both images to a common display height, preserving aspect ratios.
	const displayHeight float32 = 600
	diffDisplayW := float32(diffW) * displayHeight / float32(diffH)
	plotDisplayW := float32(plotW) * displayHeight / float32(plotH)

	plotImg := canvas.NewImageFromFile(plotPath)
	plotImg.FillMode = canvas.ImageFillContain
	plotImg.SetMinSize(fyne.NewSize(plotDisplayW, displayHeight))

	diffImg := canvas.NewImageFromFile(diffPath)
	diffImg.FillMode = canvas.ImageFillContain
	diffImg.SetMinSize(fyne.NewSize(diffDisplayW, displayHeight))

	resultsWin := fyne.CurrentApp().NewWindow("Diffraction Results")
	resultsWin.SetContent(container.NewHBox(plotImg, diffImg))
	resultsWin.Show()
}

// getImageSize returns the pixel dimensions of an image file by decoding
// only its header.
func getImageSize(path string) (width, height int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	cfg, _, decErr := image.DecodeConfig(f)
	if closeErr := f.Close(); closeErr != nil {
		return 0, 0, closeErr
	}
	if decErr != nil {
		return 0, 0, decErr
	}
	return cfg.Width, cfg.Height, nil
}

// restorePreferences reads saved window size, position, and splitter offsets
// from persistent preferences and applies them.
func restorePreferences(prefs fyne.Preferences, w fyne.Window, hSplit, vSplit *container.Split) {
	width := prefs.FloatWithFallback(prefWindowWidth, defaultWidth)
	height := prefs.FloatWithFallback(prefWindowHeight, defaultHeight)
	w.Resize(fyne.NewSize(float32(width), float32(height)))

	// Restore window position if previously saved.
	x := prefs.Int(prefWindowX)
	y := prefs.Int(prefWindowY)
	if prefs.BoolWithFallback(prefWindowPosSet, false) {
		// Position must be applied after the window is shown, so defer it.
		// RunNative handles its own thread marshaling — do not wrap in fyne.Do.
		go func() {
			time.Sleep(500 * time.Millisecond)
			setWindowPosition(w, x, y)
		}()
	}

	hSplit.SetOffset(prefs.FloatWithFallback(prefHSplitOffset, defaultHSplit))
	vSplit.SetOffset(prefs.FloatWithFallback(prefVSplitOffset, defaultVSplit))
}

// savePreferences writes the current window size, position, and splitter
// offsets to persistent preferences.
func savePreferences(prefs fyne.Preferences, w fyne.Window, hSplit, vSplit *container.Split) {
	size := w.Canvas().Size()
	prefs.SetFloat(prefWindowWidth, float64(size.Width))
	prefs.SetFloat(prefWindowHeight, float64(size.Height))

	if x, y, ok := getWindowPosition(w); ok {
		prefs.SetInt(prefWindowX, x)
		prefs.SetInt(prefWindowY, y)
		prefs.SetBool(prefWindowPosSet, true)
	}

	prefs.SetFloat(prefHSplitOffset, hSplit.Offset)
	prefs.SetFloat(prefVSplitOffset, vSplit.Offset)
}
