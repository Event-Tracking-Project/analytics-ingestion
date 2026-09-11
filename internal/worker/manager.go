/*
internal/worker/manager.go
Worker process lifecycle and graceful draining.
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
	cancel        context.CancelFunc
	draining      chan struct{}
	drainOnce     sync.Once
	wg            sync.WaitGroup
}

// Create a manager for the standalone worker process.
func NewManager(cfg config.WorkerConfig, q queue.Queue, s storage.Storage) *Manager {
	return &Manager{
		config: cfg,
		queue:  q,
		store:  s,
	}
}

// Start the configured long-running workers.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.draining = make(chan struct{})
	m.mu.Unlock()

	for i := 0; i < m.config.WorkerCount; i++ {
		m.start(false)
	}
}

// StartOne is retained for callers that need one-batch workers.
func (m *Manager) StartOne() {
	m.start(true)
}

// Starts a worker if the configured concurrency limit has not been reached.
func (m *Manager) start(once bool) {
	m.mu.Lock()
	if m.activeWorkers >= m.config.WorkerCount || m.ctx == nil {
		m.mu.Unlock()
		return
	}

	m.activeWorkers++
	workerID := m.activeWorkers
	ctx := m.ctx
	draining := m.draining
	m.wg.Add(1)
	m.mu.Unlock()

	name := fmt.Sprintf("worker-%d", workerID)
	w := New(name, m.queue, m.store)

	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			m.activeWorkers--
			m.mu.Unlock()
		}()

		var err error
		if once {
			err = w.RunOnce(ctx)
		} else {
			err = w.RunWithDrain(ctx, draining)
		}
		if err != nil && ctx.Err() == nil {
			log.WithError(err).WithField("worker_id", name).Error("Worker stopped")
		}
	}()
}

// Shutdown for workers if sigterm
func (m *Manager) Shutdown(ctx context.Context) error {
	m.drainOnce.Do(func() {
		m.mu.Lock()
		draining := m.draining
		m.mu.Unlock()
		if draining != nil {
			close(draining)
		}
	})

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		m.mu.Lock()
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Unlock()
		return ctx.Err()
	}
}
