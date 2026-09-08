#!/bin/sh
set -eu

platform_db=avito_kitchen_migration_test
venue_db=venue_migration_test

cleanup() {
    docker compose exec -T postgres dropdb -U avito --if-exists --force "$platform_db"
    docker compose exec -T postgres dropdb -U avito --if-exists --force "$venue_db"
}

trap cleanup EXIT INT TERM

docker compose up -d postgres
cleanup
docker compose exec -T postgres createdb -U avito "$platform_db"
docker compose exec -T postgres createdb -U avito "$venue_db"

platform_url="postgres://avito:avito@postgres:5432/${platform_db}?sslmode=disable"
venue_url="postgres://avito:avito@postgres:5432/${venue_db}?sslmode=disable"

docker compose run --rm --no-deps migrate-platform -path=/migrations -database="$platform_url" up
docker compose run --rm --no-deps migrate-platform -path=/migrations -database="$platform_url" up
docker compose run --rm --no-deps migrate-platform -path=/migrations -database="$platform_url" down 1
docker compose run --rm --no-deps migrate-platform -path=/migrations -database="$platform_url" up 1

docker compose run --rm --no-deps migrate-venue -path=/migrations -database="$venue_url" up
docker compose run --rm --no-deps migrate-venue -path=/migrations -database="$venue_url" up
docker compose run --rm --no-deps migrate-venue -path=/migrations -database="$venue_url" down 1
docker compose run --rm --no-deps migrate-venue -path=/migrations -database="$venue_url" up 1
