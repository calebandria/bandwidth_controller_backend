package adapter

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type PostgresTrafficHistoryRepository struct {
	db *sql.DB
}

func NewPostgresTrafficHistoryRepository(db *sql.DB) port.TrafficHistoryRepository {
	return &PostgresTrafficHistoryRepository{db: db}
}

// SaveGlobalTraffic saves a global traffic snapshot
func (r *PostgresTrafficHistoryRepository) SaveGlobalTraffic(ctx context.Context, entry *domain.TrafficHistoryEntry) error {
	// Convert IPStats to JSON
	var ipStatsJSON []byte
	var err error
	if len(entry.IPStats) > 0 {
		ipStatsJSON, err = json.Marshal(entry.IPStats)
		if err != nil {
			return fmt.Errorf("failed to marshal IP stats: %w", err)
		}
	}

	query := `
		INSERT INTO traffic_history (
			timestamp, lan_upload_rate, lan_download_rate, 
			wan_upload_rate, wan_download_rate, ip_stats
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`

	var id int64
	err = r.db.QueryRowContext(ctx, query,
		entry.Timestamp,
		entry.LanUploadRate,
		entry.LanDownloadRate,
		entry.WanUploadRate,
		entry.WanDownloadRate,
		ipStatsJSON,
	).Scan(&id)

	if err != nil {
		return fmt.Errorf("failed to save global traffic: %w", err)
	}

	entry.ID = id
	return nil
}

