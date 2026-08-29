# opc-workspace Go Sidecar

The Sidecar is opc-workspace's local HTTP, SQLite, and controlled business-file boundary for Task Artifacts plus Client and Project Attachments. It is a Go 1.22+ binary built with Gin, GORM, and the pure-Go `modernc.org/sqlite` stack through `github.com/glebarez/sqlite`. Local development and desktop runtime do not require CGO or Docker.

## Run in development

From this directory:

```powershell
$env:OPC_SESSION_TOKEN = 'opc-workspace-local-dev'
go run ./cmd/server --dev --db ../../.local/dev-data/opc-workspace.db --artifacts ../../.local/dev-data/artifacts --backups ../../.local/dev-data/backups --port 9876
```

Development starts with empty business data. `--seed` is an explicit, idempotent development-only option and is rejected without `--dev`. Production Tauri supplies absolute app-data paths and uses `--port 0`, allowing the operating system to choose an available loopback port.

| Setting                  | Flag                | Environment           |
| ------------------------ | ------------------- | --------------------- |
| SQLite path              | `--db`              | `OPC_DB_PATH`         |
| Controlled Artifact root | `--artifacts`       | `OPC_ARTIFACT_DIR`    |
| Verified backup root     | `--backups`         | `OPC_BACKUP_DIR`      |
| Operational log root     | `--logs`            | `OPC_LOG_DIR`         |
| Loopback port            | `--port`            | `OPC_PORT`            |
| Session token            | —                   | `OPC_SESSION_TOKEN`   |
| Exact Origin allowlist   | `--allowed-origins` | `OPC_ALLOWED_ORIGINS` |
| Development mode         | `--dev`             | `OPC_DEV`             |
| Development seed         | `--seed`            | `OPC_DEV_SEED`        |

When omitted for a file-backed database, Artifact, backup, and log roots default to `artifacts`, `backups`, and `logs` siblings of the database. An in-memory database requires all three directories explicitly, but verified backup creation still requires a file-backed database. The roots cannot be the database file or overlap each other.

Outside explicit development mode a non-empty session token is required. Every endpoint, including `/health`, then requires `Authorization: Bearer <token>`. Browser requests must carry an exact allowlisted `Origin`; native Tauri probes may omit browser fetch headers. The process binds only `127.0.0.1`.

After configuration succeeds, operational logs go to stderr and `<log-root>/opc-sidecar.log`. The file rotates before an entry would exceed 5 MiB and retains `opc-sidecar.log.1` through `.3`. Session-token literals and Bearer values are redacted at the final writer; access entries contain only request ID, method, route template, status, and duration. File logging failures fall back to stderr without preventing startup. Raw logs are intentionally excluded from the diagnostic package.

After migrations, Artifact reconciliation, and listening succeed, stdout receives one newline-terminated ready event:

```json
{
  "event": "ready",
  "status": "ok",
  "host": "127.0.0.1",
  "address": "127.0.0.1:49152",
  "url": "http://127.0.0.1:49152",
  "port": 49152,
  "pid": 1234,
  "version": "0.1.0",
  "app_version": "0.1.0",
  "api_version": "v1",
  "schema_version": 23
}
```

Writing `shutdown` to stdin drains active requests and checkpoints the WAL before closing SQLite.

## Implemented API

The Sidecar exposes:

