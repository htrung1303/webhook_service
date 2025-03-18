# Webhook Service

A scalable and reliable webhook delivery service that handles event notifications with fair queueing and retry mechanisms.

## Prerequisites

- Docker
- Docker Compose

That's it! You don't need Go, PostgreSQL, or any other dependencies installed locally.

## Quick Start

### Using the start script (Recommended)

```bash
# Make the script executable (Unix-like systems)
chmod +x start.sh

# Start the service
./start.sh
```

### Using Docker Compose directly

```bash
# Start the services
docker compose up --build

# Or run in detached mode
docker compose up -d --build

# View logs
docker compose logs -f

# Stop services
docker compose down
```

## Configuration

All configuration is done through environment variables. You can override the defaults by creating or modifying the `.env` file in the project root. Here are the key configuration options:

```env
# HTTP server configuration
PORT=8080
APP_ENV=local

# Database configuration
WS_DB_HOST=localhost
WS_DB_PORT=5432
WS_DB_DATABASE=webhook_service
WS_DB_USERNAME=admin
WS_DB_PASSWORD=password1234
WS_DB_SCHEMA=public

# Worker and queue configuration
QUEUE_BUFFER_SIZE=100
MIN_WORKERS=5
MAX_WORKERS=20
MIN_REQUEST_WORKERS=5
MAX_REQUEST_WORKERS=20
REQUEST_BUFFER_SIZE=1000

# Retry configuration
INITIAL_DELAY=1s
MAX_DELAY=10s
MAX_RETRIES=3
SHUTDOWN_TIMEOUT=10s
```

Note: When running with Docker Compose, the database host is automatically set to `psql_ws` (the service name in docker-compose.yml).

## Testing the Service

Once the service is running, you can test it using curl:

```bash
# Health check
curl http://localhost:8080/health

# Send a test webhook event
curl -X POST http://localhost:8080/webhooks/events \
-H "Content-Type: application/json" \
-d '{
  "event_name": "subscriber.created",
  "event_time": "2024-03-19T14:15:22Z",
  "webhook_id": "test-webhook-id",
  "subscriber": {
    "id": "sub123",
    "status": "active",
    "email": "test@example.com",
    "source": "manual",
    "first_name": "John",
    "last_name": "Doe",
    "segments": [],
    "custom_fields": {},
    "optin_ip": "127.0.0.1",
    "optin_timestamp": "2024-03-19T14:15:22Z",
    "created_at": "2024-03-19T14:15:22Z"
  }
}'
```

## Documentation

### Architecture

For detailed information about the system architecture, components, and design decisions, please see the [Architecture Documentation](ARCHITECTURE.md).

Key architectural features:
- **Reliability**: Implements retry mechanism with exponential backoff
- **Scalability**: Uses worker pools and is horizontally scalable
- **Fairness**: Implements fair queueing to prevent large customers from flooding the system

### Benchmarking

For information about benchmarking the webhook service, including performance testing methodologies, tools, and build instructions, please see the [Benchmarking Documentation](BENCHMARKING.md).

## Project Structure

- `cmd/`: Application entry points
  - `app/`: Main webhook service application
  - `benchmark/`: Performance testing tools
  - `mockserver/`: Mock webhook target server
- `internal/`: Internal packages
  - `config/`: Application configuration
  - `database/`: Database connection and migrations
  - `model/`: Data models and validation
  - `repository/`: Data access layer
  - `server/`: HTTP server and request routing
  - `service/`: Business logic and worker pools
- `scripts/`: Utility scripts
- `start.sh`: Convenience script for starting the service
- `docker-compose.yml`: Docker Compose configuration
- `Dockerfile`: Docker build configuration
- `.env`: Environment variable configuration

## License

This project is licensed under the MIT License - see the LICENSE file for details.
