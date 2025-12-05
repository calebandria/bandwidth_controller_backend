package port

import (
    "bandwidth_controller_backend/internal/core/domain"
    "context"
)

type NetworkDriver interface {
    ApplyShaping(ctx context.Context, rule domain.QoSRule) error 
    SetupHTBStructure(ctx context.Context, ilan string, iwan string, totalBandwidth string) error
    ApplyGlobalShaping(ctx context.Context, rule domain.QoSRule) error 
    ResetShaping(ctx context.Context, ilan string, iwan string) error
}

type QoSService interface {
    SetSimpleGlobalLimit(ctx context.Context, rule domain.QoSRule) error
    SetupGlobalQoS(ctx context.Context, ilan string, iwan string, maxRate string) error
    UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error
    ResetQoS(ctx context.Context, ilan string, iwan string) error
}