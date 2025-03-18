-- +goose Up
-- +goose StatementBegin
-- Seed webhook configurations
INSERT INTO webhook_configs (name, post_url, events, created_at)
VALUES 
  ('Webhook 1', 'https://example.com/webhook1', ARRAY['subscriber.created', 'subscriber.unsubscribed'], NOW()),
  ('Webhook 2', 'https://example.com/webhook2', ARRAY['subscriber.created', 'subscriber.unsubscribed', 'subscriber.added_to_segment'], NOW()),
  ('Webhook 3', 'https://example.com/webhook3', ARRAY['subscriber.created'], NOW()),
  ('Webhook 4', 'https://example.com/webhook4', ARRAY['subscriber.created', 'subscriber.unsubscribed'], NOW()),
  ('Webhook 5', 'https://example.com/webhook5', ARRAY['subscriber.created', 'subscriber.unsubscribed'], NOW()),
  ('Webhook 6', 'https://example.com/webhook6', ARRAY['subscriber.added_to_segment'], NOW()),
  ('Webhook 7', 'https://example.com/webhook7', ARRAY['subscriber.created', 'subscriber.unsubscribed', 'subscriber.added_to_segment'], NOW()),
  ('Webhook 8', 'https://example.com/webhook8', ARRAY['subscriber.created', 'subscriber.unsubscribed', 'subscriber.added_to_segment'], NOW()),
  ('Webhook 9', 'https://example.com/webhook9', ARRAY['subscriber.unsubscribed'], NOW()),
  ('Webhook 10', 'https://example.com/webhook10', ARRAY['subscriber.created', 'subscriber.unsubscribed', 'subscriber.added_to_segment'], NOW());
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove seed data
DELETE FROM webhook_configs 
WHERE name IN (
  'Webhook 1',
  'Webhook 2',
  'Webhook 3',
  'Webhook 4',
  'Webhook 5',
  'Webhook 6',
  'Webhook 7',
  'Webhook 8',
  'Webhook 9',
  'Webhook 10'
);
-- +goose StatementEnd
