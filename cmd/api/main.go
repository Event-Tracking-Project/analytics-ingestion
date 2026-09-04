/*
analytics-ingestion main file.
Wires the object graph and starts the HTTP server.
Endpoints:
  - POST /v1/event -> ingests a single event
  - POST /v1/batch -> ingests a batch of events
*/
package main

import (
	"fmt"
	"net/http"

	"analytics-ingestion/internal/ingest"
	"analytics-ingestion/internal/storage"

	log "github.com/sirupsen/logrus"
)

/*
Main function
Builds the store, service and handler, then starts the HTTP server.
main is the only place that decides which Store implementation is used;
every layer below it depends on the storage.Store interface instead.
*/
func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})

	// In-memory for now: events are lost when the process exits.
	store := storage.NewInMemoryStore()

	// Creates new ingestion service and handler
	ingestService := ingest.NewService(store)
	handler := ingest.NewHandler(ingestService)

	// Mux for routing event to service
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/event", handler.Ingest)
	mux.HandleFunc("POST /v1/batch", handler.BatchIngest)

	fmt.Println("Starting Event Ingestion...")

	// Server startup
	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		log.Error(err)
	}
}
