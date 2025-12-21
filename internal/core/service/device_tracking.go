package service

import (
	"bandwidth_controller_backend/internal/core/domain"
	"context"
	"log"
	"time"
)

// trackDeviceOnConnect handles device tracking and limit re-application when an IP connects
func (s *QoSManager) trackDeviceOnConnect(ctx context.Context, ip string, now time.Time) {
	// Resolve MAC address
	macDriver, ok := s.driver.(interface {
		GetMACFromIP(context.Context, string) (string, error)
	})
	if !ok || s.deviceRepo == nil {
		return // MAC resolution or device repo not available
	}

	mac, err := macDriver.GetMACFromIP(ctx, ip)
	if err != nil {
		log.Printf("Could not resolve MAC for IP %s: %v", ip, err)
		return
	}

	// Check database for this IP
	device, err := s.deviceRepo.FindByIP(ctx, ip)
	if err != nil {
		log.Printf("Error querying device database for IP %s: %v", ip, err)
		return
	}

	if device != nil {
		// Device record exists - check if MAC matches
		if device.MACAddress == mac {
			log.Printf("Known device %s (MAC: %s) reconnected", ip, mac)

			// Re-apply saved bandwidth limit if exists
			if device.BandwidthLimit != nil && *device.BandwidthLimit != "" {
				log.Printf("Re-applying saved limit %s to %s", *device.BandwidthLimit, ip)
				rule := domain.QoSRule{
					LanInterface: s.ilan,
					WanInterface: s.inet,
					Bandwidth:    *device.BandwidthLimit,
				}
				if err := s.AddIPRateLimit(ctx, ip, rule); err != nil {
					log.Printf("Failed to re-apply limit to %s: %v", ip, err)
				}
			}

			// Update last_seen
			if err := s.deviceRepo.UpdateLastSeen(ctx, ip); err != nil {
				log.Printf("Failed to update last_seen for %s: %v", ip, err)
			}
		} else {
			// MAC address changed - different device using same IP
			log.Printf("IP %s MAC changed: %s -> %s. Different device detected, clearing limit.", ip, device.MACAddress, mac)

			// CRITICAL: Remove the active tc limit for this IP
			if err := s.RemoveIPRateLimit(ctx, ip); err != nil {
				log.Printf("Warning: Failed to remove active limit for IP %s: %v", ip, err)
				// Continue anyway - we still want to update the database
			}

			// Delete old device record (old MAC with old limit)
			if err := s.deviceRepo.Delete(ctx, ip); err != nil {
				log.Printf("Failed to delete old device record: %v", err)
			}

			// Create new device record WITHOUT bandwidth limit
			newDevice := &domain.Device{
				IP:             ip,
				MACAddress:     mac,
				BandwidthLimit: nil, // New device gets no limit
				LastSeen:       now,
			}
			if err := s.deviceRepo.Upsert(ctx, newDevice); err != nil {
				log.Printf("Failed to create new device record: %v", err)
			}

			log.Printf("New device %s (MAC: %s) will use global limit", ip, mac)
		}
	} else {
		// New device - create record
		log.Printf("New device detected: IP %s, MAC %s", ip, mac)
		newDevice := &domain.Device{
			IP:         ip,
			MACAddress: mac,
			LastSeen:   now,
		}
		if err := s.deviceRepo.Upsert(ctx, newDevice); err != nil {
			log.Printf("Failed to create device record: %v", err)
		}
	}
}

// persistIPLimit saves the bandwidth limit for an IP to the database
func (s *QoSManager) persistIPLimit(ctx context.Context, ip string, limit string) {
	if s.deviceRepo == nil {
		return // Database not available
	}

	// Get MAC address
	macDriver, ok := s.driver.(interface {
		GetMACFromIP(context.Context, string) (string, error)
	})
	if !ok {
		log.Printf("Cannot persist limit: MAC resolution not available")
		return
	}

	mac, err := macDriver.GetMACFromIP(ctx, ip)
	if err != nil {
		log.Printf("Cannot persist limit for %s: MAC resolution failed: %v", ip, err)
		return
	}

	// Upsert device with limit
	device := &domain.Device{
		IP:             ip,
		MACAddress:     mac,
		BandwidthLimit: &limit,
		LastSeen:       time.Now(),
	}

	if err := s.deviceRepo.Upsert(ctx, device); err != nil {
		log.Printf("Failed to persist bandwidth limit for %s: %v", ip, err)
	} else {
		log.Printf("Bandwidth limit %s persisted for device %s (MAC: %s)", limit, ip, mac)
	}
}
