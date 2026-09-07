package main

import (
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"analytics-ingestion/internal/worker"
	"context"

	"log"
)

func main() {
	/*
		cfg, err := config.Load("configs/config.yaml")
		if err != nil {
			log.Fatal(err)
		}
	*/

	q := queue.NewMemory()
	s := storage.NewMemory()

	w := worker.New("worker-1", q, s)

	if err := w.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
