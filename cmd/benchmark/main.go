package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EventPayload represents a webhook event payload
type EventPayload struct {
	ID         string    `json:"id"`           // Unique ID for tracking
	WebhookID  string    `json:"webhook_id"`
	EventName  string    `json:"event_name"`
	EventTime  time.Time `json:"event_time"`
	Subscriber Subscriber `json:"subscriber"`
}

// Subscriber represents a subscriber in the system
type Subscriber struct {
	ID             string                 `json:"id"`
	Status         string                 `json:"status"`
	Email          string                 `json:"email"`
	Source         string                 `json:"source"`
	FirstName      string                 `json:"first_name"`
	LastName       string                 `json:"last_name"`
	Segments       []string               `json:"segments"`
	CustomFields   map[string]interface{} `json:"custom_fields"`
	OptinIP        string                 `json:"optin_ip"`
	OptinTimestamp time.Time             `json:"optin_timestamp"`
	CreatedAt      time.Time             `json:"created_at"`
}

type BenchmarkMode string

const (
	ModeSend BenchmarkMode = "send"
	ModeEnd2End BenchmarkMode = "end2end"
	ModeStress BenchmarkMode = "stress"
)

type BenchmarkResults struct {
	TotalRequests       int64
	SuccessfulRequests  int64
	FailedRequests      int64
	TotalDuration       time.Duration
	MinLatency          time.Duration
	MaxLatency          time.Duration
	AvgLatency          time.Duration
	P50Latency          time.Duration
	P90Latency          time.Duration
	P95Latency          time.Duration
	P99Latency          time.Duration
	RequestsPerSecond   float64
	SuccessRate         float64
	
	End2EndMinLatency   time.Duration
	End2EndMaxLatency   time.Duration
	End2EndAvgLatency   time.Duration
	End2EndP50Latency   time.Duration
	End2EndP90Latency   time.Duration
	End2EndP95Latency   time.Duration
	End2EndP99Latency   time.Duration
	DeliverySuccessRate float64
}

type StressTestResults struct {
	Concurrency      int
	RequestsPerSecond float64
	SuccessRate      float64
	AvgLatency       time.Duration
	P95Latency       time.Duration
}

func main() {
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	totalRequests := flag.Int("n", 1000, "Total number of requests to send")
	webhookIDs := flag.String("webhooks", "1,2,3,4,5", "Comma-separated list of webhook IDs to use")
	baseURL := flag.String("url", "http://localhost:8080", "Base URL of the webhook service")
	mockServerURL := flag.String("mock", "http://localhost:9090", "URL of the mock server (for end-to-end testing)")
	mode := flag.String("mode", "send", "Benchmark mode: 'send' (default), 'end2end', or 'stress'")
	outputFile := flag.String("output", "", "Optional file to write detailed JSON results")
	webhookTargetURL := flag.String("target", "http://localhost:9090/webhook", "Target URL to configure for webhooks (used in end2end mode)")
	waitForDelivery := flag.Duration("wait", 30*time.Second, "Time to wait for webhook delivery in end2end mode")
	stressStartConcurrency := flag.Int("stress-start", 10, "Starting concurrency for stress test")
	stressMaxConcurrency := flag.Int("stress-max", 100, "Max concurrency for stress test")
	stressStep := flag.Int("stress-step", 10, "Concurrency step for stress test")
	stressStepDuration := flag.Duration("stress-duration", 10*time.Second, "Duration for each step in stress test")
	flag.Parse()
	
	var benchmarkMode BenchmarkMode
	switch strings.ToLower(*mode) {
	case "send":
		benchmarkMode = ModeSend
	case "end2end":
		benchmarkMode = ModeEnd2End
	case "stress":
		benchmarkMode = ModeStress
	default:
		log.Fatalf("Invalid mode: %s. Must be 'send', 'end2end', or 'stress'", *mode)
	}
	
	webhookIDList := parseWebhookIDs(*webhookIDs)
	if len(webhookIDList) == 0 {
		log.Fatal("No webhook IDs provided")
	}
	
	if benchmarkMode == ModeEnd2End {
		log.Println("End-to-end benchmark mode selected")
		if err := checkMockServerHealth(*mockServerURL); err != nil {
			log.Fatalf("Mock server not accessible: %v", err)
		}
		
		if err := resetMockServerMetrics(*mockServerURL); err != nil {
			log.Fatalf("Failed to reset mock server metrics: %v", err)
		}
		
		log.Printf("Mock server ready at %s", *mockServerURL)
		log.Printf("Webhooks will be configured to deliver to: %s", *webhookTargetURL)
		
		for _, id := range webhookIDList {
			log.Printf("Configuring webhook %s to deliver to %s", id, *webhookTargetURL)
		}
	}
	
	var results BenchmarkResults
	var stressResults []StressTestResults
	
	if benchmarkMode == ModeStress {
		log.Println("Running stress test benchmark...")
		stressResults = runStressTest(
			*stressStartConcurrency, 
			*stressMaxConcurrency, 
			*stressStep, 
			*stressStepDuration, 
			webhookIDList, 
			*baseURL,
		)
		printStressResults(stressResults)
	} else {
		log.Printf("Running benchmark with %d concurrent workers, %d total requests...", *concurrency, *totalRequests)
		
		results = runBenchmark(*concurrency, *totalRequests, webhookIDList, *baseURL)
		
		if benchmarkMode == ModeEnd2End {
			log.Printf("Waiting up to %v for webhook deliveries...", *waitForDelivery)
			time.Sleep(*waitForDelivery)
			
			log.Println("Fetching delivery metrics from mock server...")
			deliveryMetrics, err := getMockServerMetrics(*mockServerURL)
			if err != nil {
				log.Printf("Warning: Failed to get mock server metrics: %v", err)
			} else {
				updateResultsWithDeliveryMetrics(&results, deliveryMetrics)
			}
		}
		
		printResults(results)
	}
	
	if *outputFile != "" {
		if benchmarkMode == ModeStress {
			saveStressResultsToFile(stressResults, *outputFile)
		} else {
			saveResultsToFile(results, *outputFile)
		}
	}
}

