package port

import (
	"bandwidth_controller_backend/internal/core/domain"
	"context"
	"time"
)

// TrafficHistoryRepository handles storage and retrieval of traffic history
type TrafficHistoryRepository interface {
	// SaveGlobalTraffic saves a global traffic snapshot
	SaveGlobalTraffic(ctx context.Context, entry *domain.TrafficHistoryEntry) error

	// SaveIPTraffic saves per-IP traffic data
	SaveIPTraffic(ctx context.Context, entries []*domain.IPTrafficHistoryEntry) error

	// GetGlobalTrafficHistory retrieves global traffic history for a time range
	GetGlobalTrafficHistory(ctx context.Context, startTime, endTime time.Time, interval string) ([]*domain.TrafficHistoryEntry, error)

	// GetIPTrafficHistory retrieves traffic history for a specific IP
	GetIPTrafficHistory(ctx context.Context, ip string, startTime, endTime time.Time) ([]*domain.IPTrafficHistoryEntry, error)

	// GetTopConsumers returns the top N IPs by total traffic consumption
	GetTopConsumers(ctx context.Context, startTime, endTime time.Time, limit int) ([]*domain.TopConsumer, error)

	// CleanupOldData removes traffic history older than the specified duration
	CleanupOldData(ctx context.Context, olderThan time.Duration) error
}
