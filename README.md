# Analytics Ingestion

A Go HTTP microservice for receiving product analytics events from an SDK. It decodes incoming JSON, validates the required event fields, and accepts valid events for downstream processing.

## Contents

- [Overview](#overview)
- [Current capabilities](#current-capabilities)
- [Requirements](#requirements)
- [Run locally](#run-locally)
- [Run Redis](#run-redis)
- [Start the services](#start-the-services)
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
SDK -> API -> Redis Stream -> worker process -> in-memory storage
```

The API and workers run as separate processes. The API publishes batches to a
Redis Stream, and the standalone worker process consumes, validates, and
acknowledges them. Batch validation is performed by workers before valid
events are stored.

> **Temporary storage:** Worker storage is currently in memory for development
> and testing. It is process-local and non-persistent, so stored events are
> lost when the worker process stops. A shared database will be added later.
> Redis is currently used for the queue.

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
- Recovers Redis Stream messages left pending by an inactive worker.
- Gracefully drains queued work when the worker process shuts down.

## Requirements

- Go 1.26.3 or a compatible Go installation, as specified in [`go.mod`](go.mod)
- Redis 7 or compatible Redis server
- Docker (optional, for running Redis locally)
- `curl` or another HTTP client for sending test events

No database is currently required. Redis is required for the API and worker
processes to communicate.

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

4. Start Redis by following [Run Redis](#run-redis).

5. Start the services by following [Start the services](#start-the-services).

## Run Redis

The default configuration expects Redis at `localhost:6379`. To run a local
Redis 7 instance with Docker:

```bash
docker run --name analytics-redis -p 6379:6379 -d redis:7
```

Verify that Redis is available:

```bash
docker exec analytics-redis redis-cli ping
```

Expected output:

```text
PONG
```

If the container already exists, start it with:

```bash
docker start analytics-redis
```

To stop it:

```bash
docker stop analytics-redis
```

## Start the services

Start the worker process first in one terminal:

```bash
go run ./cmd/worker
```

The worker process connects to Redis, creates the configured consumer group
when necessary, starts the configured number of workers, and waits for
batches.

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
| `logging.file.path` | Path used when the destination is `file`. |
| `logging.failed_events` | Configures failed-event logging behavior. |
| `ingestion.max_batch_size` | Maximum number of events allowed in one batch request. |
| `redis.address` | Redis server address. |
| `redis.stream` | Redis Stream used for queued batches. |
| `redis.consumer_group` | Redis consumer group shared by worker processes. |
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

The worker currently writes valid events to temporary in-memory storage.
Because that storage belongs only to the worker process, the data is lost when
the worker exits. The intended future deployment adds persistent shared
storage:

```text
cmd/api -> Redis Stream -> cmd/worker -> persistent database
```

## Roadmap

The next planned stage is:

- Replace in-memory storage with persistent database storage.

## Project structure

```text
cmd/api/main.go              HTTP server and Redis publisher
cmd/worker/main.go           Standalone Redis worker-service entrypoint
internal/ingest/handler.go   JSON decoding and HTTP responses
internal/ingest/service.go   Batch-size enforcement and queue publishing
internal/event/event.go      Event data model
internal/event/validation.go Event validation and valid-event filtering
internal/queue/redis.go      Redis Stream queue implementation
internal/storage/memory.go   Temporary in-memory storage
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