func runBenchmark(concurrency int, totalRequests int, webhookIDs []string, baseURL string) BenchmarkResults {
	jobs := make(chan int, totalRequests)
	results := make(chan time.Duration, totalRequests)
	latencies := make([]time.Duration, 0, totalRequests)
	
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(i, jobs, results, webhookIDs, baseURL, &wg)
	}
	
	startTime := time.Now()
	
	for i := 0; i < totalRequests; i++ {
		jobs <- i
	}
	close(jobs)
	
	wg.Wait()
	close(results)
	
	for latency := range results {
		if latency >= 0 {
			latencies = append(latencies, latency)
		}
	}
	
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	})
	
	return calculateResults(totalRequests, results, latencies, time.Since(startTime))
}

func worker(id int, jobs <-chan int, results chan<- time.Duration, webhookIDs []string, baseURL string, wg *sync.WaitGroup) {
	defer wg.Done()
	
	for jobID := range jobs {
		event := generateRandomEvent(webhookIDs, fmt.Sprintf("event-%d", jobID))
		
		startTime := time.Now()
		success, err := sendEvent(event, baseURL)
		latency := time.Since(startTime)
		
		if err != nil {
			log.Printf("Worker %d: Error sending event %s: %v", id, event.ID, err)
		}
		
		if success {
			results <- latency
		} else {
			results <- -1
		}
	}
}

func generateRandomEvent(webhookIDs []string, eventID string) EventPayload {
	// Select a random webhook ID
	webhookID := webhookIDs[rand.Intn(len(webhookIDs))]
	
	// Generate a random subscriber ID
	subscriberID := fmt.Sprintf("sub%d", rand.Intn(10000))
	
	// Generate a random email
	email := fmt.Sprintf("user%d@example.com", rand.Intn(1000))
	
	now := time.Now()
	
	return EventPayload{
		ID:        eventID,
		WebhookID: webhookID,
		EventName: "subscriber.created",
		EventTime: now,
		Subscriber: Subscriber{
			ID:             subscriberID,
			Status:         "active",
			Email:          email,
			Source:         "manual",
			FirstName:      "John",
			LastName:       "Doe",
			Segments:       []string{},
			CustomFields:   map[string]interface{}{},
			OptinIP:        "127.0.0.1",
			OptinTimestamp: now,
			CreatedAt:      now,
		},
	}
}