- versioned non-sensitive workspace/general/appearance/focus settings with service defaults, strict full-object normalization, atomic optimistic updates, and value-free audit metadata;
- health, Task/Tag/Project/Client CRUD and queries, Task batch/reorder, project lifecycle, and today statistics; Task lists support the derived `status=active` filter plus `planned_state=scheduled/unscheduled` for complete Today grouping;
- Actor person management with Actor `ETag`, snapshot idempotency, and protected built-in owner/system records;
- Task Assignment query/create/reassign/end, using the containing Task's `If-Match` version;
- explicit Task `start`, `block`, `unblock`, `complete`, `cancel`, and `reopen` commands; the legacy `PATCH /tasks/:id/status` always returns `410 TASK_STATUS_ENDPOINT_DEPRECATED`;
- Task event history with actor summaries, request IDs, immutable snapshots, `command_seq`, and nullable Assignment/Submission/Artifact correlation IDs;
- Project Note idempotent creation, stable pagination, optimistic editing, reasoned soft deletion, immutable deleted history, archived-project read-only enforcement, and parent Project version propagation;
- Client fact CRUD with stable filtering/sorting, snapshot idempotency, aggregate `ETag`, Project association propagation, constrained hard deletion, and derived latest activity time;
- Client Activity note/meeting creation, stable pagination, optimistic editing, reasoned soft deletion, immutable deleted history, and parent Client version propagation;
- Client contact relationship query/link/unlink with aggregate concurrency, idempotent snapshots, atomic person creation, one active contact per Client, and immutable unlink history;
- Inbox Item create/query/detail, read/snooze/resolve/dismiss/reopen commands and immutable Inbox workflow history;
- Inbox Item–Task active/history relationships, server-derived progress, required-flag updates, reasoned soft unlinking, and active-relation protection for Task hard deletion;
- one-time local Reminder CRUD, optimistic concurrency, cancellation, startup compensation, periodic due scanning, and exactly-once Reminder-to-Inbox projection;
- persistent Focus Session start/pause/resume/heartbeat/stop/cancel/recovery commands, terminal history pagination, and timezone-aware today/period aggregation with streaks;
- T-18D D2 manual review, Submission, Artifact, and controlled file endpoints listed below.
- synchronous, idempotency-aware local backup creation, list, and full re-verification. Creation holds the maintenance write gate, snapshots SQLite with `VACUUM INTO`, copies the owned marker and every active controlled Task Artifact or Client Attachment through same-volume staging, checks hashes/database integrity/foreign keys/schema/identity, and atomically publishes a UUID package under the configured backup root.
- versioned business JSON and controlled-file business ZIP export/import for an empty same-schema workspace. Both imports require explicit confirmation and a verified rollback backup. ZIP preview verifies the manifest, business JSON, safe file set, size/SHA-256 and database metadata; apply publishes files without overwrite, verifies them before the database transaction commits, and compensates this import's files on database failure.

