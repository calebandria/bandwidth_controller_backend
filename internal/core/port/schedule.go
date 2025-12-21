package port

import (
	"bandwidth_controller_backend/internal/core/domain"
	"context"
)

// ScheduleRepository handles storage and retrieval of schedule rules
type ScheduleRepository interface {
	// GetAll retrieves all schedule rules
	GetAll(ctx context.Context) ([]domain.ScheduleRule, error)

	// GetByID retrieves a schedule rule by ID
	GetByID(ctx context.Context, id string) (*domain.ScheduleRule, error)

	// Create creates a new schedule rule
	Create(ctx context.Context, rule *domain.ScheduleRule) error

	// Update updates an existing schedule rule
	Update(ctx context.Context, rule *domain.ScheduleRule) error

	// Delete deletes a schedule rule by ID
	Delete(ctx context.Context, id string) error

	// GetEnabled retrieves all enabled schedule rules
	GetEnabled(ctx context.Context) ([]domain.ScheduleRule, error)
}
