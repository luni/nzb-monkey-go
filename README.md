[![Release Workflow](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/build_and_publish.yml/badge.svg?event=release)](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/build_and_publish.yml)
[![CI](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/ci.yml/badge.svg)](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/ci.yml)
[![Latest Release)](https://img.shields.io/github/v/release/Tensai75/nzb-monkey-go?logo=github)](https://github.com/Tensai75/nzb-monkey-go/releases/latest)

<img src="https://raw.githubusercontent.com/Tensai75/nzb-monkey-go/refs/heads/main/resources/nzb-monkey-go.svg" alt="Logo" width="128"/>

# NZB Monkey Go

A completely rewritten version in Golang of the [NZB Monkey](https://github.com/nzblnk/nzb-monkey), the reference implementation written in Python of how to handle a [NZBLNK](https://nzblnk.github.io/)-URI.
NZB Monkey Go also includes the functionality of the [NZBSearcher](https://github.com/Tensai75/nzbsearcher) so besides in the nzb search engines it can also search directly on the news server and create the NZB file from the article headers.

## Features

- Handles `nzblnk://` URIs - register it as protocol handler and click NZBLNKs in your browser
- Searches NZBIndex, NZBKing, Binsearch, Easynews and any Newznab-compatible indexer
- Searches article headers directly on your usenet server (directsearch) - no indexer needed
- Verifies NZB completeness (missing files/segments) and picks the best result
- Sends the NZB to SABnzbd, NZBGet or Synology DownloadStation - or just saves it to disk
- Optional web service mode (`--serve`) with browser UI and JSON API

## Quick start

1. Download a binary for your platform from the [release page](https://github.com/Tensai75/nzb-monkey-go/releases).
2. Run it once - it creates a default configuration file and opens it for editing.
3. Edit the configuration (at minimum choose a target and enable search engines).
4. Run `nzb-monkey-go --register` to register the `nzblnk://` protocol handler.
5. Click a NZBLNK - or pass one on the command line:

```bash
nzb-monkey-go "nzblnk://?h=Some.Release&p=secret&g=alt.binaries.example&d=01.02.2026"
```

## Documentation

- [Installation](docs/installation.md) - download, setup and protocol registration per OS
- [Configuration](docs/configuration.md) - complete reference of the configuration file
- [Usage](docs/usage.md) - CLI arguments and the NZBLNK URI format
- [Direct search](docs/directsearch.md) - searching article headers on the news server, incl. the overview cache
- [Web service mode](docs/web-service.md) - the `--serve` mode, JSON API and systemd deployment
- [Development](docs/development.md) - building, testing and CI

![Monkey-Gif](https://github.com/Tensai75/nzb-monkey-go/raw/main/resources/nzbmonkey-go.gif)

## Windows and Linux binaries

The binaries are available on the [release page](https://github.com/Tensai75/nzb-monkey-go/releases).

For Arch Linux / Manjaro users an AUR package is kindly provided by [nzb-tuxxx](https://github.com/nzb-tuxxx): https://aur.archlinux.org/packages/nzb-monkey-go-bin

### macOS Support

Please see the beta app bundle release here: [NZB Monkey Go v0.4.0-beta1 (darwin only)](https://github.com/Tensai75/nzb-monkey-go/releases/tag/v0.4.0-beta1).
Feedback and error reports are welcome!

## Contribution

Feel free to send pull requests.
