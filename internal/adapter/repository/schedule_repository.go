package adapter

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"database/sql"
	"fmt"
)

type PostgresScheduleRepository struct {
	db *sql.DB
}

func NewPostgresScheduleRepository(db *sql.DB) port.ScheduleRepository {
	return &PostgresScheduleRepository{db: db}
}

// GetAll retrieves all schedule rules
func (r *PostgresScheduleRepository) GetAll(ctx context.Context) ([]domain.ScheduleRule, error) {
	query := `
		SELECT id, name, description, rate_mbps, cron_expr, duration, priority, enabled
		FROM schedule_rules
		ORDER BY priority DESC, created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query schedule rules: %w", err)
	}
	defer rows.Close()

	var rules []domain.ScheduleRule
	for rows.Next() {
		var rule domain.ScheduleRule
		var description sql.NullString

		err = rows.Scan(
			&rule.ID,
			&rule.Name,
			&description,
			&rule.RateMbps,
			&rule.CronExpr,
			&rule.Duration,
			&rule.Priority,
			&rule.Enabled,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan schedule rule: %w", err)
		}

		if description.Valid {
			rule.Description = description.String
		}

		rules = append(rules, rule)
	}

	return rules, nil
}

// GetByID retrieves a schedule rule by ID
func (r *PostgresScheduleRepository) GetByID(ctx context.Context, id string) (*domain.ScheduleRule, error) {
	query := `
		SELECT id, name, description, rate_mbps, cron_expr, duration, priority, enabled
		FROM schedule_rules
		WHERE id = $1
	`

	var rule domain.ScheduleRule
	var description sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&rule.ID,
		&rule.Name,
		&description,
		&rule.RateMbps,
		&rule.CronExpr,
		&rule.Duration,
		&rule.Priority,
		&rule.Enabled,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("schedule rule not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule rule: %w", err)
	}

	if description.Valid {
		rule.Description = description.String
	}

	return &rule, nil
}

// Create creates a new schedule rule
func (r *PostgresScheduleRepository) Create(ctx context.Context, rule *domain.ScheduleRule) error {
	query := `
		INSERT INTO schedule_rules (id, name, description, rate_mbps, cron_expr, duration, priority, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := r.db.ExecContext(ctx, query,
		rule.ID,
		rule.Name,
		sql.NullString{String: rule.Description, Valid: rule.Description != ""},
		rule.RateMbps,
		rule.CronExpr,
		rule.Duration,
		rule.Priority,
		rule.Enabled,
	)

	if err != nil {
		return fmt.Errorf("failed to create schedule rule: %w", err)
	}

	return nil
}

// Update updates an existing schedule rule
func (r *PostgresScheduleRepository) Update(ctx context.Context, rule *domain.ScheduleRule) error {
	query := `
		UPDATE schedule_rules
		SET name = $2, description = $3, rate_mbps = $4, cron_expr = $5, duration = $6, priority = $7, enabled = $8
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query,
		rule.ID,
		rule.Name,
		sql.NullString{String: rule.Description, Valid: rule.Description != ""},
		rule.RateMbps,
		rule.CronExpr,
		rule.Duration,
		rule.Priority,
		rule.Enabled,
	)

	if err != nil {
		return fmt.Errorf("failed to update schedule rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("schedule rule not found: %s", rule.ID)
	}

	return nil
}

// Delete deletes a schedule rule by ID
func (r *PostgresScheduleRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM schedule_rules WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete schedule rule: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("schedule rule not found: %s", id)
	}

	return nil
}

// GetEnabled retrieves all enabled schedule rules
func (r *PostgresScheduleRepository) GetEnabled(ctx context.Context) ([]domain.ScheduleRule, error) {
	query := `
		SELECT id, name, description, rate_mbps, cron_expr, duration, priority, enabled
		FROM schedule_rules
		WHERE enabled = true
		ORDER BY priority DESC, created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled schedule rules: %w", err)
	}
	defer rows.Close()

	var rules []domain.ScheduleRule
	for rows.Next() {
		var rule domain.ScheduleRule
		var description sql.NullString

		err = rows.Scan(
			&rule.ID,
			&rule.Name,
			&description,
			&rule.RateMbps,
			&rule.CronExpr,
			&rule.Duration,
			&rule.Priority,
			&rule.Enabled,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan schedule rule: %w", err)
		}

		if description.Valid {
			rule.Description = description.String
		}

		rules = append(rules, rule)
	}

	return rules, nil
}
