package service

import (
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "math/rand"
    "net/http"
    "time"
    "webhook-service/internal/model"
)

type HTTPClientService struct {
    client *http.Client
    mockEnabled bool // Flag to enable mock responses
}

func NewHTTPClientService(mockEnabled bool) *HTTPClientService {
    return &HTTPClientService{
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
        mockEnabled: mockEnabled,
    }
}

func (hcs *HTTPClientService) DeliverWebhook(url string, event *model.WebhookEvent) (bool, string, error) {
    // If mock is enabled, return simulated responses
    if hcs.mockEnabled {
        return hcs.mockDelivery(url, event)
    }
    
    // Real implementation for production use
    payload, err := json.Marshal(event)
    if err != nil {
        return false, "", err
    }

    resp, err := hcs.client.Post(url, "application/json", bytes.NewBuffer(payload))
    if err != nil {
        return false, "", err
    }
    defer resp.Body.Close()

    body := make([]byte, 1024)
    n, _ := resp.Body.Read(body)
    success := resp.StatusCode >= 200 && resp.StatusCode < 300
    return success, string(body[:n]), nil
}

// mockDelivery simulates webhook delivery with random outcomes
func (hcs *HTTPClientService) mockDelivery(url string, event *model.WebhookEvent) (bool, string, error) {
    // Seed the random number generator
    rand.Seed(time.Now().UnixNano())
    
    // Simulate processing delay (100-500ms)
    delay := 100 + rand.Intn(400)
    time.Sleep(time.Duration(delay) * time.Millisecond)
    
    // Determine outcome based on random number
    outcome := rand.Intn(10)
    
    switch {
    // 60% chance of success
    case outcome < 6:
        return true, fmt.Sprintf(`{"success":true,"message":"Event processed successfully","event_id":"%s"}`, event.ID), nil
        
    // 30% chance of failure with response
    case outcome < 9:
        statusCodes := []int{400, 401, 403, 404, 422, 429, 500, 502, 503}
        statusCode := statusCodes[rand.Intn(len(statusCodes))]
        errorMessages := map[int]string{
            400: "Bad request: invalid payload format",
            401: "Unauthorized: invalid API key",
            403: "Forbidden: insufficient permissions",
            404: "Not found: endpoint doesn't exist",
            422: "Unprocessable entity: validation failed",
            429: "Too many requests: rate limit exceeded",
            500: "Internal server error",
            502: "Bad gateway: upstream service error",
            503: "Service unavailable: system is overloaded",
        }
        
        return false, fmt.Sprintf(`{"error":"%s","status":%d}`, errorMessages[statusCode], statusCode), nil
        
    // 10% chance of network/timeout error
    default:
        errorTypes := []string{
            "connection refused",
            "timeout exceeded",
            "no such host",
            "network unreachable",
            "TLS handshake failure",
        }
        errorMsg := errorTypes[rand.Intn(len(errorTypes))]
        return false, "", errors.New("network error: " + errorMsg)
    }
}
