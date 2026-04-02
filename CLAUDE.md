### Project: [DiffractionDemo]

This is a desktop application for showing theoretical light curves that incorporate diffraction effects, finite star diameter effects (including limb darkening), and camera exposure time effects.

Built with Go and Fyne.

---

## Stack

- **Language:** Go (latest stable)
- **UI Framework:** Fyne v2
- **Build target:** Cross-platform desktop (Windows, macOS, Linux)
- **Test runner:** `go test ./...`

---

## Architecture

```
main.go                 # Entry point, app bootstrap only
internal/
  ui/                   # Fyne widgets, layouts, screens
  report/               # Output formatting, png generation
assets/                 # Icons, fonts, embedded resources
tests/                  # Integration tests
```

**Key rule:** Business logic must have zero imports from `fyne.io`. UI and logic are strictly separated.
This makes logic testable without spinning up a Fyne app.

---

## Coding Standards

- Follow standard Go conventions (`gofmt`, `go vet` clean at all times)
- Exported types and functions must have doc comments
- Errors are returned, never swallowed — no bare `_ = err`
- No `panic` in library code; reserve for true initialization failures in `main`
- Prefer explicit over clever — this codebase needs to be readable at 11pm
  during a live occultation event
- Use `context.Context` for any operation that could block or time out
- Constants over magic numbers.

---

## Fyne UI Guidelines

- All UI construction happens on the main goroutine
- Use `binding` package for reactive data where practical — avoids manual `Refresh()` calls
- Custom widgets only when Fyne's built-ins genuinely can't do the job
- Layouts: prefer `container.NewVBox` / `HBox` / `Border` over manual
  positioning; use `container.New(layout.NewGridLayout(...))` for tabular data
- Window sizing: set a sensible `SetFixedSize` or `Resize` default;
  don't leave it to chance
- Long-running operations (file parsing, GPS sync) must run in a goroutine
  and report progress via `widget.ProgressBar` or status label — never block
  the UI thread
- Error dialogs: use `dialog.ShowError(err, window)` consistently;
  don't log-and-ignore UI errors

---

## Testing Standards

When writing tests:

- Test **behavior and outcomes**, not implementation details
- Every test must have a one-line comment explaining what it proves
- Include at least one **edge case** and one **failure/error case** per function
- Do not mock internal functions — only mock file I/O, hardware, and
  network interfaces
- Do not write tests that can never fail (no `assert obj != nil` as the
  only assertion)
- Prefer `t.Errorf` with descriptive messages over bare `t.Fatal`
- Table-driven tests for functions with multiple input/output cases
- Fyne UI code: use `test.NewApp()` and `test.NewWindow()` from `fyne.io/fyne/v2/test` — never spin up a real window in tests

**Priority test targets:**

- Capturing user data input errors

---

## Decisions Log

See `DECISIONS.md` for the *why* behind non-obvious architectural choices.
When a significant decision is made during a session, add an entry there.

---

## Phases

### Phase 1 — Foundation

- [x] 
    1. Project scaffold: see Architecture section.
- [x] 
    2. Build a Fyne app that can read and display a parameters file (JSON 5 format) from a file selection dialog. Allow for two plotting areas to be used in Phase 2, one for an image, and another for a light curve plot. Use three panels: 2 at the top and 1 at the bottom. The top left panel should display the parameters file. The top right panel will be used in phase two to display an image. The bottom panel will be used to display light curve plots. Use splitters to allow for resizing and save window position and sizing and splitter setting to preferences and use this when restarting the app.
- [x] 
    3. Add ability to edit a selected parameters file and write it to a folder with a new name.

### Phase 2 — Computing and displaying a light curve

- [ ] 
    1. Add a button that when clicked, will run the the external application IOTAdiffraction.exe found in the app directory.
- [ ] 
    2. Detect the completion of IOTAdiffraction and display the diffractionImage8bit.png (placed in the app directory by IOTAdiffraction.exe) in the right hand top panel of the GUI.
- [ ] 
    3. Use the middle row of targetImage16bit.png to extract a light curve and plot it in the bottom panel.
- [ ] 
    4. Add an entry box to allow the user to specify an observation path offset in km and recompute and redisplay the light curve


---

## Session Startup Checklist

When beginning a new Claude session, paste this file plus any relevant
excerpt from `DECISIONS.md`. State which phase you are working on and
what the immediate task is.

---

## What to Always Do

- Show test code in chat before executing it
- Save generated test files to the repo under `tests/` or alongside
  the package being tested
- After writing tests, review them and flag any that could never fail
- Add a `DECISIONS.md` entry whenever a non-obvious choice is made
- Keep UI and logic in separate packages — call this out if a proposed
  change would violate the boundary