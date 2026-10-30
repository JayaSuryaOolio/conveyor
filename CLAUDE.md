# Conveyor

## Gotchas

- Wails v2 darwin `runtime.MessageDialog` returns the clicked button's text only when `options.Buttons` is set; with none it returns `""`. Always pass explicit `Buttons` (and `DefaultButton`/`CancelButton`) when comparing the result, or a confirm silently reads as "cancelled" (CV-67).
