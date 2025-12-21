package port

import (
	"bandwidth_controller_backend/internal/core/domain"
	"context"
)

// DeviceRepository handles persistence of device IP->MAC mappings and limits
type DeviceRepository interface {
	// FindByIP retrieves a device by its IP address
	FindByIP(ctx context.Context, ip string) (*domain.Device, error)

	// FindByMAC retrieves a device by its MAC address
	FindByMAC(ctx context.Context, mac string) (*domain.Device, error)

	// Upsert creates or updates a device record
	Upsert(ctx context.Context, device *domain.Device) error

	// UpdateLimit updates the bandwidth limit for a device
	UpdateLimit(ctx context.Context, ip string, limit *string) error

	// UpdateLastSeen updates the last_seen timestamp
	UpdateLastSeen(ctx context.Context, ip string) error

	// ListAll returns all tracked devices
	ListAll(ctx context.Context) ([]domain.Device, error)

	// Delete removes a device record
	Delete(ctx context.Context, ip string) error
}
