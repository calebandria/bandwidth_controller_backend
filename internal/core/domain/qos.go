package domain

import (
	"sync"
	"time"
)

type QoSRule struct {
	LanInterface string
	WanInterface string
	Bandwidth    string
	Latency      string
	Enabled      bool
}

type TrafficStat struct {
	Interface string
	RxBytes   uint64
	TxBytes   uint64
	Timestamp time.Time
}

type NetDevStats struct {
	RxBytes uint64
	TxBytes uint64
}

type TrafficState struct {
	LastStats NetDevStats
	LastTime  time.Time
	Mu        sync.Mutex // To protect concurrent access
}

type IPTrafficStat struct {
	IP             string  `json:"ip" example:"192.168.1.10"`
	MACAddress     string  `json:"mac_address,omitempty" example:"AA:BB:CC:DD:EE:FF"` // MAC address of the device
	Hostname       string  `json:"hostname,omitempty" example:"johns-laptop"`         // Device hostname if available
	UploadRate     float64 `json:"upload_rate_mbps" example:"5.2"`                    // Taux d'upload instantané en Mbps
	DownloadRate   float64 `json:"download_rate_mbps" example:"10.8"`                 // Taux de download instantané en Mbps
	IsLimited      bool    `json:"is_limited"`                                        // Indique si une règle personnalisée est appliquée (au-delà du défaut)
	BandwidthLimit string  `json:"bandwidth_limit" example:"25mbit"`                  // Limite de bande passante appliquée (e.g., "25mbit", "100mbit")
	Status         string  `json:"status"`                                            // Ex: "Active", "Disconnected"
}

type GlobalTrafficStat struct {
	LanInterface    string    `json:"lan_interface" example:"wlo1"`
	WanInterface    string    `json:"wan_interface" example:"eno2"`
	LanUploadRate   float64   `json:"lan_upload_rate_mbps" example:"15.3"`   // Upload depuis LAN (Tx sur LAN)
	LanDownloadRate float64   `json:"lan_download_rate_mbps" example:"45.8"` // Download vers LAN (Rx sur LAN)
	WanUploadRate   float64   `json:"wan_upload_rate_mbps" example:"15.2"`   // Upload vers WAN (Tx sur WAN)
	WanDownloadRate float64   `json:"wan_download_rate_mbps" example:"45.9"` // Download depuis WAN (Rx sur WAN)
	TotalActiveIPs  int       `json:"total_active_ips" example:"5"`
	TotalLimitedIPs int       `json:"total_limited_ips" example:"3"`
	GlobalLimit     string    `json:"global_limit" example:"100mbit"` // Current global bandwidth limit
	Timestamp       time.Time `json:"timestamp"`
}

type TrafficUpdate struct {
	Type       string             `json:"type"` // "ip" ou "global"
	IPStat     *IPTrafficStat     `json:"ip_stat,omitempty"`
	GlobalStat *GlobalTrafficStat `json:"global_stat,omitempty"`
}
