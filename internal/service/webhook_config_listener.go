package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

//go:embed sql/webhook_config_triggers.sql
var webhookConfigTriggerSQL string

type WebhookConfigListener struct {
	connString     string
	cache          *WebhookConfigCache
	stopChan       chan struct{}
	listenerWg     sync.WaitGroup
	reconnectDelay time.Duration
}

type NotificationPayload struct {
	WebhookID string `json:"webhook_id"`
	Operation string `json:"operation"` // INSERT, UPDATE, DELETE
}

func NewWebhookConfigListener(connString string, cache *WebhookConfigCache) *WebhookConfigListener {
	return &WebhookConfigListener{
		connString:     connString,
		cache:          cache,
		stopChan:       make(chan struct{}),
		reconnectDelay: 5 * time.Second,
	}
}

func (l *WebhookConfigListener) Start() {
	l.listenerWg.Add(1)
	go func() {
		defer l.listenerWg.Done()
		l.listen()
	}()
	
	log.Println("Started webhook config change listener")
}

func (l *WebhookConfigListener) Stop() {
	close(l.stopChan)
	l.listenerWg.Wait()
	log.Println("Stopped webhook config change listener")
}

func (l *WebhookConfigListener) listen() {
	for {
		select {
		case <-l.stopChan:
			return
		default:
			if err := l.connectAndListen(); err != nil {
				log.Printf("Error in webhook config listener: %v. Reconnecting in %v...", 
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

func (l *WebhookConfigListener) connectAndListen() error {
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
	_, err = listenConn.Exec(ctx, "LISTEN webhook_config_changes")
	if err != nil {
		return fmt.Errorf("failed to set up LISTEN: %w", err)
	}
	
	log.Println("Connected to PostgreSQL and listening for webhook config changes")
	
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
			l.handleNotification(notification.Payload)
		}
	}
}

// runHeartbeat runs a heartbeat in a separate goroutine with its own connection
func (l *WebhookConfigListener) runHeartbeat(ctx context.Context, errCh chan<- error) {
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

func (l *WebhookConfigListener) handleNotification(payload string) {
	var notification NotificationPayload
	
	if err := json.Unmarshal([]byte(payload), &notification); err != nil {
		log.Printf("Error parsing notification payload: %v", err)
		return
	}
	
	log.Printf("Received webhook config change notification: %s operation on webhook %s", 
		notification.Operation, notification.WebhookID)
	
	l.cache.InvalidateCache(notification.WebhookID)
}

func GetTriggerSQL() string {
	return webhookConfigTriggerSQL
}

func CheckTriggerExists(db interface{}) (bool, error) {
	var exists bool
	var err error
	
	switch conn := db.(type) {
	case *pgx.Conn:
		err = conn.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_trigger 
				WHERE tgname = 'webhook_config_change_trigger'
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
				WHERE tgname = 'webhook_config_change_trigger'
			)
		`).Scan(&exists)
	}
	
	return exists, err
} 
