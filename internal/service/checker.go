package service

import (
	"context"
	"fmt"
	"time"

	"github.com/codebyanas/go-bloom-filter-lookup-engine/internal/bloom"
	db "github.com/codebyanas/go-bloom-filter-lookup-engine/internal/db"
)

type CheckResult struct {
	Available      bool    `json:"available"`
	Source         string  `json:"source"` // "bloom" or "database"
	RedisLatencyMS float64 `json:"redis_latency_ms"`
}

type CheckerService struct {
	bloom *bloom.Filter
	db    *db.Queries
}

// NewCheckerService initializes the core lookup service dependencies
func NewCheckerService(b *bloom.Filter, queries *db.Queries) *CheckerService {
	return &CheckerService{
		bloom: b,
		db:    queries,
	}
}

// IsUsernameAvailable executes the multi-tier check flow
func (s *CheckerService) IsUsernameAvailable(ctx context.Context, username string) (*CheckResult, error) {
	// 1. Measure Pure Redis Execution Latency
	startRedis := time.Now()
	maybeExists, err := s.bloom.Exists(ctx, username)
	redisDuration := time.Since(startRedis)
	redisLatencyMS := float64(redisDuration.Microseconds()) / 1000.0

	if err != nil {
		return nil, fmt.Errorf("bloom filter lookup error: %w", err)
	}

	// If Bloom Filter says NOT present, it is 100% available
	if !maybeExists {
		return &CheckResult{
			Available:      true,
			Source:         "bloom",
			RedisLatencyMS: redisLatencyMS,
		}, nil
	}

	// Tier 2: DB Fallback (Source of Truth check for false-positive validation)
	dbExists, err := s.db.CheckUsernameExists(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("database lookup error: %w", err)
	}

	return &CheckResult{
		Available:      !dbExists,
		Source:         "database",
		RedisLatencyMS: redisLatencyMS,
	}, nil
}

// RegisterUser creates a new user in PostgreSQL and populates the Bloom Filter
func (s *CheckerService) RegisterUser(ctx context.Context, username, name string) (*db.User, error) {
	user, err := s.db.CreateUser(ctx, db.CreateUserParams{
		Username: username,
		Name:     name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to persist user to database: %w", err)
	}

	// Populate Bloom Filter so future lookups hit Tier 1
	if err := s.bloom.Add(ctx, username); err != nil {
		return nil, fmt.Errorf("user registered but bloom sync failed: %w", err)
	}

	return &user, nil
}