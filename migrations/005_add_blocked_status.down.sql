-- Migration rollback: Remove blocked status from devices table
-- File: 005_add_blocked_status.down.sql

-- Drop the index first
DROP INDEX IF EXISTS idx_devices_blocked;

-- Remove is_blocked column
ALTER TABLE devices DROP COLUMN IF EXISTS is_blocked;
