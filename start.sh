#!/bin/sh

# Check if Docker is installed
if ! command -v docker >/dev/null 2>&1; then
    echo "Error: Docker is not installed. Please install Docker first."
    exit 1
fi

# Check if Docker Compose is installed
if ! command -v docker compose >/dev/null 2>&1; then
    echo "Error: Docker Compose is not installed. Please install Docker Compose first."
    exit 1
fi

# Start the application
echo "Starting webhook service..."
docker compose up --build

# Handle cleanup on script termination
cleanup() {
    echo "Stopping services..."
    docker compose down
}

trap cleanup INT TERM

# Wait for user input
read -p "Press [Enter] to stop the services..."
cleanup 
