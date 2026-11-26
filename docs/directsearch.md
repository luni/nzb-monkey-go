# Direct search

Besides the indexer-based search engines, NZB Monkey Go can search **directly on
your usenet server** and build the NZB file from the article headers - the
functionality of [NZBSearcher](https://github.com/Tensai75/nzbsearcher). No indexer
account is needed, only your usenet provider credentials.

Enable it like any other engine:

```ini
[SEARCHENGINES]
directsearch = 5
```

## Requirements

The NZBLNK must contain:

- `g` - the group(s) the post was uploaded to
- `d` - the date the post was uploaded (`DD.MM.YYYY` or a Unix timestamp)

Indexers know nothing about groups; these parameters exist exactly so the post can
be located on the server.

## How it works

For every group in the NZBLNK:

1. **Boundary scan** - the date window (`date - hours` to `date + forward_hours`,
   plus 24 h for day-precision dates) is located within the group's article numbers
   by a parallel scan that homes in on the first and last message of the window.
2. **Header scan** - overview data (subjects, message IDs, ...) for that message
   range is streamed from the server in chunks of `step` articles over
   `connections` parallel NNTP connections. Subjects matching the header are
   collected.
3. **NZB build** - matching articles are grouped into files and a real NZB file is
   assembled, which then goes through the usual completeness check and target
   pipeline.

The scan reports throughput and a progress bar while running (`use_ansi_codes`,
`show_counter`).

## Important options

See the [configuration reference](configuration.md#directsearch) for all keys.

- `connections` - parallel NNTP connections for overview requests (default 20).
  This must fit within your provider's simultaneous connection limit; if you share
  the account with a downloader, keep headroom or use `sabnzbd_pause` in
  [web service mode](web-service.md).
- `hours` / `forward_hours` - the search window around the provided date
  (default ±12 h). Widen it when posts are frequently found outside the default
  window; a wider window means more headers to scan.
- `step` - articles per overview request (default 20000).
- `overview_timeout` / `overview_retries` - per-request timeout and retry count.
- `skip` - skip the direct search when `best_nzb` already produced a complete NZB
  (default true). Direct search is the most expensive engine, so it is listed last
  and skipped when earlier engines succeeded.
- `first_group_only` - only search the first listed group; results virtually never
  differ between groups.
- `one_instance_only` - a per-user lock file prevents two direct searches from
  running at the same time, which would multiply the connection usage.
- `boundaries_scanner_step` / `boundaries_scanner_tolerance` - tuning of the
  boundary scan (defaults 500 / 30 s) - normally no need to change.

## Overview cache

Directsearch can keep a local cache of already scanned overview data so subsequent
searches skip message ranges that were downloaded before:

```ini
[DIRECTSEARCH]
cache = true
cache_path = ""     # defaults to the OS user cache dir (e.g. ~/.cache/nzb-monkey-go)
cache_size = 500    # maximum cache size in MB
```

- The cache stores raw overview data per message range (gzip compressed), so cached
  ranges are reused for **any** search term - not only for repeated identical
  searches.
- Partial reuse is supported: if a search only partially overlaps cached ranges,
  just the missing gaps are fetched from the server.
- When the size limit is reached, the oldest ranges are evicted first.
- As a rule of thumb, 500 MB covers roughly 20-25 million message headers (one or
  two full scans of a busy group); use 1 GB or more if you regularly scan several
  high-traffic groups.
- `~/` at the start of `cache_path` is expanded to your home directory.
