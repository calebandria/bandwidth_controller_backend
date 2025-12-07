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
