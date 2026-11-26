# Web service mode

Running `nzb-monkey-go --serve` starts a persistent web service with a single-page
browser UI and a JSON API that accepts NZBLNK URIs and runs them through the same
pipeline as the CLI.

```bash
nzb-monkey-go --serve --config /path/to/config
```

Each job executes as a subprocess of the same binary, so the configuration file is
re-read for every job - configuration changes take effect without restarting the
service.

## Configuration

```ini
[SERVE]
listen = "127.0.0.1:8085"  # keep on loopback unless the service shares a protected
                           # network namespace with its target
token = ""                 # optional static access token
sabnzbd_auth = true        # reuse the SABnzbd web UI login when available
sabnzbd_pause = true       # pause the SABnzbd queue while a direct search runs
```

`listen` sets the address the HTTP server binds to. The service warns on startup when
it listens on a non-loopback address without any form of authentication.

## Authentication

Two complementary mechanisms exist:

- **Static token**: when `token` is set it must be sent as `Authorization: Bearer
  <token>` or as `?token=<token>` query parameter, or entered on the login page.
- **SABnzbd login**: when `sabnzbd_auth = true` and the SABnzbd web interface itself
  requires a login (detected at startup by a redirect of the web root to `*/login/`),
  the service shows a login form and verifies the same username/password against
  SABnzbd before issuing a short-lived session token (24 h validity).

Authentication is required when either the static token is configured or SABnzbd
login delegation is active. When the SABnzbd interface is open and no token is set,
the service relies on the same network boundary as SABnzbd - keep `listen` on
loopback in that case.

## Web UI

`GET /` serves an embedded single-page application: paste a NZBLNK, submit it and
watch the job's live log. It adapts to the configured auth flow (login form when
required) and uses relative API paths, so it also works behind a path-prefix reverse
proxy.

## JSON API

All endpoints except `/api/healthz`, `/api/login` and `/api/authz` require
authentication when it is configured.

| Method & path | Description |
| ------------- | ----------- |
| `GET /api/healthz` | Health check: `{"status": "ok"}`. |
| `GET /api/authz` | Reports the auth flow: `{"required": bool, "sab_login": bool, "sab_online": bool}`. |
| `POST /api/login` | Body `{"username": "...", "password": "..."}`; verifies against SABnzbd (or the static token as password) and returns `{"token": "..."}`. |
| `POST /api/jobs` | Body `{"nzblnk": "nzblnk://?h=..."}`; validates the NZBLNK and queues a job. Returns `202` with `{"id": <n>}`, `400` on an invalid NZBLNK, `503` when the queue is full. |
| `GET /api/jobs` | Lists the known jobs (newest first): `[{"id", "nzblnk", "status", "created", "finished", "error"}]`. |
| `GET /api/jobs/{id}` | Same fields for one job plus `"log"` with the captured output. `404` when unknown. |
| `GET /api/jobs/{id}/log` | The job's log as `text/plain`. |
| `DELETE /api/jobs/{id}` | Cancels a queued or running job; returns its new status. |

Job statuses: `queued`, `running`, `done`, `failed`, `canceled`.

Jobs run **one at a time** through a single worker (queue capacity 16). The service
keeps the last 100 jobs and up to 256 KB of log tail per job in memory - everything
is lost on restart.

## SABnzbd coordination

With `sabnzbd_pause = true`, every job that can use the direct search (the NZBLNK
carries groups and a date and `directsearch` is enabled) pauses the SABnzbd queue
first and resumes it afterwards, so the shared usenet connection limit of the
account is not exceeded. Non-directsearch jobs do not pause the queue. A leftover
pause from a previous run is cleared at startup.

Instead of duplicating settings, the service can also import `sabnzbd.ini` - see the
[SABNZBD section of the configuration reference](configuration.md#sabnzbdini-import).

## Running next to SABnzbd (systemd)

The service must run in the **same network namespace** as SABnzbd so it can reach the
SABnzbd API and so that usenet traffic takes the same route.

`deploy/nzb-monkey-go.service` is a generic systemd unit template:

- If SABnzbd runs in a dedicated named network namespace, the unit joins it via
  `ip netns exec` (or natively via `NetworkNamespacePath` on systemd 256+) and binds
  its lifetime to the namespace provider.
- If SABnzbd runs in the default namespace, remove the namespace lines and the
  `BindsTo`/`After` placeholders.
- Run the service as the same user as SABnzbd so it can read `sabnzbd.ini`.

```bash
# install and adjust the marked <...> placeholders, then:
systemctl daemon-reload
systemctl enable --now nzb-monkey-go
```

The service is also happy behind a reverse proxy, including a path prefix - the UI
uses relative API paths.
