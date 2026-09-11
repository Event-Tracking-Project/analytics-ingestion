/*
internal/queue/redis.go
This file contains function for redis client init
Cretes redis client based on yaml config
Sets group and queue for batch queue
*/
package queue

import (
	"analytics-ingestion/internal/config"
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func isBusyGroupError(err error) bool {
	return strings.Contains(err.Error(), "BUSYGROUP")
}

type RedisQueue struct {
	client *redis.Client
	stream string
	group  string
}

func NewRedis(cfg config.RedisConfig) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Address,
		Password: cfg.Password,
		DB:       cfg.Database,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	err := client.XGroupCreateMkStream(
		ctx,
		cfg.Stream,
		cfg.ConsumerGroup,
		"$",
	).Err()

	if err != nil && !isBusyGroupError(err) {
		return nil, err
	}

	return &RedisQueue{
		client: client,
		stream: cfg.Stream,
		group:  cfg.ConsumerGroup,
	}, nil
}
