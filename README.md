# Analytics Ingestion

A Go HTTP microservice for receiving product analytics events from an SDK. It decodes incoming JSON, validates the required event fields, and accepts valid events for downstream processing.

## Contents

- [Overview](#overview)
- [Current capabilities](#current-capabilities)
- [Requirements](#requirements)
- [Run locally](#run-locally)
- [Send an event](#send-an-event)
- [Event schema](#event-schema)
- [Validation and responses](#validation-and-responses)
- [Roadmap](#roadmap)
- [Project structure](#project-structure)
- [Contributing](#contributing)
- [License](#license)

## Overview

Analytics Ingestion is the entry point for product-analytics data. SDK clients submit events to an HTTP endpoint; the service converts the payload into an event model and checks that it includes the identifiers and timestamp needed for ingestion.

The intended processing flow is:

```text
SDK -> ingestion API -> validation -> worker/queue -> database
```

At present, the service implements the API and validation stages. Queueing, worker routines, batch ingestion, and database writes are planned extension points.

## Current capabilities

- Accepts JSON events through `POST /v1/event`.
- Validates event name, timestamp, project ID, and organization ID.
- Returns `202 Accepted` for a valid event.
- Returns `400 Bad Request` for malformed JSON or invalid event data.
- Emits structured ingestion logs with Logrus.

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

2. Download Go dependencies and start the API.

   ```bash
   go mod download
   go run ./cmd/api
   ```

   The service listens on `http://localhost:8080`.

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

## Validation and responses

| Response | Meaning | Resolution |
| --- | --- | --- |
| `202 Accepted` | The event was decoded and passed validation. | The event is accepted for downstream processing. |
| `400 Bad Request: invalid JSON` | The request body is not valid JSON. | Send a valid JSON object with `Content-Type: application/json`. |
| `400 Bad Request` with a validation message | A required field is missing or invalid. | Provide a non-empty `event`, `projectid`, and `orgid`, plus a positive `timestamp`. |

## Roadmap

The repository contains placeholders for the next ingestion stages:

- Accept batched event payloads from SDKs.
- Publish accepted events to a queue.
- Process queued events with Go worker routines.
- Persist processed events to a database.

## Project structure

```text
cmd/api/main.go              HTTP server and route registration
internal/ingest/handler.go   JSON decoding and HTTP responses
internal/ingest/service.go   Ingestion orchestration and validation call
internal/event/event.go      Event data model
internal/event/validation.go Required-field validation
```

## Contributing

Contributions are welcome. Please keep changes focused, format Go code with `gofmt`, and verify the application still starts with:

```bash
go run ./cmd/api
```

## License

This project is licensed under the [Apache License 2.0](LICENSE).
