module github.com/solworktech/trecs-notes

go 1.26.0

// Third-party: a tray icon, a global hotkey, and a pure-Go (no cgo) SQLite
// driver. The replace lines at the bottom point golang.org/x and
// golang.design vanity paths at their github.com mirrors; they exist only
// because the build sandbox could reach github.com and nothing else. On a
// normal machine they can be deleted (then `go mod tidy`).
require golang.design/x/hotkey v0.4.1

require (
	fyne.io/systray v1.12.2
	golang.org/x/sys v0.49.0 // indirect
)

require (
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jezek/xgb v1.3.1
)

require (
	github.com/gdamore/tcell/v2 v2.13.10
	github.com/jessp01/gohighlight v0.21.4
	github.com/rivo/tview v0.42.0
	github.com/yuin/goldmark v1.8.7
	github.com/zalando/go-keyring v0.2.8
	modernc.org/sqlite v1.62.0
)

require (
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/term v0.37.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace golang.design/x/hotkey => github.com/golang-design/hotkey v0.4.1

replace golang.org/x/sys => github.com/golang/sys v0.33.0

replace golang.org/x/mod => github.com/golang/mod v0.22.0

replace golang.org/x/tools => github.com/golang/tools v0.29.0

replace golang.org/x/text => github.com/golang/text v0.21.0

replace golang.org/x/net => github.com/golang/net v0.34.0

replace golang.org/x/sync => github.com/golang/sync v0.10.0

replace golang.org/x/crypto => github.com/golang/crypto v0.32.0

replace golang.org/x/exp => github.com/golang/exp v0.0.0-20250106191152-7588d65b2ba8

replace gopkg.in/Knetic/govaluate.v3 => github.com/Knetic/govaluate v3.0.0+incompatible

replace golang.design/x/mainthread => github.com/golang-design/mainthread v0.3.0

replace fyne.io/systray => github.com/fyne-io/systray v1.12.2
