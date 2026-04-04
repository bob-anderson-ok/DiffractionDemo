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
	"fyne.io/fyne/v2/widget"
)

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
	w := a.NewWindow("DiffractionDemo")

	paramsEntry, paramsScroll := buildParamsPanel("Open a parameters file to view its contents...")
	imagePanel := buildImagePanel("No image loaded — run diffraction to generate one.")
	lightCurvePanel := buildImagePanel("No light curve — run diffraction to generate one.")

	var sourceDir string
	var paramsFilePath string
	var diffImagePath string
	var paramsDirty bool
	var yMaxEntry *focusLostEntry
	var exposureEntry *focusLostEntry
	var diffCmd *exec.Cmd

	paramsEntry.OnChanged = func(_ string) {
		paramsDirty = true
	}

	saveFileBtn := widget.NewButton("Save", func() {
		saveParameters(w, paramsEntry, paramsFilePath)
		paramsDirty = false
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
	pathOffsetEntry.SetPlaceHolder("0")
	pathOffsetEntry.OnChanged = func(text string) {
		if text == "" || text == "-" {
			return
		}
		if _, err := strconv.Atoi(text); err != nil {
			// Strip the last character that made it invalid.
			pathOffsetEntry.SetText(text[:len(text)-1])
		}
	}
	// kmPerPixel returns the pixel-to-km scale from the current parameters,
	// or 0 if the parameters cannot be parsed.
	kmPerPixel := func() float64 {
		scale, _ := report.ParsePixelScale(paramsEntry.Text)
		return scale
	}

	// exposurePixels returns the camera exposure time in pixels.
	exposurePixels := func() int {
		return calcExposurePixels(paramsEntry.Text, exposureEntry.Text, kmPerPixel())
	}

	pathOffsetKmLabel := widget.NewLabel("")
	pathOffsetEntry.OnFocusLost = func() {
		if scale := kmPerPixel(); scale > 0 {
			offsetKm := float64(parsePathOffset(pathOffsetEntry.Text)) * scale
			pathOffsetKmLabel.SetText(fmt.Sprintf("(%.3f km)", offsetKm))
		}
		if diffImagePath != "" {
			offset := parsePathOffset(pathOffsetEntry.Text)
			appDir := filepath.Dir(diffImagePath)
			edges := findEdgesForOffset(appDir, offset)
			drawPathLine(imagePanel, diffImagePath, offset, edges)
			plotRowLightCurve(w, lightCurvePanel, appDir, offset, edges, parseYMax(yMaxEntry.Text), kmPerPixel(), exposurePixels())
		}
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
		if diffImagePath != "" {
			offset := parsePathOffset(pathOffsetEntry.Text)
			appDir := filepath.Dir(diffImagePath)
			edges := findEdgesForOffset(appDir, offset)
			plotRowLightCurve(w, lightCurvePanel, appDir, offset, edges, parseYMax(yMaxEntry.Text), kmPerPixel(), exposurePixels())
		}
	}

	showPlotsCheck := widget.NewCheck("Show IOTAdiffraction plots", nil)

	statusLabel := widget.NewLabel("")
	pathOffsetLabel := widget.NewLabel("Path offset from center (rows):")
	entryMinSize := pathOffsetEntry.MinSize()
	// Preallocate space for the km label so it doesn't push adjacent widgets.
	pathOffsetKmLabel.SetText("                              ")
	kmLabelMinSize := pathOffsetKmLabel.MinSize()
	pathOffsetKmLabel.SetText("")
	pathOffsetBox := container.NewHBox(pathOffsetLabel,
		container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), pathOffsetEntry),
		container.NewGridWrap(kmLabelMinSize, pathOffsetKmLabel))
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
		if diffImagePath != "" {
			offset := parsePathOffset(pathOffsetEntry.Text)
			appDir := filepath.Dir(diffImagePath)
			edges := findEdgesForOffset(appDir, offset)
			plotRowLightCurve(w, lightCurvePanel, appDir, offset, edges, parseYMax(yMaxEntry.Text), kmPerPixel(), exposurePixels())
		}
	}
	exposureLabel := widget.NewLabel("Camera exposure (secs):")
	exposureBox := container.NewHBox(exposureLabel,
		container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), exposureEntry))

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
			return
		}
		imgPath := filepath.Join(appDir, "diffractionImage8bit.png")
		f, err := os.Open(imgPath)
		if err != nil {
			fmt.Printf("Cannot open diffractionImage8bit.png: %v\n", err)
			return
		}
		src, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			fmt.Printf("Cannot decode diffractionImage8bit.png: %v\n", err)
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
		if out, err := os.Create(outPath); err == nil {
			png.Encode(out, rotated)
			out.Close()
		}

		// Rotate geometricShadow.png with bg=255.
		geoPath := filepath.Join(appDir, "geometricShadow.png")
		if gf, err := os.Open(geoPath); err == nil {
			if geoSrc, _, err := image.Decode(gf); err == nil {
				geoGray, ok := geoSrc.(*image.Gray)
				if !ok {
					geoGray = report.ToGray(geoSrc)
				}
				geoRotated := report.RotateGrayBilinear(geoGray, rotAngleRadians, 255)
				if out, err := os.Create(filepath.Join(appDir, "geometricShadowRotated.png")); err == nil {
					png.Encode(out, geoRotated)
					out.Close()
				}
			}
			gf.Close()
		}

		// Rotate targetImage16bit.png with bg=4000.
		tgtPath := filepath.Join(appDir, "targetImage16bit.png")
		if tf, err := os.Open(tgtPath); err == nil {
			if tgtSrc, _, err := image.Decode(tf); err == nil {
				tgtGray16, ok := tgtSrc.(*image.Gray16)
				if !ok {
					tgtGray16 = report.ToGray16(tgtSrc)
				}
				tgtRotated := report.RotateGray16Bilinear(tgtGray16, rotAngleRadians, 4000)
				if out, err := os.Create(filepath.Join(appDir, "targetImage16bitRotated.png")); err == nil {
					png.Encode(out, tgtRotated)
					out.Close()
				}
			}
			tf.Close()
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
			offset := parsePathOffset(pathOffsetEntry.Text)
			appDir := filepath.Dir(diffImagePath)
			edges := findEdgesForOffset(appDir, offset)
			drawPathLine(imagePanel, diffImagePath, offset, edges)
			plotRowLightCurve(w, lightCurvePanel, appDir, offset, edges, parseYMax(yMaxEntry.Text), kmPerPixel(), exposurePixels())
		}
	}

	runBtn.OnTapped = func() {
		if paramsDirty && paramsFilePath != "" {
			saveParameters(w, paramsEntry, paramsFilePath)
			paramsDirty = false
		}
		runDiffraction(w, runBtn, statusLabel, paramsFilePath, imagePanel, &diffImagePath, showPlotsCheck.Checked, &diffCmd, func() {
			// Display the IOTAdiffraction-produced image with path overlay as-is.
			appDir, _ := os.Getwd()
			displayImage(imagePanel, filepath.Join(appDir, "diffractionImageWithPath.png"))
		})
	}
	angleLabel := widget.NewLabel("Angle (deg):")
	angleBox := container.NewHBox(angleLabel,
		container.NewGridWrap(fyne.NewSize(entryMinSize.Width*2, entryMinSize.Height), angleEntry))

	rotateStdBtn := widget.NewButton("Rotate images to standard position", func() {
		dx, dy, err := report.ParseShadowVelocity(paramsEntry.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("cannot compute standard angle: %w", err), w)
			return
		}
		pathAngle := report.PathAngleFromVelocity(dx, dy)
		stdAngle := pathAngle - 270.0
		angleEntry.SetText(strconv.FormatFloat(stdAngle, 'f', 2, 64))
		rotateImages(stdAngle)
		if diffImagePath != "" {
			offset := parsePathOffset(pathOffsetEntry.Text)
			appDir := filepath.Dir(diffImagePath)
			edges := findEdgesForOffset(appDir, offset)
			drawPathLine(imagePanel, diffImagePath, offset, edges)
			plotRowLightCurve(w, lightCurvePanel, appDir, offset, edges, parseYMax(yMaxEntry.Text), kmPerPixel(), exposurePixels())
		}
	})

	toolbarRow1 := container.NewHBox(openBtn, saveFileBtn, saveAsBtn, widget.NewSeparator(), runBtn, showPlotsCheck)
	toolbarRow2 := container.NewHBox(pathOffsetBox, exposureBox, angleBox, rotateStdBtn)
	toolbar := container.NewVBox(toolbarRow1, toolbarRow2)
	hSplit := container.NewHSplit(paramsScroll, imagePanel)
	lightCurveWithYMax := container.NewBorder(nil, nil, yMaxBox, nil, lightCurvePanel)
	vSplit := container.NewVSplit(hSplit, lightCurveWithYMax)

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