```text
GET    /api/v1/settings
PATCH  /api/v1/settings
GET    /api/v1/backups
GET    /api/v1/backups/restore-diagnostics
GET    /api/v1/exports/business-data
GET    /api/v1/exports/business-package
POST   /api/v1/imports/business-data/preview
POST   /api/v1/imports/business-data
POST   /api/v1/imports/business-package/preview
POST   /api/v1/imports/business-package
POST   /api/v1/backups
POST   /api/v1/backups/:id/verify
POST   /api/v1/backups/:id/drill
POST   /api/v1/backups/:id/restore
DELETE /api/v1/backups/:id?confirm=true
GET    /api/v1/projects/:id/notes
POST   /api/v1/projects/:id/notes
GET    /api/v1/projects/:id/artifacts
GET    /api/v1/projects/:id/attachments
POST   /api/v1/projects/:id/attachments
GET    /api/v1/project-notes/:id
PATCH  /api/v1/project-notes/:id
DELETE /api/v1/project-notes/:id?confirm=true
GET    /api/v1/project-attachments/:id
GET    /api/v1/project-attachments/:id/content
DELETE /api/v1/project-attachments/:id?confirm=true
POST   /api/v1/tasks/:id/submit-output
POST   /api/v1/tasks/:id/review
GET    /api/v1/tasks/:id/submissions
GET    /api/v1/tasks/:id/artifacts
GET    /api/v1/artifacts/:id
GET    /api/v1/artifacts/:id/content
DELETE /api/v1/artifacts/:id?confirm=true
GET    /api/v1/clients
GET    /api/v1/clients/:id/activities
POST   /api/v1/clients/:id/activities
GET    /api/v1/client-activities/:id
PATCH  /api/v1/client-activities/:id
DELETE /api/v1/client-activities/:id?confirm=true
GET    /api/v1/clients/:id/attachments
POST   /api/v1/clients/:id/attachments
GET    /api/v1/client-attachments/:id
GET    /api/v1/client-attachments/:id/content
DELETE /api/v1/client-attachments/:id?confirm=true
GET    /api/v1/clients/:id/actor-links
POST   /api/v1/clients/:id/actor-links
DELETE /api/v1/client-actor-links/:id?confirm=true
GET    /api/v1/focus-sessions?page=1&page_size=20&status=terminal
GET    /api/v1/stats/focus?date_from=2026-08-22&date_to=2026-08-28&timezone=UTC
POST   /api/v1/clients
GET    /api/v1/clients/:id
PATCH  /api/v1/clients/:id
DELETE /api/v1/clients/:id?confirm=true
GET    /api/v1/inbox-items/:id/tasks?page=1&page_size=50
GET    /api/v1/stats/inbox
POST   /api/v1/inbox-items/:id/tasks/:task_id
PATCH  /api/v1/inbox-items/:id/tasks/:task_id
DELETE /api/v1/inbox-items/:id/tasks/:task_id
POST   /api/v1/inbox-items/:id/split
POST   /api/v1/inbox-items/:id/force-resolve
GET    /api/v1/reminders
POST   /api/v1/reminders
GET    /api/v1/reminders/:id
PATCH  /api/v1/reminders/:id
DELETE /api/v1/reminders/:id
```

Successful resources use `{ "data": ... }`; lists add `meta`. Errors use `{ "code", "message", "request_id" }`. API timestamps are RFC 3339 UTC. Task, Assignment, lifecycle, output, review, Artifact deletion, and hard Task deletion writes use Task `If-Match`; Client Attachment upload/deletion and Client contact link/unlink use the containing Client `If-Match`. Stale versions return `409 VERSION_CONFLICT`. Retryable commands accept an optional stable `Idempotency-Key`, persist the normalized request hash and first response, replay the same request without repeating events or file writes, and reject key reuse with different input.

### Settings contract

`GET /api/v1/settings` always returns the four keys `workspace`, `general`, `appearance`, and `focus` in that order. A missing database row is represented by the service default with `stored=false`, `version=0`, and null update metadata; reading defaults does not insert rows. Stored rows include their normalized value, schema version 1, optimistic version, owner actor, and UTC update time.

`PATCH /api/v1/settings` accepts `{ "updates": [...] }` with 1–4 unique modules. Every update requires `key`, `expected_version`, and a complete `value` object. Creation requires expected version 0; subsequent writes require the exact current version. Unknown, missing, null non-nullable, or out-of-range fields are rejected. Workspace avatars are null or controlled `avatars/<uuid>.png|jpg|webp` references; Data URLs, tokens, API keys, and arbitrary paths are not accepted. All modules are saved in one transaction, so `409 SETTINGS_VERSION_CONFLICT` or any validation/database failure leaves every row unchanged. Each successful module appends one `settings_updated` Workflow Event containing only stored/version/schema metadata, never the setting value.

### Verified backup and restart-restore contract

`POST /api/v1/backups` freezes ordinary API traffic and background writers, creates a `VACUUM INTO` SQLite snapshot, copies the ownership marker and all active controlled Task Artifact and Client Attachment files, then verifies hashes, the exact file set, SQLite integrity/schema/identity, and controlled-file metadata before publishing a UUID package. `GET /api/v1/backups` lists recorded verification facts; `POST /api/v1/backups/:id/verify` performs the complete verification again.

`POST /api/v1/backups/:id/drill` does not replace live data. It verifies the source package again, copies it into a unique temporary data root, opens and migrates the copied database with the current migration runner, validates final SQLite facts, claims an isolated Artifact store, and checks every active file object. Database and lease handles are closed before the temporary root is removed.

