// Package ui constructs and manages the DiffractionDemo Fyne application window.
package ui

import (
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	prefHSplitOffset = "hsplit_offset"
	prefVSplitOffset = "vsplit_offset"

	defaultWidth  float64 = 1200
	defaultHeight float64 = 800
	defaultHSplit float64 = 0.4
	defaultVSplit float64 = 0.55
)

// Run creates the application window and enters the Fyne event loop.
func Run() {
	a := app.NewWithID(appID)
	w := a.NewWindow("DiffractionDemo")

	paramsEntry, paramsScroll := buildParamsPanel("Open a parameters file to view its contents...")
	imagePanel := buildImagePanel("No image loaded — run diffraction to generate one.")
	lightCurvePanel := buildImagePanel("No light curve — run diffraction to generate one.")

	var sourceDir string
	var paramsFilePath string

	saveFileBtn := widget.NewButton("Save", func() {
		saveParameters(w, paramsEntry, paramsFilePath)
	})
	saveFileBtn.Disable()

	saveAsBtn := widget.NewButton("Save As", func() {
		saveParametersAs(w, paramsEntry, sourceDir)
	})
	saveAsBtn.Disable()

	openBtn := widget.NewButton("Open Parameters File", func() {
		openParametersFile(w, paramsEntry, &sourceDir, &paramsFilePath, saveFileBtn, saveAsBtn)
	})

	statusLabel := widget.NewLabel("")
	runBtn := widget.NewButton("Run Diffraction", nil)
	runBtn.OnTapped = func() {
		runDiffraction(w, runBtn, statusLabel, paramsFilePath, imagePanel)
	}

	toolbar := container.NewHBox(openBtn, saveFileBtn, saveAsBtn, runBtn, statusLabel)
	hSplit := container.NewHSplit(paramsScroll, imagePanel)
	vSplit := container.NewVSplit(hSplit, lightCurvePanel)

	restorePreferences(a.Preferences(), w, hSplit, vSplit)

	w.SetContent(container.NewBorder(toolbar, nil, nil, nil, vSplit))
	w.SetOnClosed(func() {
		savePreferences(a.Preferences(), w, hSplit, vSplit)
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
func openParametersFile(w fyne.Window, entry *widget.Entry, sourceDir *string, paramsFilePath *string, saveFileBtn, saveAsBtn *widget.Button) {
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
		saveFileBtn.Enable()
		saveAsBtn.Enable()
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
// diffractionImage8bit.png is displayed in the provided image panel.
func runDiffraction(w fyne.Window, btn *widget.Button, status *widget.Label, paramsFile string, imagePanel *fyne.Container) {
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

	go func() {
		cmd := exec.Command(diffExe, paramsFile, "False")
		cmd.Dir = appDir
		output, err := cmd.CombinedOutput()
		fyne.Do(func() {
			if err != nil {
				btn.Enable()
				status.SetText("Failed")
				dialog.ShowError(fmt.Errorf("IOTAdiffraction failed: %w\n%s", err, string(output)), w)
				return
			}
			btn.Enable()
			status.SetText("Completed")
			displayImage(imagePanel, filepath.Join(appDir, "diffractionImage8bit.png"))
			showResultsWindow(w, appDir)
		})
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

// restorePreferences reads saved window size and splitter offsets from
// persistent preferences and applies them.
func restorePreferences(prefs fyne.Preferences, w fyne.Window, hSplit, vSplit *container.Split) {
	width := prefs.FloatWithFallback(prefWindowWidth, defaultWidth)
	height := prefs.FloatWithFallback(prefWindowHeight, defaultHeight)
	w.Resize(fyne.NewSize(float32(width), float32(height)))

	hSplit.SetOffset(prefs.FloatWithFallback(prefHSplitOffset, defaultHSplit))
	vSplit.SetOffset(prefs.FloatWithFallback(prefVSplitOffset, defaultVSplit))
}

// savePreferences writes the current window size and splitter offsets to
// persistent preferences.
func savePreferences(prefs fyne.Preferences, w fyne.Window, hSplit, vSplit *container.Split) {
	size := w.Canvas().Size()
	prefs.SetFloat(prefWindowWidth, float64(size.Width))
	prefs.SetFloat(prefWindowHeight, float64(size.Height))
	prefs.SetFloat(prefHSplitOffset, hSplit.Offset)
	prefs.SetFloat(prefVSplitOffset, vSplit.Offset)
}
