-- Create schedule_rules table to persist bandwidth scheduling rules
CREATE TABLE IF NOT EXISTS schedule_rules (
    id VARCHAR(100) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    rate_mbps INTEGER NOT NULL CHECK (rate_mbps > 0),
    cron_expr VARCHAR(100) NOT NULL,
    duration INTEGER NOT NULL CHECK (duration > 0), -- Duration in minutes
    priority INTEGER NOT NULL DEFAULT 5 CHECK (priority >= 1 AND priority <= 10), -- Priority 1-10, higher = more important
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create index on enabled rules for faster queries
CREATE INDEX idx_schedule_rules_enabled ON schedule_rules(enabled);

-- Create trigger to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_schedule_rules_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_schedule_rules_updated_at
    BEFORE UPDATE ON schedule_rules
    FOR EACH ROW
    EXECUTE FUNCTION update_schedule_rules_updated_at();
