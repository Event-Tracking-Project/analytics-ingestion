/*
cmd/stress/main.go
Command-line entrypoint for the ingestion stress test.
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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type stressOptions struct {
	url         string
	batches     int
	batchSize   int
	concurrency int
	timeout     time.Duration
	invalidRate float64
	outputDir   string
	workerLog   string
	workerWait  time.Duration
	htmlOutput  bool
}

func main() {
	options := parseOptions()
	if err := validateOptions(options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	result := runStressTest(options)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := os.MkdirAll(options.outputDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	outputPath := filepath.Join(options.outputDir, "stress-"+result.FinishedAt.Format("20060102-150405.000000000")+".json")
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %s\n%s\n", outputPath, data)

	if options.htmlOutput {
		htmlPath := filepath.Join(options.outputDir, strings.TrimSuffix(filepath.Base(outputPath), ".json")+".html")
		if err := writeHTMLReport(htmlPath, result, data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := writeReportManifest(options.outputDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("Wrote %s\nUpdated %s\n", htmlPath, filepath.Join(options.outputDir, "reports.json"))
	}

	if options.workerLog != "" {
		if _, err := os.Stat(options.workerLog); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: worker log %q was not found; worker metrics are unavailable\n", options.workerLog)
		}
	}
}

func parseOptions() stressOptions {
	var options stressOptions
	flag.StringVar(&options.url, "url", "http://localhost:8080/v1/batch", "batch endpoint")
	flag.IntVar(&options.batches, "batches", 1000, "number of batches to submit")
	flag.IntVar(&options.batchSize, "batch-size", 10, "events per batch")
	flag.IntVar(&options.concurrency, "concurrency", 8, "concurrent HTTP requests")
	flag.DurationVar(&options.timeout, "timeout", 30*time.Second, "HTTP request timeout")
	flag.Float64Var(&options.invalidRate, "invalid-rate", 0, "fraction of events made invalid, from 0 to 1")
	flag.StringVar(&options.outputDir, "output-dir", "stress-results", "directory for JSON reports")
	flag.StringVar(&options.workerLog, "worker-log", "logs/worker-stress.log", "worker log to parse per-worker counts")
	flag.DurationVar(&options.workerWait, "worker-wait", 5*time.Second, "time to wait for workers after HTTP submission")
	flag.BoolVar(&options.htmlOutput, "html", false, "also generate an HTML report and sortable index")
	flag.Parse()
	return options
}

func validateOptions(options stressOptions) error {
	if options.batches <= 0 || options.batchSize <= 0 || options.concurrency <= 0 {
		return fmt.Errorf("batches, batch-size, and concurrency must be positive")
	}
	if options.invalidRate < 0 || options.invalidRate > 1 {
		return fmt.Errorf("invalid-rate must be between 0 and 1")
	}
	if options.workerWait < 0 {
		return fmt.Errorf("worker-wait must not be negative")
	}
	return nil
}

func runStressTest(options stressOptions) report {
	started := time.Now().UTC()
	client := &http.Client{Timeout: options.timeout}
	latencies := make([]float64, 0, options.batches)
	statusCodes := make(map[string]int)
	var mu sync.Mutex
	var successful, failed int
	var submittedBytes int64
	var nextBatch int64
	sem := make(chan struct{}, options.concurrency)
	var wg sync.WaitGroup

	for i := 0; i < options.batches; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			id := atomic.AddInt64(&nextBatch, 1)
			payload, err := makeBatch(id, options.batchSize, options.invalidRate)
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
			request, err := http.NewRequest(http.MethodPost, options.url, bytes.NewReader(body))
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
			mu.Lock()
			latencies = append(latencies, latency)
			if err != nil {
				failed++
				mu.Unlock()
				return
			}
			response.Body.Close()
			status := strconv.Itoa(response.StatusCode)
			statusCodes[status]++
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
	if options.workerLog != "" && options.workerWait > 0 {
		time.Sleep(options.workerWait)
	}

	finished := time.Now().UTC()
	duration := finished.Sub(started).Seconds()
	if duration == 0 {
		duration = 0.000001
	}
	sortLatencies(latencies)
	return report{
		StartedAt: started, FinishedAt: finished, URL: options.url,
		Batches: options.batches, BatchSize: options.batchSize,
		RequestedEvents: options.batches * options.batchSize,
		SubmittedBytes:  submittedBytes, Concurrency: options.concurrency,
		SuccessfulRequests: successful, FailedRequests: failed,
		StatusCodes: statusCodes, DurationSeconds: duration,
		RequestsPerSecond: float64(options.batches) / duration,
		EventsPerSecond:   float64(options.batches*options.batchSize) / duration,
		LatencyMS:         summarize(latencies), Workers: parseWorkerLog(options.workerLog, started),
	}
}
