# Trecs Notes

Select some text in any application, press a hotkey, and keep it.

Trecs Notes is a small tray app. The hotkey (`Ctrl+Alt+Space` by default) opens a form holding the text you had selected, the name of the application it came from and that window's title. Add tags, press Save, and the note is stored in a local SQLite database. If you use a [Trecs](https://github.com/solworktech/trecs) server, notes can also be sent there, so they follow you across machines.

It is the companion to [`trecs`](https://github.com/solworktech/trecs), which records terminal sessions: `trecs` keeps the session, Trecs Notes keeps the snippet - a command, an error message, a paragraph from a page.

## Contents

- [How it works](#how-it-works)
- [Install](#install)
- [Using it](#using-it)
- [Settings](#settings)
- [Syncing to a Trecs server](#syncing-to-a-trecs-server)
- [Where things are stored](#where-things-are-stored)
- [Platform status](#platform-status)
- [Building from source](#building-from-source)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## How it works

No operating system offers a "text was selected" event or a menu entry for selected text in every application, so Trecs Notes uses a global hotkey and asks the system for the selection when you press it. Each platform is asked in the way it offers, and **no keystroke is sent into your windows** unless you turn that on in Settings:

- **Linux:** the `PRIMARY` selection - whatever is highlighted in nearly any GTK, Qt, Firefox or terminal window, with no copy needed - read through `xclip`, `xsel` or `wl-clipboard`. The application name and window title come straight from the X server.
- **Windows:** UI Automation's text interface, the one screen readers use, asked for the focused element's selection.
- **macOS:** the Accessibility API's `AXSelectedText` of the focused element, through System Events.

None of these works in every application: some programs do not expose their selection. For those there is an opt-in setting, **try copying it**, which sends Ctrl+C (Cmd+C on macOS) as a last resort. It is never sent to a terminal, where Ctrl+C means "interrupt", it only trusts the result if the clipboard really changed, and it puts your previous clipboard text back. If your clipboard holds anything but plain text (an image, files, formatted text) it does not try at all, since that could not be put back faithfully.

Notes are written to a local database first, always. Sending to a server is optional and a failure there never loses a note.

## Install

Packages for Linux (`amd64` and `arm64`) are on the [releases page](https://github.com/solworktech/trecs-notes/releases): `.deb`, `.rpm` and Arch packages, and a `.tar.gz` holding the binary.

```sh
sudo apt install ./trecs-notes_*_amd64.deb      # Debian, Ubuntu
sudo dnf install ./trecs-notes-*.x86_64.rpm     # Fedora, RHEL and clones
sudo pacman -U trecs-notes-*.pkg.tar.zst        # Arch
```

The packages depend on GTK 3 and X11, which every desktop already has, and on one of `xclip`, `xsel` or `wl-clipboard` for reading the selection; the package manager installs whatever is missing. `xdotool` is recommended as a fallback for reading window titles.

The binaries are built on Ubuntu 22.04, so they run on Debian 12, Ubuntu 22.04, RHEL 9 and anything newer.

Start it from your application menu, or run `trecs-notes`. To have it start at login, add it to your desktop's autostart.

### The tray icon

The tray icon uses the StatusNotifier (AppIndicator) protocol. KDE, XFCE, Cinnamon, MATE and most others show it directly. Stock GNOME does not: install the *AppIndicator and KStatusNotifierItem Support* extension (Ubuntu ships it enabled). Without a tray the hotkey still works.

## Using it

1. Select text in any window.
2. Press the hotkey.
3. Check the fields, add tags (space or comma separated; start typing for suggestions from tags you already use) and press **Save**.

### Quick save

A second hotkey (`Ctrl+Alt+Shift+Space` by default) stores the selected text at once: no form, so no tags. It uses the same capture as the first hotkey. If no text is selected, nothing happens and nothing is stored. Set it in **Settings** (**Quick save**); press Backspace in that box to turn it off. Without a global hotkey (Wayland), bind `trecs-notes quick` instead. Quick saves are synced like any other note when syncing is on.

The tray menu has **Settings**, **Browse notes** (not built yet) and **Quit**. Quitting also closes an open form.

### Raw and Markdown

The note area has two tabs. **Raw** is the text you edit; it is coloured as you type, using the [gohighlight](https://github.com/jessp01/gohighlight) syntax definitions. The language is guessed from the window title (`main.go - editor`), a `#!` line or the content, and the **Syntax** button at the right of the tabs shows the guess ("Auto (go)"). If the note starts with a code fence (` ```go `), that language is selected. Click the box to search the list and pick another language, or *None*, to override it. **Markdown** shows the same text rendered: headings, bold, italic, strikethrough, inline code, links, quotes, lists, task lists, tables and fenced code blocks (coloured by their language tag).

The text is saved exactly as written; the tabs only change how it is displayed. The tabs are available in the GTK form on Linux. The macOS, Windows and `yad`/`zenity` forms still show a plain text box.

The syntax definitions in `internal/render/syntax_files` are a copy of gohighlight's; see the README there for how to update them.

Tags are lowercase letters, digits and single hyphens, 2 to 32 characters, at most 10 per note - the same rule `trecs` uses for recordings.

## Settings

| Setting | What it does |
|---|---|
| Hotkey | Click the field and press the combination you want. It needs at least one modifier. A combination another program already holds is refused with a message. Changing it restarts the app. |
| Quick save | The hotkey that stores the selection without showing the form. Set the same way; Backspace empties it, which turns it off. It has to differ from the Hotkey. |
| Form size | The capture form's width and height, in pixels. |
| Try copying | For applications that do not report their selection, send the copy shortcut as a last resort. Off by default; see [How it works](#how-it-works). |
| Also save notes to a trecs server | Turns on [syncing](#syncing-to-a-trecs-server). |
| Server | The server's address. Left alone, it is `https://trecs.run`. |
| Email, Password | Your account on that server. |
| Log in / Log out | Logs in now, so you see right away whether it worked. |
| Not on the server yet | How many notes have not been sent, with a button to send them now. |
| Database | Where the local database is, with a button to open its folder. |

The Settings window exists on Linux for now. On the other platforms, **Settings** opens the folder holding `config.json`, which you can edit.

## Syncing to a Trecs server

With the option on, every saved note is also sent to the server you are logged in to, under your account, with its tags and the time you took it. Notes taken while offline, or while the server was unreachable, stay in the local database marked as unsent; **Send them now** in Settings (or the next successful sync) sends them.

The login works like `trecs login`. The email is kept in `config.json`. The password is kept in the system keyring (the macOS Keychain, the Windows Credential Manager, or the Secret Service - GNOME Keyring, KWallet - on Linux). If there is no keyring, it is kept in a file in the data folder that only you can read, **as plain text**; the Settings window tells you when that happens. When a session lapses, the stored login renews it. **Log out** removes the stored password.

Syncing needs a server that has the notes endpoint (`/v1/notes`).

## Where things are stored

Everything is under `$XDG_CONFIG_HOME/trecs/notes/` (or `~/trecs/notes/` when `XDG_CONFIG_HOME` is not set), next to where `trecs` keeps its recordings:

| File | Contents |
|---|---|
| `notes.db` | The notes (SQLite). |
| `config.json` | Hotkey, form size, sync on or off, server, email. |
| `session` | The login session cookie for the server. |
| `credentials.json` | Only if there is no system keyring: your login, readable by you only. |

## Platform status

Linux on X11 is where this is developed and tested. Be aware of the rest:

- **Wayland:** a program cannot register a global hotkey on its own; the protocol does not allow it. Bind the command `trecs-notes capture` (and `trecs-notes quick` for the quick save) to a key in your compositor's or desktop's keyboard settings instead. Reading the application name and window title is implemented for sway, Hyprland, niri, KDE (with `kdotool`) and GNOME (through an extension), but only the X11 path has been tested.
- **macOS and Windows:** the code is written - hotkey, selection capture, the capture form - but it has not been run on those systems yet. The CI builds and tests it on both, which is how problems there will show up. Reports are welcome. Chromium-based browsers and Electron applications only expose their selection once an assistive tool asks for it, so the first capture in one may come back empty. On macOS the first capture asks for the Accessibility permission (System Settings > Privacy & Security > Accessibility).
- **Settings and Browse notes** are Linux-only for now (see above); Browse notes is not built on any platform yet.

## Building from source

You need Go (the version in `go.mod`) and a C compiler. The Linux build uses GTK 3 and X11 headers:

```sh
sudo apt install libgtk-3-dev libx11-dev       # Debian, Ubuntu
sudo dnf install gtk3-devel libX11-devel       # Fedora
```

```sh
go build -o trecs-notes ./cmd/trecs-notes
```

Without the GTK headers, build with `-tags nogtk`: the form then falls back to `yad` or `zenity`, which has to be installed at run time and looks plainer.

The server used when none is configured can be set at build time, as in `trecs`:

```sh
go build -ldflags "-X main.defaultServer=https://trecs.run" -o trecs-notes ./cmd/trecs-notes
```

Run the tests with `go test ./...` (an X server is needed for the capture tests; `xvfb-run -a go test ./...` provides one). Packages are made with [GoReleaser](https://goreleaser.com): `goreleaser release --snapshot --clean` builds them locally. A release is made by pushing a tag; see `.github/workflows/release.yml`.

## Troubleshooting

`trecs-notes diagnose` prints your session type, which helper tools it found, and - after a few seconds, in which you select some text in another window - exactly what a capture read: the application, the title and the text. It also prints a warning for anything it could not read. Run it from a terminal and attach the output to any bug report.

- **The hotkey does nothing:** another program may hold the combination (the app says so when you set it in Settings), or you are on Wayland - see above.
- **Text from an earlier selection appears:** on Linux the `PRIMARY` selection holds the last thing highlighted anywhere, so pressing the hotkey in a window with nothing selected offers whatever you highlighted last. Clear the text field if that happens.
- **The application name is empty:** run `diagnose`; on Wayland the compositor has to be one of those listed above.
- **No tray icon:** see [The tray icon](#the-tray-icon).
- **Notes are not reaching the server:** open Settings and look at the login status and the count of unsent notes.

## License

Trecs Notes is free software, released under the [GNU Affero General Public License, version 3](LICENSE) or, at your option, any later version.
