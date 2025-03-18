-- Webhook config trigger definitions for webhook service

-- Create function to notify on webhook config changes
CREATE OR REPLACE FUNCTION notify_webhook_config_change() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('webhook_config_changes', json_build_object(
        'webhook_id', (CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END)::text,
        'operation', TG_OP
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for webhook config changes
CREATE TRIGGER IF NOT EXISTS webhook_config_change_trigger
AFTER UPDATE OR DELETE ON webhook_configs
FOR EACH ROW EXECUTE FUNCTION notify_webhook_config_change(); 
