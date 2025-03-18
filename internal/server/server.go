package server

import (
	"database/sql"
	"os"
	"fmt"
	"log"
	"net/http"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"webhook-service/internal/database"
	"webhook-service/internal/model"
	"webhook-service/internal/service"
	"webhook-service/internal/config"
	"webhook-service/internal/repository"
)

type Server struct {
	port               int
	db                 database.Service
	webhookService     *service.WebhookService
	eventQueueManager  *service.EventQueueManager
	httpClient         *service.HTTPClientService
	requestWorkerPool  *service.WorkerPool
	webhookConfigCache *service.WebhookConfigCache
	configListener     *service.WebhookConfigListener
	webhookRepo        *repository.WebhookRepository
	requestBuffer      chan *model.WebhookEvent
}

func NewServer(cfg *config.Config) *http.Server {
	port := cfg.Port
	db := database.New(cfg)
	instanceID := fmt.Sprintf("instance_%d_%d", os.Getpid(), time.Now().UnixNano())

	webhookRepo := repository.NewWebhookRepository(db.GetDB())
	
	webhookConfigCache := service.NewWebhookConfigCache(15*time.Minute, webhookRepo)
	webhookConfigCache.StartPeriodicRefresh(1 * time.Hour)
	
	configListener := service.NewWebhookConfigListener(cfg.GetDSN(), webhookConfigCache)
	
	httpClient := service.NewHTTPClientService(cfg.MockEnabled)
	
	eventQueueManager := service.NewEventQueueManager(db.GetDB(), cfg, webhookRepo, cfg.GetDSN(), instanceID)
	
	webhookService := service.NewWebhookService(webhookRepo, httpClient, eventQueueManager, cfg)

	s := &Server{
		port:               port,
		db:                 db,
		webhookService:     webhookService,
		eventQueueManager:  eventQueueManager,
		webhookRepo:        webhookRepo,
		httpClient:         httpClient,
		webhookConfigCache: webhookConfigCache,
		configListener:     configListener,
		requestBuffer:      make(chan *model.WebhookEvent, cfg.RequestBufferSize),
	}

	// Set up database triggers for LISTEN/NOTIFY
	setupDatabaseTriggers(db.GetDB())
	
	// Start listeners
	configListener.Start()
	eventQueueManager.Start()

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	processRequestFn := func(event *model.WebhookEvent) {
		s.processWebhookRequest(event)
	}

	s.requestWorkerPool = service.NewNamedWorkerPool(
		"RequestWorkerPool",
		cfg.MinRequestWorkers,
		cfg.MaxRequestWorkers,
		s.requestBuffer,
		processRequestFn,
	)

	return server
}

// setupDatabaseTriggers sets up the necessary database triggers for LISTEN/NOTIFY
func setupDatabaseTriggers(db *sql.DB) {
	// Set up webhook config change trigger
	exists, err := service.CheckTriggerExists(db)
	if err != nil {
		log.Printf("Error checking webhook config trigger existence: %v", err)
	} else if !exists {
		log.Println("Creating webhook config change trigger...")
		_, err := db.Exec(service.GetTriggerSQL())
		if err != nil {
			log.Printf("Error creating webhook config change trigger: %v", err)
		} else {
			log.Println("Webhook config change trigger created successfully")
		}
	} else {
		log.Println("Webhook config change trigger already exists")
	}
	
	// Set up webhook event change trigger
	exists, err = service.CheckEventTriggerExists(db)
	if err != nil {
		log.Printf("Error checking webhook event trigger existence: %v", err)
	} else if !exists {
		log.Println("Creating webhook event change trigger...")
		_, err := db.Exec(service.GetEventTriggerSQL())
		if err != nil {
			log.Printf("Error creating webhook event change trigger: %v", err)
		} else {
			log.Println("Webhook event change trigger created successfully")
		}
	} else {
		log.Println("Webhook event change trigger already exists")
	}
	
	// Create index for better polling performance
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_webhook_events_pending_next_attempt 
		ON webhook_events (status, next_attempt_at) 
		WHERE status = 'pending'
	`)
	if err != nil {
		log.Printf("Error creating webhook events index: %v", err)
	} else {
		log.Println("Webhook events index created or already exists")
	}
}

func (s *Server) processWebhookRequest(event *model.WebhookEvent) {
	eventID, err := s.eventQueueManager.EnqueueEvent(event)
	if err != nil {
		log.Printf("Error queueing event: %v", err)
		s.requestWorkerPool.RecordFailure()
		return
	}
	
	// Record the processing time from event start to completion
	if event.EventTime.IsZero() {
		s.requestWorkerPool.RecordProcessingTime(0)
	} else {
		s.requestWorkerPool.RecordProcessingTime(time.Since(event.EventTime))
	}
	
	log.Printf("Event %s queued successfully for webhook %s", 
		eventID, event.WebhookID)
	s.requestWorkerPool.RecordSuccess()
}

func (s *Server) Shutdown() {
	if s.configListener != nil {
		s.configListener.Stop()
	}
	
	if s.webhookConfigCache != nil {
		s.webhookConfigCache.StopPeriodicRefresh()
	}
	
	if s.eventQueueManager != nil {
		s.eventQueueManager.Stop()
	}
	
	if s.webhookService != nil {
		s.webhookService.Shutdown()
	}
	
	if s.db != nil {
		s.db.Close()
	}
}