`POST /api/v1/backups/:id/restore` requires the strict body `{ "confirm": true }`. Under the maintenance write gate it repeats the drill, creates and fully verifies an automatic rollback package of the current workspace, publishes a private pending package plus strict plan, then freezes ordinary v1 requests and background writers with `RESTORE_RESTART_REQUIRED`. Replaying the same target is safe; a different target is rejected while one is pending. The next Sidecar process applies the plan before opening the live database or Artifact lease: it verifies both target and rollback packages, migrates a prepared database copy, swaps the SQLite database (including WAL/SHM recovery paths) and complete `objects` directory through same-parent names, verifies the live result, and rolls back plus quarantines the plan on failure. A successful live verification atomically renames the pending directory to an applied commit marker before best-effort cleanup, so a cleanup warning cannot cause the restore to run twice. The desktop UI can request a safe application restart after the pending plan is published; browser development mode keeps the external Sidecar under developer control and shows a manual restart fallback.

`GET /api/v1/backups/restore-diagnostics` is a read-only, sanitized post-startup view of restore state. It combines an in-process applied result with a pending plan, residual applied cleanup markers, failed quarantines and invalid restore entries. The response contains only canonical backup IDs, the requested timestamp, state flags and counts; filesystem paths, cleanup errors and raw failure details are never returned. It does not delete or repair any entry. A pre-database live progress page remains a desktop-shell concern.

`DELETE /api/v1/backups/:id?confirm=true` permanently deletes one canonical UUID package without touching live data. It accepts valid or corrupt packages so users are not trapped with an invalid archive, but rejects symlink/reparse traversal and non-regular entries. The package is first atomically renamed to `.deleting-<id>` under the same backup root and the directory is synchronized; removal then resumes from that exact hidden path if an earlier attempt stopped after the rename. A pending restore freezes this route with the rest of the ordinary API.

`GET /api/v1/exports/business-data` returns an attachment using business-export format v1. It reads an explicit allowlist of business tables in one SQLite transaction with deterministic table/column/row structure. Controlled-file metadata is included while file bytes are deliberately omitted and summarized together. Session credentials, absolute machine paths, workspace identity, idempotency responses, tombstones, migrations and derived focus totals are not exported. The synchronous endpoint fails closed if an allowlisted table is unavailable.

`GET /api/v1/exports/business-package` takes the maintenance write gate and fully stages a business-package format v1 ZIP before serving it. `manifest.json` records source metadata plus the path, size and SHA-256 of `business-data.json` and every active controlled file; bodies use only `files/objects/<uuid>` and `files/avatars/<uuid>.<ext>`. File metadata is checked again while copying. A missing, changed or unsafe source fails the entire request and removes the staging ZIP. SQLite, workspace identity, the Artifact marker, session credentials, absolute paths and operational tables are excluded.

`POST /api/v1/imports/business-package/preview` accepts at most 2 GiB and 10,000 controlled files, stages the upload privately, and rejects duplicate, extra, unsafe, directory or symlink entries. It strictly verifies manifest/source totals, the embedded business export, each file body and its corresponding database metadata. `POST /api/v1/imports/business-package` requires `X-Import-Confirmation: replace-empty-workspace-with-controlled-files`, repeats preflight under the maintenance write gate, requires an empty target and terminal Focus state, creates a verified rollback backup, publishes files with no-replace semantics, and verifies disk bodies before committing the business transaction. A database failure removes files published by that attempt.

### Inbox Item–Task relationship contract

`GET /api/v1/inbox-items/:id/tasks` returns `{ "data": { "active", "history" }, "meta": { "page", "page_size", "total", "inbox_item_version", "progress" } }` and the current Inbox Item `ETag`. `active` is the complete position-ordered active set and is capped at 100; `history` alone is paginated newest-unlinked-first, so `meta.total` is the history total. Every active relation joins the current Task summary at read time. Progress is therefore derived from current Task status without copying Task state or propagating Task version changes into the Inbox Item version.

