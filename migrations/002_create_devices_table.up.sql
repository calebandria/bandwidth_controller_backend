-- Migration: Create devices table for IP->MAC tracking and bandwidth limits
-- File: 002_create_devices_table.up.sql

CREATE TABLE IF NOT EXISTS devices (
    id SERIAL PRIMARY KEY,
    ip VARCHAR(45) NOT NULL UNIQUE,
    mac_address VARCHAR(17) NOT NULL,
    bandwidth_limit VARCHAR(20) DEFAULT NULL,
    device_name VARCHAR(255) DEFAULT NULL,
    last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_devices_ip ON devices(ip);
CREATE INDEX idx_devices_mac ON devices(mac_address);
CREATE INDEX idx_devices_last_seen ON devices(last_seen);

-- Auto-update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_devices_updated_at BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
