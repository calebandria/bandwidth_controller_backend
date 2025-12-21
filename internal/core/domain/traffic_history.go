package domain

import "time"

// TrafficHistoryEntry represents a snapshot of global traffic at a point in time
type TrafficHistoryEntry struct {
	ID              int64                  `json:"id"`
	Timestamp       time.Time              `json:"timestamp"`
	LanUploadRate   float64                `json:"lan_upload_rate"`
	LanDownloadRate float64                `json:"lan_download_rate"`
	WanUploadRate   float64                `json:"wan_upload_rate"`
	WanDownloadRate float64                `json:"wan_download_rate"`
	IPStats         []IPTrafficHistoryStat `json:"ip_stats,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
}

// IPTrafficHistoryEntry represents per-IP traffic at a point in time
type IPTrafficHistoryEntry struct {
	ID             int64     `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	IP             string    `json:"ip"`
	MACAddress     string    `json:"mac_address,omitempty"`
	Hostname       string    `json:"hostname,omitempty"`
	UploadRate     float64   `json:"upload_rate"`
	DownloadRate   float64   `json:"download_rate"`
	BandwidthLimit string    `json:"bandwidth_limit,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// IPTrafficHistoryStat is a simplified version for JSONB storage
type IPTrafficHistoryStat struct {
	IP           string  `json:"ip"`
	UploadRate   float64 `json:"upload_rate"`
	DownloadRate float64 `json:"download_rate"`
	MACAddress   string  `json:"mac_address,omitempty"`
}

// TrafficStatsRequest represents query parameters for traffic history
type TrafficStatsRequest struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	IP        string    `json:"ip,omitempty"` // Optional: filter by specific IP
	Interval  string    `json:"interval"`     // e.g., "1m", "5m", "1h" for aggregation
}

// TopConsumer represents an IP with total traffic consumption
type TopConsumer struct {
	IP              string  `json:"ip"`
	MACAddress      string  `json:"mac_address,omitempty"`
	Hostname        string  `json:"hostname,omitempty"`
	TotalUpload     float64 `json:"total_upload_mb"`   // Total MB uploaded
	TotalDownload   float64 `json:"total_download_mb"` // Total MB downloaded
	TotalTraffic    float64 `json:"total_traffic_mb"`  // Total traffic (upload + download)
	AvgUploadRate   float64 `json:"avg_upload_rate"`   // Average upload rate in Mbps
	AvgDownloadRate float64 `json:"avg_download_rate"` // Average download rate in Mbps
}
