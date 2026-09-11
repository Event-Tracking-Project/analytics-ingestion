# Analytics Ingestion

A Go HTTP microservice for receiving product analytics events from an SDK. It decodes incoming JSON, validates the required event fields, and accepts valid events for downstream processing.

## Contents

- [Overview](#overview)
- [Current capabilities](#current-capabilities)
- [Requirements](#requirements)
- [Run locally](#run-locally)
- [Run Redis and PostgreSQL](#run-redis-and-postgresql)
- [Start the services](#start-the-services)
- [Test the complete pipeline](#test-the-complete-pipeline)
- [Test idempotency](#test-idempotency)
- [Test failure recovery](#test-failure-recovery)
- [Stress testing](#stress-testing)
- [Reset local data](#reset-local-data)
- [Configuration](#configuration)
- [Send an event](#send-an-event)
- [Send a batch](#send-a-batch)
- [Event schema](#event-schema)
- [Validation, limits, and responses](#validation-limits-and-responses)
- [Roadmap](#roadmap)
- [Project structure](#project-structure)
- [Contributing](#contributing)
- [License](#license)

## Overview

Analytics Ingestion is the entry point for product-analytics data. SDK clients submit events to an HTTP endpoint; the service decodes requests, enforces ingestion limits, queues batches, and processes them with worker routines.

The current processing flow is:

```text
SDK -> API -> Redis Stream -> worker process -> PostgreSQL
```

The API and workers run as separate processes. The API publishes batches to a
Redis Stream, and the standalone worker process consumes, validates, and
acknowledges them. Batch validation is performed by workers before valid
events are stored.

Redis provides delivery, consumer groups, acknowledgements, and recovery for
queued batches. PostgreSQL stores validated events durably. The worker
acknowledges a Redis message only after the PostgreSQL transaction commits.

## Current capabilities

- Accepts JSON events through `POST /v1/event`.
- Accepts batches of events through `POST /v1/batch`.
- Validates individual events synchronously through the event endpoint.
- Validates batch events in workers, removing invalid events while retaining valid events.
- Enforces the configured maximum batch size.
- Returns `202 Accepted` when a request is decoded and accepted for processing.
- Returns `400 Bad Request` for malformed JSON or batches over the configured limit.
- Emits structured ingestion and validation logs with Logrus.
- Runs API and workers independently through Redis.
- Stores validated batches and events in PostgreSQL.
- Prevents duplicate event rows when a Redis message is retried.
- Rejects reuse of a batch ID when the payload contents differ.
- Recovers Redis Stream messages left pending by an inactive worker.
- Gracefully drains queued work when the worker process shuts down.

## Requirements

- Go 1.26.3 or a compatible Go installation, as specified in [`go.mod`](go.mod)
- Redis 7 or compatible Redis server
- PostgreSQL 16 or compatible PostgreSQL server
- Docker (optional, for running Redis locally)
- `curl` or another HTTP client for sending test events

Redis is required for the API and worker processes to communicate. PostgreSQL
is required by the worker process for persistent event storage.

## Run locally

1. Clone the repository and enter the project directory.

   ```bash
   git clone <repository-url>
   cd analytics-ingestion
   ```

2. Copy the example configuration and adjust it for your environment:

   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

3. Download Go dependencies:

   ```bash
   go mod download
   ```

4. Start Redis and PostgreSQL by following
   [Run Redis and PostgreSQL](#run-redis-and-postgresql).

5. Start the services by following [Start the services](#start-the-services).

## Run Redis and PostgreSQL

The default configuration expects Redis at `localhost:6379` and PostgreSQL at
`localhost:5432`. The following commands start local Docker containers.

```bash
docker run --name analytics-redis -p 6379:6379 -d redis:7
```

```bash
docker run --name analytics-postgres \
  -e POSTGRES_DB=analytics \
  -e POSTGRES_USER=analytics \
  -e POSTGRES_PASSWORD=analytics \
  -p 5432:5432 \
  -d postgres:16
```

Verify that Redis is available:

```bash
docker exec analytics-redis redis-cli ping
```

Expected output:

```text
PONG
```

Verify PostgreSQL:

```bash
docker exec analytics-postgres \
  pg_isready -U analytics -d analytics
```

Apply the schema:

```bash
docker exec -i analytics-postgres \
  psql -U analytics -d analytics \
  < migrations/001_initial_schema.sql
```

Verify the database tables:

```bash
docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c '\dt'
```

If the container already exists, start it with:

```bash
docker start analytics-redis
```

```bash
docker start analytics-postgres
```

To stop it:

```bash
docker stop analytics-redis analytics-postgres
```

## Start the services

Start the worker process first in one terminal:

```bash
go run ./cmd/worker
```

The worker process connects to Redis, creates the configured consumer group
when necessary, starts the configured number of workers, and waits for
batches. Worker lifecycle messages are written to the configured log file,
including each worker ID:

```text
Worker started worker_id=worker-1
Worker stopped worker_id=worker-1
```

With the default configuration, follow worker startup, processing, and
shutdown messages from another terminal:

```bash
tail -f ./logs/worker-stress.log
```

Start the API in a second terminal:

```bash
go run ./cmd/api
```

The API listens on the configured host and port (default:
`http://localhost:8080`). It only publishes batches; it does not start
workers. The worker process remains independent if the API is stopped.

Both processes must use the same Redis settings in
[`configs/config.yaml`](configs/config.yaml). Copy
[`configs/config.example.yaml`](configs/config.example.yaml) if the active
configuration does not exist.

## Configuration

The application loads `configs/config.yaml` once at startup. Use
[`configs/config.example.yaml`](configs/config.example.yaml) as a starting point.

| Setting | Description |
| --- | --- |
| `server.host` | HTTP server bind host. |
| `server.port` | HTTP server bind port. |
| `logging.enabled` | Enables or disables application logging. |
| `logging.level` | Log level: `debug`, `info`, `warn`, or `error`. |
| `logging.destination` | Log destination: `stdout` or `file`. |
| `logging.file.path` | Path used when the destination is `file`. The default worker configuration uses `./logs/worker-stress.log`. |
| `logging.failed_events` | Configures failed-event logging behavior. |
| `ingestion.max_batch_size` | Maximum number of events allowed in one batch request. |
| `redis.address` | Redis server address. |
| `redis.stream` | Redis Stream used for queued batches. |
| `redis.consumer_group` | Redis consumer group shared by worker processes. |
| `database.host` | PostgreSQL server host. |
| `database.port` | PostgreSQL server port. |
| `database.name` | PostgreSQL database name. |
| `database.user` | PostgreSQL user. |
| `database.password` | PostgreSQL password. Do not commit production credentials. |
| `database.ssl_mode` | PostgreSQL SSL mode. |
| `database.max_connections` | Maximum worker database pool connections. |
| `database.min_connections` | Minimum worker database pool connections. |
| `workers.count` | Number of workers started by the standalone worker process. |
| `workers.start_on_demand` | Retained for compatibility; standalone workers start at process startup. |

## Send an event

With the service running, submit an event:

```bash
curl --request POST http://localhost:8080/v1/event \
  --header 'Content-Type: application/json' \
  --data '{
    "event": "button_clicked",
    "timestamp": 1767225600000,
    "projectid": "project_123",
    "orgid": "org_456",
    "user_id": "user_789",
    "anonymous_id": "anon_abc",
    "session_id": "session_def",
    "properties": {
      "button_name": "start-trial"
    },
    "context": {
      "page": "/pricing"
    }
  }'
```

A valid request receives `202 Accepted` with an empty response body.

## Send a batch

Submit multiple events through the batch endpoint:

```bash
curl --request POST http://localhost:8080/v1/batch \
  --header 'Content-Type: application/json' \
  --data '{
    "batch_id": "batch_001",
    "events": [
      {
        "event": "button_clicked",
        "timestamp": 1767225600000,
        "projectid": "project_123",
        "orgid": "org_456",
        "user_id": "user_789",
        "session_id": "session_def",
        "properties": {
          "button_name": "start-trial"
        },
        "context": {
          "page": "/pricing"
        }
      },
      {
        "event": "page_viewed",
        "timestamp": 1767225660000,
        "projectid": "project_123",
        "orgid": "org_456"
      }
    ]
  }'
```

A batch should include a `batch_id` and one or more events. It is queued and
accepted before worker-side event validation. Each event is validated
independently by a worker. Invalid events are logged and excluded from
storage; valid events continue through processing.

## Test the complete pipeline

Start the worker first:

```bash
go run ./cmd/worker
```

Start the API in another terminal:

```bash
go run ./cmd/api
```

Submit a batch:

```bash
curl --request POST http://localhost:8080/v1/batch \
  --header 'Content-Type: application/json' \
  --data '{
    "batch_id": "pipeline_test_001",
    "events": [{
      "event": "button_clicked",
      "timestamp": 1767225600000,
      "projectid": "project_123",
      "orgid": "org_456",
      "funnelid": "signup_funnel",
      "properties": {"button_name": "start-trial"}
    }]
  }'
```

The response should be `202 Accepted`. Query PostgreSQL:

```bash
docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "SELECT batch_id, payload_hash FROM batches WHERE batch_id = 'pipeline_test_001';"

docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "SELECT batch_id, event_index, name, project_id, org_id, funnel_id, properties FROM events WHERE batch_id = 'pipeline_test_001';"
```

Confirm the Redis message was acknowledged:

```bash
docker exec analytics-redis \
  redis-cli XPENDING analytics:batches analytics-workers
```

The pending count should normally be `0`.

## Test idempotency

Submit the exact same batch again with the same `batch_id` and event contents.
It should not create duplicate rows:

```bash
docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "SELECT COUNT(*) AS batches FROM batches WHERE batch_id = 'pipeline_test_001'; SELECT COUNT(*) AS events FROM events WHERE batch_id = 'pipeline_test_001';"
```

Expected counts are one batch and one event. Reuse the same `batch_id` with
different event contents to test conflict detection; the worker should reject
the conflicting payload instead of acknowledging it.

## Test failure recovery

Stop PostgreSQL while the worker and API are running:

```bash
docker stop analytics-postgres
```

Submit a new batch and inspect pending Redis messages:

```bash
docker exec analytics-redis \
  redis-cli XPENDING analytics:batches analytics-workers
```

The message should remain pending because PostgreSQL storage failed. Restart
PostgreSQL:

```bash
docker start analytics-postgres
```

After the pending-message recovery threshold, the worker should store the
batch and acknowledge the Redis message.

## Stress testing

The repository includes a Go stress runner at
[`cmd/stress/main.go`](cmd/stress/main.go). It submits batches concurrently and
writes a timestamped JSON report containing:

- Request and event throughput.
- Total payload bytes submitted.
- Successful and failed requests.
- HTTP status counts.
- Minimum, median, p95, p99, and maximum request latency.
- Per-worker validated and processed batch counts from the worker log.
- Worker IDs and lifecycle messages (`Worker started` and `Worker stopped`) in
  the worker log.
- Optional HTML detail reports and a sortable HTML dashboard for comparing
  multiple stress-test runs.

For a benchmark that does not write to PostgreSQL, temporarily set the worker
storage backend in `configs/config.yaml` to:

```yaml
storage:
  backend: "memory"
```

The API still uses Redis, while the worker stores results only in its process
memory. This is appropriate for throughput and queue/worker testing, but it
does not measure database write performance or persistence.

The default runtime configuration writes worker logs to
`./logs/worker-stress.log`, which is also the stress runner's default input.
The runner waits five seconds after HTTP submission before reading the log so
queued batches have time to finish. You can configure both values explicitly:

```yaml
logging:
  enabled: true
  level: "info"
  destination: "file"
  file:
    path: "./logs/worker-stress.log"
```

The `-worker-log` flag defaults to `logs/worker-stress.log`, so it is not
required for the standard configuration. The report's `workers` object is
keyed by worker ID and includes the number of batches each worker validated
and processed. The counts are read from the log after the `-worker-wait`
period:

```json
"workers": {
  "worker-1": {
    "processed": 25,
    "validated": 25
  }
}
```

The stress runner measures the worker counts from the log file; it does not
change worker concurrency. Worker concurrency is controlled independently by
`workers.count` in `configs/config.yaml`.

To generate a visual report in addition to JSON, add `-html`:

```bash
go run ./cmd/stress \
  -batches 1000 \
  -batch-size 25 \
  -concurrency 8 \
  -html
```

The dashboard is pre-created at
[`stress-results/index.html`](stress-results/index.html), so it is available
before the first benchmark. It uses a Kimbie Dark-inspired interface with a
scrollable list of test runs on the left and the selected run's metrics on the
right. Running with `-html` creates a Kimbie Dark report detail page and
refreshes the dashboard's `reports.json` data manifest:

```text
stress-results/
├── index.html
├── reports.json
├── stress-20260910-230000.json
└── stress-20260910-230000.html
```

The dashboard loads every report JSON file currently present in
`stress-results/` through `reports.json`. It does not create reports when the
Python server starts. Use the left-side search field to filter runs and select
a run to view:

- Requests per second and events per second.
- P95 latency and success rate.
- Endpoint, batch size, client concurrency, duration, and report links.
- Validated and processed counts for each worker.

Each selected run links to its JSON report and its detailed HTML report. The
dashboard itself is tracked in the repository and is not regenerated by the
stress runner; only `reports.json` and individual generated reports are
updated.

Because the dashboard loads `reports.json`, serve the directory over HTTP
instead of opening the page directly with `file://`. Start the server from
the repository root with the explicit absolute path:

```bash
python3 -m http.server 8000 \
  --directory /home/tmeers/Event-Tracking-Project/analytics-ingestion/stress-results
```

Then open <http://localhost:8000/index.html>. Verify that the server is using
the expected manifest with:

```bash
curl http://localhost:8000/reports.json
```

The response should match
`/home/tmeers/Event-Tracking-Project/analytics-ingestion/stress-results/reports.json`.
If the browser shows reports that are not in that file, stop any older
`http.server` process and force-refresh the page. Use a separate `-output-dir`
to keep different benchmark campaigns isolated; the pre-created dashboard
applies to the default `stress-results/` directory.

Start the worker and API:

```bash
go run ./cmd/worker
go run ./cmd/api
```

Run a small smoke benchmark:

```bash
go run ./cmd/stress \
  -batches 100 \
  -batch-size 10 \
  -concurrency 4
```

Use `-worker-log ""` to disable worker-log parsing, or change the drain wait
with `-worker-wait 10s` for a larger queue.

Run a larger throughput benchmark:

```bash
go run ./cmd/stress \
  -batches 10000 \
  -batch-size 100 \
  -concurrency 32 \
  -worker-log ./logs/worker-stress.log
```

Generate a mixed-validity workload:

```bash
go run ./cmd/stress \
  -batches 5000 \
  -batch-size 50 \
  -concurrency 16 \
  -invalid-rate 0.10 \
  -worker-log ./logs/worker-stress.log
```

Reports are written to `stress-results/` with names such as:

```text
stress-results/stress-20260910-230000.json
```

The runner measures HTTP acceptance latency. Worker processing latency is not
the same as request latency because the API responds after publishing to
Redis. Use the worker logs, Redis pending counts, and total processed worker
counts to evaluate queue drain time and downstream throughput.

For repeatable comparisons, keep one variable changing at a time:

```text
1. Fix batch size and vary concurrency.
2. Fix concurrency and vary batch size.
3. Increase total batches until queue depth or latency changes sharply.
4. Repeat with different worker counts.
5. Repeat with invalid-event rates such as 0%, 10%, and 50%.
```

Before each independent run, clear old Redis work, worker logs, and report
files if you want the dashboard to show only that run:

```bash
docker exec analytics-redis redis-cli FLUSHDB
rm -f ./logs/worker-stress.log
rm -f ./stress-results/stress-*.json ./stress-results/stress-*.html
```

The stress runner does not automatically clear Redis or old report files
because clearing them during an active run could destroy queued work or
previous results. `reports.json` is rebuilt from the report JSON files that
remain in `stress-results/`.

## Query stored events

```bash
docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "SELECT * FROM events WHERE project_id = 'project_123' AND name = 'button_clicked';"

docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "SELECT * FROM events WHERE funnel_id = 'signup_funnel' ORDER BY timestamp_ms;"

docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "SELECT * FROM events WHERE properties @> '{\"button_name\":\"start-trial\"}';"
```

## Event schema

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `event` | string | Yes | Name of the product event. |
| `timestamp` | integer | Yes | Positive event timestamp. |
| `projectid` | string | Yes | Project that owns the event. |
| `orgid` | string | Yes | Organization that owns the event. |
| `user_id` | string | No | Identified user ID. |
| `anonymous_id` | string | No | Anonymous visitor ID. |
| `session_id` | string | No | Client session ID. |
| `properties` | object | No | Event-specific attributes. |
| `context` | object | No | Request or application context. |

Use the field names above exactly: the API expects `projectid` and `orgid` without underscores.

## Validation, limits, and responses

| Response | Meaning | Resolution |
| --- | --- | --- |
| `202 Accepted` | The request was decoded and accepted for processing. | The event or batch is accepted for processing. Batch validation and storage happen in a worker. |
| `400 Bad Request: invalid JSON` | The request body is not valid JSON. | Send a valid JSON object with `Content-Type: application/json`. |
| `400 Bad Request` with a validation message | A required field is invalid for a single event, or the batch exceeds the configured limit. | For single events, provide a non-empty `event`, `projectid`, and `orgid` plus a positive `timestamp`. Keep batches within `ingestion.max_batch_size`. |

## Check Redis status

Inspect the Redis Stream:

```bash
docker exec analytics-redis redis-cli XRANGE analytics:batches - +
```

Inspect the consumer group:

```bash
docker exec analytics-redis \
  redis-cli XINFO GROUPS analytics:batches
```

Inspect worker consumers:

```bash
docker exec analytics-redis \
  redis-cli XINFO CONSUMERS analytics:batches analytics-workers
```

Inspect pending, unacknowledged messages:

```bash
docker exec analytics-redis \
  redis-cli XPENDING analytics:batches analytics-workers
```

After successful processing, the pending count should normally be `0`.

To clear data from a disposable local Redis database:

```bash
docker exec analytics-redis redis-cli FLUSHDB
```

`FLUSHDB` deletes all data in the selected Redis database. Do not use it
against a shared or production Redis instance.

## Reset local data

Clear PostgreSQL rows while preserving tables and indexes:

```bash
docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "TRUNCATE TABLE events, batches RESTART IDENTITY CASCADE;"
```

Clear Redis data in a disposable local instance:

```bash
docker exec analytics-redis redis-cli FLUSHDB
```

If the schema changed, recreate the tables and reapply the migration:

```bash
docker exec analytics-postgres \
  psql -U analytics -d analytics \
  -c "DROP TABLE IF EXISTS events, batches CASCADE;"

docker exec -i analytics-postgres \
  psql -U analytics -d analytics \
  < migrations/001_initial_schema.sql
```

## Workers and deployment

The current deployment is:

```text
cmd/api
  ├── HTTP API
  └── Redis publisher

cmd/worker
  ├── Redis consumer group
  ├── worker.Manager
  └── worker routines
```

When the worker receives `SIGINT` or `SIGTERM`, it finishes active work and
continues consuming queued Redis messages during its graceful drain period.
It exits when the queue is idle or the shutdown timeout is reached.

The worker writes valid events to PostgreSQL. Redis remains the delivery
mechanism and PostgreSQL remains the durable event store:

```text
cmd/api -> Redis Stream -> cmd/worker -> persistent database
```

## Roadmap

The next planned stages are:

- Add versioned migration tooling.
- Add a dead-letter strategy for permanently rejected batches.
- Add a core API for querying stored events.

## Project structure

```text
cmd/api/main.go              HTTP server and Redis publisher
cmd/worker/main.go           Standalone Redis worker-service entrypoint
cmd/stress/main.go           Concurrent benchmark and JSON report generator
internal/ingest/handler.go   JSON decoding and HTTP responses
internal/ingest/service.go   Batch-size enforcement and queue publishing
internal/event/event.go      Event data model
internal/event/validation.go Event validation and valid-event filtering
internal/queue/redis.go      Redis Stream queue implementation
internal/storage/postgres.go PostgreSQL storage and idempotent writes
internal/storage/memory.go   Optional in-memory storage for local tests
 migrations/001_initial_schema.sql PostgreSQL tables and indexes
internal/worker/worker.go    Batch processing and event storage
internal/worker/manager.go   Worker lifecycle and concurrency management
```

## Contributing

Contributions are welcome. Please keep changes focused, format Go code with `gofmt`, and verify the application still starts with:

```bash
go run ./cmd/api
```

For end-to-end processing, start Redis and run both `cmd/worker` and
`cmd/api` as described above.

## License

This project is licensed under the [Apache License 2.0](LICENSE).
