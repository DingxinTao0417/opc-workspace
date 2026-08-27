# opc-workspace Go Sidecar

The Sidecar is the local HTTP and SQLite boundary for opc-workspace. It is a
Go 1.22+ binary, uses Gin and GORM, and uses the pure-Go `modernc.org/sqlite`
stack through `github.com/glebarez/sqlite`; CGO and Docker are not required.

## Run in development

From this directory:

```powershell
$env:OPC_SESSION_TOKEN = 'opc-workspace-local-dev'
go run ./cmd/server --dev --db ../../.local/dev-data/opc-workspace.db --port 9876
```

Development starts with empty business data by default. For an explicit,
temporary demo, append `--seed`; it is rejected unless `--dev` is also present,
is idempotent, and should only be used with the repository-local development
database. In production, Tauri passes an absolute app-data database path and
uses `--port 0` so the operating system chooses an available loopback port.

Configuration can be provided by flags or environment variables:

| Setting | Flag | Environment |
| --- | --- | --- |
| SQLite path | `--db` | `OPC_DB_PATH` |
| Loopback port | `--port` | `OPC_PORT` |
| Session token | — | `OPC_SESSION_TOKEN` |
| Exact Origin allowlist | `--allowed-origins` | `OPC_ALLOWED_ORIGINS` |
| Development mode | `--dev` | `OPC_DEV` |
| Development seed | `--seed` | `OPC_DEV_SEED` |

Outside explicit development mode, a non-empty `OPC_SESSION_TOKEN` is
required. When development mode has a token, authentication remains enabled;
it is relaxed only when `--dev` is active and the token is empty. Every HTTP
endpoint, including `/health`, otherwise requires `Authorization: Bearer
<token>`. Browser requests must have an exact allowlisted `Origin`; native
Tauri probes without browser fetch headers may omit it.

The process binds only `127.0.0.1`. Once migrations and listening succeed,
stdout receives exactly one newline-terminated ready event; operational logs
go to stderr:

```json
{"event":"ready","status":"ok","host":"127.0.0.1","address":"127.0.0.1:49152","url":"http://127.0.0.1:49152","port":49152,"pid":1234,"version":"0.1.0-dev","app_version":"0.1.0-dev","api_version":"v1","schema_version":2}
```

Tauri can gracefully stop the process by writing a line containing
`shutdown` to stdin. The server drains active HTTP requests and checkpoints
the WAL before closing the database.

## Implemented API foundation

- `GET /health`
- `GET /api/v1/tasks` with paging, search, filtering, and allowlisted sorting
- `POST /api/v1/tasks` with optional `Idempotency-Key`
- `GET /api/v1/tasks/:id`
- `PATCH /api/v1/tasks/:id` and `PATCH /api/v1/tasks/:id/status`
- `DELETE /api/v1/tasks/:id`
- `GET /api/v1/stats/today?date=YYYY-MM-DD`

Successful resource responses use `{ "data": ... }`; lists also include
`meta`. Errors always use `{ "code", "message", "request_id" }`. API
timestamps are RFC 3339 UTC, pure dates are `YYYY-MM-DD`, and monetary schema
fields use integer minor units.

## Database and verification

Numbered SQL migrations are embedded from `internal/database/migrations/` and
recorded in `schema_migrations`. Startup enables foreign keys, WAL, and a
5-second busy timeout. Add future schema changes as new numbered migration
files; never edit a migration already shipped. Migration 002 removes only the
four stable IDs used by the former default demo (two tasks, one project, and
one client); all other development data is preserved.

```powershell
go test ./...
go vet ./...
go build ./cmd/server
```