`GET /api/v1/stats/inbox` derives `pending`, `unread`, `tracking`, `blocked`, and `waiting_review` from currently visible active Inbox Items and active required Task relationships. `GET /api/v1/inbox-items` accepts `risk=tracking|blocked|waiting_review` for the matching deep links; the global unread total remains independent of list filters.

`POST /api/v1/inbox-items/:id/split` requires Inbox `If-Match` and optionally accepts `Idempotency-Key`. It atomically creates 1–20 ordered Tasks, batch-local parent links, tags, owner/person assignments, owner reviewers for manual-review Tasks, `created` Inbox relationships, and audit events. `POST /api/v1/inbox-items/:id/force-resolve` is restricted to `all_required_tasks_done` items and requires `{ "confirm": true, "reason": "..." }`; it records a forced terminal fact instead of pretending incomplete required Tasks are done.

POST and PATCH use `{ "is_required": boolean }`; DELETE uses `{ "reason": string }`, trimmed to 1–1,000 characters. All three mutation routes require the Inbox Item `If-Match`, accept an optional `Idempotency-Key`, return the updated Inbox Item, relation, progress, and `ETag`, and append exactly one `task_linked`, `task_requirement_changed`, or `task_unlinked` event when the fact changes. The first active relation moves `open` to `tracking`; removing the last active relation moves `tracking` to `open`. Reopen derives `tracking` when any active relation remains and `open` otherwise. Linking never automatically resolves an Inbox Item, creates an Assignment, or creates a Task.

An active Inbox relationship makes `DELETE /api/v1/tasks/:id` return `409 TASK_HAS_ACTIVE_INBOX_RELATIONS`. Unlink the relationship first; a later successful Task deletion sets the historical relation's nullable `task_id` to null through the foreign key while retaining immutable `task_ref_id`, `task_title_snapshot`, actors, timestamps, required flag, and unlink reason. A follow-up Task Artifact is a distinct source projection rather than a relationship: an active source returns `409 TASK_HAS_ACTIVE_INBOX_SOURCES`, while deleting after every source item is terminal coordinates `source_deleted_at` and a `source_deleted` event in the same Task-deletion transaction.

### One-time Reminder contract

`GET /api/v1/reminders` provides stable pagination plus `q`, `status`, and allow-listed sorting. POST creates a manual one-time Reminder whose RFC 3339 `trigger_at` must be in the future according to the server clock. Create accepts an optional `Idempotency-Key` and snapshots the first response. Detail, PATCH, and DELETE return or consume the Reminder ETag; PATCH can change title, summary, priority, and trigger time only while scheduled. DELETE is a reasoned soft cancellation, also supports idempotent replay, and never removes the row.

Router startup synchronously projects overdue scheduled rows before readiness, then scans every 15 seconds in stable batches of 100. Each projection transaction finds or creates one `kind=reminder` Inbox Item using `reminder:<id>:due`, appends the system Inbox event, marks the Reminder fired with the Inbox ID, and appends the system Reminder event. The unique event key, conditional Reminder update, and transaction make repeated scans and restarts safe. Native OS notifications, recurrence, remote delivery, and business-source Reminder creation are not implemented.

### Client facts contract

Client resources contain `id`, `name`, nullable `contact_name/email/phone/notes`, `status`, derived `project_count`, `version`, and timestamps. The server trims text, stores blank optional values as JSON/SQL `null`, rejects disallowed control characters and unknown JSON fields, and enforces these limits: name 1–200 Unicode characters, contact name 200, email 320 and one valid mailbox, phone 50, notes 10,000. Status is `active`, `lead`, or `inactive`; creation defaults to `active`.

