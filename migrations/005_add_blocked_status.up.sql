-- Migration: Add blocked status to devices table
-- File: 005_add_blocked_status.up.sql

-- Add is_blocked column to track blocked devices
ALTER TABLE devices ADD COLUMN IF NOT EXISTS is_blocked BOOLEAN DEFAULT FALSE;

-- Create index for quick lookup of blocked devices
CREATE INDEX IF NOT EXISTS idx_devices_blocked ON devices(is_blocked) WHERE is_blocked = TRUE;
