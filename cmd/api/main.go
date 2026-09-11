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
	"os"
	"os/signal"
	"syscall"
	"time"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/ingest"
	"analytics-ingestion/internal/queue"

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

	// Initialize Redis Queue
	q, err := queue.NewRedis(cfg.Redis)
	if err != nil {
		log.Fatal(err)
	}
	defer q.Close()

	// Creates new ingestion service and handler with ingestion config
	ingestService := ingest.NewService(cfg.Ingestion, q, nil)
	handler := ingest.NewHandler(ingestService)

	// Mux for routing event to service
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/event", handler.Ingest)
	mux.HandleFunc("POST /v1/batch", handler.BatchIngest)

	log.Info("Starting Event Ingestion...")

	// Server startup
	address := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    address,
		Handler: mux,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			log.Error(err)
		}
	case <-stop:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.WithError(err).Error("API shutdown failed")
		}
	}
}
