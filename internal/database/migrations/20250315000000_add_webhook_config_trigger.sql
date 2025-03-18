-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_webhook_config_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('webhook_config_changes', json_build_object(
        'webhook_id', (CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END)::text,
        'operation', TG_OP
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS webhook_config_change_trigger ON webhook_configs;
CREATE TRIGGER webhook_config_change_trigger
AFTER UPDATE OR DELETE ON webhook_configs
FOR EACH ROW EXECUTE FUNCTION notify_webhook_config_change();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Remove the trigger and function
DROP TRIGGER IF EXISTS webhook_config_change_trigger ON webhook_configs;
DROP FUNCTION IF EXISTS notify_webhook_config_change();
-- +goose StatementEnd 
