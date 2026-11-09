//go:build linux

package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Linux has two things the other platforms don't, and capture leans on both:
//
//   - The PRIMARY selection. Whatever is highlighted in nearly any GTK, Qt,
//     Firefox or terminal window is already in PRIMARY, with no keystroke
//     needed. Reading it is why a terminal (wezterm, xterm...) works at all:
//     the old approach of sending Ctrl+C would have interrupted whatever
//     was running in it.
//   - No single way to ask "which window is focused". On X11 it's xdotool;
//     on Wayland it depends entirely on the compositor, so each of the ones
//     that offer anything is tried by what the session says it is.
//
// Anything that can't be read is left blank and explained in
// Snapshot.Warnings (main logs them; `trecs-notes diagnose` prints them).
func capture(opts Options) Snapshot {
	var s Snapshot
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""

	app, title, err := focusedWindow(wayland)
	if err != nil {
		s.Partial = true
		s.Warnings = append(s.Warnings, "focused window: "+err.Error())
	}
	s.AppName, s.WindowTitle = app, title

	text, warn := selection(wayland, app, opts)
	if warn != "" {
		s.Partial = true
		s.Warnings = append(s.Warnings, warn)
	}
	s.SelectedText = text
	return s
}

// ---- focused window --------------------------------------------------------

func focusedWindow(wayland bool) (app, title string, err error) {
	var tried []string
	if wayland {
		for _, p := range waylandProviders() {
			a, t, e := p.fn()
			if e == nil && (a != "" || t != "") {
				return a, t, nil
			}
			tried = append(tried, fmt.Sprintf("%s: %v", p.name, errOrEmpty(e)))
		}
		if len(tried) == 0 {
			tried = append(tried, "no known way to ask this Wayland compositor ("+desktopName()+") for the focused window")
		}
		// XWayland: only helps if the focused window is an X11 client.
	}
	a, t, e := x11Native()
	if e == nil && (a != "" || t != "") {
		return a, t, nil
	}
	tried = append(tried, fmt.Sprintf("X11: %v", errOrEmpty(e)))
	a, t, e = x11Window() // last resort; old xdotool lacks getwindowclassname
	if e == nil && (a != "" || t != "") {
		return a, t, nil
	}
	tried = append(tried, fmt.Sprintf("xdotool: %v", errOrEmpty(e)))
	return "", "", fmt.Errorf("%s", strings.Join(tried, "; "))
}

func errOrEmpty(e error) string {
	if e == nil {
		return "nothing returned"
	}
	return e.Error()
}

func desktopName() string {
	return strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP") + " " + os.Getenv("XDG_SESSION_TYPE"))
}

type provider struct {
	name string
	fn   func() (app, title string, err error)
}

// waylandProviders lists, in order, the compositor-specific ways to learn
// the focused window that this session could plausibly support.
func waylandProviders() []provider {
	var ps []provider
	desktop := strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP"))
	if os.Getenv("SWAYSOCK") != "" {
		ps = append(ps, provider{"sway", swayWindow})
	}
	if os.Getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		ps = append(ps, provider{"hyprland", hyprlandWindow})
	}
	if os.Getenv("NIRI_SOCKET") != "" {
		ps = append(ps, provider{"niri", niriWindow})
	}
	if strings.Contains(desktop, "kde") {
		ps = append(ps, provider{"kde (kdotool)", kdeWindow})
	}
	if strings.Contains(desktop, "gnome") {
		ps = append(ps, provider{"gnome (Focused Window D-Bus extension)", gnomeFocusedWindowExt},
			provider{"gnome (Window Calls extension)", gnomeWindowCallsExt})
	}
	return ps
}

func swayWindow() (string, string, error) {
	out, err := runOut("swaymsg", "-t", "get_tree")
	if err != nil {
		return "", "", err
	}
	var tree map[string]any
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		return "", "", err
	}
	var find func(n map[string]any) (string, string, bool)
	find = func(n map[string]any) (string, string, bool) {
		if f, _ := n["focused"].(bool); f {
			app, _ := n["app_id"].(string)
			if app == "" { // an XWayland client
				if wp, ok := n["window_properties"].(map[string]any); ok {
					app, _ = wp["class"].(string)
				}
			}
			name, _ := n["name"].(string)
			return app, name, true
		}
		for _, k := range []string{"nodes", "floating_nodes"} {
			kids, _ := n[k].([]any)
			for _, c := range kids {
				if cm, ok := c.(map[string]any); ok {
					if a, t, ok := find(cm); ok {
						return a, t, true
					}
				}
			}
		}
		return "", "", false
	}
	a, t, _ := find(tree)
	return a, t, nil
}

