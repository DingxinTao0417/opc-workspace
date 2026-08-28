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
{"event":"ready","status":"ok","host":"127.0.0.1","address":"127.0.0.1:49152","url":"http://127.0.0.1:49152","port":49152,"pid":1234,"version":"0.1.0-dev","app_version":"0.1.0-dev","api_version":"v1","schema_version":6}
```

Tauri can gracefully stop the process by writing a line containing
`shutdown` to stdin. The server drains active HTTP requests and checkpoints
the WAL before closing the database.

## Implemented API foundation

- `GET /health`
- `GET /api/v1/tasks` with paging; title/description search; status, priority, kind, project, plan/due range, repeated-tag AND, parent/root filtering; and stable allowlisted sorting
- `POST /api/v1/tasks` with kind, parent, completion criteria and tags plus normalized v2 request SHA-256, legacy-safe snapshot compatibility, and conflict detection for optional `Idempotency-Key`
- `GET /api/v1/tasks/:id` with project/parent names, tags, subtask counts and `ETag`
- `PATCH /api/v1/tasks/:id`, `PATCH /api/v1/tasks/:id/status`, and `DELETE /api/v1/tasks/:id` protected by `If-Match`
- `PATCH /api/v1/tasks/batch` for atomic 1–100 task `set_project`, `set_planned_date`, `add_tags`, or `remove_tags` operations with per-item expected versions
- `PUT /api/v1/tasks/reorder` for atomic manual/default ordering of a complete planned-date group with per-item expected versions
- `GET /api/v1/tags` with paging, search and stable sorting
- `POST /api/v1/tags` with snapshot idempotency; `PATCH /api/v1/tags/:id` and confirmed `DELETE /api/v1/tags/:id?confirm=true` protected by `If-Match`
- `GET /api/v1/projects` with paging, name/description search, status/client filtering, and allowlisted sorting
- `POST /api/v1/projects` with normalized-request SHA-256, first-response snapshot replay, and conflict detection for `Idempotency-Key`
- `GET /api/v1/projects/:id` and `PATCH /api/v1/projects/:id`
- `POST /api/v1/projects/:id/transitions` for the allowlisted project lifecycle
- `DELETE /api/v1/projects/:id?confirm=true` for confirmed hard deletion of archived projects
- `GET /api/v1/stats/today?date=YYYY-MM-DD`

Successful resource responses use `{ "data": ... }`; lists also include
`meta`. Errors always use `{ "code", "message", "request_id" }`. API
timestamps are RFC 3339 UTC, pure dates are `YYYY-MM-DD`, and monetary schema
fields use integer minor units. Task reads return related project/parent names,
ordered tags, subtask counts, and the current task version. Project reads include `client_name`,
`task_summary` (including derived progress and summed task `actual_minutes`),
`invoice_count`, and `available_actions`.

Task, tag, and project create snapshots remain replayable with the original `201`
response even if the resource is later edited or deleted; legacy keys without
a safe snapshot return `409 IDEMPOTENCY_REPLAY_UNAVAILABLE`, while the same key with a different request
returns `409 IDEMPOTENCY_CONFLICT`. Task update/status/delete, tag update/delete,
and Project `PATCH`/transition/delete require `If-Match`; stale versions return
`409 VERSION_CONFLICT`.
Schema v5 also bumps the project version when
task links/status/`actual_minutes`, invoice links/counts, or joined client names
change, so aggregate and deletion-confirmation facts are covered by the same
ETag. Schema v6 adds task and tag versions; parent/child aggregate changes and
embedded tag edits/deletion bump the affected task versions. Task list rows,
their tags, and pagination totals are read in one snapshot transaction.
Transitions are limited to
`start/pause/resume/complete/reopen/archive/restore`. Completing a project with
unfinished tasks requires explicit confirmation and never changes task status.
Archive remembers the previous state for restore. Hard deletion additionally
requires `confirm=true`, only accepts archived projects, and detaches task and
invoice references through the existing `SET NULL` foreign keys.

Archived projects reject new task links with `409 PROJECT_ARCHIVED`; an
existing linked task may still be edited without changing its project. Project
profile `PATCH` also returns `409 PROJECT_ARCHIVED` until the project is restored.

## Database and verification

Numbered SQL migrations are embedded from `internal/database/migrations/` and
recorded in `schema_migrations`. Startup enables foreign keys, WAL, and a
5-second busy timeout. Add future schema changes as new numbered migration
files; never edit a migration already shipped. Migration 002 removes only the
four stable IDs used by the former default demo (two tasks, one project, and
one client); all other development data is preserved. Migration 003 adds
`projects.version`, `projects.archived_from_status`, and project
status/due-date indexes. Migration 004 adds the request hash, response body,
and response status used for safe idempotent replay. Migration 005 advances the
schema to v5 with triggers that bump `projects.version` when task, invoice, or
client-name facts used by a project response change. Migration 006 advances
the current schema to v6: it adds task kind, parent, completion criteria and
version, adds tag version, creates task fact indexes, rejects task-parent
cycles in SQLite, and invalidates parent, child, and tag-embedded task versions
when their rendered facts change.

```powershell
go test ./...
go vet ./...
go build ./cmd/server
```
