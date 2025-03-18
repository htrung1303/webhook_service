package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// MetricsCollector collects metrics for webhook deliveries
type MetricsCollector struct {
	mu                       sync.RWMutex
	totalRequests            int64
	totalLatency             time.Duration
	minLatency               time.Duration
	maxLatency               time.Duration
	successfulRequests       int64
	failedRequests           int64
	requestTimes             []time.Time
	processingTimes          []time.Duration
	startTime                time.Time
	deliveryTimestamps       map[string]time.Time // Map of event ID to delivery timestamp
	mockFailureRate          float64              // Percentage of requests to artificially fail (0-100)
	mockResponseDelayMin     time.Duration        // Minimum simulated processing delay
	mockResponseDelayMax     time.Duration        // Maximum simulated processing delay
	responsesSizeHistogram   map[int]int          // Maps size buckets to count of responses
	responseCodeCounts       map[int]int          // Maps HTTP status codes to counts
}

func NewMetricsCollector(mockFailureRate float64, mockResponseDelayMin, mockResponseDelayMax time.Duration) *MetricsCollector {
	return &MetricsCollector{
		startTime:             time.Now(),
		minLatency:            time.Hour,
		deliveryTimestamps:    make(map[string]time.Time),
		mockFailureRate:       mockFailureRate,
		mockResponseDelayMin:  mockResponseDelayMin,
		mockResponseDelayMax:  mockResponseDelayMax,
		responsesSizeHistogram: make(map[int]int),
		responseCodeCounts:     make(map[int]int),
	}
}

// RecordWebhookDelivery records metrics for a webhook delivery
func (m *MetricsCollector) RecordWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&m.totalRequests, 1)
	m.mu.Lock()
	m.requestTimes = append(m.requestTimes, time.Now())
	m.mu.Unlock()

	// Read and decode the body
	decoder := json.NewDecoder(r.Body)
	var payload map[string]interface{}
	err := decoder.Decode(&payload)
	if err != nil {
		log.Printf("Error decoding payload: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		atomic.AddInt64(&m.failedRequests, 1)
		m.mu.Lock()
		m.responseCodeCounts[http.StatusBadRequest]++
		m.mu.Unlock()
		return
	}

	payloadBytes, _ := json.Marshal(payload)
	payloadSize := len(payloadBytes)
	m.mu.Lock()
	sizeBucket := (payloadSize / 1024) * 1024 // Round to nearest KB
	m.responsesSizeHistogram[sizeBucket]++
	m.mu.Unlock()

	eventID, ok := payload["id"].(string)
	if !ok {
		log.Printf("Payload missing event ID or not a string: %v", payload)
		eventID = fmt.Sprintf("unknown-%d", time.Now().UnixNano())
	}

	// Simulate processing delay
	var delay time.Duration
	if m.mockResponseDelayMax > m.mockResponseDelayMin {
		delay = m.mockResponseDelayMin + time.Duration(float64(m.mockResponseDelayMax-m.mockResponseDelayMin)*rand.Float64())
	} else {
		delay = m.mockResponseDelayMin
	}
	time.Sleep(delay)

	// Simulate failure based on mockFailureRate
	if rand.Float64()*100 < m.mockFailureRate {
		w.WriteHeader(http.StatusInternalServerError)
		atomic.AddInt64(&m.failedRequests, 1)
		m.mu.Lock()
		m.responseCodeCounts[http.StatusInternalServerError]++
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	m.deliveryTimestamps[eventID] = time.Now()
	m.responseCodeCounts[http.StatusOK]++
	m.mu.Unlock()

	// Calculate processing time (time from event creation to delivery)
	if eventTime, ok := payload["event_time"].(string); ok {
		if parsedTime, err := time.Parse(time.RFC3339, eventTime); err == nil {
			processingTime := time.Since(parsedTime)
			m.mu.Lock()
			m.processingTimes = append(m.processingTimes, processingTime)
			m.totalLatency += processingTime
			if processingTime < m.minLatency {
				m.minLatency = processingTime
			}
			if processingTime > m.maxLatency {
				m.maxLatency = processingTime
			}
			m.mu.Unlock()
		}
	}

	atomic.AddInt64(&m.successfulRequests, 1)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success": true}`))
}

func (m *MetricsCollector) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalRequests := atomic.LoadInt64(&m.totalRequests)
	successfulRequests := atomic.LoadInt64(&m.successfulRequests)
	failedRequests := atomic.LoadInt64(&m.failedRequests)
	
	// Calculate average latency
	var avgLatency time.Duration
	if len(m.processingTimes) > 0 {
		avgLatency = m.totalLatency / time.Duration(len(m.processingTimes))
	}

	// Calculate throughput
	var requestsPerSecond float64
	if elapsed := time.Since(m.startTime).Seconds(); elapsed > 0 {
		requestsPerSecond = float64(totalRequests) / elapsed
	}

	// Calculate percentiles if we have enough data
	var p50, p95, p99 time.Duration
	if len(m.processingTimes) > 0 {
		// Sort processingTimes
		sortedTimes := make([]time.Duration, len(m.processingTimes))
		copy(sortedTimes, m.processingTimes)
		sort.Slice(sortedTimes, func(i, j int) bool {
			return sortedTimes[i] < sortedTimes[j]
		})

		p50 = sortedTimes[len(sortedTimes)*50/100]
		p95 = sortedTimes[len(sortedTimes)*95/100]
		p99 = sortedTimes[len(sortedTimes)*99/100]
	}

	// Prepare size histogram report
	sizeHistogram := make(map[string]int)
	for size, count := range m.responsesSizeHistogram {
		sizeHistogram[fmt.Sprintf("%dKB-%dKB", size/1024, (size/1024)+1)] = count
	}

	return map[string]interface{}{
		"total_requests":       totalRequests,
		"successful_requests":  successfulRequests,
		"failed_requests":      failedRequests,
		"requests_per_second":  requestsPerSecond,
		"min_latency_ms":       float64(m.minLatency.Milliseconds()),
		"max_latency_ms":       float64(m.maxLatency.Milliseconds()),
		"avg_latency_ms":       float64(avgLatency.Milliseconds()),
		"p50_latency_ms":       float64(p50.Milliseconds()),
		"p95_latency_ms":       float64(p95.Milliseconds()),
		"p99_latency_ms":       float64(p99.Milliseconds()),
		"uptime_seconds":       time.Since(m.startTime).Seconds(),
		"response_codes":       m.responseCodeCounts,
		"response_size_histogram": sizeHistogram,
	}
}

