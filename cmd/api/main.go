package main

import (
	"fmt"
	"net/http"

	"analytics-ingestion/internal/ingest"

	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
		ForceColors:   true,
	})

	ingestService := ingest.NewService()
	handler := ingest.NewHandler(ingestService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", handler.Ingest)

	fmt.Println("Starting Event Ingestion...")

	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		log.Error(err)
	}
}
