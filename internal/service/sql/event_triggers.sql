-- Event trigger definitions for webhook service

-- Create function to notify on webhook event changes
CREATE OR REPLACE FUNCTION notify_webhook_event_change() RETURNS TRIGGER AS $$
DECLARE
    payload text;
BEGIN
    -- Only notify for pending events that are ready to be processed
    -- This includes events that have just been inserted, updated to pending status,
    -- or have their next_attempt_at time in the past
    IF (TG_OP = 'INSERT' OR 
        (TG_OP = 'UPDATE' AND NEW.status = 'pending')) AND 
       (NEW.next_attempt_at IS NULL OR NEW.next_attempt_at <= CURRENT_TIMESTAMP) THEN
        -- Create the payload with more information
        -- Explicitly cast IDs to text to ensure they're sent as strings
        payload := json_build_object(
            'event_id', NEW.id::text,
            'webhook_id', NEW.webhook_id::text,
            'operation', TG_OP,
            'timestamp', extract(epoch from now())
        )::text;
        
        -- Log the notification for debugging
        RAISE NOTICE 'Sending webhook event notification: %', payload;
        
        -- Send the notification
        PERFORM pg_notify('webhook_event_changes', payload);
    END IF;
    RETURN NEW;
EXCEPTION WHEN OTHERS THEN
    -- Log any errors but don't fail the transaction
    RAISE WARNING 'Error in webhook event notification trigger: %', SQLERRM;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for webhook event changes
DROP TRIGGER IF EXISTS webhook_event_change_trigger ON webhook_events;
CREATE TRIGGER webhook_event_change_trigger
AFTER INSERT OR UPDATE ON webhook_events
FOR EACH ROW EXECUTE FUNCTION notify_webhook_event_change();

-- Create an index to improve performance of the polling query
CREATE INDEX IF NOT EXISTS idx_webhook_events_pending_next_attempt 
ON webhook_events (status, next_attempt_at) 
WHERE status = 'pending';

-- Create a function to check for events that have reached their retry time
CREATE OR REPLACE FUNCTION check_retry_events() RETURNS INTEGER AS $$
DECLARE
    ready_events CURSOR FOR 
        SELECT id, webhook_id
        FROM webhook_events
        WHERE status = 'pending'
        AND next_attempt_at IS NOT NULL
        AND next_attempt_at <= CURRENT_TIMESTAMP
        AND (processing_instance IS NULL OR processing_started_at < CURRENT_TIMESTAMP - INTERVAL '3 minutes')
        LIMIT 50;
    event_record RECORD;
    payload text;
    processed_count INTEGER := 0;
BEGIN
    -- This function checks for events that have reached their retry time
    -- and sends notifications for them
    
    OPEN ready_events;
    LOOP
        FETCH ready_events INTO event_record;
        EXIT WHEN NOT FOUND;
        
        -- Create the payload
        -- Explicitly cast IDs to text to ensure they're sent as strings
        payload := json_build_object(
            'event_id', event_record.id::text,
            'webhook_id', event_record.webhook_id::text,
            'operation', 'RETRY',
            'timestamp', extract(epoch from now())
        )::text;
        
        -- Log the notification for debugging
        RAISE NOTICE 'Sending retry notification for event %: %', event_record.id, payload;
        
        -- Send the notification
        PERFORM pg_notify('webhook_event_changes', payload);
        
        processed_count := processed_count + 1;
    END LOOP;
    CLOSE ready_events;
    
    IF processed_count > 0 THEN
        RAISE NOTICE 'Processed % events ready for retry', processed_count;
    END IF;
    
    RETURN processed_count;
EXCEPTION WHEN OTHERS THEN
    -- Log any errors but don't fail the transaction
    RAISE WARNING 'Error in check_retry_events function: %', SQLERRM;
    RETURN 0;
END;
$$ LANGUAGE plpgsql;

-- Create a table to track the last time the retry check was run
CREATE TABLE IF NOT EXISTS webhook_system_state (
    key TEXT PRIMARY KEY,
    last_run TIMESTAMP WITH TIME ZONE,
    value TEXT
);

-- Insert initial record for retry check
INSERT INTO webhook_system_state (key, last_run, value)
VALUES ('retry_check', CURRENT_TIMESTAMP, 'initialized')
ON CONFLICT (key) DO UPDATE
SET last_run = CURRENT_TIMESTAMP, value = 'updated'; 
