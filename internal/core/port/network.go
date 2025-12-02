package port

import (
    "bandwidth_controller_backend/internal/core/domain"
    "context"
)

// NetworkDriver abstracts the Linux Kernel interactions
type NetworkDriver interface {
    // TBF/Simple Methods
    ApplyShaping(ctx context.Context, rule domain.QoSRule) error // TBF implementation
    GetStatistics(ctx context.Context, iface string) (*domain.TrafficStat, error) // Global Stats (used by TBF mode)

    // HTB/Complex Methods
    SetupHTBStructure(ctx context.Context, iface string, totalBandwidth string) error
    ApplyGlobalShaping(ctx context.Context, rule domain.QoSRule) error // HTB 1:1 change
    ApplyDevicePolicy(ctx context.Context, policy domain.DevicePolicy) error
    RemoveDevicePolicy(ctx context.Context, policy domain.DevicePolicy) error // Added for completeness
    GetDeviceStatistics(ctx context.Context, iface string) ([]*domain.DeviceStat, error) // Added for future monitoring
    
    ResetShaping(ctx context.Context, iface string) error
}

// QoSService defines the business logic available to the API
type QoSService interface {
    // TBF Mode
    SetSimpleGlobalLimit(ctx context.Context, rule domain.QoSRule) error

    // HTB Mode
    SetupGlobalQoS(ctx context.Context, iface string, maxRate string) error
    UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error
    // (Future) ApplyDeviceLimit(ctx context.Context, policy domain.DevicePolicy) error
    
    // Monitoring
    GetLiveStats() <-chan domain.TrafficStat 
}