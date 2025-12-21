-- Drop schedule_rules table
DROP TRIGGER IF EXISTS trigger_update_schedule_rules_updated_at ON schedule_rules;
DROP FUNCTION IF EXISTS update_schedule_rules_updated_at();
DROP INDEX IF EXISTS idx_schedule_rules_enabled;
DROP TABLE IF EXISTS schedule_rules;