`GET /clients` defaults to page 1 with 50 rows and allows at most 100. `q` is at most 200 characters and searches name, contact name, email, and phone with escaped LIKE semantics. `status` is optional. `sort` accepts comma-separated `name`, `contact_name`, `status`, `project_count`, `created_at`, and `updated_at`, prefixed by `-` for descending order; the default is `-updated_at` and every order appends `id ASC` for stable pagination. Project lists still exclude archived rows by default; callers needing a Client's complete association history use `GET /projects?client_id=:id&include_archived=true`.

Client creation optionally accepts `Idempotency-Key`, hashes the normalized request, and stores the first `201` response snapshot. The same key and normalized body replay that snapshot even after later edits or deletion; different input returns `409 IDEMPOTENCY_CONFLICT`. Create, detail, and update return the Client `ETag`. PATCH and DELETE require `If-Match`: missing, malformed, and stale versions return 428 `VERSION_REQUIRED`, 400 `INVALID_VERSION`, and 409 `VERSION_CONFLICT` respectively.

Hard deletion is `DELETE /clients/:id?confirm=true`, requires the latest version, and is allowed only for an `inactive` Client. An Invoice reference returns `409 CLIENT_HAS_INVOICES` without changing the Client, Project links, or either aggregate version. Project references use `ON DELETE SET NULL`; successful deletion returns `deleted_id` and `detached_projects`. Project attach, move, detach, and deletion bump the affected Client aggregate version. Client name changes and deletion continue to bump linked Project aggregate versions.

### Manual review and output contract

Task creation and non-lifecycle PATCH accept `review_policy: "none" | "manual"`. A changed policy is allowed only while the Task is `todo` and no Submission has ever existed. Direct `complete` remains limited to `none` tasks.

A manual Task may submit output only from `todo` or `in_progress`, with one active assignee and an active built-in owner reviewer. The server derives Artifact `produced_by_actor_id` from the active assignee. The built-in owner is the Submission submitter and Artifact recorder, and performs review, withdrawal, and deletion actions. Clients must not send a producer ID.

JSON submissions use:

```json
{
  "summary": "What was delivered",
  "artifacts": [
    {
      "client_ref": "draft-1",
      "storage_kind": "text",
      "name": "Notes",
      "content_text": "Result",
      "requires_followup": false
    }
  ]
}
```

The four mutually exclusive payload fields are `content_text` for `text`, `reference_url` for `link`, `structured_json` for a JSON object, and a `file_field` reference for `file`. File submissions use `multipart/form-data`: the one text field named `manifest` must be the first part, and every later part must be a uniquely and exactly referenced file part. A multipart submission may mix file and non-file Artifacts. Unknown JSON fields, unused file parts, duplicate `client_ref`/`file_field`, or mismatched payload fields are rejected.

Limits enforced by the API are:

- summary: at most 10,000 Unicode characters; summary or at least one Artifact is required;
- at most 20 Artifacts per Submission;
- `client_ref`: 1–100 safe characters and unique within the Submission;
- Artifact name: 1–255 safe characters;
- text: non-empty and at most 500,000 Unicode characters;
- link: at most 4,096 bytes, HTTP(S), with a host and no embedded credentials;
- structured payload: a JSON object whose encoded value is at most 1 MiB;
- strict JSON body and multipart `manifest`: at most 1 MiB each;
- file: non-empty and at most 50 MiB; complete multipart request at most 100 MiB.

The HTTP server allows 180 seconds for request reads and response writes. The web client applies a shorter 120-second end-to-end timeout to output upload and file download, leaving room for the server to terminate and report failures; normal small JSON requests retain their shorter client timeout.

Submission changes the Task to `waiting_review`. Review body is `{ "decision": "accept" | "request_changes", "reason"?: string }`; a change request requires a non-empty reason and review/delete reasons are limited to 1,000 characters. Accept marks the Submission `accepted`, completes the Task, and ends all active Assignments in the same transaction. Requesting changes marks it `changes_requested` and returns the Task to `in_progress`. Cancelling a Task waiting for review marks the pending Submission `withdrawn`; accept, changes requested, and cancel retain `current_submission_id`. Reopen alone clears the pointer while retaining history.

