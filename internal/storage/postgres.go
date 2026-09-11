/*
internal/storage/postgre.go
Contains postgredb client creation functions
Also closes connections
*/
package storage

import (
	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/event"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Storage pool struct
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// New postgre client function
func NewPostgres(cfg config.DatabaseConfig) (*PostgresStorage, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=%s",
		cfg.Host,
		cfg.Port,
		cfg.Name,
		cfg.User,
		cfg.Password,
		cfg.SslMode,
	)

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}

	poolConfig.MaxConns = int32(cfg.MaxConnections)
	poolConfig.MinConns = int32(cfg.MinConnections)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return &PostgresStorage{pool: pool}, nil
}

// Store/Write event function
func (s *PostgresStorage) StoreEvents(
	ctx context.Context,
	batches []event.Batch,
) error {
	return nil
}

// Close pool connection function
func (s *PostgresStorage) Close() {
	s.pool.Close()
}
