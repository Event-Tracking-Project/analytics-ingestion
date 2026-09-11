/*
internal/storage/postgres.go
Contains PostgreSQL storage client and event persistence functions.
*/
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/event"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Struct for pgx pool, allows multiple connections
type PostgresStorage struct {
	pool *pgxpool.Pool
}

// Create new postgre  client
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

	// Configure pool based on config
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

// Store/Write events function
func (s *PostgresStorage) StoreEvents(
	ctx context.Context,
	batches []event.Batch,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin event storage transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, batch := range batches {
		payload, err := json.Marshal(batch)
		if err != nil {
			return fmt.Errorf("marshal batch %q: %w", batch.BatchID, err)
		}
		payloadHash := fmt.Sprintf("%x", sha256.Sum256(payload))

		if _, err := tx.Exec(ctx, `
			INSERT INTO batches (batch_id, payload_hash)
			VALUES ($1, $2)
			ON CONFLICT (batch_id) DO NOTHING
		`, batch.BatchID, payloadHash); err != nil {
			return fmt.Errorf("insert batch %q: %w", batch.BatchID, err)
		}

		var storedHash string
		if err := tx.QueryRow(ctx, `
			SELECT payload_hash
			FROM batches
			WHERE batch_id = $1
		`, batch.BatchID).Scan(&storedHash); err != nil {
			return fmt.Errorf("check batch %q idempotency: %w", batch.BatchID, err)
		}
		if storedHash != payloadHash {
			return fmt.Errorf("batch %q already exists with different contents", batch.BatchID)
		}

		for eventIndex, e := range batch.EventBatch {
			properties, err := json.Marshal(e.Properties)
			if err != nil {
				return fmt.Errorf(
					"marshal properties for batch %q event %d: %w",
					batch.BatchID,
					eventIndex,
					err,
				)
			}

			contextData, err := json.Marshal(e.Context)
			if err != nil {
				return fmt.Errorf(
					"marshal context for batch %q event %d: %w",
					batch.BatchID,
					eventIndex,
					err,
				)
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO events (
					batch_id,
					event_index,
					event_id,
					name,
					timestamp_ms,
					project_id,
					org_id,
					funnel_id,
					user_id,
					anonymous_id,
					session_id,
					properties,
					context,
					received_at_ms
				)
				VALUES (
					$1, $2, $3, $4, $5, $6, $7,
					$8, $9, $10, $11, $12, $13, $14
				)
				ON CONFLICT (batch_id, event_index) DO NOTHING
			`,
				batch.BatchID,
				eventIndex,
				e.ID,
				e.Name,
				e.Timestamp,
				e.ProjectID,
				e.OrgID,
				e.FunnelID,
				e.UserID,
				e.AnonymousID,
				e.SessionID,
				properties,
				contextData,
				e.ReceivedAt,
			); err != nil {
				return fmt.Errorf(
					"insert event for batch %q event %d: %w",
					batch.BatchID,
					eventIndex,
					err,
				)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit event storage transaction: %w", err)
	}
	return nil
}

// Close connection
func (s *PostgresStorage) Close() {
	s.pool.Close()
}

var _ Storage = (*PostgresStorage)(nil)