func (m *MetricsCollector) ResetMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	atomic.StoreInt64(&m.totalRequests, 0)
	atomic.StoreInt64(&m.successfulRequests, 0)
	atomic.StoreInt64(&m.failedRequests, 0)
	
	m.totalLatency = 0
	m.minLatency = time.Hour
	m.maxLatency = 0
	m.requestTimes = nil
	m.processingTimes = nil
	m.startTime = time.Now()
	m.deliveryTimestamps = make(map[string]time.Time)
	m.responsesSizeHistogram = make(map[int]int)
	m.responseCodeCounts = make(map[int]int)
}

func main() {
	port := flag.Int("port", 9090, "Port to listen on")
	failureRate := flag.Float64("failure-rate", 0, "Percentage of requests to fail (0-100)")
	responseDelayMin := flag.Duration("min-delay", 0, "Minimum simulated processing delay (e.g., 10ms)")
	responseDelayMax := flag.Duration("max-delay", 0, "Maximum simulated processing delay (e.g., 100ms)")
	flag.Parse()

	metricsCollector := NewMetricsCollector(*failureRate, *responseDelayMin, *responseDelayMax)
	mux := http.NewServeMux()

	mux.HandleFunc("/webhook", metricsCollector.RecordWebhookDelivery)

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := metricsCollector.GetMetrics()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metrics)
	})

	mux.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
		metricsCollector.ResetMetrics()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "Metrics reset successfully"}`))
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	go func() {
		log.Printf("Mock Webhook Server starting on port %d", *port)
		log.Printf("Configuration: Failure Rate: %.2f%%, Min Delay: %v, Max Delay: %v", *failureRate, *responseDelayMin, *responseDelayMax)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Server is shutting down...")
	
	log.Println("Final Metrics:")
	metrics := metricsCollector.GetMetrics()
	prettyMetrics, _ := json.MarshalIndent(metrics, "", "  ")
	log.Println(string(prettyMetrics))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
} 
