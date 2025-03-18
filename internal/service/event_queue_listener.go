package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	_ "embed"

	"github.com/jackc/pgx/v5"
	"webhook-service/internal/model"
	"webhook-service/internal/repository"
)

//go:embed sql/event_triggers.sql
var eventTriggerSQL string

type EventQueueListener struct {
	connString     string
	eventQueue     chan *model.WebhookEvent
	webhookRepo    *repository.WebhookRepository
	stopChan       chan struct{}
	listenerWg     sync.WaitGroup
	reconnectDelay time.Duration
	instanceID     string
}

type EventNotificationPayload struct {
	EventID   string  `json:"event_id"`
	WebhookID string  `json:"webhook_id"`
	Operation string  `json:"operation"` // INSERT, UPDATE, RETRY
	Timestamp float64 `json:"timestamp"` // Unix timestamp
}

func NewEventQueueListener(connString string, eventQueue chan *model.WebhookEvent, webhookRepo *repository.WebhookRepository, instanceID string) *EventQueueListener {
	return &EventQueueListener{
		connString:     connString,
		eventQueue:     eventQueue,
		webhookRepo:    webhookRepo,
		stopChan:       make(chan struct{}),
		reconnectDelay: 5 * time.Second,
		instanceID:     instanceID,
	}
}

func (l *EventQueueListener) Start() {
	l.listenerWg.Add(1)
	go func() {
		defer l.listenerWg.Done()
		l.listen()
	}()
	
	log.Println("Started webhook event listener")
}

func (l *EventQueueListener) Stop() {
	close(l.stopChan)
	l.listenerWg.Wait()
	log.Println("Stopped webhook event listener")
}

func (l *EventQueueListener) listen() {
	for {
		select {
		case <-l.stopChan:
			return
		default:
			if err := l.connectAndListen(); err != nil {
				log.Printf("Error in webhook event listener: %v. Reconnecting in %v...", 
					err, l.reconnectDelay)
				
				select {
				case <-time.After(l.reconnectDelay):
				case <-l.stopChan:
					return
				}
			}
		}
	}
}

func (l *EventQueueListener) connectAndListen() error {
	// Create a context that can be canceled when stopping
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Set up a goroutine to cancel the context when stopping
	go func() {
		select {
		case <-l.stopChan:
			cancel()
		case <-ctx.Done():
			return
		}
	}()
	
	// Connect to PostgreSQL with a timeout for the main listening connection
	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()
	
	// Create a dedicated connection just for LISTEN/NOTIFY
	listenConn, err := pgx.Connect(connCtx, l.connString)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL for listening: %w", err)
	}
	defer listenConn.Close(context.Background())
	
	// Set up the LISTEN
	_, err = listenConn.Exec(ctx, "LISTEN webhook_event_changes")
	if err != nil {
		return fmt.Errorf("failed to set up LISTEN: %w", err)
	}
	
	log.Println("Connected to PostgreSQL and listening for webhook event changes")
	
	// Set up a heartbeat to ensure the connection is still alive
	// This is done in a separate goroutine with its own connection
	heartbeatCtx, heartbeatCancel := context.WithCancel(ctx)
	defer heartbeatCancel()
	
	heartbeatErrCh := make(chan error, 1)
	go l.runHeartbeat(heartbeatCtx, heartbeatErrCh)
	
	// Main notification loop
	for {
		select {
		case err := <-heartbeatErrCh:
			return fmt.Errorf("heartbeat failed: %w", err)
		case <-ctx.Done():
			return nil
		default:
			// Wait for notification with a timeout
			notification, err := listenConn.WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Context was canceled, this is expected during shutdown
					return nil
				}
				return fmt.Errorf("error waiting for notification: %w", err)
			}
			
			// Process the notification
			if err := l.handleNotification(notification.Payload); err != nil {
				log.Printf("Error handling notification: %v", err)
				// Continue listening even if handling one notification fails
			}
		}
	}
}

// runHeartbeat runs a heartbeat in a separate goroutine with its own connection
func (l *EventQueueListener) runHeartbeat(ctx context.Context, errCh chan<- error) {
	// Connect to PostgreSQL with a timeout
	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()
	
	heartbeatConn, err := pgx.Connect(connCtx, l.connString)
	if err != nil {
		errCh <- fmt.Errorf("failed to connect to PostgreSQL for heartbeat: %w", err)
		return
	}
	defer heartbeatConn.Close(context.Background())
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Send a simple query to keep the connection alive
			var result int
			err := heartbeatConn.QueryRow(ctx, "SELECT 1").Scan(&result)
			if err != nil {
				errCh <- fmt.Errorf("heartbeat query failed: %w", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (l *EventQueueListener) handleNotification(payload string) error {
	var notification EventNotificationPayload
	
	if err := json.Unmarshal([]byte(payload), &notification); err != nil {
		return fmt.Errorf("error parsing event notification payload: %w", err)
	}
	
	// Use the repository to get and mark the event in a single transaction
	event, err := l.webhookRepo.GetAndMarkEventByID(notification.EventID, l.instanceID)
	
	if err != nil {
		if strings.Contains(err.Error(), "not found or already being processed") {
			log.Printf("Event %s not found or already being processed by another instance", notification.EventID)
			return nil
		}
		return fmt.Errorf("error getting and marking event: %w", err)
	}
	
	// Try to enqueue the event with a timeout
	select {
	case l.eventQueue <- event:
		log.Printf("Enqueued event %s for webhook %s via notification", event.ID, event.WebhookID)
		return nil
	case <-time.After(500 * time.Millisecond): // Short timeout to avoid blocking
		// If we can't enqueue, release the event back to pending
		log.Printf("Event queue is full, releasing event: %s", event.ID)
		if err := l.webhookRepo.ReleaseEvent(event.ID); err != nil {
			return fmt.Errorf("error releasing event %s: %w", event.ID, err)
		}
		return fmt.Errorf("event queue is full, could not enqueue event: %s", event.ID)
	}
}

func GetEventTriggerSQL() string {
	return eventTriggerSQL
}

func CheckEventTriggerExists(db interface{}) (bool, error) {
	var exists bool
	var err error
	
	switch conn := db.(type) {
	case *pgx.Conn:
		err = conn.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_trigger 
				WHERE tgname = 'webhook_event_change_trigger'
			)
		`).Scan(&exists)
	default:
		sqlDB, ok := db.(*sql.DB)
		if !ok {
			return false, fmt.Errorf("unsupported database connection type")
		}
		err = sqlDB.QueryRow(`
			SELECT EXISTS (
				SELECT 1 FROM pg_trigger 
				WHERE tgname = 'webhook_event_change_trigger'
			)
		`).Scan(&exists)
	}
	
	return exists, err
} 
