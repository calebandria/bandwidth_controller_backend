package port

import (
	"bandwidth_controller_backend/internal/core/domain"
	"context"
)

type IPRateMonitor interface {
	GetActiveIPs() map[string]uint16
	GetInstantaneousClassStats(iface string, classID uint16) (domain.NetDevStats, error)
	CalculateIPRateMbps(ip string, iface string, classID uint16, currentStats domain.NetDevStats) (rateMbps float64, err error)
}

type NetworkDriver interface {
	ApplyShaping(ctx context.Context, rule domain.QoSRule) error
	SetupHTBStructure(ctx context.Context, ilan string, iwan string, totalBandwidth string) error
	ApplyGlobalShaping(ctx context.Context, rule domain.QoSRule) error
	ResetShaping(ctx context.Context, ilan string, iwan string) error
	GetConnectedLANIPs(ctx context.Context, ilan string) ([]string, error)
	CalculateRateMbps(ilan string, currentStats domain.NetDevStats) (txRateMbps float64, rxRateMbps float64, err error)
	GetInstantaneousNetDevStats(iface string) (domain.NetDevStats, error)
	RemoveIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error
	AddIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error
	IsHTBInitialized(ctx context.Context, lanInterface, wanInterface string) bool
}

type QoSService interface {
	SetSimpleGlobalLimit(ctx context.Context, rule domain.QoSRule) error
	SetupGlobalQoS(ctx context.Context, ilan string, iwan string, maxRate string) error
	UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error
	ResetQoS(ctx context.Context, ilan string, iwan string) error
	GetConnectedLANIPs(ctx context.Context, ilan string) ([]string, error)
	AddIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error
	RemoveIPRateLimit(ctx context.Context, ip string) error
	GetStatsStream() <-chan domain.IPTrafficStat
}
