# Distributed Sub-Millisecond Identity Lookup Engine in Go

[![Go](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![RedisBloom](https://img.shields.io/badge/RedisBloom-Redis%20Stack-DC382D?logo=redis&logoColor=white)](https://redis.io/docs/latest/develop/data-types/probabilistic/bloom-filter/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A production-oriented Go service for checking username availability at scale. It uses a RedisBloom probabilistic filter as a fast rejection tier and PostgreSQL as the authoritative source of truth. The design removes database I/O entirely for usernames that are definitely not present, while preserving correctness by validating Bloom-filter positives against PostgreSQL.

## Problem and solution

Username availability checks are often implemented as a database query for every request. At millions of rows and high concurrency, that pattern makes database network, connection-pool, index, and disk activity part of the latency of every lookup—even when the requested username does not exist.

This service changes the read path:

1. Ask RedisBloom whether the username may exist.
2. Return **available** immediately when RedisBloom says it is definitely absent.
3. Query PostgreSQL only when RedisBloom says **maybe present**.

Because Bloom filters have no false negatives, the first result is safe: a negative Bloom result proves that the username is not in the indexed set. Bloom positives can be false positives, so they are never treated as proof that a username is taken; PostgreSQL performs the authoritative validation.

## Architecture and two-tier lookup

```text
                         GET /check?username=...
                                     |
                                     v
                         Go HTTP API / CheckerService
                                     |
                    +----------------+----------------+
                    |                                 |
                    v                                 v
        Tier 1: RedisBloom                    Tier 2: PostgreSQL
        BF.EXISTS usernames_bloom              SELECT EXISTS(...)
                    |                                 |
       definitely absent | maybe present                |
                    |                                 |
                    v                                 v
        available, source=bloom              taken or available,
        no database request                 source=database
```

### Tier 1: RedisBloom

- The API initializes a RedisBloom filter named `usernames_bloom`.
- The current API reserves capacity for 10,000,000 users with a 1% target error rate.
- `BF.EXISTS` returns:
  - `false`: the username is definitely absent and therefore available.
  - `true`: the username may exist and requires Tier 2 validation.
- RedisBloom is probabilistic and may produce false positives, but it does not produce false negatives when populated correctly.

### Tier 2: PostgreSQL

- PostgreSQL is the source of truth.
- The `users.username` column is `UNIQUE` and has an explicit B-tree index (`idx_users_username`).
- The generated `sqlc` query uses `SELECT EXISTS(...)` for a minimal indexed existence check.
- A Bloom-positive lookup is taken if the row exists and available if it does not.

### Database I/O bypass

For every Bloom-negative request, the service returns from the Redis result without calling PostgreSQL. This is a **100% database disk-I/O bypass for non-existent users** on the lookup path: PostgreSQL is not contacted for those requests, so there is no database query, index traversal, buffer lookup, or possible disk read. Only Bloom-positive requests reach the fallback query.

## Features and performance

| Capability | Implementation |
| --- | --- |
| Seeded dataset | 1,000,000 deterministic users (`user_1` through `user_1000000`) |
| Bloom capacity | 10,000,000 users, 1% target error rate |
| Tier 1 latency | Approximately **0.64 ms** for Bloom rejection |
| Tier 2 latency | Approximately **2.16 ms** for PostgreSQL fallback |
| PostgreSQL bulk load | `pgx.CopyFrom` using PostgreSQL's binary `COPY` protocol |
| Redis bulk load | Redis pipeline with batched `BF.ADD` commands |
| Latency observability | Separate `redis_latency_ms` and `total_latency_ms` response fields |
| Persistence | Docker-managed PostgreSQL and Redis volumes |

The latency figures are representative benchmark targets for this design and environment, not a guarantee for every machine or deployment. Network distance, container resources, dataset state, concurrency, and storage all affect measured results.

### Seed path

`cmd/seed/main.go` loads 10,000 records per batch. Each batch is written to PostgreSQL with `pgx.CopyFrom` and to RedisBloom through one Redis pipeline, reducing round trips compared with one insert and one Redis command per user.

### Latency fields

- `redis_latency_ms` measures the RedisBloom lookup itself.
- `total_latency_ms` measures the complete HTTP handler path, including Redis and, when required, PostgreSQL.
- `source` identifies the path used: `bloom` for a Bloom-negative response and `database` for a Bloom-positive response validated by PostgreSQL.

## Tech stack

- **Go 1.27.1+**
- **Redis Stack / RedisBloom**
- **PostgreSQL 16**
- **pgx/v5** for PostgreSQL connectivity and bulk copy
- **sqlc** for type-safe generated PostgreSQL queries
- **go-redis/v9** for Redis and RedisBloom commands
- **Docker and Docker Compose**
- **WSL2/Linux** recommended for a Linux-compatible local development workflow

## Project structure

```text
go-bloom-filter-lookup-engine/
├── cmd/
│   ├── api/
│   │   └── main.go                  # HTTP API, dependency wiring, /check and /register
│   └── seed/
│       └── main.go                  # One-million-record PostgreSQL + RedisBloom seeder
├── db/
│   ├── migrations/
│   │   └── 000001_create_users_table.up.sql
│   └── queries/
│       └── users.sql                # sqlc query definitions
├── internal/
│   ├── bloom/
│   │   └── filter.go                 # RedisBloom reserve, add, and exists operations
│   ├── db/
│   │   ├── db.go                     # sqlc DBTX and query wrapper
│   │   ├── models.go                 # Generated User model
│   │   └── users.sql.go              # Generated typed queries
│   └── service/
│       └── checker.go                # Two-tier lookup and registration orchestration
├── .env.example                      # Local configuration template
├── docker-compose.yml                # PostgreSQL and Redis Stack services
├── go.mod
├── go.sum
├── sqlc.yaml                         # sqlc generation configuration
├── LICENSE
└── README.md
```

Files under `internal/db/` are generated by sqlc. Edit `db/queries/users.sql` or the schema, then regenerate the package rather than editing generated files directly.

## Getting started

### Prerequisites

- Go **1.27.1 or later**
- Docker Engine or Docker Desktop
- Docker Compose v2 (`docker compose`)
- `curl` for the examples below
- On Windows, WSL2/Linux is recommended for running the Go commands and Docker workflow

### 1. Clone the repository

```bash
git clone https://github.com/codebyanas/go-bloom-filter-lookup-engine.git
cd go-bloom-filter-lookup-engine
```

### 2. Configure the environment

Copy the template and adjust values as needed:

```bash
cp .env.example .env
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | API listen port |
| `DB_HOST` | `localhost` | PostgreSQL host from the Go process |
| `DB_PORT` | `5432` | PostgreSQL host port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres_password` | PostgreSQL password |
| `DB_NAME` | `identity_db` | PostgreSQL database |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode for local development |
| `REDIS_HOST` | `localhost` | Redis host from the Go process |
| `REDIS_PORT` | `6379` | Redis host port |
| `REDIS_PASSWORD` | empty | Redis password, if configured |

Do not commit `.env`; it is ignored by Git. Use stronger credentials and TLS settings outside local development.

### 3. Start PostgreSQL and Redis Stack

```bash
docker compose up -d
docker compose ps
```

The Compose file exposes PostgreSQL on `DB_PORT`, Redis on `REDIS_PORT`, and RedisInsight on port `8001`.

### 4. Apply the database schema

This repository contains the up migration but does not include a migration executable. Apply it directly after PostgreSQL reports healthy:

```bash
docker exec -i identity_postgres \
  psql -U "$DB_USER" -d "$DB_NAME" < db/migrations/000001_create_users_table.up.sql
```

If your shell does not export `.env` variables, use the configured values explicitly:

```bash
docker exec -i identity_postgres \
  psql -U postgres -d identity_db < db/migrations/000001_create_users_table.up.sql
```

The migration creates the `users` table, the unique constraint on `username`, and the explicit `idx_users_username` B-tree index.

### 5. Seed the dataset

```bash
go run cmd/seed/main.go
```

The seeder creates 1,000,000 users in 10,000-row batches and populates the `usernames_bloom` filter with matching `BF.ADD` operations. It is intended for a clean local dataset; rerunning it against the same database will encounter the `users.username` uniqueness constraint.

### 6. Start the API

```bash
go run cmd/api/main.go
```

The API loads `.env`, verifies PostgreSQL and Redis connectivity, reserves or reuses the Bloom filter, and listens on `PORT` (default `8080`).

## API reference

### `GET /check?username=<username>`

Checks whether a username is available.

| Item | Behavior |
| --- | --- |
| Method | `GET` |
| Query parameter | `username` (required, non-empty) |
| Success status | `200 OK` |
| Missing username | `400 Bad Request` |
| Other methods | `405 Method Not Allowed` |
| Response type | `application/json` |
| Timing header | `X-Response-Time` |

#### Available username: Tier 1 Bloom rejection

```bash
curl "http://localhost:8080/check?username=brand_new_user"
```

```json
{
  "username": "brand_new_user",
  "available": true,
  "message": "Username is available",
  "source": "bloom",
  "redis_latency_ms": 0.64,
  "total_latency_ms": 0.71
}
```

`source: "bloom"` means RedisBloom returned a definite negative. PostgreSQL was not queried.

#### Taken username: Tier 2 PostgreSQL validation

```bash
curl "http://localhost:8080/check?username=user_42"
```

```json
{
  "username": "user_42",
  "available": false,
  "message": "Username is already taken",
  "source": "database",
  "redis_latency_ms": 0.64,
  "total_latency_ms": 2.16
}
```

`source: "database"` means RedisBloom returned a possible match and PostgreSQL confirmed that the username exists. A Bloom false positive can produce the same source with `"available": true` when PostgreSQL finds no row.

### `POST /register`

The implementation also exposes a registration endpoint that inserts a user into PostgreSQL and then adds the username to RedisBloom:

```bash
curl -X POST "http://localhost:8080/register" \
  -H "Content-Type: application/json" \
  -d '{"username":"new_user","name":"New User"}'
```

The endpoint returns `201 Created` with the created user record. A database write can succeed even if the subsequent Bloom synchronization fails; the service reports that error explicitly so it can be repaired operationally.

## Regenerating sqlc code

Install sqlc separately if it is not already available, then run:

```bash
sqlc generate
```

The configuration in `sqlc.yaml` reads the PostgreSQL schema and queries from `db/` and writes generated Go code to `internal/db/`.

## Operations

```bash
# View service status and logs
docker compose ps
docker compose logs -f postgres
docker compose logs -f redis

# Stop containers while retaining named volumes
docker compose down

# Stop containers and remove local database/filter data
docker compose down -v
```

Removing volumes deletes the seeded PostgreSQL records and RedisBloom state. Reapply the schema and rerun the seeder after doing so.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