func hyprlandWindow() (string, string, error) {
	out, err := runOut("hyprctl", "activewindow", "-j")
	if err != nil {
		return "", "", err
	}
	var w struct {
		Class string `json:"class"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		return "", "", err
	}
	return w.Class, w.Title, nil
}

func niriWindow() (string, string, error) {
	out, err := runOut("niri", "msg", "--json", "focused-window")
	if err != nil {
		return "", "", err
	}
	var w struct {
		AppID string `json:"app_id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &w); err != nil {
		return "", "", err
	}
	return w.AppID, w.Title, nil
}

// kdeWindow needs kdotool (https://github.com/jinliu/kdotool), which drives
// KWin's scripting interface; KDE has no stable built-in CLI for this.
func kdeWindow() (string, string, error) {
	id, err := runOut("kdotool", "getactivewindow")
	if err != nil {
		return "", "", err
	}
	id = strings.TrimSpace(id)
	title, _ := runOut("kdotool", "getwindowname", id)
	class, _ := runOut("kdotool", "getwindowclassname", id)
	return strings.TrimSpace(class), strings.TrimSpace(title), nil
}

// GNOME on Wayland will not tell a plain client which window is focused
// (org.gnome.Shell.Eval is locked down since GNOME 41). The only routes are
// shell extensions that re-export it over D-Bus; these two are the common
// ones. UNTESTED here - no GNOME session was available to try them on.
func gnomeFocusedWindowExt() (string, string, error) {
	out, err := runOut("gdbus", "call", "--session", "--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/shell/extensions/FocusedWindow",
		"--method", "org.gnome.shell.extensions.FocusedWindow.Get")
	if err != nil {
		return "", "", err
	}
	return parseGdbusJSON(out)
}

func gnomeWindowCallsExt() (string, string, error) {
	out, err := runOut("gdbus", "call", "--session", "--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell/Extensions/Windows",
		"--method", "org.gnome.Shell.Extensions.Windows.List")
	if err != nil {
		return "", "", err
	}
	j, err := gdbusString(out)
	if err != nil {
		return "", "", err
	}
	var wins []struct {
		Focus   bool   `json:"focus"`
		WMClass string `json:"wm_class"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal([]byte(j), &wins); err != nil {
		return "", "", err
	}
	for _, w := range wins {
		if w.Focus {
			return w.WMClass, w.Title, nil
		}
	}
	return "", "", fmt.Errorf("no focused window in the list")
}

// gdbusString unwraps gdbus's text form "('<json>',)" into the JSON string.
func gdbusString(out string) (string, error) {
	out = strings.TrimSpace(out)
	i, j := strings.Index(out, "'"), strings.LastIndex(out, "'")
	if i < 0 || j <= i {
		return "", fmt.Errorf("unexpected gdbus output %q", out)
	}
	return strings.ReplaceAll(out[i+1:j], `\'`, `'`), nil
}

func parseGdbusJSON(out string) (string, string, error) {
	j, err := gdbusString(out)
	if err != nil {
		return "", "", err
	}
	var w struct {
		WMClass string `json:"wm_class"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal([]byte(j), &w); err != nil {
		return "", "", err
	}
	return w.WMClass, w.Title, nil
}

func x11Window() (string, string, error) {
	if !commandExists("xdotool") {
		return "", "", fmt.Errorf("xdotool is not installed")
	}
	id, err := runOut("xdotool", "getactivewindow")
	if err != nil {
		return "", "", err
	}
	id = strings.TrimSpace(id)
	title, _ := runOut("xdotool", "getwindowname", id)
	class, _ := runOut("xdotool", "getwindowclassname", id)
	return strings.TrimSpace(class), strings.TrimSpace(title), nil
}

// ---- selection -------------------------------------------------------------

func selection(wayland bool, app string, opts Options) (text, warning string) {
	if t, ok := readPrimary(wayland); ok && t != "" {
		return t, ""
	}
	// Nothing in PRIMARY: nothing is highlighted, or the application does
	// not set it.
	if !opts.CopyFallback {
		return "", "nothing in the PRIMARY selection (the copy fallback is off in Settings)"
	}
	if IsTerminal(app) {
		return "", "nothing in the PRIMARY selection, and not sending Ctrl+C to a terminal"
	}
	return copyFallback(wayland)
}

// copyFallback sends Ctrl+C, trusts the result only if the clipboard really
// changed, and puts the previous clipboard text back. It refuses to start
// if the clipboard holds anything that is not plain text, because that
// could not be restored.
func copyFallback(wayland bool) (text, warning string) {
	targets, known := clipboardTargets(wayland)
	if known && !textOnly(targets) {
		return "", "the clipboard holds something other than text, which cannot be put back after a copy: not copying"
	}
	before, _ := readClipboard(wayland)
	if err := sendCopy(wayland); err != nil {
		return "", "could not send Ctrl+C: " + err.Error()
	}
	var after string
	changed := false
	for i := 0; i < 20 && !changed; i++ { // up to ~400 ms for the application to answer
		time.Sleep(20 * time.Millisecond)
		after, _ = readClipboard(wayland)
		changed = after != "" && after != before
	}
	if !changed {
		return "", "nothing selected (the copy did not change the clipboard)"
	}
	if err := writeClipboard(wayland, before); err != nil {
		return after, "the selection was copied but the previous clipboard text could not be restored: " + err.Error()
	}
	return after, ""
}

// clipboardTargets lists the formats the clipboard offers; known is false when
// the available tools cannot say (xsel), in which case the clipboard is
// assumed to be text.
func clipboardTargets(wayland bool) (targets []string, known bool) {
	var out string
	var err error
	switch {
	case wayland && commandExists("wl-paste"):
		out, err = runOut("wl-paste", "--list-types")
	case commandExists("xclip"):
		out, err = runOut("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	default:
		return nil, false
	}
	if err != nil {
		return nil, true // empty clipboard: the tools exit with an error
	}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			targets = append(targets, l)
		}
	}
	return targets, true
}

