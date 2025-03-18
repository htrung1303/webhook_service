# Simple Makefile for a Go project

# Determine the binary name based on OS
ifeq ($(OS),Windows_NT)
	BINARY=main.exe
else
	BINARY=main
endif

# Include the .env file
ifneq (,$(wildcard ./.env))
	include .env
	export
endif

# Database configuration using .env variables with fallbacks
DB_USER ?= $(WS_DB_USERNAME)
DB_PASSWORD ?= $(WS_DB_PASSWORD)
DB_HOST ?= $(WS_DB_HOST)
DB_PORT ?= $(WS_DB_PORT)
DB_NAME ?= $(WS_DB_DATABASE)
DB_SCHEMA ?= $(WS_DB_SCHEMA)
DB_DSN = postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=disable&search_path=$(DB_SCHEMA)
MIGRATIONS_DIR = internal/database/migrations
SEEDS_DIR = internal/database/seeds

# Build the application
all: build test

build:
	@echo "Building..."
	@go build -o $(BINARY) cmd/api/main.go

# Run the application
run:
	@echo "Running..."
	@go run cmd/api/main.go

# Run with race detection
run-race:
	@echo "Running with race detection..."
	@go run -race cmd/api/main.go

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

# Integration Tests for the application
itest:
	@echo "Running integration tests..."
	@go test ./internal/database -v

# Clean the binary
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY)

# Create and run containers
docker-up:
	@docker compose up --build

# Create and run containers in detached mode
docker-up-d:
	@docker compose up -d --build

# Stop containers
docker-down:
	@docker compose down

# View container logs
docker-logs:
	@docker compose logs -f

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Vet code
vet:
	@echo "Vetting code..."
	@go vet ./...

# --- Database Migration Commands ---

# Create a new migration file
migrate-create:
	@read -p "Enter migration name: " name; \
	goose -dir $(MIGRATIONS_DIR) create $${name} sql

# Apply all available migrations
migrate-up:
	@echo "Applying all migrations..."
	@goose -dir $(MIGRATIONS_DIR) postgres "$(DB_DSN)" up

# Apply only the next migration
migrate-up-one:
	@echo "Applying one migration..."
	@goose -dir $(MIGRATIONS_DIR) postgres "$(DB_DSN)" up-by-one

# Rollback the most recent migration
migrate-down:
	@echo "Rolling back the last migration..."
	@goose -dir $(MIGRATIONS_DIR) postgres "$(DB_DSN)" down

# Rollback all migrations
migrate-reset:
	@echo "Rolling back all migrations..."
	@goose -dir $(MIGRATIONS_DIR) postgres "$(DB_DSN)" reset

# Show the current migration status
migrate-status:
	@echo "Migration status:"
	@goose -dir $(MIGRATIONS_DIR) postgres "$(DB_DSN)" status

# --- Seed Commands ---

# Create a new seed file
seed-create:
	@read -p "Enter seed name: " name; \
	goose -dir $(SEEDS_DIR) create $${name} sql

# Apply all available seeds
seed-up:
	@echo "Applying all seeds..."
	@goose -dir $(SEEDS_DIR) postgres "$(DB_DSN)" up

# Rollback the most recent seed
seed-down:
	@echo "Rolling back the last seed..."
	@goose -dir $(SEEDS_DIR) postgres "$(DB_DSN)" down

.PHONY: all build run test clean docker-up docker-up-d docker-down docker-logs fmt vet \
        migrate-create migrate-up migrate-up-one migrate-down migrate-reset migrate-status \
        seed-create seed-up seed-down
