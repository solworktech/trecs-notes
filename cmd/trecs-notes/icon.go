package main

import _ "embed"

// trayIconPNG is a placeholder tray icon (a plain teal square) so the tray
// app runs end to end without needing a real asset yet. Swap
// assets/icon.png for a proper design whenever one exists - nothing else
// changes.
//
//go:embed assets/icon.png
var trayIconPNG []byte
