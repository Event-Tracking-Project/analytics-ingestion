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

	// Start workers based on config
	workerManager := worker.NewManager(cfg.Workers, q, s)
	workerManager.Start(context.Background())

	// Creates new ingestion service and handler with ingestion config
	ingestService := ingest.NewService(cfg.Ingestion, q, workerManager.StartOne)
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
