/*
internal/worker/manager.go
Worker management functions
Manages and logs workers
Works with worker.go to manage them based on config

Will change once redis queue is implemented
*/
package worker

import (
	"context"
	"fmt"
	"sync"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/queue"
	"analytics-ingestion/internal/storage"

	log "github.com/sirupsen/logrus"
)

// Struct for worker manager
type Manager struct {
	config config.WorkerConfig
	queue  queue.Queue
	store  storage.Storage

	mu            sync.Mutex
	activeWorkers int
	ctx           context.Context
}

// Create a new manager at api start
func NewManager(cfg config.WorkerConfig, q queue.Queue, s storage.Storage) *Manager {
	return &Manager{
		config: cfg,
		queue:  q,
		store:  s,
	}
}

// Start workers
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()

	if m.config.StartOnDemand {
		return
	}

	for i := 0; i < m.config.WorkerCount; i++ {
		m.start(false)
	}
}

// Call the start function
func (m *Manager) StartOne() {
	m.start(true)
}

// Starts workers based on active workers. Stops once limit is hit
func (m *Manager) start(once bool) {
	m.mu.Lock()
	if m.activeWorkers >= m.config.WorkerCount || m.ctx == nil {
		m.mu.Unlock()
		return
	}

	m.activeWorkers++
	workerID := m.activeWorkers
	ctx := m.ctx
	m.mu.Unlock()

	name := fmt.Sprintf("worker-%d", workerID)
	w := New(name, m.queue, m.store)

	go func() {
		defer func() {
			m.mu.Lock()
			m.activeWorkers--
			m.mu.Unlock()
		}()

		var err error
		if once {
			err = w.RunOnce(ctx)
		} else {
			err = w.Run(ctx)
		}
		if err != nil && ctx.Err() == nil {
			log.WithError(err).WithField("worker_id", name).Error("Worker stopped")
		}
	}()
}
