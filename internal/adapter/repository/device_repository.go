package adapter

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"database/sql"
	"fmt"
)

type PostgresDeviceRepository struct {
	db *sql.DB
}

func NewPostgresDeviceRepository(db *sql.DB) port.DeviceRepository {
	return &PostgresDeviceRepository{db: db}
}

func (r *PostgresDeviceRepository) FindByIP(ctx context.Context, ip string) (*domain.Device, error) {
	query := `
		SELECT id, ip, mac_address, bandwidth_limit, device_name, is_blocked, last_seen, created_at, updated_at
		FROM devices
		WHERE ip = $1
	`

	var device domain.Device
	err := r.db.QueryRowContext(ctx, query, ip).Scan(
		&device.ID,
		&device.IP,
		&device.MACAddress,
		&device.BandwidthLimit,
		&device.DeviceName,
		&device.IsBlocked,
		&device.LastSeen,
		&device.CreatedAt,
		&device.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find device by IP: %w", err)
	}

	return &device, nil
}

func (r *PostgresDeviceRepository) FindByMAC(ctx context.Context, mac string) (*domain.Device, error) {
	query := `
		SELECT id, ip, mac_address, bandwidth_limit, device_name, is_blocked, last_seen, created_at, updated_at
		FROM devices
		WHERE mac_address = $1
	`

	var device domain.Device
	err := r.db.QueryRowContext(ctx, query, mac).Scan(
		&device.ID,
		&device.IP,
		&device.MACAddress,
		&device.BandwidthLimit,
		&device.DeviceName,
		&device.IsBlocked,
		&device.LastSeen,
		&device.CreatedAt,
		&device.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find device by MAC: %w", err)
	}

	return &device, nil
}

func (r *PostgresDeviceRepository) Upsert(ctx context.Context, device *domain.Device) error {
	query := `
		INSERT INTO devices (ip, mac_address, bandwidth_limit, device_name, is_blocked, last_seen)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (ip) DO UPDATE SET
			mac_address = EXCLUDED.mac_address,
			bandwidth_limit = COALESCE(EXCLUDED.bandwidth_limit, devices.bandwidth_limit),
			device_name = COALESCE(EXCLUDED.device_name, devices.device_name),
			is_blocked = COALESCE(EXCLUDED.is_blocked, devices.is_blocked),
			last_seen = EXCLUDED.last_seen,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRowContext(ctx, query,
		device.IP,
		device.MACAddress,
		device.BandwidthLimit,
		device.DeviceName,
		device.IsBlocked,
		device.LastSeen,
	).Scan(&device.ID, &device.CreatedAt, &device.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert device: %w", err)
	}

	return nil
}

func (r *PostgresDeviceRepository) UpdateLimit(ctx context.Context, ip string, limit *string) error {
	query := `
		UPDATE devices
		SET bandwidth_limit = $1, updated_at = CURRENT_TIMESTAMP
		WHERE ip = $2
	`

	result, err := r.db.ExecContext(ctx, query, limit, ip)
	if err != nil {
		return fmt.Errorf("failed to update device limit: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("device with IP %s not found", ip)
	}

	return nil
}

func (r *PostgresDeviceRepository) UpdateLastSeen(ctx context.Context, ip string) error {
	query := `
		UPDATE devices
		SET last_seen = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE ip = $1
	`

	_, err := r.db.ExecContext(ctx, query, ip)
	if err != nil {
		return fmt.Errorf("failed to update last_seen: %w", err)
	}

	return nil
}

func (r *PostgresDeviceRepository) ListAll(ctx context.Context) ([]domain.Device, error) {
	query := `
		SELECT id, ip, mac_address, bandwidth_limit, device_name, is_blocked, last_seen, created_at, updated_at
		FROM devices
		ORDER BY last_seen DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}
	defer rows.Close()

	var devices []domain.Device
	for rows.Next() {
		var device domain.Device
		err := rows.Scan(
			&device.ID,
			&device.IP,
			&device.MACAddress,
			&device.BandwidthLimit,
			&device.DeviceName,
			&device.IsBlocked,
			&device.LastSeen,
			&device.CreatedAt,
			&device.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan device: %w", err)
		}
		devices = append(devices, device)
	}

	return devices, nil
}

func (r *PostgresDeviceRepository) UpdateBlockedStatus(ctx context.Context, ip string, isBlocked bool) error {
	query := `
		UPDATE devices
		SET is_blocked = $1, updated_at = CURRENT_TIMESTAMP
		WHERE ip = $2
	`

	result, err := r.db.ExecContext(ctx, query, isBlocked, ip)
	if err != nil {
		return fmt.Errorf("failed to update blocked status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("device with IP %s not found", ip)
	}

	return nil
}

func (r *PostgresDeviceRepository) ListBlocked(ctx context.Context) ([]domain.Device, error) {
	query := `
		SELECT id, ip, mac_address, bandwidth_limit, device_name, is_blocked, last_seen, created_at, updated_at
		FROM devices
		WHERE is_blocked = TRUE
		ORDER BY last_seen DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list blocked devices: %w", err)
	}
	defer rows.Close()

	var devices []domain.Device
	for rows.Next() {
		var device domain.Device
		err := rows.Scan(
			&device.ID,
			&device.IP,
			&device.MACAddress,
			&device.BandwidthLimit,
			&device.DeviceName,
			&device.IsBlocked,
			&device.LastSeen,
			&device.CreatedAt,
			&device.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan device: %w", err)
		}
		devices = append(devices, device)
	}

	return devices, nil
}

func (r *PostgresDeviceRepository) Delete(ctx context.Context, ip string) error {
	query := `DELETE FROM devices WHERE ip = $1`

	result, err := r.db.ExecContext(ctx, query, ip)
	if err != nil {
		return fmt.Errorf("failed to delete device: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("device with IP %s not found", ip)
	}

	return nil
}
