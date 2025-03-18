-- +goose Up
-- +goose StatementBegin
ALTER TABLE webhook_events 
ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN IF NOT EXISTS processing_instance VARCHAR(255),
ADD COLUMN IF NOT EXISTS processing_started_at TIMESTAMP WITH TIME ZONE;

-- Add an index to improve query performance for retry operations
CREATE INDEX IF NOT EXISTS idx_webhook_events_next_attempt_at ON webhook_events(next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_webhook_events_processing_instance ON webhook_events(processing_instance);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove the indexes first
DROP INDEX IF EXISTS idx_webhook_events_processing_instance;
DROP INDEX IF EXISTS idx_webhook_events_next_attempt_at;

-- Then remove the columns
ALTER TABLE webhook_events 
DROP COLUMN IF EXISTS processing_started_at,
DROP COLUMN IF EXISTS processing_instance,
DROP COLUMN IF EXISTS next_attempt_at;
-- +goose StatementEnd
