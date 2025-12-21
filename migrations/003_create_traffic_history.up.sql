-- Create traffic history table for storing historical bandwidth data
CREATE TABLE IF NOT EXISTS traffic_history (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- Global traffic stats
    lan_upload_rate DECIMAL(10, 4),      -- Mbps
    lan_download_rate DECIMAL(10, 4),    -- Mbps
    wan_upload_rate DECIMAL(10, 4),      -- Mbps
    wan_download_rate DECIMAL(10, 4),    -- Mbps
    
    -- Per-IP traffic stats (aggregated)
    ip_stats JSONB,                      -- Array of {ip, upload_rate, download_rate, mac_address}
    
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create index on timestamp for efficient time-range queries
CREATE INDEX IF NOT EXISTS idx_traffic_history_timestamp ON traffic_history(timestamp DESC);

-- Create index on created_at for cleanup queries
CREATE INDEX IF NOT EXISTS idx_traffic_history_created_at ON traffic_history(created_at);

-- Create a separate table for detailed IP traffic history
CREATE TABLE IF NOT EXISTS ip_traffic_history (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip VARCHAR(45) NOT NULL,              -- IPv4 or IPv6
    mac_address VARCHAR(17),              -- MAC address
    hostname VARCHAR(255),                -- Device hostname
    upload_rate DECIMAL(10, 4),           -- Mbps
    download_rate DECIMAL(10, 4),         -- Mbps
    bandwidth_limit VARCHAR(20),          -- e.g., "25mbit"
    
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create indexes for efficient queries
CREATE INDEX IF NOT EXISTS idx_ip_traffic_timestamp ON ip_traffic_history(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_ip_traffic_ip ON ip_traffic_history(ip);
CREATE INDEX IF NOT EXISTS idx_ip_traffic_ip_timestamp ON ip_traffic_history(ip, timestamp DESC);

-- Function to automatically clean up old data (older than 30 days)
CREATE OR REPLACE FUNCTION cleanup_old_traffic_history()
RETURNS void AS $$
BEGIN
    DELETE FROM traffic_history WHERE created_at < NOW() - INTERVAL '30 days';
    DELETE FROM ip_traffic_history WHERE created_at < NOW() - INTERVAL '30 days';
END;
$$ LANGUAGE plpgsql;

-- Optional: Create a scheduled job to run cleanup (requires pg_cron extension)
-- SELECT cron.schedule('cleanup-traffic-history', '0 2 * * *', 'SELECT cleanup_old_traffic_history()');
