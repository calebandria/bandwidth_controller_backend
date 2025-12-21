-- Rollback traffic history tables
DROP TABLE IF EXISTS ip_traffic_history;
DROP TABLE IF EXISTS traffic_history;
DROP FUNCTION IF EXISTS cleanup_old_traffic_history();