// textOnly reports whether every format on offer is plain text (or X's own
// bookkeeping targets).
func textOnly(targets []string) bool {
	for _, t := range targets {
		switch {
		case t == "TARGETS", t == "TIMESTAMP", t == "MULTIPLE", t == "SAVE_TARGETS", t == "DELETE",
			t == "STRING", t == "UTF8_STRING", t == "TEXT", t == "COMPOUND_TEXT",
			strings.HasPrefix(t, "text/plain"), t == "text/plain;charset=utf-8":
		default:
			return false
		}
	}
	return true
}

// writeClipboard puts text on the clipboard; empty text clears it.
func writeClipboard(wayland bool, text string) error {
	try := func(args ...string) error {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(text) // output left unset: xclip forks and must not hold our pipes
		return cmd.Run()
	}
	switch {
	case wayland && commandExists("wl-copy"):
		if text == "" {
			return try("wl-copy", "--clear")
		}
		return try("wl-copy")
	case commandExists("xclip"):
		return try("xclip", "-selection", "clipboard", "-i")
	case commandExists("xsel"):
		if text == "" {
			return try("xsel", "--clipboard", "--clear")
		}
		return try("xsel", "--clipboard", "--input")
	}
	return fmt.Errorf("no tool to write the clipboard (xclip, xsel or wl-copy)")
}

func readPrimary(wayland bool) (string, bool) {
	if wayland && commandExists("wl-paste") {
		if out, err := runOut("wl-paste", "--primary", "--no-newline"); err == nil {
			return out, true
		}
	}
	for _, try := range [][]string{
		{"xclip", "-selection", "primary", "-o"},
		{"xsel", "--primary", "--output"},
	} {
		if commandExists(try[0]) {
			if out, err := runOut(try[0], try[1:]...); err == nil {
				return out, true
			}
		}
	}
	return "", false
}

func readClipboard(wayland bool) (string, bool) {
	if wayland && commandExists("wl-paste") {
		if out, err := runOut("wl-paste", "--no-newline"); err == nil {
			return out, true
		}
	}
	for _, try := range [][]string{
		{"xclip", "-selection", "clipboard", "-o"},
		{"xsel", "--clipboard", "--output"},
	} {
		if commandExists(try[0]) {
			if out, err := runOut(try[0], try[1:]...); err == nil {
				return out, true
			}
		}
	}
	return "", false
}

func sendCopy(wayland bool) error {
	if wayland && commandExists("wtype") {
		return run("wtype", "-M", "ctrl", "-k", "c", "-m", "ctrl")
	}
	if commandExists("xdotool") {
		return run("xdotool", "key", "--clearmodifiers", "ctrl+c")
	}
	return fmt.Errorf("neither wtype nor xdotool is installed")
}

// ---- helpers ----------------------------------------------------------------

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func runOut(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}
