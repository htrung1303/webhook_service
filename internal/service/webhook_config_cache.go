package service

import (
	"database/sql"
	"log"
	"sync"
	"time"

	"webhook-service/internal/repository"
)

type WebhookConfigCache struct {
	webhookConfigs map[string]*repository.WebhookConfigDTO
	expiry         map[string]time.Time
	mu             sync.RWMutex
	ttl            time.Duration
	repo           *repository.WebhookRepository
	stopChan       chan struct{}
	refreshWg      sync.WaitGroup
}

func NewWebhookConfigCache(ttl time.Duration, repo *repository.WebhookRepository) *WebhookConfigCache {
	cache := &WebhookConfigCache{
		webhookConfigs: make(map[string]*repository.WebhookConfigDTO),
		expiry:         make(map[string]time.Time),
		ttl:            ttl,
		repo:           repo,
		stopChan:       make(chan struct{}),
	}
	
	return cache
}

// StartPeriodicRefresh begins a background goroutine that periodically refreshes cached configs
func (c *WebhookConfigCache) StartPeriodicRefresh(interval time.Duration) {
	c.refreshWg.Add(1)
	go func() {
		defer c.refreshWg.Done()
		
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				c.RefreshAllConfigs()
			case <-c.stopChan:
				log.Println("Stopping webhook config cache refresh routine")
				return
			}
		}
	}()
	
	log.Printf("Started periodic webhook config refresh (interval: %v)", interval)
}

// StopPeriodicRefresh stops the periodic refresh goroutine
func (c *WebhookConfigCache) StopPeriodicRefresh() {
	close(c.stopChan)
	c.refreshWg.Wait()
}

// ValidateWebhook checks if a webhook supports a specific event type
func (c *WebhookConfigCache) ValidateWebhook(webhookID, eventName string) (bool, error) {
	c.mu.RLock()
	if webhookConfig, exists := c.webhookConfigs[webhookID]; exists {
		if time.Now().Before(c.expiry[webhookID]) {
			for _, event := range webhookConfig.Events {
				if event == eventName {
					c.mu.RUnlock()
					return true, nil
				}
			}
			// Event not found in allowed events
			c.mu.RUnlock()
			return false, nil
		}
	}
	c.mu.RUnlock()

	webhook, err := c.repo.GetWebhookByID(webhookID)
	if err == sql.ErrNoRows {
    return false, nil
	} else if err != nil {
		return false, err
	}

	isAllowed := false
	for _, event := range webhook.Events {
		if event == eventName {
			isAllowed = true
			break
		}
	}
		
	c.mu.Lock()
	c.webhookConfigs[webhookID] = webhook
	c.expiry[webhookID] = time.Now().Add(c.ttl)
	c.mu.Unlock()

	return isAllowed, nil
}

func (c *WebhookConfigCache) GetPostURL(webhookID string) (string, error) {
	c.mu.RLock()
	if config, exists := c.webhookConfigs[webhookID]; exists {
		if time.Now().Before(c.expiry[webhookID]) {
			postURL := config.PostURL
			c.mu.RUnlock()
			if postURL != "" {
				return postURL, nil
			}
		}
	}
	c.mu.RUnlock()

	webhook, err := c.repo.GetWebhookByID(webhookID)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.webhookConfigs[webhookID] = webhook
	c.expiry[webhookID] = time.Now().Add(c.ttl)
	c.mu.Unlock()

	return webhook.PostURL, nil
}

func (c *WebhookConfigCache) RefreshConfig(webhookID string) error {
	config, err := c.repo.GetWebhookByID(webhookID)
	if err != nil {
		return err
	}
	
	c.mu.Lock()
	c.webhookConfigs[webhookID] = config
	c.expiry[webhookID] = time.Now().Add(c.ttl)
	c.mu.Unlock()
	
	log.Printf("Refreshed cache for webhook ID: %s", webhookID)
	return nil
}

func (c *WebhookConfigCache) RefreshAllConfigs() {
	c.mu.RLock()
	webhookIDs := make([]string, 0, len(c.webhookConfigs))
	for id := range c.webhookConfigs {
		webhookIDs = append(webhookIDs, id)
	}
	c.mu.RUnlock()
	
	refreshCount := 0
	errorCount := 0
	
	for _, id := range webhookIDs {
		if err := c.RefreshConfig(id); err != nil {
			log.Printf("Error refreshing webhook %s: %v", id, err)
			errorCount++
		} else {
			refreshCount++
		}
	}
	
	log.Printf("Refreshed %d webhook configs (%d errors)", refreshCount, errorCount)
}

func (c *WebhookConfigCache) InvalidateCache(webhookID string) {
	c.mu.Lock()
	delete(c.webhookConfigs, webhookID)
	delete(c.expiry, webhookID)
	c.mu.Unlock()
	
	log.Printf("Invalidated cache for webhook ID: %s", webhookID)
}

func (c *WebhookConfigCache) GetCacheStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	stats := map[string]interface{}{
		"total_cached": len(c.webhookConfigs),
		"ttl_seconds":  c.ttl.Seconds(),
	}
	
	now := time.Now()
	expired := 0
	for _, expiryTime := range c.expiry {
		if now.After(expiryTime) {
			expired++
		}
	}
	
	stats["expired_entries"] = expired
	
	return stats
}