func sendEvent(event EventPayload, baseURL string) (bool, error) {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("error marshaling event: %w", err)
	}
	
	resp, err := http.Post(baseURL+"/webhooks/events", "application/json", bytes.NewBuffer(eventJSON))
	if err != nil {
		return false, fmt.Errorf("error sending event: %w", err)
	}
	defer resp.Body.Close()
	
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

func calculateResults(totalRequests int, resultsChannel <-chan time.Duration, sortedLatencies []time.Duration, totalDuration time.Duration) BenchmarkResults {
	var successfulRequests, failedRequests int64
	var totalLatency, minLatency, maxLatency time.Duration
	
	minLatency = time.Hour // Start with a large value
	
	// Process the results channel (this is already drained from the runBenchmark function)
	for latency := range resultsChannel {
		if latency < 0 {
			failedRequests++
		} else {
			successfulRequests++
			totalLatency += latency
			
			if latency < minLatency {
				minLatency = latency
			}
			
			if latency > maxLatency {
				maxLatency = latency
			}
		}
	}
	
	var avgLatency time.Duration
	if successfulRequests > 0 {
		avgLatency = time.Duration(int64(totalLatency) / successfulRequests)
	}
	
	requestsPerSecond := float64(successfulRequests) / totalDuration.Seconds()
	successRate := float64(successfulRequests) / float64(totalRequests) * 100
	
	// Calculate percentiles
	var p50, p90, p95, p99 time.Duration
	if len(sortedLatencies) > 0 {
		p50 = getPercentile(sortedLatencies, 50)
		p90 = getPercentile(sortedLatencies, 90)
		p95 = getPercentile(sortedLatencies, 95)
		p99 = getPercentile(sortedLatencies, 99)
	}
	
	return BenchmarkResults{
		TotalRequests:      int64(totalRequests),
		SuccessfulRequests: successfulRequests,
		FailedRequests:     failedRequests,
		TotalDuration:      totalDuration,
		MinLatency:         minLatency,
		MaxLatency:         maxLatency,
		AvgLatency:         avgLatency,
		P50Latency:         p50,
		P90Latency:         p90,
		P95Latency:         p95,
		P99Latency:         p99,
		RequestsPerSecond:  requestsPerSecond,
		SuccessRate:        successRate,
	}
}

func getPercentile(sortedLatencies []time.Duration, percentile int) time.Duration {
	if len(sortedLatencies) == 0 {
		return 0
	}
	
	index := len(sortedLatencies) * percentile / 100
	if index >= len(sortedLatencies) {
		index = len(sortedLatencies) - 1
	}
	
	return sortedLatencies[index]
}

func printResults(results BenchmarkResults) {
	fmt.Println("\n=== Benchmark Results ===")
	fmt.Printf("Total Requests:        %d\n", results.TotalRequests)
	fmt.Printf("Successful Requests:   %d (%.2f%%)\n", results.SuccessfulRequests, results.SuccessRate)
	fmt.Printf("Failed Requests:       %d\n", results.FailedRequests)
	fmt.Printf("Total Duration:        %v\n", results.TotalDuration)
	fmt.Printf("Requests Per Second:   %.2f\n", results.RequestsPerSecond)
	fmt.Println("\n=== Webhook Service Acceptance Latency ===")
	fmt.Printf("Min Latency:           %v\n", results.MinLatency)
	fmt.Printf("Avg Latency:           %v\n", results.AvgLatency)
	fmt.Printf("Max Latency:           %v\n", results.MaxLatency)
	fmt.Printf("P50 Latency:           %v\n", results.P50Latency)
	fmt.Printf("P90 Latency:           %v\n", results.P90Latency)
	fmt.Printf("P95 Latency:           %v\n", results.P95Latency)
	fmt.Printf("P99 Latency:           %v\n", results.P99Latency)
	
	// Print end-to-end metrics if available
	if results.End2EndMinLatency > 0 {
		fmt.Println("\n=== End-to-End Delivery Latency ===")
		fmt.Printf("Delivery Success Rate:  %.2f%%\n", results.DeliverySuccessRate)
		fmt.Printf("Min E2E Latency:        %v\n", results.End2EndMinLatency)
		fmt.Printf("Avg E2E Latency:        %v\n", results.End2EndAvgLatency)
		fmt.Printf("Max E2E Latency:        %v\n", results.End2EndMaxLatency)
		fmt.Printf("P50 E2E Latency:        %v\n", results.End2EndP50Latency)
		fmt.Printf("P90 E2E Latency:        %v\n", results.End2EndP90Latency)
		fmt.Printf("P95 E2E Latency:        %v\n", results.End2EndP95Latency)
		fmt.Printf("P99 E2E Latency:        %v\n", results.End2EndP99Latency)
	}
}

