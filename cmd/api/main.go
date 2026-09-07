/*
analytics-ingestion main file.
Takes in singular event data and batched events (eventually)
1 API Endpoint
  - POST /v1/event -> Takes in singular event for testing
*/
package main

import (
	"fmt"
	"net/http"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/ingest"

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

	// Creates new ingestion service and handler
	ingestService := ingest.NewService()
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
