package model

import (
	"fmt"
	"time"
	"encoding/json"
)

// Event types
const (
	EventSubscriberCreated        = "subscriber.created"
	EventSubscriberUnsubscribed   = "subscriber.unsubscribed"
	EventSubscriberAddedToSegment = "subscriber.added_to_segment"
)

// WebhookConfig represents a webhook configuration
type WebhookConfig struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PostURL   string    `json:"post_url"`
	Events    []string  `json:"events"`
	CreatedAt time.Time `json:"created_at"`
}

// WebhookEvent represents the common structure for all webhook events
type WebhookEvent struct {
	ID          string    `json:"id"`
	WebhookID   string    `json:"webhook_id"`
	EventName   string    `json:"event_name"`
	EventTime   time.Time `json:"event_time"`
	Subscriber  Subscriber `json:"subscriber"`
	Segment     *Segment   `json:"segment,omitempty"` // Only present in subscriber.added_to_segment events
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	LastAttemptAt *time.Time `json:"last_attempt,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	ProcessingInstance *string `json:"processing_instance,omitempty"`
	ProcessingStartedAt *time.Time `json:"processing_started_at,omitempty"`
}

// Validate checks if the webhook event is valid
func (e *WebhookEvent) Validate() error {
	// Check if event type is valid
	if !isValidEventType(e.EventName) {
		return fmt.Errorf("invalid event type: %s", e.EventName)
	}

	// Check if webhook ID is provided
	if e.WebhookID == "" {
		return fmt.Errorf("webhook_id is required")
	}

	// Check if event time is provided
	if e.EventTime.IsZero() {
		return fmt.Errorf("event_time is required")
	}

	// Check if subscriber is valid
	if err := e.Subscriber.Validate(); err != nil {
		return fmt.Errorf("invalid subscriber: %w", err)
	}

	// Check if segment is provided for segment events
	if e.EventName == EventSubscriberAddedToSegment {
		if e.Segment == nil {
			return fmt.Errorf("segment is required for %s event", EventSubscriberAddedToSegment)
		}
		if err := e.Segment.Validate(); err != nil {
			return fmt.Errorf("invalid segment: %w", err)
		}
	}

	return nil
}

// isValidEventType checks if the event type is supported
func isValidEventType(eventType string) bool {
	switch eventType {
	case EventSubscriberCreated,
		EventSubscriberUnsubscribed,
		EventSubscriberAddedToSegment:
		return true
	}
	return false
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

// Validate checks if the subscriber data is valid
func (s *Subscriber) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("subscriber id is required")
	}
	if s.Email == "" {
		return fmt.Errorf("subscriber email is required")
	}
	if s.Status == "" {
		return fmt.Errorf("subscriber status is required")
	}
	return nil
}

// Segment represents a segment in the system
type Segment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Validate checks if the segment data is valid
func (s *Segment) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("segment id is required")
	}
	if s.Name == "" {
		return fmt.Errorf("segment name is required")
	}
	return nil
}

// DeliveryAttempt represents a single attempt to deliver a webhook
type DeliveryAttempt struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	Status    string    `json:"status"` // success, failed
	Response  string    `json:"response"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WebhookService represents the webhook processing service
type WebhookService struct {
	eventQueue    chan *WebhookEvent
	workerPool    chan struct{}
	webhookQueues map[string]chan *WebhookEvent
} 

// UnmarshalSubscriber unmarshals subscriber data from JSON
func UnmarshalSubscriber(data []byte, subscriber *Subscriber) error {
	return json.Unmarshal(data, subscriber)
}

// UnmarshalSegment unmarshals segment data from JSON
func UnmarshalSegment(data []byte, segment *Segment) error {
	return json.Unmarshal(data, segment)
} 
