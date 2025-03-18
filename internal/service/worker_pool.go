package service

import (
	"log"
	"math"
	"sync"
	"time"
	"webhook-service/internal/model"
)

type WorkerPool struct {
	name          string         // Added pool name for identification in logs
	minWorkers    int
	maxWorkers    int
	activeWorkers int
	workers       map[int]*Worker
	nextWorkerID  int
	mu            sync.Mutex
	
	// For scaling decisions
	lastCheckTime   time.Time
	lastProcessed   int64         // Track last check's processed count
	
	// Function that processes an event (will be provided by WebhookService)
	processEventFn func(*model.WebhookEvent)
	
	// Reference to the event queue we're consuming from
	eventQueue     chan *model.WebhookEvent

	// Metrics
	totalProcessed       int64
	successfulDeliveries int64
	failedDeliveries     int64
	processingTimeNs     int64
	metricsMu            sync.Mutex
}

type Worker struct {
	id        int
	createdAt time.Time
	stopChan  chan struct{}
}

func NewWorkerPool(minWorkers, maxWorkers int, eventQueue chan *model.WebhookEvent, processEventFn func(*model.WebhookEvent)) *WorkerPool {
	return NewNamedWorkerPool("WorkerPool", minWorkers, maxWorkers, eventQueue, processEventFn)
}

// Added new constructor that takes a name parameter
func NewNamedWorkerPool(name string, minWorkers, maxWorkers int, eventQueue chan *model.WebhookEvent, processEventFn func(*model.WebhookEvent)) *WorkerPool {
	if minWorkers < 1 {
		minWorkers = 1
	}
	if maxWorkers < minWorkers {
		maxWorkers = minWorkers
	}
	
	wp := &WorkerPool{
		name:          name,
		minWorkers:    minWorkers,
		maxWorkers:    maxWorkers,
		activeWorkers: 0,
		workers:       make(map[int]*Worker),
		nextWorkerID:  0,
		lastCheckTime: time.Now(),
		processEventFn: processEventFn,
		eventQueue:    eventQueue,
	}
	
	go wp.manageWorkerPool()
	
	log.Printf("[%s] Starting worker pool with min=%d, max=%d workers", wp.name, minWorkers, maxWorkers)
	
	for i := 0; i < minWorkers; i++ {
		wp.addWorker()
	}
	
	return wp
}

func (wp *WorkerPool) addWorker() {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	
	if wp.activeWorkers >= wp.maxWorkers {
		log.Printf("[%s] Cannot add worker: already at maximum capacity (%d workers)", wp.name, wp.maxWorkers)
		return
	}
	
	workerID := wp.nextWorkerID
	wp.nextWorkerID++
	
	worker := &Worker{
		id:        workerID,
		createdAt: time.Now(),
		stopChan:  make(chan struct{}),
	}
	
	wp.workers[workerID] = worker
	wp.activeWorkers++
	
	log.Printf("[%s] Starting worker %d (active workers: %d)", wp.name, workerID, wp.activeWorkers)
	
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[%s] Worker %d panicked: %v", wp.name, workerID, r)
				
				// Automatically replace the crashed worker
				wp.mu.Lock()
				delete(wp.workers, workerID)
				wp.activeWorkers--
				wp.mu.Unlock()
				
				// Add a replacement worker
				go wp.addWorker()
			}
		}()
		
		for {
			select {
			case <-worker.stopChan:
				log.Printf("[%s] Worker %d stopping gracefully", wp.name, workerID)
				return
			case event := <-wp.eventQueue:
				if event != nil {
					wp.processEventFn(event)
				}
			}
		}
	}()
}

func (wp *WorkerPool) removeWorker() {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	
	if wp.activeWorkers <= wp.minWorkers {
		log.Printf("[%s] Cannot remove worker: already at minimum capacity (%d workers)", wp.name, wp.minWorkers)
		return
	}
	
	var oldestWorkerID int
	var oldestTime time.Time
	
	for id, worker := range wp.workers {
		if oldestTime.IsZero() || worker.createdAt.Before(oldestTime) {
			oldestWorkerID = id
			oldestTime = worker.createdAt
		}
	}
	
	worker := wp.workers[oldestWorkerID]
	close(worker.stopChan)
	
	delete(wp.workers, oldestWorkerID)
	wp.activeWorkers--
	
	log.Printf("[%s] Removed worker %d (active workers: %d)", wp.name, oldestWorkerID, wp.activeWorkers)
}

func (wp *WorkerPool) manageWorkerPool() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			wp.adjustWorkerCount()
		}
	}
}