func printStressResults(results []StressTestResults) {
	fmt.Println("\n=== Stress Test Results ===")
	fmt.Println("Concurrency\tRPS\t\tSuccess Rate\tAvg Latency\tP95 Latency")
	fmt.Println("-------------------------------------------------------------------")
	
	for _, result := range results {
		fmt.Printf("%d\t\t%.2f\t\t%.2f%%\t\t%v\t\t%v\n",
			result.Concurrency,
			result.RequestsPerSecond,
			result.SuccessRate,
			result.AvgLatency,
			result.P95Latency)
	}
}

func saveResultsToFile(results BenchmarkResults, filename string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Error creating output file: %v", err)
		return
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		log.Printf("Error writing results to file: %v", err)
	} else {
		log.Printf("Results written to %s", filename)
	}
}

func saveStressResultsToFile(results []StressTestResults, filename string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Error creating output file: %v", err)
		return
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		log.Printf("Error writing results to file: %v", err)
	} else {
		log.Printf("Results written to %s", filename)
	}
}

func parseWebhookIDs(webhookIDs string) []string {
	var result []string
	
	// Split the string by comma
	for _, id := range strings.Split(webhookIDs, ",") {
		if len(id) > 0 {
			result = append(result, strings.TrimSpace(id))
		}
	}
	
	return result
}

// Mock server integration functions

func checkMockServerHealth(mockServerURL string) error {
	resp, err := http.Get(mockServerURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mock server returned status %d", resp.StatusCode)
	}
	
	return nil
}

func resetMockServerMetrics(mockServerURL string) error {
	resp, err := http.Post(mockServerURL+"/reset", "application/json", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mock server returned status %d for reset request", resp.StatusCode)
	}
	
	return nil
}

func getMockServerMetrics(mockServerURL string) (map[string]interface{}, error) {
	resp, err := http.Get(mockServerURL + "/metrics")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mock server returned status %d for metrics request", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}
	
	var metrics map[string]interface{}
	if err := json.Unmarshal(body, &metrics); err != nil {
		return nil, fmt.Errorf("error parsing metrics JSON: %w", err)
	}
	
	return metrics, nil
}

func updateResultsWithDeliveryMetrics(results *BenchmarkResults, metrics map[string]interface{}) {
	deliverySucessRate := 0.0
	if totalReq, ok := metrics["total_requests"].(float64); ok && totalReq > 0 {
		if successReq, ok := metrics["successful_requests"].(float64); ok {
			deliverySucessRate = successReq / totalReq * 100
		}
	}
	
	results.DeliverySuccessRate = deliverySucessRate
	
	if minLatency, ok := metrics["min_latency_ms"].(float64); ok {
		results.End2EndMinLatency = time.Duration(minLatency) * time.Millisecond
	}
	
	if maxLatency, ok := metrics["max_latency_ms"].(float64); ok {
		results.End2EndMaxLatency = time.Duration(maxLatency) * time.Millisecond
	}
	
	if avgLatency, ok := metrics["avg_latency_ms"].(float64); ok {
		results.End2EndAvgLatency = time.Duration(avgLatency) * time.Millisecond
	}
	
	if p50Latency, ok := metrics["p50_latency_ms"].(float64); ok {
		results.End2EndP50Latency = time.Duration(p50Latency) * time.Millisecond
	}
	
	if p90Latency, ok := metrics["p90_latency_ms"].(float64); ok {
		results.End2EndP90Latency = time.Duration(p90Latency) * time.Millisecond
	} else if p95Latency, ok := metrics["p95_latency_ms"].(float64); ok {
		// If no p90, use p95 as an approximation
		results.End2EndP90Latency = time.Duration(p95Latency) * time.Millisecond
	}
	
	if p95Latency, ok := metrics["p95_latency_ms"].(float64); ok {
		results.End2EndP95Latency = time.Duration(p95Latency) * time.Millisecond
	}
	
	if p99Latency, ok := metrics["p99_latency_ms"].(float64); ok {
		results.End2EndP99Latency = time.Duration(p99Latency) * time.Millisecond
	}
}

