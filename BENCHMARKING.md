# Webhook Service - Benchmarking Documentation

## Benchmarking Overview

This document describes the benchmarking tools and methodologies for evaluating the performance of the webhook service under various conditions. The benchmarking suite includes multiple components that work together to provide comprehensive performance insights.

## Components

### Benchmark Client (`cmd/benchmark`)

The benchmark client is a tool that sends webhook events to the webhook service with configurable concurrency levels and measures various performance metrics. It's designed to:

- Generate synthetic webhook events at a specified rate
- Support various testing modes for different evaluation scenarios
- Collect detailed metrics on latency, throughput, and success rates
- Generate reports that can be used for performance analysis

### Mock Server (`cmd/mockserver`)

The mock server acts as a target for webhook deliveries, replacing the real endpoints that would normally receive these webhooks. This allows for:

- Recording accurate delivery metrics
- Simulating various real-world conditions (delays, failures)
- Measuring end-to-end performance from submission to delivery
- Collecting statistics on webhook payloads and processing times

## Benchmark Modes

The benchmark tool supports three primary testing modes:

### 1. Send Mode

This mode focuses on measuring the webhook service's event acceptance performance:

- Sends webhook events to the service's API endpoint
- Measures how quickly the service acknowledges these events
- Evaluates the service's ability to handle incoming traffic
- Provides metrics on acceptance latency and throughput

### 2. End-to-End Mode

This mode measures the complete webhook delivery cycle:

- Sends events to the webhook service
- Configures webhooks to deliver to the mock server
- Waits for all deliveries to complete
- Collects metrics from both the client and mock server
- Provides comprehensive statistics on the entire process

### 3. Stress Test Mode

This mode is designed to find the performance limits of the system:

- Gradually increases load in steps
- Starts with low concurrency and increases to a specified maximum
- Runs each concurrency level for a fixed duration
- Measures how performance changes with increased load
- Helps identify bottlenecks and optimal operating parameters

## Usage Guide

### Building the Benchmark Tools

```bash
# Build the benchmark client
go build -o benchmark cmd/benchmark/main.go

# Build the mock server
go build -o mockserver cmd/mockserver/main.go
```

### Windows (Command Prompt)

```bat
# Build the benchmark client
go build -o benchmark.exe cmd/benchmark/main.go

# Build the mock server
go build -o mockserver.exe cmd/mockserver/main.go

# Run the binaries
benchmark.exe --c 10 --n 1000 [OPTIONS]
mockserver.exe --port 9090 [OPTIONS]
```

### Windows (PowerShell)

```powershell
# Build the benchmark client
go build -o benchmark.exe cmd/benchmark/main.go

# Build the mock server
go build -o mockserver.exe cmd/mockserver/main.go

# Run the binaries
.\benchmark.exe --c 10 --n 1000 [OPTIONS]
.\mockserver.exe --port 9090 [OPTIONS]
```

### Running the Mock Server

Start the mock server before running end-to-end benchmarks:

```bash
./mockserver --port 9090 [OPTIONS]
```

Options:
- `--port`: Port to listen on (default: 9090)
- `--failure-rate`: Percentage of requests to artificially fail (0-100, default: 0)
- `--min-delay`: Minimum simulated processing delay (e.g., 10ms, default: 0)
- `--max-delay`: Maximum simulated processing delay (e.g., 100ms, default: 0)

The mock server exposes several endpoints:
- `/webhook`: The endpoint where webhooks will be delivered
- `/metrics`: Returns current metrics in JSON format
- `/reset`: Resets all metrics
- `/health`: Health check endpoint

### Running Benchmarks

#### Send Mode (Default)

Measures webhook event acceptance performance only:

```bash
./benchmark --c 10 --n 1000 --url http://localhost:8080 --webhooks "1,2,3,4,5"
```

#### End-to-End Mode

Measures full webhook delivery cycle including delivery to the mock server:

```bash
./benchmark --mode end2end --c 10 --n 1000 --url http://localhost:8080 --mock http://localhost:9090 --target http://localhost:9090/webhook --wait 30s
```

#### Stress Test Mode

Gradually increases load to find performance limits:

```bash
./benchmark --mode stress --url http://localhost:8080 --stress-start 10 --stress-max 100 --stress-step 10 --stress-duration 30s
```

### Command-Line Options

#### Common Options

- `--c`: Number of concurrent workers (default: 10)
- `--n`: Total number of requests to send (default: 1000)
- `--webhooks`: Comma-separated list of webhook IDs to use (default: "1,2,3,4,5")
- `--url`: Base URL of the webhook service (default: "http://localhost:8080")
- `--output`: Optional file to write detailed JSON results
- `--mode`: Benchmark mode: 'send' (default), 'end2end', or 'stress'

#### End-to-End Mode Specific Options

- `--mock`: URL of the mock server (default: "http://localhost:9090")
- `--target`: Target URL to configure for webhooks (default: "http://localhost:9090/webhook")
- `--wait`: Time to wait for webhook delivery (default: 30s)

#### Stress Test Mode Specific Options

- `--stress-start`: Starting concurrency (default: 10)
- `--stress-max`: Maximum concurrency (default: 100)
- `--stress-step`: Concurrency step (default: 10)
- `--stress-duration`: Duration for each step (default: 10s)

## Benchmark Results and Metrics

The benchmark tool provides detailed metrics tailored to each mode:

### Send Mode Results

- **Total requests**: The number of webhook events sent
- **Successful/failed requests**: Count and percentage of success/failure
- **Throughput**: Requests per second the service can handle
- **Latency metrics**: 
  - Min/avg/max latency
  - Percentile latencies (p50, p90, p95, p99)

### End-to-End Mode Results

All of the send mode metrics, plus:
- **End-to-end delivery latency**: Time from sending to delivery
- **Delivery success rate**: Percentage of webhooks successfully delivered
- **Request size histograms**: Distribution of webhook payload sizes
- **Response status code distribution**: Count of different HTTP responses

### Stress Test Results

- **Performance by concurrency level**: Metrics at each step
- **Throughput curve**: How RPS changes with increased concurrency
- **Success rate curve**: How reliability changes under load
- **Latency curve**: How response time is affected by load

## Example Benchmarking Scenarios

### Basic Performance Test

```bash
# Start the webhook service
# Start the mock server
./mockserver --port 9090

# Run benchmark in send mode
./benchmark --c 20 --n 5000
```

### End-to-End Performance Test

```bash
# Start the webhook service (configured to deliver to the mock server)
# Start the mock server
./mockserver --port 9090

# Run benchmark in end2end mode
./benchmark --mode end2end --c 20 --n 5000 --wait 60s
```

### Finding Maximum Throughput

```bash
# Start the webhook service
# Start the mock server
./mockserver --port 9090

# Run stress test
./benchmark --mode stress --stress-start 10 --stress-max 200 --stress-step 10 --stress-duration 30s --output results.json
```

### Testing Resilience with Slow Endpoints

```bash
# Start the webhook service
# Start the mock server with delays
./mockserver --port 9090 --min-delay 100ms --max-delay 500ms

# Run end2end benchmark
./benchmark --mode end2end --c 20 --n 1000
```

### Testing Error Handling with Failing Endpoints

```bash
# Start the webhook service
# Start the mock server with failure rate
./mockserver --port 9090 --failure-rate 20

# Run end2end benchmark
./benchmark --mode end2end --c 20 --n 1000
```

## Performance Analysis and Visualization

The benchmark results can be saved to a JSON file using the `--output` option. 
