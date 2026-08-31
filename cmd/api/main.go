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

	"analytics-ingestion/internal/ingest"

	log "github.com/sirupsen/logrus"
)

/*
Main function
Starts HTTP server and endpoints
*/
func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})

	// Creates new ingestion service and handler
	ingestService := ingest.NewService()
	handler := ingest.NewHandler(ingestService)

	// Mux for routing event to service
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/event", handler.Ingest)

	fmt.Println("Starting Event Ingestion...")

	// Server startup
	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		log.Error(err)
	}
}
