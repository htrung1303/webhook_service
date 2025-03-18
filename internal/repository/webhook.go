package repository

import (
	"time"
	"log"
	"strings"
	"database/sql"
	"encoding/json"
	"fmt"
	"webhook-service/internal/model"
	"github.com/lib/pq"
	"context"
)

type WebhookRepository struct {
	db *sql.DB
}

type WebhookConfigDTO struct {
	ID      string   `json:"id"`
	Events  []string `json:"events"`
	PostURL string   `json:"post_url"`
}

func NewWebhookRepository(db *sql.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

func (r *WebhookRepository) GetDB() *sql.DB {
	return r.db
}

func (wr *WebhookRepository) ValidateWebhook(webhookID string, eventName string) (bool, error) {
	var valid bool
	
	err := wr.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM webhook_configs 
			WHERE id = $1 AND $2 = ANY(events)
		)`, 
		webhookID, eventName,
	).Scan(&valid)
	
	return valid, err
}

func (wr *WebhookRepository) GetWebhookByID(webhookID string) (*WebhookConfigDTO, error) {
	var config WebhookConfigDTO
	var eventsStr string
	
	err := wr.db.QueryRow(`
        SELECT id, post_url, array_to_string(events, ',') as events
        FROM webhook_configs 
        WHERE id = $1`, 
        webhookID,
    ).Scan(
        &config.ID,
        &config.PostURL,
        &eventsStr,
    )
    
    if err != nil {
        return nil, err
    }
    
    if eventsStr != "" {
        config.Events = strings.Split(eventsStr, ",")
    } else {
        config.Events = []string{}
    }
    
    return &config, nil
}

func (wr *WebhookRepository) StoreEvent(event *model.WebhookEvent) (string, error) {
	subscriberData, err := json.Marshal(event.Subscriber)
	if err != nil {
		return "", err
	}

	var segmentData []byte
	if event.Segment != nil {
		segmentData, err = json.Marshal(event.Segment)
		if err != nil {
			return "", err
		}
	}

	var eventID string
	err = wr.db.QueryRow(`
		INSERT INTO webhook_events 
		(webhook_id, event_name, event_time, subscriber_data, segment_data, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		RETURNING id`,
		event.WebhookID, event.EventName, event.EventTime, subscriberData, segmentData).Scan(&eventID)

	if err != nil {
		return "", err
	}

	return eventID, nil
}

func (wr *WebhookRepository) UpdateEventAttempts(eventID string, attempts int, lastAttempt *time.Time) error {
	_, err := wr.db.Exec(`
		UPDATE webhook_events 
		SET attempts = $1, last_attempt_at = $2 
		WHERE id = $3`,
		attempts, lastAttempt, eventID)
	
	if err != nil {
		log.Printf("Error updating event attempts: %v", err)
	}
	
	return err
}

func (wr *WebhookRepository) LogDeliveryAttempt(eventID string, success bool, response string, err error) {
	status := "success"
	if !success {
		status = "failed"
	}

	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	_, err = wr.db.Exec(`
		INSERT INTO delivery_attempts (event_id, status, response, error)
		VALUES ($1, $2, $3, $4)`,
		eventID, status, response, errStr)
}

func (wr *WebhookRepository) GetEventsToProcess(txn *sql.Tx, limit int) (*sql.Rows, error) {
	query := `
		WITH webhook_count AS (
				SELECT COUNT(DISTINCT webhook_id) as count
				FROM webhook_events
				WHERE status = 'pending'
					AND (next_attempt_at IS NULL OR next_attempt_at <= CURRENT_TIMESTAMP)
		),
		events_per_webhook AS (
				SELECT GREATEST(1, CEIL($1::float / NULLIF(count, 0))) as count
				FROM webhook_count
		),
		webhook_selection AS (
				SELECT e.id,
								ROW_NUMBER() OVER (PARTITION BY e.webhook_id 
																	ORDER BY e.next_attempt_at NULLS FIRST, e.created_at) as rn
				FROM webhook_events e
				WHERE e.status = 'pending' 
					AND (e.next_attempt_at IS NULL OR e.next_attempt_at <= CURRENT_TIMESTAMP)
		)` + 
		wr.getEventSelectSQL() + ` e
		JOIN webhook_selection ws ON e.id = ws.id
		CROSS JOIN events_per_webhook epw
		WHERE ws.rn <= epw.count
		ORDER BY e.next_attempt_at NULLS FIRST, e.created_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	rows, err := txn.Query(query, limit)
	if err != nil {
		return nil, err
	}

	return rows, nil
}


// ReleaseEvent releases an event back to pending status
func (wr *WebhookRepository) ReleaseEvent(eventID string) error {
	return wr.withTransaction(func(tx *sql.Tx) error {
		// Update the event status
		_, err := tx.Exec(`
			UPDATE webhook_events
			SET status = 'pending',
				processing_instance = NULL,
				processing_started_at = NULL
			WHERE id = $1`,
			eventID)
		
		if err != nil {
			return fmt.Errorf("failed to release event: %w", err)
		}
		
		return nil
	})
}

// GetAndMarkEventsForProcessing fetches events that are ready to be processed,
// marks them as processing, and returns them in a single transaction
func (wr *WebhookRepository) GetAndMarkEventsForProcessing(txn *sql.Tx, limit int, processingInstanceID string) ([]model.WebhookEvent, error) {
	// Get events with FOR UPDATE SKIP LOCKED
	rows, err := wr.GetEventsToProcess(txn, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get events to process: %w", err)
	}
	defer rows.Close()

	events, eventIDs, err := wr.prepareEventsFromRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare events: %w", err)
	}

	if len(events) == 0 {
		return events, nil
	}

	// Mark events as processing
	err = wr.markEventsProcessing(txn, eventIDs, processingInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark events as processing: %w", err)
	}

	return events, nil
}