Every submitted Artifact with `requires_followup=true` is projected in the same transaction to exactly one `kind=event`, `source_entity_type=task_artifact` Inbox Item with stable key `task-artifact:<artifact-id>:followup`. Its immutable payload snapshots only the Artifact identity/name/type, source Task identity/title, Submission identity/sequence, and optional Project identity/name; it never copies Artifact content, controlled-file paths, Task status, or Assignment state. Idempotent submission replay does not duplicate the source, and `requires_followup=false` creates no Inbox Item.

Submission and Artifact lists default to 50 items and allow at most 100, return `{ "data": [...], "meta": { "page", "page_size", "total", "task_version" } }` plus the Task `ETag`, and sort newest Submission first. Artifact listing accepts `submission_id` and `include_deleted=true`. Every Artifact summary and detail carries required `submission_status` (`pending_review | accepted | changes_requested | withdrawn`) derived by joining its parent Submission; clients use this authoritative value to suppress pending-review deletion. Summary responses omit text/link/structured payloads and controlled relative paths; `GET /artifacts/:id` returns the payload on demand. A deleted Artifact remains readable as metadata but all payload fields are null.

File content is served only through the authenticated content endpoint after size and SHA-256 verification. Successful responses are attachments with safe UTF-8 filenames plus `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`, and SHA-256 `ETag`. Missing or mismatched files are refused and update `integrity_status` to `missing` or `mismatch`; valid reads set it to `verified`.

Artifact deletion requires `confirm=true`, Task `If-Match`, and a 1–1,000 character reason. It is forbidden while the owning Submission is `pending_review`, and an active projected Inbox source returns `409 ARTIFACT_HAS_ACTIVE_INBOX_SOURCE`. Once the source Inbox Item is resolved or dismissed, deletion coordinates its `source_deleted_at`, version and audit event before soft-deleting the Artifact in the same transaction. Metadata remains auditable; the same transaction writes an immutable `artifact_deletion_tombstones` row before an existing file moves through controlled trash. Task hard deletion writes the same tombstone with `deletion_scope = task` before cascading the aggregate. Tombstones deliberately survive aggregate deletion so startup recovery can distinguish authorized deletion from an unknown candidate. If the physical object is already missing, confirmed deletion still succeeds; Artifact soft deletion records `integrity_status = missing` with a check time.

## Controlled Artifact storage

The Sidecar exclusively manages this layout:

```text
artifacts/
  .opc-artifact-store-v1
  .opc-artifact-store.lock
  .staging/
  objects/
  .trash/
  .quarantine/
```

Schema v9 creates one `workspace_identity` row with an immutable `database_id` and a nullable `artifact_store_id` that may be set exactly once. An unbound database may claim an empty root by writing `.opc-artifact-store-v1` as exact JSON containing `format_version`, that database ID, and a new `store_id`; the Sidecar then persists the same store ID in the database. Every later startup requires both identities and the canonical marker bytes to match, so a database cannot silently switch roots and a root cannot be adopted by another database. Before reconciliation, the Sidecar acquires a non-blocking process-level exclusive lease through `.opc-artifact-store.lock` and holds it for the router lifetime; a second Sidecar targeting the same root fails startup instead of risking concurrent file coordination. The root and child directories must be regular directories and cannot traverse symbolic links or Windows reparse points.

