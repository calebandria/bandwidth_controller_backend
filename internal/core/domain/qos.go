package domain

import "time"

// --- Policy for individual devices ---
type DevicePolicy struct {
    Interface string // The LAN interface (e.g., "wlo1")
    IPAddress string 
    MaxUploadRate string // Limit for traffic originating from the device (Ingress path)
    MaxDownloadRate string // Limit for traffic destined for the device (Egress path)
    Active bool
}

// --- Stats for individual devices (rate, not cumulative bytes) ---
type DeviceStat struct {
    IPAddress string
    RxRate float64 // Download speed (bytes/sec)
    TxRate float64 // Upload speed (bytes/sec)
    Timestamp time.Time
}

// ... (other entities like QoSRule, if still needed for global limits)
// QoSRule represents the target state
type QoSRule struct {
    Interface   string // e.g., "wlo1"
    Bandwidth   string // e.g., "10mbit", "500kbit" (Used instead of RateLimit)
    Latency     string // e.g., "50ms"
    Enabled     bool   // Whether the rule should be applied/active
}

type NetStat struct {
    Interface   string
    // Using BytesSent/Recv to be consistent with kernel file naming
    BytesSent   uint64 
    BytesRecv   uint64
    Timestamp   time.Time
}

// TrafficStat represents the current state
type TrafficStat struct {
    Interface string
    RxBytes   uint64 // Received bytes
    TxBytes   uint64 // Transmitted bytes
    Timestamp time.Time
}

