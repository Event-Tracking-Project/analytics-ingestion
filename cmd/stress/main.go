/*
cmd/stres/main.go
This file contains functions for stress testing ingestion service and workers
Uses several parameters such as batches, batch size, and concurrency
Read documentation for more info
*/

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type eventPayload struct {
	Name       string         `json:"event"`
	Timestamp  int64          `json:"timestamp"`
	ProjectID  string         `json:"projectid"`
	OrgID      string         `json:"orgid"`
	FunnelID   string         `json:"funnelid"`
	Properties map[string]any `json:"properties"`
}

type batchPayload struct {
	BatchID string         `json:"batch_id"`
	Events  []eventPayload `json:"events"`
}

type workerStats struct {
	Processed int `json:"processed"`
	Validated int `json:"validated"`
}

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

type latencyStats struct {
	Min float64 `json:"min"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

func main() {
	url := flag.String("url", "http://localhost:8080/v1/batch", "batch endpoint")
	batches := flag.Int("batches", 1000, "number of batches to submit")
	batchSize := flag.Int("batch-size", 10, "events per batch")
	concurrency := flag.Int("concurrency", 8, "concurrent HTTP requests")
	timeout := flag.Duration("timeout", 30*time.Second, "HTTP request timeout")
	invalidRate := flag.Float64("invalid-rate", 0, "fraction of events made invalid, from 0 to 1")
	outputDir := flag.String("output-dir", "stress-results", "directory for JSON reports")
	workerLog := flag.String("worker-log", "logs/worker-stress.log", "worker log to parse per-worker counts")
	workerWait := flag.Duration("worker-wait", 5*time.Second, "time to wait for workers after HTTP submission")
	flag.Parse()

	if *batches <= 0 || *batchSize <= 0 || *concurrency <= 0 || *invalidRate < 0 || *invalidRate > 1 || *workerWait < 0 {
		fmt.Fprintln(os.Stderr, "batches, batch-size, and concurrency must be positive; invalid-rate must be between 0 and 1; worker-wait must not be negative")
		os.Exit(2)
	}

	started := time.Now().UTC()
	client := &http.Client{Timeout: *timeout}
	latencies := make([]float64, 0, *batches)
	statusCodes := make(map[string]int)
	var mu sync.Mutex
	var successful, failed int
	var submittedBytes int64
	var nextBatch int64
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup

	for i := 0; i < *batches; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			id := atomic.AddInt64(&nextBatch, 1)
			payload, err := makeBatch(id, *batchSize, *invalidRate)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			body, err := json.Marshal(payload)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			request, err := http.NewRequest(http.MethodPost, *url, bytes.NewReader(body))
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			request.Header.Set("Content-Type", "application/json")

			requestStart := time.Now()
			response, err := client.Do(request)
			latency := time.Since(requestStart).Seconds() * 1000
			if err != nil {
				mu.Lock()
				failed++
				latencies = append(latencies, latency)
				mu.Unlock()
				return
			}
			response.Body.Close()

			status := strconv.Itoa(response.StatusCode)
			mu.Lock()
			statusCodes[status]++
			latencies = append(latencies, latency)
			submittedBytes += int64(len(body))
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				successful++
			} else {
				failed++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if *workerLog != "" && *workerWait > 0 {
		time.Sleep(*workerWait)
	}
	finished := time.Now().UTC()

	duration := finished.Sub(started).Seconds()
	if duration == 0 {
		duration = 0.000001
	}
	sort.Float64s(latencies)

	result := report{
		StartedAt:          started,
		FinishedAt:         finished,
		URL:                *url,
		Batches:            *batches,
		BatchSize:          *batchSize,
		RequestedEvents:    *batches * *batchSize,
		SubmittedBytes:     submittedBytes,
		Concurrency:        *concurrency,
		SuccessfulRequests: successful,
		FailedRequests:     failed,
		StatusCodes:        statusCodes,
		DurationSeconds:    duration,
		RequestsPerSecond:  float64(*batches) / duration,
		EventsPerSecond:    float64(*batches**batchSize) / duration,
		LatencyMS:          summarize(latencies),
		Workers:            parseWorkerLog(*workerLog),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	outputPath := filepath.Join(*outputDir, "stress-"+finished.Format("20060102-150405")+".json")
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("Wrote %s\n%s\n", outputPath, data)
}

func makeBatch(id int64, size int, invalidRate float64) (batchPayload, error) {
	events := make([]eventPayload, size)
	for i := range events {
		name := "stress_event"
		project := "stress_project"
		org := "stress_org"
		if invalidRate > 0 && float64((id+int64(i))%1000)/1000 < invalidRate {
			name = ""
			project = ""
		}
		events[i] = eventPayload{
			Name: name, Timestamp: time.Now().UnixMilli(),
			ProjectID: project, OrgID: org, FunnelID: "stress_funnel",
			Properties: map[string]any{"sequence": i},
		}
	}
	return batchPayload{
		BatchID: fmt.Sprintf("stress-%d-%d", time.Now().UnixNano(), id),
		Events:  events,
	}, nil
}

func summarize(values []float64) latencyStats {
	if len(values) == 0 {
		return latencyStats{}
	}
	return latencyStats{
		Min: values[0], P50: percentile(values, 0.50),
		P95: percentile(values, 0.95), P99: percentile(values, 0.99),
		Max: values[len(values)-1],
	}
}

func percentile(values []float64, ratio float64) float64 {
	index := int(float64(len(values)-1) * ratio)
	return values[index]
}

func parseWorkerLog(path string) map[string]workerStats {
	result := make(map[string]workerStats)
	if path == "" {
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	workerID := regexp.MustCompile(`worker_id=(\S+)`)
	for _, line := range bytes.Split(data, []byte("\n")) {
		match := workerID.FindSubmatch(line)
		if len(match) != 2 {
			continue
		}

		name := string(match[1])
		stats := result[name]
		if strings.Contains(string(line), "Worker processed batch") {
			stats.Processed++
		}
		if strings.Contains(string(line), "Worker validated batch") {
			stats.Validated++
		}
		result[name] = stats
	}
	return result
}