// Stress testing functions

func runStressTest(startConcurrency, maxConcurrency, step int, stepDuration time.Duration, webhookIDs []string, baseURL string) []StressTestResults {
	var stressResults []StressTestResults
	
	log.Printf("Starting stress test: concurrency from %d to %d, step %d, duration per step %v",
		startConcurrency, maxConcurrency, step, stepDuration)
	
	for concurrency := startConcurrency; concurrency <= maxConcurrency; concurrency += step {
		log.Printf("Running step with concurrency %d for %v...", concurrency, stepDuration)
		
		estimatedRPS := float64(concurrency) * 10
		totalRequests := int(estimatedRPS * stepDuration.Seconds() * 2)
		
		jobs := make(chan int, totalRequests)
		resultsChannel := make(chan time.Duration, totalRequests)
		latencies := make([]time.Duration, 0, totalRequests)
		var completedRequests int64
		
		startTime := time.Now()
		endTime := startTime.Add(stepDuration)
		
		var wg sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go stressWorker(i, jobs, resultsChannel, webhookIDs, baseURL, &wg, &completedRequests, endTime)
		}
		
		for i := 0; i < totalRequests; i++ {
			jobs <- i
		}
		
		time.Sleep(stepDuration)
		
		close(jobs)
		wg.Wait()
		close(resultsChannel)
		
		actualDuration := time.Since(startTime)
		
		for latency := range resultsChannel {
			if latency >= 0 {
				latencies = append(latencies, latency)
			}
		}
		
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})
		
		var successfulRequests int
		var totalLatency time.Duration
		
		for _, latency := range latencies {
			successfulRequests++
			totalLatency += latency
		}
		
		failedRequests := int(completedRequests) - successfulRequests
		log.Printf("Successful: %d, Failed: %d, Total: %d", successfulRequests, failedRequests, int(completedRequests))
		
		var avgLatency time.Duration
		if successfulRequests > 0 {
			avgLatency = totalLatency / time.Duration(successfulRequests)
		}
		
		rps := float64(successfulRequests) / actualDuration.Seconds()
		successRate := 0.0
		if int(completedRequests) > 0 {
			successRate = float64(successfulRequests) / float64(completedRequests) * 100
		}
		
		var p95 time.Duration
		if len(latencies) > 0 {
			p95 = getPercentile(latencies, 95)
		}
		
		stepResult := StressTestResults{
			Concurrency:      concurrency,
			RequestsPerSecond: rps,
			SuccessRate:      successRate,
			AvgLatency:       avgLatency,
			P95Latency:       p95,
		}
		
		stressResults = append(stressResults, stepResult)
		
		log.Printf("Completed step: concurrency=%d, RPS=%.2f, success=%.2f%%, avg=%v, p95=%v",
			concurrency, rps, successRate, avgLatency, p95)
	}
	
	return stressResults
}

func stressWorker(id int, jobs <-chan int, results chan<- time.Duration, webhookIDs []string, baseURL string, wg *sync.WaitGroup, completedRequests *int64, endTime time.Time) {
	defer wg.Done()
	
	for jobID := range jobs {
		if time.Now().After(endTime) {
			return
		}
		
		event := generateRandomEvent(webhookIDs, fmt.Sprintf("stress-%d-%d", id, jobID))
		
		startTime := time.Now()
		success, err := sendEvent(event, baseURL)
		latency := time.Since(startTime)
		
		atomic.AddInt64(completedRequests, 1)
		
		if err != nil && id%10 == 0 {
			log.Printf("Worker %d: Error sending event: %v", id, err)
		}
		
		if success {
			results <- latency
		} else {
			results <- -1
		}
	}
} 
