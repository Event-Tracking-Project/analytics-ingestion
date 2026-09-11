/*
internal/queue/redis.go
This file contains function for redis client init
Cretes redis client based on yaml config
Sets group and queue for batch queue
*/
package queue

import (
	"analytics-ingestion/internal/config"
	"analytics-ingestion/internal/event"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Checks if group is busy
func isBusyGroupError(err error) bool {
	return strings.Contains(err.Error(), "BUSYGROUP")
}

// Redis queue struct
type RedisQueue struct {
	client *redis.Client
	stream string
	group  string
}

// Creates new redis client
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

// Publish method to add to redis queue
func (q *RedisQueue) Publish(ctx context.Context, batch event.Batch) error {
	payload, err := json.Marshal(batch)
	if err != nil {
		return err
	}

	_, err = q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: map[string]interface{}{
			"batch_id": batch.BatchID,
			"payload":  string(payload),
		},
	}).Result()

	return err
}

// Consume method that lets worker read from Redis queue
func (q *RedisQueue) Consume(
	ctx context.Context,
	consumer string,
) (Message, error) {
	result, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    q.group,
		Consumer: consumer,
		Streams:  []string{q.stream, ">"},
		Count:    1,
		Block:    5 * time.Second,
	}).Result()

	if err != nil {
		return Message{}, err
	}

	if len(result) == 0 || len(result[0].Messages) == 0 {
		return Message{}, ErrQueueEmpty
	}

	redisMessage := result[0].Messages[0]

	// Check batch id
	rawBatchID, ok := redisMessage.Values["batch_id"].(string)
	if !ok {
		return Message{}, errors.New("Redis message has invalid batch_id")
	}

	// Check payload
	rawPayload, ok := redisMessage.Values["payload"].(string)
	if !ok {
		return Message{}, errors.New("Redis message has invalid payload")
	}

	var batch event.Batch
	if err := json.Unmarshal([]byte(rawPayload), &batch); err != nil {
		return Message{}, err
	}

	return Message{
		ID:      redisMessage.ID,
		BatchID: rawBatchID,
		Batch:   batch,
	}, nil
}

// Redis ack function
func (q *RedisQueue) Ack(ctx context.Context, message Message) error {
	return q.client.XAck(
		ctx,
		q.stream,
		q.group,
		message.ID,
	).Err()
}