// buildParamsPanel creates an editable multiline text area with a horizontal
// scroll container. Returns both the entry (for accessing text) and the
// scroll container (for layout).
func buildParamsPanel(placeholder string) (*widget.Entry, *container.Scroll) {
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder(placeholder)
	scroll := container.NewHScroll(entry)
	return entry, scroll
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
		entry.SetText(string(data))
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

		w.SetTitle("DiffractionDemo — " + filePath)
	}, w)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".json", ".json5"}))
	fd.Resize(fyne.NewSize(800, 600))
	fd.Show()
}

// saveParameters writes the current editor contents back to the originally
// opened parameters file.
func saveParameters(w fyne.Window, entry *widget.Entry, paramsFile string) {
	if err := os.WriteFile(paramsFile, []byte(entry.Text), 0644); err != nil {
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
		if err := os.WriteFile(savePath, []byte(entry.Text), 0644); err != nil {
			dialog.ShowError(err, w)
			return
		}
		dialog.ShowInformation("Saved", "File saved to:\n"+savePath, w)
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

		// Poll until all output images are written (mod times change) or
		// the process exits, whichever comes first.
		diffReady := false
		targetReady := false
		withPathReady := false
		for !diffReady || !targetReady || !withPathReady {
			time.Sleep(500 * time.Millisecond)
			if !diffReady {
				if info, err := os.Stat(outputPath); err == nil && info.ModTime().After(prevModTime) {
					diffReady = true
				}
			}
			if !targetReady {
				if info, err := os.Stat(targetPath); err == nil && info.ModTime().After(prevTargetModTime) {
					targetReady = true
				}
			}
			if !withPathReady {
				if info, err := os.Stat(withPathPath); err == nil && info.ModTime().After(prevWithPathModTime) {
					withPathReady = true
				}
			}
			// Also stop polling if the process has already exited.
			if cmd.ProcessState != nil {
				break
			}
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
			displayImage(imagePanel, *diffImagePath)
			onImageReady()
		})

		// Let the process finish in the background (plot windows still open).
		cmd.Wait()
		*diffCmd = nil
	}()
}

