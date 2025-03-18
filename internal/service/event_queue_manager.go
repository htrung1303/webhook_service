package service

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"webhook-service/internal/config"
	"webhook-service/internal/model"
	"webhook-service/internal/repository"
)

type EventQueueManager struct {
	db *sql.DB
	eventQueue chan *model.WebhookEvent
	mu sync.RWMutex
	config *config.Config
	stopChan chan struct{}
	workerWg sync.WaitGroup
	instanceID string
	webhookRepo *repository.WebhookRepository
	eventListener *EventQueueListener
	connString string
	backupPollingInterval time.Duration
	maxEventsPerPoll int
	retryCheckInterval time.Duration
}

func NewEventQueueManager(db *sql.DB, cfg *config.Config, webhookRepo *repository.WebhookRepository, connString string, instanceID string) *EventQueueManager {
	return &EventQueueManager{
		db: db,
		eventQueue: make(chan *model.WebhookEvent, cfg.QueueBufferSize),
		config: cfg,
		stopChan: make(chan struct{}),
		instanceID: instanceID,
		webhookRepo: webhookRepo,
		connString: connString,
		backupPollingInterval: cfg.BackupPollingInterval, // Poll every 15 minutes as backup
		maxEventsPerPoll: cfg.MaxEventsPerPoll, // Process a smaller batch during backup polling
		retryCheckInterval: cfg.RetryCheckInterval, // Check for retries every 30 seconds by default
	}
}

func (eqm *EventQueueManager) Start() {
	eqm.mu.Lock()
	defer eqm.mu.Unlock()
	
	// Create and start the event listener with the same instance ID
	eqm.eventListener = NewEventQueueListener(eqm.connString, eqm.eventQueue, eqm.webhookRepo, eqm.instanceID)
	eqm.eventListener.Start()
	
	// Start the backup polling routine
	eqm.workerWg.Add(1)
	go eqm.backupPollingRoutine()
	
	// Start the retry scheduler routine
	eqm.workerWg.Add(1)
	go eqm.retrySchedulerRoutine()
	
	// Start the cleanup routine
	eqm.workerWg.Add(1)
	go eqm.cleanupRoutine()

	log.Printf("Starting event queue manager (instance ID: %s) with LISTEN/NOTIFY, retry scheduler, and backup polling", eqm.instanceID)
}

func (eqm *EventQueueManager) Stop() {
	eqm.mu.Lock()
	close(eqm.stopChan)
	eqm.mu.Unlock()

	// Stop the event listener
	if eqm.eventListener != nil {
		eqm.eventListener.Stop()
	}

	eqm.workerWg.Wait()
	log.Printf("Event queue manager (instance ID: %s) stopped", eqm.instanceID)
}

func (eqm *EventQueueManager) EnqueueEvent(event *model.WebhookEvent) (string, error) {
	eventID, err := eqm.webhookRepo.StoreEvent(event)

	if err != nil {
		return "", fmt.Errorf("failed to store event: %w", err)
	}

	log.Printf("Enqueued event %s for webhook %s", eventID, event.WebhookID)
	return eventID, nil
}

func (eqm *EventQueueManager) backupPollingRoutine() {
	defer eqm.workerWg.Done()
	
	// Wait a bit before starting the first poll to allow the system to initialize
	select {
	case <-time.After(1 * time.Minute):
	case <-eqm.stopChan:
		return
	}
	
	ticker := time.NewTicker(eqm.backupPollingInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			log.Printf("Running backup polling for missed events...")
			eqm.pollEvents()
		case <-eqm.stopChan:
			return
		}
	}
}

func (eqm *EventQueueManager) pollEvents() {
	// Skip polling if queue is more than 75% full
	if len(eqm.eventQueue) >= cap(eqm.eventQueue) * 3/4 {
		log.Printf("Main queue is at %d/%d capacity, skipping backup poll", 
			len(eqm.eventQueue), cap(eqm.eventQueue))
		return
	}

	txn, err := eqm.db.BeginTx(context.Background(), &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		log.Printf("Failed to begin transaction for backup polling: %v", err)
		return
	}
	defer txn.Rollback()

	// Get and mark events in a single repository call
	events, err := eqm.webhookRepo.GetAndMarkEventsForProcessing(txn, eqm.maxEventsPerPoll, eqm.instanceID)
	if err != nil {
		log.Printf("Failed during backup polling: %v", err)
		return
	}

	if len(events) == 0 {
		log.Printf("No missed events found during backup polling")
		return
	}

	// Commit the transaction
	if err := txn.Commit(); err != nil {
		log.Printf("Failed to commit transaction during backup polling: %v", err)
		return
	}

	log.Printf("Backup polling found %d missed events, enqueueing them", len(events))
	for _, event := range events {
		select {
		case <-eqm.stopChan:
			return
		default:
			select {
			case eqm.eventQueue <- &event:
				log.Printf("Backup poll: Enqueued event %s for webhook %s", event.ID, event.WebhookID)
			default:
				log.Printf("Event queue is full, releasing event: %s", event.ID)
				eqm.releaseEvent(event.ID)
			}
		}
	}
}

