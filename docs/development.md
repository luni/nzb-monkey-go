# Development

NZB Monkey Go is a single Go module in the repository root - no internal packages,
just files grouped by concern.

## Build and test

```bash
go build ./...
go vet ./...
go test -race ./...
```

The test suite uses only local fake servers (a fake NNTP server and HTTP test
servers) and needs no credentials or external services. The integration tests in
`integration_test.go` exercise the full pipeline from NZBLNK input through the
search engines to the configured target, including the directsearch cache.

## CI

GitHub Actions (`.github/workflows/`):

- `ci.yml` - on every push and PR: `gofmt` check, `go vet`, `go test -race`, plus
  cross-platform compile checks (linux amd64/arm64, windows amd64, darwin
  amd64/arm64).
- `build_and_publish.yml` - builds and publishes release binaries.

Dependabot keeps the Go module dependencies up to date.

## Code map

| Area | Files |
| ---- | ----- |
| Entry point, result pipeline | `main.go` |
| CLI arguments, NZBLNK parsing | `arguments.go` |
| Configuration loading, defaults | `configuration.go`, `defaultconfig.go`, `checkforconfig.go`, `setconfpath_*.go` |
| Search engines | `searchengines.go` (nzbindex, nzbking, binsearch, newznab), `easynews.go`, `nzbdirectsearch.go`, `directsearch_cache.go`, `nntp.go` |
| NZB targets | `targets.go`, `target_execute.go`, `target_sabnzbd.go`, `target_nzbget.go`, `target_synologyds.go` |
| Web service | `serve.go`, `sabnzbd_api.go`, `sabnzbd_ini.go`, embedded UI `assets/serve.html` |
| Categories | `categories.go` |
| Protocol registration | `registerprotocol_*.go` |
| Utilities | `logger.go`, `http.go` |
| Deployment | `deploy/nzb-monkey-go.service` |

Notable dependencies (`go.mod`): `go-arg` (CLI), `ini.v1` (config), `nntpDirectSearch`
(header scanning), `nzbparser` (NZB model), `nntpPool` (NNTP connection pool),
`fslock` (single-instance lock), `fatih/color`, `progressbar`.

## Conventions

- `gofmt`-clean, `go vet`-clean code; race-enabled tests must pass.
- Exported behavior is covered by table-driven unit tests plus the integration
  tests; new features should extend the fake servers rather than hit the network.
- Keep changelog entries under `Unreleased` in `CHANGELOG.md`.
