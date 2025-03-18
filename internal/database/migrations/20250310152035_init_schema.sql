-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS webhook_configs (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    post_url VARCHAR(1024) NOT NULL,
    events TEXT[] NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS webhook_events (
    id SERIAL PRIMARY KEY,
    webhook_id INTEGER REFERENCES webhook_configs(id),
    event_name VARCHAR(255) NOT NULL,
    event_time TIMESTAMP WITH TIME ZONE NOT NULL,
    subscriber_data JSONB NOT NULL,
    segment_data JSONB,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS delivery_attempts (
    id SERIAL PRIMARY KEY,
    event_id INTEGER REFERENCES webhook_events(id),
    status VARCHAR(50) NOT NULL,
    response TEXT,
    error TEXT,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for better performance
CREATE INDEX IF NOT EXISTS idx_webhook_events_status ON webhook_events(status);
CREATE INDEX IF NOT EXISTS idx_webhook_events_webhook_id ON webhook_events(webhook_id);
CREATE INDEX IF NOT EXISTS idx_webhook_events_name ON webhook_events(event_name);
CREATE INDEX IF NOT EXISTS idx_delivery_attempts_event ON delivery_attempts(event_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_delivery_attempts_event;
DROP INDEX IF EXISTS idx_webhook_events_name;
DROP INDEX IF EXISTS idx_webhook_events_webhook_id;
DROP INDEX IF EXISTS idx_webhook_events_status;
DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS webhook_events;
DROP TABLE IF EXISTS webhook_configs;
-- +goose StatementEnd
