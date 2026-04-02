# Decisions Log

## 2026-04-01: App ID and Preferences Storage

**Decision:** Use `app.NewWithID("com.iota.diffractiondemo")` for the Fyne application.

**Why:** Fyne's `Preferences` API requires a stable app ID to locate the preferences file on disk. Without an ID, preferences are not persisted across sessions. The reverse-domain format follows Fyne convention.

## 2026-04-01: Three-Panel Layout with Splitters

**Decision:** Use `container.NewVSplit` (top/bottom) wrapping a `container.NewHSplit` (left/right) for the three-panel layout, rather than a grid or manual positioning.

**Why:** Splitters let the user resize panels to match their display and workflow. The nested split approach (HSplit inside VSplit) maps directly to the requirement: two panels on top, one on the bottom. Splitter offsets are persisted to preferences so the layout survives restarts.

## 2026-04-01: Disabled MultiLineEntry for Parameters Display

**Decision:** Display the parameters file in a `widget.NewMultiLineEntry()` set to `Disable()` with monospace text style.

**Why:** A disabled entry provides scrolling, text selection, and monospace rendering out of the box. It will be straightforward to enable for editing in Phase 1 item 3. The slight visual dimming from `Disable()` is acceptable as it signals read-only state.

## 2026-04-01: Enable/Disable Entry on File Load

**Decision:** Briefly enable the entry before calling `SetText`, then disable it again.

**Why:** Fyne's `MultiLineEntry.SetText()` is a no-op when the widget is disabled. The enable-set-disable pattern ensures the loaded text is actually displayed while keeping the widget read-only.
