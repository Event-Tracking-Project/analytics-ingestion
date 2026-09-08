/*
analytics-ingestion main file.
Takes in singular event data and batched events (eventually)
1 API Endpoint
  - POST /v1/event -> Takes in singular event for testing
  - POST /v1/batch -> Takes in batch of events for processing
*/
package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/ingest"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"analytics-ingestion/internal/worker"

	log "github.com/sirupsen/logrus"
)

/*
Main function
Starts HTTP server and endpoints
*/
func main() {

	// Load config settings
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	// Configure logging for service
	if err := config.ConfigureLogging(cfg.Logging); err != nil {
		log.Fatal(err)
	}

	// Initialize queue (Testing Workers)
	q := queue.NewMemory()
	s := storage.NewMemory()

	// Worker starts based on config. Will start a worker if batch is ingested
	// Integrate Redis queue and db later
	var workerMu sync.Mutex
	activeWorkers := 0
	startWorkers := func() {
		workerMu.Lock()
		if activeWorkers >= cfg.Workers.WorkerCount {
			workerMu.Unlock()
			return
		}

		activeWorkers++
		workerID := activeWorkers
		workerMu.Unlock()

		name := fmt.Sprintf("worker-%d", workerID)
		w := worker.New(name, q, s)
		go func() {

			// Lowers active workers if worker goes down
			defer func() {
				workerMu.Lock()
				activeWorkers--
				workerMu.Unlock()
			}()

			var err error

			// If statement to run workers once if start on demand is true
			if cfg.Workers.StartOnDemand {
				err = w.RunOnce(context.Background())
			} else {
				err = w.Run(context.Background())
			}
			if err != nil {
				log.WithError(err).WithField("worker_id", name).Error("Worker stopped")
			}
		}()
	}

	// Starts all workers to reduce startup time and keep them up
	if !cfg.Workers.StartOnDemand {
		for i := 0; i < cfg.Workers.WorkerCount; i++ {
			startWorkers()
		}
	}

	// Creates new ingestion service and handler with ingestion config
	ingestService := ingest.NewService(cfg.Ingestion, q, startWorkers)
	handler := ingest.NewHandler(ingestService)

	// Mux for routing event to service
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/event", handler.Ingest)
	mux.HandleFunc("POST /v1/batch", handler.BatchIngest)

	log.Info("Starting Event Ingestion...")

	// Server startup
	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	err = http.ListenAndServe(address, mux)
	if err != nil {
		log.Error(err)
	}
}
