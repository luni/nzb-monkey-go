# Installation

## Download

Pre-built binaries for Windows, Linux (amd64 and arm64) and macOS are available on the
[release page](https://github.com/Tensai75/nzb-monkey-go/releases). The binary is a
single self-contained executable - no runtime or installer is required.

| Platform | Package |
| -------- | ------- |
| Windows  | `nzb-monkey-go.exe` |
| Linux    | `nzb-monkey-go` (amd64 / arm64) |
| macOS    | `NZBMonkeyGo.app` beta bundle (see the v0.4.0-beta1 release) |

For Arch Linux / Manjaro an AUR package is kindly provided by
[nzb-tuxxx](https://github.com/nzb-tuxxx): https://aur.archlinux.org/packages/nzb-monkey-go-bin

## First run

Run the binary once without arguments. It detects that no configuration file exists,
writes the default configuration and opens it in your editor. It also registers the
`nzblnk://` URL protocol handler automatically (same as running `--register`).

Edit the configuration file before continuing - at minimum choose a `target` in the
`GENERAL` section and review the enabled engines in `SEARCHENGINES`. See
[Configuration](configuration.md) for a complete reference.

### Configuration file location

| OS | Default location |
| -- | ---------------- |
| Windows | `config.txt` in the same directory as `nzb-monkey-go.exe` |
| macOS | `config.txt` in the same directory as the binary (inside the app bundle: `NZBMonkeyGo.app/Contents/MacOS/`) |
| Linux | `~/.config/nzb-monkey-go.conf` |

A different file can be selected on every invocation with `--config /path/to/file`.

On Linux, a `config.txt` found next to the binary is automatically migrated to
`~/.config/nzb-monkey-go.conf` on startup.

## Registering the NZBLNK protocol

Registering makes the browser open `nzblnk://` links with NZB Monkey Go:

```bash
nzb-monkey-go --register
```

What happens per OS:

- **Windows**: creates `HKEY_CURRENT_USER\SOFTWARE\Classes\nzblnk` registry keys that
  point to the executable. No admin rights needed.
- **Linux**: writes `~/.local/share/applications/nzblnk.desktop` and sets it as the
  default handler for `x-scheme-handler/nzblnk` via `xdg-mime`. The handler opens the
  binary in the default terminal.
- **macOS**: updates `Info.plist` of the app bundle - this only works when running
  from inside the distributed `NZBMonkeyGo.app` bundle.

If you move the binary afterwards, run `--register` again so the registered path stays
correct.

## Running as a web service

Instead of handling `nzblnk://` clicks on a desktop, NZB Monkey Go can run permanently
as a small web service that accepts NZBLNK URIs through a web page and a JSON API:

```bash
nzb-monkey-go --serve
```

See [Web service mode](web-service.md) for configuration, API reference and a systemd
unit template for running next to SABnzbd.

## Troubleshooting

Set `debug = true` in the `GENERAL` section (or pass `--debug`) to write a debug log:

- Windows/macOS: `logfile.txt` next to the binary
- Linux: `/tmp/nzb-monkey-go.log`
