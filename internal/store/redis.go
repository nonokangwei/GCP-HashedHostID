package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	redisKeyPrefix = "hosthashedid:instance:"
	redisSetKey    = "hosthashedid:all_instances"
)

// RedisStore is a Redis-backed implementation of Store.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a new RedisStore with the provided connection options.
func NewRedisStore(ctx context.Context, host string, port int, password string, db int) (*RedisStore, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis at %s: %w", addr, err)
	}

	return &RedisStore{
		client: rdb,
	}, nil
}

// NewRedisStoreWithClient creates a RedisStore with a pre-configured redis.Client (useful for unit tests).
func NewRedisStoreWithClient(client *redis.Client) *RedisStore {
	return &RedisStore{
		client: client,
	}
}

// Save stores the record as a JSON string and adds instanceID to the tracking set.
func (r *RedisStore) Save(ctx context.Context, record InstanceRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal instance record: %w", err)
	}

	key := redisKeyPrefix + record.InstanceID
	pipe := r.client.Pipeline()
	pipe.Set(ctx, key, data, 0)
	pipe.SAdd(ctx, redisSetKey, record.InstanceID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to save instance to Redis: %w", err)
	}
	return nil
}

// Get retrieves an instance record by ID.
func (r *RedisStore) Get(ctx context.Context, instanceID string) (*InstanceRecord, error) {
	key := redisKeyPrefix + instanceID
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get instance from Redis: %w", err)
	}

	var rec InstanceRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instance record: %w", err)
	}
	return &rec, nil
}

// List returns all instance records.
func (r *RedisStore) List(ctx context.Context) ([]InstanceRecord, error) {
	ids, err := r.client.SMembers(ctx, redisSetKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve instance IDs from Redis set: %w", err)
	}

	if len(ids) == 0 {
		return []InstanceRecord{}, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisKeyPrefix + id
	}

	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to MGet instances from Redis: %w", err)
	}

	records := make([]InstanceRecord, 0, len(values))
	for _, v := range values {
		if v == nil {
			continue
		}
		strVal, ok := v.(string)
		if !ok {
			continue
		}
		var rec InstanceRecord
		if err := json.Unmarshal([]byte(strVal), &rec); err == nil {
			records = append(records, rec)
		}
	}

	return records, nil
}

// Delete removes an instance record from Redis and removes it from the tracking set.
func (r *RedisStore) Delete(ctx context.Context, instanceID string) error {
	key := redisKeyPrefix + instanceID
	pipe := r.client.Pipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, redisSetKey, instanceID)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete instance from Redis: %w", err)
	}
	return nil
}

// Close closes the Redis client connection.
func (r *RedisStore) Close() error {
	return r.client.Close()
}
