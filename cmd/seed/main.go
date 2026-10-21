package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

const TotalRecords = 1000000 // 1 Million Users
const BatchSize = 10000       // Insert in chunks of 10,000

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading system environment variables")
	}

	ctx := context.Background()

	// 1. Initialize PostgreSQL Connection Pool
	dbConnStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
		os.Getenv("DB_SSLMODE"),
	)

	dbPool, err := pgxpool.New(ctx, dbConnStr)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer dbPool.Close()

	// 2. Initialize Redis Client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		Password: os.Getenv("REDIS_PASSWORD"),
	})

	log.Printf("Starting bulk seed of %d records...", TotalRecords)
	startTime := time.Now()

	for i := 0; i < TotalRecords; i += BatchSize {
		batchStart := time.Now()
		rows := make([][]any, 0, BatchSize)

		// Create Redis Pipeline for fast network batching
		pipe := redisClient.Pipeline()

		for j := 0; j < BatchSize; j++ {
			idx := i + j + 1
			username := fmt.Sprintf("user_%d", idx)
			name := fmt.Sprintf("User %d", idx)

			rows = append(rows, []any{username, name})
			pipe.Do(ctx, "BF.ADD", "usernames_bloom", username)
		}

		// High-speed PostgreSQL Binary Copy Protocol
		_, err := dbPool.CopyFrom(
			ctx,
			pgx.Identifier{"users"},
			[]string{"username", "name"},
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			log.Fatalf("PostgreSQL bulk copy failed at batch %d: %v", i, err)
		}

		// Execute Redis Pipeline Batch
		_, err = pipe.Exec(ctx)
		if err != nil {
			log.Fatalf("Redis pipeline execution failed at batch %d: %v", i, err)
		}

		log.Printf("Inserted batch %d to %d (%v)", i+1, i+BatchSize, time.Since(batchStart))
	}

	log.Printf("Successfully seeded %d records in %v!", TotalRecords, time.Since(startTime))
}