# Usage

## Command line

```text
Usage: nzb-monkey-go [--subject SUBJECT] [--title TITLE] [--password PASSWORD]
       [--group GROUP [GROUP ...]] [--date DATE] [--category CATEGORY]
       [--config CONFIG] [--debug] [--register] [--serve] [NZBLNK]
```

| Argument | Description |
| -------- | ----------- |
| `NZBLNK` | A qualified NZBLNK URI (`nzblnk://?h=...`). Positional. |
| `-s`, `--subject` | The header/subject to search for. Required when no NZBLNK is given. |
| `-t`, `--title` | The title/tag used for the NZB file name. Defaults to the subject. |
| `-p`, `--password` | The password to extract the download. |
| `-g`, `--group` | The group(s) to search in, several groups separated by space. |
| `-d`, `--date` | The date the upload was posted to Usenet - `DD.MM.YYYY` or a Unix timestamp. |
| `-c`, `--category` | The category to use for the target (if supported by the target). |
| `--config` | Path to the configuration file. |
| `--debug` | Log output to the log file. |
| `--register` | Register the `nzblnk` URL protocol handler and exit. |
| `--serve` | Run as a web service accepting NZBLNK URIs via a web page and API. See [Web service mode](web-service.md). |
| `-h`, `--help` | Show usage. |
| `--version` | Show version. |

All search parameters can either come from a NZBLNK URI or be passed as arguments.
**Arguments take precedence over the parameters of the NZBLNK** - you can e.g. take
the header from the link but override the password on the command line.

Examples:

```bash
# full NZBLNK
nzb-monkey-go "nzblnk://?h=Some.Release.poster&p=secret&g=alt.binaries.boneless&d=01.02.2026"

# no NZBLNK at all - arguments only
nzb-monkey-go --subject "Some.Release.poster" --group alt.binaries.boneless --date 1738368000

# NZBLNK with an override
nzb-monkey-go "nzblnk://?h=Some.Release.poster" --password "other-password"
```

## The NZBLNK URI format

NZBLNK is a simple URI scheme carrying the information needed to find a usenet post:

```text
nzblnk://?h=<header>&t=<title>&p=<password>&g=<group>&d=<date>
```

| Parameter | Required | Description |
| --------- | -------- | ----------- |
| `h` | yes | Header/subject to search for. |
| `t` | no | Title for the NZB file name; defaults to `h`. |
| `p` | no | Archive password; stored in the NZB metadata and (for EXECUTE) optionally appended to the file name. |
| `g` | required for directsearch | Group(s) the post was uploaded to. May be repeated (`&g=a.b.x&g=a.b.y`) or carry several groups separated by comma, semicolon or space. The shorthand `a.b.` expands to `alt.binaries.`. |
| `d` | required for directsearch | Date the post was uploaded - `DD.MM.YYYY` or a Unix timestamp. A `DD.MM.YYYY` date covers the whole day (the scan range is extended by 24 h), a timestamp is treated as exact. |

Without `g` and `d` only the indexer-based search engines can be used; the
directsearch needs both. See [Direct search](directsearch.md).

## What happens on a run

1. The configuration file is loaded (created with defaults and opened when missing).
2. All enabled search engines are tried in the order configured in `[SEARCHENGINES]`.
3. Every found NZB is checked for completeness (`[NZBCheck]`); incomplete NZBs are
   skipped or, with `best_nzb = true`, the best one wins at the end.
4. The winning NZB is pushed to every configured `[GENERAL]` target (saved to disk
   and/or uploaded to SABnzbd/NZBGet/DownloadStation).

The program exits with code `0` on success and `1` on failure.

## Logging

Normal output goes to the console. With `--debug` or `debug = true` in the
configuration, a log file is written:

- Windows/macOS: `logfile.txt` next to the binary
- Linux: `/tmp/nzb-monkey-go.log`
