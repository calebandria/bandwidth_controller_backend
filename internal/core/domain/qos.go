package domain

import (
	"sync"
	"time"
)

type QoSRule struct {
	LanInterface string
	WanInterface  string
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
    LastTime time.Time
    Mu sync.Mutex // To protect concurrent access
}

type IPTrafficStat struct {
	IP           string    `json:"ip" example:"192.168.1.10"`
	UploadRate   float64   `json:"upload_rate_mbps" example:"5.2"`   // Taux d'upload instantané en Mbps
	DownloadRate float64   `json:"download_rate_mbps" example:"10.8"` // Taux de download instantané en Mbps
	IsLimited    bool      `json:"is_limited"` // Indique si une règle personnalisée est appliquée (au-delà du défaut)
	Status       string    `json:"status"`     // Ex: "Active", "Disconnected"
}