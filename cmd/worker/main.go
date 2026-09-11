/*
cmd/worker/main.go
Worker seperate process
Currently un used until redis queue implementation
Meant for server fail safe support
*/
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"analytics-ingestion/internal/worker"

	log "github.com/sirupsen/logrus"
)

func main() {
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	if err := config.ConfigureLogging(cfg.Logging); err != nil {
		log.Fatal(err)
	}

	q, err := queue.NewRedis(cfg.Redis)
	if err != nil {
		log.Fatal(err)
	}
	s := storage.NewMemory()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for i := 1; i <= cfg.Workers.WorkerCount; i++ {
		workerID := fmt.Sprintf("worker-%d", i)
		w := worker.New(workerID, q, s)

		go func() {
			if err := w.Run(ctx); err != nil && ctx.Err() == nil {
				log.WithError(err).WithField("worker_id", workerID).Error("Worker stopped")
			}
		}()
	}

	<-ctx.Done()
	log.Info("Worker service shutting down")
}
