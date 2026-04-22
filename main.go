// DiffractionDemo displays theoretical light curves incorporating diffraction,
// finite star diameter, limb darkening, and camera exposure time effects.
package main

import "DiffractionDemo/internal/ui"

const VERSION = "1.0.4"

func main() {
	ui.Version = VERSION
	ui.Run()
}
