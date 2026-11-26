# Configuration

NZB Monkey Go is configured through a single INI-style configuration file. If the
file does not exist it is created with defaults on the first run.

Default locations:

- Windows/macOS: `config.txt` in the same directory as the binary
- Linux: `~/.config/nzb-monkey-go.conf`
- Any other file can be selected with `--config /path/to/file`

Sections are marked `[LIKE_THIS]`, values are assigned with `key = value`. Strings may
be quoted. `#` starts a comment.

## [GENERAL]

| Key | Default | Description |
| --- | ------- | ----------- |
| `target` | `"EXECUTE"` | Where to send the NZB file: `EXECUTE`, `SABNZBD`, `NZBGET` or `SYNOLOGYDLS`. Multiple targets can be comma-separated, e.g. `"EXECUTE,SABNZBD"`. |
| `categorize` | `"off"` | Category handling: `off`, `auto` (match title against the `[CATEGORIZER]` regexes) or `manual` (pick from the target's category list). |
| `success_wait_time` | `3` | Seconds to wait before closing the window after success. |
| `error_wait_time` | `10` | Seconds to wait before closing the window after an error. |
| `debug` | `false` | Write a debug log (`logfile.txt` next to the binary on Windows/macOS, `/tmp/nzb-monkey-go.log` on Linux). |

## [EXECUTE]

Saves the NZB file to disk. Optionally opens it with the default application for
`.nzb` files.

| Key | Default | Description |
| --- | ------- | ----------- |
| `passtofile` | `true` | Append the password to the filename as `{{password}}`. |
| `passtoclipboard` | `false` | Copy the password to the clipboard. |
| `nzbsavepath` | `"./Downloads/nzb"` | Where to save NZB files - absolute path or relative to the user's home directory. |
| `category_folder` | `false` | Save into a sub-folder named after the category. |
| `dontexecute` | `true` | Don't open the saved file with the default application. |
| `save_as_zip` | `false` | Save the NZB as a `.zip` instead of `.nzb`. |
| `clean_up_enable` | `false` | Delete old NZB files from `nzbsavepath` before saving a new one. |
| `clean_up_max_age` | `2` | Files older than this many days are deleted (`.nzb` and `.zip` only). |

## [SABNZBD]

Uploads the NZB to SABnzbd via its API.

| Key | Default | Description |
| --- | ------- | ----------- |
| `host` | `"localhost"` | SABnzbd hostname. |
| `port` | `8080` | SABnzbd port. |
| `ssl` | `false` | Use HTTPS. |
| `skip_check` | `false` | Skip TLS certificate verification (e.g. self-signed certificates). |
| `nzbkey` | `""` | SABnzbd API key (NZB key). |
| `basicauth_username` / `basicauth_password` | `""` | HTTP basic auth credentials, if the web server in front of SABnzbd requires them. |
| `basepath` | `""` | URL base path if SABnzbd is behind a reverse proxy path prefix. |
| `category` | `""` | SABnzbd category for the upload. |
| `addpaused` | `false` | Add the NZB paused to the queue. |
| `compression` | `"none"` | Upload compression: `none` or `zip` (gzip). |
| `timeout` | `30` | Connection timeout in seconds. |
| `ini` | `""` | Path to a `sabnzbd.ini` file to import settings from (see below). |
| `use_server_credentials` | `true` | Fill empty `DIRECTSEARCH` credentials from the enabled usenet server with the lowest priority in `sabnzbd.ini`. |

### sabnzbd.ini import

When `ini` points to a `sabnzbd.ini` file, the following values are imported - but
only into fields that are empty in the nzb-monkey-go configuration; explicitly
configured values always win:

- API key and web interface host/port/SSL into `SABNZBD`
- the enabled usenet server with the lowest priority (host, port, SSL, username,
  password, connection limit) into `DIRECTSEARCH` when `use_server_credentials = true`

```ini
[SABNZBD]
ini = "/path/to/sabnzbd.ini"
use_server_credentials = true
```

## [SERVE]

Settings for the web service started with `--serve`. See
[Web service mode](web-service.md) for details.

| Key | Default | Description |
| --- | ------- | ----------- |
| `listen` | `"127.0.0.1:8085"` | Listen address. Keep it on loopback unless the service shares a protected network namespace with its target. |
| `token` | `""` | Optional static access token (bearer token or login page). |
| `sabnzbd_auth` | `true` | Reuse the SABnzbd web UI login when SABnzbd requires one. |
| `sabnzbd_pause` | `true` | Pause the SABnzbd queue while a direct search is running. |

## [NZBGET]

Uploads the NZB to NZBGet via its API.

| Key | Default | Description |
| --- | ------- | ----------- |
| `host` | `"localhost"` | NZBGet host. |
| `port` | `6789` | NZBGet port. |
| `ssl` | `false` | Use HTTPS. |
| `skip_check` | `false` | Skip TLS certificate verification. |
| `user` / `pass` | `""` | NZBGet username/password. |
| `basepath` | `""` | URL base path behind a reverse proxy. |
| `category` | `""` | NZBGet category for the upload. |
| `addpaused` | `false` | Add the NZB paused to the queue. |
| `timeout` | `30` | Connection timeout in seconds. |

## [SYNOLOGYDLS]

Uploads the NZB to a Synology DownloadStation.

| Key | Default | Description |
| --- | ------- | ----------- |
| `host` | `"localhost"` | DownloadStation host. |
| `port` | `5000` | DownloadStation port. |
| `ssl` | `false` | Use HTTPS. |
| `skip_check` | `false` | Skip TLS certificate verification. |
| `user` / `pass` | `""` | DownloadStation username/password. |
| `basepath` | `""` | URL base path. |
| `timeout` | `30` | Connection timeout in seconds. |

## [NZBCheck]

Every downloaded/created NZB is checked for completeness before it is used.

| Key | Default | Description |
| --- | ------- | ----------- |
| `skip_failed` | `true` | Skip NZBs that exceed the missing file/segment limits below. |
| `max_missing_segments_percent` | `1` | Maximum allowed percentage of missing segments. |
| `max_missing_files` | `1` | Maximum number of missing files. |
| `best_nzb` | `true` | Always run all search engines and use the best NZB. Stops early once a 100 % complete NZB is found. |

## [CATEGORIZER]

Used when `categorize = "auto"`. Each key is a category name, each value a regex that
is matched case-insensitively against the title. The first matching category wins.

```ini
[CATEGORIZER]
series = "(s\d+e\d+|s\d+ complete)"
movies = "(x264|xvid|bluray|720p|1080p|untouched)"
```

With `categorize = "manual"` the category list is fetched from the first configured
target that supports it (SABnzbd, NZBGet) and you pick one interactively.

## [SEARCHENGINES]

Each engine gets a weight of `0-9`. `0` disables the engine, `1-9` enables it and
defines the order in which engines are tried. Several engines may share a weight
(they then run in alphabetical order).

| Engine | Default weight | Notes |
| ------ | -------------- | ----- |
| `nzbindex` | 1 | NZBIndex |
| `nzbking` | 2 | NZBKing |
| `binsearch` | 3 | Binsearch |
| `easynews` | 4 | Needs an Easynews account, see `[EASYNEWS]` |
| `directsearch` | 5 | Searches your own usenet server, see `[DIRECTSEARCH]` and [Direct search](directsearch.md) |

If `best_nzb = true`, all enabled engines are used and the best NZB wins; a 100 %
complete result ends the search early. Otherwise the first acceptable NZB is used.

### [NEWZNAB] - generic Newznab indexers

Any Newznab-compatible indexer (DrunkenSlug, NZBGeek, NZBPlanet, abNZB, altHUB,
Usenet Crawler, ...) can be added. Register an account at the indexer and copy the
API key from your profile.

```ini
[NEWZNAB]
drunkenslug = "https://api.drunkenslug.com,YOUR_API_KEY"
nzbgeek = "https://api.nzbgeek.info,YOUR_API_KEY"

[SEARCHENGINES]
drunkenslug = 6
```

Each entry registers a search engine under the given name (`host,apikey` per line,
`https://` is assumed when no scheme is given) which can then be weighted in
`[SEARCHENGINES]` like the built-in engines.

## [EASYNEWS]

| Key | Default | Description |
| --- | ------- | ----------- |
| `username` | `""` | Easynews username. |
| `password` | `""` | Easynews password. |
| `subject_search_only` | `false` | Search only in subjects instead of the keyword search. More accurate in some cases but will not find obfuscated uploads. |
| `oldest_result` | `false` | Use the oldest result instead of the newest. |

## [DIRECTSEARCH]

Searches article headers directly on your usenet server and builds the NZB file
itself - no indexer needed. Requires a NZBLNK with groups (`g`) and a date (`d`).
See [Direct search](directsearch.md) for how it works and tuning advice.

| Key | Default | Description |
| --- | ------- | ----------- |
| `host` | `"news-eu.newshosting.com"` | Usenet server host name. |
| `port` | `119` | Usenet server port (`563` for SSL). |
| `ssl` | `false` | Use SSL/TLS. |
| `skip_check` | `true` | Skip TLS certificate verification. |
| `username` / `password` | `""` | Usenet account credentials. |
| `connections` | `20` | Number of connections for overview requests. Must not exceed your provider's connection limit. |
| `hours` | `12` | Hours to search backwards from the provided date. |
| `forward_hours` | `12` | Hours to search forward from the provided date. |
| `step` | `20000` | Articles per overview request. |
| `overview_timeout` | `5` | Timeout in seconds for overview requests. |
| `overview_retries` | `3` | Retries per overview request. |
| `skip` | `true` | Skip the direct search when `best_nzb` already found a complete NZB. |
| `first_group_only` | `false` | Only search the first group when the NZBLNK lists several (results are virtually always identical across groups). |
| `use_ansi_codes` | `true` | Use ANSI codes for the progress bar. |
| `show_counter` | `true` | Show a counter in the progress bar. |
| `one_instance_only` | `true` | Only allow one direct search at a time (avoids exceeding the connection limit). |
| `boundaries_scanner_step` | `500` | Step size of the boundary scan that locates the first/last message for the date range. |
| `boundaries_scanner_tolerance` | `30` | Tolerance in seconds for the boundary scan to consider a date close enough. |
| `cache` | `false` | Keep scanned overview data in a local cache - subsequent searches reuse it. |
| `cache_path` | `""` | Cache directory; defaults to the OS user cache dir (e.g. `~/.cache/nzb-monkey-go`). `~/` is expanded. |
| `cache_size` | `500` | Maximum cache size in MB; oldest ranges are evicted first. |
