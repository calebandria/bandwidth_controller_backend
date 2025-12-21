package domain

import "time"

// Device represents a tracked network device with IP->MAC mapping
type Device struct {
	ID             int       `json:"id" db:"id"`
	IP             string    `json:"ip" db:"ip"`
	MACAddress     string    `json:"mac_address" db:"mac_address"`
	BandwidthLimit *string   `json:"bandwidth_limit" db:"bandwidth_limit"` // NULL if no custom limit
	DeviceName     *string   `json:"device_name" db:"device_name"`
	IsBlocked      bool      `json:"is_blocked" db:"is_blocked"` // Whether device is blocked from internet
	LastSeen       time.Time `json:"last_seen" db:"last_seen"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}
