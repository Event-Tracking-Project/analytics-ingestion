# Contract Changes

What the ingestion API now expects, why, and what would have to change in
[`README.md`](README.md) to describe it. The README still documents the older
contract and has **not** been updated; this file is the record of the gap.

Reference: [`../docs/analytics-ingestion-event-contract.md`](../docs/analytics-ingestion-event-contract.md).

---

## Summary

| # | Change | Breaking for SDK clients |
| --- | --- | --- |
| 1 | Every request needs `Authorization: Bearer <PROJECT_API_KEY>` | Yes |
| 2 | `project_id` / `org_id` come from the API key and must not be sent | Yes |
| 3 | `received_at` is server-owned and must not be sent | Yes |
| 4 | `event_id` is required | Yes |
| 5 | Server-owned fields serialize as `project_id` / `org_id`, not `projectid` / `orgid` | Responses only |
| 6 | New status codes: `401`, `404`, `500` | Yes |
| 7 | Batch ingestion exists at `POST /v1/batch` | No (additive) |

---

## 1. Authentication is now required

`Authorization: Bearer <PROJECT_API_KEY>` on every request. A missing,
malformed, or unrecognized header returns `401` with a `WWW-Authenticate: Bearer`
challenge, and the request never reaches a handler.

The key resolver is an interface (`internal/auth.Resolver`) with a
development-only implementation: a single key read from `$DEV_API_KEY`,
defaulting to `dev_key_local_only`, resolving to project `proj_dev` in
organization `org_dev`. Keys are held in a hardcoded map, not a database, and
the service logs a warning at startup saying so.

## 2. Ownership is derived, not declared

The contract states the client "should **not** be trusted to provide
authoritative `project_id` or `org_id` values." Previously both arrived in the
request body and were taken at face value, which meant project scoping was
advisory: any caller could write an event into any project by naming it.

Both fields are now resolved from the API key. A request that carries either
one is **rejected with `400`** rather than having the value silently dropped —
a client that sends the field learns it is not theirs to set, instead of
receiving a `202` and a discarded value.

Both spellings are refused, `project_id` and `projectid`, so an SDK following
the contract document and one following the old README are both told.

For batches, one identity is applied to every member, so a single request
cannot fan out across projects.

## 3. `received_at` is server-owned

Stamped from the server clock on arrival. Sending it is rejected with `400`,
same as the ownership fields. Every event in one batch shares a single reading,
since they arrived together.

## 4. `event_id` is required

Client-generated, per the contract. It exists for idempotency on retry, so the
server cannot mint it: a server-generated ID would differ on every retry and
defeat the purpose.

## 5. Field naming

Server-derived fields serialize as `project_id` and `org_id`, matching the
contract document. This affects **responses and storage only** — there is no
longer any request field with these names, so nothing an SDK sends is affected.

## 6. Unknown fields are tolerated

Deliberately narrower than rejecting every unrecognized key: only the fields
the server owns are refused. An unknown field this service has never heard of
is ignored, so an older deployment does not reject a newer SDK that added one.

## 7. Batch endpoint

`POST /v1/batch` takes `batch_id` and an `events` array. Validation is
all-or-nothing: one invalid member rejects the whole batch before anything is
stored.

