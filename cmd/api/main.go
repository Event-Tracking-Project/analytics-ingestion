/*
analytics-ingestion main file.
Wires the object graph and starts the HTTP server.
Endpoints:
  - POST   /v1/event       -> ingests a single event
  - POST   /v1/batch       -> ingests a batch of events
  - GET    /v1/events      -> lists the calling project's events
  - GET    /v1/events/{id} -> reads one event
  - DELETE /v1/events/{id} -> removes one event

Every route is authenticated, and reads are scoped to the project the API key
resolves to rather than to anything in the request.
*/
package main

import (
	"fmt"
	"net/http"
	"os"

	"analytics-ingestion/internal/auth"
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

	// Development stub: one key, one project. Swapping this for a database
	// resolver is the only change needed to make authentication real; nothing
	// below main knows which Resolver it is talking to.
	apiKey := os.Getenv("DEV_API_KEY")
	if apiKey == "" {
		apiKey = "dev_key_local_only"
	}

	resolver := auth.NewStaticResolver(map[string]auth.Identity{
		apiKey: {ProjectID: "proj_dev", OrgID: "org_dev"},
	})

	log.Warn("authentication is a development stub: keys come from a hardcoded table, not a database")

	authenticate := auth.Middleware(resolver)

	// Mux for routing event to service.
	// Routes are wrapped individually rather than wrapping the mux, so an
	// unauthenticated endpoint (health, metrics) can be added later without
	// having to carve an exception out of a blanket middleware.
	mux := http.NewServeMux()
	mux.Handle("POST /v1/event", authenticate(http.HandlerFunc(handler.Ingest)))
	mux.Handle("POST /v1/batch", authenticate(http.HandlerFunc(handler.BatchIngest)))
	mux.Handle("GET /v1/events", authenticate(http.HandlerFunc(handler.ListEvents)))
	mux.Handle("GET /v1/events/{id}", authenticate(http.HandlerFunc(handler.GetEvent)))
	mux.Handle("DELETE /v1/events/{id}", authenticate(http.HandlerFunc(handler.DeleteEvent)))

	fmt.Println("Starting Event Ingestion...")

	// Server startup
	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		log.Error(err)
	}
}
