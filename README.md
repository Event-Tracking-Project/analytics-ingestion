# Analytics Ingestion

A Go HTTP microservice for receiving product analytics events from an SDK. It decodes incoming JSON, validates the required event fields, and accepts valid events for downstream processing.

## Contents

- [Overview](#overview)
- [Current capabilities](#current-capabilities)
- [Requirements](#requirements)
- [Run locally](#run-locally)
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

The intended processing flow is:

```text
SDK -> ingestion API -> in-memory queue -> workers -> in-memory storage
```

The API and workers currently run in the same process so they can share the in-memory queue and storage implementations. Batch validation is performed by workers before valid events are stored.

> **Temporary infrastructure:** The in-memory queue and storage are for local development and testing only. They are process-local and non-persistent, so queued or stored data is lost when the API process stops. They will be replaced with Redis and persistent storage as the project evolves.

## Current capabilities

- Accepts JSON events through `POST /v1/event`.
- Accepts batches of events through `POST /v1/batch`.
- Validates individual events synchronously through the event endpoint.
- Validates batch events in workers, removing invalid events while retaining valid events.
- Enforces the configured maximum batch size.
- Returns `202 Accepted` when a request is decoded and accepted for processing.
- Returns `400 Bad Request` for malformed JSON or batches over the configured limit.
- Emits structured ingestion and validation logs with Logrus.
- Starts workers at startup or on demand, according to configuration.

## Requirements

- Go 1.26.3 or a compatible Go installation, as specified in [`go.mod`](go.mod)
- `curl` or another HTTP client for sending test events

No database or external service is currently required to run the application.

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

3. Download Go dependencies and start the API.

   ```bash
   go mod download
   go run ./cmd/api
   ```

   The service listens on the host and port configured in `configs/config.yaml` (default: `http://localhost:8080`).

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
| `workers.count` | Maximum number of workers that may run concurrently. |
| `workers.start_on_demand` | Starts one worker for an incoming batch and stops it after processing when `true`; starts all configured workers at startup when `false`. |

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

## Workers and future deployment

At present, `cmd/api` creates the queue and storage, starts `worker.Manager`,
and runs the workers in the same process:

```text
cmd/api
  ├── HTTP API
  ├── in-memory queue
  ├── in-memory storage
  └── worker.Manager
      └── worker routines
```

The repository also contains `cmd/worker` as a future standalone worker
service entrypoint. It is not used with the current in-memory setup because a
separate process cannot access the API process's memory. Once Redis and
persistent storage are added, the intended deployment will be:

```text
cmd/api -> Redis queue -> cmd/worker
                    └── persistent storage
```

In that deployment, the API will publish batches to Redis, while the separate
worker service will create and manage workers that consume and process those
batches independently. Graceful shutdown will allow active workers to finish
their current batch before the worker process exits.

## Roadmap

The repository contains placeholders for the next ingestion stages:

- Replace the in-memory queue with Redis.
- Replace in-memory storage with persistent database storage.
- Run `cmd/worker` as a separate service from `cmd/api`.
- Add graceful shutdown and draining for active workers.

## Project structure

```text
cmd/api/main.go              HTTP server, dependencies, and worker startup
cmd/worker/main.go           Future standalone worker-service entrypoint
internal/ingest/handler.go   JSON decoding and HTTP responses
internal/ingest/service.go   Batch-size enforcement and queue publishing
internal/event/event.go      Event data model
internal/event/validation.go Event validation and valid-event filtering
internal/queue/memory.go     Temporary in-memory queue
internal/storage/memory.go   Temporary in-memory storage
internal/worker/worker.go    Batch processing and event storage
internal/worker/manager.go   Worker lifecycle and concurrency management
```

## Contributing

Contributions are welcome. Please keep changes focused, format Go code with `gofmt`, and verify the application still starts with:

```bash
go run ./cmd/api
```

## License

This project is licensed under the [Apache License 2.0](LICENSE).
