/*
cmd/worker/main.go
Worker seperate process
Currently un used until redis queue implementation
Meant for server fail safe support
*/
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"
	"analytics-ingestion/internal/worker"

	log "github.com/sirupsen/logrus"
)

// Worker manager function
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
	defer q.Close()
	s := storage.NewMemory()

	ctx := context.Background()

	manager := worker.NewManager(cfg.Workers, q, s)
	manager.Start(ctx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	<-stop
	log.Info("Worker service shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("Worker shutdown failed")
	}
}
