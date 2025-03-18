package server

import (
	"log"
	"net/http"
	"webhook-service/internal/model"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func (s *Server) RegisterRoutes() http.Handler {
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"https://*", "http://*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check endpoint
	e.GET("/health", s.healthHandler)

	// Webhook event endpoint
	e.POST("/webhooks/events", s.handleWebhookEvent)

	return e
}

// handleWebhookEvent receives webhook events and queues them for processing
func (s *Server) handleWebhookEvent(c echo.Context) error {
	var event model.WebhookEvent
	if err := c.Bind(&event); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid event format",
		})
	}

	// Validate event data
	if err := event.Validate(); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
	}

	valid, err := s.webhookConfigCache.ValidateWebhook(event.WebhookID, event.EventName)
	if err != nil {
			log.Printf("Error validating webhook: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"error": "Error validating webhook",
			})
	} else if !valid {
			return c.JSON(http.StatusBadRequest, map[string]string{
					"error": "Webhook not found or event type not allowed",
			})
	}

	// Try to add to the request buffer
	select {
	case s.requestBuffer <- &event:
		return c.JSON(http.StatusAccepted, map[string]string{
			"message": "Event accepted for processing",
		})
	default:
		return c.JSON(http.StatusTooManyRequests, map[string]string{
			"error": "Server is currently at capacity, please try again later",
		})
	}
}

func (s *Server) healthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, s.db.Health())
}
