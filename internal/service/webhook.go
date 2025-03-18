package service

import (
    "fmt"
    "time"
    "math"
    "math/rand"
    "log"
    "webhook-service/internal/model"
    "webhook-service/internal/repository"
    "webhook-service/internal/config"
)

type WebhookService struct {
    webhookRepo       *repository.WebhookRepository
    httpClient        *HTTPClientService
    eventQueueManager *EventQueueManager
    workerPool        *WorkerPool
    webhookConfigCache *WebhookConfigCache
    cfg               *config.Config
}

func NewWebhookService(webhookRepo *repository.WebhookRepository, httpClient *HTTPClientService, eventQueueManager *EventQueueManager, cfg *config.Config) *WebhookService {
    // Create the webhook config cache
    webhookConfigCache := NewWebhookConfigCache(15*time.Minute, webhookRepo)
    
    // Start periodic refresh every hour
    webhookConfigCache.StartPeriodicRefresh(1 * time.Hour)
    
    ws := &WebhookService{
        webhookRepo:       webhookRepo,
        httpClient:        httpClient,
        eventQueueManager: eventQueueManager,
        webhookConfigCache: webhookConfigCache,
        cfg:               cfg,
    }

    ws.workerPool = NewNamedWorkerPool("DeliveryWorkerPool", cfg.MinWorkers, cfg.MaxWorkers, eventQueueManager.eventQueue, ws.processEvent)

    eventQueueManager.Start()

    return ws
}

func (ws *WebhookService) processEvent(event *model.WebhookEvent) {
    // Get post URL from cache instead of direct DB query
    postURL, err := ws.webhookConfigCache.GetPostURL(event.WebhookID)
    if err != nil {
        log.Printf("Error retrieving webhook config for ID %s: %v", event.WebhookID, err)
        ws.webhookRepo.LogDeliveryAttempt(event.ID, false, "", fmt.Errorf("webhook config not found"))
        ws.webhookRepo.UpdateEventStatus(event.ID, "failed")
        ws.workerPool.RecordFailure()
        return
    }

    attemptCount := event.Attempts + 1
    success, response, err := ws.httpClient.DeliverWebhook(postURL, event)
    ws.webhookRepo.LogDeliveryAttempt(event.ID, success, response, err)

    duration := time.Since(event.EventTime)
    ws.workerPool.RecordProcessingTime(duration)

    if success {
        log.Printf("Successfully delivered event %s to webhook %s (attempt %d/%d)", 
            event.ID, event.WebhookID, attemptCount, ws.cfg.MaxRetries)
        ws.webhookRepo.UpdateEventStatus(event.ID, "success")
        ws.workerPool.RecordSuccess()
        return
    }

    log.Printf("Failed to deliver event %s to webhook %s (attempt %d/%d): %v", 
        event.ID, event.WebhookID, attemptCount, ws.cfg.MaxRetries, err)
    
    if attemptCount >= ws.cfg.MaxRetries {
        log.Printf("Max retries reached for event %s, marking as failed", event.ID)
        ws.webhookRepo.UpdateEventStatus(event.ID, "failed")
        ws.workerPool.RecordFailure()
        return
    }

    delay := calculateBackoff(attemptCount, ws.cfg.InitialDelay, ws.cfg.MaxDelay)
    log.Printf("Scheduling retry for event %s in %v (attempt %d/%d)", 
        event.ID, delay, attemptCount+1, ws.cfg.MaxRetries)

    event.Attempts = attemptCount
    now := time.Now()
    event.LastAttemptAt = &now

    ws.webhookRepo.UpdateEventAttempts(event.ID, attemptCount, event.LastAttemptAt)

    err = ws.eventQueueManager.ScheduleRetry(event.ID, delay)
    if err != nil {
        log.Printf("Failed to schedule retry for event %s: %v", event.ID, err)
        ws.webhookRepo.UpdateEventStatus(event.ID, "failed")
        ws.workerPool.RecordFailure()
    }
}

func calculateBackoff(attemptCount int, initialDelay, maxDelay time.Duration) time.Duration {
    // Base delay grows exponentially: initialDelay * 2^(attemptCount-1)
    // For example, with initialDelay=1s: 1s, 2s, 4s, 8s, 16s, ...
    backoff := float64(initialDelay) * math.Pow(2, float64(attemptCount-1))
    
    // Add jitter (random value between 0-30% of the backoff)
    jitter := rand.Float64() * 0.3 * backoff
    
    // Calculate final delay with jitter
    delay := time.Duration(backoff + jitter)
    
    // Cap at max delay
    if delay > maxDelay {
        delay = maxDelay
    }
    
    return delay
}

func (ws *WebhookService) GetWorkerStats() map[string]interface{} {
    stats := ws.workerPool.GetStats()
    
    cacheStats := ws.webhookConfigCache.GetCacheStats()
    for k, v := range cacheStats {
        stats["cache_"+k] = v
    }
    
    return stats
}

func (ws *WebhookService) Shutdown() {
    ws.webhookConfigCache.StopPeriodicRefresh()
    
    // Stop the worker pool
    if ws.workerPool != nil {
        ws.workerPool.Shutdown()
    }
    
    log.Println("WebhookService shut down")
}
