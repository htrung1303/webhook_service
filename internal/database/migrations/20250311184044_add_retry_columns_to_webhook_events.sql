-- +goose Up
-- +goose StatementBegin
ALTER TABLE webhook_events 
ADD COLUMN IF NOT EXISTS attempts INTEGER DEFAULT 0,
ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMP WITH TIME ZONE;

-- Add an index to improve query performance for retry operations
CREATE INDEX IF NOT EXISTS idx_webhook_events_attempts ON webhook_events(attempts);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove the index first
DROP INDEX IF EXISTS idx_webhook_events_attempts;

-- Then remove the columns
ALTER TABLE webhook_events 
DROP COLUMN IF EXISTS last_attempt_at,
DROP COLUMN IF EXISTS attempts;
-- +goose StatementEnd