func (wr *WebhookRepository) markEventsProcessing(txn *sql.Tx, eventIDs []string, processingInstanceID string) error {
	query := `
        UPDATE webhook_events
        SET status = 'processing', 
            processing_instance = $1,
            processing_started_at = CURRENT_TIMESTAMP,
            attempts = attempts + 1
        WHERE id = ANY($2)`

	_, err := txn.Exec(query, processingInstanceID, pq.Array(eventIDs))
	return err
}

func (wr *WebhookRepository) prepareEventsFromRows(rows *sql.Rows) ([]model.WebhookEvent, []string, error) {
	eventIDs := make([]string, 0)
	events := make([]model.WebhookEvent, 0)

	for rows.Next() {
		event, err := wr.scanEventFromRow(rows)
		if err != nil {
			return nil, nil, fmt.Errorf("error scanning event row: %w", err)
		}

		eventIDs = append(eventIDs, event.ID)
		events = append(events, *event)
	}

	return events, eventIDs, nil
}

func (wr *WebhookRepository) UpdateRetrySchedule(eventID string, delay time.Duration) error {
	nextAttemptAt := time.Now().Add(delay)

	return wr.withTransaction(func(tx *sql.Tx) error {
		// Update the event with the next attempt time
		result, err := tx.Exec(`
			UPDATE webhook_events
			SET status = 'pending',
				last_attempt_at = CURRENT_TIMESTAMP,
				next_attempt_at = $1,
				processing_instance = NULL,
				processing_started_at = NULL
			WHERE id = $2`,
			nextAttemptAt, eventID)

		if err != nil {
			return fmt.Errorf("failed to schedule retry: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			return fmt.Errorf("event %s not found or already processed", eventID)
		}

		return nil
	})
}

// UpdateStalledEventsToPending resets stalled events back to pending status
func (wr *WebhookRepository) UpdateStalledEventsToPending(stalledTimeout time.Duration) (int64, error) {
	result, err := wr.db.Exec(`
		UPDATE webhook_events
		SET status = 'pending',
			processing_instance = NULL,
			processing_started_at = NULL
		WHERE status = 'processing'
		AND processing_started_at < $1`,
		time.Now().Add(-stalledTimeout))

	if err != nil {
		return 0, fmt.Errorf("failed to cleanup stalled events: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get affected rows count: %w", err)
	}

	return count, nil
}

func (wr *WebhookRepository) GetAndMarkEventByID(eventID string, processingInstance string) (*model.WebhookEvent, error) {
	var event *model.WebhookEvent
	
	err := wr.withTransaction(func(tx *sql.Tx) error {
		// Fetch the event from the database with a lock
		// Using FOR UPDATE SKIP LOCKED ensures only one instance will get the row
		query := wr.getEventSelectSQL() + ` 
			WHERE id = $1 AND status = 'pending'
			FOR UPDATE SKIP LOCKED`
		
		row := tx.QueryRow(query, eventID)
		var err error
		event, err = wr.scanEventFromRow(row)
		
		if err != nil {
			if err == sql.ErrNoRows {
				// Event was either not found or already locked by another instance
				return fmt.Errorf("event %s not found or already being processed", eventID)
			}
			return fmt.Errorf("error fetching event %s: %w", eventID, err)
		}
		
		// Mark the event as processing within the transaction
		_, err = tx.Exec(`
			UPDATE webhook_events
			SET status = 'processing',
				processing_instance = $1,
				processing_started_at = CURRENT_TIMESTAMP,
				attempts = attempts + 1
			WHERE id = $2`,
			processingInstance, event.ID)
		
		if err != nil {
			return fmt.Errorf("error marking event %s as processing: %w", event.ID, err)
		}
		
		return nil
	})
	
	if err != nil {
		return nil, err
	}
	

	instanceStr := processingInstance
	event.ProcessingInstance = &instanceStr
	
	return event, nil
}

// scanEventFromRow scans a single event from a database row or query result
func (wr *WebhookRepository) scanEventFromRow(scanner interface {
	Scan(dest ...interface{}) error
}) (*model.WebhookEvent, error) {
	var event model.WebhookEvent
	var subscriberData, segmentData []byte
	var lastAttemptAt, processedAt, nextAttemptAt, processingStartedAt sql.NullTime
	var processingInstance sql.NullString
	
	err := scanner.Scan(
		&event.ID,
		&event.WebhookID,
		&event.EventName,
		&event.EventTime,
		&subscriberData,
		&segmentData,
		&event.Status,
		&event.Attempts,
		&lastAttemptAt,
		&event.CreatedAt,
		&processedAt,
		&nextAttemptAt,
		&processingInstance,
		&processingStartedAt,
	)
	
	if err != nil {
		return nil, err
	}
	
	// Convert sql.NullTime to *time.Time
	if lastAttemptAt.Valid {
		event.LastAttemptAt = &lastAttemptAt.Time
	}
	
	if processedAt.Valid {
		event.ProcessedAt = &processedAt.Time
	}
	
	if nextAttemptAt.Valid {
		event.NextAttemptAt = &nextAttemptAt.Time
	}
	
	if processingStartedAt.Valid {
		event.ProcessingStartedAt = &processingStartedAt.Time
	}
	
	// Convert sql.NullString to *string
	if processingInstance.Valid {
		event.ProcessingInstance = &processingInstance.String
	}
	
	// Parse subscriber data
	if err := json.Unmarshal(subscriberData, &event.Subscriber); err != nil {
		return nil, fmt.Errorf("error parsing subscriber data: %w", err)
	}
	
	// Parse segment data if present
	if len(segmentData) > 0 {
		event.Segment = &model.Segment{}
		if err := json.Unmarshal(segmentData, event.Segment); err != nil {
			return nil, fmt.Errorf("error parsing segment data: %w", err)
		}
	}
	
	return &event, nil
}


func (wr *WebhookRepository) withTransaction(fn func(*sql.Tx) error) error {
	tx, err := wr.db.BeginTx(context.Background(), &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Will be ignored if transaction is committed
	
	if err := fn(tx); err != nil {
		return err
	}
	
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	
	return nil
}

func (wr *WebhookRepository) UpdateEventStatus(eventID string, status string) error {
	_, err := wr.db.Exec(`
		UPDATE webhook_events 
		SET status = $1
		WHERE id = $2`,
		status, eventID)
	
	if err != nil {
		return fmt.Errorf("failed to update event status to %s: %w", status, err)
	}
	
	return nil
}

// getEventSelectSQL returns the standard SQL for selecting all event fields
func (wr *WebhookRepository) getEventSelectSQL() string {
	return `
		SELECT id, webhook_id, event_name, event_time, 
			   subscriber_data, segment_data, status, attempts,
			   last_attempt_at, created_at, processed_at,
			   next_attempt_at, processing_instance, processing_started_at
		FROM webhook_events`
}