> `batch_id` is required by this service but does not appear in the contract
> document, and the contract puts the batch endpoint at `/v1/events/batch`
> rather than `/v1/batch`. Both are unresolved — see [Still open](#still-open).

---

## What would change in the README

Section by section, against the current file.

### "Current capabilities"

Add authentication and storage; correct the validation list, which still names
project ID and organization ID as validated client input.

```diff
-- Accepts JSON events through `POST /v1/event`.
-- Validates event name, timestamp, project ID, and organization ID.
+- Accepts JSON events through `POST /v1/event` and batches through `POST /v1/batch`.
+- Authenticates each request against a project API key and derives `project_id` and `org_id` from it.
+- Validates event ID, event name, and timestamp.
+- Stores accepted events in memory, scoped per project.
 - Returns `202 Accepted` for a valid event.
-- Returns `400 Bad Request` for malformed JSON or invalid event data.
+- Returns `400 Bad Request` for malformed JSON or invalid event data, and `401 Unauthorized` for a missing or unknown API key.
```

### "Requirements"

The line "No database or external service is currently required" stays true,
but needs a note that authentication is a stub, naming the dev key and the
project it resolves to. Otherwise a reader has no way to make a request work.

### "Send an event"

The example is currently wrong three ways: no `Authorization` header, no
`event_id`, and it sends `projectid` / `orgid`. Run verbatim against the
current service it returns `401`; with a key added, `400` twice over.

```diff
 curl --request POST http://localhost:8080/v1/event \
+  --header 'Authorization: Bearer dev_key_local_only' \
   --header 'Content-Type: application/json' \
   --data '{
+    "event_id": "evt_01JABC123",
     "event": "button_clicked",
-    "timestamp": 1767225600000,
-    "projectid": "project_123",
-    "orgid": "org_456",
+    "timestamp": 1767225600,
     "user_id": "user_789",
```

Worth a sentence afterwards explaining the absence of `project_id` — a reader
who knows the domain model will otherwise assume it was left out by mistake.

> The timestamp also drops three digits above. The current example sends
> milliseconds; the contract specifies Unix **seconds**. Validation only checks
> the value is positive, so both are accepted today and this is not strictly a
> change — but the example should not model the wrong unit. See
> [Still open](#still-open).

### "Event schema"

The single table mixes fields of different provenance, which is what made the
old trust model easy to get wrong. It should split in two:

- **Request fields** — `event_id` (new, required), `event`, `timestamp`,
  `user_id`, `anonymous_id`, `session_id`, `properties`, `context`.
- **Server-derived fields** — `project_id`, `org_id`, `received_at`, marked
  must-not-be-sent, with the source of each.

This line must go, since it now describes the opposite of the behavior:

```diff
-Use the field names above exactly: the API expects `projectid` and `orgid` without underscores.
```

A short "Batches" subsection covering `POST /v1/batch`, `batch_id`, and
all-or-nothing validation would fill the remaining gap.

### "Validation and responses"

Three rows exist; three more are needed, and one is now inaccurate.

```diff
-| `400 Bad Request` with a validation message | A required field is missing or invalid. | Provide a non-empty `event`, `projectid`, and `orgid`, plus a positive `timestamp`. |
+| `400 Bad Request` with a validation message | A required field is missing or invalid. | Provide a non-empty `event_id` and `event`, plus a positive `timestamp`. |
+| `400 Bad Request` naming a server-owned field | The payload set `project_id`, `org_id` or `received_at`. | Remove the field; the server derives it. |
+| `401 Unauthorized` | The `Authorization` header is missing, malformed, or the key is unknown. | Send `Authorization: Bearer <PROJECT_API_KEY>`. |
+| `500 Internal Server Error` | The service failed to handle the request. | Not caused by the request; check the server logs. |
```

`404` belongs here too once the read and delete endpoints exist.

### "Roadmap"

"Accept batched event payloads from SDKs" is done and should be replaced by
what is actually next: a real key store, and the read/delete endpoints.

### "Project structure"

Five files are listed; there are now ten. Missing: the whole `internal/auth`
package, `internal/ingest/dto.go`, and both `internal/storage` files.

### "Contributing"

Currently suggests verifying with `go run ./cmd/api`, which only proves the
process starts. The repo now has a linter config and tests:

```bash
gofmt -l . && go vet ./... && golangci-lint run ./... && go test -race ./...
```

---

## Still open

Decisions that affect the contract and have not been made:

1. **Duplicate `event_id`** — the store is last-write-wins, pinned by
   `TestSaveOverwrites`. First-write-wins is arguably what an idempotency key
   means: a retry should be a no-op, not an overwrite. Maps to
   `INSERT ... ON CONFLICT (event_id) DO NOTHING`.
2. **Batch route** — this service uses `/v1/batch`; the contract says
   `/v1/events/batch`.
3. **`batch_id`** — required here, absent from the contract.
4. **Timestamp precision** — the contract says Unix seconds; the README example
   sends milliseconds; validation enforces neither.
5. **Identity requirement** — the contract says an event should normally carry
   at least one of `user_id` or `anonymous_id`. Nothing enforces that yet.
