[![Release Workflow](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/build_and_publish.yml/badge.svg?event=release)](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/build_and_publish.yml)
[![CI](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/ci.yml/badge.svg)](https://github.com/Tensai75/nzb-monkey-go/actions/workflows/ci.yml)
[![Latest Release)](https://img.shields.io/github/v/release/Tensai75/nzb-monkey-go?logo=github)](https://github.com/Tensai75/nzb-monkey-go/releases/latest)

<img src="https://raw.githubusercontent.com/Tensai75/nzb-monkey-go/refs/heads/main/resources/nzb-monkey-go.svg" alt="Logo" width="128"/>

# NZB Monkey Go

A completely rewritten version in Golang of the [NZB Monkey](https://github.com/nzblnk/nzb-monkey), the reference implementation written in Python of how to handle a [NZBLNK](https://nzblnk.github.io/)-URI.
NZB Monkey Go also includes the functionality of the [NZBSearcher](https://github.com/Tensai75/nzbsearcher) so besides in the nzb search engines it can also search directly on the news server and create the NZB file from the article headers.

## Running the Monkey

Running the NZB Monkey Go is virtually identical to running the original NZB Monkey.

Differences are that in the configuration file, 'directsearch' can be enabled as an additional search engine in the section 'SEARCHENGINES' and an additional section 'DIRECTSEARCH' is included, with additional configuration options required for the direct search on the news server to work.

See the [NZB Monkey Go Wiki](https://github.com/Tensai75/nzb-monkey-go/wiki) for further information regarding installation and configuration.

Note that directsearch requires the NZBLNK to include the news groups (parameter 'g') and an additional parameter for the date ('d') when the post was uploaded to the Usenet.
Parameter 'd' can be provided either in the format 'DD.MM.YYYY' or as an unix timestamp.

### Generic Newznab indexers

Besides the built-in search engines, any Newznab-compatible indexer (DrunkenSlug, NZBGeek, altHUB, Usenet Crawler, ...) can be added via the `NEWZNAB` section of the configuration file:

```ini
[NEWZNAB]
myindexer = "https://api.example.com,YOUR_API_KEY"

[SEARCHENGINES]
myindexer = 5
```

Each entry registers a search engine under the given name which can then be enabled and weighted like the built-in engines.

### Direct search cache

Directsearch can keep a local cache of already scanned overview data so that subsequent searches skip message ranges that have been downloaded before:

```ini
[DIRECTSEARCH]
cache = true
cache_path = ""     # defaults to the OS user cache dir (e.g. ~/.cache/nzb-monkey-go)
cache_size = 500    # maximum cache size in MB
```

- The cache stores raw overview data per message range (gzip compressed), so cached ranges are reused for **any** search term - not only for repeated identical searches.
- Partial reuse is supported: if a search only partially overlaps cached ranges, just the missing gaps are fetched from the server.
- When the size limit is reached, the oldest ranges are evicted first.
- As a rule of thumb, 500 MB covers roughly 20-25 million message headers (one or two full scans of a busy group); use 1 GB or more if you regularly scan several high-traffic groups.

## Web service mode

Running `nzb-monkey-go --serve` starts a persistent web service with a single-page UI and a JSON API that accepts NZBLNK URIs and runs them through the same pipeline as the CLI. Each job executes as a subprocess of the same binary, so the configuration file is re-read for every job.

```ini
[SERVE]
listen = "127.0.0.1:8085"  # keep on loopback unless the service shares a protected namespace with its target
token = ""                 # optional static access token
sabnzbd_auth = true        # reuse the SABnzbd web UI login when available
sabnzbd_pause = true       # pause the SABnzbd queue while a direct search runs
```

The API exposes `POST /api/jobs` (`{"nzblnk": "..."}`), `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/jobs/{id}/log`, `DELETE /api/jobs/{id}` and `GET /api/healthz`. Jobs run one at a time and keep their logs in memory for the last 100 jobs.

When the SABnzbd web interface requires a login (its web root redirects to `*/login/`), the service shows a login form and verifies the same username/password against SABnzbd before issuing a short-lived session token. When the SABnzbd interface is open, the service relies on the same network boundary and the optional static `token` instead.

### SABnzbd configuration import

Instead of duplicating settings, the service can read a `sabnzbd.ini` file:

```ini
[SABNZBD]
ini = "/path/to/sabnzbd.ini"
use_server_credentials = true
```

The enabled usenet server with the lowest priority and the API key/web interface settings are imported, but only into fields that are empty in the nzb-monkey-go configuration - explicitly configured values always win.

### Running next to SABnzbd

The service must run in the **same network namespace** as SABnzbd so it can reach the SABnzbd API and so that usenet traffic takes the same route. `deploy/nzb-monkey-go.service` contains a generic systemd unit - if SABnzbd runs in a dedicated namespace it joins it via `ip netns exec` (or `NetworkNamespacePath` on systemd 256+); run the service as the same user as SABnzbd so it can read `sabnzbd.ini`.

With `sabnzbd_pause = true`, every directsearch job pauses the SABnzbd queue first and resumes it afterwards, so the shared usenet connection limit of the account is not exceeded. Non-directsearch jobs do not pause the queue.

![Monkey-Gif](https://github.com/Tensai75/nzb-monkey-go/raw/main/resources/nzbmonkey-go.gif)

## Windows and Linux binaries

The binaries are available on the [release page](https://github.com/Tensai75/nzb-monkey-go/releases).

For Arch Linux / Manjao user a AUR package is kindly provided by [nzb-tuxxx](https://github.com/nzb-tuxxx): https://aur.archlinux.org/packages/nzb-monkey-go-bin

### macOS Support

Please see the beta app bundle release here: [NZB Monkey Go v0.4.0-beta1 (darwin only)](https://github.com/Tensai75/nzb-monkey-go/releases/tag/v0.4.0-beta1).
Feedback and error reports are welcome!

## Development

Build and test locally:

```bash
go build ./...
go vet ./...
go test -race ./...
```

The test suite uses only local fake servers (fake NNTP and HTTP test servers) and needs no credentials or external services. The integration tests in `integration_test.go` exercise the full pipeline from NZBLNK input through the search engines to the configured target, including the directsearch cache.

CI runs `gofmt`, `go vet` and `go test -race` plus cross-platform compile checks on every push and pull request.

## Contribution

Feel free to send pull requests.