// displayImage replaces the contents of a panel container with the image at
// the given path, scaled to fill the available space.
func displayImage(panel *fyne.Container, path string) {
	img := canvas.NewImageFromFile(path)
	img.FillMode = canvas.ImageFillContain
	panel.Layout = layout.NewStackLayout()
	panel.Objects = []fyne.CanvasObject{img}
	panel.Refresh()
}

// drawPathLine loads the image at imagePath, draws a 4-pixel wide red
// horizontal line at the vertical center plus offset rows, draws green
// vertical lines at edge positions, and displays the result in the panel.
func drawPathLine(panel *fyne.Container, imagePath string, offset int, edges []int) {
	rotatedPath := filepath.Join(filepath.Dir(imagePath), "diffractionImage8bitRotated.png")
	f, err := os.Open(rotatedPath)
	if err != nil {
		return
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return
	}

	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	// Draw observation path as a 4-pixel red horizontal line.
	lineY := bounds.Min.Y + bounds.Dy()/2 + offset
	red := color.RGBA{R: 255, A: 255}
	for dy := 0; dy < 4; dy++ {
		y := lineY + dy
		if y < bounds.Min.Y || y >= bounds.Max.Y {
			continue
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dst.Set(x, y, red)
		}
	}

	// Draw edge positions as full-height green vertical lines, 3 pixels wide.
	green := color.RGBA{G: 255, A: 255}
	for _, ex := range edges {
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

	img := canvas.NewImageFromImage(dst)
	img.FillMode = canvas.ImageFillContain
	panel.Layout = layout.NewStackLayout()
	panel.Objects = []fyne.CanvasObject{img}
	panel.Refresh()
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
// given path offset, or nil if the shadow image cannot be read.
func findEdgesForOffset(appDir string, offset int) []int {
	shadowPath := filepath.Join(appDir, "geometricShadowRotated.png")
	edges, _ := report.FindEdges(shadowPath, offset)
	return edges
}

// plotRowLightCurve extracts intensity values from the center+offset row of
// targetImage16bit.png and plots them with the provided edge markers as a
// light curve in the given panel.
func plotRowLightCurve(w fyne.Window, panel *fyne.Container, appDir string, offset int, edges []int, yMax, kmPerPixel float64, exposurePixels int) {
	targetPath := filepath.Join(appDir, "targetImage16bitRotated.png")
	values, err := report.ExtractRow(targetPath, offset)
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot extract light curve: %w", err), w)
		return
	}
	var integrated []float64
	if exposurePixels > 1 {
		integrated = report.ApplyExposure(values, exposurePixels)
	}
	plotImg := report.PlotLightCurve(values, 1200, 400, edges, yMax, kmPerPixel, integrated)
	img := canvas.NewImageFromImage(plotImg)
	img.FillMode = canvas.ImageFillContain
	panel.Layout = layout.NewStackLayout()
	panel.Objects = []fyne.CanvasObject{img}
	panel.Refresh()
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
