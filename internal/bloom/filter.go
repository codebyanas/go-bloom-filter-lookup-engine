package bloom

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Filter struct {
	client *redis.Client
	key    string
}

// NewFilter initializes a new Bloom Filter manager instance
func NewFilter(client *redis.Client, key string) *Filter {
	return &Filter{
		client: client,
		key:    key,
	}
}

// Init creates a new RedisBloom filter with target capacity and error rate
func (f *Filter) Init(ctx context.Context, errorRate float64, capacity int64) error {
	// BF.RESERVE key error_rate capacity
	err := f.client.Do(ctx, "BF.RESERVE", f.key, errorRate, capacity).Err()
	if err != nil && err.Error() != "ERR item exists" {
		return fmt.Errorf("failed to reserve bloom filter: %w", err)
	}
	return nil
}

// Add inserts a new username into the Bloom Filter
func (f *Filter) Add(ctx context.Context, username string) error {
	return f.client.Do(ctx, "BF.ADD", f.key, username).Err()
}

// Exists checks if a username might exist in the Bloom Filter
// Returns false if DEFINITELY NOT present, true if MAYBE present
func (f *Filter) Exists(ctx context.Context, username string) (bool, error) {
	res, err := f.client.Do(ctx, "BF.EXISTS", f.key, username).Bool()
	if err != nil {
		return false, fmt.Errorf("bloom filter check execution failed: %w", err)
	}
	return res, nil
}