func (eqm *EventQueueManager) releaseEvent(eventID string) {
	if err := eqm.webhookRepo.ReleaseEvent(eventID); err != nil {
		log.Printf("Failed to release event %s: %v", eventID, err)
	}
}

func (eqm *EventQueueManager) cleanupRoutine() {
	defer eqm.workerWg.Done()
	
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			eqm.CleanupStalledEvents()
		case <-eqm.stopChan:
			return
		}
	}
}

func (eqm *EventQueueManager) ScheduleRetry(eventID string, delay time.Duration) error {
	err := eqm.webhookRepo.UpdateRetrySchedule(eventID, delay)
	if err != nil {
		return err
	}
	
	log.Printf("Scheduled retry for event %s in %v", eventID, delay)
	return nil
}

func (eqm *EventQueueManager) CleanupStalledEvents() {
	stalledTimeout := 3 * time.Minute
	
	count, err := eqm.webhookRepo.UpdateStalledEventsToPending(stalledTimeout)
	if err != nil {
		log.Printf("Failed to cleanup stalled events: %v", err)
		return
	}
	
	if count > 0 {
		log.Printf("Cleaned up %d stalled events", count)
	}
}

func (eqm *EventQueueManager) GetQueueMetrics() map[string]interface{} {
	eqm.mu.RLock()
	defer eqm.mu.RUnlock()

	var pendingCount, processingCount, completedCount, failedCount int
	
	eqm.db.QueryRow(`SELECT COUNT(*) FROM webhook_events WHERE status = 'pending'`).Scan(&pendingCount)
	eqm.db.QueryRow(`SELECT COUNT(*) FROM webhook_events WHERE status = 'processing'`).Scan(&processingCount)
	eqm.db.QueryRow(`SELECT COUNT(*) FROM webhook_events WHERE status = 'success'`).Scan(&completedCount)
	eqm.db.QueryRow(`SELECT COUNT(*) FROM webhook_events WHERE status = 'failed'`).Scan(&failedCount)

	// Get webhook-specific metrics
	rows, err := eqm.db.Query(`
		SELECT webhook_id, status, COUNT(*) 
		FROM webhook_events 
		GROUP BY webhook_id, status`)
	
	webhookMetrics := make(map[string]map[string]int)
	if err == nil {
		defer rows.Close()
		
		for rows.Next() {
			var webhookID, status string
			var count int
			
			if err := rows.Scan(&webhookID, &status, &count); err != nil {
				continue
			}
			
			if _, exists := webhookMetrics[webhookID]; !exists {
				webhookMetrics[webhookID] = make(map[string]int)
			}
			
			webhookMetrics[webhookID][status] = count
		}
	}

	return map[string]interface{}{
		"pending": pendingCount,
		"processing": processingCount,
		"success": completedCount,
		"failed": failedCount,
		"webhook_metrics": webhookMetrics,
	}
}

// retrySchedulerRoutine checks for events that have reached their retry time
// and updates them to trigger notifications
func (eqm *EventQueueManager) retrySchedulerRoutine() {
	defer eqm.workerWg.Done()
	
	// Wait a bit before starting the first check
	select {
	case <-time.After(15 * time.Second):
	case <-eqm.stopChan:
		return
	}
	
	ticker := time.NewTicker(eqm.retryCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			eqm.processReadyRetries()
		case <-eqm.stopChan:
			return
		}
	}
}

// processReadyRetries finds events that have reached their retry time
// and triggers notifications for them
func (eqm *EventQueueManager) processReadyRetries() {
	// Call the database function that checks for events ready for retry
	// and sends notifications for them
	_, err := eqm.db.Exec(`SELECT check_retry_events()`)
	
	if err != nil {
		log.Printf("Error processing ready retries: %v", err)
		return
	}
	
	// The function itself logs how many events were processed
}
