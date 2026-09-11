/*
cmd/stress/types.go
Contains structs for stress reports data
*/
package main

import "time"

// Worker stats struct
type workerStats struct {
	Processed int `json:"processed"`
	Validated int `json:"validated"`
}

// Report struct
type report struct {
	StartedAt          time.Time              `json:"started_at"`
	FinishedAt         time.Time              `json:"finished_at"`
	URL                string                 `json:"url"`
	Batches            int                    `json:"batches"`
	BatchSize          int                    `json:"batch_size"`
	RequestedEvents    int                    `json:"requested_events"`
	SubmittedBytes     int64                  `json:"submitted_bytes"`
	Concurrency        int                    `json:"concurrency"`
	SuccessfulRequests int                    `json:"successful_requests"`
	FailedRequests     int                    `json:"failed_requests"`
	StatusCodes        map[string]int         `json:"status_codes"`
	DurationSeconds    float64                `json:"duration_seconds"`
	RequestsPerSecond  float64                `json:"requests_per_second"`
	EventsPerSecond    float64                `json:"events_per_second"`
	LatencyMS          latencyStats           `json:"latency_ms"`
	Workers            map[string]workerStats `json:"workers,omitempty"`
}

// Struct for latenc
type latencyStats struct {
	Min float64 `json:"min"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}