Stored file names are server-generated lowercase Artifact UUIDs; SQLite stores the fixed `objects/<artifact-id>` relative path, never an arbitrary client path. Uploads stream through `.staging/` while calculating MIME, size, and SHA-256, then use a no-replace hard-link promotion into `objects/`. If the enclosing Submission transaction reports an error, compensation first queries SQLite and removes only an object proven unreferenced; an inconclusive query preserves the object for startup reconciliation, avoiding data loss after an ambiguous COMMIT result. Marker creation, staged-file flush, object promotion, moves, removals, and critical directory entries are durably synchronized before success is reported. Startup removes regular staging orphans, reconciles interrupted known trash moves, and moves unreferenced controlled object/trash candidates into `.quarantine/` instead of permanently deleting unknown data. Before restoring trash for an active Artifact it verifies expected size and SHA-256; mismatches are quarantined and persisted as `integrity_status = mismatch`. Unexpected directories, links, and unrecognized names are never followed or recursively removed.

## Database and verification

Numbered SQL migrations are embedded from `internal/database/migrations/` and recorded in `schema_migrations`. Startup uses one physical SQLite connection and enables foreign keys, WAL, and a 5-second busy timeout. Add schema changes as new numbered migrations; never edit a shipped migration.

The current schema is v28. Migrations 009–014 add controlled Artifact/Submission, Client aggregate facts, Focus intervals, manual Inbox Items, Inbox–Task relationships, and one-time Reminders. Migration 015 adds indexes and guards for required-Task reconciliation. Migration 016 adds an initially empty `app_settings` table and its guards. Migration 017 adds constrained, versioned Task saved views. Migrations 018–022 add Client activities/attachments/contact relationships and Project notes/attachments. Migrations 023–026 add Task Artifact, blocked, due and system-maintenance Inbox-source guards. Migration 027 adds controlled Workspace Avatars; migration 028 adds Project-completion Inbox projection and deletion coordination. These migrations do not seed demo data. Future changes must start at `029_*`; never edit a shipped migration. A migration that deletes, rebuilds, or irreversibly rewrites existing facts must include `-- migration: destructive` in its consecutive header directives. Existing workspaces stop before the first such migration, publish a fully verified SQLite and controlled-file rollback package, then reopen and continue; backup failure leaves destructive SQL unapplied and prevents ready.

Each v13 relationship stores an immutable relation ID, Inbox ID, stable `task_ref_id`, nullable live `task_id`, title snapshot, `linked | created` relation type, required flag, positive position, link actor/time, and all-or-none unlink actor/time/reason. The current public POST API creates only `linked` relationships to existing Tasks. Active rows have all unlink fields null and a live Task; history rows have all three unlink facts present. Duplicate active Inbox/Task pairs and active positions are rejected. Relationship rows cannot be hard-deleted while their Inbox Item exists.

The foreign-key-off migration path runs on a fixed connection, validates `PRAGMA foreign_key_check` before commit, and restores foreign keys on success or rollback.

```powershell
go test ./... -count=1
go test ./internal/database -count=10
go vet ./...
go build ./cmd/server
```

At the PRD v9.10 / schema v29 baseline, regression coverage includes historical migration preservation; Task/Actor/Assignment/Submission/Artifact lifecycles; Client and Project activities, contacts, notes and controlled attachments; Focus interval reports; Inbox/Reminder/source projection; settings, controlled avatars, and a versioned 1–100 GiB low-space threshold; verified SQLite+controlled-file backup creation, verification, drills, restart restore, sanitized startup-result diagnostics and deletion; deterministic business JSON export; empty-workspace JSON and controlled-file ZIP import with rollback, manifest/body/integrity checks and file compensation; diagnostics; runtime database failure projection with a sanitized durable journal fallback; proactive low-space monitoring with private physical-volume deduplication and pathless on-demand capacity status across database, controlled-file and backup roots; canonical WebView-to-Sidecar request correlation; a global sanitized startup-failure gate; and strict frontend contracts. The Tauri shell exposes safe restart, pathless opening of its own log directory, and a whitelist-only rotating lifecycle log; browser development mode intentionally leaves the externally managed Sidecar under developer control. Pre-database backup selection/progress UI, volume-level trends, in-process bounded Sidecar respawn, non-empty/cross-schema merge, native notifications, Agent Runtime, finance and release packaging remain separate future work.