// SaveIPTraffic saves per-IP traffic data
func (r *PostgresTrafficHistoryRepository) SaveIPTraffic(ctx context.Context, entries []*domain.IPTrafficHistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Use transaction for batch insert
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO ip_traffic_history (
			timestamp, ip, mac_address, hostname, 
			upload_rate, download_rate, bandwidth_limit
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		_, err := stmt.ExecContext(ctx,
			entry.Timestamp,
			entry.IP,
			entry.MACAddress,
			entry.Hostname,
			entry.UploadRate,
			entry.DownloadRate,
			entry.BandwidthLimit,
		)
		if err != nil {
			return fmt.Errorf("failed to insert IP traffic entry: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetGlobalTrafficHistory retrieves global traffic history for a time range
func (r *PostgresTrafficHistoryRepository) GetGlobalTrafficHistory(ctx context.Context, startTime, endTime time.Time, interval string) ([]*domain.TrafficHistoryEntry, error) {
	// Parse interval for aggregation (e.g., "5 minute" -> "minute")
	var query string
	var rows *sql.Rows
	var err error

	if interval != "" && interval != "raw" {
		// Extract the time unit from interval (e.g., "5 minute" -> "minute", "1 hour" -> "hour")
		truncUnit := interval
		if len(interval) > 2 && interval[1] == ' ' {
			// Format: "5 minute", "1 hour", etc. - extract the unit
			parts := strings.Fields(interval)
			if len(parts) == 2 {
				truncUnit = parts[1] // Get the time unit (minute, hour, day)
				// Remove trailing 's' if present (e.g., "minutes" -> "minute")
				truncUnit = strings.TrimSuffix(truncUnit, "s")
			}
		}

		// Aggregate data by time buckets
		query = `
			SELECT 
				date_trunc($1, timestamp) as timestamp,
				AVG(lan_upload_rate) as lan_upload_rate,
				AVG(lan_download_rate) as lan_download_rate,
				AVG(wan_upload_rate) as wan_upload_rate,
				AVG(wan_download_rate) as wan_download_rate
			FROM traffic_history
			WHERE timestamp >= $2 AND timestamp <= $3
			GROUP BY date_trunc($1, timestamp)
			ORDER BY timestamp ASC
		`
		rows, err = r.db.QueryContext(ctx, query, truncUnit, startTime, endTime)
	} else {
		// Return raw data
		query = `
			SELECT 
				id, timestamp, lan_upload_rate, lan_download_rate,
				wan_upload_rate, wan_download_rate, created_at
			FROM traffic_history
			WHERE timestamp >= $1 AND timestamp <= $2
			ORDER BY timestamp ASC
		`
		rows, err = r.db.QueryContext(ctx, query, startTime, endTime)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query global traffic history: %w", err)
	}
	defer rows.Close()

	var entries []*domain.TrafficHistoryEntry
	for rows.Next() {
		entry := &domain.TrafficHistoryEntry{}
		if interval != "" && interval != "raw" {
			err = rows.Scan(
				&entry.Timestamp,
				&entry.LanUploadRate,
				&entry.LanDownloadRate,
				&entry.WanUploadRate,
				&entry.WanDownloadRate,
			)
		} else {
			err = rows.Scan(
				&entry.ID,
				&entry.Timestamp,
				&entry.LanUploadRate,
				&entry.LanDownloadRate,
				&entry.WanUploadRate,
				&entry.WanDownloadRate,
				&entry.CreatedAt,
			)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to scan traffic history: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// GetIPTrafficHistory retrieves traffic history for a specific IP
func (r *PostgresTrafficHistoryRepository) GetIPTrafficHistory(ctx context.Context, ip string, startTime, endTime time.Time) ([]*domain.IPTrafficHistoryEntry, error) {
	query := `
		SELECT 
			id, timestamp, ip, mac_address, hostname,
			upload_rate, download_rate, bandwidth_limit, created_at
		FROM ip_traffic_history
		WHERE ip = $1 AND timestamp >= $2 AND timestamp <= $3
		ORDER BY timestamp ASC
	`

	rows, err := r.db.QueryContext(ctx, query, ip, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query IP traffic history: %w", err)
	}
	defer rows.Close()

	var entries []*domain.IPTrafficHistoryEntry
	for rows.Next() {
		entry := &domain.IPTrafficHistoryEntry{}
		var macAddress, hostname, bandwidthLimit sql.NullString
		err = rows.Scan(
			&entry.ID,
			&entry.Timestamp,
			&entry.IP,
			&macAddress,
			&hostname,
			&entry.UploadRate,
			&entry.DownloadRate,
			&bandwidthLimit,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan IP traffic history: %w", err)
		}

		if macAddress.Valid {
			entry.MACAddress = macAddress.String
		}
		if hostname.Valid {
			entry.Hostname = hostname.String
		}
		if bandwidthLimit.Valid {
			entry.BandwidthLimit = bandwidthLimit.String
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// GetTopConsumers returns the top N IPs by total traffic consumption
func (r *PostgresTrafficHistoryRepository) GetTopConsumers(ctx context.Context, startTime, endTime time.Time, limit int) ([]*domain.TopConsumer, error) {
	query := `
		WITH traffic_totals AS (
			SELECT 
				ip,
				-- Get the most recent MAC and hostname for this IP
				(SELECT mac_address FROM ip_traffic_history t2 
				 WHERE t2.ip = t1.ip AND t2.timestamp >= $1 AND t2.timestamp <= $2 
				 ORDER BY timestamp DESC LIMIT 1) as mac_address,
				(SELECT hostname FROM ip_traffic_history t2 
				 WHERE t2.ip = t1.ip AND t2.timestamp >= $1 AND t2.timestamp <= $2 
				 ORDER BY timestamp DESC LIMIT 1) as hostname,
				-- Calculate total MB by integrating rate over time (assuming 2-second intervals)
				SUM(upload_rate * 2 / 8) as total_upload_mb,      -- Mbps * seconds / 8 = MB
				SUM(download_rate * 2 / 8) as total_download_mb,
				AVG(upload_rate) as avg_upload_rate,
				AVG(download_rate) as avg_download_rate
			FROM ip_traffic_history t1
			WHERE timestamp >= $1 AND timestamp <= $2
			GROUP BY ip
		)
		SELECT 
			ip,
			COALESCE(mac_address, '') as mac_address,
			COALESCE(hostname, '') as hostname,
			total_upload_mb,
			total_download_mb,
			(total_upload_mb + total_download_mb) as total_traffic_mb,
			avg_upload_rate,
			avg_download_rate
		FROM traffic_totals
		ORDER BY total_traffic_mb DESC
		LIMIT $3
	`

	rows, err := r.db.QueryContext(ctx, query, startTime, endTime, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query top consumers: %w", err)
	}
	defer rows.Close()

	var consumers []*domain.TopConsumer
	for rows.Next() {
		consumer := &domain.TopConsumer{}
		err = rows.Scan(
			&consumer.IP,
			&consumer.MACAddress,
			&consumer.Hostname,
			&consumer.TotalUpload,
			&consumer.TotalDownload,
			&consumer.TotalTraffic,
			&consumer.AvgUploadRate,
			&consumer.AvgDownloadRate,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan top consumer: %w", err)
		}
		consumers = append(consumers, consumer)
	}

	return consumers, nil
}

// CleanupOldData removes traffic history older than the specified duration
func (r *PostgresTrafficHistoryRepository) CleanupOldData(ctx context.Context, olderThan time.Duration) error {
	cutoffTime := time.Now().Add(-olderThan)

	// Clean traffic_history
	_, err := r.db.ExecContext(ctx, `DELETE FROM traffic_history WHERE created_at < $1`, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup traffic_history: %w", err)
	}

	// Clean ip_traffic_history
	_, err = r.db.ExecContext(ctx, `DELETE FROM ip_traffic_history WHERE created_at < $1`, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup ip_traffic_history: %w", err)
	}

	return nil
}