func (wp *WorkerPool) adjustWorkerCount() {
	queueLength := len(wp.eventQueue)
	queueCapacity := cap(wp.eventQueue)
	
	// Calculate utilization percentage
	utilizationPct := float64(queueLength) / float64(queueCapacity) * 100

	// Get processing metrics
	wp.metricsMu.Lock()
	currentProcessed := wp.totalProcessed
	successfulDeliveries := wp.successfulDeliveries
	failedDeliveries := wp.failedDeliveries
	processingTimeNs := wp.processingTimeNs
	
	// Calculate events processed since last check
	recentProcessed := currentProcessed - wp.lastProcessed
	wp.lastProcessed = currentProcessed
	wp.metricsMu.Unlock()

	// Calculate processing rate (events per second)
	timeSinceLastCheck := time.Since(wp.lastCheckTime).Seconds()
	processingRate := 0.0
	if timeSinceLastCheck > 0 {
		processingRate = float64(recentProcessed) / timeSinceLastCheck
	}
	
	// Calculate average processing time in ms (if we've processed any events)
	avgProcessingTime := 0.0
	totalDeliveries := successfulDeliveries + failedDeliveries
	if totalDeliveries > 0 {
		avgProcessingTime = float64(processingTimeNs) / float64(totalDeliveries) / 1e6
	}

	log.Printf("[%s] Stats: queue=%d/%d (%.1f%%), workers=%d, rate=%.1f/sec, success=%d, fail=%d, avg_time=%.1fms", 
		wp.name, queueLength, queueCapacity, utilizationPct, wp.activeWorkers, 
		processingRate, successfulDeliveries, failedDeliveries, avgProcessingTime)
		
	// If we've been idle too long, scale down
	if processingRate < 0.1 && wp.activeWorkers > wp.minWorkers {
		// Scale down more aggressively if completely idle
		workersToRemove := 1
		if processingRate == 0 && queueLength == 0 {
			// Remove up to half of extra workers, but keep at least minWorkers
			extraWorkers := wp.activeWorkers - wp.minWorkers
			workersToRemove = min(extraWorkers, maxInt(1, extraWorkers/2))
		}
		
		log.Printf("[%s] Low activity (%.2f/sec), removing %d worker(s)", wp.name, processingRate, workersToRemove)
		
		for i := 0; i < workersToRemove; i++ {
			wp.removeWorker()
		}
	}
	
	// Scale up based on queue utilization and processing rate
	if queueLength > 0 {
		// Calculate expected time to process the current queue with current workers
		// Add a small constant to processingRate to avoid division by zero
		processingRateWithMin := math.Max(0.001, processingRate)
		expectedTimeToEmpty := float64(queueLength) / processingRateWithMin
		
		// If queue will take more than 5 seconds to empty or utilization is high, add workers
		if (expectedTimeToEmpty > 5 || utilizationPct > 50) && wp.activeWorkers < wp.maxWorkers {
			// More aggressive scaling when queue is very full
			workersToAdd := 1
			if utilizationPct > 80 || expectedTimeToEmpty > 30 {
				workersToAdd = min(3, wp.maxWorkers-wp.activeWorkers)
			}
			
			log.Printf("[%s] High load (queue=%.1f%%, est. time=%.1fs), adding %d worker(s)", 
				wp.name, utilizationPct, expectedTimeToEmpty, workersToAdd)
			
			for i := 0; i < workersToAdd; i++ {
				wp.addWorker()
			}
		}
	}
	
	wp.lastCheckTime = time.Now()
}

func (wp *WorkerPool) GetStats() map[string]interface{} {
	wp.metricsMu.Lock()
	defer wp.metricsMu.Unlock()
	
	var avgProcessingTimeMs float64 = 0
	totalDeliveries := wp.successfulDeliveries + wp.failedDeliveries
	if totalDeliveries > 0 {
		avgProcessingTimeMs = float64(wp.processingTimeNs) / float64(totalDeliveries) / 1e6
	}
	
	return map[string]interface{}{
		"pool_name":            wp.name,
		"active_workers":       wp.activeWorkers,
		"successful_deliveries": wp.successfulDeliveries,
		"failed_deliveries":    wp.failedDeliveries,
		"total_processed":      wp.totalProcessed,
		"avg_processing_time_ms": avgProcessingTimeMs,
		"queue_capacity":       cap(wp.eventQueue),
		"queue_length":         len(wp.eventQueue),
	}
}

func (wp *WorkerPool) Shutdown() {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	
	log.Printf("[%s] Shutting down worker pool with %d active workers", wp.name, wp.activeWorkers)
	
	for id, worker := range wp.workers {
		close(worker.stopChan)
		delete(wp.workers, id)
	}
	
	wp.activeWorkers = 0
	log.Printf("[%s] Worker pool shut down", wp.name)
}

func (wp *WorkerPool) RecordSuccess() {
	wp.metricsMu.Lock()
	defer wp.metricsMu.Unlock()
	wp.successfulDeliveries++
	wp.totalProcessed++
}

func (wp *WorkerPool) RecordFailure() {
	wp.metricsMu.Lock()
	defer wp.metricsMu.Unlock()
	wp.failedDeliveries++
	wp.totalProcessed++
}

func (wp *WorkerPool) RecordProcessingTime(duration time.Duration) {
	wp.metricsMu.Lock()
	defer wp.metricsMu.Unlock()
	wp.processingTimeNs += duration.Nanoseconds()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Convert float64 to int safely
func floatToInt(f float64) int {
	if f > float64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	if f < -float64(^uint(0)>>1) {
		return -int(^uint(0) >> 1)
	}
	return int(f)
}

// Integer version of max
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
