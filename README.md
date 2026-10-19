# go-bloom-filter-lookup-engine
A sub-millisecond, high-concurrency username lookup engine in Go using a 3-tier architecture (RedisBloom + PostgreSQL via database/sql, pgx, and sqlc). Optimized for O(1) time complexity at 10M+ scale.
