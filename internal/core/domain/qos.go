package domain

import "time"

type QoSRule struct {
	LanInterface string
	WanInterface  string
	Bandwidth    string
	Latency      string
	Enabled      bool
}

type NetStat struct {
	Interface string
	BytesSent uint64
	BytesRecv uint64
	Timestamp time.Time
}

type TrafficStat struct {
	Interface string
	RxBytes   uint64 
	TxBytes   uint64 
	Timestamp time.Time
}
